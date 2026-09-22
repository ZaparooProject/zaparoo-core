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
	"errors"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestLauncherMatcher_ShouldSkipScanSymlink(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	rootDir := t.TempDir()
	otherRoot := t.TempDir()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{rootDir, otherRoot})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
		{
			ID:                       "Arcade",
			SystemID:                 "Arcade",
			Folders:                  []string{"_Arcade"},
			Extensions:               []string{".mra"},
			ScanSkipInternalSymlinks: true,
		},
		{
			ID:         "Plain",
			SystemID:   "Plain",
			Folders:    []string{"plain"},
			Extensions: []string{".bin"},
		},
		{
			ID:                       "SharedOptIn",
			SystemID:                 "Shared",
			Folders:                  []string{"shared"},
			ScanSkipInternalSymlinks: true,
		},
		{
			ID:       "SharedPlain",
			SystemID: "Shared",
			Folders:  []string{"shared"},
		},
		{
			ID:                       "SharedSkippedOptIn",
			SystemID:                 "SharedSkipped",
			Folders:                  []string{"shared-skipped"},
			ScanSkipInternalSymlinks: true,
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
	arcadeDir := filepath.Join(rootDir, "_Arcade")
	canonical := filepath.Join(arcadeDir, "Pooyan.mra")
	targetOf := func(target string) func() (string, error) {
		return func() (string, error) { return target, nil }
	}
	mustNotRead := func() (string, error) {
		t.Helper()
		t.Fatal("readTarget must not be called when no launcher opts in")
		return "", nil
	}

	skip, err := matcher.ShouldSkipScanSymlink(
		"Arcade", filepath.Join(arcadeDir, "_1 A-E", "Pooyan.mra"), targetOf(canonical),
	)
	require.NoError(t, err)
	assert.True(t, skip, "absolute target inside the scan root is an alias")

	skip, err = matcher.ShouldSkipScanSymlink(
		"Arcade", filepath.Join(arcadeDir, "_1 A-E", "Pooyan.mra"), targetOf(filepath.Join("..", "Pooyan.mra")),
	)
	require.NoError(t, err)
	assert.True(t, skip, "relative target resolved against the link directory")

	skip, err = matcher.ShouldSkipScanSymlink(
		"Arcade", filepath.Join(arcadeDir, "_Konami"),
		targetOf(filepath.Join(arcadeDir, "_3 Collections", "_1 By Platform", "_Konami")),
	)
	require.NoError(t, err)
	assert.True(t, skip, "directory symlinks inside the scan root are aliases too")

	skip, err = matcher.ShouldSkipScanSymlink(
		"Arcade", filepath.Join(arcadeDir, "alias.mra"), targetOf(filepath.Join(otherRoot, "_Arcade", "Pooyan.mra")),
	)
	require.NoError(t, err)
	assert.True(t, skip, "target under another root of the same launcher is still indexed there")

	skip, err = matcher.ShouldSkipScanSymlink(
		"Arcade", filepath.Join(rootDir, "_ARCADE", "alias.mra"),
		targetOf(filepath.Join(rootDir, "_arcade", "Pooyan.mra")),
	)
	require.NoError(t, err)
	assert.True(t, skip, "matching is case-insensitive")

	skip, err = matcher.ShouldSkipScanSymlink(
		"Arcade", filepath.Join(arcadeDir, "External.mra"),
		targetOf(filepath.Join(rootDir, "elsewhere", "External.mra")),
	)
	require.NoError(t, err)
	assert.False(t, skip, "target outside every scan root is real media reachable only through the link")

	skip, err = matcher.ShouldSkipScanSymlink("Arcade", filepath.Join(arcadeDir, "_loop"), targetOf(".."))
	require.NoError(t, err)
	assert.False(t, skip, "a link to the parent of the scan root is left to the walker's cycle guard")

	skip, err = matcher.ShouldSkipScanSymlink("Arcade", arcadeDir, mustNotRead)
	require.NoError(t, err)
	assert.False(t, skip, "the scan root itself is never skipped")

	skip, err = matcher.ShouldSkipScanSymlink("Arcade", filepath.Join(rootDir, "other", "alias.mra"), mustNotRead)
	require.NoError(t, err)
	assert.False(t, skip, "links outside every scan root are not the matcher's concern")

	skip, err = matcher.ShouldSkipScanSymlink("Plain", filepath.Join(rootDir, "plain", "alias.bin"), mustNotRead)
	require.NoError(t, err)
	assert.False(t, skip, "launchers without the flag keep their aliases")

	skip, err = matcher.ShouldSkipScanSymlink("Shared", filepath.Join(rootDir, "shared", "alias"), mustNotRead)
	require.NoError(t, err)
	assert.False(t, skip, "one launcher must not drop an alias needed by another launcher on the same root")

	skip, err = matcher.ShouldSkipScanSymlink(
		"SharedSkipped", filepath.Join(rootDir, "shared-skipped", "alias"),
		targetOf(filepath.Join(rootDir, "shared-skipped", "real")),
	)
	require.NoError(t, err)
	assert.True(t, skip, "a non-filesystem launcher must not block the opt-in")

	readErr := errors.New("stale mount")
	skip, err = matcher.ShouldSkipScanSymlink(
		"Arcade", filepath.Join(arcadeDir, "alias.mra"), func() (string, error) { return "", readErr },
	)
	require.ErrorIs(t, err, readErr, "read errors are the caller's to interpret")
	assert.False(t, skip)

	assert.True(t, matcher.MatchSystemFile("Arcade", filepath.Join(arcadeDir, "_1 A-E", "Pooyan.mra")),
		"alias skipping must not affect direct launches")
}

