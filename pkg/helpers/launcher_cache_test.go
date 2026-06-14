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
	"path/filepath"
	"sync"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/launchables"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestGetLaunchableSystems_ReturnsCopy(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	systems := []launchables.VirtualSystem{
		{ID: id, Name: "Chess", Category: "Other"},
	}
	cache := &LauncherCache{}
	cache.setLaunchableSystems(systems)
	systems[0].Name = "Mutated Source"

	got := cache.GetLaunchableSystems()
	require.Len(t, got, 1)
	assert.Equal(t, id, got[0].ID)
	assert.Equal(t, "Chess", got[0].Name)
	assert.Equal(t, "Other", got[0].Category)

	got[0].Name = "Mutated Result"
	gotAgain := cache.GetLaunchableSystems()
	require.Len(t, gotAgain, 1)
	assert.Equal(t, "Chess", gotAgain[0].Name)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.Len(t, cache.GetLaunchableSystems(), 1)
		}()
	}
	wg.Wait()
}

func TestGetLaunchableSystems_EmptyCache(t *testing.T) {
	t.Parallel()

	cache := &LauncherCache{}
	got := cache.GetLaunchableSystems()
	assert.Empty(t, got)

	got = append(got, launchables.VirtualSystem{Name: "Mutated Result"})
	assert.Len(t, got, 1)
	assert.Empty(t, cache.GetLaunchableSystems())
}

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

	// Use t.TempDir() to get a path that is absolute on all platforms
	// (including Windows where /custom/ps2 is not considered absolute).
	absFolder := filepath.Join(t.TempDir(), "ps2")
	absPath := filepath.Join(absFolder, "game.iso")

	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "custom-ps2", SystemID: "ps2", Folders: []string{absFolder}},
	})

	got := cache.ToRelativePath(nil, "ps2", absPath)
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

func TestInitialize_DeduplicatesExtraLaunchers(t *testing.T) {
	t.Parallel()

	mp := mocks.NewMockPlatform()
	mp.On("Launchers", mock.Anything).Return([]platforms.Launcher{
		{ID: "platform-launcher", SystemID: "NES"},
	})

	extra := platforms.Launcher{ID: "extra-launcher", SystemID: "SNES"}
	duplicate := platforms.Launcher{ID: "platform-launcher", SystemID: "NES"} // same ID as platform launcher

	cache := &LauncherCache{}
	cache.Initialize(mp, nil, extra, duplicate)
	mp.AssertExpectations(t)

	all := cache.GetAllLaunchers()
	require.Len(t, all, 2, "duplicate extra launcher must not be added twice")

	ids := make([]string, 0, len(all))
	for _, l := range all {
		ids = append(ids, l.ID)
	}
	assert.Contains(t, ids, "platform-launcher")
	assert.Contains(t, ids, "extra-launcher")
}

func TestInitialize_ExtraLauncherIsRetrievable(t *testing.T) {
	t.Parallel()

	mp := mocks.NewMockPlatform()
	mp.On("Launchers", mock.Anything).Return([]platforms.Launcher{})

	extra := platforms.Launcher{ID: "native-audio", SystemID: "Audio"}

	cache := &LauncherCache{}
	cache.Initialize(mp, nil, extra)
	mp.AssertExpectations(t)

	found := cache.GetLauncherByID("native-audio")
	require.NotNil(t, found)
	assert.Equal(t, "Audio", found.SystemID)
}
