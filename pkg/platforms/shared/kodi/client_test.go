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

func TestClient_LaunchFile_MakesCorrectAPICall(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of LaunchFile to make real API requests
	// It should use Player.Open API method with the file path

	var receivedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request format
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Decode the payload
		err := json.NewDecoder(r.Body).Decode(&receivedPayload)
		if err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Return success response
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      receivedPayload["id"],
			"result":  "OK",
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(response)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	// Create client and set URL to test server
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Execute
	testPath := "/storage/videos/test.mp4"
	err := client.LaunchFile(testPath)

	// Verify
	require.NoError(t, err)

	// Verify the API call details
	assert.Equal(t, "2.0", receivedPayload["jsonrpc"])
	assert.Equal(t, "Player.Open", receivedPayload["method"])
	assert.NotNil(t, receivedPayload["id"])

	// Verify parameters structure
	params, ok := receivedPayload["params"].(map[string]any)
	require.True(t, ok, "params should be an object")

	item, ok := params["item"].(map[string]any)
	require.True(t, ok, "params.item should be an object")

	assert.Equal(t, testPath, item["file"])

	options, ok := params["options"].(map[string]any)
	require.True(t, ok, "params.options should be an object")

	assert.Equal(t, true, options["resume"])
}

// Test helper to create a mock Kodi server for testing API requests
func createMockKodiServer(
	_ *testing.T,
	handler func(payload map[string]any) map[string]any,
) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var receivedPayload map[string]any
		err := json.NewDecoder(r.Body).Decode(&receivedPayload)
		if err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		response := handler(receivedPayload)

		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(response)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}))
}

func TestClient_LaunchMovie_ParsesURLAndMakesAPICall(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of LaunchMovie to parse kodi-movie:// URLs
	// and make the correct API call with movieid parameter

	var receivedPayload map[string]any
	server := createMockKodiServer(t, func(payload map[string]any) map[string]any {
		receivedPayload = payload
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      payload["id"],
			"result":  "OK",
		}
	})
	defer server.Close()

	// Create client and set URL to test server
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Execute with kodi-movie:// URL
	moviePath := "kodi-movie://123/The Matrix"
	err := client.LaunchMovie(moviePath)

	// Verify
	require.NoError(t, err)

	// Verify the API call details
	assert.Equal(t, "2.0", receivedPayload["jsonrpc"])
	assert.Equal(t, "Player.Open", receivedPayload["method"])

	// Verify parameters structure for movie launch
	params, ok := receivedPayload["params"].(map[string]any)
	require.True(t, ok, "params should be an object")

	item, ok := params["item"].(map[string]any)
	require.True(t, ok, "params.item should be an object")

	// Should use movieid instead of file
	movieID, ok := item["movieid"].(float64) // JSON numbers decode as float64
	require.True(t, ok, "movieid should be present")
	assert.Equal(t, 123, int(movieID))

	// File should not be present
	_, hasFile := item["file"]
	assert.False(t, hasFile, "file should not be present for movie launch")

	options, ok := params["options"].(map[string]any)
	require.True(t, ok, "params.options should be an object")

	assert.Equal(t, true, options["resume"])
}

func TestClient_LaunchTVEpisode_ParsesURLAndMakesAPICall(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of LaunchTVEpisode to parse kodi-episode:// URLs
	// and make the correct API call with episodeid parameter

	var receivedPayload map[string]any
	server := createMockKodiServer(t, func(payload map[string]any) map[string]any {
		receivedPayload = payload
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      payload["id"],
			"result":  "OK",
		}
	})
	defer server.Close()

	// Create client and set URL to test server
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Execute with kodi-episode:// URL
	episodePath := "kodi-episode://456/Breaking Bad - Pilot"
	err := client.LaunchTVEpisode(episodePath)

	// Verify
	require.NoError(t, err)

	// Verify the API call details
	assert.Equal(t, "2.0", receivedPayload["jsonrpc"])
	assert.Equal(t, "Player.Open", receivedPayload["method"])

	// Verify parameters structure for episode launch
	params, ok := receivedPayload["params"].(map[string]any)
	require.True(t, ok, "params should be an object")

	item, ok := params["item"].(map[string]any)
	require.True(t, ok, "params.item should be an object")

	// Should use episodeid instead of file
	episodeID, ok := item["episodeid"].(float64) // JSON numbers decode as float64
	require.True(t, ok, "episodeid should be present")
	assert.Equal(t, 456, int(episodeID))

	// File and movieid should not be present
	_, hasFile := item["file"]
	assert.False(t, hasFile, "file should not be present for episode launch")
	_, hasMovieID := item["movieid"]
	assert.False(t, hasMovieID, "movieid should not be present for episode launch")

	options, ok := params["options"].(map[string]any)
	require.True(t, ok, "params.options should be an object")

	assert.Equal(t, true, options["resume"])
}

