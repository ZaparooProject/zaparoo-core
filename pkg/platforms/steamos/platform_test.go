//go:build linux

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package steamos

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/steamos/steamruntime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRuntimeBroker struct {
	started    *steamruntime.Command
	available  bool
	owns       bool
	clear      bool
	clearedPID int
}

func (f *fakeRuntimeBroker) Start(_ context.Context, command *steamruntime.Command) (*os.Process, error) {
	f.started = command
	return &os.Process{Pid: 1}, nil
}

func (*fakeRuntimeBroker) Stop(context.Context) error      { return nil }
func (*fakeRuntimeBroker) Wait(context.Context, int) error { return nil }
func (f *fakeRuntimeBroker) Available() bool               { return f.available }
func (*fakeRuntimeBroker) HasActive() bool                 { return false }
func (f *fakeRuntimeBroker) Owns(int) bool                 { return f.owns }
func (f *fakeRuntimeBroker) Clear(pid int) bool {
	f.clearedPID = pid
	return f.clear
}

func TestParseSteamOSSessionEnv(t *testing.T) {
	t.Parallel()

	result := parseSteamOSSessionEnv("PATH=/usr/bin\nDISPLAY=:0\nWAYLAND_DISPLAY=wayland-0\n" +
		"XDG_CURRENT_DESKTOP=KDE\nINVALID\n")

	assert.Equal(t, []string{
		"DISPLAY=:0",
		"WAYLAND_DISPLAY=wayland-0",
		"XDG_CURRENT_DESKTOP=KDE",
	}, result)
}

func TestSteamRuntimeEnvOverridesPreserveOnlyCommandSpecificValues(t *testing.T) {
	t.Parallel()

	result := steamRuntimeEnvOverrides([]string{
		"DISPLAY=:0",
		"WAYLAND_DISPLAY=wayland-0",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus",
		"HOME=/tmp/emulator-home",
		"LD_LIBRARY_PATH=/opt/emulator/lib",
		"FLAG_WITHOUT_VALUE",
	})

	assert.Equal(t, []string{
		"HOME=/tmp/emulator-home",
		"LD_LIBRARY_PATH=/opt/emulator/lib",
	}, result)
}

func TestWrapSteamRuntimeDelegatesLaunchCommand(t *testing.T) {
	t.Parallel()

	broker := &fakeRuntimeBroker{available: true}
	platform := NewPlatform()
	platform.steamRuntime = broker
	launcher := platforms.Launcher{
		Kill: func(*config.Instance) error { return nil },
		BuildLaunchCommand: func(
			*config.Instance,
			string,
			*platforms.LaunchOptions,
		) (*platforms.LaunchCommand, error) {
			return &platforms.LaunchCommand{
				Executable: "/usr/bin/emulator",
				Dir:        "/tmp",
				Args:       []string{"--fullscreen", "game.rom"},
				Env:        []string{"DISPLAY=:0", "EMULATOR_OPTION=1"},
			}, nil
		},
	}

	platform.wrapSteamRuntime(&launcher)
	process, err := launcher.Launch(nil, "game.rom", nil)

	require.NoError(t, err)
	assert.Equal(t, 1, process.Pid)
	assert.Equal(t, platforms.LifecycleBlocking, launcher.Lifecycle)
	assert.NotNil(t, launcher.Kill)
	require.NotNil(t, broker.started)
	assert.Equal(t, "/usr/bin/emulator", broker.started.Executable)
	assert.Equal(t, "/tmp", broker.started.Dir)
	assert.Equal(t, []string{"--fullscreen", "game.rom"}, broker.started.Args)
	assert.Equal(t, []string{"EMULATOR_OPTION=1"}, broker.started.Env)
}

func TestWrapSteamRuntimeFallsBackWhenIntegrationRemoved(t *testing.T) {
	t.Parallel()

	broker := &fakeRuntimeBroker{available: true}
	platform := NewPlatform()
	platform.steamRuntime = broker
	directLaunches := 0
	killCalled := false
	launcher := platforms.Launcher{
		ID:        "TestEmulator",
		Lifecycle: platforms.LifecycleTracked,
		Kill: func(*config.Instance) error {
			killCalled = true
			return nil
		},
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			directLaunches++
			return &os.Process{Pid: 2}, nil
		},
		BuildLaunchCommand: func(
			*config.Instance,
			string,
			*platforms.LaunchOptions,
		) (*platforms.LaunchCommand, error) {
			return &platforms.LaunchCommand{Executable: "/usr/bin/emulator"}, nil
		},
	}

	platform.wrapSteamRuntime(&launcher)
	broker.available = false
	process, err := launcher.Launch(nil, "game.rom", nil)

	require.NoError(t, err)
	assert.Equal(t, 2, process.Pid)
	assert.Equal(t, 1, directLaunches)
	assert.Nil(t, broker.started)
	assert.Equal(t, platforms.LifecycleBlocking, launcher.Lifecycle)
	require.NotNil(t, launcher.Kill)
	require.NoError(t, launcher.Kill(nil))
	assert.True(t, killCalled)
}

func TestClearTrackedProcessMediaClearsRuntimeActiveMedia(t *testing.T) {
	t.Parallel()

	broker := &fakeRuntimeBroker{owns: true, clear: true}
	active := &models.ActiveMedia{Path: "test"}
	platform := NewPlatform()
	platform.steamRuntime = broker
	platform.setActiveMedia = func(media *models.ActiveMedia) { active = media }
	process := &os.Process{Pid: 42}

	assert.True(t, platform.ClearTrackedProcessMedia(process))
	assert.Equal(t, 42, broker.clearedPID)
	assert.Nil(t, active)
}

