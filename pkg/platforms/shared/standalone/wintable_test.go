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

package standalone

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWindowsEmulatorTable(t *testing.T) {
	t.Parallel()

	ids := make(map[string]string)
	names := make(map[string]bool)
	for _, emu := range WindowsEmulators() {
		assert.NotEmpty(t, emu.Name)
		assert.False(t, names[emu.Name], "duplicate emulator %s", emu.Name)
		names[emu.Name] = true
		require.NotEmpty(t, emu.Exes, emu.Name)
		for _, exe := range emu.Exes {
			assert.True(t, strings.HasSuffix(strings.ToLower(exe), ".exe") || strings.HasSuffix(exe, "*"),
				"%s executable %q", emu.Name, exe)
		}
		for _, hint := range emu.DirHints {
			assert.Equal(t, strings.ToLower(hint), hint, "%s hint must be lower case", emu.Name)
		}
		require.NotEmpty(t, emu.Systems, emu.Name)
		for i := range emu.Systems {
			system := &emu.Systems[i]
			key := strings.ToLower(system.ID)
			assert.Empty(t, ids[key], "%s reuses launcher ID %s from %s", emu.Name, system.ID, ids[key])
			ids[key] = emu.Name
			assert.False(t, strings.HasPrefix(system.ID, "RetroArch"), "%s collides with RetroArch IDs", system.ID)
			assert.NotNil(t, system.Args, system.ID)
			_, err := systemdefs.GetSystem(system.SystemID)
			require.NoError(t, err, system.ID)
			if !system.NoScan {
				assert.NotEmpty(t, LauncherExtensions(system), "%s has nothing to index", system.ID)
				assert.NotEmpty(t, system.Folders, system.ID)
			}
		}
	}
}

func TestLauncherFoldersAddSystemIDOnce(t *testing.T) {
	t.Parallel()

	system := &WindowsSystem{SystemID: systemdefs.SystemArcade, Folders: []string{"arcade", "mame", "MAME"}}
	assert.Equal(t, []string{"arcade", "mame"}, LauncherFolders(system))

	system = &WindowsSystem{SystemID: systemdefs.SystemGenesis, Folders: []string{"megadrive", "genesis"}}
	assert.Equal(t, []string{"megadrive", "genesis"}, LauncherFolders(system))

	system = &WindowsSystem{SystemID: systemdefs.SystemNintendo64, Folders: []string{"n64"}}
	assert.Equal(t, []string{"n64", systemdefs.SystemNintendo64}, LauncherFolders(system))
}

func TestLauncherExtensions(t *testing.T) {
	t.Parallel()

	explicit := &WindowsSystem{Folders: []string{"snes"}, Extensions: []string{".sfc"}}
	assert.Equal(t, []string{".sfc"}, LauncherExtensions(explicit))

	extra := &WindowsSystem{Folders: []string{"gb", "gbc"}, ExtraExtensions: []string{".zzz", ".gb"}}
	extensions := LauncherExtensions(extra)
	assert.Contains(t, extensions, ".gb")
	assert.Contains(t, extensions, ".gbc")
	assert.Contains(t, extensions, ".zzz")
	seen := make(map[string]bool)
	for _, ext := range extensions {
		assert.False(t, seen[strings.ToLower(ext)], "duplicate %s", ext)
		seen[strings.ToLower(ext)] = true
	}
}

func findWindowsSystem(t *testing.T, id string) (*WindowsEmulator, *WindowsSystem) {
	t.Helper()
	emulators := WindowsEmulators()
	for i := range emulators {
		for j := range emulators[i].Systems {
			if emulators[i].Systems[j].ID == id {
				return &emulators[i], &emulators[i].Systems[j]
			}
		}
	}
	t.Fatalf("no launcher %s", id)
	return nil, nil
}

