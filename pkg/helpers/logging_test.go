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
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureDirectories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		errorContains string
		tempDirPerms  os.FileMode
		logDirPerms   os.FileMode
		setupDirs     bool
		expectError   bool
	}{
		{
			name:         "creates both directories successfully",
			setupDirs:    false,
			tempDirPerms: 0o750,
			logDirPerms:  0o750,
			expectError:  false,
		},
		{
			name:         "works when directories already exist",
			setupDirs:    true,
			tempDirPerms: 0o750,
			logDirPerms:  0o750,
			expectError:  false,
		},
		{
			name:         "creates nested directories",
			setupDirs:    false,
			tempDirPerms: 0o750,
			logDirPerms:  0o750,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create temp test directory
			testRoot := t.TempDir()
			tempDir := filepath.Join(testRoot, "temp", "nested")
			logDir := filepath.Join(testRoot, "logs", "nested")

			if tt.setupDirs {
				require.NoError(t, os.MkdirAll(tempDir, 0o750))
				require.NoError(t, os.MkdirAll(logDir, 0o750))
			}

			platform := mocks.NewMockPlatform()
			platform.On("Settings").Return(platforms.Settings{
				TempDir: tempDir,
				LogDir:  logDir,
			})

			// Execute
			err := EnsureDirectories(platform)

			// Verify
			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)

			// Verify directories exist
			tempInfo, err := os.Stat(tempDir)
			require.NoError(t, err, "TempDir should exist")
			assert.True(t, tempInfo.IsDir(), "TempDir should be a directory")

			logInfo, err := os.Stat(logDir)
			require.NoError(t, err, "LogDir should exist")
			assert.True(t, logInfo.IsDir(), "LogDir should be a directory")

			// Verify permissions (on Unix-like systems)
			if runtime.GOOS != "windows" {
				assert.Equal(t, os.FileMode(0o750), tempInfo.Mode().Perm(), "TempDir should have 0750 permissions")
				assert.Equal(t, os.FileMode(0o750), logInfo.Mode().Perm(), "LogDir should have 0750 permissions")
			}
		})
	}
}

func TestEnsureDirectoriesErrorHandling(t *testing.T) {
	t.Parallel()

	t.Run("fails when temp dir path is invalid", func(t *testing.T) {
		t.Parallel()

		platform := mocks.NewMockPlatform()
		platform.On("Settings").Return(platforms.Settings{
			TempDir: "/proc/invalid\x00path", // null byte makes it invalid
			LogDir:  t.TempDir(),
		})

		err := EnsureDirectories(platform)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create temp directory")
	})

	t.Run("fails when log dir path is invalid", func(t *testing.T) {
		t.Parallel()

		platform := mocks.NewMockPlatform()
		platform.On("Settings").Return(platforms.Settings{
			TempDir: t.TempDir(),
			LogDir:  "/proc/invalid\x00path", // null byte makes it invalid
		})

		err := EnsureDirectories(platform)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create log directory")
	})
}

func TestInitLogging(t *testing.T) {
	// Note: Cannot use t.Parallel() because InitLogging modifies global log.Logger

	t.Run("configures logging with LogDir path", func(t *testing.T) {
		testRoot := t.TempDir()
		tempDir := filepath.Join(testRoot, "temp")
		logDir := filepath.Join(testRoot, "logs")

		platform := mocks.NewMockPlatform()
		platform.On("Settings").Return(platforms.Settings{
			TempDir: tempDir,
			LogDir:  logDir,
		})

		// Ensure directories exist first
		err := EnsureDirectories(platform)
		require.NoError(t, err)

		// Initialize logging - should succeed
		err = InitLogging(platform, nil)
		require.NoError(t, err)

		// Note: We don't check for log file existence because lumberjack
		// creates it lazily (only when something is logged). The important
		// thing is that InitLogging configured the path correctly.
		// The integration tests verify actual logging works.
	})

	t.Run("works with additional writers", func(t *testing.T) {
		testRoot := t.TempDir()
		logDir := filepath.Join(testRoot, "logs")

		platform := mocks.NewMockPlatform()
		platform.On("Settings").Return(platforms.Settings{
			TempDir: filepath.Join(testRoot, "temp"),
			LogDir:  logDir,
		})

		err := EnsureDirectories(platform)
		require.NoError(t, err)

		// Create a dummy writer
		dummyWriter := &testWriter{}

		// Should not error with additional writers
		err = InitLogging(platform, []io.Writer{dummyWriter})
		require.NoError(t, err)
	})
}

// testWriter is a no-op io.Writer for testing
type testWriter struct{}

func (*testWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func TestInitLoggingIntegration(t *testing.T) {
	// Note: Cannot use t.Parallel() because InitLogging modifies global log.Logger

	t.Run("full startup sequence", func(t *testing.T) {
		testRoot := t.TempDir()
		platform := mocks.NewMockPlatform()
		platform.On("Settings").Return(platforms.Settings{
			TempDir: filepath.Join(testRoot, "temp"),
			LogDir:  filepath.Join(testRoot, "logs"),
		})

		// Step 1: Ensure directories (as done in cli.Setup)
		err := EnsureDirectories(platform)
		require.NoError(t, err, "EnsureDirectories should succeed")

		// Step 2: Initialize logging (as done in cli.Setup)
		err = InitLogging(platform, nil)
		require.NoError(t, err, "InitLogging should succeed")

		// Step 3: Verify both directories exist and are separate
		tempInfo, err := os.Stat(platform.Settings().TempDir)
		require.NoError(t, err)
		assert.True(t, tempInfo.IsDir())

		logInfo, err := os.Stat(platform.Settings().LogDir)
		require.NoError(t, err)
		assert.True(t, logInfo.IsDir())

		// Step 4: Verify directories are different
		assert.NotEqual(t, platform.Settings().TempDir, platform.Settings().LogDir,
			"TempDir and LogDir should be different directories")
	})
}