func TestClient_Stop_NoActivePlayers(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of Stop method when no players are active
	// It should call GetActivePlayers and not make any stop calls when empty array returned

	var receivedPayloads []map[string]any
	server := createMockKodiServer(t, func(payload map[string]any) map[string]any {
		receivedPayloads = append(receivedPayloads, payload)
		method, ok := payload["method"].(string)
		if !ok {
			t.Fatalf("expected method to be string, got %T", payload["method"])
		}

		switch method {
		case "Player.GetActivePlayers":
			// Return empty players array
			return map[string]any{
				"jsonrpc": "2.0",
				"id":      payload["id"],
				"result":  []any{}, // No active players
			}
		default:
			t.Errorf("Unexpected API method called: %s", method)
			return map[string]any{}
		}
	})
	defer server.Close()

	// Create client and set URL to test server
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Execute
	err := client.Stop()

	// Verify
	require.NoError(t, err)

	// Should only call GetActivePlayers, no Player.Stop calls
	require.Len(t, receivedPayloads, 1)
	assert.Equal(t, "Player.GetActivePlayers", receivedPayloads[0]["method"])
}

func TestClient_Stop_SingleActivePlayer(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of Stop method with one active player
	// It should call GetActivePlayers then Player.Stop with the correct player ID

	var receivedPayloads []map[string]any
	server := createMockKodiServer(t, func(payload map[string]any) map[string]any {
		receivedPayloads = append(receivedPayloads, payload)
		method, ok := payload["method"].(string)
		if !ok {
			t.Fatalf("expected method to be string, got %T", payload["method"])
		}

		switch method {
		case "Player.GetActivePlayers":
			// Return one active player
			return map[string]any{
				"jsonrpc": "2.0",
				"id":      payload["id"],
				"result": []any{
					map[string]any{
						"playerid": 1,
						"type":     "video",
					},
				},
			}
		case "Player.Stop":
			// Verify stop call parameters
			params, ok := payload["params"].(map[string]any)
			require.True(t, ok, "params should be an object")
			playerID, ok := params["playerid"].(float64)
			require.True(t, ok, "playerid should be present and numeric")
			assert.Equal(t, 1, int(playerID))

			return map[string]any{
				"jsonrpc": "2.0",
				"id":      payload["id"],
				"result":  "OK",
			}
		default:
			t.Errorf("Unexpected API method called: %s", method)
			return map[string]any{}
		}
	})
	defer server.Close()

	// Create client and set URL to test server
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Execute
	err := client.Stop()

	// Verify
	require.NoError(t, err)

	// Should call both GetActivePlayers and Player.Stop
	require.Len(t, receivedPayloads, 2)
	assert.Equal(t, "Player.GetActivePlayers", receivedPayloads[0]["method"])
	assert.Equal(t, "Player.Stop", receivedPayloads[1]["method"])
}

