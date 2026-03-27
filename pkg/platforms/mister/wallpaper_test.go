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

package mister

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPaths(t *testing.T) wallpaperPaths {
	t.Helper()
	dir := t.TempDir()
	return wallpaperPaths{
		sdRoot:        dir,
		wallpapersDir: filepath.Join(dir, "wallpapers"),
		menuCfgFile:   filepath.Join(dir, "config", "MENU.CFG"),
	}
}

// --- cleanupMenuFile ---

func TestCleanupMenuFile_NotExists(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	err := cleanupMenuFile(filepath.Join(paths.sdRoot, "menu.png"), paths.wallpapersDir)
	assert.NoError(t, err)
}

func TestCleanupMenuFile_Symlink(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	require.NoError(t, os.MkdirAll(paths.wallpapersDir, 0o750))
	target := filepath.Join(paths.wallpapersDir, "target.png")
	//nolint:gosec // G306: test file in t.TempDir()
	require.NoError(t, os.WriteFile(target, []byte("img"), 0o644))

	link := filepath.Join(paths.sdRoot, "menu.png")
	require.NoError(t, os.Symlink(target, link))

	err := cleanupMenuFile(link, paths.wallpapersDir)
	require.NoError(t, err)

	_, statErr := os.Lstat(link)
	assert.True(t, os.IsNotExist(statErr), "symlink should be removed")

	_, statErr = os.Stat(target)
	assert.NoError(t, statErr, "target file should still exist")
}

func TestCleanupMenuFile_RegularFile(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	menuFile := filepath.Join(paths.sdRoot, "menu.png")
	//nolint:gosec // G306: test file in t.TempDir()
	require.NoError(t, os.WriteFile(menuFile, []byte("user wallpaper"), 0o644))

	err := cleanupMenuFile(menuFile, paths.wallpapersDir)
	require.NoError(t, err)

	// Original file should be gone
	_, statErr := os.Lstat(menuFile)
	assert.True(t, os.IsNotExist(statErr), "original file should be moved")

	// File should be in wallpapers dir with timestamp suffix
	entries, err := os.ReadDir(paths.wallpapersDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "should have one file in wallpapers dir")
	assert.True(t, strings.HasPrefix(entries[0].Name(), "menu_"), "moved file should have menu_ prefix")
	assert.True(t, strings.HasSuffix(entries[0].Name(), ".png"), "moved file should keep .png extension")
}

// --- setBackgroundMode ---

func TestSetBackgroundMode(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	require.NoError(t, os.MkdirAll(filepath.Dir(paths.menuCfgFile), 0o750))
	//nolint:gosec // G306: test file in t.TempDir()
	require.NoError(t, os.WriteFile(paths.menuCfgFile, []byte{0, 1, 2, 3}, 0o644))

	err := setBackgroundMode(paths.menuCfgFile, backgroundModeWallpaper)
	require.NoError(t, err)

	//nolint:gosec // G304: test file path from t.TempDir()
	result, err := os.ReadFile(paths.menuCfgFile)
	require.NoError(t, err)
	assert.Equal(t, backgroundModeWallpaper, result[0])
	assert.Equal(t, byte(1), result[1], "rest of file should be preserved")
	assert.Equal(t, byte(2), result[2])
	assert.Equal(t, byte(3), result[3])
}

func TestSetBackgroundMode_CreatesFile(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	err := setBackgroundMode(paths.menuCfgFile, backgroundModeWallpaper)
	require.NoError(t, err)

	//nolint:gosec // G304: test file path from t.TempDir()
	result, err := os.ReadFile(paths.menuCfgFile)
	require.NoError(t, err)
	assert.Equal(t, []byte{backgroundModeWallpaper}, result)
}

// --- setWallpaper ---

func TestSetWallpaper_InvalidExtension(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	_, err := setWallpaper(paths, "wallpaper.bmp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported wallpaper format")
}

func TestSetWallpaper_MissingFile(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	_, err := setWallpaper(paths, "nonexistent.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wallpaper file not found")
}

func TestSetWallpaper_PathTraversal(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	_, err := setWallpaper(paths, "../../etc/passwd.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid wallpaper filename")
}

