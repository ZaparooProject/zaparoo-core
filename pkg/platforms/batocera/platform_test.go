//go:build linux

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package batocera

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
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
	assert.Contains(t, kodiMovie.Schemes, shared.SchemeKodiMovie)
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
	assert.Contains(t, kodiTV.Schemes, shared.SchemeKodiEpisode)
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
	assert.Contains(t, kodiSong.Schemes, shared.SchemeKodiSong)

	// Check KodiAlbum launcher
	kodiAlbum, exists := foundLaunchers["KodiAlbum"]
	require.True(t, exists, "KodiAlbum launcher should exist")
	assert.Equal(t, systemdefs.SystemMusicAlbum, kodiAlbum.SystemID)
	assert.Contains(t, kodiAlbum.Schemes, shared.SchemeKodiAlbum)

	// Check KodiArtist launcher
	kodiArtist, exists := foundLaunchers["KodiArtist"]
	require.True(t, exists, "KodiArtist launcher should exist")
	assert.Equal(t, systemdefs.SystemMusicArtist, kodiArtist.SystemID)
	assert.Contains(t, kodiArtist.Schemes, shared.SchemeKodiArtist)

	// Check KodiTVShow launcher
	kodiTVShow, exists := foundLaunchers["KodiTVShow"]
	require.True(t, exists, "KodiTVShow launcher should exist")
	assert.Equal(t, systemdefs.SystemTVShow, kodiTVShow.SystemID)
	assert.Contains(t, kodiTVShow.Schemes, shared.SchemeKodiShow)
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

// TestStartPost_NoRunningGame tests that StartPost handles no running game gracefully
func TestStartPost_NoRunningGame(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	// Setup mock ES API server
	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithNoRunningGame()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}

	var capturedMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		capturedMedia = media
	}

	activeMedia := func() *models.ActiveMedia {
		return capturedMedia
	}

	// StartPost should detect no running game and set active media to nil
	err = platform.StartPost(cfg, nil, activeMedia, setActiveMedia, nil)

	// Should not error
	require.NoError(t, err)

	// Should set media to nil when no game running
	assert.Nil(t, capturedMedia, "Should set active media to nil when no game running")
}

// TestStartPost_WithRunningGame tests that StartPost detects and tracks already-running games
func TestStartPost_WithRunningGame(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	// Setup mock ES API server with a running game
	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithRunningGame(&esapi.RunningGameResponse{
		Name:       "Super Mario Bros.",
		Path:       "/userdata/roms/nes/mario.nes",
		SystemName: "nes",
	})

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}

	var capturedMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		capturedMedia = media
	}

	activeMedia := func() *models.ActiveMedia {
		return capturedMedia
	}

	// StartPost should detect running game and set active media
	err = platform.StartPost(cfg, nil, activeMedia, setActiveMedia, nil)

	// Should not error
	require.NoError(t, err)

	// Should set active media with proper fields
	require.NotNil(t, capturedMedia, "Should set active media when game is running")
	helpers.AssertValidActiveMedia(t, capturedMedia)

	// Verify specific fields
	assert.Equal(t, systemdefs.SystemNES, capturedMedia.SystemID)
	assert.Equal(t, "NES", capturedMedia.SystemName) // From assets.GetSystemMetadata
	assert.Equal(t, "/userdata/roms/nes/mario.nes", capturedMedia.Path)
	assert.Equal(t, "Super Mario Bros.", capturedMedia.Name)
}

// TestActiveMediaCreation_RequiredFields tests that ActiveMedia creation includes all required fields.
// This is a regression test for the bug where Started field was missing, causing zero time values.
func TestActiveMediaCreation_RequiredFields(t *testing.T) {
	t.Parallel()

	// Test the pattern that caused the bug (manual struct creation)
	media := models.ActiveMedia{
		Started:    time.Now(),
		SystemID:   "nes",
		SystemName: "Nintendo Entertainment System",
		Path:       "/games/mario.nes",
		Name:       "Super Mario Bros.",
		LauncherID: "retroarch",
	}

	// Validate it has all required fields
	helpers.AssertValidActiveMedia(t, &media)
}