func TestBuildWindowsCommand(t *testing.T) {
	t.Parallel()

	install := t.TempDir()
	game := writeFile(t, filepath.Join(t.TempDir(), "Arcade", "1941.zip"), "zip")
	emu, system := findWindowsSystem(t, "MAME")
	installInfo := WindowsInstall{Exe: filepath.Join(install, "mame.exe"), Dir: install}
	var asked []*platforms.LaunchCommand
	showConfig := func(command *platforms.LaunchCommand) ([]byte, error) {
		asked = append(asked, command)
		return []byte("rompath bios\n"), nil
	}

	command, err := BuildWindowsCommand(emu, system, installInfo, game, showConfig)
	require.NoError(t, err)
	assert.Equal(t, []*platforms.LaunchCommand{{
		Executable: installInfo.Exe, Args: []string{"-showconfig"}, Dir: install,
	}}, asked, "MAME is asked for its settings from its own folder")
	assert.Equal(t, &platforms.LaunchCommand{
		Executable: installInfo.Exe,
		Args:       []string{"-rompath", filepath.Dir(game) + ";bios", "1941"},
		Dir:        install,
	}, command)

	_, err = BuildWindowsCommand(emu, system, installInfo, filepath.Join(t.TempDir(), "missing.zip"), showConfig)
	require.ErrorContains(t, err, "stat media path")

	emu, system = findWindowsSystem(t, "DuckStation")
	game = writeFile(t, filepath.Join(t.TempDir(), "PSX", "game.chd"), "chd")
	command, err = BuildWindowsCommand(emu, system, installInfo, game, showConfig)
	require.NoError(t, err)
	assert.Len(t, asked, 1, "only MAME builds are asked for their settings")
	assert.Empty(t, command.Dir, "only emulators that need it run from their folder")
	assert.Equal(t, game, command.Args[len(command.Args)-1])
}

func TestWindowsLaunchersOnlyForInstalledEmulators(t *testing.T) {
	t.Parallel()

	install := t.TempDir()
	exe := writeFile(t, filepath.Join(install, "Dolphin.exe"), "MZ")
	emulators := WindowsEmulators()
	installs := map[string]WindowsInstall{"Dolphin": {Exe: exe, Dir: install}}
	prepared := 0
	launchers := WindowsLaunchers(emulators, installs,
		func(id string) bool { return id == "DolphinWii" },
		func(*exec.Cmd) { prepared++ })

	require.Len(t, launchers, 1)
	launcher := launchers[0]
	assert.Equal(t, "DolphinGameCube", launcher.ID)
	assert.Equal(t, systemdefs.SystemGameCube, launcher.SystemID)
	assert.Equal(t, platforms.LifecycleBlocking, launcher.Lifecycle)
	assert.Contains(t, launcher.Folders, "gc")
	require.NoError(t, launcher.Availability(nil))

	missing := NewWindowsLauncher(&emulators[0], &emulators[0].Systems[0],
		WindowsInstall{Exe: filepath.Join(install, "gone.exe"), Dir: install}, nil)
	require.ErrorContains(t, missing.Availability(nil), "no longer at")
}

func TestLaunchersSkipFormatsTheEmulatorCannotOpen(t *testing.T) {
	t.Parallel()

	for _, emu := range WindowsEmulators() {
		if emu.Name != "Mednafen" {
			continue
		}
		for i := range emu.Systems {
			extensions := LauncherExtensions(&emu.Systems[i])
			assert.NotContains(t, extensions, ".chd", emu.Systems[i].ID)
			assert.NotContains(t, extensions, ".7z", emu.Systems[i].ID)
		}
	}
	_, system := findWindowsSystem(t, "KegaFusionMegaCD")
	assert.NotContains(t, LauncherExtensions(system), ".chd")
	_, system = findWindowsSystem(t, "MednafenPSX")
	assert.Equal(t, []string{".cue", ".ccd", ".toc", ".m3u"}, LauncherExtensions(system))
}

func TestJgenesisOpensFullscreenWithoutChangingSettings(t *testing.T) {
	t.Parallel()

	_, system := findWindowsSystem(t, "JgenesisGenesis")
	path := filepath.Join(string(filepath.Separator), "roms", "genesis", "sonic.md")
	args, err := system.Args(&Context{Path: path})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"--no-gui", "--config-override", "common.launch_in_fullscreen=true", "--file-path", path,
	}, args)
}
