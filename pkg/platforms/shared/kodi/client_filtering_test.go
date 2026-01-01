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

package kodi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLaunchAlbum_UsesDirectAlbumLaunch verifies that the implementation
// uses direct Player.Open with albumid instead of playlist-based approach
func TestLaunchAlbum_UsesDirectAlbumLaunch(t *testing.T) {
	t.Parallel()

	var receivedParams PlayerOpenParams

	// Create a mock Kodi server that tracks Player.Open requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload APIPayload
		err := json.NewDecoder(r.Body).Decode(&payload)
		if err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")

		switch payload.Method {
		case APIMethodPlayerOpen:
			// Extract params
			paramsJSON, _ := json.Marshal(payload.Params)
			_ = json.Unmarshal(paramsJSON, &receivedParams)

			_, _ = w.Write([]byte(`{"id":"1","jsonrpc":"2.0","result":"OK"}`))

		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":-1,"message":"Unknown method"}}`))
		}
	}))
	defer server.Close()

	client := &Client{url: server.URL}

	// Launch album ID 1
	err := client.LaunchAlbum("kodi-album://1/Test Album")
	require.NoError(t, err)

	// Verify Player.Open was called with albumid
	assert.Equal(t, 1, receivedParams.Item.AlbumID, "Item should contain albumid: 1")
}
