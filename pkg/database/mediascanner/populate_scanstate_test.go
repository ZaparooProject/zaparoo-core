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
	"context"
	"database/sql"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPopulateScanStateFromDB_EdgeCases tests edge cases for PopulateScanStateFromDB
// This function was previously a stub and completely non-functional
func TestPopulateScanStateFromDB_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("Empty Database", func(t *testing.T) {
		t.Parallel()

		// Create in-memory SQLite database
		sqlDB, err := sql.Open("sqlite3", ":memory:")
		require.NoError(t, err)
		defer func() { _ = sqlDB.Close() }()

		ctx := context.Background()
		mockPlatform := mocks.NewMockPlatform()
		mockPlatform.On("ID").Return("test-platform")

		mediaDB := &mediadb.MediaDB{}
		err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
		require.NoError(t, err)

		scanState := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   999, // Set to non-zero to verify it gets reset
			TitlesIndex:    999,
			MediaIndex:     999,
			TagTypesIndex:  999,
			TagsIndex:      999,
			MediaTagsIndex: 999,
		}

		err = PopulateScanStateFromDB(mediaDB, scanState)
		require.NoError(t, err)

		// All indexes should be 0 for empty database
		assert.Equal(t, 0, scanState.SystemsIndex, "Empty DB should have SystemsIndex = 0")
		assert.Equal(t, 0, scanState.TitlesIndex, "Empty DB should have TitlesIndex = 0")
		assert.Equal(t, 0, scanState.MediaIndex, "Empty DB should have MediaIndex = 0")
		assert.Equal(t, 0, scanState.TagTypesIndex, "Empty DB should have TagTypesIndex = 0")
		assert.Equal(t, 0, scanState.TagsIndex, "Empty DB should have TagsIndex = 0")
		assert.Equal(t, 0, scanState.MediaTagsIndex, "Empty DB should have MediaTagsIndex = 0")
	})

	t.Run("Database With Only Systems", func(t *testing.T) {
		t.Parallel()

		sqlDB, err := sql.Open("sqlite3", ":memory:")
		require.NoError(t, err)
		defer func() { _ = sqlDB.Close() }()

		ctx := context.Background()
		mockPlatform := mocks.NewMockPlatform()
		mockPlatform.On("ID").Return("test-platform")

		mediaDB := &mediadb.MediaDB{}
		err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
		require.NoError(t, err)

		// Insert only systems, no other data
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'NES', 'Nintendo Entertainment System')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Systems (DBID, SystemID, Name) VALUES (2, 'SNES', 'Super Nintendo')")
		require.NoError(t, err)

		scanState := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   0,
			TitlesIndex:    0,
			MediaIndex:     0,
			TagTypesIndex:  0,
			TagsIndex:      0,
			MediaTagsIndex: 0,
		}

		err = PopulateScanStateFromDB(mediaDB, scanState)
		require.NoError(t, err)

		assert.Equal(t, 2, scanState.SystemsIndex, "Should have SystemsIndex = 2")
		assert.Equal(t, 0, scanState.TitlesIndex, "Should have TitlesIndex = 0 (no titles)")
		assert.Equal(t, 0, scanState.MediaIndex, "Should have MediaIndex = 0 (no media)")
		assert.Equal(t, 0, scanState.TagTypesIndex, "Should have TagTypesIndex = 0 (no tag types)")
		assert.Equal(t, 0, scanState.TagsIndex, "Should have TagsIndex = 0 (no tags)")
		assert.Equal(t, 0, scanState.MediaTagsIndex, "Should have MediaTagsIndex = 0 (no media tags)")
	})

	t.Run("Database With Sparse Data", func(t *testing.T) {
		t.Parallel()

		sqlDB, err := sql.Open("sqlite3", ":memory:")
		require.NoError(t, err)
		defer func() { _ = sqlDB.Close() }()

		ctx := context.Background()
		mockPlatform := mocks.NewMockPlatform()
		mockPlatform.On("ID").Return("test-platform")

		mediaDB := &mediadb.MediaDB{}
		err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
		require.NoError(t, err)

		// Insert sparse data with gaps
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Systems (DBID, SystemID, Name) VALUES (10, 'NES', 'Nintendo Entertainment System')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (25, 10, 'test_game', 'Test Game')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO TagTypes (DBID, Type) VALUES (5, 'TestType')")
		require.NoError(t, err)

		scanState := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   0,
			TitlesIndex:    0,
			MediaIndex:     0,
			TagTypesIndex:  0,
			TagsIndex:      0,
			MediaTagsIndex: 0,
		}

		err = PopulateScanStateFromDB(mediaDB, scanState)
		require.NoError(t, err)

		// Should use MAX values, not counts
		assert.Equal(t, 10, scanState.SystemsIndex, "Should use max DBID (10), not count (1)")
		assert.Equal(t, 25, scanState.TitlesIndex, "Should use max DBID (25), not count (1)")
		assert.Equal(t, 0, scanState.MediaIndex, "Should be 0 (no media)")
		assert.Equal(t, 5, scanState.TagTypesIndex, "Should use max DBID (5), not count (1)")
		assert.Equal(t, 0, scanState.TagsIndex, "Should be 0 (no tags)")
		assert.Equal(t, 0, scanState.MediaTagsIndex, "Should be 0 (no media tags)")
	})

	t.Run("Error Handling With Mock Database", func(t *testing.T) {
		t.Parallel()

		// Use mock database to simulate errors - function should handle gracefully
		mockDB := &helpers.MockMediaDBI{}
		mockDB.On("GetMaxSystemID").Return(int64(0), assert.AnError)
		mockDB.On("GetMaxTitleID").Return(int64(5), nil)
		mockDB.On("GetMaxMediaID").Return(int64(0), assert.AnError)
		mockDB.On("GetMaxTagTypeID").Return(int64(3), nil)
		mockDB.On("GetMaxTagID").Return(int64(0), assert.AnError)
		mockDB.On("GetMaxMediaTagID").Return(int64(10), nil)
		mockDB.On("GetTotalMediaCount").Return(0, nil).Maybe()
		mockDB.On("GetAllSystems").Return([]database.System{}, assert.AnError)
		mockDB.On("GetAllMediaTitles").Return([]database.MediaTitle{}, assert.AnError)
		mockDB.On("GetAllMedia").Return([]database.Media{}, assert.AnError)

		scanState := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   0,
			TitlesIndex:    0,
			MediaIndex:     0,
			TagTypesIndex:  0,
			TagsIndex:      0,
			MediaTagsIndex: 0,
		}

		err := PopulateScanStateFromDB(mockDB, scanState)
		require.NoError(t, err, "Function should handle errors gracefully, not return error")

		// Check that it fell back to 0 for failed calls, used values for successful calls
		assert.Equal(t, 0, scanState.SystemsIndex, "Should fall back to 0 on error")
		assert.Equal(t, 5, scanState.TitlesIndex, "Should use successful return value")
		assert.Equal(t, 0, scanState.MediaIndex, "Should fall back to 0 on error")
		assert.Equal(t, 3, scanState.TagTypesIndex, "Should use successful return value")
		assert.Equal(t, 0, scanState.TagsIndex, "Should fall back to 0 on error")
		assert.Equal(t, 10, scanState.MediaTagsIndex, "Should use successful return value")

		mockDB.AssertExpectations(t)
	})

	t.Run("Error Resilience", func(t *testing.T) {
		t.Parallel()

		// Test that function is resilient to database errors and falls back to 0
		mockDB := &helpers.MockMediaDBI{}
		mockDB.On("GetMaxSystemID").Return(int64(0), assert.AnError)
		mockDB.On("GetMaxTitleID").Return(int64(10), nil)
		mockDB.On("GetMaxMediaID").Return(int64(0), assert.AnError)
		mockDB.On("GetMaxTagTypeID").Return(int64(3), nil)
		mockDB.On("GetMaxTagID").Return(int64(0), assert.AnError)
		mockDB.On("GetMaxMediaTagID").Return(int64(25), nil)
		mockDB.On("GetTotalMediaCount").Return(0, nil).Maybe()
		mockDB.On("GetAllSystems").Return([]database.System{}, assert.AnError)
		mockDB.On("GetAllMediaTitles").Return([]database.MediaTitle{}, assert.AnError)
		mockDB.On("GetAllMedia").Return([]database.Media{}, assert.AnError)

		scanState := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   0,
			TitlesIndex:    0,
			MediaIndex:     0,
			TagTypesIndex:  0,
			TagsIndex:      0,
			MediaTagsIndex: 0,
		}

		err := PopulateScanStateFromDB(mockDB, scanState)
		require.NoError(t, err, "Function should not return error, but handle errors gracefully")

		// Check that failed calls fall back to 0, successful calls use the returned value
		assert.Equal(t, 0, scanState.SystemsIndex, "SystemsIndex should fall back to 0 on error")
		assert.Equal(t, 10, scanState.TitlesIndex, "TitlesIndex should use successful return value")
		assert.Equal(t, 0, scanState.MediaIndex, "MediaIndex should fall back to 0 on error")
		assert.Equal(t, 3, scanState.TagTypesIndex, "TagTypesIndex should use successful return value")
		assert.Equal(t, 0, scanState.TagsIndex, "TagsIndex should fall back to 0 on error")
		assert.Equal(t, 25, scanState.MediaTagsIndex, "MediaTagsIndex should use successful return value")

		mockDB.AssertExpectations(t)
	})

	t.Run("Successful Complete Population", func(t *testing.T) {
		t.Parallel()

		sqlDB, err := sql.Open("sqlite3", ":memory:")
		require.NoError(t, err)
		defer func() { _ = sqlDB.Close() }()

		ctx := context.Background()
		mockPlatform := mocks.NewMockPlatform()
		mockPlatform.On("ID").Return("test-platform")

		mediaDB := &mediadb.MediaDB{}
		err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
		require.NoError(t, err)

		// Insert complete data set
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'NES', 'Nintendo Entertainment System')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (1, 1, 'test_game_1', 'Test Game 1')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, 'test_game_2', 'Test Game 2')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Media (DBID, MediaTitleDBID, Path) VALUES (1, 1, 'path/to/game1.nes')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Media (DBID, MediaTitleDBID, Path) VALUES (2, 2, 'path/to/game2.nes')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO TagTypes (DBID, Type) VALUES (1, 'Genre')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES (1, 1, 'Action')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES (2, 1, 'Adventure')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO MediaTags (DBID, MediaDBID, TagDBID) VALUES (1, 1, 1)")
		require.NoError(t, err)

		scanState := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   0,
			TitlesIndex:    0,
			MediaIndex:     0,
			TagTypesIndex:  0,
			TagsIndex:      0,
			MediaTagsIndex: 0,
		}

		err = PopulateScanStateFromDB(mediaDB, scanState)
		require.NoError(t, err)

		// Verify all values are populated correctly
		assert.Equal(t, 1, scanState.SystemsIndex, "Should have max system ID")
		assert.Equal(t, 2, scanState.TitlesIndex, "Should have max title ID")
		assert.Equal(t, 2, scanState.MediaIndex, "Should have max media ID")
		assert.Equal(t, 1, scanState.TagTypesIndex, "Should have max tag type ID")
		assert.Equal(t, 2, scanState.TagsIndex, "Should have max tag ID")
		assert.Equal(t, 1, scanState.MediaTagsIndex, "Should have max media tag ID")
	})

	t.Run("Large ID Values", func(t *testing.T) {
		t.Parallel()

		sqlDB, err := sql.Open("sqlite3", ":memory:")
		require.NoError(t, err)
		defer func() { _ = sqlDB.Close() }()

		ctx := context.Background()
		mockPlatform := mocks.NewMockPlatform()
		mockPlatform.On("ID").Return("test-platform")

		mediaDB := &mediadb.MediaDB{}
		err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
		require.NoError(t, err)

		// Insert data with very large IDs
		const largeID = 2147483647 // Near max int32
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Systems (DBID, SystemID, Name) VALUES (?, 'NES', 'Nintendo Entertainment System')", largeID)
		require.NoError(t, err)

		scanState := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   0,
			TitlesIndex:    0,
			MediaIndex:     0,
			TagTypesIndex:  0,
			TagsIndex:      0,
			MediaTagsIndex: 0,
		}

		err = PopulateScanStateFromDB(mediaDB, scanState)
		require.NoError(t, err)

		assert.Equal(t, int(largeID), scanState.SystemsIndex, "Should handle large ID values")
	})
}
