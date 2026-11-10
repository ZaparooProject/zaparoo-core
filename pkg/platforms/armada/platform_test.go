//go:build linux && !android

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

package armada

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxemu"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConfig(t *testing.T) *config.Instance {
	t.Helper()
	cfg, err := helpers.NewTestConfig(helpers.NewOSFS(), filepath.Join(t.TempDir(), "config"))
	require.NoError(t, err)
	return cfg
}

func launcherIDs(ls []platforms.Launcher) map[string]bool {
	ids := make(map[string]bool, len(ls))
	for i := range ls {
		ids[ls[i].ID] = true
	}
	return ids
}

// isolatedEmulationOptions keeps launcher discovery away from the host's
// real home directory, Flatpaks and PATH.
func isolatedEmulationOptions(home string) linuxemu.Options {
	options := linuxemu.NewOptions(home, sharedretroarch.Options{})
	options.IsFlatpakInstalled = func(string) bool { return false }
	options.LookPath = func(string) (string, error) { return "", errors.New("missing") }
	return options
}

func TestNewPlatform(t *testing.T) {
	t.Parallel()

	p := NewPlatform()

	assert.NotNil(t, p)
	assert.NotNil(t, p.Base)
	assert.True(t, p.gameMode.Enabled())
	assert.Equal(t, platformids.Armada, p.ID())
}

func TestPlatformSettings(t *testing.T) {
	t.Parallel()

	settings := NewPlatform().Settings()

	assert.NotEmpty(t, settings.DataDir)
	assert.NotEmpty(t, settings.ConfigDir)
	assert.NotEmpty(t, settings.TempDir)
	assert.NotEmpty(t, settings.LogDir)
}

func TestPlatformStartPreDoesNotRequireEmulators(t *testing.T) {
	t.Parallel()

	assert.NoError(t, NewPlatform().StartPre(nil))
}

func TestPlatformSupportedReaders(t *testing.T) {
	t.Parallel()

	assert.NotNil(t, NewPlatform().SupportedReaders(newTestConfig(t)))
}

func TestPlatformRootDirs(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	p := NewPlatform()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(home, "ROMs")}, p.RootDirs(cfg))

	root := t.TempDir()
	require.NoError(t, cfg.LoadTOML(fmt.Sprintf("[launchers]\nindex_root = [%q]\n", root)))
	assert.Equal(t, []string{root}, p.RootDirs(cfg))
}

func TestSteamOptionsTargetNativeArmClient(t *testing.T) {
	t.Parallel()

	opts := steamOptions()
	assert.Equal(t, "/var/home/armada/.local/share/Steam", opts.FallbackPath)
	assert.False(t, opts.UseXdgOpen, "Armada's steam wrapper forwards URLs to the running client")
	assert.False(t, opts.CheckFlatpak, "Flathub Steam has no aarch64 build")
}

func TestPlatformLaunchers(t *testing.T) {
	t.Parallel()

	p := NewPlatform()
	options := isolatedEmulationOptions(t.TempDir())
	p.emulationOptionsOverride = &options
	launchers := p.Launchers(newTestConfig(t))
	ids := launcherIDs(launchers)

	for _, id := range []string{"Steam", "Heroic", "Moonlight", "WebBrowser", "Generic"} {
		assert.True(t, ids[id], "missing %s launcher", id)
	}
	for _, id := range []string{"Lutris", "Bottles", "Faugus", "KodiLocalVideo"} {
		assert.False(t, ids[id], "%s has no aarch64 build and must not be offered", id)
	}

	for i := range launchers {
		switch launchers[i].ID {
		case "Steam":
			assert.Equal(t, platforms.LifecycleExternal, launchers[i].Lifecycle)
		case "Generic":
			assert.NotNil(t, launchers[i].Kill, "non-Steam launchers should restore Gaming Mode focus")
		}
	}
}

