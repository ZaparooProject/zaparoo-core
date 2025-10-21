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

package systemdefs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAllSystemsHaveValidProperties tests that all systems in the Systems map have required properties
func TestAllSystemsHaveValidProperties(t *testing.T) {
	t.Parallel()

	for systemID, system := range Systems {
		t.Run(systemID, func(t *testing.T) {
			t.Parallel()
			// Test that system has required properties
			assert.NotEmpty(t, system.ID, "System %s must have non-empty ID", systemID)
			assert.Equal(t, systemID, system.ID, "System ID should match map key for %s", systemID)

			// Test that system ID follows reasonable format (no whitespace, reasonable length)
			assert.NotRegexp(t, `\s`, system.ID, "System ID %s should not contain whitespace", systemID)
			assert.Greater(t, len(system.ID), 1, "System ID %s should be more than 1 character", systemID)
			assert.Less(t, len(system.ID), 50, "System ID %s should be less than 50 characters", systemID)

			// Test aliases are valid if present
			for _, alias := range system.Aliases {
				assert.NotEmpty(t, alias, "Alias should not be empty for system %s", systemID)
				assert.NotEqual(t, system.ID, alias, "Alias should not be the same as system ID for %s", systemID)
			}
		})
	}
}

// TestSystemsMapIntegrity tests the integrity of the Systems map as a whole
func TestSystemsMapIntegrity(t *testing.T) {
	t.Parallel()

	// Test that we have a reasonable number of systems
	assert.GreaterOrEqual(t, len(Systems), 100, "Should have at least 100 systems defined")

	// Test that all system IDs are unique (this is guaranteed by map, but good to verify)
	seenIDs := make(map[string]string)
	for mapKey, system := range Systems {
		if existingKey, exists := seenIDs[system.ID]; exists {
			assert.Fail(t, "Duplicate system ID",
				"System ID %s appears in both %s and %s", system.ID, existingKey, mapKey)
		}
		seenIDs[system.ID] = mapKey
	}

	// Test that aliases don't conflict with system IDs
	for mapKey, system := range Systems {
		for _, alias := range system.Aliases {
			if conflictSystem, exists := seenIDs[alias]; exists {
				assert.Fail(t, "Alias conflicts with system ID",
					"Alias %s for system %s conflicts with system ID %s", alias, mapKey, conflictSystem)
			}
		}
	}
}

// TestGetSystemFunction tests that GetSystem works correctly for all defined systems
func TestGetSystemFunction(t *testing.T) {
	t.Parallel()

	for systemID := range Systems {
		t.Run(systemID, func(t *testing.T) {
			t.Parallel()
			system, err := GetSystem(systemID)
			require.NoError(t, err, "GetSystem should not error for valid system %s", systemID)
			assert.NotNil(t, system, "GetSystem should return non-nil system for %s", systemID)
			assert.Equal(t, systemID, system.ID, "Returned system should have correct ID")
		})
	}

	// Test that GetSystem returns error for invalid system
	_, err := GetSystem("NonExistentSystem")
	assert.Error(t, err, "GetSystem should return error for non-existent system")
}

// TestAllSystemsFunction tests that AllSystems returns all systems correctly
func TestAllSystemsFunction(t *testing.T) {
	t.Parallel()

	allSystems := AllSystems()
	assert.Len(t, allSystems, len(Systems), "AllSystems should return same number of systems as in Systems map")

	// Test that all systems from the map are present
	systemIDs := make(map[string]bool)
	for _, system := range allSystems {
		systemIDs[system.ID] = true
	}

	for systemID := range Systems {
		assert.True(t, systemIDs[systemID], "AllSystems should include system %s", systemID)
	}
}

