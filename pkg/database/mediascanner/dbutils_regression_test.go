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

// TestAddMediaPath_ErrorPropagation_Regression tests that critical infrastructure errors
// are properly propagated instead of being silently swallowed.
//
// This is a regression test for the bug where AddMediaPath would return (0, 0) on
// critical errors, causing the caller to continue with invalid IDs and corrupt data.
//
// The fix ensures that:
// 1. Database connection failures return errors
// 2. Post-constraint-violation lookup failures return errors
// 3. Missing tag type errors return errors
//
// These are infrastructure failures that should stop indexing, not file-level issues.
func TestAddMediaPath_ErrorPropagation_Regression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(*helpers.MockMediaDBI)
		expectedError string
	}{
		{
			name: "database connection failure on system insert",
			setupMock: func(mockDB *helpers.MockMediaDBI) {
				// Simulate a database connection error (not UNIQUE constraint)
				mockDB.On("InsertSystem", database.System{
					DBID:     int64(1),
					SystemID: "NES",
					Name:     "NES",
				}).Return(database.System{}, assert.AnError).Once()
			},
			expectedError: "error inserting system",
		},
		{
			name: "lookup failure after UNIQUE constraint on system",
			setupMock: func(mockDB *helpers.MockMediaDBI) {
				// System insert fails with UNIQUE constraint
				constraintErr := sqlite3.Error{
					Code:         sqlite3.ErrConstraint,
					ExtendedCode: sqlite3.ErrConstraintUnique,
				}
				mockDB.On("InsertSystem", database.System{
					DBID:     int64(1),
					SystemID: "NES",
					Name:     "NES",
				}).Return(database.System{}, constraintErr).Once()

				// Lookup of existing system fails
				mockDB.On("FindSystemBySystemID", "NES").
					Return(database.System{}, assert.AnError).Once()
			},
			expectedError: "failed to get existing system",
		},
		{
			name: "database connection failure on media title insert",
			setupMock: func(mockDB *helpers.MockMediaDBI) {
				// System insert succeeds
				mockDB.On("InsertSystem", database.System{
					DBID:     int64(1),
					SystemID: "NES",
					Name:     "NES",
				}).Return(database.System{DBID: 1}, nil).Once()

				// MediaTitle insert fails with non-UNIQUE error
				mockDB.On("InsertMediaTitle", database.MediaTitle{
					DBID:       int64(1),
					Slug:       "supermariobros",
					Name:       "Super Mario Bros",
					SystemDBID: int64(1),
				}).Return(database.MediaTitle{}, assert.AnError).Once()
			},
			expectedError: "error inserting media title",
		},
		{
			name: "lookup failure after UNIQUE constraint on media title",
			setupMock: func(mockDB *helpers.MockMediaDBI) {
				// System insert succeeds
				mockDB.On("InsertSystem", database.System{
					DBID:     int64(1),
					SystemID: "NES",
					Name:     "NES",
				}).Return(database.System{DBID: 1}, nil).Once()

				// MediaTitle insert fails with UNIQUE constraint
				constraintErr := sqlite3.Error{
					Code:         sqlite3.ErrConstraint,
					ExtendedCode: sqlite3.ErrConstraintUnique,
				}
				mockDB.On("InsertMediaTitle", database.MediaTitle{
					DBID:       int64(1),
					Slug:       "supermariobros",
					Name:       "Super Mario Bros",
					SystemDBID: int64(1),
				}).Return(database.MediaTitle{}, constraintErr).Once()

				// Lookup of existing title fails
				mockDB.On("FindMediaTitle", database.MediaTitle{
					Slug:       "supermariobros",
					SystemDBID: int64(1),
				}).Return(database.MediaTitle{}, assert.AnError).Once()
			},
			expectedError: "failed to get existing media title",
		},
		{
			name: "extension tag type not found",
			setupMock: func(mockDB *helpers.MockMediaDBI) {
				// System insert succeeds
				mockDB.On("InsertSystem", database.System{
					DBID:     int64(1),
					SystemID: "NES",
					Name:     "NES",
				}).Return(database.System{DBID: 1}, nil).Once()

				// MediaTitle insert succeeds
				mockDB.On("InsertMediaTitle", database.MediaTitle{
					DBID:       int64(1),
					Slug:       "supermariobros",
					Name:       "Super Mario Bros",
					SystemDBID: int64(1),
				}).Return(database.MediaTitle{DBID: 1}, nil).Once()

				// Media insert succeeds
				mockDB.On("InsertMedia", database.Media{
					DBID:           int64(1),
					Path:           "/games/nes/Super Mario Bros.nes",
					MediaTitleDBID: int64(1),
					SystemDBID:     int64(1),
				}).Return(database.Media{DBID: 1}, nil).Once()

				// Extension tag type lookup fails (should not happen)
				mockDB.On("FindTagType", database.TagType{Type: "extension"}).
					Return(database.TagType{}, assert.AnError).Once()
			},
			expectedError: "extension tag type not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockDB := &helpers.MockMediaDBI{}
			tt.setupMock(mockDB)

			// Allow any additional method calls
			mockDB.On("GetTotalMediaCount").Return(0, nil).Maybe()

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

			// Call AddMediaPath
			titleIndex, mediaIndex, err := AddMediaPath(
				mockDB,
				scanState,
				"NES",
				"/games/nes/Super Mario Bros.nes",
				false,
				false,
				nil,
			)

			// CRITICAL: Error must be returned, not silently swallowed
			require.Error(t, err, "critical infrastructure errors must be propagated")
			assert.Contains(t, err.Error(), tt.expectedError,
				"error message should indicate the specific failure")

			// Indices should be 0 on error
			assert.Equal(t, 0, titleIndex, "titleIndex should be 0 on error")
			assert.Equal(t, 0, mediaIndex, "mediaIndex should be 0 on error")

			mockDB.AssertExpectations(t)
		})
	}
}

