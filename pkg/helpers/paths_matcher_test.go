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
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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

// Test functions receive the path with its original case. They are the only
// matcher hook that reaches the filesystem (stat, EvalSymlinks) or compares
// against a real directory, so a lowercased path silently fails to match on a
// case-sensitive filesystem. Both matcher implementations must agree.
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
			return p == `C:\ROMS\Custom\Game.rom`
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
	assert.Equal(t, `C:\ROMS\Custom\Game.rom`, matcherSeen)
}

// A launcher that discovers its media through a Scanner (RetroDECK, EmuDeck,
// AmigaVision) declares no folders or extensions, so its Test function is the
// only gate and does the containment check itself against the real filesystem.
// A lowercased path makes every such lookup miss on a case-sensitive
// filesystem, and the launcher is never matched.
func TestPathIsLauncher_TestFuncCanStatTheRealPath(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	romsDir := t.TempDir()
	systemDir := filepath.Join(romsDir, "snes")
	require.NoError(t, os.MkdirAll(systemDir, 0o750))
	game := filepath.Join(systemDir, "Super Mario World.sfc")
	require.NoError(t, os.WriteFile(game, []byte("rom"), 0o600))

	launcher := platforms.Launcher{
		ID:                 "ScannerBacked",
		SystemID:           "SNES",
		SkipFilesystemScan: true,
		Test: func(_ *config.Instance, path string) bool {
			// Both sides are resolved, as providerPathTest does: on macOS
			// t.TempDir() sits under a symlinked /var, so comparing a resolved
			// path against an unresolved root escapes the root every time.
			root, err := filepath.EvalSymlinks(systemDir)
			if err != nil {
				return false
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return false
			}
			rel, err := filepath.Rel(root, resolved)
			return err == nil && !strings.HasPrefix(rel, "..")
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

	assert.True(t, PathIsLauncher(cfg, mockPlatform, &launcher, game))

	matcher := NewLauncherMatcher(cfg, mockPlatform)
	assert.True(t, matcher.MatchSystemFile("SNES", game))
}

// A launcher that declares extensions still falls through to its Test function
// when the extension misses, and that fallback has to see the original path as
// well. MiSTer's NeoGeo launcher declares only .neo and leans on Test to accept
// the .zip romsets, which it then has to find on disk.
func TestPathIsLauncher_ExtensionMissPassesOriginalPathToTest(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	var directSeen, matcherSeen string

	launcher := platforms.Launcher{
		ID:         "ExtensionLauncher",
		SystemID:   "NeoGeo",
		Extensions: []string{".neo"},
		Test: func(_ *config.Instance, p string) bool {
			if directSeen == "" {
				directSeen = p
			} else {
				matcherSeen = p
			}
			return strings.EqualFold(filepath.Ext(p), ".zip")
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

	path := filepath.Join("NEOGEO", "KOF98.ZIP")
	assert.True(t, PathIsLauncher(cfg, mockPlatform, &launcher, path))

	matcher := NewLauncherMatcher(cfg, mockPlatform)
	assert.True(t, matcher.MatchSystemFile("NeoGeo", path))

	assert.Equal(t, path, directSeen)
	assert.Equal(t, path, matcherSeen)
}

// A LauncherMatcher precomputes its state from the launcher cache it was built
// against, and a media scan holds one for the whole walk. A launchers.refresh
// part way through leaves the matcher with no precomputed entry for the new
// launchers, so every match falls back to reading the launcher directly. That
// fallback has to agree with the precomputed branch, original path included.
func TestLauncherMatcher_FallbackWithoutPrecomputedLauncher(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	var seen string

	staleLauncher := platforms.Launcher{
		ID:         "StaleLauncher",
		SystemID:   "NeoGeo",
		Extensions: []string{".neo"},
	}
	refreshedLauncher := platforms.Launcher{
		ID:         "RefreshedLauncher",
		SystemID:   "NeoGeo",
		Extensions: []string{".neo"},
		Test: func(_ *config.Instance, p string) bool {
			seen = p
			return strings.EqualFold(filepath.Ext(p), ".zip")
		},
	}

	cfg := &config.Instance{}

	stalePlatform := mocks.NewMockPlatform()
	stalePlatform.On("Settings").Return(platforms.Settings{})
	stalePlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})
	stalePlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{staleLauncher})

	refreshedPlatform := mocks.NewMockPlatform()
	refreshedPlatform.On("Settings").Return(platforms.Settings{})
	refreshedPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})
	refreshedPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{refreshedLauncher})

	testLauncherCacheMutex.Lock()
	originalCache := GlobalLauncherCache
	staleCache := &LauncherCache{}
	staleCache.Initialize(stalePlatform, cfg)
	GlobalLauncherCache = staleCache
	defer func() {
		GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	}()

	matcher := NewLauncherMatcher(cfg, stalePlatform)

	refreshedCache := &LauncherCache{}
	refreshedCache.Initialize(refreshedPlatform, cfg)
	GlobalLauncherCache = refreshedCache

	// Declared extension, matched without ever reaching Test.
	assert.True(t, matcher.MatchSystemFile("NeoGeo", filepath.Join("NEOGEO", "MSLUG.NEO")))
	assert.Empty(t, seen)

	// Extension miss, so Test decides and must see the original path.
	path := filepath.Join("NEOGEO", "KOF98.ZIP")
	assert.True(t, matcher.MatchSystemFile("NeoGeo", path))
	assert.Equal(t, path, seen)
}

