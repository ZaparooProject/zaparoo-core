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
			SystemIDs:     make(map[string]int),
			TitleIDs:      make(map[string]int),
			MediaIDs:      make(map[string]int),
			TagTypeIDs:    make(map[string]int),
			TagIDs:        make(map[string]int),
			SystemsIndex:  999, // Set to non-zero to verify it gets reset
			TitlesIndex:   999,
			MediaIndex:    999,
			TagTypesIndex: 999,
			TagsIndex:     999,
		}

		err = PopulateScanStateFromDB(ctx, mediaDB, scanState)
		require.NoError(t, err)

		// All indexes should be 0 for empty database
		assert.Equal(t, 0, scanState.SystemsIndex, "Empty DB should have SystemsIndex = 0")
		assert.Equal(t, 0, scanState.TitlesIndex, "Empty DB should have TitlesIndex = 0")
		assert.Equal(t, 0, scanState.MediaIndex, "Empty DB should have MediaIndex = 0")
		assert.Equal(t, 0, scanState.TagTypesIndex, "Empty DB should have TagTypesIndex = 0")
		assert.Equal(t, 0, scanState.TagsIndex, "Empty DB should have TagsIndex = 0")
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
			SystemIDs:     make(map[string]int),
			TitleIDs:      make(map[string]int),
			MediaIDs:      make(map[string]int),
			TagTypeIDs:    make(map[string]int),
			TagIDs:        make(map[string]int),
			SystemsIndex:  0,
			TitlesIndex:   0,
			MediaIndex:    0,
			TagTypesIndex: 0,
			TagsIndex:     0,
		}

		err = PopulateScanStateFromDB(ctx, mediaDB, scanState)
		require.NoError(t, err)

		assert.Equal(t, 2, scanState.SystemsIndex, "Should have SystemsIndex = 2")
		assert.Equal(t, 0, scanState.TitlesIndex, "Should have TitlesIndex = 0 (no titles)")
		assert.Equal(t, 0, scanState.MediaIndex, "Should have MediaIndex = 0 (no media)")
		assert.Equal(t, 0, scanState.TagTypesIndex, "Should have TagTypesIndex = 0 (no tag types)")
		assert.Equal(t, 0, scanState.TagsIndex, "Should have TagsIndex = 0 (no tags)")
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
			SystemIDs:     make(map[string]int),
			TitleIDs:      make(map[string]int),
			MediaIDs:      make(map[string]int),
			TagTypeIDs:    make(map[string]int),
			TagIDs:        make(map[string]int),
			SystemsIndex:  0,
			TitlesIndex:   0,
			MediaIndex:    0,
			TagTypesIndex: 0,
			TagsIndex:     0,
		}

		err = PopulateScanStateFromDB(ctx, mediaDB, scanState)
		require.NoError(t, err)

		// Should use MAX values, not counts
		assert.Equal(t, 10, scanState.SystemsIndex, "Should use max DBID (10), not count (1)")
		assert.Equal(t, 25, scanState.TitlesIndex, "Should use max DBID (25), not count (1)")
		assert.Equal(t, 0, scanState.MediaIndex, "Should be 0 (no media)")
		assert.Equal(t, 5, scanState.TagTypesIndex, "Should use max DBID (5), not count (1)")
		assert.Equal(t, 0, scanState.TagsIndex, "Should be 0 (no tags)")
	})

	t.Run("Error Handling With Mock Database", func(t *testing.T) {
		t.Parallel()

		// Test that function fails fast on first error instead of continuing
		mockDB := &helpers.MockMediaDBI{}
		mockDB.On("GetMaxSystemID").Return(int64(0), assert.AnError).Once()

		scanState := &database.ScanState{
			SystemIDs:     make(map[string]int),
			TitleIDs:      make(map[string]int),
			MediaIDs:      make(map[string]int),
			TagTypeIDs:    make(map[string]int),
			TagIDs:        make(map[string]int),
			SystemsIndex:  0,
			TitlesIndex:   0,
			MediaIndex:    0,
			TagTypesIndex: 0,
			TagsIndex:     0,
		}

		err := PopulateScanStateFromDB(context.Background(), mockDB, scanState)
		// Function now fails fast on first error
		require.Error(t, err, "Function should fail fast when database operations fail")
		assert.Contains(t, err.Error(), "failed to get max system ID", "Error should indicate which operation failed")

		mockDB.AssertExpectations(t)
	})

	t.Run("Error On GetAllSystems", func(t *testing.T) {
		t.Parallel()

		// Test that function fails when GetAllSystems errors
		mockDB := &helpers.MockMediaDBI{}
		mockDB.On("GetMaxSystemID").Return(int64(5), nil).Once()
		mockDB.On("GetMaxTitleID").Return(int64(10), nil).Once()
		mockDB.On("GetMaxMediaID").Return(int64(15), nil).Once()
		mockDB.On("GetMaxTagTypeID").Return(int64(3), nil).Once()
		mockDB.On("GetMaxTagID").Return(int64(20), nil).Once()
		// Fail on GetAllSystems
		mockDB.On("GetAllSystems").Return([]database.System{}, assert.AnError).Once()

		scanState := &database.ScanState{
			SystemIDs:     make(map[string]int),
			TitleIDs:      make(map[string]int),
			MediaIDs:      make(map[string]int),
			TagTypeIDs:    make(map[string]int),
			TagIDs:        make(map[string]int),
			SystemsIndex:  0,
			TitlesIndex:   0,
			MediaIndex:    0,
			TagTypesIndex: 0,
			TagsIndex:     0,
		}

		err := PopulateScanStateFromDB(context.Background(), mockDB, scanState)
		// Function should fail when data loading fails
		require.Error(t, err, "Function should return error when GetAllSystems fails")
		assert.Contains(
			t, err.Error(), "failed to get existing systems",
			"Error should indicate which operation failed",
		)

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
			"INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (1, 1, 1, 'path/to/game1.nes')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 1, 'path/to/game2.nes')")
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
			"INSERT INTO MediaTags (MediaDBID, TagDBID) VALUES (1, 1)")
		require.NoError(t, err)

		scanState := &database.ScanState{
			SystemIDs:     make(map[string]int),
			TitleIDs:      make(map[string]int),
			MediaIDs:      make(map[string]int),
			TagTypeIDs:    make(map[string]int),
			TagIDs:        make(map[string]int),
			SystemsIndex:  0,
			TitlesIndex:   0,
			MediaIndex:    0,
			TagTypesIndex: 0,
			TagsIndex:     0,
		}

		err = PopulateScanStateFromDB(ctx, mediaDB, scanState)
		require.NoError(t, err)

		// Verify all values are populated correctly
		assert.Equal(t, 1, scanState.SystemsIndex, "Should have max system ID")
		assert.Equal(t, 2, scanState.TitlesIndex, "Should have max title ID")
		assert.Equal(t, 2, scanState.MediaIndex, "Should have max media ID")
		assert.Equal(t, 1, scanState.TagTypesIndex, "Should have max tag type ID")
		assert.Equal(t, 2, scanState.TagsIndex, "Should have max tag ID")
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
			SystemIDs:     make(map[string]int),
			TitleIDs:      make(map[string]int),
			MediaIDs:      make(map[string]int),
			TagTypeIDs:    make(map[string]int),
			TagIDs:        make(map[string]int),
			SystemsIndex:  0,
			TitlesIndex:   0,
			MediaIndex:    0,
			TagTypesIndex: 0,
			TagsIndex:     0,
		}

		err = PopulateScanStateFromDB(ctx, mediaDB, scanState)
		require.NoError(t, err)

		assert.Equal(t, int(largeID), scanState.SystemsIndex, "Should handle large ID values")
	})

	t.Run("DoesNotLoadTitlesOrMedia", func(t *testing.T) {
		t.Parallel()

		// This test verifies that PopulateScanStateFromDB no longer loads TitleIDs and MediaIDs
		// (they are now lazy-loaded per-system via PopulateScanStateForSystem)
		sqlDB, err := sql.Open("sqlite3", ":memory:")
		require.NoError(t, err)
		defer func() { _ = sqlDB.Close() }()

		ctx := context.Background()
		mockPlatform := mocks.NewMockPlatform()
		mockPlatform.On("ID").Return("test-platform")

		mediaDB := &mediadb.MediaDB{}
		err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
		require.NoError(t, err)

		// Insert systems, titles, and media
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'NES', 'Nintendo Entertainment System')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (1, 1, 'test_game', 'Test Game')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (1, 1, 1, 'path/to/game.nes')")
		require.NoError(t, err)

		scanState := &database.ScanState{
			SystemIDs:     make(map[string]int),
			TitleIDs:      make(map[string]int),
			MediaIDs:      make(map[string]int),
			TagTypeIDs:    make(map[string]int),
			TagIDs:        make(map[string]int),
			SystemsIndex:  0,
			TitlesIndex:   0,
			MediaIndex:    0,
			TagTypesIndex: 0,
			TagsIndex:     0,
		}

		err = PopulateScanStateFromDB(ctx, mediaDB, scanState)
		require.NoError(t, err)

		// Max indexes should be set correctly
		assert.Equal(t, 1, scanState.SystemsIndex, "Should have SystemsIndex = 1")
		assert.Equal(t, 1, scanState.TitlesIndex, "Should have TitlesIndex = 1")
		assert.Equal(t, 1, scanState.MediaIndex, "Should have MediaIndex = 1")

		// SystemIDs map should be populated
		assert.Len(t, scanState.SystemIDs, 1, "SystemIDs map should have 1 entry")
		assert.Equal(t, 1, scanState.SystemIDs["NES"], "SystemIDs should have NES")

		// TitleIDs and MediaIDs maps should NOT be populated (lazy loading)
		assert.Empty(t, scanState.TitleIDs, "TitleIDs should be empty (lazy loaded per-system)")
		assert.Empty(t, scanState.MediaIDs, "MediaIDs should be empty (lazy loaded per-system)")
	})
}

