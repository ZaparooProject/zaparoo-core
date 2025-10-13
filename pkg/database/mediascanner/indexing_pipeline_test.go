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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
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
		SystemIDs:     make(map[string]int),
		TitleIDs:      make(map[string]int),
		MediaIDs:      make(map[string]int),
		TagIDs:        make(map[string]int),
		TagTypeIDs:    make(map[string]int),
		SystemsIndex:  0,
		TitlesIndex:   0,
		MediaIndex:    0,
		TagsIndex:     0,
		TagTypesIndex: 0,
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

	// Mock FindSystemBySystemID to return the existing system when insert fails
	// This is what should happen - we should query the existing system
	existingSystem := database.System{
		DBID:     int64(42), // Existing system has different DBID
		SystemID: "TV",
		Name:     "TV",
	}
	mockDB.On("FindSystemBySystemID", "TV").Return(existingSystem, nil).Once()

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
		SystemDBID:     int64(42), // Should use existing system DBID
	}).Return(database.Media{DBID: 1}, nil).Once()

	// Mock additional methods that might be called
	mockDB.On("GetTotalMediaCount").Return(0, nil).Maybe()

	// Call AddMediaPath with a TV show path
	titleIndex, mediaIndex, _ := AddMediaPath(mockDB, scanState, "TV", "kodi-show://1/Loki", false, false, nil)

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
		SystemIDs:     make(map[string]int),
		TitleIDs:      make(map[string]int),
		MediaIDs:      make(map[string]int),
		TagIDs:        make(map[string]int),
		TagTypeIDs:    make(map[string]int),
		SystemsIndex:  0,
		TitlesIndex:   0,
		MediaIndex:    0,
		TagsIndex:     0,
		TagTypesIndex: 0,
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

	// Mock FindSystemBySystemID to also fail - this simulates a more serious database issue
	mockDB.On("FindSystemBySystemID", "TV").Return(database.System{}, assert.AnError).Once()

	// Mock additional methods that might be called
	mockDB.On("GetTotalMediaCount").Return(0, nil).Maybe()

	// Call AddMediaPath with a TV show path
	titleIndex, mediaIndex, err := AddMediaPath(mockDB, scanState, "TV", "kodi-show://1/Loki", false, false, nil)

	// Function should return error and (0, 0) to prevent invalid data
	require.Error(t, err, "should return error when system cannot be resolved")
	assert.Contains(t, err.Error(), "failed to get existing system", "error should indicate system resolution failure")
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
		SystemIDs:     make(map[string]int),
		TitleIDs:      make(map[string]int),
		MediaIDs:      make(map[string]int),
		TagIDs:        make(map[string]int),
		TagTypeIDs:    make(map[string]int),
		SystemsIndex:  0,
		TitlesIndex:   0,
		MediaIndex:    0,
		TagsIndex:     0,
		TagTypesIndex: 0,
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

	// Mock additional methods that might be called
	mockDB.On("GetTotalMediaCount").Return(0, nil).Maybe()

	// Call AddMediaPath with a TV show path
	titleIndex, mediaIndex, err := AddMediaPath(mockDB, scanState, "TV", "kodi-show://1/Loki", false, false, nil)

	// Function should return error and (0, 0) for non-recoverable errors
	require.Error(t, err, "should return error for non-recoverable database errors")
	assert.Contains(t, err.Error(), "error inserting system", "error should indicate insert failure")
	assert.Equal(t, 0, titleIndex, "titleIndex should be 0 for non-recoverable database errors")
	assert.Equal(t, 0, mediaIndex, "mediaIndex should be 0 for non-recoverable database errors")

	// SystemIDs cache should still be empty
	assert.Empty(t, scanState.SystemIDs, "SystemIDs cache should be empty when non-recoverable error occurs")

	// Verify all mocks were called as expected (FindSystem should NOT have been called)
	mockDB.AssertExpectations(t)
}

func TestGetTitleFromFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{
			name:     "strips parentheses",
			filename: "Super Mario Bros (USA)",
			want:     "Super Mario Bros",
		},
		{
			name:     "strips square brackets",
			filename: "Legend of Zelda [!]",
			want:     "Legend of Zelda",
		},
		{
			name:     "strips braces",
			filename: "Game Title {Europe}",
			want:     "Game Title",
		},
		{
			name:     "strips angle brackets",
			filename: "Sonic <Beta>",
			want:     "Sonic",
		},
		{
			name:     "strips all bracket types",
			filename: "Final Fantasy (USA)[!]{En}<Proto>",
			want:     "Final Fantasy",
		},
		{
			name:     "handles mixed metadata",
			filename: "Metal Gear Solid (Disc 1 of 2) (USA) [!]",
			want:     "Metal Gear Solid",
		},
		{
			name:     "no brackets returns full name",
			filename: "Plain Game Name",
			want:     "Plain Game Name",
		},
		{
			name:     "trims whitespace",
			filename: "  Spaced Name  (USA)",
			want:     "Spaced Name",
		},
		{
			name:     "preserves dashes and colons in title",
			filename: "Legend of Zelda: Link's Awakening - DX (USA)",
			want:     "Legend of Zelda: Link's Awakening - DX",
		},
		{
			name:     "handles brace before other brackets",
			filename: "Game {Proto} (USA) [!]",
			want:     "Game",
		},
		{
			name:     "handles angle before other brackets",
			filename: "Game <Alpha> (USA) [!]",
			want:     "Game",
		},
		{
			name:     "preserves leading number with period (no stripping)",
			filename: "01. Super Mario Bros (USA)",
			want:     "01. Super Mario Bros",
		},
		{
			name:     "preserves leading number with dash (no stripping)",
			filename: "42 - Answer (USA)",
			want:     "42 - Answer",
		},
		{
			name:     "preserves leading number with space (no stripping)",
			filename: "1 Game Title (USA)",
			want:     "1 Game Title",
		},
		{
			name:     "converts underscores to spaces",
			filename: "Super_Mario_Bros (USA)",
			want:     "Super Mario Bros",
		},
		{
			name:     "converts mixed underscores and spaces",
			filename: "Mega_Man_X (USA)",
			want:     "Mega Man X",
		},
		{
			name:     "preserves ampersand",
			filename: "Sonic & Knuckles (USA)",
			want:     "Sonic & Knuckles",
		},
		{
			name:     "normalizes multiple spaces",
			filename: "Game   Title   Here (USA)",
			want:     "Game Title Here",
		},
		{
			name:     "handles all transformations combined (no number stripping)",
			filename: "01. Super_Mario_Bros & Luigi   (USA)",
			want:     "01. Super Mario Bros & Luigi",
		},
		{
			name:     "preserves dashes in title after cleanup",
			filename: "Zelda - Link's Awakening (USA)",
			want:     "Zelda - Link's Awakening",
		},
		{
			name:     "preserves colons in title after cleanup",
			filename: "Game: The Subtitle (USA)",
			want:     "Game: The Subtitle",
		},
		{
			name:     "converts underscores without brackets",
			filename: "Super_Mario_World",
			want:     "Super Mario World",
		},
		{
			name:     "preserves leading number without brackets (no stripping)",
			filename: "01. Game Title",
			want:     "01. Game Title",
		},
		{
			name:     "preserves ampersand without brackets",
			filename: "Rock & Roll Racing",
			want:     "Rock & Roll Racing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Pass false for stripLeadingNumbers - we removed unconditional stripping
			got := tags.ParseTitleFromFilename(tt.filename, false)
			assert.Equal(t, tt.want, got)
		})
	}
}
