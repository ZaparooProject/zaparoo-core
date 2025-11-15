//go:build linux

package linux

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinuxHasKodiLocalLauncher(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}
	launchers := platform.Launchers(cfg)

	// Check for KodiLocal launcher
	var kodiLocal *string
	for _, launcher := range launchers {
		if launcher.ID == "KodiLocalVideo" {
			kodiLocal = &launcher.ID
			assert.Equal(t, systemdefs.SystemVideo, launcher.SystemID)
			assert.Contains(t, launcher.Extensions, ".mp4")
			break
		}
	}

	require.NotNil(t, kodiLocal, "KodiLocal launcher should exist")
}

func TestLinuxHasKodiMovieLauncher(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}
	launchers := platform.Launchers(cfg)

	// Check for KodiMovie launcher
	var kodiMovie *string
	for _, launcher := range launchers {
		if launcher.ID == "KodiMovie" {
			kodiMovie = &launcher.ID
			assert.Equal(t, systemdefs.SystemMovie, launcher.SystemID)
			assert.Contains(t, launcher.Schemes, shared.SchemeKodiMovie)
			break
		}
	}

	require.NotNil(t, kodiMovie, "KodiMovie launcher should exist")
}

func TestLinuxHasKodiTVLauncher(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}
	launchers := platform.Launchers(cfg)

	// Check for KodiTVEpisode launcher
	var kodiTV *string
	for _, launcher := range launchers {
		if launcher.ID == "KodiTVEpisode" {
			kodiTV = &launcher.ID
			assert.Equal(t, systemdefs.SystemTVEpisode, launcher.SystemID)
			assert.Contains(t, launcher.Schemes, shared.SchemeKodiEpisode)
			break
		}
	}

	require.NotNil(t, kodiTV, "KodiTV launcher should exist")
}

func TestLinuxHasKodiMusicLauncher(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}
	launchers := platform.Launchers(cfg)

	// Check for KodiMusic launcher
	var kodiMusic *string
	for _, launcher := range launchers {
		if launcher.ID == "KodiLocalAudio" {
			kodiMusic = &launcher.ID
			assert.Equal(t, systemdefs.SystemMusic, launcher.SystemID)
			assert.Contains(t, launcher.Extensions, ".mp3")
			break
		}
	}

	require.NotNil(t, kodiMusic, "KodiMusic launcher should exist")
}

func TestLinuxHasAllKodiCollectionLaunchers(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}
	launchers := platform.Launchers(cfg)

	// Build launcher map for quick lookup
	launcherMap := make(map[string]bool)
	for _, launcher := range launchers {
		launcherMap[launcher.ID] = true
	}

	// Test all remaining collection launchers exist
	expectedLaunchers := []string{"KodiSong", "KodiAlbum", "KodiArtist", "KodiTVShow"}
	for _, expected := range expectedLaunchers {
		assert.True(t, launcherMap[expected], "%s launcher should exist", expected)
	}
}