// scanDuplicatesConfig returns a config enabling scan_duplicates for one
// launcher reference, which may name a launcher ID or one of its groups.
func scanDuplicatesConfig(t *testing.T, launcherRef string) *config.Instance {
	t.Helper()
	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[launchers.default]]
launcher = "`+launcherRef+`"
scan_duplicates = true
`))
	return cfg
}

func TestLauncherMatcher_ShouldSkipScanSymlink_ScanDuplicates(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	tests := []struct {
		name        string
		launcherRef string
	}{
		{name: "by launcher id", launcherRef: "Arcade"},
		{name: "by group", launcherRef: "MisterArcade"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.On("Settings").Return(platforms.Settings{})
			mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{rootDir})
			mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
				{
					ID:                       "Arcade",
					SystemID:                 "Arcade",
					Folders:                  []string{"_Arcade"},
					Groups:                   []string{"MisterArcade"},
					Extensions:               []string{".mra"},
					ScanSkipInternalSymlinks: true,
				},
			})

			cfg := scanDuplicatesConfig(t, tt.launcherRef)
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
			mustNotRead := func() (string, error) {
				t.Helper()
				t.Fatal("readTarget must not be called once the launcher scans duplicates")
				return "", nil
			}

			skip, err := matcher.ShouldSkipScanSymlink(
				"Arcade", filepath.Join(arcadeDir, "_1 A-E", "Pooyan.mra"), mustNotRead,
			)
			require.NoError(t, err)
			assert.False(t, skip, "an alias must be kept when the launcher scans duplicates")

			skip, err = matcher.ShouldSkipScanSymlink(
				"Arcade", filepath.Join(arcadeDir, "_Konami"), mustNotRead,
			)
			require.NoError(t, err)
			assert.False(t, skip, "directory aliases are kept too")
		})
	}
}

// arcadeMatcherForBrokenSymlinks builds a matcher over one Arcade launcher that
// suppresses duplicates, optionally with the scan_duplicates opt-in applied.
func arcadeMatcherForBrokenSymlinks(t *testing.T, rootDir string, optIn bool) *LauncherMatcher {
	t.Helper()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{rootDir})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
		{
			ID:                       "Arcade",
			SystemID:                 "Arcade",
			Folders:                  []string{"_Arcade"},
			Extensions:               []string{".mra"},
			ScanDirectoryExcludes:    []string{"_Organized"},
			ScanSkipInternalSymlinks: true,
		},
	})

	cfg := &config.Instance{}
	if optIn {
		cfg = scanDuplicatesConfig(t, "Arcade")
	}
	testLauncherCacheMutex.Lock()
	originalCache := GlobalLauncherCache
	testCache := &LauncherCache{}
	testCache.Initialize(mockPlatform, cfg)
	GlobalLauncherCache = testCache
	t.Cleanup(func() {
		GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	})
	return NewLauncherMatcher(cfg, mockPlatform)
}

func TestLauncherMatcher_ShouldSkipBrokenScanSymlink(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	rootDir := t.TempDir()
	matcher := arcadeMatcherForBrokenSymlinks(t, rootDir, true)
	arcadeDir := filepath.Join(rootDir, "_Arcade")

	skip, err := matcher.ShouldSkipBrokenScanSymlink(
		"Arcade", filepath.Join(arcadeDir, "_Organized", "Gone.mra"),
		func() (bool, error) { return false, nil },
	)
	require.NoError(t, err)
	assert.True(t, skip, "an alias whose target is gone must be dropped")

	skip, err = matcher.ShouldSkipBrokenScanSymlink(
		"Arcade", filepath.Join(arcadeDir, "_Organized", "Pooyan.mra"),
		func() (bool, error) { return true, nil },
	)
	require.NoError(t, err)
	assert.False(t, skip, "an alias that still resolves is kept")

	// A stat that fails for any reason other than absence must keep the entry,
	// so a slow or flaky mount never deletes a library.
	statErr := errors.New("input/output error")
	skip, err = matcher.ShouldSkipBrokenScanSymlink(
		"Arcade", filepath.Join(arcadeDir, "_Organized", "Flaky.mra"),
		func() (bool, error) { return false, statErr },
	)
	require.ErrorIs(t, err, statErr)
	assert.False(t, skip)

	skip, err = matcher.ShouldSkipBrokenScanSymlink(
		"Arcade", filepath.Join(rootDir, "elsewhere", "Gone.mra"),
		func() (bool, error) {
			t.Helper()
			t.Fatal("a link outside the launcher's roots must not be stat'd")
			return false, nil
		},
	)
	require.NoError(t, err)
	assert.False(t, skip, "only paths the opted-in launcher owns are checked")
}

// TestLauncherMatcher_ShouldSkipBrokenScanSymlink_NoOptIn is the guarantee that
// every scan without the opt-in is unchanged: the answer is no, and the target
// is never stat'd.
func TestLauncherMatcher_ShouldSkipBrokenScanSymlink_NoOptIn(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	rootDir := t.TempDir()
	matcher := arcadeMatcherForBrokenSymlinks(t, rootDir, false)

	skip, err := matcher.ShouldSkipBrokenScanSymlink(
		"Arcade", filepath.Join(rootDir, "_Arcade", "_Organized", "Gone.mra"),
		func() (bool, error) {
			t.Helper()
			t.Fatal("a launcher without scan_duplicates must not stat its symlinks")
			return false, nil
		},
	)
	require.NoError(t, err)
	assert.False(t, skip, "a dangling link is still indexed without the opt-in")
}