func TestPlatformLaunchersIncludeSharedRetroArchOnce(t *testing.T) {
	t.Parallel()

	options := linuxemu.NewOptions(t.TempDir(), sharedretroarch.Options{Exec: []string{"flatpak"}})
	options.IncludeStandalone = false
	options.IncludeProviderDecks = false
	options.IsFlatpakInstalled = func(id string) bool { return id == linuxemu.RetroArchFlatpakID }
	p := NewPlatform()
	p.emulationOptionsOverride = &options

	count := 0
	for _, launcher := range p.Launchers(newTestConfig(t)) {
		if launcher.ID == "RetroArchSNES9x" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestPlatformCustomLauncherSuppressesSharedDuplicate(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	require.NoError(t, cfg.LoadTOML(`
[[launchers.custom]]
id = "PCSX2"
system = "PS2"
execute = "echo [[media_path]]"
`))
	options := isolatedEmulationOptions(t.TempDir())
	options.IncludeProviderDecks = false
	options.IsFlatpakInstalled = func(id string) bool { return id == "net.pcsx2.PCSX2" }
	p := NewPlatform()
	p.emulationOptionsOverride = &options

	count := 0
	for _, launcher := range p.Launchers(cfg) {
		if launcher.ID == "PCSX2" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

// Armada's app store saves aarch64 AppImages to ~/Applications under these
// fixed names.
func TestPlatformDiscoversArmadaStoreAppImages(t *testing.T) {
	t.Parallel()

	appImages := map[string]string{
		"Cemu.AppImage":            "Cemu",
		"DuckStation.AppImage":     "DuckStation",
		"rpcs3.AppImage":           "RPCS3",
		"Vita3K.AppImage":          "Vita3K",
		"melonDS.AppImage":         "MelonDS",
		"Azahar-Enhanced.AppImage": "Azahar",
		"xemu.AppImage":            "Xemu",
	}
	mediaNames := map[string]string{"Vita3K": "PCSE00000.psvita"}
	home := t.TempDir()
	applications := filepath.Join(home, "Applications")
	require.NoError(t, os.MkdirAll(applications, 0o750))
	for name := range appImages {
		path := filepath.Join(applications, name)
		require.NoError(t, os.WriteFile(path, []byte("appimage"), 0o600))
		require.NoError(t, os.Chmod(path, 0o700)) //nolint:gosec // Test AppImage must be executable.
	}
	options := isolatedEmulationOptions(home)
	options.IncludeRetroArch = false
	options.IncludeProviderDecks = false
	p := NewPlatform()
	p.emulationOptionsOverride = &options

	launchers := p.Launchers(newTestConfig(t))
	for name, id := range appImages {
		idx := slices.IndexFunc(launchers, func(l platforms.Launcher) bool { return l.ID == id })
		require.GreaterOrEqual(t, idx, 0, "%s not discovered from %s", id, name)
		mediaName := mediaNames[id]
		if mediaName == "" {
			mediaName = "game-" + id
		}
		mediaPath := filepath.Join(home, mediaName)
		require.NoError(t, os.WriteFile(mediaPath, []byte("rom"), 0o600))
		command, err := launchers[idx].BuildLaunchCommand(nil, mediaPath, nil)
		require.NoError(t, err, id)
		assert.Equal(t, filepath.Join(applications, name), command.Executable)
	}
}

func TestPlatformDiscoversEmuDeck(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	paths := linuxemu.DefaultEmuDeckPaths(home)
	require.NoError(t, os.MkdirAll(filepath.Join(paths.RomsPath, "gc"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(paths.GamelistPath, "gc"), 0o750))
	wrapper := filepath.Join(paths.ToolsPath, "launchers", "dolphin-emu.sh")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o750))
	require.NoError(t, os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o600))
	require.NoError(t, os.Chmod(wrapper, 0o700)) //nolint:gosec // Test wrapper must be runnable.

	options := isolatedEmulationOptions(home)
	options.IncludeRetroArch = false
	options.IncludeStandalone = false
	p := NewPlatform()
	p.emulationOptionsOverride = &options

	assert.True(t, launcherIDs(p.Launchers(newTestConfig(t)))["EmuDeckGameCube"])
}

func TestPlatformReturnToMenuWithoutActiveMedia(t *testing.T) {
	t.Parallel()

	assert.NoError(t, NewPlatform().ReturnToMenu())
}

func TestPlatformSteamLaunchEnvUsesSessionDirs(t *testing.T) {
	t.Parallel()

	// The value can come from the host's systemd user manager, so only the
	// presence of the session variable is checked.
	env := NewPlatform().steamLaunchEnv()
	hasConfigHome := slices.ContainsFunc(env, func(entry string) bool {
		return strings.HasPrefix(entry, "XDG_CONFIG_HOME=")
	})
	assert.True(t, hasConfigHome, "Steam launches should carry the session environment: %v", env)
}
