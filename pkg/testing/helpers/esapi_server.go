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
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
)

// MockESAPIServer provides a test double for Batocera's EmulationStation API.
// It listens on localhost:1234 (the hardcoded ES API port) to intercept API calls during tests.
type MockESAPIServer struct {
	*http.Server
	listener    net.Listener
	runningGame *esapi.RunningGameResponse
	isRunning   bool
	mu          sync.Mutex
}

// NewMockESAPIServer creates a mock EmulationStation API server on localhost:1234.
// This server intercepts esapi.APIRunningGame() calls during tests.
func NewMockESAPIServer(t *testing.T) *MockESAPIServer {
	t.Helper()

	mock := &MockESAPIServer{
		isRunning: false,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/runningGame", mock.handleRunningGame)
	mux.HandleFunc("/emukill", mock.handleEmuKill)
	mux.HandleFunc("/launch", mock.handleLaunch)
	mux.HandleFunc("/notify", mock.handleNotify)

	// Listen on the hardcoded ES API port
	lc := net.ListenConfig{}
	listener, err := lc.Listen(context.Background(), "tcp", "localhost:1234")
	if err != nil {
		t.Fatalf("Failed to create mock ES API listener: %v", err)
	}

	mock.listener = listener
	mock.Server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Start server in background
	go func() {
		_ = mock.Serve(listener)
	}()

	// Register cleanup
	t.Cleanup(func() {
		_ = mock.Close()
		_ = listener.Close()
	})

	return mock
}

// WithNoRunningGame configures the mock to return "no game running"
func (m *MockESAPIServer) WithNoRunningGame() *MockESAPIServer {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isRunning = false
	m.runningGame = nil
	return m
}

// WithRunningGame configures the mock to return a running game
func (m *MockESAPIServer) WithRunningGame(game *esapi.RunningGameResponse) *MockESAPIServer {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isRunning = true
	m.runningGame = game
	return m
}

func (m *MockESAPIServer) handleRunningGame(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning || m.runningGame == nil {
		_, _ = w.Write([]byte(`{"msg":"NO GAME RUNNING"}`))
		return
	}

	jsonData, err := json.Marshal(m.runningGame)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(jsonData)
}

func (m *MockESAPIServer) handleEmuKill(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Simulate killing the emulator
	m.isRunning = false
	m.runningGame = nil

	w.WriteHeader(http.StatusOK)
}

func (*MockESAPIServer) handleLaunch(w http.ResponseWriter, _ *http.Request) {
	// Simulate launching a game - just return OK
	w.WriteHeader(http.StatusOK)
}

func (*MockESAPIServer) handleNotify(w http.ResponseWriter, _ *http.Request) {
	// Simulate notification - just return OK
	w.WriteHeader(http.StatusOK)
}
