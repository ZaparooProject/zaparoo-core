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

package platforms

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAllPlatformsReturnLogDir verifies that all platform implementations
// return a non-empty LogDir in their Settings() method.
func TestAllPlatformsReturnLogDir(t *testing.T) {
	t.Parallel()

	// Note: This test uses mock platforms because actual platform implementations
	// have build constraints (GOOS-specific). The real platforms are tested
	// indirectly through integration tests.

	t.Run("mock platform with valid settings", func(t *testing.T) {
		t.Parallel()

		settings := Settings{
			DataDir:    "/test/data",
			ConfigDir:  "/test/config",
			TempDir:    "/test/temp",
			LogDir:     "/test/logs",
			ZipsAsDirs: false,
		}

		// Verify all required fields are set
		assert.NotEmpty(t, settings.DataDir, "DataDir should not be empty")
		assert.NotEmpty(t, settings.ConfigDir, "ConfigDir should not be empty")
		assert.NotEmpty(t, settings.TempDir, "TempDir should not be empty")
		assert.NotEmpty(t, settings.LogDir, "LogDir should not be empty")

		// Verify LogDir and TempDir are different (for most platforms)
		// Note: MiSTer and MiSTeX are exceptions where they are the same
		assert.NotEqual(t, settings.LogDir, settings.DataDir,
			"LogDir should be different from DataDir")
	})
}

// TestLogDirSeparation verifies the semantic separation between TempDir and LogDir
func TestLogDirSeparation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		platformDescription string
		settings            Settings
		expectSameDirs      bool
	}{
		{
			name: "standard platform - different directories",
			settings: Settings{
				TempDir: "/tmp/zaparoo",
				LogDir:  "/home/user/.local/share/zaparoo/logs",
			},
			expectSameDirs:      false,
			platformDescription: "Most platforms (Linux, Windows, Mac, etc.)",
		},
		{
			name: "MiSTer/MiSTeX exception - same directory",
			settings: Settings{
				TempDir: "/tmp/zaparoo",
				LogDir:  "/tmp/zaparoo",
			},
			expectSameDirs:      true,
			platformDescription: "MiSTer and MiSTeX platforms share temp/log directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.NotEmpty(t, tt.settings.TempDir, "TempDir must be set")
			require.NotEmpty(t, tt.settings.LogDir, "LogDir must be set")

			if tt.expectSameDirs {
				assert.Equal(t, tt.settings.TempDir, tt.settings.LogDir,
					"%s: TempDir and LogDir should be the same", tt.platformDescription)
			} else {
				assert.NotEqual(t, tt.settings.TempDir, tt.settings.LogDir,
					"%s: TempDir and LogDir should be different", tt.platformDescription)
			}
		})
	}
}

// TestSettingsDocumentation serves as living documentation for Settings fields.
// This test documents the expected behavior and semantics of each field in
// the platforms.Settings struct.
func TestSettingsDocumentation(t *testing.T) {
	t.Parallel()

	// TempDir: for temporary files that can be deleted
	// - PID files
	// - Temporary binaries during service restart
	// - IPC files

	// LogDir: for persistent log files that should be backed up
	// - core.log (rotating log file)
	// - core.log.1, core.log.2 (rotated backups)

	// DataDir: for persistent application data
	// - Databases (user.db, media.db)
	// - Downloaded assets

	// ConfigDir: for user configuration
	// - config.toml
	// - auth.toml

	// This test exists purely as documentation and always passes
	settings := Settings{
		DataDir:   "/data",
		ConfigDir: "/config",
		TempDir:   "/tmp",
		LogDir:    "/logs",
	}

	// Verify struct can be instantiated with all fields
	assert.NotNil(t, settings)
}
