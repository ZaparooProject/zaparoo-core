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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMovieVirtualPathRoundTrip(t *testing.T) {
	t.Parallel()

	path := createMovieVirtualPath(Movie{
		ID:        7,
		Label:     "Alien (1979)",
		UniqueIDs: map[string]string{"tmdb": "348", "imdb": "tt0078748", "unknown": "x"},
	})
	assert.Equal(t, "kodi-movie://7/Alien%20%281979%29;tmdb=348;imdb=tt0078748", path)

	ref, err := parseMovieReference(path)
	require.NoError(t, err)
	assert.Equal(t, 7, ref.ID)
	assert.Equal(t, "Alien (1979)", ref.Name)
	assert.Equal(t, map[string]string{"tmdb": "348", "imdb": "tt0078748"}, ref.UniqueIDs)

	assert.Equal(t, "kodi-movie://7/Alien", createMovieVirtualPath(Movie{ID: 7, Label: "Alien"}))
}

func TestEpisodeVirtualPathRoundTrip(t *testing.T) {
	t.Parallel()

	show := TVShow{ID: 3, Label: "Lost", UniqueIDs: map[string]string{"tvdb": "73739"}}
	ep := Episode{ID: 40, Label: "Pilot", Season: 1, Episode: 2, UniqueIDs: map[string]string{"tvdb": "127131"}}
	path := createEpisodeVirtualPath(show, &ep)
	assert.Equal(t, "kodi-episode://40/Lost%20-%20Pilot;tvdb=127131;season=1;episode=2;show_tvdb=73739", path)

	ref, err := parseEpisodeReference(path)
	require.NoError(t, err)
	assert.Equal(t, episodeReference{
		UniqueIDs:     map[string]string{"tvdb": "127131"},
		ShowUniqueIDs: map[string]string{"tvdb": "73739"},
		Name:          "Lost - Pilot",
		ID:            40,
		Season:        1,
		Episode:       2,
	}, ref)
	assert.True(t, ref.hasIdentity())

	legacy, err := parseEpisodeReference("kodi-episode://40/Lost%20-%20Pilot")
	require.NoError(t, err)
	assert.False(t, legacy.hasIdentity())
}

func TestParseReferenceRejectsBadPaths(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"kodi-movie://abc/Name",
		"kodi-episode://1/Name",
		"kodi-movie://1/Name?tmdb=1&tmdb=2",
	} {
		_, err := parseMovieReference(path)
		require.Error(t, err, path)
	}
	_, err := parseEpisodeReference("kodi-episode://1/Name?show_tvdb=1&show_tvdb=2")
	assert.Error(t, err)
}

func TestResolveMovie(t *testing.T) {
	t.Parallel()

	alien := Movie{ID: 70, Label: "Alien (1979)", UniqueIDs: map[string]string{"tmdb": "348"}}
	other := Movie{ID: 7, Label: "Heat (1995)", UniqueIDs: map[string]string{"tmdb": "949"}}
	cardPath := "kodi-movie://7/Alien%20%281979%29?tmdb=348"

	tests := []struct {
		details *Movie
		name    string
		path    string
		errText string
		movies  []Movie
		want    int
	}{
		{
			name:    "legacy card keeps its ID",
			path:    "kodi-movie://7/Alien",
			details: &other,
			want:    7,
		},
		{
			name:    "card ID still names the same movie",
			path:    cardPath,
			details: &Movie{ID: 7, Label: "Alien (1979)", UniqueIDs: map[string]string{"tmdb": "348"}},
			want:    7,
		},
		{
			name:    "ID reused by another movie follows the provider ID",
			path:    cardPath,
			details: &other,
			movies:  []Movie{other, alien},
			want:    70,
		},
		{
			name:   "deleted ID follows the provider ID",
			path:   cardPath,
			movies: []Movie{other, alien},
			want:   70,
		},
		{
			name:   "legacy card with deleted ID falls back to the title",
			path:   "kodi-movie://7/Alien%20%281979%29",
			movies: []Movie{other, alien},
			want:   70,
		},
		{
			name:   "provider ID missing in Kodi falls back to the title",
			path:   cardPath,
			movies: []Movie{other, {ID: 71, Label: "Alien (1979)"}},
			want:   71,
		},
		{
			name:    "ambiguous title is an error",
			path:    "kodi-movie://7/Alien",
			movies:  []Movie{{ID: 1, Label: "Alien"}, {ID: 2, Label: "alien"}},
			errText: "no longer available",
		},
		{
			name: "ambiguous provider ID is an error",
			path: cardPath,
			movies: []Movie{
				{ID: 1, Label: "A", UniqueIDs: alien.UniqueIDs},
				{ID: 2, Label: "B", UniqueIDs: alien.UniqueIDs},
			},
			errText: "multiple Kodi movies",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := &MockKodiClient{}
			client.On("GetMovieDetails", mock.Anything, 7).Return(tc.details, nil)
			client.On("GetMovies", mock.Anything).Return(tc.movies, nil).Maybe()

			got, err := resolveMovie(context.Background(), client, tc.path)
			if tc.errText != "" {
				require.ErrorContains(t, err, tc.errText)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			if tc.movies == nil {
				client.AssertNotCalled(t, "GetMovies", mock.Anything)
			}
		})
	}
}

