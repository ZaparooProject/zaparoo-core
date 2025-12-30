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
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
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
	players, err := client.GetActivePlayers(context.Background())

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
	movies, err := client.GetMovies(context.Background())

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
	tvShows, err := client.GetTVShows(context.Background())

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
	episodes, err := client.GetEpisodes(context.Background(), tvShowID)

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

func TestNewClient_HierarchicalConfigLookup(t *testing.T) {
	t.Parallel()

	// Test the hierarchical config lookup for Kodi client
	// Should use: specific launcher ID → "Kodi" generic → hardcoded localhost fallback

	tests := []struct {
		name           string
		launcherID     string
		configDefaults map[string]string
		expectedURL    string
	}{
		{
			name:       "uses specific launcher ID configuration",
			launcherID: "KodiSpecific",
			configDefaults: map[string]string{
				"KodiSpecific": "http://specific-kodi:8080/jsonrpc",
				"Kodi":         "http://generic-kodi:8080/jsonrpc",
			},
			expectedURL: "http://specific-kodi:8080/jsonrpc",
		},
		{
			name:       "falls back to Kodi generic when specific not found",
			launcherID: "KodiSpecific",
			configDefaults: map[string]string{
				"Kodi": "http://generic-kodi:8080/jsonrpc",
			},
			expectedURL: "http://generic-kodi:8080/jsonrpc",
		},
		{
			name:           "falls back to hardcoded localhost when no config found",
			launcherID:     "KodiSpecific",
			configDefaults: map[string]string{},
			expectedURL:    "http://localhost:8080/jsonrpc",
		},
		{
			name:       "handles trailing slashes in ServerURL",
			launcherID: "KodiSpecific",
			configDefaults: map[string]string{
				"KodiSpecific": "http://specific-kodi:8080/",
			},
			expectedURL: "http://specific-kodi:8080/jsonrpc",
		},
		{
			name:       "adds http scheme when missing in launcher config",
			launcherID: "KodiSpecific",
			configDefaults: map[string]string{
				"KodiSpecific": "specific-kodi:8080",
			},
			expectedURL: "http://specific-kodi:8080/jsonrpc",
		},
		{
			name:       "preserves https scheme in launcher config",
			launcherID: "KodiSpecific",
			configDefaults: map[string]string{
				"KodiSpecific": "https://specific-kodi:8080",
			},
			expectedURL: "https://specific-kodi:8080/jsonrpc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// This test should drive the implementation to accept a launcher ID
			// and perform hierarchical configuration lookup

			// For now, the test will fail because NewClient doesn't accept launcher ID
			// This drives us to modify the NewClient signature and implementation
			// Create config with the test defaults
			var cfg *config.Instance
			if len(tt.configDefaults) > 0 {
				configDir := t.TempDir()
				var launcherDefaults []config.LaunchersDefault

				for launcherID, serverURL := range tt.configDefaults {
					launcherDefaults = append(launcherDefaults, config.LaunchersDefault{
						Launcher:  launcherID,
						ServerURL: serverURL,
					})
				}

				values := config.Values{
					Launchers: config.Launchers{
						Default: launcherDefaults,
					},
				}

				var err error
				cfg, err = config.NewConfig(configDir, values)
				require.NoError(t, err)
			}

			client := kodi.NewClientWithLauncherID(cfg, tt.launcherID)

			actualURL := client.GetURL()
			assert.Equal(t, tt.expectedURL, actualURL)
		})
	}
}