// TestAllSystemsHaveMetadataJSON tests that every system has an associated metadata JSON file
func TestAllSystemsHaveMetadataJSON(t *testing.T) {
	t.Parallel()

	// Define the metadata structure we expect
	type SystemMetadata struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Category     string `json:"category"`
		ReleaseDate  string `json:"releaseDate,omitempty"`
		Manufacturer string `json:"manufacturer,omitempty"`
	}

	// Get the path to the systems metadata directory
	metadataDir := filepath.Join("..", "..", "assets", "systems")

	// Check each system defined in the Systems map
	for systemID, system := range Systems {
		t.Run(systemID, func(t *testing.T) {
			t.Parallel()

			// Build the expected JSON file path
			jsonFilePath := filepath.Join(metadataDir, fmt.Sprintf("%s.json", system.ID))

			// Check if the file exists
			fileInfo, err := os.Stat(jsonFilePath)
			if err != nil {
				if os.IsNotExist(err) {
					assert.Fail(t, "Missing metadata JSON file",
						"System %s is missing metadata file at %s", systemID, jsonFilePath)
				} else {
					assert.NoError(t, err, "Error checking metadata file for system %s", systemID)
				}
				return
			}

			// Verify it's a regular file
			assert.True(t, fileInfo.Mode().IsRegular(),
				"Metadata path for system %s should be a regular file", systemID)

			// Read and parse the JSON file
			data, err := os.ReadFile(filepath.Clean(jsonFilePath))
			require.NoError(t, err, "Failed to read metadata file for system %s", systemID)

			var metadata SystemMetadata
			err = json.Unmarshal(data, &metadata)
			require.NoError(t, err, "Failed to parse metadata JSON for system %s", systemID)

			// Validate the metadata
			assert.Equal(t, system.ID, metadata.ID, "Metadata ID should match system ID for %s", systemID)
			assert.NotEmpty(t, metadata.Name, "Metadata should have a name for system %s", systemID)
			assert.NotEmpty(t, metadata.Category, "Metadata should have a category for system %s", systemID)

			// Validate category is one of the expected values
			validCategories := map[string]bool{
				"Console":  true,
				"Computer": true,
				"Arcade":   true,
				"Other":    true,
				"Media":    true,
				"Handheld": true,
			}
			expectedCategories := "Console, Computer, Arcade, Other, Media, Handheld"
			assert.True(t, validCategories[metadata.Category],
				"System %s has invalid category '%s', expected one of: %s",
				systemID, metadata.Category, expectedCategories)
		})
	}
}

// TestNoSlugCollisions verifies that no slug collisions exist across different systems
func TestNoSlugCollisions(t *testing.T) {
	t.Parallel()

	slugToSystems := make(map[string]map[string]bool)

	// Collect all possible lookup keys for each system
	for sysID, sys := range Systems {
		keys := []string{
			strings.ToLower(sys.ID),
			slugs.SlugifyString(sys.ID),
		}

		for _, alias := range sys.Aliases {
			keys = append(keys,
				strings.ToLower(alias),
				slugs.SlugifyString(alias),
			)
		}

		keys = append(keys, sys.Slugs...)

		// Track which systems use each key (using map for deduplication)
		for _, key := range keys {
			if key != "" {
				if slugToSystems[key] == nil {
					slugToSystems[key] = make(map[string]bool)
				}
				slugToSystems[key][sysID] = true
			}
		}
	}

	// Report any collisions (same key used by different systems)
	var collisions []string
	for key, systemsMap := range slugToSystems {
		if len(systemsMap) > 1 {
			systems := make([]string, 0, len(systemsMap))
			for sysID := range systemsMap {
				systems = append(systems, sysID)
			}
			collisions = append(collisions,
				fmt.Sprintf("  key %q used by: %v", key, systems))
		}
	}

	if len(collisions) > 0 {
		t.Errorf("Found slug collisions:\n%s",
			strings.Join(collisions, "\n"))
	}
}

