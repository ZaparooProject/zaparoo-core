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

package helpers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestPathIsLauncher_AbsoluteFolderNoRootDirs(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "game.iso")

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	// Empty root dirs — the absolute folder path in the launcher should still work
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})

	cfg := &config.Instance{}

	launcher := platforms.Launcher{
		ID:         "CustomPS2",
		SystemID:   "PS2",
		Folders:    []string{tmpDir},
		Extensions: []string{".iso", ".bin", ".chd"},
	}

	assert.True(t, PathIsLauncher(cfg, mockPlatform, &launcher, testFile),
		"file in absolute folder path should match even with empty RootDirs")
}

func TestPathIsLauncher_AbsoluteFolderWithExtensionMatch(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})

	cfg := &config.Instance{}

	launcher := platforms.Launcher{
		ID:         "CustomPS2",
		SystemID:   "PS2",
		Folders:    []string{tmpDir},
		Extensions: []string{".iso", ".bin", ".chd"},
	}

	tests := []struct {
		name    string
		path    string
		matches bool
	}{
		{
			name:    "matching extension .iso",
			path:    filepath.Join(tmpDir, "game.iso"),
			matches: true,
		},
		{
			name:    "matching extension .chd",
			path:    filepath.Join(tmpDir, "game.chd"),
			matches: true,
		},
		{
			name:    "matching extension .bin",
			path:    filepath.Join(tmpDir, "game.bin"),
			matches: true,
		},
		{
			name:    "non-matching extension",
			path:    filepath.Join(tmpDir, "game.txt"),
			matches: false,
		},
		{
			name:    "file outside folder",
			path:    filepath.Join(t.TempDir(), "game.iso"),
			matches: false,
		},
		{
			name:    "subdirectory file matches",
			path:    filepath.Join(tmpDir, "subdir", "game.iso"),
			matches: true,
		},
		{
			name:    "case insensitive extension",
			path:    filepath.Join(tmpDir, "game.ISO"),
			matches: true,
		},
		{
			name:    "dot file is ignored",
			path:    filepath.Join(tmpDir, ".hidden.iso"),
			matches: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := PathIsLauncher(cfg, mockPlatform, &launcher, tt.path)
			assert.Equal(t, tt.matches, result)
		})
	}
}

func TestPathIsLauncher_RelativeFolderWithRootDirs(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{"/roms"})

	cfg := &config.Instance{}

	launcher := platforms.Launcher{
		ID:         "NESLauncher",
		SystemID:   "NES",
		Folders:    []string{"nes"},
		Extensions: []string{".nes"},
	}

	assert.True(t, PathIsLauncher(cfg, mockPlatform, &launcher, "/roms/nes/mario.nes"),
		"file under root+folder should match")
	assert.False(t, PathIsLauncher(cfg, mockPlatform, &launcher, "/other/nes/mario.nes"),
		"file under wrong root should not match")
}

func TestMatchSystemFile_CustomLauncherAbsolutePath(t *testing.T) {
	tmpDir := t.TempDir()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
		{
			ID:         "CustomPS2",
			SystemID:   "PS2",
			Folders:    []string{tmpDir},
			Extensions: []string{".iso", ".bin", ".chd"},
		},
	})

	cfg := &config.Instance{}

	// Swap GlobalLauncherCache for this test
	originalCache := GlobalLauncherCache
	testCache := &LauncherCache{}
	testCache.Initialize(mockPlatform, cfg)
	GlobalLauncherCache = testCache
	defer func() { GlobalLauncherCache = originalCache }()

	assert.True(t, MatchSystemFile(cfg, mockPlatform, "PS2", filepath.Join(tmpDir, "game.iso")),
		"MatchSystemFile should find file via custom launcher with absolute path")
	assert.True(t, MatchSystemFile(cfg, mockPlatform, "PS2", filepath.Join(tmpDir, "game.chd")),
		"MatchSystemFile should match .chd extension")
	assert.False(t, MatchSystemFile(cfg, mockPlatform, "PS2", filepath.Join(tmpDir, "readme.txt")),
		"MatchSystemFile should not match unrelated extension")
	assert.False(t, MatchSystemFile(cfg, mockPlatform, "NES", filepath.Join(tmpDir, "game.iso")),
		"MatchSystemFile should not match wrong system ID")
}

