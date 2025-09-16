//go:build linux

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package batocera

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
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
	assert.Equal(t, systemdefs.SystemMusicAlbum, kodiAlbum.SystemID)
	assert.Contains(t, kodiAlbum.Schemes, kodi.SchemeKodiAlbum)

	// Check KodiArtist launcher
	kodiArtist, exists := foundLaunchers["KodiArtist"]
	require.True(t, exists, "KodiArtist launcher should exist")
	assert.Equal(t, systemdefs.SystemMusicArtist, kodiArtist.SystemID)
	assert.Contains(t, kodiArtist.Schemes, kodi.SchemeKodiArtist)

	// Check KodiTVShow launcher
	kodiTVShow, exists := foundLaunchers["KodiTVShow"]
	require.True(t, exists, "KodiTVShow launcher should exist")
	assert.Equal(t, systemdefs.SystemTVShow, kodiTVShow.SystemID)
	assert.Contains(t, kodiTVShow.Schemes, kodi.SchemeKodiShow)
}

// TestLauncherExtensions tests that launchers have proper extensions from SystemInfo
func TestLauncherExtensions(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	p := &Platform{}
	launchers := p.Launchers(cfg)

	// Find the 3DO launcher to test extensions
	var threeDOLauncher *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "3DO" {
			threeDOLauncher = &launchers[i]
			break
		}
	}
	require.NotNil(t, threeDOLauncher, "3DO launcher should exist")

	// Test that 3DO launcher has proper extensions
	expectedExtensions := []string{".iso", ".chd", ".cue"}
	for _, ext := range expectedExtensions {
		assert.Contains(t, threeDOLauncher.Extensions, ext, "3DO launcher should support %s files", ext)
	}
}

// TestCommanderX16SystemImplemented tests that CommanderX16 system is properly implemented
func TestCommanderX16SystemImplemented(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	p := &Platform{}
	launchers := p.Launchers(cfg)

	// Find the CommanderX16 launcher
	var commanderX16Launcher *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "CommanderX16" {
			commanderX16Launcher = &launchers[i]
			break
		}
	}
	require.NotNil(t, commanderX16Launcher, "CommanderX16 launcher should exist")

	// Test that CommanderX16 launcher has proper system ID
	assert.Equal(t, systemdefs.SystemCommanderX16, commanderX16Launcher.SystemID)

	// Test that CommanderX16 launcher has proper extensions
	expectedExtensions := []string{".prg", ".crt", ".bin", ".zip"}
	for _, ext := range expectedExtensions {
		assert.Contains(t, commanderX16Launcher.Extensions, ext, "CommanderX16 launcher should support %s files", ext)
	}

	// Test that CommanderX16 has proper folder mapping
	assert.Contains(t, commanderX16Launcher.Folders, "commanderx16")
}

// TestBatoceraGameLaunchersSkipFilesystemScan tests that Batocera game launchers
// have SkipFilesystemScan set to true since they use gamelist.xml via custom Scanner
func TestBatoceraGameLaunchersSkipFilesystemScan(t *testing.T) {
	t.Parallel()
	platform := &Platform{}
	cfg := &config.Instance{}

	launchers := platform.Launchers(cfg)

	// Find Batocera game launchers (not Generic or Kodi launchers)
	gameSystemLaunchers := []string{}
	for _, launcher := range launchers {
		// Skip non-game launchers
		if launcher.ID == "Generic" ||
			launcher.ID == "KodiLocal" ||
			launcher.ID == "KodiMovie" ||
			launcher.ID == "KodiTV" ||
			launcher.ID == "KodiMusic" ||
			launcher.ID == "KodiSong" ||
			launcher.ID == "KodiAlbum" ||
			launcher.ID == "KodiArtist" ||
			launcher.ID == "KodiTVShow" {
			continue
		}

		gameSystemLaunchers = append(gameSystemLaunchers, launcher.ID)

		// EXPECTED: Batocera game system launchers should skip filesystem scanning
		// ACTUAL (before fix): SkipFilesystemScan is false (default)
		// This test will FAIL until we set SkipFilesystemScan = true on these launchers
		assert.True(t, launcher.SkipFilesystemScan,
			"Batocera game launcher %s should skip filesystem scanning (uses gamelist.xml)", launcher.ID)
	}

	// Verify we found some game system launchers to test
	assert.NotEmpty(t, gameSystemLaunchers, "Should find at least some Batocera game system launchers")
	t.Logf("Tested %d Batocera game system launchers: %v", len(gameSystemLaunchers), gameSystemLaunchers)
}

// TestKodiLaunchersAreIncluded tests that Batocera platform includes Kodi launchers
func TestKodiLaunchersAreIncluded(t *testing.T) {
	t.Parallel()
	platform := &Platform{}
	cfg := &config.Instance{}

	launchers := platform.Launchers(cfg)

	// Find Kodi library launchers that should have SkipFilesystemScan=true
	kodiLibraryLaunchers := []string{"KodiMovie", "KodiTV", "KodiAlbum", "KodiArtist", "KodiTVShow", "KodiSong"}
	foundKodiLaunchers := make(map[string]bool)

	for _, launcher := range launchers {
		for _, kodiID := range kodiLibraryLaunchers {
			if launcher.ID == kodiID {
				foundKodiLaunchers[kodiID] = true

				// Kodi library launchers should skip filesystem scanning (use API)
				assert.True(t, launcher.SkipFilesystemScan,
					"Kodi library launcher %s should skip filesystem scanning (uses API)", launcher.ID)
			}
		}

		// File-based Kodi launchers should allow filesystem scanning
		if launcher.ID == "KodiLocal" || launcher.ID == "KodiMusic" {
			assert.False(t, launcher.SkipFilesystemScan,
				"Kodi file launcher %s should allow filesystem scanning", launcher.ID)
		}
	}

	// Verify we found all expected Kodi library launchers
	for _, expectedID := range kodiLibraryLaunchers {
		assert.True(t, foundKodiLaunchers[expectedID],
			"Should find Kodi library launcher %s in Batocera platform", expectedID)
	}
}
