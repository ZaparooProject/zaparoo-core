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

package kodi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLaunchAlbum_UsesAPIFiltering verifies that the implementation
// uses API-level filtering instead of fetching all songs
func TestLaunchAlbum_UsesAPIFiltering(t *testing.T) {
	t.Parallel()

	var getSongsRequest *APIPayload
	songCallCount := 0

	// Create a mock Kodi server that tracks GetSongs requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request APIPayload
		err := json.NewDecoder(r.Body).Decode(&request)
		if err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")

		switch request.Method {
		case APIMethodPlaylistClear:
			_, _ = w.Write([]byte(`{"id":"1","jsonrpc":"2.0","result":"OK"}`))

		case APIMethodAudioLibraryGetSongs:
			songCallCount++
			getSongsRequest = &request

			// Return a large dataset to highlight the inefficiency
			songs := make([]Song, 1000) // Simulating a large music library
			for i := 0; i < 1000; i++ {
				albumID := (i % 10) + 1 // Distribute across 10 albums
				songs[i] = Song{
					ID:      i + 1,
					AlbumID: albumID,
					Label:   fmt.Sprintf("Song %d", i+1),
					Artist:  fmt.Sprintf("Artist %d", albumID),
				}
			}

			response := AudioLibraryGetSongsResponse{Songs: songs}
			responseJSON, _ := json.Marshal(map[string]any{
				"id":      request.ID,
				"jsonrpc": "2.0",
				"result":  response,
			})
			_, _ = w.Write(responseJSON)

		case APIMethodPlaylistAdd:
			_, _ = w.Write([]byte(`{"id":"1","jsonrpc":"2.0","result":"OK"}`))

		case APIMethodPlayerOpen:
			_, _ = w.Write([]byte(`{"id":"1","jsonrpc":"2.0","result":"OK"}`))

		case APIMethodPlayerGetActivePlayers, APIMethodPlayerStop, APIMethodVideoLibraryGetMovies,
			APIMethodVideoLibraryGetTVShows, APIMethodVideoLibraryGetEpisodes,
			APIMethodAudioLibraryGetAlbums, APIMethodAudioLibraryGetArtists:
			// These methods are not used in this test
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":-1,"message":"Method not implemented in test"}}`))

		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":-1,"message":"Unknown method"}}`))
		}
	}))
	defer server.Close()

	client := &Client{url: server.URL}

	// Launch album ID 1 - this should only play songs from album 1
	err := client.LaunchAlbum("kodi-album://1/Test Album")
	require.NoError(t, err)

	// Verify the inefficiency: GetSongs was called without any filtering
	assert.Equal(t, 1, songCallCount, "GetSongs should be called exactly once")
	require.NotNil(t, getSongsRequest, "GetSongs request should be captured")

	// Verify API-level filtering is used
	require.NotNil(t, getSongsRequest.Params, "GetSongs should be called with filter parameters")

	paramsMap, ok := getSongsRequest.Params.(map[string]any)
	require.True(t, ok, "Params should be a map")

	filterData, exists := paramsMap["filter"]
	require.True(t, exists, "Filter parameter should exist")
	require.NotNil(t, filterData, "Filter should not be nil")

	filterMap, ok := filterData.(map[string]any)
	require.True(t, ok, "Filter should be a map")

	assert.Equal(t, "albumid", filterMap["field"], "Filter should target albumid field")
	assert.Equal(t, "is", filterMap["operator"], "Filter should use 'is' operator")
	assert.Equal(t, 1, int(filterMap["value"].(float64)), "Filter should target album ID 1")
}
