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

package mediascanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestShouldSkipSymlinkAlias_Skip(t *testing.T) {
	t.Parallel()

	skip, err := shouldSkipSymlinkAlias(context.Background(), func() (bool, error) { return true, nil })
	require.NoError(t, err)
	assert.True(t, skip)
}

func TestShouldSkipSymlinkAlias_FsTimeout(t *testing.T) {
	t.Parallel()

	skip, err := shouldSkipSymlinkAlias(context.Background(), func() (bool, error) {
		return false, fmt.Errorf("stale mount: %w", ErrFsTimeout)
	})
	require.NoError(t, err)
	assert.True(t, skip, "a timed-out readlink must not hand the link back to fastwalk's untimed stat")
}

func TestShouldSkipSymlinkAlias_OtherError(t *testing.T) {
	t.Parallel()

	skip, err := shouldSkipSymlinkAlias(context.Background(), func() (bool, error) {
		return false, os.ErrNotExist
	})
	require.NoError(t, err)
	assert.False(t, skip, "an unreadable link keeps its ordinary handling")
}

func TestShouldSkipSymlinkAlias_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	skip, err := shouldSkipSymlinkAlias(ctx, func() (bool, error) {
		return false, fmt.Errorf("stale mount: %w", ErrFsTimeout)
	})
	assert.False(t, skip)
	assert.ErrorIs(t, err, context.Canceled)
}

// A link read that lands after cancellation still answers. Returning that
// answer let the walk carry on as though nothing had been interrupted, and a
// cancellation during the final entry then reported a partial scan as complete.
func TestShouldSkipSymlinkAlias_CancelledDuringSuccessfulCheck(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	skip, err := shouldSkipSymlinkAlias(ctx, func() (bool, error) {
		cancel()
		return true, nil
	})
	assert.False(t, skip)
	assert.ErrorIs(t, err, context.Canceled)
}

// installArcadeOrganizerLauncher swaps in a launcher cache holding a MiSTer
// style Arcade launcher rooted at rootDir and returns the matching platform.
// The caller must not run in parallel: the global launcher cache is shared.
func installArcadeOrganizerLauncher(t *testing.T, cfg *config.Instance, rootDir string) *mocks.MockPlatform {
	t.Helper()

	launcher := platforms.Launcher{
		ID:                       "arcade-launcher",
		SystemID:                 systemdefs.SystemArcade,
		Folders:                  []string{"_Arcade"},
		Extensions:               []string{".mra"},
		ScanDirectoryExcludes:    []string{"_Organized", "_1 A-E", "_3 Collections"},
		ScanSkipInternalSymlinks: true,
	}

	platform := mocks.NewMockPlatform()
	platform.On("ID").Return("test-platform")
	platform.On("Settings").Return(platforms.Settings{})
	platform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{rootDir})
	platform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{launcher})

	testLauncherCacheMutex.Lock()
	originalCache := helpers.GlobalLauncherCache
	testCache := &helpers.LauncherCache{}
	testCache.Initialize(platform, cfg)
	helpers.GlobalLauncherCache = testCache
	t.Cleanup(func() {
		helpers.GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	})
	return platform
}