// TestLookupSystemExact verifies case-insensitive exact lookups
func TestLookupSystemExact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		wantID string
	}{
		{"Genesis uppercase", "GENESIS", "Genesis"},
		{"Genesis lowercase", "genesis", "Genesis"},
		{"Genesis mixed case", "GenEsIs", "Genesis"},
		{"SNES lowercase", "snes", "SNES"},
		{"PSX uppercase", "PSX", "PSX"},
		{"NES mixed case", "NeS", "NES"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys, err := LookupSystem(tt.input)
			require.NoError(t, err)
			require.NotNil(t, sys)
			assert.Equal(t, tt.wantID, sys.ID)
		})
	}
}

// TestLookupSystemAliases verifies alias lookups
func TestLookupSystemAliases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		wantID string
	}{
		{"MegaDrive alias", "MegaDrive", "Genesis"},
		{"MegaDrive lowercase", "megadrive", "Genesis"},
		{"SuperNintendo alias", "SuperNintendo", "SNES"},
		{"Playstation alias", "Playstation", "PSX"},
		{"PS1 alias", "PS1", "PSX"},
		{"N64 alias", "N64", "Nintendo64"},
		{"GB alias", "GB", "Gameboy"},
		{"GBA alias", "GameboyAdvance", "GBA"},
		{"MAME alias", "MAME", "Arcade"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys, err := LookupSystem(tt.input)
			require.NoError(t, err)
			require.NotNil(t, sys)
			assert.Equal(t, tt.wantID, sys.ID)
		})
	}
}

// TestLookupSystemNaturalLanguage verifies natural language lookups using manufacturer prefixes
func TestLookupSystemNaturalLanguage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		wantID string
	}{
		// Sega systems
		{"Sega Genesis", "Sega Genesis", "Genesis"},
		{"Sega Mega Drive", "Sega Mega Drive", "Genesis"},
		{"Sega Master System", "Sega Master System", "MasterSystem"},
		{"Sega Dreamcast", "Sega Dreamcast", "Dreamcast"},
		{"Sega Saturn", "Sega Saturn", "Saturn"},

		// Nintendo systems
		{"Super Nintendo", "Super Nintendo", "SNES"},
		{"Super Famicom", "Super Famicom", "SNES"},
		{"Nintendo Entertainment System", "Nintendo Entertainment System", "NES"},
		{"Nintendo 64", "Nintendo 64", "Nintendo64"},
		{"Nintendo GameBoy", "Nintendo GameBoy", "Gameboy"},
		{"Game Boy Advance", "Game Boy Advance", "GBA"},
		{"Nintendo GameCube", "Nintendo GameCube", "GameCube"},
		{"Nintendo Switch", "Nintendo Switch", "Switch"},
		{"Nintendo Wii", "Nintendo Wii", "Wii"},
		{"Nintendo Wii U", "Nintendo Wii U", "WiiU"},

		// Sony systems
		{"Sony PlayStation", "Sony PlayStation", "PSX"},
		{"PlayStation 1", "PlayStation 1", "PSX"},
		{"PlayStation One", "PlayStation One", "PSX"},
		{"Sony PS2", "Sony PS2", "PS2"},
		{"PlayStation 2", "PlayStation 2", "PS2"},

		// Microsoft systems
		{"Microsoft Xbox", "Microsoft Xbox", "Xbox"},
		{"Microsoft Xbox 360", "Microsoft Xbox 360", "Xbox360"},

		// NEC/Hudson systems
		{"PC Engine", "PC Engine", "TurboGrafx16"},
		{"NEC PC Engine", "NEC PC Engine", "TurboGrafx16"},
		{"NEC TurboGrafx-16", "NEC TurboGrafx-16", "TurboGrafx16"},
		{"TurboGrafx 16", "TurboGrafx 16", "TurboGrafx16"},

		// Atari systems
		{"Atari 2600", "Atari 2600", "Atari2600"},
		{"Atari 7800", "Atari 7800", "Atari7800"},

		// Computer systems
		{"MS-DOS", "MS-DOS", "DOS"},
		{"IBM PC", "IBM PC", "DOS"},
		{"Commodore 64", "Commodore 64", "C64"},
		{"Commodore Amiga", "Commodore Amiga", "Amiga"},

		// Arcade
		{"Arcade Machine", "Arcade Machine", "Arcade"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys, err := LookupSystem(tt.input)
			require.NoError(t, err, "Failed to lookup %q", tt.input)
			require.NotNil(t, sys)
			assert.Equal(t, tt.wantID, sys.ID, "Input %q expected %s, got %s", tt.input, tt.wantID, sys.ID)
		})
	}
}

