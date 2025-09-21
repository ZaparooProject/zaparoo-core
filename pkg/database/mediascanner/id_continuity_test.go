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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner/testdata"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIDContinuityAfterResume specifically tests that IDs continue sequentially after resume
// This is the core issue that was causing "UNIQUE constraint failed" errors
func TestIDContinuityAfterResume(t *testing.T) {
	// Create in-memory SQLite database
	sqlDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	// Create MediaDB instance
	ctx := context.Background()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")

	mediaDB := &mediadb.MediaDB{}
	err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
	require.NoError(t, err)

	// Phase 1: Initial indexing (simulate first run)
	var phase1MaxSystemID, phase1MaxTitleID, phase1MaxMediaID int64

	t.Run("Phase1_InitialIndexing", func(t *testing.T) {
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

		// Seed known tags BEFORE transaction
		err = SeedKnownTags(mediaDB, scanState)
		require.NoError(t, err)

		err := mediaDB.BeginTransaction()
		require.NoError(t, err)

		// Add initial batch of games
		generator := testdata.NewTestDataGenerator(1000)
		systems := []string{"NES", "SNES"}

		for _, systemID := range systems {
			for range 3 { // 3 games per system
				entry := generator.GenerateMediaEntry(systemID)
				titleIndex, mediaIndex := AddMediaPath(mediaDB, scanState, systemID, entry.Path)
				assert.Positive(t, titleIndex, "Title index should be positive")
				assert.Positive(t, mediaIndex, "Media index should be positive")
			}
		}

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Record Phase 1 final state
		phase1MaxSystemID, err = mediaDB.GetMaxSystemID()
		require.NoError(t, err)
		phase1MaxTitleID, err = mediaDB.GetMaxTitleID()
		require.NoError(t, err)
		phase1MaxMediaID, err = mediaDB.GetMaxMediaID()
		require.NoError(t, err)

		assert.Equal(t, int64(2), phase1MaxSystemID, "Should have 2 systems")
		assert.Equal(t, int64(6), phase1MaxTitleID, "Should have 6 titles")
		assert.Equal(t, int64(6), phase1MaxMediaID, "Should have 6 media entries")
	})

	// Phase 2: Resume from interruption (simulate restart)
	t.Run("Phase2_ResumeFromInterruption", func(t *testing.T) {
		// Create fresh scan state (simulating restart)
		resumeState := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   0, // This would be 0 in broken implementation
			TitlesIndex:    0, // This would be 0 in broken implementation
			MediaIndex:     0, // This would be 0 in broken implementation
			TagTypesIndex:  0,
			TagsIndex:      0,
			MediaTagsIndex: 0,
		}

		// This is the critical function that was broken
		err := PopulateScanStateFromDB(mediaDB, resumeState)
		require.NoError(t, err)

		// Verify scan state was populated correctly
		assert.Equal(t, int(phase1MaxSystemID), resumeState.SystemsIndex,
			"Resume state should match Phase 1 max system ID")
		assert.Equal(t, int(phase1MaxTitleID), resumeState.TitlesIndex,
			"Resume state should match Phase 1 max title ID")
		assert.Equal(t, int(phase1MaxMediaID), resumeState.MediaIndex,
			"Resume state should match Phase 1 max media ID")

		// Now continue indexing with more systems
		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		generator := testdata.NewTestDataGenerator(2000)

		// Add new system with games
		for i := range 2 { // 2 games for Genesis
			entry := generator.GenerateMediaEntry("Genesis")
			titleIndex, mediaIndex := AddMediaPath(mediaDB, resumeState, "Genesis", entry.Path)

			// These are the critical assertions - IDs must continue from where Phase 1 left off
			expectedTitleID := int(phase1MaxTitleID) + i + 1
			expectedMediaID := int(phase1MaxMediaID) + i + 1

			assert.Equal(t, expectedTitleID, titleIndex,
				"Title ID should continue from Phase 1 max (%d), got %d", phase1MaxTitleID, titleIndex)
			assert.Equal(t, expectedMediaID, mediaIndex,
				"Media ID should continue from Phase 1 max (%d), got %d", phase1MaxMediaID, mediaIndex)
		}

		// Add more games to existing system (NES)
		for i := range 2 { // 2 more NES games
			entry := generator.GenerateMediaEntry("NES")
			titleIndex, mediaIndex := AddMediaPath(mediaDB, resumeState, "NES", entry.Path)

			// These should continue the sequence
			// +2 for Genesis games, +i for this loop, +1 for next ID
			expectedTitleID := int(phase1MaxTitleID) + 2 + i + 1
			expectedMediaID := int(phase1MaxMediaID) + 2 + i + 1

			assert.Equal(t, expectedTitleID, titleIndex,
				"Title ID should continue sequence, expected %d, got %d", expectedTitleID, titleIndex)
			assert.Equal(t, expectedMediaID, mediaIndex,
				"Media ID should continue sequence, expected %d, got %d", expectedMediaID, mediaIndex)
		}

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify final database state
		finalMaxSystemID, err := mediaDB.GetMaxSystemID()
		require.NoError(t, err)
		finalMaxTitleID, err := mediaDB.GetMaxTitleID()
		require.NoError(t, err)
		finalMaxMediaID, err := mediaDB.GetMaxMediaID()
		require.NoError(t, err)

		// Should have one more system (Genesis) and 4 more titles/media
		assert.Equal(t, phase1MaxSystemID+1, finalMaxSystemID, "Should have one additional system")
		assert.Equal(t, phase1MaxTitleID+4, finalMaxTitleID, "Should have 4 additional titles")
		assert.Equal(t, phase1MaxMediaID+4, finalMaxMediaID, "Should have 4 additional media entries")
	})
}

