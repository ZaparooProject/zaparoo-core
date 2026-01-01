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

package kodi_test

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/stretchr/testify/assert"
)

func TestTypes_CanBeCreated(t *testing.T) {
	t.Parallel()

	// Test basic Kodi types for JSON marshaling

	// Test Player type
	player := kodi.Player{
		ID:   1,
		Type: "video",
	}
	assert.Equal(t, 1, player.ID)
	assert.Equal(t, "video", player.Type)

	// Test Movie type
	movie := kodi.Movie{
		ID:    123,
		Label: "Test Movie",
		File:  "/path/to/movie.mp4",
	}
	assert.Equal(t, 123, movie.ID)
	assert.Equal(t, "Test Movie", movie.Label)
	assert.Equal(t, "/path/to/movie.mp4", movie.File)

	// Test TVShow type
	show := kodi.TVShow{
		ID:    456,
		Label: "Test Show",
	}
	assert.Equal(t, 456, show.ID)
	assert.Equal(t, "Test Show", show.Label)

	// Test Episode type
	episode := kodi.Episode{
		ID:       789,
		TVShowID: 456,
		Label:    "Test Episode",
		File:     "/path/to/episode.mp4",
	}
	assert.Equal(t, 789, episode.ID)
	assert.Equal(t, 456, episode.TVShowID)
	assert.Equal(t, "Test Episode", episode.Label)
	assert.Equal(t, "/path/to/episode.mp4", episode.File)
}

func TestAudioSchemeConstants(t *testing.T) {
	t.Parallel()

	// Test that audio scheme constants are defined correctly
	// This test drives the implementation of audio support constants

	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{
			name:     "song scheme constant",
			constant: shared.SchemeKodiSong,
			expected: "kodi-song",
		},
		{
			name:     "album scheme constant",
			constant: shared.SchemeKodiAlbum,
			expected: "kodi-album",
		},
		{
			name:     "artist scheme constant",
			constant: shared.SchemeKodiArtist,
			expected: "kodi-artist",
		},
		{
			name:     "show scheme constant",
			constant: shared.SchemeKodiShow,
			expected: "kodi-show",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.constant)
		})
	}
}

func TestAudioTypes_CanBeCreated(t *testing.T) {
	t.Parallel()

	// Test that audio types can be created with the right fields
	// This drives the implementation of Song, Album, and Artist types

	// Test Song type
	song := kodi.Song{
		ID:       123,
		Label:    "Test Song",
		File:     "/music/artist/album/song.mp3",
		AlbumID:  456,
		Artist:   "Test Artist",
		Duration: 240,
	}
	assert.Equal(t, 123, song.ID)
	assert.Equal(t, "Test Song", song.Label)
	assert.Equal(t, "/music/artist/album/song.mp3", song.File)
	assert.Equal(t, 456, song.AlbumID)
	assert.Equal(t, "Test Artist", song.Artist)
	assert.Equal(t, 240, song.Duration)

	// Test Album type
	album := kodi.Album{
		ID:     456,
		Label:  "Test Album",
		Artist: "Test Artist",
		Year:   2023,
	}
	assert.Equal(t, 456, album.ID)
	assert.Equal(t, "Test Album", album.Label)
	assert.Equal(t, "Test Artist", album.Artist)
	assert.Equal(t, 2023, album.Year)

	// Test Artist type
	artist := kodi.Artist{
		ID:    789,
		Label: "Test Artist",
	}
	assert.Equal(t, 789, artist.ID)
	assert.Equal(t, "Test Artist", artist.Label)
}

func TestAudioAPIMethodConstants(t *testing.T) {
	t.Parallel()

	// Test that audio API method constants are defined correctly
	// This drives the implementation of audio library and playlist API methods

	tests := []struct {
		name     string
		constant kodi.APIMethod
		expected string
	}{
		{
			name:     "AudioLibrary.GetSongs method",
			constant: kodi.APIMethodAudioLibraryGetSongs,
			expected: "AudioLibrary.GetSongs",
		},
		{
			name:     "AudioLibrary.GetAlbums method",
			constant: kodi.APIMethodAudioLibraryGetAlbums,
			expected: "AudioLibrary.GetAlbums",
		},
		{
			name:     "AudioLibrary.GetArtists method",
			constant: kodi.APIMethodAudioLibraryGetArtists,
			expected: "AudioLibrary.GetArtists",
		},
		{
			name:     "Playlist.Clear method",
			constant: kodi.APIMethodPlaylistClear,
			expected: "Playlist.Clear",
		},
		{
			name:     "Playlist.Add method",
			constant: kodi.APIMethodPlaylistAdd,
			expected: "Playlist.Add",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, string(tt.constant))
		})
	}
}

func TestAudioResponseTypes_CanBeCreated(t *testing.T) {
	t.Parallel()

	// Test that audio response types can be created with the right structure
	// This drives the implementation of response types for audio API operations

	// Test AudioLibraryGetSongsResponse
	songsResponse := kodi.AudioLibraryGetSongsResponse{
		Songs: []kodi.Song{
			{ID: 1, Label: "Song 1", Artist: "Artist 1"},
			{ID: 2, Label: "Song 2", Artist: "Artist 2"},
		},
	}
	assert.Len(t, songsResponse.Songs, 2)
	assert.Equal(t, 1, songsResponse.Songs[0].ID)
	assert.Equal(t, "Song 1", songsResponse.Songs[0].Label)

	// Test AudioLibraryGetAlbumsResponse
	albumsResponse := kodi.AudioLibraryGetAlbumsResponse{
		Albums: []kodi.Album{
			{ID: 1, Label: "Album 1", Artist: "Artist 1"},
			{ID: 2, Label: "Album 2", Artist: "Artist 2"},
		},
	}
	assert.Len(t, albumsResponse.Albums, 2)
	assert.Equal(t, 1, albumsResponse.Albums[0].ID)
	assert.Equal(t, "Album 1", albumsResponse.Albums[0].Label)

	// Test AudioLibraryGetArtistsResponse
	artistsResponse := kodi.AudioLibraryGetArtistsResponse{
		Artists: []kodi.Artist{
			{ID: 1, Label: "Artist 1"},
			{ID: 2, Label: "Artist 2"},
		},
	}
	assert.Len(t, artistsResponse.Artists, 2)
	assert.Equal(t, 1, artistsResponse.Artists[0].ID)
	assert.Equal(t, "Artist 1", artistsResponse.Artists[0].Label)
}

func TestPlaylistOperationTypes_CanBeCreated(t *testing.T) {
	t.Parallel()

	// Test that playlist operation types can be created with the right structure
	// This drives the implementation of playlist parameter types

	// Test PlaylistClearParams
	clearParams := kodi.PlaylistClearParams{
		PlaylistID: 0, // Music playlist
	}
	assert.Equal(t, 0, clearParams.PlaylistID)

	// Test PlaylistAddParams with songs
	addParams := kodi.PlaylistAddParams{
		PlaylistID: 0, // Music playlist
		Item: []kodi.PlaylistItemSongID{
			{SongID: 123},
			{SongID: 456},
		},
	}
	assert.Equal(t, 0, addParams.PlaylistID)
	assert.Len(t, addParams.Item, 2)
	assert.Equal(t, 123, addParams.Item[0].SongID)
	assert.Equal(t, 456, addParams.Item[1].SongID)

	// Test PlaylistItemSongID
	songItem := kodi.PlaylistItemSongID{
		SongID: 789,
	}
	assert.Equal(t, 789, songItem.SongID)
}