// TestLookupSystemUnknown verifies proper error handling for unknown systems
func TestLookupSystemUnknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"Completely invalid", "ThisSystemDoesNotExist"},
		{"Gibberish", "asdfghjkl"},
		{"Empty string", ""},
		{"Near miss", "Genesi"}, // Close to Genesis but not exact
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys, err := LookupSystem(tt.input)
			require.Error(t, err)
			assert.Nil(t, sys)
		})
	}
}

// TestBuildLookupMapIdempotent verifies that buildLookupMap can be called multiple times safely
func TestBuildLookupMapIdempotent(t *testing.T) {
	t.Parallel()

	// Call it multiple times
	err1 := buildLookupMap()
	err2 := buildLookupMap()
	err3 := buildLookupMap()

	// All should return the same result
	assert.Equal(t, err1, err2)
	assert.Equal(t, err2, err3)

	// Map should be populated
	assert.NotNil(t, lookupMap)
	assert.NotEmpty(t, lookupMap)
}

// TestSlugDerivation verifies that slugified IDs and Aliases work without explicit Slugs entries
func TestSlugDerivation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		wantID string
	}{
		// These should work via auto-derived slugification, not explicit Slugs entries
		{"genesis slug", "genesis", "Genesis"},
		{"megadrive slug", "megadrive", "Genesis"},
		{"snes slug", "snes", "SNES"},
		{"psx slug", "psx", "PSX"},
		{"n64 slug", "n64", "Nintendo64"},
		{"gameboy slug", "gameboy", "Gameboy"},
		{"dreamcast slug", "dreamcast", "Dreamcast"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys, err := LookupSystem(tt.input)
			require.NoError(t, err)
			require.NotNil(t, sys)
			assert.Equal(t, tt.wantID, sys.ID)
		})
	}
}

// TestNoRedundantSlugs verifies that custom slugs don't duplicate auto-derived slugs.
// Custom slugs should only be used for manufacturer prefixes, regional names, or other
// variations that wouldn't be automatically generated from the ID or aliases.
func TestNoRedundantSlugs(t *testing.T) {
	t.Parallel()

	var redundancies []string

	// Check each system
	for sysID, sys := range Systems {
		// Collect auto-derived slugs (what would be automatically generated)
		autoDerived := make(map[string]bool)

		// Add slugified ID
		slugifiedID := slugs.SlugifyString(sys.ID)
		if slugifiedID != "" {
			autoDerived[slugifiedID] = true
		}

		// Add slugified aliases
		for _, alias := range sys.Aliases {
			slugifiedAlias := slugs.SlugifyString(alias)
			if slugifiedAlias != "" {
				autoDerived[slugifiedAlias] = true
			}
		}

		// Check if any custom slugs are redundant with auto-derived ones
		for _, customSlug := range sys.Slugs {
			if autoDerived[customSlug] {
				redundancies = append(redundancies,
					fmt.Sprintf("  System %s: custom slug %q is redundant (auto-derived from ID or alias)",
						sysID, customSlug))
			}
		}
	}

	if len(redundancies) > 0 {
		t.Errorf("Found redundant custom slugs that are already auto-derived:\n%s\n\n"+
			"These custom slugs can be removed as they are automatically generated from the system ID or aliases.",
			strings.Join(redundancies, "\n"))
	}
}

