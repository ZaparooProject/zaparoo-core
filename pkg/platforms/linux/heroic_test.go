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

package linux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanHeroicGames(t *testing.T) {
	t.Parallel()

	t.Run("directory_not_found", func(t *testing.T) {
		t.Parallel()

		results, err := ScanHeroicGames("/nonexistent/path/store_cache")
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("library_with_games", func(t *testing.T) {
		t.Parallel()

		// Create temporary directory structure
		tmpDir := t.TempDir()
		storeCacheDir := filepath.Join(tmpDir, "store_cache")
		require.NoError(t, os.MkdirAll(storeCacheDir, 0o750))

		// Create Epic Games library file (legendary_library.json)
		epicLibrary := `{
			"library": [
				{
					"app_name": "Fortnite",
					"title": "Fortnite",
					"is_installed": true,
					"runner": "legendary"
				},
				{
					"app_name": "RocketLeague",
					"title": "Rocket League",
					"is_installed": false,
					"runner": "legendary"
				},
				{
					"app_name": "Hades",
					"title": "Hades",
					"is_installed": true,
					"runner": "legendary"
				},
				{
					"app_name": "",
					"title": "No App Name Game",
					"is_installed": true,
					"runner": "legendary"
				}
			]
		}`
		epicPath := filepath.Join(storeCacheDir, "legendary_library.json")
		require.NoError(t, os.WriteFile(epicPath, []byte(epicLibrary), 0o600))

		// Create GOG library file (gog_library.json)
		gogLibrary := `{
			"games": [
				{
					"app_name": "1207664543",
					"title": "Cyberpunk 2077",
					"is_installed": true,
					"runner": "gog"
				},
				{
					"app_name": "1207658924",
					"title": "The Witcher 3: Wild Hunt",
					"is_installed": false,
					"runner": "gog"
				},
				{
					"app_name": "1441974651",
					"title": "Stardew Valley",
					"is_installed": true,
					"runner": "gog"
				}
			]
		}`
		gogPath := filepath.Join(storeCacheDir, "gog_library.json")
		require.NoError(t, os.WriteFile(gogPath, []byte(gogLibrary), 0o600))

		// Scan the library
		results, err := ScanHeroicGames(storeCacheDir)
		require.NoError(t, err)

		// Should find 4 installed games (2 Epic + 2 GOG)
		// Excludes: RocketLeague (not installed), Witcher 3 (not installed), No App Name Game (no app_name)
		assert.Len(t, results, 4)

		// Verify results
		expectedGames := map[string]string{
			"Fortnite":       "Fortnite",
			"Hades":          "Hades",
			"Cyberpunk 2077": "1207664543",
			"Stardew Valley": "1441974651",
		}

		foundGames := make(map[string]bool)
		for _, result := range results {
			expectedAppName, exists := expectedGames[result.Name]
			assert.True(t, exists, "unexpected game found: %s", result.Name)
			assert.False(t, foundGames[result.Name], "duplicate game found: %s", result.Name)
			foundGames[result.Name] = true

			// Verify path format: heroic://appName/title (with URL encoding)
			assert.True(t, strings.HasPrefix(result.Path, "heroic://"), "path should start with heroic://")
			assert.Contains(t, result.Path, expectedAppName, "path should contain app_name")
			assert.True(t, result.NoExt, "NoExt should be true for virtual paths")
		}

		// Verify all expected games were found
		for name := range expectedGames {
			assert.True(t, foundGames[name], "expected game not found: %s", name)
		}
	})

	t.Run("empty_library_files", func(t *testing.T) {
		t.Parallel()

		// Create temporary directory structure
		tmpDir := t.TempDir()
		storeCacheDir := filepath.Join(tmpDir, "store_cache")
		require.NoError(t, os.MkdirAll(storeCacheDir, 0o750))

		// Create empty library files
		epicLibrary := `{"library": []}`
		epicPath := filepath.Join(storeCacheDir, "legendary_library.json")
		require.NoError(t, os.WriteFile(epicPath, []byte(epicLibrary), 0o600))

		gogLibrary := `{"games": []}`
		gogPath := filepath.Join(storeCacheDir, "gog_library.json")
		require.NoError(t, os.WriteFile(gogPath, []byte(gogLibrary), 0o600))

		// Scan should return empty results
		results, err := ScanHeroicGames(storeCacheDir)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("missing_library_files", func(t *testing.T) {
		t.Parallel()

		// Create directory but no library files
		tmpDir := t.TempDir()
		storeCacheDir := filepath.Join(tmpDir, "store_cache")
		require.NoError(t, os.MkdirAll(storeCacheDir, 0o750))

		// Scan should return empty results (no error for missing files)
		results, err := ScanHeroicGames(storeCacheDir)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("only_epic_library", func(t *testing.T) {
		t.Parallel()

		// Create temporary directory structure
		tmpDir := t.TempDir()
		storeCacheDir := filepath.Join(tmpDir, "store_cache")
		require.NoError(t, os.MkdirAll(storeCacheDir, 0o750))

		// Create only Epic Games library file
		epicLibrary := `{
			"library": [
				{
					"app_name": "Fortnite",
					"title": "Fortnite",
					"is_installed": true,
					"runner": "legendary"
				}
			]
		}`
		epicPath := filepath.Join(storeCacheDir, "legendary_library.json")
		require.NoError(t, os.WriteFile(epicPath, []byte(epicLibrary), 0o600))

		// Scan should find Epic game only
		results, err := ScanHeroicGames(storeCacheDir)
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "Fortnite", results[0].Name)
	})

	t.Run("only_gog_library", func(t *testing.T) {
		t.Parallel()

		// Create temporary directory structure
		tmpDir := t.TempDir()
		storeCacheDir := filepath.Join(tmpDir, "store_cache")
		require.NoError(t, os.MkdirAll(storeCacheDir, 0o750))

		// Create only GOG library file
		gogLibrary := `{
			"games": [
				{
					"app_name": "1207664543",
					"title": "Cyberpunk 2077",
					"is_installed": true,
					"runner": "gog"
				}
			]
		}`
		gogPath := filepath.Join(storeCacheDir, "gog_library.json")
		require.NoError(t, os.WriteFile(gogPath, []byte(gogLibrary), 0o600))

		// Scan should find GOG game only
		results, err := ScanHeroicGames(storeCacheDir)
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "Cyberpunk 2077", results[0].Name)
	})
}

func TestScanHeroicLibraryFile(t *testing.T) {
	t.Parallel()

	t.Run("valid_epic_library", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "legendary_library.json")

		library := `{
			"library": [
				{
					"app_name": "TestGame",
					"title": "Test Game",
					"is_installed": true,
					"runner": "legendary"
				}
			]
		}`
		require.NoError(t, os.WriteFile(filePath, []byte(library), 0o600))

		results, err := scanHeroicLibraryFile(filePath, "library")
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "Test Game", results[0].Name)
	})

	t.Run("valid_gog_library", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "gog_library.json")

		library := `{
			"games": [
				{
					"app_name": "1234567890",
					"title": "GOG Game",
					"is_installed": true,
					"runner": "gog"
				}
			]
		}`
		require.NoError(t, os.WriteFile(filePath, []byte(library), 0o600))

		results, err := scanHeroicLibraryFile(filePath, "games")
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "GOG Game", results[0].Name)
	})

	t.Run("file_not_found", func(t *testing.T) {
		t.Parallel()

		results, err := scanHeroicLibraryFile("/nonexistent/path/library.json", "library")
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("malformed_json", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "bad_library.json")

		// Invalid JSON
		require.NoError(t, os.WriteFile(filePath, []byte("{invalid json}"), 0o600))

		results, err := scanHeroicLibraryFile(filePath, "library")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse Heroic library JSON")
		assert.Empty(t, results)
	})

	t.Run("missing_expected_key", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "library.json")

		// Valid JSON but wrong key
		library := `{"wrong_key": []}`
		require.NoError(t, os.WriteFile(filePath, []byte(library), 0o600))

		results, err := scanHeroicLibraryFile(filePath, "library")
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("filters_non_installed_games", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "library.json")

		library := `{
			"library": [
				{
					"app_name": "InstalledGame",
					"title": "Installed Game",
					"is_installed": true,
					"runner": "legendary"
				},
				{
					"app_name": "NotInstalledGame",
					"title": "Not Installed Game",
					"is_installed": false,
					"runner": "legendary"
				}
			]
		}`
		require.NoError(t, os.WriteFile(filePath, []byte(library), 0o600))

		results, err := scanHeroicLibraryFile(filePath, "library")
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "Installed Game", results[0].Name)
	})

	t.Run("filters_games_without_app_name", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "library.json")

		library := `{
			"library": [
				{
					"app_name": "ValidGame",
					"title": "Valid Game",
					"is_installed": true,
					"runner": "legendary"
				},
				{
					"app_name": "",
					"title": "No App Name",
					"is_installed": true,
					"runner": "legendary"
				}
			]
		}`
		require.NoError(t, os.WriteFile(filePath, []byte(library), 0o600))

		results, err := scanHeroicLibraryFile(filePath, "library")
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "Valid Game", results[0].Name)
	})
}
