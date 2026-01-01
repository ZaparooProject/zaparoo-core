//go:build linux

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

package steamos

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultEmuDeckPaths tests the default path generation for EmuDeck.
func TestDefaultEmuDeckPaths(t *testing.T) {
	t.Parallel()

	paths := DefaultEmuDeckPaths()

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(homeDir, "Emulation", "roms"), paths.RomsPath)
	assert.Equal(t, filepath.Join(homeDir, "ES-DE", "gamelists"), paths.GamelistPath)
}

// TestDefaultRetroDECKPaths tests the default path generation for RetroDECK.
func TestDefaultRetroDECKPaths(t *testing.T) {
	t.Parallel()

	paths := DefaultRetroDECKPaths()

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(homeDir, "retrodeck", "roms"), paths.RomsPath)
	assert.Equal(t, filepath.Join(homeDir, "retrodeck", "ES-DE", "gamelists"), paths.GamelistPath)
}

// TestRetroDECKFlatpakID tests that the constant is set correctly.
func TestRetroDECKFlatpakID(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "net.retrodeck.retrodeck", RetroDECKFlatpakID)
}

// TestEmulatorTypes tests the emulator type constants.
func TestEmulatorTypes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, EmulatorRetroArch, EmulatorType("retroarch"))
	assert.Equal(t, EmulatorStandalone, EmulatorType("standalone"))
}

// TestEmulatorMapping tests that expected systems are in the emulator mapping.
func TestEmulatorMapping(t *testing.T) {
	t.Parallel()

	// Test RetroArch systems
	retroArchSystems := []string{"nes", "snes", "gb", "gbc", "gba", "n64", "nds", "megadrive", "arcade", "mame"}
	for _, system := range retroArchSystems {
		cfg, ok := emulatorMapping[system]
		assert.True(t, ok, "system %s should be in emulatorMapping", system)
		if ok {
			assert.Equal(t, EmulatorRetroArch, cfg.Type, "system %s should use RetroArch", system)
			assert.NotEmpty(t, cfg.Core, "system %s should have a core defined", system)
			assert.Equal(t, "org.libretro.RetroArch", cfg.FlatpakID)
		}
	}

	// Test standalone systems
	standaloneSystems := []struct {
		system    string
		flatpakID string
	}{
		{"psx", "org.duckstation.DuckStation"},
		{"ps2", "net.pcsx2.PCSX2"},
		{"ps3", "net.rpcs3.RPCS3"},
		{"psp", "org.ppsspp.PPSSPP"},
		{"gamecube", "org.DolphinEmu.dolphin-emu"},
		{"wii", "org.DolphinEmu.dolphin-emu"},
		{"dreamcast", "org.flycast.Flycast"},
		{"scummvm", "org.scummvm.ScummVM"},
	}

	for _, tt := range standaloneSystems {
		cfg, ok := emulatorMapping[tt.system]
		assert.True(t, ok, "system %s should be in emulatorMapping", tt.system)
		if ok {
			assert.Equal(t, EmulatorStandalone, cfg.Type, "system %s should use standalone emulator", tt.system)
			assert.Equal(t, tt.flatpakID, cfg.FlatpakID, "system %s should have correct FlatpakID", tt.system)
		}
	}
}

// TestCreateEmuDeckLauncherTest tests the Test function of EmuDeck launchers.
func TestCreateEmuDeckLauncherTest(t *testing.T) {
	t.Parallel()

	paths := EmuDeckPaths{
		RomsPath:     "/home/testuser/Emulation/roms",
		GamelistPath: "/home/testuser/ES-DE/gamelists",
	}

	systemInfo := esde.SystemInfo{
		SystemID: "nes",
	}

	launcher := createEmuDeckLauncher("nes", systemInfo, paths)

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "valid ROM path within system",
			path:     "/home/testuser/Emulation/roms/nes/super_mario.nes",
			expected: true,
		},
		{
			name:     "valid ROM path in subdirectory",
			path:     "/home/testuser/Emulation/roms/nes/USA/zelda.nes",
			expected: true,
		},
		{
			name:     "path outside system directory",
			path:     "/home/testuser/Emulation/roms/snes/mario_world.sfc",
			expected: false,
		},
		{
			name:     "path with parent directory traversal",
			path:     "/home/testuser/Emulation/roms/nes/../snes/game.sfc",
			expected: false,
		},
		{
			name:     "txt file should be skipped",
			path:     "/home/testuser/Emulation/roms/nes/readme.txt",
			expected: false,
		},
		{
			name:     "directory path (no extension) should be skipped",
			path:     "/home/testuser/Emulation/roms/nes/subdir",
			expected: false,
		},
		{
			name:     "absolute path outside roms",
			path:     "/etc/passwd",
			expected: false,
		},
		{
			name:     "zip archive is valid",
			path:     "/home/testuser/Emulation/roms/nes/game.zip",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := launcher.Test(nil, tt.path)
			assert.Equal(t, tt.expected, result, "path: %s", tt.path)
		})
	}
}

