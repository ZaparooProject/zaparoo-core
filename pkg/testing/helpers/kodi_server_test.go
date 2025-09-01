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

package helpers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMockKodiServer_CanBeCreated(t *testing.T) {
	t.Parallel()

	// This test drives the creation of a mock Kodi server for integration testing
	server := helpers.NewMockKodiServer(t)
	defer server.Close()

	// Server should have a URL for testing
	url := server.URL()
	assert.NotEmpty(t, url)
	assert.Contains(t, url, "http://")
}

func TestMockKodiServer_HandlesPlayerGetActivePlayersRequest(t *testing.T) {
	t.Parallel()

	// This test drives the JSON-RPC handling functionality needed for shared Kodi client testing
	server := helpers.NewMockKodiServer(t)
	defer server.Close()

	// We need the server to handle actual HTTP requests to /jsonrpc endpoint
	// This will drive the implementation of HTTP handling and JSON-RPC protocol support
	url := server.GetURLForConfig()
	assert.Contains(t, url, "/jsonrpc")
	assert.NotEmpty(t, url)

	// Test actual JSON-RPC request handling - this will drive the implementation
	payload := kodi.APIPayload{
		JSONRPC: "2.0",
		Method:  kodi.APIMethodPlayerGetActivePlayers,
		ID:      "test-123",
	}

	// Marshal payload to JSON for HTTP request
	jsonData, err := json.Marshal(payload)
	require.NoError(t, err)

	// Make actual HTTP POST request to the server
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	require.NoError(t, err)
	defer func() {
		err := resp.Body.Close()
		require.NoError(t, err)
	}()

	// Server should respond with valid JSON-RPC response - this will fail until implemented
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Test that the response contains proper JSON-RPC structure
	var apiResp kodi.APIResponse
	err = json.NewDecoder(resp.Body).Decode(&apiResp)
	require.NoError(t, err)
	assert.Equal(t, "test-123", apiResp.ID)
	assert.Equal(t, "2.0", apiResp.JSONRPC)
}

func TestMockKodiServer_WithActivePlayers(t *testing.T) {
	t.Parallel()

	// This test drives the need for configurable mock server responses
	server := helpers.NewMockKodiServer(t).WithActivePlayers()
	defer server.Close()

	// Test that configured active players are returned
	payload := kodi.APIPayload{
		JSONRPC: "2.0",
		Method:  kodi.APIMethodPlayerGetActivePlayers,
		ID:      "test-players",
	}

	jsonData, err := json.Marshal(payload)
	require.NoError(t, err)

	resp, err := http.Post(server.GetURLForConfig(), "application/json", bytes.NewBuffer(jsonData))
	require.NoError(t, err)
	defer func() {
		err := resp.Body.Close()
		require.NoError(t, err)
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var apiResp kodi.APIResponse
	err = json.NewDecoder(resp.Body).Decode(&apiResp)
	require.NoError(t, err)

	// Should contain actual player data, not empty array
	var players []kodi.Player
	err = json.Unmarshal(apiResp.Result, &players)
	require.NoError(t, err)
	assert.Len(t, players, 1)
	assert.Equal(t, "video", players[0].Type)
	assert.Equal(t, 1, players[0].ID)
}