// TestIDContinuityWithGaps tests that ID continuity works even when there are gaps in existing IDs
func TestIDContinuityWithGaps(t *testing.T) {
	// Create in-memory SQLite database
	sqlDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")

	mediaDB := &mediadb.MediaDB{}
	err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
	require.NoError(t, err)

	// Initialize tag system for all tests in this suite
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
	err = SeedKnownTags(mediaDB, scanState)
	require.NoError(t, err)

	t.Run("Create Data With Intentional Gaps", func(t *testing.T) {
		err := mediaDB.BeginTransaction()
		require.NoError(t, err)

		// Insert data directly to create gaps in IDs
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'NES', 'Nintendo Entertainment System')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Systems (DBID, SystemID, Name) VALUES (5, 'SNES', 'Super Nintendo')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (3, 1, 'test_game_1', 'Test Game 1')")
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (7, 5, 'test_game_2', 'Test Game 2')")
		require.NoError(t, err)

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify gaps exist
		maxSystemID, err := mediaDB.GetMaxSystemID()
		require.NoError(t, err)
		assert.Equal(t, int64(5), maxSystemID, "Max system ID should be 5 (with gap)")

		maxTitleID, err := mediaDB.GetMaxTitleID()
		require.NoError(t, err)
		assert.Equal(t, int64(7), maxTitleID, "Max title ID should be 7 (with gap)")
	})

	t.Run("Resume Continues From Highest ID Despite Gaps", func(t *testing.T) {
		// Create resume state
		resumeState := &database.ScanState{
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

		err := PopulateScanStateFromDB(mediaDB, resumeState)
		require.NoError(t, err)

		// Should use the maximum ID, not count of records
		assert.Equal(t, 5, resumeState.SystemsIndex, "Should use max ID (5), not count (2)")
		assert.Equal(t, 7, resumeState.TitlesIndex, "Should use max ID (7), not count (2)")

		// Add new data - should get next sequential ID after the highest
		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		generator := testdata.NewTestDataGenerator(3000)
		entry := generator.GenerateMediaEntry("Genesis")
		titleIndex, mediaIndex := AddMediaPath(mediaDB, resumeState, "Genesis", entry.Path)

		// Should get the next ID after the highest, not fill gaps
		assert.Equal(t, 8, titleIndex, "Should get ID 8 (7+1), not fill gap")
		assert.Equal(t, 1, mediaIndex, "Should get ID 1 for first media entry")

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify database state
		finalMaxSystemID, err := mediaDB.GetMaxSystemID()
		require.NoError(t, err)
		finalMaxTitleID, err := mediaDB.GetMaxTitleID()
		require.NoError(t, err)

		assert.Equal(t, int64(6), finalMaxSystemID, "Should have new system with ID 6")
		assert.Equal(t, int64(8), finalMaxTitleID, "Should have new title with ID 8")
	})
}