// TestAddMediaPath_SuccessfulRecovery_Regression tests that UNIQUE constraint violations
// are handled gracefully and don't return errors when recovery succeeds.
//
// This ensures that expected constraint violations (e.g., during resume or multiple scanners)
// are treated as non-errors and allow indexing to continue normally.
func TestAddMediaPath_SuccessfulRecovery_Regression(t *testing.T) {
	t.Parallel()

	mockDB := &helpers.MockMediaDBI{}

	// System already exists - UNIQUE constraint, but lookup succeeds
	constraintErr := sqlite3.Error{
		Code:         sqlite3.ErrConstraint,
		ExtendedCode: sqlite3.ErrConstraintUnique,
	}
	mockDB.On("InsertSystem", database.System{
		DBID:     int64(1),
		SystemID: "NES",
		Name:     "NES",
	}).Return(database.System{}, constraintErr).Once()

	mockDB.On("FindSystemBySystemID", "NES").
		Return(database.System{DBID: 5, SystemID: "NES", Name: "NES"}, nil).Once()

	// MediaTitle insert succeeds with recovered system ID
	mockDB.On("InsertMediaTitle", database.MediaTitle{
		DBID:       int64(1),
		Slug:       "supermariobros", // Slugified version (dashes removed)
		Name:       "Super Mario Bros",
		SystemDBID: int64(5), // Using recovered system ID
	}).Return(database.MediaTitle{DBID: 1}, nil).Once()

	// Media insert succeeds
	mockDB.On("InsertMedia", database.Media{
		DBID:           int64(1),
		Path:           "/games/nes/Super Mario Bros.nes",
		MediaTitleDBID: int64(1),
		SystemDBID:     int64(5), // Using recovered system ID
	}).Return(database.Media{DBID: 1}, nil).Once()

	// Extension tag type lookup (from scan state cache)
	mockDB.On("FindTagType", database.TagType{Type: "extension"}).
		Return(database.TagType{DBID: 2, Type: "extension"}, nil).Maybe()

	// Tag insert for nes extension (dot removed)
	mockDB.On("InsertTag", database.Tag{
		DBID:     int64(1),
		Tag:      "nes",
		TypeDBID: int64(2),
	}).Return(database.Tag{DBID: 1}, nil).Maybe()

	// MediaTag insert for extension tag
	mockDB.On("InsertMediaTag", database.MediaTag{
		TagDBID:   int64(1),
		MediaDBID: int64(1),
	}).Return(database.MediaTag{}, nil).Maybe()

	mockDB.On("GetTotalMediaCount").Return(0, nil).Maybe()

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

	// Call AddMediaPath
	titleIndex, mediaIndex, err := AddMediaPath(
		mockDB,
		scanState,
		"NES",
		"/games/nes/Super Mario Bros.nes",
		false,
		false,
		nil,
	)

	// CRITICAL: No error should be returned when recovery succeeds
	require.NoError(t, err, "successful recovery from UNIQUE constraint should not return error")
	assert.Equal(t, 1, titleIndex, "titleIndex should be set on success")
	assert.Equal(t, 1, mediaIndex, "mediaIndex should be set on success")

	// Scan state should be updated with recovered system ID
	assert.Equal(t, 5, scanState.SystemIDs["NES"], "scan state should use recovered system ID")

	mockDB.AssertExpectations(t)
}
