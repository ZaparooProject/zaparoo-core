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

package kodi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/shared/kodi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_APIRequest(t *testing.T) {
	t.Parallel()

	t.Run("successful API request", func(t *testing.T) {
		t.Parallel()
		// Create a mock Kodi server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify the request
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "application/json", r.Header.Get("Accept"))

			// Parse the request body
			var payload kodi.APIPayload
			err := json.NewDecoder(r.Body).Decode(&payload)
			assert.NoError(t, err)

			// Verify payload structure
			assert.Equal(t, "2.0", payload.JSONRPC)
			assert.NotEmpty(t, payload.ID)
			assert.Equal(t, kodi.APIMethodPlayerGetActivePlayers, payload.Method)

			// Return a mock response
			response := kodi.APIResponse{
				JSONRPC: "2.0",
				ID:      payload.ID,
				Result:  json.RawMessage(`[{"playerid": 1, "type": "video"}]`),
			}

			w.Header().Set("Content-Type", "application/json")
			err = json.NewEncoder(w).Encode(response)
			assert.NoError(t, err)
		}))
		defer server.Close()

		// Create client with the mock server URL
		client := kodi.NewClient(nil)
		client.SetURL(server.URL + "/jsonrpc")

		// This should fail until we implement APIRequest method
		result, err := client.APIRequest(kodi.APIMethodPlayerGetActivePlayers, nil)
		require.NoError(t, err)
		assert.NotNil(t, result)

		// Verify we can parse the result
		var players []kodi.Player
		err = json.Unmarshal(result, &players)
		require.NoError(t, err)
		assert.Len(t, players, 1)
		assert.Equal(t, 1, players[0].ID)
		assert.Equal(t, "video", players[0].Type)
	})

	t.Run("API error response", func(t *testing.T) {
		t.Parallel()
		// Create a mock server that returns an error
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload kodi.APIPayload
			err := json.NewDecoder(r.Body).Decode(&payload)
			assert.NoError(t, err)

			response := kodi.APIResponse{
				JSONRPC: "2.0",
				ID:      payload.ID,
				Error: &kodi.APIError{
					Code:    -1,
					Message: "Method not found",
				},
			}

			w.Header().Set("Content-Type", "application/json")
			err = json.NewEncoder(w).Encode(response)
			assert.NoError(t, err)
		}))
		defer server.Close()

		client := kodi.NewClient(nil)
		client.SetURL(server.URL + "/jsonrpc")

		// This should return an error
		_, err := client.APIRequest(kodi.APIMethodPlayerGetActivePlayers, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Method not found")
	})

	t.Run("HTTP error", func(t *testing.T) {
		t.Parallel()
		client := kodi.NewClient(nil)
		client.SetURL("http://invalid-url-that-does-not-exist.local/jsonrpc")

		// This should return a connection error
		_, err := client.APIRequest(kodi.APIMethodPlayerGetActivePlayers, nil)
		require.Error(t, err)
	})
}
