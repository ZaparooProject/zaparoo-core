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

package helpers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fixtures"
)

// MockKodiServer provides a mock Kodi JSON-RPC server for integration testing
type MockKodiServer struct {
	*httptest.Server
	players []kodi.Player
	mu      syncutil.Mutex
}

// NewMockKodiServer creates a new mock Kodi server for testing
func NewMockKodiServer(_ *testing.T) *MockKodiServer {
	mock := &MockKodiServer{
		players: make([]kodi.Player, 0),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/jsonrpc", mock.handleJSONRPC)
	mock.Server = httptest.NewServer(mux)

	return mock
}

// URL returns the mock server's URL for configuration
func (m *MockKodiServer) URL() string {
	return m.Server.URL
}

// GetURLForConfig returns the mock server's URL formatted for Kodi client configuration
func (m *MockKodiServer) GetURLForConfig() string {
	return m.URL() + "/jsonrpc"
}

// WithActivePlayers configures the mock server to return active players
func (m *MockKodiServer) WithActivePlayers() *MockKodiServer {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.players = fixtures.TestActivePlayers
	return m
}

func (m *MockKodiServer) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request to get ID for response
	var payload kodi.APIPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		// For now, just use hardcoded ID
		payload.ID = "test-123"
	}

	// Return configured players data
	m.mu.Lock()
	players := m.players
	m.mu.Unlock()

	result, _ := json.Marshal(players)
	response := kodi.APIResponse{
		ID:      payload.ID,
		JSONRPC: "2.0",
		Result:  result,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
