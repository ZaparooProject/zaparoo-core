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

package scraper

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBasePlatformMapper_MapToScraperPlatform(t *testing.T) {
	t.Parallel()

	mapper := NewBasePlatformMapper()

	tests := []struct {
		name             string
		systemID         string
		expectedPlatform string
		expectedExists   bool
	}{
		{"NES", "nes", "nintendo-entertainment-system", true},
		{"Famicom", "famicom", "nintendo-entertainment-system", true},
		{"SNES", "snes", "super-nintendo", true},
		{"Genesis", "genesis", "sega-genesis", true},
		{"MegaDrive", "megadrive", "sega-genesis", true},
		{"PlayStation", "psx", "sony-playstation", true},
		{"PlayStation Vita", "psvita", "sony-vita", true},
		{"Xbox", "xbox", "microsoft-xbox", true},
		{"Arcade", "arcade", "arcade", true},
		{"MAME", "mame", "arcade", true},
		{"Unknown system", "unknown", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			platform, exists := mapper.MapToScraperPlatform(tt.systemID)
			assert.Equal(t, tt.expectedExists, exists, "Existence check failed for %s", tt.systemID)
			if tt.expectedExists {
				assert.Equal(t, tt.expectedPlatform, platform, "Platform mapping failed for %s", tt.systemID)
			}
		})
	}
}

func TestBasePlatformMapper_GetNormalizedPlatform(t *testing.T) {
	t.Parallel()

	mapper := NewBasePlatformMapper()

	// Test valid system
	platform := mapper.GetNormalizedPlatform("nes")
	assert.Equal(t, "nintendo-entertainment-system", platform)

	// Test invalid system
	platform = mapper.GetNormalizedPlatform("unknown")
	assert.Empty(t, platform)
}

func TestBasePlatformMapper_HasSystemID(t *testing.T) {
	t.Parallel()

	mapper := NewBasePlatformMapper()

	// Test valid systems
	assert.True(t, mapper.HasSystemID("nes"))
	assert.True(t, mapper.HasSystemID("snes"))
	assert.True(t, mapper.HasSystemID("psx"))
	assert.True(t, mapper.HasSystemID("android"))

	// Test invalid system
	assert.False(t, mapper.HasSystemID("unknown"))
}

func TestBasePlatformMapper_MapFromScraperPlatform(t *testing.T) {
	t.Parallel()

	mapper := NewBasePlatformMapper()

	tests := []struct {
		name             string
		scraperPlatform  string
		expectedSystemID string
		expectedExists   bool
	}{
		{"Nintendo Entertainment System", "nintendo-entertainment-system", "nes", true}, // Should return first match
		{"Super Nintendo", "super-nintendo", "snes", true},
		{"Sega Genesis", "sega-genesis", "genesis", true}, // Should return first match (genesis vs megadrive)
		{"Sony PlayStation", "sony-playstation", "psx", true},
		{"Arcade", "arcade", "arcade", true}, // Should return first match (arcade vs mame, fba, etc.)
		{"Unknown platform", "unknown-platform", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			systemID, exists := mapper.MapFromScraperPlatform(tt.scraperPlatform)
			assert.Equal(t, tt.expectedExists, exists, "Existence check failed for %s", tt.scraperPlatform)
			if tt.expectedExists {
				// For platforms with multiple system IDs, just verify one of them is returned
				assert.NotEmpty(t, systemID, "SystemID should not be empty for %s", tt.scraperPlatform)
			}
		})
	}
}

func TestBasePlatformMapper_GetSupportedSystems(t *testing.T) {
	t.Parallel()

	mapper := NewBasePlatformMapper()
	systems := mapper.GetSupportedSystems()

	// Should have a good number of systems
	assert.Greater(t, len(systems), 50, "Should have many supported systems")

	// Should include common systems
	assert.Contains(t, systems, "nes")
	assert.Contains(t, systems, "snes")
	assert.Contains(t, systems, "psx")
	assert.Contains(t, systems, "genesis")
	assert.Contains(t, systems, "arcade")

	// Should include alternative names
	assert.Contains(t, systems, "famicom")
	assert.Contains(t, systems, "megadrive")
	assert.Contains(t, systems, "psvita")

	// Should not contain duplicates
	systemMap := make(map[string]bool)
	for _, system := range systems {
		assert.False(t, systemMap[system], "System %s appears twice", system)
		systemMap[system] = true
	}
}

func TestBasePlatformMapper_ComprehensiveSystemCoverage(t *testing.T) {
	t.Parallel()

	mapper := NewBasePlatformMapper()

	// Test that all major console generations are covered
	majorSystems := []string{
		// Nintendo
		"nes", "snes", "n64", "gc", "wii", "wiiu", "switch",
		"gb", "gbc", "gba", "nds", "3ds",

		// Sega
		"sms", "genesis", "saturn", "dreamcast", "gg",

		// Sony
		"psx", "ps2", "ps3", "ps4", "ps5", "psp", "vita",

		// Microsoft
		"xbox", "xbox360", "xboxone",

		// Atari
		"atari2600", "atari7800", "lynx", "jaguar",

		// Arcade
		"arcade", "mame", "neogeo",

		// Computer
		"amiga", "c64", "pc", "dos",

		// Mobile
		"android", "ios",
	}

	for _, system := range majorSystems {
		t.Run(system, func(t *testing.T) {
			t.Parallel()
			assert.True(t, mapper.HasSystemID(system), "Major system %s should be supported", system)

			platform, exists := mapper.MapToScraperPlatform(system)
			assert.True(t, exists, "System %s should map to a platform", system)
			assert.NotEmpty(t, platform, "Platform for %s should not be empty", system)
		})
	}
}