// TestSlugsAreAlphanumeric verifies that all custom slugs contain only alphanumeric characters.
// Slugs with special characters or spaces would be stripped during slugification and never match.
func TestSlugsAreAlphanumeric(t *testing.T) {
	t.Parallel()

	var invalidSlugs []string

	// Check each system
	for sysID, sys := range Systems {
		for _, slug := range sys.Slugs {
			// Check if slug contains only lowercase alphanumeric characters
			for i, r := range slug {
				if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
					invalidSlugs = append(invalidSlugs,
						fmt.Sprintf("  System %s: slug %q contains invalid character %q at position %d",
							sysID, slug, string(r), i))
					break
				}
			}

			// Also check if slug is empty (shouldn't happen, but defensive)
			if slug == "" {
				invalidSlugs = append(invalidSlugs,
					fmt.Sprintf("  System %s: has an empty slug", sysID))
			}
		}
	}

	if len(invalidSlugs) > 0 {
		t.Errorf("Found custom slugs with invalid characters:\n%s\n\n"+
			"Custom slugs must contain only lowercase alphanumeric characters (a-z, 0-9).\n"+
			"Special characters and spaces are stripped during slugification and would never match.",
			strings.Join(invalidSlugs, "\n"))
	}
}

// TestCollisionDetectionWorks verifies that the collision detection actually catches conflicts
func TestCollisionDetectionWorks(t *testing.T) {
	// NOTE: Cannot use t.Parallel() because this test modifies global Systems map

	// This test verifies that the buildLookupMap collision detection works
	// by checking that it would catch an intentional conflict

	// Create a temporary modified Systems map with an intentional conflict
	originalGenesis := Systems["Genesis"]
	originalSNES := Systems["SNES"]

	// Test 1: Conflicting custom slug
	t.Run("ConflictingCustomSlug", func(t *testing.T) {
		// Temporarily modify SNES to have a slug that conflicts with Genesis
		Systems["SNES"] = System{
			ID:      "SNES",
			Aliases: originalSNES.Aliases,
			Slugs:   []string{"segagenesis"}, // Conflicts with Genesis slug!
		}

		// Reset the lookup map so it rebuilds
		lookupMap = nil
		lookupMapOnce = sync.Once{}
		errLookupMap = nil

		// This should detect the collision
		err := buildLookupMap()
		require.Error(t, err, "Should detect slug collision between Genesis and SNES")
		assert.Contains(t, err.Error(), "collision", "Error should mention collision")

		// Restore original
		Systems["SNES"] = originalSNES
		lookupMap = nil
		lookupMapOnce = sync.Once{}
		errLookupMap = nil
	})

	// Test 2: No collision in original data
	t.Run("NoCollisionInOriginalData", func(t *testing.T) {
		// Restore originals
		Systems["Genesis"] = originalGenesis
		Systems["SNES"] = originalSNES

		// Reset the lookup map
		lookupMap = nil
		lookupMapOnce = sync.Once{}
		errLookupMap = nil

		// This should NOT detect any collision
		err := buildLookupMap()
		assert.NoError(t, err, "Original system data should have no collisions")
	})
}

// BenchmarkLookupSystemExact benchmarks exact ID lookups
func BenchmarkLookupSystemExact(b *testing.B) {
	for range b.N {
		_, _ = LookupSystem("Genesis")
	}
}

// BenchmarkLookupSystemSlug benchmarks slug-based lookups
func BenchmarkLookupSystemSlug(b *testing.B) {
	for range b.N {
		_, _ = LookupSystem("Sega Genesis")
	}
}

// BenchmarkLookupSystemAlias benchmarks alias lookups
func BenchmarkLookupSystemAlias(b *testing.B) {
	for range b.N {
		_, _ = LookupSystem("MegaDrive")
	}
}