func TestClient_Stop_MultipleActivePlayers(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of Stop method with multiple active players
	// It should call GetActivePlayers then Player.Stop for each player with correct IDs

	var receivedPayloads []map[string]any
	server := createMockKodiServer(t, func(payload map[string]any) map[string]any {
		receivedPayloads = append(receivedPayloads, payload)
		method, ok := payload["method"].(string)
		if !ok {
			t.Fatalf("expected method to be string, got %T", payload["method"])
		}

		switch method {
		case "Player.GetActivePlayers":
			// Return multiple active players
			return map[string]any{
				"jsonrpc": "2.0",
				"id":      payload["id"],
				"result": []any{
					map[string]any{
						"playerid": 1,
						"type":     "video",
					},
					map[string]any{
						"playerid": 2,
						"type":     "audio",
					},
				},
			}
		case "Player.Stop":
			// Return success for stop call
			return map[string]any{
				"jsonrpc": "2.0",
				"id":      payload["id"],
				"result":  "OK",
			}
		default:
			t.Errorf("Unexpected API method called: %s", method)
			return map[string]any{}
		}
	})
	defer server.Close()

	// Create client and set URL to test server
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Execute
	err := client.Stop()

	// Verify
	require.NoError(t, err)

	// Should call GetActivePlayers then Player.Stop for each player
	require.Len(t, receivedPayloads, 3)
	assert.Equal(t, "Player.GetActivePlayers", receivedPayloads[0]["method"])
	assert.Equal(t, "Player.Stop", receivedPayloads[1]["method"])
	assert.Equal(t, "Player.Stop", receivedPayloads[2]["method"])

	// Verify player IDs in stop calls
	params1, ok := receivedPayloads[1]["params"].(map[string]any)
	require.True(t, ok)
	playerID1, ok := params1["playerid"].(float64)
	require.True(t, ok)

	params2, ok := receivedPayloads[2]["params"].(map[string]any)
	require.True(t, ok)
	playerID2, ok := params2["playerid"].(float64)
	require.True(t, ok)

	// Player IDs should be 1 and 2 (order may vary)
	playerIDs := []float64{playerID1, playerID2}
	assert.Contains(t, playerIDs, float64(1))
	assert.Contains(t, playerIDs, float64(2))
}

func TestClient_GetActivePlayers_MakesCorrectAPICall(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of GetActivePlayers to make real API requests
	// It should use Player.GetActivePlayers API method and parse the response

	var receivedPayload map[string]any
	server := createMockKodiServer(t, func(payload map[string]any) map[string]any {
		receivedPayload = payload
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      payload["id"],
			"result": []any{
				map[string]any{
					"playerid": 1,
					"type":     "video",
				},
				map[string]any{
					"playerid": 2,
					"type":     "audio",
				},
			},
		}
	})
	defer server.Close()

	// Create client and set URL to test server
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Execute
	players, err := client.GetActivePlayers()

	// Verify
	require.NoError(t, err)
	assert.Len(t, players, 2)

	// Verify first player
	assert.Equal(t, 1, players[0].ID)
	assert.Equal(t, "video", players[0].Type)

	// Verify second player
	assert.Equal(t, 2, players[1].ID)
	assert.Equal(t, "audio", players[1].Type)

	// Verify the API call details
	assert.Equal(t, "2.0", receivedPayload["jsonrpc"])
	assert.Equal(t, "Player.GetActivePlayers", receivedPayload["method"])
	assert.NotNil(t, receivedPayload["id"])
}

func TestClient_GetMovies_MakesCorrectAPICall(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of GetMovies to make real API requests
	// It should use VideoLibrary.GetMovies API method and parse the response to []Movie

	var receivedPayload map[string]any
	server := createMockKodiServer(t, func(payload map[string]any) map[string]any {
		receivedPayload = payload
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      payload["id"],
			"result": map[string]any{
				"movies": []any{
					map[string]any{
						"movieid": 1,
						"label":   "The Matrix",
						"file":    "/storage/movies/The Matrix (1999).mkv",
					},
					map[string]any{
						"movieid": 2,
						"label":   "Inception",
						"file":    "/storage/movies/Inception (2010).mkv",
					},
				},
			},
		}
	})
	defer server.Close()

	// Create client and set URL to test server
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Execute
	movies, err := client.GetMovies()

	// Verify
	require.NoError(t, err)
	assert.Len(t, movies, 2)

	// Verify first movie
	assert.Equal(t, 1, movies[0].ID)
	assert.Equal(t, "The Matrix", movies[0].Label)
	assert.Equal(t, "/storage/movies/The Matrix (1999).mkv", movies[0].File)

	// Verify second movie
	assert.Equal(t, 2, movies[1].ID)
	assert.Equal(t, "Inception", movies[1].Label)
	assert.Equal(t, "/storage/movies/Inception (2010).mkv", movies[1].File)

	// Verify the API call details
	assert.Equal(t, "2.0", receivedPayload["jsonrpc"])
	assert.Equal(t, "VideoLibrary.GetMovies", receivedPayload["method"])
	assert.NotNil(t, receivedPayload["id"])
}

