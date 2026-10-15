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

package helpers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fixtures"
	"github.com/gorilla/websocket"
)

// MockKodiServer provides a mock Kodi JSON-RPC server for integration testing
type MockKodiServer struct {
	*httptest.Server
	players  []kodi.Player
	requests [][]byte
	failing  map[kodi.APIMethod]bool
	sockets  []*websocket.Conn
	accepted int
	mu       syncutil.Mutex
	wsMu     syncutil.Mutex
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
	if websocket.IsWebSocketUpgrade(r) {
		m.handleWebSocket(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request to get ID for response
	var payload kodi.APIPayload
	body, _ := io.ReadAll(r.Body)
	m.mu.Lock()
	m.requests = append(m.requests, body)
	m.mu.Unlock()
	if err := json.Unmarshal(body, &payload); err != nil {
		// For now, just use hardcoded ID
		payload.ID = "test-123"
	}

	// Return configured players data
	m.mu.Lock()
	players := m.players
	fail := m.failing[payload.Method]
	m.mu.Unlock()

	if fail {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kodi.APIResponse{
			ID:      payload.ID,
			JSONRPC: "2.0",
			Error:   &kodi.APIError{Code: -32100, Message: "mock failure"},
		})
		return
	}

	result, _ := json.Marshal(players)
	response := kodi.APIResponse{
		ID:      payload.ID,
		JSONRPC: "2.0",
		Result:  result,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// FailMethod makes the server answer the given JSON-RPC method with an error.
func (m *MockKodiServer) FailMethod(method kodi.APIMethod) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failing == nil {
		m.failing = make(map[kodi.APIMethod]bool)
	}
	m.failing[method] = true
}

// Requests returns the raw JSON-RPC request bodies received over HTTP, in order.
func (m *MockKodiServer) Requests() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(m.requests))
	copy(out, m.requests)
	return out
}

func (m *MockKodiServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	m.wsMu.Lock()
	m.sockets = append(m.sockets, conn)
	m.accepted++
	m.wsMu.Unlock()
	// Drain client frames so close frames are processed.
	go func() {
		defer m.dropSocket(conn)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
}

func (m *MockKodiServer) dropSocket(conn *websocket.Conn) {
	_ = conn.Close()
	m.wsMu.Lock()
	defer m.wsMu.Unlock()
	for i, c := range m.sockets {
		if c == conn {
			m.sockets = append(m.sockets[:i], m.sockets[i+1:]...)
			return
		}
	}
}

// WebSocketClients returns the number of notification sockets accepted so far
// that are still open.
func (m *MockKodiServer) WebSocketClients() int {
	m.wsMu.Lock()
	defer m.wsMu.Unlock()
	return len(m.sockets)
}

// WebSocketsAccepted returns the number of notification sockets accepted so
// far, open or closed. A test that replaces one watcher with another waits
// for this to count the new socket, because the open count can read 1 while
// only the old socket is registered.
func (m *MockKodiServer) WebSocketsAccepted() int {
	m.wsMu.Lock()
	defer m.wsMu.Unlock()
	return m.accepted
}

// PushNotification sends a JSON-RPC notification to every connected socket.
func (m *MockKodiServer) PushNotification(method string, data any) error {
	msg, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  map[string]any{"data": data, "sender": "xbmc"},
	})
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	m.wsMu.Lock()
	defer m.wsMu.Unlock()
	for _, c := range m.sockets {
		if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
			return fmt.Errorf("write notification: %w", err)
		}
	}
	return nil
}

// CloseWebSockets closes every notification socket from the server side.
func (m *MockKodiServer) CloseWebSockets() {
	m.wsMu.Lock()
	sockets := append([]*websocket.Conn(nil), m.sockets...)
	m.wsMu.Unlock()
	for _, c := range sockets {
		_ = c.Close()
	}
}