func TestClearTrackedProcessMediaPreservesReplacementRuntimeMedia(t *testing.T) {
	t.Parallel()

	broker := &fakeRuntimeBroker{owns: true, clear: false}
	active := &models.ActiveMedia{Path: "replacement"}
	platform := NewPlatform()
	platform.steamRuntime = broker
	platform.setActiveMedia = func(media *models.ActiveMedia) { active = media }

	assert.False(t, platform.ClearTrackedProcessMedia(&os.Process{Pid: 42}))
	assert.Equal(t, "replacement", active.Path)
}

func TestNewPlatform(t *testing.T) {
	t.Parallel()

	p := NewPlatform()

	assert.NotNil(t, p)
	assert.NotNil(t, p.Base)
	assert.Equal(t, platformids.SteamOS, p.ID())
}

func TestPlatformID(t *testing.T) {
	t.Parallel()

	p := NewPlatform()

	assert.Equal(t, platformids.SteamOS, p.ID())
}

func TestPlatformSettings(t *testing.T) {
	t.Parallel()

	p := NewPlatform()
	settings := p.Settings()

	// Settings should be XDG-based
	assert.NotEmpty(t, settings.DataDir)
	assert.NotEmpty(t, settings.ConfigDir)
	assert.NotEmpty(t, settings.TempDir)
	assert.NotEmpty(t, settings.LogDir)
}

func TestPlatformSupportedReaders(t *testing.T) {
	t.Parallel()

	// Setup temporary directory for config
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fsHelper := helpers.NewOSFS()
	cfg, err := helpers.NewTestConfig(fsHelper, configDir)
	require.NoError(t, err)

	p := NewPlatform()
	readers := p.SupportedReaders(cfg)

	// Should return a list (possibly empty depending on config)
	assert.NotNil(t, readers)
}

func TestPlatformLaunchers(t *testing.T) {
	t.Parallel()

	// Setup temporary directory for config
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fsHelper := helpers.NewOSFS()
	cfg, err := helpers.NewTestConfig(fsHelper, configDir)
	require.NoError(t, err)

	p := NewPlatform()
	launchers := p.Launchers(cfg)

	// SteamOS should have Steam and Generic launchers
	assert.GreaterOrEqual(t, len(launchers), 2,
		"SteamOS should have at least 2 launchers")

	// Verify expected launcher IDs are present
	launcherIDs := make(map[string]bool)
	for _, l := range launchers {
		launcherIDs[l.ID] = true
	}

	assert.True(t, launcherIDs["Steam"], "Should have Steam launcher")
	assert.True(t, launcherIDs["Generic"], "Should have Generic launcher")
}

func TestPlatformLaunchersUsesDirectSteam(t *testing.T) {
	t.Parallel()

	// SteamOS uses direct steam command (not xdg-open) for better Game Mode integration
	// We verify Steam launcher exists and is configured for console experience

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fsHelper := helpers.NewOSFS()
	cfg, err := helpers.NewTestConfig(fsHelper, configDir)
	require.NoError(t, err)

	p := NewPlatform()
	launchers := p.Launchers(cfg)

	// Find Steam launcher
	var steamLauncher *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "Steam" {
			steamLauncher = &launchers[i]
			break
		}
	}

	require.NotNil(t, steamLauncher, "Steam launcher should be present")
	// SteamOS Steam launcher exists and supports steam scheme.
	assert.Contains(t, steamLauncher.Schemes, "steam")
	assert.Equal(t, platforms.LifecycleExternal, steamLauncher.Lifecycle)
}

func TestPlatformDoesNotHaveFlatpakLaunchers(t *testing.T) {
	t.Parallel()

	// SteamOS uses native Steam, not Flatpak
	// Verify no Flatpak-specific launchers are present

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fsHelper := helpers.NewOSFS()
	cfg, err := helpers.NewTestConfig(fsHelper, configDir)
	require.NoError(t, err)

	p := NewPlatform()
	launchers := p.Launchers(cfg)

	// SteamOS should NOT have Lutris or Heroic by default
	launcherIDs := make(map[string]bool)
	for _, l := range launchers {
		launcherIDs[l.ID] = true
	}

	// These are not included in default SteamOS launchers
	assert.False(t, launcherIDs["Lutris"], "Should not have Lutris launcher by default")
	assert.False(t, launcherIDs["Heroic"], "Should not have Heroic launcher by default")
}

func TestPlatformSteamDeckPaths(t *testing.T) {
	t.Parallel()

	// Verify Steam launcher is configured with Steam Deck paths
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fsHelper := helpers.NewOSFS()
	cfg, err := helpers.NewTestConfig(fsHelper, configDir)
	require.NoError(t, err)

	p := NewPlatform()
	launchers := p.Launchers(cfg)

	// Find Steam launcher
	var steamLauncher *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "Steam" {
			steamLauncher = &launchers[i]
			break
		}
	}

	require.NotNil(t, steamLauncher, "Steam launcher should be present")
	// The launcher is properly configured - we just verify it exists
	// Internal paths like /home/deck/.steam/steam are set in the launcher options
}

func TestPlatformReturnToMenuStopsActiveMedia(t *testing.T) {
	t.Parallel()

	p := NewPlatform()
	assert.NoError(t, p.ReturnToMenu())
}