func TestLauncherMatcher_SchemeUsesTestFunction(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	launcher := platforms.Launcher{
		ID:       "VirtualLauncher",
		SystemID: "Virtual",
		Schemes:  []string{"zaparoo"},
		Test: func(_ *config.Instance, path string) bool {
			return path == "zaparoo://expected/Game"
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

	matcher := NewLauncherMatcher(cfg, mockPlatform)
	assert.True(t, matcher.MatchSystemFile("Virtual", "zaparoo://expected/Game"))
	assert.False(t, matcher.MatchSystemFile("Virtual", "zaparoo://other/Game"))
}

func TestLauncherMatcher_NilPlatformDoesNotSynthesizeMediaPaths(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	launcher := platforms.Launcher{
		ID:         "NESLauncher",
		SystemID:   "NES",
		Folders:    []string{"roms"},
		Extensions: []string{".nes"},
	}
	cfg := &config.Instance{}

	testLauncherCacheMutex.Lock()
	originalCache := GlobalLauncherCache
	testCache := &LauncherCache{}
	testCache.InitializeFromSlice([]platforms.Launcher{launcher})
	GlobalLauncherCache = testCache
	defer func() {
		GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	}()

	matcher := NewLauncherMatcher(cfg, nil)
	assert.False(t, matcher.MatchSystemFile("NES", filepath.Join("media", "nes", "game.nes")))
}

func TestLauncherMatcher_FindLauncherNoMatch(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	launcher := platforms.Launcher{
		ID:         "NESLauncher",
		SystemID:   "NES",
		Extensions: []string{".nes"},
	}
	cfg := &config.Instance{}

	testLauncherCacheMutex.Lock()
	originalCache := GlobalLauncherCache
	testCache := &LauncherCache{}
	testCache.InitializeFromSlice([]platforms.Launcher{launcher})
	GlobalLauncherCache = testCache
	defer func() {
		GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	}()

	matcher := NewLauncherMatcher(cfg, nil)
	_, err := matcher.FindLauncher(filepath.Join("media", "games", "gamelist.xml"))

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoLauncher)
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

func TestLauncherMatcher_MatchSystemFileForScanExcludes(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	rootDir := t.TempDir()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{rootDir})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
		{
			ID:           "NESLauncher",
			SystemID:     "NES",
			Folders:      []string{"nes"},
			Extensions:   []string{".rom", ".vhd", ".sav", ".srm"},
			ScanExcludes: []string{"boot.rom", "boot.zip/boot.vhd", "*.sav"},
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

	gamePath := filepath.Join(rootDir, "nes", "game.rom")
	assert.True(t, matcher.MatchSystemFile("NES", gamePath))
	assert.True(t, matcher.MatchSystemFileForScan("NES", gamePath))

	bootPath := filepath.Join(rootDir, "nes", "boot.rom")
	assert.True(t, matcher.MatchSystemFile("NES", bootPath))
	assert.False(t, matcher.MatchSystemFileForScan("NES", bootPath))

	zipBootPath := filepath.Join(rootDir, "nes", "boot.zip", "boot.vhd")
	assert.True(t, matcher.MatchSystemFile("NES", zipBootPath))
	assert.False(t, matcher.MatchSystemFileForScan("NES", zipBootPath))

	wildcardSavePath := filepath.Join(rootDir, "nes", "zelda.sav")
	assert.True(t, matcher.MatchSystemFile("NES", wildcardSavePath))
	assert.False(t, matcher.MatchSystemFileForScan("NES", wildcardSavePath))

	nonMatchingWildcardPath := filepath.Join(rootDir, "nes", "zelda.srm")
	assert.True(t, matcher.MatchSystemFile("NES", nonMatchingWildcardPath))
	assert.True(t, matcher.MatchSystemFileForScan("NES", nonMatchingWildcardPath))
}

func TestLauncherMatcher_ShouldSkipScanDirectory(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	rootDir := t.TempDir()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{rootDir})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
		{
			ID:                    "Arcade",
			SystemID:              "Arcade",
			Folders:               []string{"_Arcade"},
			Extensions:            []string{".mra"},
			ScanDirectoryExcludes: []string{"_Organized"},
		},
		{
			ID:                    "SharedFiltered",
			SystemID:              "Shared",
			Folders:               []string{"shared"},
			ScanDirectoryExcludes: []string{"generated"},
		},
		{
			ID:       "SharedUnfiltered",
			SystemID: "Shared",
			Folders:  []string{filepath.Join("shared", "generated")},
		},
		{
			ID:                    "SharedSkippedFiltered",
			SystemID:              "SharedSkipped",
			Folders:               []string{"shared-skipped"},
			ScanDirectoryExcludes: []string{"generated"},
		},
		{
			ID:                 "SharedSkippedNonFilesystem",
			SystemID:           "SharedSkipped",
			Folders:            []string{"shared-skipped"},
			SkipFilesystemScan: true,
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
	organized := filepath.Join(rootDir, "_Arcade", "_oRgAnIzEd")
	assert.True(t, matcher.ShouldSkipScanDirectory("Arcade", organized))
	assert.False(t, matcher.ShouldSkipScanDirectory("Arcade", filepath.Join(rootDir, "_Arcade")))
	assert.False(t, matcher.ShouldSkipScanDirectory("Arcade", filepath.Join(rootDir, "_Arcade", "alternatives")))
	assert.False(t, matcher.ShouldSkipScanDirectory("Arcade", filepath.Join(rootDir, "other", "_Organized")))
	assert.False(t, matcher.ShouldSkipScanDirectory("Shared", filepath.Join(rootDir, "shared", "generated")),
		"one launcher must not hide a directory needed by another launcher")
	sharedSkipped := filepath.Join(rootDir, "shared-skipped", "generated")
	assert.True(t, matcher.ShouldSkipScanDirectory("SharedSkipped", sharedSkipped),
		"a non-filesystem launcher must not prevent a filesystem launcher from excluding a directory")

	aliasPath := filepath.Join(organized, "Pooyan.mra")
	assert.True(t, matcher.MatchSystemFile("Arcade", aliasPath),
		"scan directory exclusions must not affect direct launches")
}

func TestLauncherMatcher_RootSlashAndDotFolder(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{"/"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
		{
			ID:         "RootLauncher",
			SystemID:   "ROOT",
			Folders:    []string{"."},
			Extensions: []string{".nes"},
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

	assert.True(t, matcher.MatchSystemFile("ROOT", "/game.nes"))
	assert.False(t, matcher.MatchSystemFile("ROOT", "/game.txt"))
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

func TestLauncherMatcher_ShouldSkipScanDirectory_ScanDuplicates(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	rootDir := t.TempDir()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{rootDir})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
		{
			ID:                    "Arcade",
			SystemID:              "Arcade",
			Folders:               []string{"_Arcade"},
			Extensions:            []string{".mra"},
			ScanExcludes:          []string{"boot.rom"},
			ScanDirectoryExcludes: []string{"_Organized"},
		},
		{
			ID:                    "SharedToggled",
			SystemID:              "Shared",
			Folders:               []string{"shared"},
			Extensions:            []string{".bin"},
			ScanDirectoryExcludes: []string{"generated"},
		},
		{
			ID:                    "SharedPlain",
			SystemID:              "Shared",
			Folders:               []string{"shared"},
			Extensions:            []string{".bin"},
			ScanDirectoryExcludes: []string{"generated"},
		},
	})

	cfg := scanDuplicatesConfig(t, "Arcade")
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
	arcadeDir := filepath.Join(rootDir, "_Arcade")

	assert.False(t, matcher.ShouldSkipScanDirectory("Arcade", filepath.Join(arcadeDir, "_Organized")),
		"an excluded directory must be scanned once the launcher scans duplicates")
	assert.True(t, matcher.ShouldSkipScanDirectory("Shared", filepath.Join(rootDir, "shared", "generated")),
		"a launcher the config does not name keeps its excludes")

	assert.True(t, matcher.MatchSystemFileForScan("Arcade", filepath.Join(arcadeDir, "_Organized", "Pooyan.mra")),
		"media inside the previously excluded directory is now indexed")
	assert.False(t, matcher.MatchSystemFileForScan("Arcade", filepath.Join(arcadeDir, "boot.rom")),
		"scan_duplicates must not disable file excludes for non-media files")
}

// TestLauncherMatcher_ScanDuplicatesDuplicateLauncherIDs covers the
// launcherPaths fallback: a blanked cache entry recomputes its precomp inside
// the walk, so it must read the same scan_duplicates answer as construction did.
func TestLauncherMatcher_ScanDuplicatesDuplicateLauncherIDs(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	tmpDir := t.TempDir()
	launcher := platforms.Launcher{
		ID:                    "PS2",
		SystemID:              "PS2",
		Folders:               []string{tmpDir},
		Extensions:            []string{".iso"},
		ScanDirectoryExcludes: []string{"updates"},
	}
	other := launcher
	other.Extensions = []string{".chd"}

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return(
		[]platforms.Launcher{launcher, other})

	cfg := scanDuplicatesConfig(t, "PS2")
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
	assert.False(t, matcher.ShouldSkipScanDirectory("PS2", filepath.Join(tmpDir, "updates")),
		"a duplicate launcher ID must recompute with scan_duplicates still applied")
}

// TestLauncherMatcher_ScanDuplicatesReachesEverySharingLauncher pins how far
// the opt-in reaches. A directory is only skipped when every scanning launcher
// on that root excludes it, so naming one launcher opens the directory for the
// whole system's walk, including launchers the config never mentions.
func TestLauncherMatcher_ScanDuplicatesReachesEverySharingLauncher(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	rootDir := t.TempDir()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{rootDir})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
		{
			ID:                    "SharedToggled",
			SystemID:              "Shared",
			Folders:               []string{"shared"},
			Extensions:            []string{".bin"},
			ScanDirectoryExcludes: []string{"generated"},
		},
		{
			ID:                    "SharedPlain",
			SystemID:              "Shared",
			Folders:               []string{"shared"},
			Extensions:            []string{".chd"},
			ScanDirectoryExcludes: []string{"generated"},
		},
	})

	cfg := scanDuplicatesConfig(t, "SharedToggled")
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
	generated := filepath.Join(rootDir, "shared", "generated")
	assert.False(t, matcher.ShouldSkipScanDirectory("Shared", generated),
		"one launcher opting in stops the shared directory being skipped")
	assert.True(t, matcher.MatchSystemFileForScan("Shared", filepath.Join(generated, "dupe.chd")),
		"the launcher that did not opt in indexes the reopened directory too")
}
