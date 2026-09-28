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
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sourceTestPath(t *testing.T, segments ...string) string {
	t.Helper()
	id := strings.TrimPrefix(platforms.SourceRootPath("tree"), platforms.SourceScheme+"://")
	path, err := virtualpath.CreateVirtualPathSegments(platforms.SourceScheme, id, segments)
	require.NoError(t, err)
	return path
}

// A source path's name comes from its decoded last segment, split into name
// and extension exactly as a filesystem path's base name is.
func TestSourcePathNames(t *testing.T) {
	t.Parallel()
	path := sourceTestPath(t, "NES", "Sub Dir", "Game #3 & 50% (USA).nes")
	assert.Equal(t, "Game #3 & 50% (USA)", FilenameFromPath(path))
	info := GetPathInfo(path)
	assert.Equal(t, "Game #3 & 50% (USA).nes", info.Filename)
	assert.Equal(t, ".nes", info.Extension)
	assert.Equal(t, "Game #3 & 50% (USA)", info.Name)

	noExt := sourceTestPath(t, "ScummVM", "Monkey Island")
	assert.Equal(t, "Monkey Island", FilenameFromPath(noExt))
	info = GetPathInfo(noExt)
	assert.Equal(t, "Monkey Island", info.Filename)
	assert.Empty(t, info.Extension)

	// Title schemes are unchanged: no extension is split off.
	info = GetPathInfo("steam://123/Half-Life%202.5")
	assert.Equal(t, "Half-Life 2.5", info.Name)
	assert.Empty(t, info.Extension)
}

func sourceMatcherFixture(t *testing.T, launchers []platforms.Launcher) (*config.Instance, *mocks.MockPlatform) {
	t.Helper()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("source-test")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", &config.Instance{}).Return([]string{"/roms"})
	mockPlatform.On("Launchers", &config.Instance{}).Return(launchers)
	cfg := &config.Instance{}
	testLauncherCacheMutex.Lock()
	original := GlobalLauncherCache
	cache := &LauncherCache{}
	cache.Initialize(mockPlatform, cfg)
	GlobalLauncherCache = cache
	t.Cleanup(func() {
		GlobalLauncherCache = original
		testLauncherCacheMutex.Unlock()
	})
	return cfg, mockPlatform
}

// A source root is matched like a root directory: the path below it must sit
// in one of the launcher's relative folders, and the extension must match.
func TestSourcePathMatchesLauncherFolders(t *testing.T) {
	launchers := []platforms.Launcher{
		{ID: "NESCore", SystemID: "NES", Folders: []string{"NES"}, Extensions: []string{".nes"}},
		{ID: "Nested", SystemID: "SNES", Folders: []string{"Console/SNES"}, Extensions: []string{".sfc"}},
		{ID: "Absolute", SystemID: "GB", Folders: []string{"/media/gb"}, Extensions: []string{".gb"}},
		{ID: "Generic", SystemID: "PSX", Extensions: []string{".cue"}},
	}
	cfg, pl := sourceMatcherFixture(t, launchers)
	matcher := NewLauncherMatcher(cfg, pl)

	for _, tt := range []struct {
		launcher *platforms.Launcher
		path     string
		want     bool
	}{
		{&launchers[0], sourceTestPath(t, "nes", "Game.nes"), true},
		{&launchers[0], sourceTestPath(t, "NES", "Sub", "Game.NES"), true},
		{&launchers[0], sourceTestPath(t, "NES2", "Game.nes"), false},
		{&launchers[0], sourceTestPath(t, "Games", "NES", "Game.nes"), false},
		{&launchers[0], sourceTestPath(t, "NES", "Game.sfc"), false},
		{&launchers[1], sourceTestPath(t, "console", "snes", "Game.sfc"), true},
		{&launchers[2], sourceTestPath(t, "media", "gb", "Game.gb"), false},
		{&launchers[3], sourceTestPath(t, "anywhere", "Game.cue"), true},
	} {
		assert.Equal(t, tt.want, PathIsLauncher(cfg, pl, tt.launcher, tt.path), tt.path)
		assert.Equal(t, tt.want, matcher.MatchLauncherFileForScan(tt.launcher, tt.path), tt.path)
	}
}

// Scan excludes and directory excludes see a source path's real names.
func TestSourcePathScanExcludes(t *testing.T) {
	launchers := []platforms.Launcher{{
		ID: "NESCore", SystemID: "NES", Folders: []string{"NES"}, Extensions: []string{".nes"},
		ScanExcludes:          []string{"* (Beta).nes"},
		ScanDirectoryExcludes: []string{"Hacks & Homebrew"},
	}}
	cfg, pl := sourceMatcherFixture(t, launchers)
	matcher := NewLauncherMatcher(cfg, pl)

	assert.True(t, matcher.MatchSystemFileForScan("NES", sourceTestPath(t, "NES", "Game (USA).nes")))
	assert.False(t, matcher.MatchSystemFileForScan("NES", sourceTestPath(t, "NES", "Game (Beta).nes")))
	assert.True(t, matcher.ShouldSkipScanDirectory("NES", sourceTestPath(t, "NES", "Hacks & Homebrew")))
	assert.False(t, matcher.ShouldSkipScanDirectory("NES", sourceTestPath(t, "NES", "Sub")))
	assert.False(t, matcher.ShouldSkipScanDirectory("NES", sourceTestPath(t, "NES")),
		"the system folder itself is never skipped")
}
