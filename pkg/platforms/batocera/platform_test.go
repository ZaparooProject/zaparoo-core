//go:build linux

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package batocera

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKodiLocalLauncherExists tests that KodiLocal launcher exists
func TestKodiLocalLauncherExists(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}

	launchers := platform.Launchers(cfg)

	// Look for KodiLocal launcher
	var kodiLocal *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "KodiLocal" {
			kodiLocal = &launchers[i]
			break
		}
	}

	require.NotNil(t, kodiLocal, "KodiLocal launcher should exist")
	assert.Equal(t, "KodiLocal", kodiLocal.ID)
	assert.Equal(t, systemdefs.SystemVideo, kodiLocal.SystemID)
	assert.Contains(t, kodiLocal.Folders, "videos")
	assert.Contains(t, kodiLocal.Extensions, ".mp4")
	assert.Contains(t, kodiLocal.Extensions, ".mkv")
	assert.Contains(t, kodiLocal.Extensions, ".avi")
}

// TestKodiLocalLaunchesVideoFiles tests that KodiLocal launcher can launch video files
func TestKodiLocalLaunchesVideoFiles(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}

	launchers := platform.Launchers(cfg)

	// Find KodiLocal launcher
	var kodiLocal *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "KodiLocal" {
			kodiLocal = &launchers[i]
			break
		}
	}

	require.NotNil(t, kodiLocal, "KodiLocal launcher should exist")

	// Test that the launcher has a Launch function
	// We don't test the actual launch since it requires a running Kodi instance
	assert.NotNil(t, kodiLocal.Launch, "KodiLocal should have a Launch function")
}

// TestKodiMovieLauncherExists tests that KodiMovie launcher exists in Batocera
func TestKodiMovieLauncherExists(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}

	launchers := platform.Launchers(cfg)

	// Look for KodiMovie launcher
	var kodiMovie *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "KodiMovie" {
			kodiMovie = &launchers[i]
			break
		}
	}

	require.NotNil(t, kodiMovie, "KodiMovie launcher should exist")
	assert.Equal(t, "KodiMovie", kodiMovie.ID)
	assert.Equal(t, systemdefs.SystemMovie, kodiMovie.SystemID)
	assert.Contains(t, kodiMovie.Schemes, kodi.SchemeKodiMovie)
}

// TestKodiTVLauncherExists tests that KodiTV launcher exists in Batocera
func TestKodiTVLauncherExists(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}

	launchers := platform.Launchers(cfg)

	// Look for KodiTV launcher
	var kodiTV *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "KodiTV" {
			kodiTV = &launchers[i]
			break
		}
	}

	require.NotNil(t, kodiTV, "KodiTV launcher should exist")
	assert.Equal(t, "KodiTV", kodiTV.ID)
	assert.Equal(t, systemdefs.SystemTV, kodiTV.SystemID)
	assert.Contains(t, kodiTV.Schemes, kodi.SchemeKodiEpisode)
}

// TestKodiMusicLauncherExists tests that KodiMusic launcher exists in Batocera
func TestKodiMusicLauncherExists(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}
	launchers := platform.Launchers(cfg)

	// Look for KodiMusic launcher
	var kodiMusic *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "KodiMusic" {
			kodiMusic = &launchers[i]
			break
		}
	}

	require.NotNil(t, kodiMusic, "KodiMusic launcher should exist")
	assert.Equal(t, "KodiMusic", kodiMusic.ID)
	assert.Equal(t, systemdefs.SystemMusic, kodiMusic.SystemID)
	assert.Contains(t, kodiMusic.Extensions, ".mp3")
	assert.Contains(t, kodiMusic.Extensions, ".flac")
}

// TestKodiCollectionLaunchersExist tests that all Kodi collection launchers exist in Batocera
func TestKodiCollectionLaunchersExist(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}
	launchers := platform.Launchers(cfg)

	// Map to track found launchers
	foundLaunchers := make(map[string]*platforms.Launcher)
	for i := range launchers {
		foundLaunchers[launchers[i].ID] = &launchers[i]
	}

	// Check KodiSong launcher
	kodiSong, exists := foundLaunchers["KodiSong"]
	require.True(t, exists, "KodiSong launcher should exist")
	assert.Equal(t, systemdefs.SystemMusic, kodiSong.SystemID)
	assert.Contains(t, kodiSong.Schemes, kodi.SchemeKodiSong)

	// Check KodiAlbum launcher
	kodiAlbum, exists := foundLaunchers["KodiAlbum"]
	require.True(t, exists, "KodiAlbum launcher should exist")
	assert.Equal(t, systemdefs.SystemMusic, kodiAlbum.SystemID)
	assert.Contains(t, kodiAlbum.Schemes, kodi.SchemeKodiAlbum)

	// Check KodiArtist launcher
	kodiArtist, exists := foundLaunchers["KodiArtist"]
	require.True(t, exists, "KodiArtist launcher should exist")
	assert.Equal(t, systemdefs.SystemMusic, kodiArtist.SystemID)
	assert.Contains(t, kodiArtist.Schemes, kodi.SchemeKodiArtist)

	// Check KodiTVShow launcher
	kodiTVShow, exists := foundLaunchers["KodiTVShow"]
	require.True(t, exists, "KodiTVShow launcher should exist")
	assert.Equal(t, systemdefs.SystemTV, kodiTVShow.SystemID)
	assert.Contains(t, kodiTVShow.Schemes, kodi.SchemeKodiShow)
}
