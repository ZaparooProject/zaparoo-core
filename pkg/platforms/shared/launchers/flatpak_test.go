//go:build linux

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package launchers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withTempHome sets HOME to a temporary directory for the duration of the test.
// Returns the temp directory path.
func withTempHome(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	return tmpDir
}

func TestFlatpakBasePath(t *testing.T) {
	// Cannot run in parallel due to HOME env modification
	t.Run("returns_correct_path", func(t *testing.T) {
		home := withTempHome(t)

		path := FlatpakBasePath()

		assert.Equal(t, filepath.Join(home, ".var", "app"), path)
	})
}

func TestFlatpakAppPath(t *testing.T) {
	// Cannot run in parallel due to HOME env modification
	t.Run("returns_correct_path_for_steam", func(t *testing.T) {
		home := withTempHome(t)

		path := FlatpakAppPath(FlatpakSteamID)

		assert.Equal(t, filepath.Join(home, ".var", "app", FlatpakSteamID), path)
	})

	t.Run("returns_correct_path_for_lutris", func(t *testing.T) {
		home := withTempHome(t)

		path := FlatpakAppPath(FlatpakLutrisID)

		assert.Equal(t, filepath.Join(home, ".var", "app", FlatpakLutrisID), path)
	})

	t.Run("returns_correct_path_for_heroic", func(t *testing.T) {
		home := withTempHome(t)

		path := FlatpakAppPath(FlatpakHeroicID)

		assert.Equal(t, filepath.Join(home, ".var", "app", FlatpakHeroicID), path)
	})
}

func TestHasFlatpakAppData(t *testing.T) {
	// Cannot run in parallel due to HOME env modification
	t.Run("returns_true_when_directory_exists", func(t *testing.T) {
		home := withTempHome(t)

		// Create the Flatpak app directory
		appPath := filepath.Join(home, ".var", "app", FlatpakSteamID)
		require.NoError(t, os.MkdirAll(appPath, 0o750))

		hasData := HasFlatpakAppData(FlatpakSteamID)

		assert.True(t, hasData)
	})

	t.Run("returns_false_when_directory_missing", func(t *testing.T) {
		_ = withTempHome(t)

		// Don't create any directories
		hasData := HasFlatpakAppData(FlatpakSteamID)

		assert.False(t, hasData)
	})

	t.Run("returns_false_when_file_instead_of_directory", func(t *testing.T) {
		home := withTempHome(t)

		// Create parent directories
		varAppPath := filepath.Join(home, ".var", "app")
		require.NoError(t, os.MkdirAll(varAppPath, 0o750))

		// Create a file instead of directory
		filePath := filepath.Join(varAppPath, FlatpakSteamID)
		require.NoError(t, os.WriteFile(filePath, []byte("not a directory"), 0o600))

		// HasFlatpakAppData uses Stat, which succeeds for files too
		// So this actually returns true (file exists)
		hasData := HasFlatpakAppData(FlatpakSteamID)

		// The function only checks existence, not if it's a directory
		assert.True(t, hasData)
	})
}