func TestClient_GetTVShows_MakesCorrectAPICall(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of GetTVShows to make real API requests
	// It should use VideoLibrary.GetTVShows API method and parse the response to []TVShow

	var receivedPayload map[string]any
	server := createMockKodiServer(t, func(payload map[string]any) map[string]any {
		receivedPayload = payload
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      payload["id"],
			"result": map[string]any{
				"tvshows": []any{
					map[string]any{
						"tvshowid": 1,
						"label":    "Breaking Bad",
					},
					map[string]any{
						"tvshowid": 2,
						"label":    "The Wire",
					},
				},
			},
		}
	})
	defer server.Close()

	// Create client and set URL to test server
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Execute
	tvShows, err := client.GetTVShows()

	// Verify
	require.NoError(t, err)
	assert.Len(t, tvShows, 2)

	// Verify first TV show
	assert.Equal(t, 1, tvShows[0].ID)
	assert.Equal(t, "Breaking Bad", tvShows[0].Label)

	// Verify second TV show
	assert.Equal(t, 2, tvShows[1].ID)
	assert.Equal(t, "The Wire", tvShows[1].Label)

	// Verify the API call details
	assert.Equal(t, "2.0", receivedPayload["jsonrpc"])
	assert.Equal(t, "VideoLibrary.GetTVShows", receivedPayload["method"])
	assert.NotNil(t, receivedPayload["id"])
}

func TestClient_GetEpisodes_MakesCorrectAPICall(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of GetEpisodes to make real API requests
	// It should use VideoLibrary.GetEpisodes API method with tvshowid parameter and parse the response to []Episode

	var receivedPayload map[string]any
	server := createMockKodiServer(t, func(payload map[string]any) map[string]any {
		receivedPayload = payload
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      payload["id"],
			"result": map[string]any{
				"episodes": []any{
					map[string]any{
						"episodeid": 1,
						"tvshowid":  1,
						"label":     "Pilot",
						"season":    1,
						"episode":   1,
						"file":      "/storage/tv/Breaking Bad/Season 1/S01E01 - Pilot.mkv",
					},
					map[string]any{
						"episodeid": 2,
						"tvshowid":  1,
						"label":     "Cat's in the Bag...",
						"season":    1,
						"episode":   2,
						"file":      "/storage/tv/Breaking Bad/Season 1/S01E02 - Cat's in the Bag.mkv",
					},
				},
			},
		}
	})
	defer server.Close()

	// Create client and set URL to test server
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Execute with tvShowID
	tvShowID := 1
	episodes, err := client.GetEpisodes(tvShowID)

	// Verify
	require.NoError(t, err)
	assert.Len(t, episodes, 2)

	// Verify first episode
	assert.Equal(t, 1, episodes[0].ID)
	assert.Equal(t, 1, episodes[0].TVShowID)
	assert.Equal(t, "Pilot", episodes[0].Label)
	assert.Equal(t, 1, episodes[0].Season)
	assert.Equal(t, 1, episodes[0].Episode)
	assert.Equal(t, "/storage/tv/Breaking Bad/Season 1/S01E01 - Pilot.mkv", episodes[0].File)

	// Verify second episode
	assert.Equal(t, 2, episodes[1].ID)
	assert.Equal(t, 1, episodes[1].TVShowID)
	assert.Equal(t, "Cat's in the Bag...", episodes[1].Label)
	assert.Equal(t, 1, episodes[1].Season)
	assert.Equal(t, 2, episodes[1].Episode)
	assert.Equal(t, "/storage/tv/Breaking Bad/Season 1/S01E02 - Cat's in the Bag.mkv", episodes[1].File)

	// Verify the API call details
	assert.Equal(t, "2.0", receivedPayload["jsonrpc"])
	assert.Equal(t, "VideoLibrary.GetEpisodes", receivedPayload["method"])
	assert.NotNil(t, receivedPayload["id"])

	// Verify the tvshowid parameter was passed
	params, ok := receivedPayload["params"].(map[string]any)
	require.True(t, ok, "params should be an object")
	tvshowid, ok := params["tvshowid"].(float64)
	require.True(t, ok, "tvshowid should be present")
	assert.Equal(t, 1, int(tvshowid))
}