// TestPopulateScanStateForSystem tests the per-system lazy loading function
func TestPopulateScanStateForSystem(t *testing.T) {
	t.Parallel()

	t.Run("LoadsOnlyRequestedSystem", func(t *testing.T) {
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

		// Insert multiple systems with data
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'NES', 'Nintendo Entertainment System')")
		require.NoError(t, err)
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Systems (DBID, SystemID, Name) VALUES (2, 'SNES', 'Super Nintendo')")
		require.NoError(t, err)

		// Insert titles for both systems
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (1, 1, 'nes_game_1', 'NES Game 1')")
		require.NoError(t, err)
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, 'nes_game_2', 'NES Game 2')")
		require.NoError(t, err)
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (3, 2, 'snes_game_1', 'SNES Game 1')")
		require.NoError(t, err)

		// Insert media for both systems
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (1, 1, 1, 'path/to/nes1.nes')")
		require.NoError(t, err)
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 1, 'path/to/nes2.nes')")
		require.NoError(t, err)
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (3, 3, 2, 'path/to/snes1.sfc')")
		require.NoError(t, err)

		scanState := &database.ScanState{
			SystemIDs:     make(map[string]int),
			TitleIDs:      make(map[string]int),
			MediaIDs:      make(map[string]int),
			TagTypeIDs:    make(map[string]int),
			TagIDs:        make(map[string]int),
			SystemsIndex:  2,
			TitlesIndex:   3,
			MediaIndex:    3,
			TagTypesIndex: 0,
			TagsIndex:     0,
		}

		// Load only NES data
		err = PopulateScanStateForSystem(ctx, mediaDB, scanState, "NES")
		require.NoError(t, err)

		// Should have only NES titles and media loaded
		assert.Len(t, scanState.TitleIDs, 2, "Should have 2 NES titles")
		assert.Len(t, scanState.MediaIDs, 2, "Should have 2 NES media")

		// Verify the correct keys are present
		assert.Equal(t, 1, scanState.TitleIDs["NES:nes_game_1"], "Should have NES:nes_game_1")
		assert.Equal(t, 2, scanState.TitleIDs["NES:nes_game_2"], "Should have NES:nes_game_2")
		assert.Equal(t, 1, scanState.MediaIDs["NES:path/to/nes1.nes"], "Should have NES:path/to/nes1.nes")
		assert.Equal(t, 2, scanState.MediaIDs["NES:path/to/nes2.nes"], "Should have NES:path/to/nes2.nes")

		// SNES data should NOT be loaded
		_, hasTitle := scanState.TitleIDs["SNES:snes_game_1"]
		assert.False(t, hasTitle, "Should NOT have SNES titles")
		_, hasMedia := scanState.MediaIDs["SNES:path/to/snes1.sfc"]
		assert.False(t, hasMedia, "Should NOT have SNES media")
	})

	t.Run("EmptySystemReturnsNoError", func(t *testing.T) {
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

		// Insert a system but no data for it
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'NES', 'Nintendo Entertainment System')")
		require.NoError(t, err)

		scanState := &database.ScanState{
			SystemIDs:     make(map[string]int),
			TitleIDs:      make(map[string]int),
			MediaIDs:      make(map[string]int),
			TagTypeIDs:    make(map[string]int),
			TagIDs:        make(map[string]int),
			SystemsIndex:  1,
			TitlesIndex:   0,
			MediaIndex:    0,
			TagTypesIndex: 0,
			TagsIndex:     0,
		}

		// Loading a system with no data should succeed (empty result)
		err = PopulateScanStateForSystem(ctx, mediaDB, scanState, "NES")
		require.NoError(t, err)

		assert.Empty(t, scanState.TitleIDs, "Should have no titles")
		assert.Empty(t, scanState.MediaIDs, "Should have no media")
	})

	t.Run("NonExistentSystemReturnsNoError", func(t *testing.T) {
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

		scanState := &database.ScanState{
			SystemIDs:     make(map[string]int),
			TitleIDs:      make(map[string]int),
			MediaIDs:      make(map[string]int),
			TagTypeIDs:    make(map[string]int),
			TagIDs:        make(map[string]int),
			SystemsIndex:  0,
			TitlesIndex:   0,
			MediaIndex:    0,
			TagTypesIndex: 0,
			TagsIndex:     0,
		}

		// Loading a non-existent system should succeed (empty result)
		err = PopulateScanStateForSystem(ctx, mediaDB, scanState, "NONEXISTENT")
		require.NoError(t, err)

		assert.Empty(t, scanState.TitleIDs, "Should have no titles")
		assert.Empty(t, scanState.MediaIDs, "Should have no media")
	})

	t.Run("MultipleCallsAppendData", func(t *testing.T) {
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

		// Insert multiple systems with data
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'NES', 'Nintendo Entertainment System')")
		require.NoError(t, err)
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Systems (DBID, SystemID, Name) VALUES (2, 'SNES', 'Super Nintendo')")
		require.NoError(t, err)

		// Insert titles and media for both systems
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (1, 1, 'nes_game', 'NES Game')")
		require.NoError(t, err)
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 2, 'snes_game', 'SNES Game')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (1, 1, 1, 'path/to/nes.nes')")
		require.NoError(t, err)
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 2, 'path/to/snes.sfc')")
		require.NoError(t, err)

		scanState := &database.ScanState{
			SystemIDs:     make(map[string]int),
			TitleIDs:      make(map[string]int),
			MediaIDs:      make(map[string]int),
			TagTypeIDs:    make(map[string]int),
			TagIDs:        make(map[string]int),
			SystemsIndex:  2,
			TitlesIndex:   2,
			MediaIndex:    2,
			TagTypesIndex: 0,
			TagsIndex:     0,
		}

		// Load NES first
		err = PopulateScanStateForSystem(ctx, mediaDB, scanState, "NES")
		require.NoError(t, err)
		assert.Len(t, scanState.TitleIDs, 1, "Should have 1 title after NES load")
		assert.Len(t, scanState.MediaIDs, 1, "Should have 1 media after NES load")

		// Load SNES - should append, not replace
		err = PopulateScanStateForSystem(ctx, mediaDB, scanState, "SNES")
		require.NoError(t, err)
		assert.Len(t, scanState.TitleIDs, 2, "Should have 2 titles after SNES load")
		assert.Len(t, scanState.MediaIDs, 2, "Should have 2 media after SNES load")

		// Verify both systems' data is present
		assert.Equal(t, 1, scanState.TitleIDs["NES:nes_game"], "NES title should still be present")
		assert.Equal(t, 2, scanState.TitleIDs["SNES:snes_game"], "SNES title should be added")
	})

	t.Run("CancellationHandled", func(t *testing.T) {
		t.Parallel()

		sqlDB, err := sql.Open("sqlite3", ":memory:")
		require.NoError(t, err)
		defer func() { _ = sqlDB.Close() }()

		ctx, cancel := context.WithCancel(context.Background())
		mockPlatform := mocks.NewMockPlatform()
		mockPlatform.On("ID").Return("test-platform")

		mediaDB := &mediadb.MediaDB{}
		err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
		require.NoError(t, err)

		scanState := &database.ScanState{
			SystemIDs:     make(map[string]int),
			TitleIDs:      make(map[string]int),
			MediaIDs:      make(map[string]int),
			TagTypeIDs:    make(map[string]int),
			TagIDs:        make(map[string]int),
			SystemsIndex:  0,
			TitlesIndex:   0,
			MediaIndex:    0,
			TagTypesIndex: 0,
			TagsIndex:     0,
		}

		// Cancel before calling
		cancel()

		err = PopulateScanStateForSystem(ctx, mediaDB, scanState, "NES")
		require.Error(t, err, "Should return error when context is cancelled")
		assert.ErrorIs(t, err, context.Canceled, "Error should be context.Canceled")
	})
}