func TestNewClient_UsesConfigurationSystem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      *config.Instance
		expectedURL string
	}{
		{
			name:        "nil config falls back to localhost",
			config:      nil,
			expectedURL: "http://localhost:8080/jsonrpc",
		},
		{
			name: "uses Kodi generic configuration",
			config: createTestConfigWithKodiDefaults(t, config.LaunchersDefault{
				Launcher:  "Kodi",
				ServerURL: "http://configured-kodi:9090",
			}),
			expectedURL: "http://configured-kodi:9090/jsonrpc",
		},
		{
			name: "handles trailing slash in ServerURL",
			config: createTestConfigWithKodiDefaults(t, config.LaunchersDefault{
				Launcher:  "Kodi",
				ServerURL: "http://configured-kodi:9090/",
			}),
			expectedURL: "http://configured-kodi:9090/jsonrpc",
		},
		{
			name: "preserves existing jsonrpc endpoint",
			config: createTestConfigWithKodiDefaults(t, config.LaunchersDefault{
				Launcher:  "Kodi",
				ServerURL: "http://configured-kodi:9090/jsonrpc",
			}),
			expectedURL: "http://configured-kodi:9090/jsonrpc",
		},
		{
			name: "adds http scheme when missing",
			config: createTestConfigWithKodiDefaults(t, config.LaunchersDefault{
				Launcher:  "Kodi",
				ServerURL: "configured-kodi:9090",
			}),
			expectedURL: "http://configured-kodi:9090/jsonrpc",
		},
		{
			name: "preserves https scheme",
			config: createTestConfigWithKodiDefaults(t, config.LaunchersDefault{
				Launcher:  "Kodi",
				ServerURL: "https://configured-kodi:9090",
			}),
			expectedURL: "https://configured-kodi:9090/jsonrpc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := kodi.NewClient(tt.config)
			actualURL := client.GetURL()
			assert.Equal(t, tt.expectedURL, actualURL)
		})
	}
}

// Helper function to create config instance with Kodi defaults for testing
func createTestConfigWithKodiDefaults(t *testing.T, defaults config.LaunchersDefault) *config.Instance {
	t.Helper()

	configDir := t.TempDir()
	values := config.Values{
		Launchers: config.Launchers{
			Default: []config.LaunchersDefault{defaults},
		},
	}

	cfg, err := config.NewConfig(configDir, values)
	require.NoError(t, err)
	return cfg
}

func TestClient_LaunchSong_MakesCorrectAPICall(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of LaunchSong to make real API requests
	// It should parse the song ID from the path and use Player.Open with songid parameter

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

		// Mock successful response
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      receivedPayload["id"],
			"result":  "OK",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Test launching a song
	songPath := "kodi-song://123/Artist - Song Title"
	err := client.LaunchSong(songPath)
	require.NoError(t, err)

	// Verify the correct API call was made
	assert.Equal(t, "Player.Open", receivedPayload["method"])
	assert.Equal(t, "2.0", receivedPayload["jsonrpc"])

	// Verify the parameters contain the song ID
	params, ok := receivedPayload["params"].(map[string]any)
	require.True(t, ok, "params should be a map")

	item, ok := params["item"].(map[string]any)
	require.True(t, ok, "item should be a map")

	// The song should be launched by ID, not file path
	songID, ok := item["songid"].(float64)
	require.True(t, ok, "songid should be present")
	assert.Equal(t, 123, int(songID))
}

