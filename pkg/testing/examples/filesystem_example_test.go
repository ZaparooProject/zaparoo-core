// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package examples

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFilesystemHelperUsage demonstrates how to use filesystem helpers for testing
func TestFilesystemHelperUsage(t *testing.T) {
	t.Parallel()
	t.Run("Basic Memory Filesystem Operations", func(t *testing.T) {
		t.Parallel()
		// Create in-memory filesystem helper
		fs := helpers.NewMemoryFS()

		// Test file creation and reading
		testPath := "/test/config.json"
		testContent := []byte(`{"test": "value"}`)

		err := fs.WriteFile(testPath, testContent, 0o644)
		require.NoError(t, err)

		// Verify file exists
		assert.True(t, fs.FileExists(testPath))

		// Read file content
		content, err := fs.ReadFile(testPath)
		require.NoError(t, err)
		assert.Equal(t, testContent, content)

		// List files in directory
		files, err := fs.ListFiles("/test")
		require.NoError(t, err)
		assert.Contains(t, files, "config.json")
	})

	t.Run("Config File Creation", func(t *testing.T) {
		t.Parallel()
		fs := helpers.NewMemoryFS()

		// Create config with map
		config := map[string]any{
			"service": map[string]any{
				"api_port": 7497,
			},
			"readers": map[string]any{
				"polling": 500,
			},
		}

		configPath := "/config/zaparoo.json"
		err := fs.CreateConfigFile(configPath, config)
		require.NoError(t, err)

		// Verify config file was created
		assert.True(t, fs.FileExists(configPath))

		// Read and verify content
		content, err := fs.ReadFile(configPath)
		require.NoError(t, err)
		assert.Contains(t, string(content), "api_port")
		assert.Contains(t, string(content), "7497")
	})

	t.Run("Auth File Creation", func(t *testing.T) {
		t.Parallel()
		fs := helpers.NewMemoryFS()

		authData := []byte(`{"token": "test-auth-token", "expires": "2025-12-31"}`)
		authPath := "/config/auth.json"

		err := fs.CreateAuthFile(authPath, authData)
		require.NoError(t, err)

		assert.True(t, fs.FileExists(authPath))

		content, err := fs.ReadFile(authPath)
		require.NoError(t, err)
		assert.Equal(t, authData, content)
	})

	t.Run("Media Directory Structure", func(t *testing.T) {
		t.Parallel()
		fs := helpers.NewMemoryFS()

		mediaPath := "/media"
		err := fs.CreateMediaDirectory(mediaPath)
		require.NoError(t, err)

		// Verify system directories were created
		systems := []string{
			"Atari - 2600",
			"Nintendo - Game Boy",
			"Nintendo - Nintendo Entertainment System",
			"Sega - Master System - Mark III",
			"Sony - PlayStation",
		}

		for _, system := range systems {
			systemPath := filepath.Join(mediaPath, system)
			assert.True(t, fs.FileExists(systemPath))

			// Verify sample games were created
			files, err := fs.ListFiles(systemPath)
			require.NoError(t, err)
			assert.Len(t, files, 3) // Should have 3 sample games
			assert.Contains(t, files, "Game 1.zip")
		}
	})

	t.Run("Complete Test Environment Setup", func(t *testing.T) {
		t.Parallel()
		fs := helpers.NewMemoryFS()

		baseDir := "/test-env"
		config, err := fs.SetupTestConfigEnvironment(baseDir)
		require.NoError(t, err)

		// Verify config structure
		assert.Contains(t, config, "service")
		assert.Contains(t, config, "readers")
		assert.Contains(t, config, "media_folder")
		assert.Contains(t, config, "database")

		// Verify directories were created
		assert.True(t, fs.FileExists(filepath.Join(baseDir, "config")))
		assert.True(t, fs.FileExists(filepath.Join(baseDir, "config", "config.json")))

		// Verify media directory structure
		mediaFolder, ok := config["media_folder"].(map[string]any)
		if !ok {
			t.Fatal("media_folder not found or not a map")
		}
		mediaPath, ok := mediaFolder["path"].(string)
		if !ok {
			t.Fatal("media_folder path not found or not a string")
		}
		assert.True(t, fs.FileExists(mediaPath))
		assert.True(t, fs.FileExists(filepath.Join(mediaPath, "Atari - 2600")))

		// Verify database directory
		database, ok := config["database"].(map[string]any)
		if !ok {
			t.Fatal("database not found or not a map")
		}
		dbPath, ok := database["path"].(string)
		if !ok {
			t.Fatal("database path not found or not a string")
		}
		assert.True(t, fs.FileExists(dbPath))
	})
}

// TestFilesystemStructureHelpers demonstrates the predefined directory structure helpers
func TestFilesystemStructureHelpers(t *testing.T) {
	t.Parallel()
	t.Run("Basic Test Structure Definition", func(t *testing.T) {
		t.Parallel()
		// Test that the structure definitions are available
		basicStructure := helpers.GetBasicTestStructure()
		assert.Contains(t, basicStructure, "config")
		assert.Contains(t, basicStructure, "media")
		assert.Contains(t, basicStructure, "database")
		assert.Contains(t, basicStructure, "logs")

		// Verify config content
		config, ok := basicStructure["config"].(map[string]any)
		if !ok {
			t.Fatal("config not found or not a map")
		}
		assert.Contains(t, config, "config.json")
	})

	t.Run("Complex Test Structure Definition", func(t *testing.T) {
		t.Parallel()
		// Test that the complex structure definitions are available
		complexStructure := helpers.GetComplexTestStructure()
		assert.Contains(t, complexStructure, "config")
		assert.Contains(t, complexStructure, "media")
		assert.Contains(t, complexStructure, "database")
		assert.Contains(t, complexStructure, "logs")
		assert.Contains(t, complexStructure, "tmp")

		// Verify more complex structure
		config, ok := complexStructure["config"].(map[string]any)
		if !ok {
			t.Fatal("config not found or not a map")
		}
		assert.Contains(t, config, "config.json")
		assert.Contains(t, config, "auth.json")

		media, ok := complexStructure["media"].(map[string]any)
		if !ok {
			t.Fatal("media not found or not a map")
		}
		atari, ok := media["Atari - 2600"].(map[string]any)
		if !ok {
			t.Fatal("Atari - 2600 not found or not a map")
		}
		assert.Contains(t, atari, "Action")
		assert.Contains(t, atari, "Sports")
	})
}