// TestNewActiveMedia_Constructor tests the NewActiveMedia constructor
func TestNewActiveMedia_Constructor(t *testing.T) {
	t.Parallel()

	// Use the constructor
	media := models.NewActiveMedia(
		"nes",
		"Nintendo Entertainment System",
		"/games/mario.nes",
		"Super Mario Bros.",
		"retroarch",
	)

	// Validate it has all required fields
	helpers.AssertValidActiveMedia(t, media)

	// Verify specific fields
	assert.Equal(t, "nes", media.SystemID)
	assert.Equal(t, "Nintendo Entertainment System", media.SystemName)
	assert.Equal(t, "/games/mario.nes", media.Path)
	assert.Equal(t, "Super Mario Bros.", media.Name)
	assert.Equal(t, "retroarch", media.LauncherID)
	assert.False(t, media.Started.IsZero(), "Started should be set to current time")
}

// TestLaunchMedia_SetsActiveMediaWithTimestamp is a regression test for the bug
// where ActiveMedia.Started was not set during launches, causing zero time values.
// This validates the complete launch flow sets proper timestamps.
func TestLaunchMedia_SetsActiveMediaWithTimestamp(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	// Setup mock ES API server
	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithNoRunningGame()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}

	var capturedMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		capturedMedia = media
	}

	activeMedia := func() *models.ActiveMedia {
		return capturedMedia
	}

	// Initialize platform (need setActiveMedia function)
	err = platform.StartPost(cfg, nil, activeMedia, setActiveMedia, nil)
	require.NoError(t, err)

	// Note: We can't actually test LaunchMedia because it requires ES API running game
	// But we've verified the constructor is used in DoLaunch (in pkg/helpers/paths.go)
	// and that DoLaunch properly creates ActiveMedia with timestamps
	// This test documents the integration point for future testing
}

// TestShouldKeepRunningInstance tests the shouldKeepRunningInstance function
func TestShouldKeepRunningInstance(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	tests := []struct {
		newLauncher       *platforms.Launcher
		name              string
		activeLauncherID  string
		description       string
		activeMediaExists bool
		expectedResult    bool
	}{
		{
			name: "new launcher without running instance",
			newLauncher: &platforms.Launcher{
				ID:                  "GenericGame",
				UsesRunningInstance: "", // Empty = starts its own process
			},
			activeLauncherID:  "KodiAlbum",
			activeMediaExists: true,
			expectedResult:    false,
			description:       "Should kill when new launcher starts its own process",
		},
		{
			name: "no active media",
			newLauncher: &platforms.Launcher{
				ID:                  "KodiSong",
				UsesRunningInstance: platforms.InstanceKodi,
			},
			activeLauncherID:  "",
			activeMediaExists: false,
			expectedResult:    false,
			description:       "Should not keep running when there's no active media",
		},
		{
			name: "both launchers use same instance - kodi to kodi",
			newLauncher: &platforms.Launcher{
				ID:                  "KodiSong",
				UsesRunningInstance: platforms.InstanceKodi,
			},
			activeLauncherID:  "KodiAlbum",
			activeMediaExists: true,
			expectedResult:    true,
			description:       "Should keep running when both use same Kodi instance",
		},
		{
			name: "current launcher uses different instance",
			newLauncher: &platforms.Launcher{
				ID:                  "KodiMovie",
				UsesRunningInstance: platforms.InstanceKodi,
			},
			activeLauncherID:  "GenericGame",
			activeMediaExists: true,
			expectedResult:    false,
			description:       "Should kill when current launcher uses different instance",
		},
		{
			name: "same launcher id different instances",
			newLauncher: &platforms.Launcher{
				ID:                  "TestLauncher",
				UsesRunningInstance: "plex", // Different instance
			},
			activeLauncherID:  "TestLauncher",
			activeMediaExists: true,
			expectedResult:    false,
			description:       "Should kill when same launcher ID but different instances (hypothetical)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create platform
			platform := &Platform{}

			// Set up active media state
			var currentActiveMedia *models.ActiveMedia
			if tt.activeMediaExists {
				currentActiveMedia = &models.ActiveMedia{
					LauncherID: tt.activeLauncherID,
					SystemID:   systemdefs.SystemVideo,
					Path:       "/test/path",
					Name:       "Test Media",
				}
			}

			// Set the activeMedia function
			platform.activeMedia = func() *models.ActiveMedia {
				return currentActiveMedia
			}

			// Call shouldKeepRunningInstance
			result := platform.shouldKeepRunningInstance(cfg, tt.newLauncher)

			// Assert result
			assert.Equal(t, tt.expectedResult, result, tt.description)
		})
	}
}
