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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetLauncherByID_Found(t *testing.T) {
	t.Parallel()

	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "launcher-a", SystemID: "NES"},
		{ID: "launcher-b", SystemID: "SNES"},
	})

	launcher := cache.GetLauncherByID("launcher-b")
	require.NotNil(t, launcher)
	assert.Equal(t, "launcher-b", launcher.ID)
	assert.Equal(t, "SNES", launcher.SystemID)
}

func TestGetLauncherByID_NotFound(t *testing.T) {
	t.Parallel()

	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "launcher-a", SystemID: "NES"},
	})

	launcher := cache.GetLauncherByID("nonexistent")
	assert.Nil(t, launcher)
}

func TestGetLauncherByID_EmptyCache(t *testing.T) {
	t.Parallel()

	cache := &LauncherCache{}
	launcher := cache.GetLauncherByID("anything")
	assert.Nil(t, launcher)
}

func TestToRelativePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		systemID string
		path     string
		want     string
		rootDirs []string
		folders  []string
	}{
		{
			name:     "basic match",
			rootDirs: []string{"/roms"},
			systemID: "snes",
			path:     "/roms/SNES/game.sfc",
			folders:  []string{"SNES"},
			want:     "snes/game.sfc",
		},
		{
			name:     "nested subdirectory preserved",
			rootDirs: []string{"/roms"},
			systemID: "snes",
			path:     "/roms/SNES/USA/game.sfc",
			folders:  []string{"SNES"},
			want:     "snes/USA/game.sfc",
		},
		{
			name:     "second rootdir matches",
			rootDirs: []string{"/mnt/sd1/games", "/mnt/sd2/games"},
			systemID: "nes",
			path:     "/mnt/sd2/games/NES/game.nes",
			folders:  []string{"NES"},
			want:     "nes/game.nes",
		},
		{
			name:     "URI passthrough",
			rootDirs: []string{"/roms"},
			systemID: "steam",
			path:     "steam://rungameid/12345",
			folders:  []string{"Steam"},
			want:     "steam://rungameid/12345",
		},
		{
			name:     "no match returns original",
			rootDirs: []string{"/roms"},
			systemID: "snes",
			path:     "/other/path/game.sfc",
			folders:  []string{"SNES"},
			want:     "/other/path/game.sfc",
		},
		{
			name:     "case insensitive folder match",
			rootDirs: []string{"/roms"},
			systemID: "snes",
			path:     "/roms/snes/game.sfc",
			folders:  []string{"SNES"},
			want:     "snes/game.sfc",
		},
		{
			name:     "case insensitive rootdir match",
			rootDirs: []string{"/roms"},
			systemID: "snes",
			path:     "/ROMS/SNES/game.sfc",
			folders:  []string{"SNES"},
			want:     "snes/game.sfc",
		},
		{
			name:     "no launchers for system",
			rootDirs: []string{"/roms"},
			systemID: "unknown",
			path:     "/roms/Unknown/game.bin",
			folders:  nil,
			want:     "/roms/Unknown/game.bin",
		},
		{
			name:     "empty rootdirs",
			rootDirs: []string{},
			systemID: "snes",
			path:     "/roms/SNES/game.sfc",
			folders:  []string{"SNES"},
			want:     "/roms/SNES/game.sfc",
		},
		{
			name:     "deeply nested path",
			rootDirs: []string{"/media/fat/games"},
			systemID: "ps1",
			path:     "/media/fat/games/PSX/USA/RPG/Final Fantasy VII/disc1.bin",
			folders:  []string{"PSX"},
			want:     "ps1/USA/RPG/Final Fantasy VII/disc1.bin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cache := &LauncherCache{}
			if tt.folders != nil {
				cache.InitializeFromSlice([]platforms.Launcher{
					{ID: "test-launcher", SystemID: tt.systemID, Folders: tt.folders},
				})
			}

			got := cache.ToRelativePath(tt.rootDirs, tt.systemID, tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToRelativePath_AbsoluteFolder(t *testing.T) {
	t.Parallel()

	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "custom-ps2", SystemID: "ps2", Folders: []string{"/custom/ps2"}},
	})

	got := cache.ToRelativePath(nil, "ps2", "/custom/ps2/game.iso")
	assert.Equal(t, "ps2/game.iso", got)
}

func TestToRelativePath_MultipleLaunchers(t *testing.T) {
	t.Parallel()

	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "megadrive", SystemID: "genesis", Folders: []string{"MegaDrive"}},
		{ID: "genesis", SystemID: "genesis", Folders: []string{"Genesis"}},
	})

	// Path under the second launcher's folder should still match.
	got := cache.ToRelativePath([]string{"/roms"}, "genesis", "/roms/Genesis/game.md")
	assert.Equal(t, "genesis/game.md", got)
}

func TestToRelativePath_SkipFilesystemScan(t *testing.T) {
	t.Parallel()

	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "skip-me", SystemID: "snes", Folders: []string{"SNES"}, SkipFilesystemScan: true},
	})

	// Launcher with SkipFilesystemScan should not be used for matching.
	got := cache.ToRelativePath([]string{"/roms"}, "snes", "/roms/SNES/game.sfc")
	assert.Equal(t, "/roms/SNES/game.sfc", got)
}
