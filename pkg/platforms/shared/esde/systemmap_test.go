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

package esde

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupByFolderName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		folder     string
		wantID     string
		wantExists bool
	}{
		{
			name:       "nes folder",
			folder:     "nes",
			wantID:     systemdefs.SystemNES,
			wantExists: true,
		},
		{
			name:       "snes folder",
			folder:     "snes",
			wantID:     systemdefs.SystemSNES,
			wantExists: true,
		},
		{
			name:       "case insensitive",
			folder:     "NES",
			wantID:     systemdefs.SystemNES,
			wantExists: true,
		},
		{
			name:       "unknown folder",
			folder:     "unknown_system",
			wantExists: false,
		},
		{
			name:       "psx folder",
			folder:     "psx",
			wantID:     systemdefs.SystemPSX,
			wantExists: true,
		},
		{
			name:       "gamecube folder",
			folder:     "gamecube",
			wantID:     systemdefs.SystemGameCube,
			wantExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			info, exists := LookupByFolderName(tt.folder)
			assert.Equal(t, tt.wantExists, exists)
			if tt.wantExists {
				assert.Equal(t, tt.wantID, info.SystemID)
			}
		})
	}
}

func TestGetSystemID(t *testing.T) {
	t.Parallel()

	t.Run("valid folder returns system ID", func(t *testing.T) {
		t.Parallel()

		systemID, err := GetSystemID("nes")
		require.NoError(t, err)
		assert.Equal(t, systemdefs.SystemNES, systemID)
	})

	t.Run("unknown folder returns error", func(t *testing.T) {
		t.Parallel()

		_, err := GetSystemID("unknown")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown system folder")
	})
}

func TestGetFoldersForSystemID(t *testing.T) {
	t.Parallel()

	t.Run("NES has one folder", func(t *testing.T) {
		t.Parallel()

		folders := GetFoldersForSystemID(systemdefs.SystemNES)
		assert.Contains(t, folders, "nes")
	})

	t.Run("GameNWatch has multiple folders", func(t *testing.T) {
		t.Parallel()

		folders := GetFoldersForSystemID(systemdefs.SystemGameNWatch)
		assert.Contains(t, folders, "gameandwatch")
		assert.Contains(t, folders, "lcdgames")
		assert.Contains(t, folders, "gw")
	})

	t.Run("unknown system returns empty", func(t *testing.T) {
		t.Parallel()

		folders := GetFoldersForSystemID("UnknownSystem")
		assert.Empty(t, folders)
	})
}

func TestHasExtension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		folder string
		ext    string
		want   bool
	}{
		{
			name:   "nes has .nes",
			folder: "nes",
			ext:    ".nes",
			want:   true,
		},
		{
			name:   "nes has .zip",
			folder: "nes",
			ext:    ".zip",
			want:   true,
		},
		{
			name:   "nes doesn't have .smc",
			folder: "nes",
			ext:    ".smc",
			want:   false,
		},
		{
			name:   "case insensitive extension",
			folder: "nes",
			ext:    ".NES",
			want:   true,
		},
		{
			name:   "unknown folder returns false",
			folder: "unknown",
			ext:    ".nes",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := HasExtension(tt.folder, tt.ext)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSystemInfoGetLauncherID(t *testing.T) {
	t.Parallel()

	t.Run("returns LauncherID when set", func(t *testing.T) {
		t.Parallel()

		info := SystemInfo{
			SystemID:   "test_system",
			LauncherID: "TestLauncher",
		}
		assert.Equal(t, "TestLauncher", info.GetLauncherID())
	})

	t.Run("falls back to SystemID when LauncherID not set", func(t *testing.T) {
		t.Parallel()

		info := SystemInfo{
			SystemID: "test_system",
		}
		assert.Equal(t, "test_system", info.GetLauncherID())
	})
}

func TestSystemMapConsistency(t *testing.T) {
	t.Parallel()

	t.Run("all entries have SystemID", func(t *testing.T) {
		t.Parallel()

		for folder, info := range SystemMap {
			assert.NotEmpty(t, info.SystemID, "folder %s has empty SystemID", folder)
		}
	})

	t.Run("common systems exist", func(t *testing.T) {
		t.Parallel()

		commonSystems := []string{
			"nes", "snes", "n64", "gamecube", "wii", "gb", "gbc", "gba", "nds",
			"psx", "ps2", "psp", "megadrive", "dreamcast", "saturn",
			"mastersystem", "gamegear", "arcade", "mame",
		}

		for _, system := range commonSystems {
			_, ok := SystemMap[system]
			assert.True(t, ok, "common system %s not in SystemMap", system)
		}
	})
}
