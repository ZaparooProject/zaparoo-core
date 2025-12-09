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

package chimeraos

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindChimeraExecutable(t *testing.T) {
	t.Parallel()

	t.Run("finds_start_sh", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()

		// Create start.sh file
		startPath := filepath.Join(tmpDir, "start.sh")
		err := os.WriteFile(startPath, []byte("#!/bin/bash\necho test"), 0o600)
		require.NoError(t, err)

		result := findChimeraExecutable(tmpDir)
		assert.Equal(t, startPath, result)
	})

	t.Run("finds_start_without_extension", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()

		// Create start file (no extension)
		startPath := filepath.Join(tmpDir, "start")
		err := os.WriteFile(startPath, []byte("#!/bin/bash\necho test"), 0o600)
		require.NoError(t, err)

		result := findChimeraExecutable(tmpDir)
		assert.Equal(t, startPath, result)
	})

	t.Run("finds_run_sh", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()

		// Create run.sh file
		runPath := filepath.Join(tmpDir, "run.sh")
		err := os.WriteFile(runPath, []byte("#!/bin/bash\necho test"), 0o600)
		require.NoError(t, err)

		result := findChimeraExecutable(tmpDir)
		assert.Equal(t, runPath, result)
	})

	t.Run("finds_launch_sh", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()

		// Create launch.sh file
		launchPath := filepath.Join(tmpDir, "launch.sh")
		err := os.WriteFile(launchPath, []byte("#!/bin/bash\necho test"), 0o600)
		require.NoError(t, err)

		result := findChimeraExecutable(tmpDir)
		assert.Equal(t, launchPath, result)
	})

	t.Run("prefers_start_sh_over_others", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()

		// Create multiple executable files
		startPath := filepath.Join(tmpDir, "start.sh")
		err := os.WriteFile(startPath, []byte("#!/bin/bash\necho start"), 0o600)
		require.NoError(t, err)

		runPath := filepath.Join(tmpDir, "run.sh")
		err = os.WriteFile(runPath, []byte("#!/bin/bash\necho run"), 0o600)
		require.NoError(t, err)

		// start.sh should be preferred (first in list)
		result := findChimeraExecutable(tmpDir)
		assert.Equal(t, startPath, result)
	})

	t.Run("returns_empty_when_no_executable_found", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()

		// Create a file that's not in the expected list
		otherPath := filepath.Join(tmpDir, "game.exe")
		err := os.WriteFile(otherPath, []byte("not a script"), 0o600)
		require.NoError(t, err)

		result := findChimeraExecutable(tmpDir)
		assert.Empty(t, result)
	})

	t.Run("returns_empty_for_nonexistent_directory", func(t *testing.T) {
		t.Parallel()

		result := findChimeraExecutable("/nonexistent/path/to/game")
		assert.Empty(t, result)
	})

	t.Run("ignores_directories_with_executable_names", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()

		// Create a directory named start.sh
		dirPath := filepath.Join(tmpDir, "start.sh")
		err := os.Mkdir(dirPath, 0o750)
		require.NoError(t, err)

		result := findChimeraExecutable(tmpDir)
		assert.Empty(t, result)
	})
}

func TestNewChimeraGOGLauncher(t *testing.T) {
	t.Parallel()

	launcher := NewChimeraGOGLauncher()

	t.Run("has_correct_id", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "ChimeraGOG", launcher.ID)
	})

	t.Run("has_correct_system_id", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, systemdefs.SystemPC, launcher.SystemID)
	})

	t.Run("supports_gog_scheme", func(t *testing.T) {
		t.Parallel()
		assert.Contains(t, launcher.Schemes, shared.SchemeGOG)
	})

	t.Run("has_scanner_function", func(t *testing.T) {
		t.Parallel()
		assert.NotNil(t, launcher.Scanner)
	})

	t.Run("has_launch_function", func(t *testing.T) {
		t.Parallel()
		assert.NotNil(t, launcher.Launch)
	})
}

func TestChimeraGOGLauncherPathTraversalProtection(t *testing.T) {
	t.Parallel()

	launcher := NewChimeraGOGLauncher()

	// Test path traversal attack prevention
	testCases := []struct {
		name      string
		path      string
		expectErr bool
	}{
		{
			name:      "path_traversal_with_double_dots",
			path:      "gog://../../../etc/passwd/exploit",
			expectErr: true,
		},
		{
			name:      "path_traversal_with_encoded_dots",
			path:      "gog://..%2F..%2F..%2Fetc%2Fpasswd/exploit",
			expectErr: true,
		},
		{
			name:      "single_dot_path",
			path:      "gog://./game",
			expectErr: true,
		},
		{
			name:      "double_dot_path",
			path:      "gog://../game",
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := launcher.Launch(nil, tc.path)
			if tc.expectErr {
				require.Error(t, err)
				// The error should indicate an invalid game ID
				assert.Contains(t, err.Error(), "invalid GOG game ID")
			}
		})
	}
}

func TestChimeraGOGLauncherValidGameID(t *testing.T) {
	t.Parallel()

	launcher := NewChimeraGOGLauncher()

	// Test valid game IDs that should pass validation but may still fail
	// due to the game not being installed
	testCases := []struct {
		name   string
		path   string
		gameID string
	}{
		{
			name:   "simple_numeric_id",
			path:   "gog://123456789/GameName",
			gameID: "123456789",
		},
		{
			name:   "alphanumeric_id",
			path:   "gog://game_id_123/GameName",
			gameID: "game_id_123",
		},
		{
			name:   "simple_name_id",
			path:   "gog://mygame/MyGame",
			gameID: "mygame",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := launcher.Launch(nil, tc.path)
			// Should fail because game isn't installed, not because of validation
			require.Error(t, err)
			// Should NOT be an invalid game ID error
			assert.NotContains(t, err.Error(), "invalid GOG game ID")
		})
	}
}

func TestChimeraExecutableNames(t *testing.T) {
	t.Parallel()

	// Verify the expected executable names are in the list
	expectedNames := []string{"start.sh", "start", "run.sh", "launch.sh"}

	assert.Equal(t, expectedNames, chimeraExecutableNames)
}