func TestClient_GetSongs_MakesCorrectAPICall(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of GetSongs to retrieve songs from Kodi's library
	// It should use AudioLibrary.GetSongs API method

	var receivedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Decode the payload
		err := json.NewDecoder(r.Body).Decode(&receivedPayload)
		if err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Mock successful response with songs
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      receivedPayload["id"],
			"result": map[string]any{
				"songs": []map[string]any{
					{
						"songid":        123,
						"label":         "Test Song 1",
						"albumid":       456,
						"displayartist": "Test Artist",
						"album":         "Test Album",
						"duration":      240,
					},
					{
						"songid":        124,
						"label":         "Test Song 2",
						"albumid":       456,
						"displayartist": "Test Artist",
						"album":         "Test Album",
						"duration":      180,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Test getting songs
	songs, err := client.GetSongs(context.Background())
	require.NoError(t, err)

	// Verify the correct API call was made
	assert.Equal(t, "AudioLibrary.GetSongs", receivedPayload["method"])
	assert.Equal(t, "2.0", receivedPayload["jsonrpc"])

	// Verify properties were requested
	params, ok := receivedPayload["params"].(map[string]any)
	require.True(t, ok, "Params should be present")
	properties, ok := params["properties"].([]any)
	require.True(t, ok, "Properties should be present in params")
	assert.Contains(t, properties, "displayartist", "Should request displayartist property")
	assert.Contains(t, properties, "title", "Should request title property")
	assert.Contains(t, properties, "album", "Should request album property")
	assert.Contains(t, properties, "albumid", "Should request albumid property")

	// Verify the songs were parsed correctly
	require.Len(t, songs, 2)
	assert.Equal(t, 123, songs[0].ID)
	assert.Equal(t, "Test Song 1", songs[0].Label)
	assert.Equal(t, 456, songs[0].AlbumID)
	assert.Equal(t, "Test Artist", songs[0].Artist)
	assert.Equal(t, "Test Album", songs[0].Album)
	assert.Equal(t, 240, songs[0].Duration)
}

func TestClient_LaunchAlbum_MakesCorrectAPICall(t *testing.T) {
	t.Parallel()

	// This test verifies LaunchAlbum makes a direct Player.Open call with albumid

	var receivedParams kodi.PlayerOpenParams
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload kodi.APIPayload
		err := json.NewDecoder(r.Body).Decode(&payload)
		if err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		assert.Equal(t, kodi.APIMethodPlayerOpen, payload.Method)

		// Extract params
		paramsJSON, _ := json.Marshal(payload.Params)
		_ = json.Unmarshal(paramsJSON, &receivedParams)

		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      payload.ID,
			"result":  "OK",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Test launching an album
	albumPath := "kodi-album://456/Test Artist - Test Album"
	err := client.LaunchAlbum(albumPath)
	require.NoError(t, err)

	// Verify Player.Open was called with albumid
	assert.Equal(t, 456, receivedParams.Item.AlbumID, "albumid should be 456")
	assert.True(t, receivedParams.Options.Resume, "resume should be true")
}

func TestClient_GetAlbums_MakesCorrectAPICall(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of GetAlbums to retrieve albums from Kodi's library
	// It should use AudioLibrary.GetAlbums API method

	var receivedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Decode the payload
		err := json.NewDecoder(r.Body).Decode(&receivedPayload)
		if err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Mock successful response with albums
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      receivedPayload["id"],
			"result": map[string]any{
				"albums": []map[string]any{
					{
						"albumid":       456,
						"label":         "Test Album 1",
						"displayartist": "Test Artist 1",
						"year":          2020,
					},
					{
						"albumid":       457,
						"label":         "Test Album 2",
						"displayartist": "Test Artist 2",
						"year":          2021,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Test getting albums
	albums, err := client.GetAlbums(context.Background())
	require.NoError(t, err)

	// Verify the correct API call was made
	assert.Equal(t, "AudioLibrary.GetAlbums", receivedPayload["method"])
	assert.Equal(t, "2.0", receivedPayload["jsonrpc"])

	// Verify the albums were parsed correctly
	require.Len(t, albums, 2)
	assert.Equal(t, 456, albums[0].ID)
	assert.Equal(t, "Test Album 1", albums[0].Label)
	assert.Equal(t, "Test Artist 1", albums[0].Artist)
	assert.Equal(t, 2020, albums[0].Year)

	assert.Equal(t, 457, albums[1].ID)
	assert.Equal(t, "Test Album 2", albums[1].Label)
	assert.Equal(t, "Test Artist 2", albums[1].Artist)
	assert.Equal(t, 2021, albums[1].Year)
}

func TestClient_GetArtists_MakesCorrectAPICall(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of GetArtists to retrieve artists from Kodi's library
	// It should use AudioLibrary.GetArtists API method

	var receivedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Decode the payload
		err := json.NewDecoder(r.Body).Decode(&receivedPayload)
		if err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Mock successful response with artists
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      receivedPayload["id"],
			"result": map[string]any{
				"artists": []map[string]any{
					{
						"artistid": 789,
						"label":    "Test Artist 1",
					},
					{
						"artistid": 790,
						"label":    "Test Artist 2",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Test getting artists
	artists, err := client.GetArtists(context.Background())
	require.NoError(t, err)

	// Verify the correct API call was made
	assert.Equal(t, "AudioLibrary.GetArtists", receivedPayload["method"])
	assert.Equal(t, "2.0", receivedPayload["jsonrpc"])

	// Verify the artists were parsed correctly
	require.Len(t, artists, 2)
	assert.Equal(t, 789, artists[0].ID)
	assert.Equal(t, "Test Artist 1", artists[0].Label)

	assert.Equal(t, 790, artists[1].ID)
	assert.Equal(t, "Test Artist 2", artists[1].Label)
}

func TestClient_LaunchArtist_MakesCorrectAPICall(t *testing.T) {
	t.Parallel()

	// This test verifies LaunchArtist makes a direct Player.Open call with artistid

	var receivedParams kodi.PlayerOpenParams
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload kodi.APIPayload
		err := json.NewDecoder(r.Body).Decode(&payload)
		if err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		assert.Equal(t, kodi.APIMethodPlayerOpen, payload.Method)

		// Extract params
		paramsJSON, _ := json.Marshal(payload.Params)
		_ = json.Unmarshal(paramsJSON, &receivedParams)

		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      payload.ID,
			"result":  "OK",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Test launching an artist
	artistPath := "kodi-artist://789/Test Artist"
	err := client.LaunchArtist(artistPath)
	require.NoError(t, err)

	// Verify Player.Open was called with artistid
	assert.Equal(t, 789, receivedParams.Item.ArtistID, "artistid should be 789")
	assert.True(t, receivedParams.Options.Resume, "resume should be true")
}

func TestClient_LaunchTVShow_MakesCorrectAPICall(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of LaunchTVShow to make playlist-based API requests
	// It should: 1) Clear video playlist, 2) Get episodes for the show, 3) Add episodes to playlist, 4) Start playback

	var receivedPayloads []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		err := json.NewDecoder(r.Body).Decode(&payload)
		if err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		receivedPayloads = append(receivedPayloads, payload)

		method, ok := payload["method"].(string)
		assert.True(t, ok, "method should be a string")

		// Mock different responses based on method
		var response map[string]any
		switch method {
		case "Playlist.Clear":
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      payload["id"],
				"result":  "OK",
			}
		case "VideoLibrary.GetEpisodes":
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      payload["id"],
				"result": map[string]any{
					"episodes": []map[string]any{
						{
							"episodeid": 123,
							"tvshowid":  456,
							"label":     "Pilot",
							"season":    1,
							"episode":   1,
						},
						{
							"episodeid": 124,
							"tvshowid":  456,
							"label":     "Episode 2",
							"season":    1,
							"episode":   2,
						},
					},
				},
			}
		case "Playlist.Add":
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      payload["id"],
				"result":  "OK",
			}
		case "Player.Open":
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      payload["id"],
				"result":  "OK",
			}
		default:
			http.Error(w, "Unknown method", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client
	client := kodi.NewClient(nil)
	client.SetURL(server.URL)

	// Test launching a TV show
	showPath := "kodi-show://456/Breaking Bad"
	err := client.LaunchTVShow(showPath)
	require.NoError(t, err)

	// Verify the correct sequence of API calls was made
	require.Len(t, receivedPayloads, 4, "Should make 4 API calls: Clear, GetEpisodes, Add, Open")

	// 1. Clear video playlist (playlistid=1)
	assert.Equal(t, "Playlist.Clear", receivedPayloads[0]["method"])
	clearParams, ok := receivedPayloads[0]["params"].(map[string]any)
	require.True(t, ok, "params should be a map[string]any")
	assert.Equal(t, 1, int(clearParams["playlistid"].(float64)))

	// 2. Get episodes for the show
	assert.Equal(t, "VideoLibrary.GetEpisodes", receivedPayloads[1]["method"])
	episodesParams, ok := receivedPayloads[1]["params"].(map[string]any)
	require.True(t, ok, "params should be a map[string]any")
	assert.Equal(t, 456, int(episodesParams["tvshowid"].(float64)))

	// 3. Add episodes to playlist
	assert.Equal(t, "Playlist.Add", receivedPayloads[2]["method"])
	addParams, ok := receivedPayloads[2]["params"].(map[string]any)
	require.True(t, ok, "params should be a map[string]any")
	assert.Equal(t, 1, int(addParams["playlistid"].(float64)))

	// Verify episode items are properly structured
	items, ok := addParams["item"].([]any)
	require.True(t, ok, "item should be an array")
	require.Len(t, items, 2, "should have 2 episodes")

	firstItem, ok := items[0].(map[string]any)
	require.True(t, ok, "first item should be a map[string]any")
	assert.Equal(t, 123, int(firstItem["episodeid"].(float64)))

	// 4. Start playbook with playlist
	assert.Equal(t, "Player.Open", receivedPayloads[3]["method"])
	openParams, ok := receivedPayloads[3]["params"].(map[string]any)
	require.True(t, ok, "params should be a map[string]any")
	item, ok := openParams["item"].(map[string]any)
	require.True(t, ok, "item should be a map[string]any")
	assert.Equal(t, 1, int(item["playlistid"].(float64)))
}

func TestClient_APIRequest_UsesAuthenticationWhenConfigured(t *testing.T) {
	t.Parallel()

	// This test drives the implementation of authentication integration in APIRequest
	// It should look up credentials using LookupAuth and add the appropriate headers

	// Test 1: No authentication configured
	t.Run("no auth configured", func(t *testing.T) {
		t.Parallel()

		var receivedHeaders http.Header
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeaders = r.Header.Clone()

			response := map[string]any{
				"jsonrpc": "2.0",
				"id":      "test-id",
				"result":  "OK",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		// Create client
		client := kodi.NewClient(nil)
		client.SetURL(server.URL)

		// Make an API call - this should trigger authentication lookup
		_, err := client.APIRequest(context.Background(), "Player.GetActivePlayers", nil)
		require.NoError(t, err)

		// No auth headers should be present since no auth is configured
		assert.Empty(t, receivedHeaders.Get("Authorization"))
	})

	// Test 2: Basic authentication configured
	t.Run("basic auth configured", func(t *testing.T) {
		t.Parallel()

		// Test basic auth integration using config.LookupAuth

		var receivedHeaders http.Header
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeaders = r.Header.Clone()

			response := map[string]any{
				"jsonrpc": "2.0",
				"id":      "test-id",
				"result":  "OK",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		// Mock auth config by temporarily setting it
		// This simulates having auth configured for the server URL
		originalAuthCfg := config.GetAuthCfg()
		defer func() {
			// Restore original config after test
			if originalAuthCfg == nil {
				config.ClearAuthCfgForTesting()
			}
		}()

		// Set up test auth config
		testAuthCfg := map[string]config.CredentialEntry{
			server.URL: {
				Username: "testuser",
				Password: "testpass",
			},
		}
		config.SetAuthCfgForTesting(testAuthCfg)

		// Create client
		client := kodi.NewClient(nil)
		client.SetURL(server.URL)

		// Make an API call - this should add basic auth headers
		_, err := client.APIRequest(context.Background(), "Player.GetActivePlayers", nil)
		require.NoError(t, err)

		// Should have basic auth header
		authHeader := receivedHeaders.Get("Authorization")
		assert.True(t, strings.HasPrefix(authHeader, "Basic "), "Should have Basic auth header")

		// Decode and verify credentials
		basicAuth := strings.TrimPrefix(authHeader, "Basic ")
		decoded, err := base64.StdEncoding.DecodeString(basicAuth)
		require.NoError(t, err)
		assert.Equal(t, "testuser:testpass", string(decoded))
	})

	t.Run("bearer auth configured", func(t *testing.T) {
		// No t.Parallel() - modifies global auth config

		var receivedHeaders http.Header
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeaders = r.Header.Clone()

			response := map[string]any{
				"jsonrpc": "2.0",
				"id":      "test-id",
				"result":  "OK",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		// Mock auth config with bearer token
		originalAuthCfg := config.GetAuthCfg()
		defer func() {
			if originalAuthCfg == nil {
				config.ClearAuthCfgForTesting()
			}
		}()

		testAuthCfg := map[string]config.CredentialEntry{
			server.URL: {
				Bearer: "test-bearer-token-12345",
			},
		}
		config.SetAuthCfgForTesting(testAuthCfg)

		// Create client
		client := kodi.NewClient(nil)
		client.SetURL(server.URL)

		// Make an API call - this should add bearer auth headers
		_, err := client.APIRequest(context.Background(), "Player.GetActivePlayers", nil)
		require.NoError(t, err)

		// Should have bearer auth header
		authHeader := receivedHeaders.Get("Authorization")
		assert.Equal(t, "Bearer test-bearer-token-12345", authHeader)
	})
}