func TestGetFiles_SkipsArcadeOrganizerInPlace(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	rootDir := t.TempDir()
	arcadeDir := filepath.Join(rootDir, "_Arcade")
	externalDir := filepath.Join(rootDir, "external")
	require.NoError(t, os.MkdirAll(filepath.Join(arcadeDir, "_alternatives"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(arcadeDir, "_Arcade Offset"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(arcadeDir, "_favorites"), 0o750))
	require.NoError(t, os.MkdirAll(externalDir, 0o750))

	canonicalPath := filepath.Join(arcadeDir, "Pooyan.mra")
	alternativePath := filepath.Join(arcadeDir, "_alternatives", "Pooyan (alt).mra")
	offsetPath := filepath.Join(arcadeDir, "_Arcade Offset", "Pooyan (fix).mra")
	externalTarget := filepath.Join(externalDir, "External.mra")
	for _, path := range []string{canonicalPath, alternativePath, offsetPath, externalTarget} {
		require.NoError(t, os.WriteFile(path, []byte("<misterromdescription/>"), 0o600))
	}

	// Organizer category folders written straight into _Arcade (ORGDIR=_Arcade).
	platformDir := filepath.Join(arcadeDir, "_3 Collections", "_1 By Platform", "_Konami")
	require.NoError(t, os.MkdirAll(filepath.Join(arcadeDir, "_1 A-E"), 0o750))
	require.NoError(t, os.MkdirAll(platformDir, 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(arcadeDir, "_Organized", "_1 A-E"), 0o750))
	categoryAlias := filepath.Join(arcadeDir, "_1 A-E", "Pooyan.mra")
	platformAlias := filepath.Join(platformDir, "Pooyan.mra")
	organizedAlias := filepath.Join(arcadeDir, "_Organized", "_1 A-E", "Pooyan.mra")
	for _, alias := range []string{categoryAlias, platformAlias, organizedAlias} {
		require.NoError(t, os.Symlink(canonicalPath, alias))
	}
	// TOPDIR adds directory symlinks at the Organizer root; PREPEND_YEAR adds a
	// second file symlink per game.
	topdirLink := filepath.Join(arcadeDir, "_Konami")
	require.NoError(t, os.Symlink(platformDir, topdirLink))
	yearAlias := filepath.Join(arcadeDir, "}82 Pooyan.mra")
	require.NoError(t, os.Symlink(canonicalPath, yearAlias))
	// Hand-made aliases with relative and dangling internal targets.
	relativeAlias := filepath.Join(arcadeDir, "_favorites", "Pooyan.mra")
	require.NoError(t, os.Symlink(filepath.Join("..", "Pooyan.mra"), relativeAlias))
	brokenAlias := filepath.Join(arcadeDir, "Missing.mra")
	require.NoError(t, os.Symlink(filepath.Join(arcadeDir, "missing.mra"), brokenAlias))
	// A loop back to the parent and a link to media outside the scan root.
	require.NoError(t, os.Symlink("..", filepath.Join(arcadeDir, "_loop")))
	externalAlias := filepath.Join(arcadeDir, "External.mra")
	require.NoError(t, os.Symlink(externalTarget, externalAlias))

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)
	platform := installArcadeOrganizerLauncher(t, cfg, rootDir)

	files, err := GetFiles(context.Background(), cfg, platform, systemdefs.SystemArcade, arcadeDir, nil)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{canonicalPath, alternativePath, offsetPath, externalAlias}, files)

	matcher := helpers.NewLauncherMatcher(cfg, platform)
	for _, alias := range []string{
		categoryAlias, platformAlias, organizedAlias, yearAlias, relativeAlias,
		filepath.Join(topdirLink, "Pooyan.mra"),
	} {
		assert.True(t, matcher.MatchSystemFile(systemdefs.SystemArcade, alias),
			"organizer alias %s must remain directly launchable", alias)
	}
}

func TestGetFiles_IgnoresArcadeOrganizerSibling(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	rootDir := t.TempDir()
	arcadeDir := filepath.Join(rootDir, "_Arcade")
	coresDir := filepath.Join(arcadeDir, "cores")
	require.NoError(t, os.MkdirAll(coresDir, 0o750))
	canonicalPath := filepath.Join(arcadeDir, "Pooyan.mra")
	require.NoError(t, os.WriteFile(canonicalPath, []byte("<misterromdescription/>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(coresDir, "Pooyan_20240101.rbf"), []byte("rbf"), 0o600))

	// ORGDIR=/media/fat/_Arcade Organized: a sibling tree plus a cores symlink.
	siblingDir := filepath.Join(rootDir, "_Arcade Organized")
	require.NoError(t, os.MkdirAll(filepath.Join(siblingDir, "_1 A-E"), 0o750))
	siblingAlias := filepath.Join(siblingDir, "_1 A-E", "Pooyan.mra")
	require.NoError(t, os.Symlink(canonicalPath, siblingAlias))
	require.NoError(t, os.Symlink(coresDir, filepath.Join(siblingDir, "cores")))

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)
	platform := installArcadeOrganizerLauncher(t, cfg, rootDir)

	matcher := helpers.NewLauncherMatcher(cfg, platform)
	assert.False(t, matcher.MatchSystemFileForScan(systemdefs.SystemArcade, siblingAlias),
		"a folder whose name merely starts with _Arcade is not part of the Arcade scan root")

	files, err := GetFiles(context.Background(), cfg, platform, systemdefs.SystemArcade, arcadeDir, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{canonicalPath}, files)
}
