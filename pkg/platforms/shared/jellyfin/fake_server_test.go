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

package jellyfin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

const (
	testToken  = "test-token"
	testUserID = "a1b2c3d4e5f60718293a4b5c6d7e8f90"
)

// fakeItem is an item on the fake server plus the relations Jellyfin derives
// from the library layout.
type fakeItem struct {
	seriesID string
	artistID string
	Item
	// missing is a placeholder for an episode with no file, which the
	// server lists unless asked not to.
	missing bool
}

type playCall struct {
	session string
	command string
	ids     []string
}

type recordedRequest struct {
	query  url.Values
	method string
	path   string
	auth   string
}

// fakeJellyfin is an httptest Jellyfin server holding a small library.
type fakeJellyfin struct {
	t          *testing.T
	srv        *httptest.Server
	items      []fakeItem
	artists    []Item
	sessions   []Session
	plays      []playCall
	playstates []string
	requests   []recordedRequest
	getItemErr int
	mu         syncutil.Mutex
	// failPlayLast makes the server refuse to add items to a queue.
	failPlayLast bool
	// ignoresProviderFilter makes the server return every item regardless
	// of AnyProviderIdEquals, as some server versions do.
	ignoresProviderFilter bool
}

func newFakeJellyfin(t *testing.T) *fakeJellyfin {
	t.Helper()
	f := &fakeJellyfin{t: t}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeJellyfin) URL() string { return f.srv.URL }

func (f *fakeJellyfin) setSessions(s ...Session) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = s
}

func (f *fakeJellyfin) addItems(items ...fakeItem) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = append(f.items, items...)
}

func (f *fakeJellyfin) recorded() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

func (f *fakeJellyfin) playCalls() []playCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.plays)
}

func (f *fakeJellyfin) playstateCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.playstates)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeJellyfin) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	f.requests = append(f.requests, recordedRequest{
		method: r.Method, path: r.URL.Path, query: q, auth: r.Header.Get("Authorization"),
	})
	if !strings.Contains(r.Header.Get("Authorization"), `Token="`+testToken+`"`) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	userScoped := func() bool {
		if q.Get("userId") != testUserID {
			w.WriteHeader(http.StatusBadRequest)
			return false
		}
		return true
	}
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/Users/Me":
		writeJSON(w, map[string]string{"Id": testUserID, "Name": "tester"})
	case r.Method == http.MethodGet && path == "/Sessions":
		if q.Get("controllableByUserId") != testUserID {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		writeJSON(w, f.sessions)
	case r.Method == http.MethodGet && path == "/Items":
		if userScoped() {
			writeJSON(w, page(f.queryItems(q), q))
		}
	case r.Method == http.MethodGet && path == "/Artists/AlbumArtists":
		if userScoped() {
			writeJSON(w, page(filterSearch(f.artists, q.Get("searchTerm")), q))
		}
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/Items/"):
		if !userScoped() {
			return
		}
		if f.getItemErr != 0 {
			w.WriteHeader(f.getItemErr)
			return
		}
		id := strings.TrimPrefix(path, "/Items/")
		all := f.allItems()
		for i := range all {
			it := &all[i]
			if it.ID == id {
				writeJSON(w, it)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/Shows/") && strings.HasSuffix(path, "/Episodes"):
		if !userScoped() {
			return
		}
		showID := strings.TrimSuffix(strings.TrimPrefix(path, "/Shows/"), "/Episodes")
		var eps []Item
		for i := range f.items {
			it := &f.items[i]
			if it.Type == typeEpisode && it.seriesID == showID && (!it.missing || q.Get("isMissing") != "false") {
				eps = append(eps, it.Item)
			}
		}
		writeJSON(w, ItemsPage{Items: eps, TotalRecordCount: len(eps)})
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/Sessions/"):
		rest := strings.TrimPrefix(path, "/Sessions/")
		parts := strings.Split(rest, "/")
		switch {
		case len(parts) == 2 && parts[1] == "Playing" && f.failPlayLast && q.Get("playCommand") == "PlayLast":
			w.WriteHeader(http.StatusInternalServerError)
			return
		case len(parts) == 2 && parts[1] == "Playing":
			f.plays = append(f.plays, playCall{
				session: parts[0], command: q.Get("playCommand"), ids: strings.Split(q.Get("itemIds"), ","),
			})
		case len(parts) == 3 && parts[1] == "Playing":
			f.playstates = append(f.playstates, parts[0]+"/"+parts[2])
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeJellyfin) allItems() []Item {
	out := make([]Item, 0, len(f.items)+len(f.artists))
	for i := range f.items {
		it := &f.items[i]
		out = append(out, it.Item)
	}
	return append(out, f.artists...)
}

func (f *fakeJellyfin) queryItems(q url.Values) []Item {
	var out []Item
	types := strings.Split(q.Get("IncludeItemTypes"), ",")
	for i := range f.items {
		it := &f.items[i]
		if q.Get("IncludeItemTypes") != "" && !slices.Contains(types, it.Type) {
			continue
		}
		if p := q.Get("ParentId"); p != "" && it.AlbumID != p {
			continue
		}
		if a := q.Get("ArtistIds"); a != "" && it.artistID != a {
			continue
		}
		if ids := q.Get("AnyProviderIdEquals"); ids != "" && !f.ignoresProviderFilter &&
			!hasAnyProvider(it.ProviderIDs, ids) {
			continue
		}
		if it.missing && q.Get("IsMissing") == "false" {
			continue
		}
		out = append(out, it.Item)
	}
	return filterSearch(out, q.Get("searchTerm"))
}

func hasAnyProvider(have map[string]string, list string) bool {
	for _, entry := range strings.Split(list, ",") {
		name, value, ok := strings.Cut(entry, ".")
		if ok && have[name] == value {
			return true
		}
	}
	return false
}

func filterSearch(items []Item, term string) []Item {
	if term == "" {
		return items
	}
	var out []Item
	for i := range items {
		if strings.Contains(strings.ToLower(items[i].Name), strings.ToLower(term)) {
			out = append(out, items[i])
		}
	}
	return out
}

func page(items []Item, q url.Values) ItemsPage {
	start, _ := strconv.Atoi(q.Get("StartIndex"))
	limit, err := strconv.Atoi(q.Get("Limit"))
	if err != nil || limit <= 0 {
		limit = len(items)
	}
	total := len(items)
	start = min(start, total)
	end := min(start+limit, total)
	return ItemsPage{Items: items[start:end], TotalRecordCount: total, StartIndex: start}
}

// testConfig returns a config naming the fake server and player for every
// Jellyfin launcher, with a token stored for the server.
func testConfig(t *testing.T, serverURL, player string) *config.Instance {
	t.Helper()
	defaults := config.BaseDefaults
	defaults.Launchers.Default = []config.LaunchersDefault{{
		Launcher: shared.LauncherGroupJellyfin, ServerURL: serverURL, Client: player,
	}}
	cfg, err := config.NewConfigWithFs(t.TempDir(), defaults, afero.NewMemMapFs())
	require.NoError(t, err)
	t.Cleanup(config.AddAuthEntryForTesting(serverURL, config.CredentialEntry{Bearer: testToken}))
	return cfg
}

func testClient(t *testing.T, f *fakeJellyfin, player string) *HTTPClient {
	t.Helper()
	c, err := NewClient(f.URL(), testToken, player, "zaparoo-test-device")
	require.NoError(t, err)
	return c
}

func intPtr(i int) *int { return &i }

// id builds a deterministic 32-character hex item ID.
func id(n int) string {
	return strings.Repeat("0", 32-len(strconv.Itoa(n))) + strconv.Itoa(n)
}