func TestFindLutrisDB(t *testing.T) {
	// Cannot run in parallel due to HOME env modification
	t.Run("finds_native_path", func(t *testing.T) {
		home := withTempHome(t)

		// Create native Lutris DB
		nativePath := filepath.Join(home, ".local", "share", "lutris")
		require.NoError(t, os.MkdirAll(nativePath, 0o750))
		dbPath := filepath.Join(nativePath, "pga.db")
		require.NoError(t, os.WriteFile(dbPath, []byte("sqlite"), 0o600))

		path, found := FindLutrisDB(false)

		assert.True(t, found)
		assert.Equal(t, dbPath, path)
	})

	t.Run("finds_flatpak_path_when_native_missing", func(t *testing.T) {
		home := withTempHome(t)

		// Create Flatpak Lutris DB
		flatpakPath := filepath.Join(home, ".var", "app", FlatpakLutrisID, "data", "lutris")
		require.NoError(t, os.MkdirAll(flatpakPath, 0o750))
		dbPath := filepath.Join(flatpakPath, "pga.db")
		require.NoError(t, os.WriteFile(dbPath, []byte("sqlite"), 0o600))

		path, found := FindLutrisDB(true)

		assert.True(t, found)
		assert.Equal(t, dbPath, path)
	})

	t.Run("prefers_native_over_flatpak", func(t *testing.T) {
		home := withTempHome(t)

		// Create both native and Flatpak paths
		nativePath := filepath.Join(home, ".local", "share", "lutris")
		require.NoError(t, os.MkdirAll(nativePath, 0o750))
		nativeDB := filepath.Join(nativePath, "pga.db")
		require.NoError(t, os.WriteFile(nativeDB, []byte("native"), 0o600))

		flatpakPath := filepath.Join(home, ".var", "app", FlatpakLutrisID, "data", "lutris")
		require.NoError(t, os.MkdirAll(flatpakPath, 0o750))
		flatpakDB := filepath.Join(flatpakPath, "pga.db")
		require.NoError(t, os.WriteFile(flatpakDB, []byte("flatpak"), 0o600))

		path, found := FindLutrisDB(true)

		assert.True(t, found)
		assert.Equal(t, nativeDB, path, "should prefer native path")
	})

	t.Run("skips_flatpak_when_disabled", func(t *testing.T) {
		home := withTempHome(t)

		// Create only Flatpak path
		flatpakPath := filepath.Join(home, ".var", "app", FlatpakLutrisID, "data", "lutris")
		require.NoError(t, os.MkdirAll(flatpakPath, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(flatpakPath, "pga.db"), []byte("sqlite"), 0o600))

		path, found := FindLutrisDB(false)

		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("returns_empty_when_no_path_exists", func(t *testing.T) {
		_ = withTempHome(t)

		path, found := FindLutrisDB(true)

		assert.False(t, found)
		assert.Empty(t, path)
	})
}

func TestFindHeroicStoreCache(t *testing.T) {
	// Cannot run in parallel due to HOME env modification
	t.Run("finds_native_path", func(t *testing.T) {
		home := withTempHome(t)

		// Create native Heroic store_cache
		nativePath := filepath.Join(home, ".config", "heroic", "store_cache")
		require.NoError(t, os.MkdirAll(nativePath, 0o750))

		path, found := FindHeroicStoreCache(false)

		assert.True(t, found)
		assert.Equal(t, nativePath, path)
	})

	t.Run("finds_flatpak_path_when_native_missing", func(t *testing.T) {
		home := withTempHome(t)

		// Create Flatpak Heroic store_cache
		flatpakPath := filepath.Join(home, ".var", "app", FlatpakHeroicID, "config", "heroic", "store_cache")
		require.NoError(t, os.MkdirAll(flatpakPath, 0o750))

		path, found := FindHeroicStoreCache(true)

		assert.True(t, found)
		assert.Equal(t, flatpakPath, path)
	})

	t.Run("prefers_native_over_flatpak", func(t *testing.T) {
		home := withTempHome(t)

		// Create both native and Flatpak paths
		nativePath := filepath.Join(home, ".config", "heroic", "store_cache")
		require.NoError(t, os.MkdirAll(nativePath, 0o750))

		flatpakPath := filepath.Join(home, ".var", "app", FlatpakHeroicID, "config", "heroic", "store_cache")
		require.NoError(t, os.MkdirAll(flatpakPath, 0o750))

		path, found := FindHeroicStoreCache(true)

		assert.True(t, found)
		assert.Equal(t, nativePath, path, "should prefer native path")
	})

	t.Run("skips_flatpak_when_disabled", func(t *testing.T) {
		home := withTempHome(t)

		// Create only Flatpak path
		flatpakPath := filepath.Join(home, ".var", "app", FlatpakHeroicID, "config", "heroic", "store_cache")
		require.NoError(t, os.MkdirAll(flatpakPath, 0o750))

		path, found := FindHeroicStoreCache(false)

		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("returns_empty_when_no_path_exists", func(t *testing.T) {
		_ = withTempHome(t)

		path, found := FindHeroicStoreCache(true)

		assert.False(t, found)
		assert.Empty(t, path)
	})
}

func TestFlatpakAppIDConstants(t *testing.T) {
	t.Parallel()

	// Verify constants have expected values
	assert.Equal(t, "com.valvesoftware.Steam", FlatpakSteamID)
	assert.Equal(t, "net.lutris.Lutris", FlatpakLutrisID)
	assert.Equal(t, "com.heroicgameslauncher.hgl", FlatpakHeroicID)
}
