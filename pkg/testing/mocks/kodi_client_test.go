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

package mocks_test

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMockKodiClient_ImplementsInterface(t *testing.T) {
	t.Parallel()

	// Test that our mock can be used as a KodiClient
	mock := mocks.NewMockKodiClient()

	// Verify it implements the interface
	var client kodi.KodiClient = mock
	assert.NotNil(t, client)

	// Test that basic mock functionality works
	err := client.LaunchFile("/test/path")
	require.NoError(t, err) // Should succeed due to SetupBasicMock
}

func TestMockKodiClient_GetAlbums(t *testing.T) {
	t.Parallel()

	// Test GetAlbums mock method
	mock := &mocks.MockKodiClient{}

	// Setup mock expectation
	expectedAlbums := []kodi.Album{
		{Label: "Test Album", ID: 1, Artist: "Test Artist", Year: 2023},
	}
	mock.On("GetAlbums").Return(expectedAlbums, nil)

	// Use mock as KodiClient interface - this will fail compilation if GetAlbums is missing
	var client kodi.KodiClient = mock

	// Execute
	albums, err := client.GetAlbums()

	// Verify
	require.NoError(t, err)
	assert.Equal(t, expectedAlbums, albums)
	mock.AssertExpectations(t)
}

func TestMockKodiClient_GetArtists(t *testing.T) {
	t.Parallel()

	// Test GetArtists mock method
	mock := &mocks.MockKodiClient{}

	// Setup mock expectation
	expectedArtists := []kodi.Artist{
		{Label: "Test Artist", ID: 1},
	}
	mock.On("GetArtists").Return(expectedArtists, nil)

	// Use mock as KodiClient interface - this will fail compilation if GetArtists is missing
	var client kodi.KodiClient = mock

	// Execute
	artists, err := client.GetArtists()

	// Verify
	require.NoError(t, err)
	assert.Equal(t, expectedArtists, artists)
	mock.AssertExpectations(t)
}

func TestMockKodiClient_GetSongs(t *testing.T) {
	t.Parallel()

	// Test GetSongs mock method
	mock := &mocks.MockKodiClient{}

	// Setup mock expectation
	expectedSongs := []kodi.Song{
		{Label: "Test Song", ID: 1, AlbumID: 1, Artist: "Test Artist", Duration: 180},
	}
	mock.On("GetSongs").Return(expectedSongs, nil)

	// Use mock as KodiClient interface - this will fail compilation if GetSongs is missing
	var client kodi.KodiClient = mock

	// Execute
	songs, err := client.GetSongs()

	// Verify
	require.NoError(t, err)
	assert.Equal(t, expectedSongs, songs)
	mock.AssertExpectations(t)
}

func TestMockKodiClient_LaunchAlbum(t *testing.T) {
	t.Parallel()

	// Test LaunchAlbum mock method
	mock := &mocks.MockKodiClient{}

	// Setup mock expectation
	mock.On("LaunchAlbum", "kodi-album://1/Test Album").Return(nil)

	// Use mock as KodiClient interface - this will fail compilation if LaunchAlbum is missing
	var client kodi.KodiClient = mock

	// Execute
	err := client.LaunchAlbum("kodi-album://1/Test Album")

	// Verify
	require.NoError(t, err)
	mock.AssertExpectations(t)
}

func TestMockKodiClient_LaunchArtist(t *testing.T) {
	t.Parallel()

	// Test LaunchArtist mock method
	mock := &mocks.MockKodiClient{}

	// Setup mock expectation
	mock.On("LaunchArtist", "kodi-artist://1/Test Artist").Return(nil)

	// Use mock as KodiClient interface - this will fail compilation if LaunchArtist is missing
	var client kodi.KodiClient = mock

	// Execute
	err := client.LaunchArtist("kodi-artist://1/Test Artist")

	// Verify
	require.NoError(t, err)
	mock.AssertExpectations(t)
}

func TestMockKodiClient_LaunchSong(t *testing.T) {
	t.Parallel()

	// Test LaunchSong mock method
	mock := &mocks.MockKodiClient{}

	// Setup mock expectation
	mock.On("LaunchSong", "kodi-song://1/Test Song").Return(nil)

	// Use mock as KodiClient interface - this will fail compilation if LaunchSong is missing
	var client kodi.KodiClient = mock

	// Execute
	err := client.LaunchSong("kodi-song://1/Test Song")

	// Verify
	require.NoError(t, err)
	mock.AssertExpectations(t)
}

func TestMockKodiClient_LaunchTVShow(t *testing.T) {
	t.Parallel()

	// Test LaunchTVShow mock method
	mock := &mocks.MockKodiClient{}

	// Setup mock expectation
	mock.On("LaunchTVShow", "kodi-show://1/Test Show").Return(nil)

	// Use mock as KodiClient interface - this will fail compilation if LaunchTVShow is missing
	var client kodi.KodiClient = mock

	// Execute
	err := client.LaunchTVShow("kodi-show://1/Test Show")

	// Verify
	require.NoError(t, err)
	mock.AssertExpectations(t)
}
