// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package plex

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

const (
	testMachineID   = "server-machine-id"
	testServerToken = "server-token"
	testClientID    = "zaparoo-core-test"
)

// fakeServer is a Plex Media Server with an in-memory library.
type fakeServer struct {
	*httptest.Server
	metadata    map[string]Metadata
	sectionData map[string][]Metadata
	leaves      map[string][]Metadata
	queue       []Metadata
	sections    []Section
	clients     []ServerClient
	requests    []*http.Request
	mu          syncutil.Mutex
	// filtersGUID makes the server honour the guid filter; a server that
	// ignores it returns every item of the type.
	filtersGUID bool
	// primaryGUIDOnly makes the guid filter match only an item's own
	// plex:// guid, so a filter on an external identity finds nothing.
	primaryGUIDOnly bool
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{
		metadata:    map[string]Metadata{},
		sectionData: map[string][]Metadata{},
		leaves:      map[string][]Metadata{},
		filtersGUID: true,
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

// addSection adds a library section holding items.
func (f *fakeServer) addSection(key, typ, title string, items ...Metadata) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sections = append(f.sections, Section{Key: key, Type: typ, Title: title})
	f.sectionData[key] = append(f.sectionData[key], items...)
	for i := range items {
		f.metadata[items[i].RatingKey] = items[i]
	}
}

func (f *fakeServer) seen() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*http.Request(nil), f.requests...)
}

func (f *fakeServer) seenPath(path string) []*http.Request {
	var out []*http.Request
	for _, r := range f.seen() {
		if r.URL.Path == path {
			out = append(out, r)
		}
	}
	return out
}

func writeContainer(w http.ResponseWriter, mc *mediaContainer) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(containerResponse{MediaContainer: *mc})
}

func page(items []Metadata, q url.Values) mediaContainer {
	start, _ := strconv.Atoi(q.Get("X-Plex-Container-Start"))
	size, err := strconv.Atoi(q.Get("X-Plex-Container-Size"))
	if err != nil || size <= 0 {
		size = len(items)
	}
	total := len(items)
	if start > total {
		start = total
	}
	end := min(start+size, total)
	return mediaContainer{
		Metadata:  items[start:end],
		Size:      FlexInt(end - start),
		TotalSize: FlexInt(total),
	}
}

func (f *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Clone(r.Context()))
	f.mu.Unlock()
	if r.Header.Get("X-Plex-Token") != testServerToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/identity":
		writeContainer(w, &mediaContainer{MachineIdentifier: testMachineID, Version: "1.40.0"})
	case r.URL.Path == "/library/sections":
		writeContainer(w, &mediaContainer{Directory: f.sections})
	case len(parts) == 4 && parts[0] == "library" && parts[1] == "sections" && parts[3] == "all":
		var out []Metadata
		for i := range f.sectionData[parts[2]] {
			md := &f.sectionData[parts[2]][i]
			if q.Get("type") != "" && typeNumber(md.Type) != q.Get("type") {
				continue
			}
			if guid := q.Get("guid"); guid != "" && f.filtersGUID &&
				(!hasGUID(md, guid) || f.primaryGUIDOnly && !strings.HasPrefix(guid, "plex://")) {
				continue
			}
			out = append(out, *md)
		}
		mc := page(out, q)
		writeContainer(w, &mc)
	case len(parts) == 4 && parts[0] == "library" && parts[1] == "metadata" && parts[3] == "allLeaves":
		mc := page(f.leaves[parts[2]], q)
		writeContainer(w, &mc)
	case len(parts) == 3 && parts[0] == "library" && parts[1] == "metadata":
		md, ok := f.metadata[parts[2]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeContainer(w, &mediaContainer{Metadata: []Metadata{md}})
	case r.URL.Path == "/playQueues" && r.Method == http.MethodPost:
		items := f.queue
		if len(items) == 0 {
			key := q.Get("uri")[strings.LastIndex(q.Get("uri"), "/")+1:]
			items = []Metadata{f.metadata[key]}
		}
		for i := range items {
			items[i].PlayQueueItemID = FlexInt(100 + i)
		}
		writeContainer(w, &mediaContainer{
			PlayQueueID: 42, PlayQueueSelectedItemID: 100, Metadata: items,
		})
	case r.URL.Path == "/clients":
		writeContainer(w, &mediaContainer{Server: f.clients})
	default:
		http.NotFound(w, r)
	}
}

func typeNumber(typ string) string {
	for _, k := range []*mediaKind{kindMovie, kindEpisode, kindShow, kindTrack, kindAlbum, kindArtist} {
		if k.typeName == typ {
			return strconv.Itoa(k.plexType)
		}
	}
	return ""
}

// useTokens stores the server token, and the account token when set, in the
// global auth config for the test.
func useTokens(t *testing.T, serverURL, clientsURL, accountToken string) {
	t.Helper()
	entries := map[string]config.CredentialEntry{
		serverURL: {Bearer: testServerToken},
	}
	if clientsURL != "" && accountToken != "" {
		entries[clientsURL] = config.CredentialEntry{Bearer: accountToken}
	}
	config.SetAuthCfgForTesting(entries)
	t.Cleanup(config.ClearAuthCfgForTesting)
}

func (f *fakeServer) client(playerName string) *Client {
	return NewClient(f.URL, "http://clients.invalid", testClientID, playerName, f.Client())
}

func movie(key, title string, year int, guids ...string) Metadata {
	md := Metadata{RatingKey: key, Type: "movie", Title: title, Year: FlexInt(year)}
	for _, g := range guids {
		md.GUIDs = append(md.GUIDs, GUID{ID: g})
	}
	return md
}

func episode(key, show string, season, number int, title string) Metadata {
	return Metadata{
		RatingKey: key, Type: "episode", Title: title, GrandparentTitle: show,
		ParentIndex: FlexInt(season), Index: FlexInt(number),
	}
}

func track(key, artist, album, albumKey string, disc, number int, title string) Metadata {
	return Metadata{
		RatingKey: key, Type: "track", Title: title, GrandparentTitle: artist, ParentTitle: album,
		ParentRatingKey: albumKey, ParentIndex: FlexInt(disc), Index: FlexInt(number),
	}
}