func TestSetWallpaper_Success(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	require.NoError(t, os.MkdirAll(paths.wallpapersDir, 0o750))
	//nolint:gosec // G306: test file in t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(paths.wallpapersDir, "cool.png"),
		[]byte("png data"), 0o644,
	))

	_, err := setWallpaper(paths, "cool.png")
	require.NoError(t, err)

	// Verify symlink was created
	link := filepath.Join(paths.sdRoot, "menu.png")
	fi, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "menu.png should be a symlink")

	target, err := os.Readlink(link)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(paths.wallpapersDir, "cool.png"), target)

	// Verify MENU.CFG was set to wallpaper mode
	//nolint:gosec // G304: test file path from t.TempDir()
	cfgData, err := os.ReadFile(paths.menuCfgFile)
	require.NoError(t, err)
	assert.Equal(t, backgroundModeWallpaper, cfgData[0])
}

func TestSetWallpaper_JPG(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	require.NoError(t, os.MkdirAll(paths.wallpapersDir, 0o750))
	//nolint:gosec // G306: test file in t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(paths.wallpapersDir, "cool.jpg"),
		[]byte("jpg data"), 0o644,
	))

	_, err := setWallpaper(paths, "cool.jpg")
	require.NoError(t, err)

	link := filepath.Join(paths.sdRoot, "menu.jpg")
	fi, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "menu.jpg should be a symlink")
}

func TestSetWallpaper_CleansUpExisting(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	require.NoError(t, os.MkdirAll(paths.wallpapersDir, 0o750))

	// Create an existing symlink at menu.png
	oldTarget := filepath.Join(paths.wallpapersDir, "old.png")
	//nolint:gosec // G306: test file in t.TempDir()
	require.NoError(t, os.WriteFile(oldTarget, []byte("old"), 0o644))
	require.NoError(t, os.Symlink(oldTarget, filepath.Join(paths.sdRoot, "menu.png")))

	// Set new wallpaper
	newTarget := filepath.Join(paths.wallpapersDir, "new.png")
	//nolint:gosec // G306: test file in t.TempDir()
	require.NoError(t, os.WriteFile(newTarget, []byte("new"), 0o644))

	_, err := setWallpaper(paths, "new.png")
	require.NoError(t, err)

	// New symlink should point to new file
	target, err := os.Readlink(filepath.Join(paths.sdRoot, "menu.png"))
	require.NoError(t, err)
	assert.Equal(t, newTarget, target)
}

// --- unsetWallpaper ---

func TestUnsetWallpaper_RemovesSymlink(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	require.NoError(t, os.MkdirAll(paths.wallpapersDir, 0o750))
	target := filepath.Join(paths.wallpapersDir, "bg.png")
	//nolint:gosec // G306: test file in t.TempDir()
	require.NoError(t, os.WriteFile(target, []byte("img"), 0o644))
	require.NoError(t, os.Symlink(target, filepath.Join(paths.sdRoot, "menu.png")))

	_, err := unsetWallpaper(paths)
	require.NoError(t, err)

	_, statErr := os.Lstat(filepath.Join(paths.sdRoot, "menu.png"))
	assert.True(t, os.IsNotExist(statErr), "symlink should be removed")

	// Verify MENU.CFG was reset to none mode
	//nolint:gosec // G304: test file path from t.TempDir()
	cfgData, err := os.ReadFile(paths.menuCfgFile)
	require.NoError(t, err)
	assert.Equal(t, backgroundModeNone, cfgData[0])
}

func TestUnsetWallpaper_SkipsRegularFile(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	menuFile := filepath.Join(paths.sdRoot, "menu.png")
	//nolint:gosec // G306: test file in t.TempDir()
	require.NoError(t, os.WriteFile(menuFile, []byte("user file"), 0o644))

	_, err := unsetWallpaper(paths)
	require.NoError(t, err)

	// Regular file should NOT be removed
	_, statErr := os.Stat(menuFile)
	assert.NoError(t, statErr, "regular file should be preserved")
}

func TestUnsetWallpaper_NoFiles(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	_, err := unsetWallpaper(paths)
	assert.NoError(t, err)
}
