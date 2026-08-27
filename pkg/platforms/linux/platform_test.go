//go:build linux

package linux

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxemu"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettings_AllowsZapScriptAlongsideTUI(t *testing.T) {
	t.Parallel()
	assert.False(t, (&Platform{}).Settings().DisableZapScriptInTUI)
}

func TestPlatformStartPreDoesNotRequireEmulators(t *testing.T) {
	t.Parallel()

	assert.NoError(t, NewPlatform().StartPre(nil))
}

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
			assert.Equal(t, systemdefs.SystemMusicTrack, launcher.SystemID)
			assert.Contains(t, launcher.Extensions, ".mp3")
			break
		}
	}

	require.NotNil(t, kodiMusic, "KodiMusic launcher should exist")
}

func TestLinuxHasRetroArchLaunchers(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	options := linuxemu.NewOptions(t.TempDir(), linuxRetroArchOptions())
	options.IncludeStandalone = false
	options.IncludeProviderDecks = false
	options.IsFlatpakInstalled = func(id string) bool { return id == linuxemu.RetroArchFlatpakID }
	launchers := (&Platform{emulationOptionsOverride: &options}).Launchers(cfg)
	for _, launcher := range launchers {
		if launcher.ID != "RetroArchSNES9x" {
			continue
		}
		assert.Equal(t, systemdefs.SystemSNES, launcher.SystemID)
		assert.Equal(t, []string{"snes"}, launcher.Folders)
		assert.NotEmpty(t, launcher.Controls)
		return
	}
	t.Fatal("RetroArchSNES9x launcher should exist")
}

func TestLinuxSuppressesDuplicateSharedRetroArch(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)
	options := linuxemu.NewOptions(t.TempDir(), sharedretroarch.Options{Exec: []string{"flatpak"}})
	options.IncludeStandalone = false
	options.IncludeProviderDecks = false
	options.IsFlatpakInstalled = func(id string) bool { return id == linuxemu.RetroArchFlatpakID }
	platform := &Platform{emulationOptionsOverride: &options}

	count := 0
	for _, launcher := range platform.Launchers(cfg) {
		if launcher.ID == "RetroArchSNES9x" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestLinuxHasOptionalGameManagerLaunchers(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)
	launchers := make(map[string]platforms.Launcher)
	for _, launcher := range (&Platform{}).Launchers(cfg) {
		launchers[launcher.ID] = launcher
	}
	for _, id := range []string{"Bottles", "Faugus", "Moonlight"} {
		assert.Contains(t, launchers, id, "%s launcher should exist", id)
	}
	assert.Equal(t, platforms.LifecycleExternal, launchers["Steam"].Lifecycle)
	assert.Equal(t, platforms.LifecycleBlocking, launchers["Bottles"].Lifecycle)
	assert.Nil(t, launchers["Bottles"].Kill)
	assert.Equal(t, platforms.LifecycleBlocking, launchers["Faugus"].Lifecycle)
	assert.Equal(t, platforms.LifecycleBlocking, launchers["Moonlight"].Lifecycle)
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