func TestResolveEpisode(t *testing.T) {
	t.Parallel()

	lost := TVShow{ID: 30, Label: "Lost", UniqueIDs: map[string]string{"tvdb": "73739"}}
	heroes := TVShow{ID: 31, Label: "Heroes", UniqueIDs: map[string]string{"tvdb": "79501"}}
	lostEpisodes := []Episode{
		{ID: 400, Label: "Pilot", Season: 1, Episode: 1, UniqueIDs: map[string]string{"tvdb": "127131"}},
		{ID: 401, Label: "Tabula Rasa", Season: 1, Episode: 3},
	}
	heroesEpisodes := []Episode{{ID: 500, Label: "Genesis", Season: 1, Episode: 1}}
	pilotCard := "kodi-episode://40/Lost%20-%20Pilot?tvdb=127131&season=1&episode=1&show_tvdb=73739"

	tests := []struct {
		details *Episode
		name    string
		path    string
		errText string
		want    int
		listed  bool
	}{
		{
			name:    "legacy card keeps its ID",
			path:    "kodi-episode://40/Lost%20-%20Pilot",
			details: &Episode{ID: 40, Label: "Genesis", ShowTitle: "Heroes"},
			want:    40,
		},
		{
			name:    "card ID still names the same episode",
			path:    pilotCard,
			details: &Episode{ID: 40, Label: "Pilot", ShowTitle: "Lost", Season: 1, Episode: 1},
			want:    40,
		},
		{
			name:    "renamed episode in the same place is kept",
			path:    "kodi-episode://40/Lost%20-%20Pilot?season=1&episode=1",
			details: &Episode{ID: 40, Label: "Pilot (1)", ShowTitle: "Lost", Season: 1, Episode: 1},
			want:    40,
		},
		{
			name:    "ID reused by another show's episode follows the provider ID",
			path:    pilotCard,
			details: &Episode{ID: 40, Label: "Genesis", ShowTitle: "Heroes", Season: 1, Episode: 1},
			want:    400,
			listed:  true,
		},
		{
			name:   "deleted ID with no episode provider ID uses season and episode",
			path:   "kodi-episode://41/Lost%20-%20Old%20Title?season=1&episode=3&show_tvdb=73739",
			want:   401,
			listed: true,
		},
		{
			name:   "legacy card with deleted ID falls back to the title",
			path:   "kodi-episode://41/Lost%20-%20Tabula%20Rasa",
			want:   401,
			listed: true,
		},
		{
			name:    "nothing matches",
			path:    "kodi-episode://41/Lost%20-%20Gone?season=9&episode=9",
			errText: "no longer available",
			listed:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := &MockKodiClient{}
			client.On("GetEpisodeDetails", mock.Anything, mock.AnythingOfType("int")).Return(tc.details, nil)
			client.On("GetTVShows", mock.Anything).Return([]TVShow{lost, heroes}, nil).Maybe()
			client.On("GetEpisodes", mock.Anything, 30).Return(lostEpisodes, nil).Maybe()
			client.On("GetEpisodes", mock.Anything, 31).Return(heroesEpisodes, nil).Maybe()

			got, err := resolveEpisode(context.Background(), client, tc.path)
			if tc.errText != "" {
				require.ErrorContains(t, err, tc.errText)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			if !tc.listed {
				client.AssertNotCalled(t, "GetTVShows", mock.Anything)
			}
		})
	}
}

func TestResolveLibraryPathDropsIdentity(t *testing.T) {
	t.Parallel()

	client := &MockKodiClient{}
	client.On("GetMovieDetails", mock.Anything, 7).Return((*Movie)(nil), nil)
	client.On("GetMovies", mock.Anything).
		Return([]Movie{{ID: 70, Label: "Alien", UniqueIDs: map[string]string{"tmdb": "348"}}}, nil)

	got, err := resolveLibraryPath(client, "kodi-movie://7/Alien?tmdb=348", "kodi-movie", resolveMovie)
	require.NoError(t, err)
	assert.Equal(t, "kodi-movie://70/Alien", got)
}

func TestGetMovieDetailsMissingItem(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == string(APIMethodVideoLibraryGetMovieDetails) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","error":{"code":-32602,"message":"Invalid params."}}`))
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","error":{"code":-32601,"message":"Method not found."}}`))
	}))
	defer server.Close()

	client := &Client{url: server.URL + "/jsonrpc"}
	movie, err := client.GetMovieDetails(context.Background(), 9)
	require.NoError(t, err)
	assert.Nil(t, movie)

	_, err = client.GetEpisodeDetails(context.Background(), 9)
	require.ErrorContains(t, err, "Method not found")
}

func FuzzParseLibraryReferences(f *testing.F) {
	f.Add("kodi-movie://7/Alien?tmdb=348&imdb=tt0078748")
	f.Add("kodi-episode://40/Lost%20-%20Pilot?tvdb=1&season=1&episode=2&show_tvdb=73739")
	f.Add("kodi-episode://40/x?season=-1&episode=99999999999999999999")
	f.Add("kodi-movie://1/%zz?tmdb=%")
	f.Add("kodi-episode://40/Lost%20-%20Pilot;tvdb=1;season=1;episode=2;show_tvdb=73739")
	f.Add("kodi-movie://7/Up%3B%20Down;tmdb=348?imdb=tt0078748")
	f.Add("kodi-movie://1/x;tmdb=%")
	f.Fuzz(func(t *testing.T, path string) {
		if ref, err := parseMovieReference(path); err == nil {
			_ = createMovieVirtualPath(Movie{ID: ref.ID, Label: ref.Name, UniqueIDs: ref.UniqueIDs})
		}
		if ref, err := parseEpisodeReference(path); err == nil {
			if ref.Season < 0 || ref.Episode < 0 {
				t.Fatalf("negative episode position from %q", path)
			}
		}
	})
}
