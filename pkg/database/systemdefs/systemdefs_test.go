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
	"testing"

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
