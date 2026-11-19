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

package romm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
)

// fakeServer is a RomM server holding a fixed set of platforms and ROMs.
type fakeServer struct {
	*httptest.Server
	content   map[string][]byte
	token     string
	platforms []Platform
	roms      []Rom
	romCalls  atomic.Int32
	platCalls atomic.Int32
	failAll   atomic.Bool
}

func newFakeServer(t *testing.T, platforms []Platform, roms []Rom) *fakeServer {
	t.Helper()
	f := &fakeServer{platforms: platforms, roms: roms, content: map[string][]byte{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

// withToken makes the server demand a bearer token, and stores it for the
// server's URL the way linking does.
func (f *fakeServer) withToken(t *testing.T, token string) {
	t.Helper()
	f.token = token
	t.Cleanup(config.AddAuthEntryForTesting(f.URL, config.CredentialEntry{Bearer: token}))
}

func (f *fakeServer) config() *config.Instance {
	cfg := &config.Instance{}
	cfg.SetLauncherServerURL(LauncherID, f.URL)
	return cfg
}

func (f *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	if f.failAll.Load() {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	if f.token != "" && r.Header.Get("Authorization") != "Bearer "+f.token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	p := r.URL.Path
	switch {
	case p == "/api/platforms":
		f.platCalls.Add(1)
		writeJSON(w, f.platforms)
	case p == "/api/roms":
		f.romCalls.Add(1)
		f.serveList(w, r)
	case strings.HasPrefix(p, "/api/roms/"):
		rest := strings.TrimPrefix(p, "/api/roms/")
		idText, file, hasFile := strings.Cut(rest, "/content/")
		id, err := strconv.Atoi(idText)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if hasFile {
			body, ok := f.content[idText+"/"+file]
			if !ok || r.URL.Query().Get("purpose") != "play" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(body)
			return
		}
		for i := range f.roms {
			if f.roms[i].ID == id {
				writeJSON(w, f.roms[i])
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	case strings.HasPrefix(p, "/assets/"):
		body, ok := f.content[p]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(body)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeServer) serveList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("with_files") != "true" {
		http.Error(w, "files not requested", http.StatusBadRequest)
		return
	}
	platformID, _ := strconv.Atoi(q.Get("platform_ids"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	var all []Rom
	for i := range f.roms {
		if f.roms[i].PlatformID == platformID {
			all = append(all, f.roms[i])
		}
	}
	page := romPage{Total: len(all), Limit: limit, Offset: offset}
	if offset < len(all) {
		end := min(offset+limit, len(all))
		page.Items = all[offset:end]
	}
	writeJSON(w, page)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func singleFileRom(id, platformID int, slug, file string) Rom {
	return Rom{
		ID:           id,
		PlatformID:   platformID,
		PlatformSlug: slug,
		Name:         strings.TrimSuffix(file, file[strings.LastIndex(file, "."):]),
		FSName:       file,
		Files:        []RomFile{{ID: id * 10, FileName: file, FileSizeBytes: 3}},
		FSSizeBytes:  3,
	}
}
