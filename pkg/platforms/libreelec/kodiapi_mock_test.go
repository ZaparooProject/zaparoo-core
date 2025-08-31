// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package libreelec

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type MockKodiServer struct {
	*httptest.Server
	episodes    map[int][]KodiItem
	shouldError map[string]error
	requests    []KodiAPIPayload
	movies      []KodiItem
	tvShows     []KodiItem
	players     []KodiPlayer
	mu          sync.Mutex
}

func NewMockKodiServer(_ *testing.T) *MockKodiServer {
	mock := &MockKodiServer{
		requests:    make([]KodiAPIPayload, 0),
		movies:      testMovies,
		tvShows:     testTVShows,
		episodes:    testEpisodes,
		players:     testNoActivePlayers,
		shouldError: make(map[string]error),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/jsonrpc", mock.handleJSONRPC)
	mock.Server = httptest.NewServer(mux)

	return mock
}

func (m *MockKodiServer) WithActivePlayers() *MockKodiServer {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.players = testActivePlayers
	return m
}

func (m *MockKodiServer) WithError(method string, err error) *MockKodiServer {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldError[method] = err
	return m
}

func (m *MockKodiServer) WithMovies(movies []KodiItem) *MockKodiServer {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.movies = movies
	return m
}

func (m *MockKodiServer) WithTVShows(shows []KodiItem) *MockKodiServer {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tvShows = shows
	return m
}

func (m *MockKodiServer) WithEpisodes(tvShowID int, episodes []KodiItem) *MockKodiServer {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.episodes == nil {
		m.episodes = make(map[int][]KodiItem)
	}
	m.episodes[tvShowID] = episodes
	return m
}

func (m *MockKodiServer) GetRequests() []KodiAPIPayload {
	m.mu.Lock()
	defer m.mu.Unlock()
	requests := make([]KodiAPIPayload, len(m.requests))
	copy(requests, m.requests)
	return requests
}

func (m *MockKodiServer) ClearRequests() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = m.requests[:0]
}

func (m *MockKodiServer) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload KodiAPIPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		m.sendErrorResponse(w, payload.ID, -32700, "Parse error")
		return
	}

	m.mu.Lock()
	m.requests = append(m.requests, payload)
	m.mu.Unlock()

	if err, exists := m.shouldError[string(payload.Method)]; exists {
		m.sendErrorResponse(w, payload.ID, -32603, err.Error())
		return
	}

	switch payload.Method {
	case KodiAPIMethodPlayerOpen:
		m.handlePlayerOpen(w, payload)
	case KodiAPIMethodPlayerStop:
		m.handlePlayerStop(w, payload)
	case KodiAPIMethodPlayerGetActivePlayers:
		m.handlePlayerGetActivePlayers(w, payload)
	case KodiAPIMethodVideoLibraryGetMovies:
		m.handleVideoLibraryGetMovies(w, payload)
	case KodiAPIMethodVideoLibraryGetTVShows:
		m.handleVideoLibraryGetTVShows(w, payload)
	case KodiAPIMethodVideoLibraryGetEpisodes:
		m.handleVideoLibraryGetEpisodes(w, payload)
	default:
		m.sendErrorResponse(w, payload.ID, -32601, "Method not found")
	}
}

func (m *MockKodiServer) handlePlayerOpen(w http.ResponseWriter, payload KodiAPIPayload) {
	response := KodiAPIResponse{
		ID:      payload.ID,
		JSONRPC: "2.0",
		Result:  json.RawMessage(`"OK"`),
	}
	m.sendResponse(w, response)
}

func (m *MockKodiServer) handlePlayerStop(w http.ResponseWriter, payload KodiAPIPayload) {
	response := KodiAPIResponse{
		ID:      payload.ID,
		JSONRPC: "2.0",
		Result:  json.RawMessage(`"OK"`),
	}
	m.sendResponse(w, response)
}

func (m *MockKodiServer) handlePlayerGetActivePlayers(w http.ResponseWriter, payload KodiAPIPayload) {
	m.mu.Lock()
	players := m.players
	m.mu.Unlock()

	result, _ := json.Marshal(players)
	response := KodiAPIResponse{
		ID:      payload.ID,
		JSONRPC: "2.0",
		Result:  result,
	}
	m.sendResponse(w, response)
}

func (m *MockKodiServer) handleVideoLibraryGetMovies(w http.ResponseWriter, payload KodiAPIPayload) {
	m.mu.Lock()
	movies := m.movies
	m.mu.Unlock()

	resp := KodiVideoLibraryGetMoviesResponse{Movies: movies}
	result, _ := json.Marshal(resp)
	response := KodiAPIResponse{
		ID:      payload.ID,
		JSONRPC: "2.0",
		Result:  result,
	}
	m.sendResponse(w, response)
}

func (m *MockKodiServer) handleVideoLibraryGetTVShows(w http.ResponseWriter, payload KodiAPIPayload) {
	m.mu.Lock()
	shows := m.tvShows
	m.mu.Unlock()

	resp := KodiVideoLibraryGetTVShowsResponse{TVShows: shows}
	result, _ := json.Marshal(resp)
	response := KodiAPIResponse{
		ID:      payload.ID,
		JSONRPC: "2.0",
		Result:  result,
	}
	m.sendResponse(w, response)
}

func (m *MockKodiServer) handleVideoLibraryGetEpisodes(w http.ResponseWriter, payload KodiAPIPayload) {
	var params KodiVideoLibraryGetEpisodesParams
	if payload.Params != nil {
		paramBytes, _ := json.Marshal(payload.Params)
		_ = json.Unmarshal(paramBytes, &params)
	}

	m.mu.Lock()
	episodes := m.episodes[params.TVShowID]
	m.mu.Unlock()

	if episodes == nil {
		episodes = []KodiItem{}
	}

	resp := KodiVideoLibraryGetEpisodesResponse{Episodes: episodes}
	result, _ := json.Marshal(resp)
	response := KodiAPIResponse{
		ID:      payload.ID,
		JSONRPC: "2.0",
		Result:  result,
	}
	m.sendResponse(w, response)
}

func (*MockKodiServer) sendResponse(w http.ResponseWriter, response KodiAPIResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (*MockKodiServer) sendErrorResponse(w http.ResponseWriter, id string, code int, message string) {
	response := KodiAPIResponse{
		ID:      id,
		JSONRPC: "2.0",
		Error: &KodiAPIError{
			Code:    code,
			Message: message,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (m *MockKodiServer) GetURLForConfig() string {
	return m.URL + "/jsonrpc"
}