// TestCreateRetroDECKLauncherTest tests the Test function of RetroDECK launchers.
func TestCreateRetroDECKLauncherTest(t *testing.T) {
	t.Parallel()

	paths := RetroDECKPaths{
		RomsPath:     "/home/testuser/retrodeck/roms",
		GamelistPath: "/home/testuser/retrodeck/ES-DE/gamelists",
	}

	systemInfo := esde.SystemInfo{
		SystemID: "snes",
	}

	launcher := createRetroDECKLauncher("snes", systemInfo, paths)

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "valid ROM path within system",
			path:     "/home/testuser/retrodeck/roms/snes/chrono_trigger.sfc",
			expected: true,
		},
		{
			name:     "valid ROM path in subdirectory",
			path:     "/home/testuser/retrodeck/roms/snes/JPN/game.sfc",
			expected: true,
		},
		{
			name:     "path outside system directory",
			path:     "/home/testuser/retrodeck/roms/nes/mario.nes",
			expected: false,
		},
		{
			name:     "path with parent directory traversal",
			path:     "/home/testuser/retrodeck/roms/snes/../nes/game.nes",
			expected: false,
		},
		{
			name:     "txt file should be skipped",
			path:     "/home/testuser/retrodeck/roms/snes/notes.txt",
			expected: false,
		},
		{
			name:     "directory path (no extension) should be skipped",
			path:     "/home/testuser/retrodeck/roms/snes/folder",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := launcher.Test(nil, tt.path)
			assert.Equal(t, tt.expected, result, "path: %s", tt.path)
		})
	}
}

// TestEmuDeckLauncherID tests that EmuDeck launcher IDs are formatted correctly.
func TestEmuDeckLauncherID(t *testing.T) {
	t.Parallel()

	paths := EmuDeckPaths{
		RomsPath:     "/home/testuser/Emulation/roms",
		GamelistPath: "/home/testuser/ES-DE/gamelists",
	}

	systemInfo := esde.SystemInfo{
		SystemID: "nes",
	}

	launcher := createEmuDeckLauncher("nes", systemInfo, paths)

	assert.Equal(t, "nes", launcher.SystemID)
	assert.Contains(t, launcher.ID, "EmuDeck")
}

// TestRetroDECKLauncherID tests that RetroDECK launcher IDs are formatted correctly.
func TestRetroDECKLauncherID(t *testing.T) {
	t.Parallel()

	paths := RetroDECKPaths{
		RomsPath:     "/home/testuser/retrodeck/roms",
		GamelistPath: "/home/testuser/retrodeck/ES-DE/gamelists",
	}

	systemInfo := esde.SystemInfo{
		SystemID: "snes",
	}

	launcher := createRetroDECKLauncher("snes", systemInfo, paths)

	assert.Equal(t, "snes", launcher.SystemID)
	assert.Contains(t, launcher.ID, "RetroDECK")
}

// TestGetRetroArchCoresPath tests the RetroArch cores path function.
func TestGetRetroArchCoresPath(t *testing.T) {
	t.Parallel()

	path := getRetroArchCoresPath()

	// Should contain the expected path components
	assert.Contains(t, path, ".var")
	assert.Contains(t, path, "app")
	assert.Contains(t, path, "org.libretro.RetroArch")
	assert.Contains(t, path, "cores")
}
