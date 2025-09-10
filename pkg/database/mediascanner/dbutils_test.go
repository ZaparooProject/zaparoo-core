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

package mediascanner

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddMediaPath_SystemInsertFailure tests the scenario where system insertion fails
// due to UNIQUE constraint violation (system already exists in database)
// This test reproduces the TV show search issue where multiple scanners
// (KodiTV and KodiTVShow) try to insert the same system causing failures
func TestAddMediaPath_SystemInsertFailure(t *testing.T) {
	t.Parallel()

	// Create mock database
	mockDB := &helpers.MockMediaDBI{}

	// Create fresh scan state - this simulates the state after FlushScanStateMaps
	// which clears the SystemIDs cache between scanner batches
	scanState := &database.ScanState{
		SystemIDs:      make(map[string]int),
		TitleIDs:       make(map[string]int),
		MediaIDs:       make(map[string]int),
		TagIDs:         make(map[string]int),
		TagTypeIDs:     make(map[string]int),
		SystemsIndex:   0,
		TitlesIndex:    0,
		MediaIndex:     0,
		TagsIndex:      0,
		TagTypesIndex:  0,
		MediaTagsIndex: 0,
	}

	// Mock the system insertion to fail (simulating UNIQUE constraint violation)
	// This happens when KodiTVShow scanner tries to insert TV system after
	// KodiTV scanner already inserted it and committed
	uniqueConstraintErr := sqlite3.Error{
		Code:         sqlite3.ErrConstraint,
		ExtendedCode: sqlite3.ErrConstraintUnique,
	}
	mockDB.On("InsertSystem", database.System{
		DBID:     int64(1),
		SystemID: "TV",
		Name:     "TV",
	}).Return(database.System{}, uniqueConstraintErr).Once()

	// Mock FindSystem to return the existing system when insert fails
	// This is what should happen - we should query the existing system
	existingSystem := database.System{
		DBID:     int64(42), // Existing system has different DBID
		SystemID: "TV",
		Name:     "TV",
	}
	mockDB.On("FindSystem", database.System{
		SystemID: "TV",
	}).Return(existingSystem, nil).Once()

	// Mock successful media title and media insertion using the correct system ID
	mockDB.On("InsertMediaTitle", database.MediaTitle{
		DBID:       int64(1),
		Slug:       "loki",
		Name:       "Loki",
		SystemDBID: int64(42), // Should use existing system DBID
	}).Return(database.MediaTitle{DBID: 1}, nil).Once()

	mockDB.On("InsertMedia", database.Media{
		DBID:           int64(1),
		Path:           "kodi-show://1/Loki",
		MediaTitleDBID: int64(1),
	}).Return(database.Media{DBID: 1}, nil).Once()

	// Call AddMediaPath with a TV show path
	titleIndex, mediaIndex := AddMediaPath(mockDB, scanState, "TV", "kodi-show://1/Loki")

	// Verify that the function succeeded and returned valid indices
	// With the bug, this would return (0, 0) because systemIndex would be 0
	require.NotEqual(t, 0, titleIndex, "titleIndex should not be 0")
	require.NotEqual(t, 0, mediaIndex, "mediaIndex should not be 0")

	// Verify that the SystemIDs cache was updated with the correct system ID
	assert.Equal(t, 42, scanState.SystemIDs["TV"], "SystemIDs cache should contain existing system DBID")

	// Verify all mocks were called as expected
	mockDB.AssertExpectations(t)
}

// TestAddMediaPath_SystemInsertFailure_CannotFindExisting tests the scenario
// where system insert fails AND we cannot find the existing system
func TestAddMediaPath_SystemInsertFailure_CannotFindExisting(t *testing.T) {
	t.Parallel()

	// Create mock database
	mockDB := &helpers.MockMediaDBI{}

	// Create fresh scan state
	scanState := &database.ScanState{
		SystemIDs:      make(map[string]int),
		TitleIDs:       make(map[string]int),
		MediaIDs:       make(map[string]int),
		TagIDs:         make(map[string]int),
		TagTypeIDs:     make(map[string]int),
		SystemsIndex:   0,
		TitlesIndex:    0,
		MediaIndex:     0,
		TagsIndex:      0,
		TagTypesIndex:  0,
		MediaTagsIndex: 0,
	}

	// Mock the system insertion to fail with a UNIQUE constraint error
	// This ensures the code will attempt recovery by calling FindSystem
	constraintErr := sqlite3.Error{
		Code:         sqlite3.ErrConstraint,
		ExtendedCode: sqlite3.ErrConstraintUnique,
	}
	mockDB.On("InsertSystem", database.System{
		DBID:     int64(1),
		SystemID: "TV",
		Name:     "TV",
	}).Return(database.System{}, constraintErr).Once()

	// Mock FindSystem to also fail - this simulates a more serious database issue
	mockDB.On("FindSystem", database.System{
		SystemID: "TV",
	}).Return(database.System{}, assert.AnError).Once()

	// Call AddMediaPath with a TV show path
	titleIndex, mediaIndex := AddMediaPath(mockDB, scanState, "TV", "kodi-show://1/Loki")

	// Function should return early with (0, 0) to prevent invalid data
	assert.Equal(t, 0, titleIndex, "titleIndex should be 0 when system cannot be resolved")
	assert.Equal(t, 0, mediaIndex, "mediaIndex should be 0 when system cannot be resolved")

	// SystemIDs cache should still be empty
	assert.Empty(t, scanState.SystemIDs, "SystemIDs cache should be empty when system resolution fails")

	// Verify all mocks were called as expected
	mockDB.AssertExpectations(t)
}

// TestAddMediaPath_NonUniqueError tests that non-UNIQUE errors fail immediately
// without attempting to find existing system
func TestAddMediaPath_NonUniqueError(t *testing.T) {
	t.Parallel()

	// Create mock database
	mockDB := &helpers.MockMediaDBI{}

	// Create fresh scan state
	scanState := &database.ScanState{
		SystemIDs:      make(map[string]int),
		TitleIDs:       make(map[string]int),
		MediaIDs:       make(map[string]int),
		TagIDs:         make(map[string]int),
		TagTypeIDs:     make(map[string]int),
		SystemsIndex:   0,
		TitlesIndex:    0,
		MediaIndex:     0,
		TagsIndex:      0,
		TagTypesIndex:  0,
		MediaTagsIndex: 0,
	}

	// Create a non-UNIQUE database error (like connection error)
	connectionError := assert.AnError // This will be different from UNIQUE constraint

	// Mock the system insertion to fail with non-UNIQUE error
	mockDB.On("InsertSystem", database.System{
		DBID:     int64(1),
		SystemID: "TV",
		Name:     "TV",
	}).Return(database.System{}, connectionError).Once()

	// FindSystem should NOT be called for non-UNIQUE errors
	// This is the key difference - we should fail fast, not try recovery

	// Call AddMediaPath with a TV show path
	titleIndex, mediaIndex := AddMediaPath(mockDB, scanState, "TV", "kodi-show://1/Loki")

	// Function should return early with (0, 0) for non-recoverable errors
	assert.Equal(t, 0, titleIndex, "titleIndex should be 0 for non-recoverable database errors")
	assert.Equal(t, 0, mediaIndex, "mediaIndex should be 0 for non-recoverable database errors")

	// SystemIDs cache should still be empty
	assert.Empty(t, scanState.SystemIDs, "SystemIDs cache should be empty when non-recoverable error occurs")

	// Verify all mocks were called as expected (FindSystem should NOT have been called)
	mockDB.AssertExpectations(t)
}
