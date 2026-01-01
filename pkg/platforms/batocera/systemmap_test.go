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

package batocera

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFromBatoceraSystem tests the fromBatoceraSystem helper function.
func TestFromBatoceraSystem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		batoceraName  string
		expectedValue string
		expectError   bool
	}{
		{
			name:          "valid system - nes",
			batoceraName:  "nes",
			expectError:   false,
			expectedValue: "NES",
		},
		{
			name:          "valid system - snes",
			batoceraName:  "snes",
			expectError:   false,
			expectedValue: "SNES",
		},
		{
			name:          "valid system - psx",
			batoceraName:  "psx",
			expectError:   false,
			expectedValue: "PSX",
		},
		{
			name:         "invalid system",
			batoceraName: "nonexistent_system",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := fromBatoceraSystem(tt.batoceraName)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedValue, result)
			}
		})
	}
}

// TestToBatoceraSystems tests the toBatoceraSystems helper function.
func TestToBatoceraSystems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		zaparooSystem  string
		expectNonEmpty bool
	}{
		{
			name:           "NES maps to folder",
			zaparooSystem:  "NES",
			expectNonEmpty: true,
		},
		{
			name:           "Arcade maps to multiple folders",
			zaparooSystem:  "Arcade",
			expectNonEmpty: true,
		},
		{
			name:           "nonexistent system returns empty",
			zaparooSystem:  "NonExistentSystem",
			expectNonEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := toBatoceraSystems(tt.zaparooSystem)
			require.NoError(t, err)
			if tt.expectNonEmpty {
				assert.NotEmpty(t, result)
			} else {
				assert.Empty(t, result)
			}
		})
	}
}

// TestCommonSystemsExist tests that commonly expected systems exist in the shared esde.SystemMap.
func TestCommonSystemsExist(t *testing.T) {
	t.Parallel()

	// Test for some common/critical systems that should exist
	commonSystems := []string{
		"nes", "snes", "megadrive", "arcade", "c64", "amiga500",
		"psx", "n64", "gba", "nds", "psp", "saturn",
	}

	for _, systemName := range commonSystems {
		t.Run(systemName, func(t *testing.T) {
			t.Parallel()
			info, exists := esde.LookupByFolderName(systemName)
			assert.True(t, exists, "Common system %s should exist in esde.SystemMap", systemName)
			if exists {
				assert.NotEmpty(t, info.SystemID, "Common system %s should have SystemID", systemName)
				assert.NotEmpty(t, info.Extensions, "Common system %s should have extensions", systemName)
				assert.NotEmpty(t, info.GetLauncherID(), "Common system %s should have LauncherID", systemName)
			}
		})
	}
}

// TestBatoceraOfficialExtensions tests that extensions match Batocera's official es_systems.yml config.
// These were verified against batocera-linux/batocera.linux repository at
// package/batocera/emulationstation/batocera-es-system/es_systems.yml
func TestBatoceraOfficialExtensions(t *testing.T) {
	t.Parallel()

	// Define expected extensions from Batocera's official configuration
	// This ensures our extensions match what Batocera actually uses
	expectedExtensions := map[string][]string{
		// Port/Engine systems
		"ports":       {".sh", ".squashfs"},
		"mrboom":      {".libretro"},
		"quake":       {".quake"},
		"quake2":      {".quake2", ".zip", ".7zip"},
		"quake3":      {".quake3"},
		"sonic-mania": {".sman"},
		"catacomb":    {".game"},
		"fury":        {".grp"},
		"hurrican":    {".game"},
		"dxx-rebirth": {".d1x", ".d2x"},
		"gong":        {".game"},

		// Specialized systems
		"dice":   {".zip", ".dmy"},
		"doom3":  {".d3"},
		"raze":   {".raze"},
		"tyrian": {".game"},
		"library": {
			".jpg", ".jpeg", ".png", ".bmp", ".psd", ".tga", ".gif", ".hdr", ".pic", ".ppm", ".pgm",
			".mkv", ".pdf", ".mp4", ".avi", ".webm", ".cbz", ".mp3", ".wav", ".ogg", ".flac",
			".mod", ".xm", ".stm", ".s3m", ".far", ".it", ".669", ".mtm",
		},

		// Modern console systems
		"ps2":     {".iso", ".mdf", ".nrg", ".bin", ".img", ".dump", ".gz", ".cso", ".chd", ".m3u"},
		"ps3":     {".ps3", ".psn", ".squashfs"},
		"psvita":  {".zip", ".psvita"},
		"switch":  {".xci", ".nsp"},
		"wiiu":    {".wua", ".wup", ".wud", ".wux", ".rpx", ".squashfs", ".wuhb"},
		"xbox360": {".iso", ".xex", ".xbox360", ".zar"},
	}

	for systemName, expectedExts := range expectedExtensions {
		t.Run(systemName, func(t *testing.T) {
			t.Parallel()

			info, exists := esde.LookupByFolderName(systemName)
			assert.True(t, exists, "System %s should exist in esde.SystemMap", systemName)

			if !exists {
				return
			}

			// Verify all expected extensions are present
			for _, expectedExt := range expectedExts {
				assert.Contains(t, info.Extensions, expectedExt,
					"System %s should have extension %s according to Batocera official config",
					systemName, expectedExt)
			}

			// Verify no unexpected extensions are present
			for _, actualExt := range info.Extensions {
				assert.Contains(t, expectedExts, actualExt,
					"System %s has unexpected extension %s (not in Batocera official config)",
					systemName, actualExt)
			}

			// Verify exact match (same count and content)
			assert.ElementsMatch(t, expectedExts, info.Extensions,
				"System %s extensions should exactly match Batocera official config", systemName)
		})
	}
}
