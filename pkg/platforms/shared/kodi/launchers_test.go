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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
)

// TestNewKodiLocalLauncher tests the creation of standard KodiLocal launcher
func TestNewKodiLocalLauncher(t *testing.T) {
	t.Parallel()

	launcher := NewKodiLocalLauncher()

	assert.Equal(t, "KodiLocal", launcher.ID)
	assert.Equal(t, systemdefs.SystemVideo, launcher.SystemID)
	assert.Equal(t, []string{"videos", "tvshows"}, launcher.Folders)

	// Test all required extensions from LibreELEC
	expectedExtensions := []string{
		".avi", ".mp4", ".mkv", ".iso", ".bdmv", ".ifo", ".mpeg", ".mpg",
		".mov", ".wmv", ".flv", ".webm", ".m4v", ".3gp", ".ts", ".m2ts", ".mts",
	}
	assert.Equal(t, expectedExtensions, launcher.Extensions)
	assert.NotNil(t, launcher.Launch, "Launch function should be set")
}

// TestNewKodiMovieLauncher tests the creation of standard KodiMovie launcher
func TestNewKodiMovieLauncher(t *testing.T) {
	t.Parallel()

	launcher := NewKodiMovieLauncher()

	assert.Equal(t, "KodiMovie", launcher.ID)
	assert.Equal(t, systemdefs.SystemMovie, launcher.SystemID)
	assert.Equal(t, []string{SchemeKodiMovie}, launcher.Schemes)
	assert.NotNil(t, launcher.Launch, "Launch function should be set")
	assert.NotNil(t, launcher.Scanner, "Scanner function should be set")
}

// TestNewKodiTVLauncher tests the creation of standard KodiTV launcher
func TestNewKodiTVLauncher(t *testing.T) {
	t.Parallel()

	launcher := NewKodiTVLauncher()

	assert.Equal(t, "KodiTV", launcher.ID)
	assert.Equal(t, systemdefs.SystemTV, launcher.SystemID)
	assert.Equal(t, []string{SchemeKodiEpisode}, launcher.Schemes)
	assert.NotNil(t, launcher.Launch, "Launch function should be set")
	assert.NotNil(t, launcher.Scanner, "Scanner function should be set")
}

// TestNewKodiSongLauncher tests the creation of KodiSong launcher for individual songs
func TestNewKodiSongLauncher(t *testing.T) {
	t.Parallel()

	launcher := NewKodiSongLauncher()

	assert.Equal(t, "KodiSong", launcher.ID)
	assert.Equal(t, systemdefs.SystemMusic, launcher.SystemID)
	assert.Equal(t, []string{SchemeKodiSong}, launcher.Schemes)
	assert.NotNil(t, launcher.Launch, "Launch function should be set")
	// Scanner will be tested when scanners are implemented
	// assert.NotNil(t, launcher.Scanner, "Scanner function should be set")
}

// TestNewKodiMusicLauncher tests the creation of KodiMusic launcher for local music files
func TestNewKodiMusicLauncher(t *testing.T) {
	t.Parallel()

	launcher := NewKodiMusicLauncher()

	assert.Equal(t, "KodiMusic", launcher.ID)
	assert.Equal(t, systemdefs.SystemMusic, launcher.SystemID)
	assert.Contains(t, launcher.Folders, "music")
	assert.Contains(t, launcher.Extensions, ".mp3")
	assert.Contains(t, launcher.Extensions, ".flac")
	assert.Contains(t, launcher.Extensions, ".ogg")
	assert.Contains(t, launcher.Extensions, ".m4a")
	assert.NotNil(t, launcher.Launch, "Launch function should be set")
	assert.Nil(t, launcher.Scanner, "Scanner function should not be set for local files")
}

// TestNewKodiAlbumLauncher tests the creation of KodiAlbum launcher for album collection playback
func TestNewKodiAlbumLauncher(t *testing.T) {
	t.Parallel()

	launcher := NewKodiAlbumLauncher()

	assert.Equal(t, "KodiAlbum", launcher.ID)
	assert.Equal(t, systemdefs.SystemMusic, launcher.SystemID)
	assert.Equal(t, []string{SchemeKodiAlbum}, launcher.Schemes)
	assert.NotNil(t, launcher.Launch, "Launch function should be set")
	assert.NotNil(t, launcher.Scanner, "Scanner function should be set for collection")
}

// TestNewKodiArtistLauncher tests the creation of KodiArtist launcher for artist collection playback
func TestNewKodiArtistLauncher(t *testing.T) {
	t.Parallel()

	launcher := NewKodiArtistLauncher()

	assert.Equal(t, "KodiArtist", launcher.ID)
	assert.Equal(t, systemdefs.SystemMusic, launcher.SystemID)
	assert.Equal(t, []string{SchemeKodiArtist}, launcher.Schemes)
	assert.NotNil(t, launcher.Launch, "Launch function should be set")
	assert.NotNil(t, launcher.Scanner, "Scanner function should be set for collection")
}

// TestNewKodiTVShowLauncher tests the creation of KodiTVShow launcher for TV show collection playback
func TestNewKodiTVShowLauncher(t *testing.T) {
	t.Parallel()

	launcher := NewKodiTVShowLauncher()

	assert.Equal(t, "KodiTVShow", launcher.ID)
	assert.Equal(t, systemdefs.SystemTV, launcher.SystemID)
	assert.Equal(t, []string{SchemeKodiShow}, launcher.Schemes)
	assert.NotNil(t, launcher.Launch, "Launch function should be set")
	assert.NotNil(t, launcher.Scanner, "Scanner function should be set for collection")
}