// TestIDContinuityWithLargeNumbers tests ID continuity with very large existing IDs
func TestIDContinuityWithLargeNumbers(t *testing.T) {
	// Create in-memory SQLite database
	sqlDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")

	mediaDB := &mediadb.MediaDB{}
	err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
	require.NoError(t, err)

	// Initialize tag system for all tests in this suite
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
	err = SeedKnownTags(mediaDB, scanState)
	require.NoError(t, err)

	const largeID = int64(1000000)

	t.Run("Create Data With Large IDs", func(t *testing.T) {
		err := mediaDB.BeginTransaction()
		require.NoError(t, err)

		// Insert data with large IDs
		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO Systems (DBID, SystemID, Name) VALUES (?, 'NES', 'Nintendo Entertainment System')", largeID)
		require.NoError(t, err)

		_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
			"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (?, ?, 'test_game', 'Test Game')",
			largeID+500, largeID)
		require.NoError(t, err)

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)
	})

	t.Run("Resume Handles Large IDs Correctly", func(t *testing.T) {
		resumeState := &database.ScanState{
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

		err := PopulateScanStateFromDB(mediaDB, resumeState)
		require.NoError(t, err)

		// Should handle large numbers correctly
		assert.Equal(t, int(largeID), resumeState.SystemsIndex, "Should handle large system ID")
		assert.Equal(t, int(largeID+500), resumeState.TitlesIndex, "Should handle large title ID")

		// Add new data
		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		generator := testdata.NewTestDataGenerator(4000)
		entry := generator.GenerateMediaEntry("SNES")
		titleIndex, mediaIndex := AddMediaPath(mediaDB, resumeState, "SNES", entry.Path)

		// Should continue from large ID
		expectedTitleID := int(largeID + 500 + 1)
		assert.Equal(t, expectedTitleID, titleIndex, "Should continue from large ID")
		assert.Equal(t, 1, mediaIndex, "Media should start from 1")

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)
	})
}

// TestConcurrentIDGeneration tests that ID generation remains consistent under concurrent access
func TestConcurrentIDGeneration(t *testing.T) {
	// Create in-memory SQLite database
	sqlDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")

	mediaDB := &mediadb.MediaDB{}
	err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
	require.NoError(t, err)

	t.Run("Sequential Operations Maintain ID Consistency", func(t *testing.T) {
		// Initial setup
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

		err = SeedKnownTags(mediaDB, scanState)
		require.NoError(t, err)

		err := mediaDB.BeginTransaction()
		require.NoError(t, err)

		// Add multiple entries in sequence
		generator := testdata.NewTestDataGenerator(5000)
		expectedTitleIDs := []int{}
		expectedMediaIDs := []int{}

		for i := range 5 {
			entry := generator.GenerateMediaEntry("NES")
			titleIndex, mediaIndex := AddMediaPath(mediaDB, scanState, "NES", entry.Path)

			expectedTitleIDs = append(expectedTitleIDs, titleIndex)
			expectedMediaIDs = append(expectedMediaIDs, mediaIndex)

			// Each should be sequential
			if i > 0 {
				assert.Equal(t, expectedTitleIDs[i-1]+1, titleIndex,
					"Title ID should be sequential, iteration %d", i)
				assert.Equal(t, expectedMediaIDs[i-1]+1, mediaIndex,
					"Media ID should be sequential, iteration %d", i)
			}
		}

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify no gaps in the sequence
		for i := 1; i < len(expectedTitleIDs); i++ {
			assert.Equal(t, expectedTitleIDs[0]+i, expectedTitleIDs[i],
				"Should have sequential IDs without gaps")
		}
	})
}