func TestMatchSystemFile_CustomLauncherWithBuiltinLaunchers(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate a platform that has both built-in launchers (with Test functions,
	// relative Folders) and a custom launcher with absolute path
	builtinLauncher := platforms.Launcher{
		ID:         "BuiltinPS2",
		SystemID:   "PS2",
		Folders:    []string{"PS2"},
		Extensions: []string{".iso"},
		Test: func(_ *config.Instance, _ string) bool {
			return false
		},
	}

	customLauncher := platforms.Launcher{
		ID:         "CustomPS2",
		SystemID:   "PS2",
		Folders:    []string{tmpDir},
		Extensions: []string{".iso", ".bin", ".chd"},
	}

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return(
		[]platforms.Launcher{builtinLauncher, customLauncher})

	cfg := &config.Instance{}

	originalCache := GlobalLauncherCache
	testCache := &LauncherCache{}
	testCache.Initialize(mockPlatform, cfg)
	GlobalLauncherCache = testCache
	defer func() { GlobalLauncherCache = originalCache }()

	// File in the custom launcher's absolute path should match PS2
	assert.True(t, MatchSystemFile(cfg, mockPlatform, "PS2", filepath.Join(tmpDir, "game.chd")),
		"custom launcher should match file even when built-in launchers exist")

	// File in the built-in launcher's relative path should also match
	assert.True(t, MatchSystemFile(cfg, mockPlatform, "PS2", "/roms/PS2/game.iso"),
		"built-in launcher should still match via root+folder path")
}

func TestMatchSystemFile_EmptyFoldersLauncherWithTestFunc(t *testing.T) {
	t.Parallel()

	// Launcher with no Folders but a Test function — acts as a generic matcher
	genericLauncher := platforms.Launcher{
		ID:       "GenericLauncher",
		SystemID: "Custom",
		Test: func(_ *config.Instance, p string) bool {
			return p == "/some/specific/file.rom"
		},
	}

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return(
		[]platforms.Launcher{genericLauncher})

	cfg := &config.Instance{}

	originalCache := GlobalLauncherCache
	testCache := &LauncherCache{}
	testCache.Initialize(mockPlatform, cfg)
	GlobalLauncherCache = testCache
	defer func() { GlobalLauncherCache = originalCache }()

	assert.True(t, MatchSystemFile(cfg, mockPlatform, "Custom", "/some/specific/file.rom"))
	assert.False(t, MatchSystemFile(cfg, mockPlatform, "Custom", "/some/other/file.rom"))
}

func TestGetSystemPaths_CustomLauncherAbsolutePath(t *testing.T) {
	// Create a real temp directory with a file so FindPath can resolve it
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "game.iso"), []byte("test"), 0o600)
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
		{
			ID:         "CustomPS2",
			SystemID:   "PS2",
			Folders:    []string{tmpDir},
			Extensions: []string{".iso"},
		},
	})

	cfg := &config.Instance{}

	// We need to import mediascanner's GetSystemPaths, but it's in another package.
	// Instead, test the underlying logic: LauncherCache returns the launcher with
	// absolute Folders, and the folder is valid on disk.

	originalCache := GlobalLauncherCache
	testCache := &LauncherCache{}
	testCache.Initialize(mockPlatform, cfg)
	GlobalLauncherCache = testCache
	defer func() { GlobalLauncherCache = originalCache }()

	// Verify the cache correctly stores the launcher with its absolute folder
	launchers := GlobalLauncherCache.GetLaunchersBySystem("PS2")
	assert.Len(t, launchers, 1)
	assert.Equal(t, []string{tmpDir}, launchers[0].Folders)
	assert.True(t, filepath.IsAbs(launchers[0].Folders[0]),
		"custom launcher folder should be absolute")

	// Verify PathIsLauncher works for a file in this directory
	assert.True(t, PathIsLauncher(cfg, mockPlatform, &launchers[0], filepath.Join(tmpDir, "game.iso")))
}
