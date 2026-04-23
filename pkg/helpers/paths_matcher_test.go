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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestPathHasPrefixNormalized(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		path     string
		root     string
		expected bool
	}{
		{"exact match", "/media/fat/games", "/media/fat/games", true},
		{"child path", "/media/fat/games/nes/mario.nes", "/media/fat/games", true},
		{"not a child", "/media/fat/other/game.nes", "/media/fat/games", false},
		{"prefix boundary", "/media/fat/games2/game.nes", "/media/fat/games", false},
		{"root with trailing slash", "/media/fat/games/nes/game.nes", "/media/fat/games/", true},
		{"empty root", "/media/fat/games", "", false},
		{"both empty", "", "", true},
		{"root only slash", "/a/b", "/", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, pathHasPrefixNormalized(tt.path, tt.root))
		})
	}
}

func TestLauncherMatcher_PassesSamePathToTestFunc(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	var directSeen string
	var matcherSeen string

	launcher := platforms.Launcher{
		ID:       "GenericLauncher",
		SystemID: "Custom",
		Test: func(_ *config.Instance, p string) bool {
			if directSeen == "" {
				directSeen = p
			} else {
				matcherSeen = p
			}
			return p == `c:\roms\custom\game.rom`
		},
	}

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{launcher})

	cfg := &config.Instance{}

	testLauncherCacheMutex.Lock()
	originalCache := GlobalLauncherCache
	testCache := &LauncherCache{}
	testCache.Initialize(mockPlatform, cfg)
	GlobalLauncherCache = testCache
	defer func() {
		GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	}()

	path := `C:\ROMS\Custom\Game.rom`
	assert.True(t, MatchSystemFile(cfg, mockPlatform, "Custom", path))

	matcher := NewLauncherMatcher(cfg, mockPlatform)
	assert.True(t, matcher.MatchSystemFile("Custom", path))

	assert.Equal(t, directSeen, matcherSeen)
	assert.Equal(t, `c:\roms\custom\game.rom`, matcherSeen)
}

func TestLauncherMatcher_MatchSystemFile(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	tmpDir := t.TempDir()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
		{
			ID:         "NESLauncher",
			SystemID:   "NES",
			Folders:    []string{"nes"},
			Extensions: []string{".nes"},
		},
		{
			ID:         "CustomPS2",
			SystemID:   "PS2",
			Folders:    []string{tmpDir},
			Extensions: []string{".iso"},
		},
	})

	cfg := &config.Instance{}

	testLauncherCacheMutex.Lock()
	originalCache := GlobalLauncherCache
	testCache := &LauncherCache{}
	testCache.Initialize(mockPlatform, cfg)
	GlobalLauncherCache = testCache
	defer func() {
		GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	}()

	matcher := NewLauncherMatcher(cfg, mockPlatform)

	// NES via root + relative folder
	assert.True(t, matcher.MatchSystemFile("NES", "/roms/nes/mario.nes"))
	assert.False(t, matcher.MatchSystemFile("NES", "/other/nes/mario.nes"))
	assert.False(t, matcher.MatchSystemFile("NES", "/roms/nes/readme.txt"))

	// PS2 via absolute folder
	assert.True(t, matcher.MatchSystemFile("PS2", filepath.Join(tmpDir, "game.iso")))
	assert.False(t, matcher.MatchSystemFile("PS2", filepath.Join(tmpDir, "game.txt")))

	// Wrong system
	assert.False(t, matcher.MatchSystemFile("SNES", "/roms/nes/mario.nes"))

	// Empty path
	assert.False(t, matcher.MatchSystemFile("NES", ""))

	// Dot file
	assert.False(t, matcher.MatchSystemFile("NES", "/roms/nes/.hidden.nes"))
}

func TestLauncherMatcher_EquivalentToMatchSystemFile(t *testing.T) {
	// Verify LauncherMatcher produces the same results as the original MatchSystemFile
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	tmpDir := t.TempDir()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
		{
			ID:         "NESLauncher",
			SystemID:   "NES",
			Folders:    []string{"nes"},
			Extensions: []string{".nes", ".zip"},
		},
		{
			ID:         "AbsLauncher",
			SystemID:   "PS2",
			Folders:    []string{tmpDir},
			Extensions: []string{".iso"},
		},
	})

	cfg := &config.Instance{}

	testLauncherCacheMutex.Lock()
	originalCache := GlobalLauncherCache
	testCache := &LauncherCache{}
	testCache.Initialize(mockPlatform, cfg)
	GlobalLauncherCache = testCache
	defer func() {
		GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	}()

	matcher := NewLauncherMatcher(cfg, mockPlatform)

	paths := []struct {
		system string
		path   string
	}{
		{"NES", "/roms/nes/mario.nes"},
		{"NES", "/roms/nes/game.zip"},
		{"NES", "/roms/snes/game.nes"},
		{"NES", "/other/nes/mario.nes"},
		{"NES", "/roms/nes/readme.txt"},
		{"NES", ""},
		{"NES", "/roms/nes/.hidden.nes"},
		{"PS2", filepath.Join(tmpDir, "game.iso")},
		{"PS2", filepath.Join(tmpDir, "game.txt")},
		{"PS2", "/other/game.iso"},
		{"SNES", "/roms/nes/mario.nes"},
	}

	for _, p := range paths {
		expected := MatchSystemFile(cfg, mockPlatform, p.system, p.path)
		actual := matcher.MatchSystemFile(p.system, p.path)
		assert.Equal(t, expected, actual,
			"mismatch for system=%s path=%s: MatchSystemFile=%v, LauncherMatcher=%v",
			p.system, p.path, expected, actual)
	}
}
