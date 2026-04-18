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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner/testdata"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBatchModeResumeIndexing tests resume functionality with batch mode enabled.
// This is a critical test that would have caught the INSERT OR IGNORE bug.
//
// The bug: If INSERT OR IGNORE silently fails, the pre-generated DBID stays in
// scanState maps but never gets inserted into the database. This corrupt DBID
// is then used as a foreign key in child tables, causing FK constraint violations.
//
// This test verifies that:
// 1. scanState maps correctly prevent duplicate insert attempts
// 2. Database UNIQUE constraints provide fail-fast behavior if maps fail
// 3. Resume indexing works correctly with batch mode enabled
func TestBatchModeResumeIndexing(t *testing.T) {
	// Create in-memory SQLite database
	sqlDB, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	// Create MediaDB instance
	ctx := context.Background()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")

	mediaDB := &mediadb.MediaDB{}
	err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
	require.NoError(t, err)

	// Generate test data - small batch for focused testing
	testSystems := []string{"NES", "SNES"}
	batch := testdata.CreateReproducibleBatch(testSystems, 3) // 3 games per system = 6 total

	// Seed canonical tags ONCE for the entire test (shared across all subtests)
	sharedScanState := &database.ScanState{
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
	err = SeedCanonicalTags(mediaDB, sharedScanState)
	require.NoError(t, err, "Failed to seed canonical tags")

	t.Run("InitialIndexWithBatchMode", func(t *testing.T) {
		// Test initial indexing with batch mode enabled
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

		// Copy tag state from shared state (tags were already seeded)
		scanState.TagTypesIndex = sharedScanState.TagTypesIndex
		scanState.TagsIndex = sharedScanState.TagsIndex
		for k, v := range sharedScanState.TagTypeIDs {
			scanState.TagTypeIDs[k] = v
		}
		for k, v := range sharedScanState.TagIDs {
			scanState.TagIDs[k] = v
		}

		// Begin transaction with batch mode enabled
		err = mediaDB.BeginTransaction(true)
		require.NoError(t, err)

		// Add systems and media
		for _, systemID := range testSystems {
			entries := batch.Entries[systemID]
			for _, entry := range entries {
				titleIndex, mediaIndex, addErr := AddMediaPath(
					mediaDB, scanState, systemID, entry.Path, false, false, nil, "",
				)
				require.NoError(t, addErr)
				assert.Positive(t, titleIndex, "Title index should be > 0")
				assert.Positive(t, mediaIndex, "Media index should be > 0")
			}
		}

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify data was inserted
		maxSystemID, err := mediaDB.GetMaxSystemID()
		require.NoError(t, err)
		assert.Equal(t, int64(len(testSystems)), maxSystemID, "Should have %d systems", len(testSystems))

		maxTitleID, err := mediaDB.GetMaxTitleID()
		require.NoError(t, err)
		assert.Equal(t, int64(batch.Total), maxTitleID, "Should have %d titles", batch.Total)

		maxMediaID, err := mediaDB.GetMaxMediaID()
		require.NoError(t, err)
		assert.Equal(t, int64(batch.Total), maxMediaID, "Should have %d media entries", batch.Total)
	})

	t.Run("ResumeIndexingWithBatchMode", func(t *testing.T) {
		// Create scan state populated from database (simulating resume)
		resumeState := &database.ScanState{
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

		// Populate from database
		err := PopulateScanStateFromDB(ctx, mediaDB, resumeState)
		require.NoError(t, err)

		// Record state before resume
		originalSystemsIndex := resumeState.SystemsIndex
		originalTitlesIndex := resumeState.TitlesIndex
		originalMediaIndex := resumeState.MediaIndex

		// Now add more data with batch mode (simulating resume)
		err = mediaDB.BeginTransaction(true)
		require.NoError(t, err)

		// Add one more system with games
		newEntries := testdata.CreateReproducibleBatch([]string{"Genesis"}, 2)
		for _, entry := range newEntries.Entries["Genesis"] {
			titleIndex, mediaIndex, addErr := AddMediaPath(
				mediaDB, resumeState, "Genesis", entry.Path, false, false, nil, "",
			)
			require.NoError(t, addErr)

			// Verify the new IDs are sequential from where we left off
			assert.Greater(t, titleIndex, originalTitlesIndex, "New title should get next available ID")
			assert.Greater(t, mediaIndex, originalMediaIndex, "New media should get next available ID")
		}

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify database has the new data with correct IDs
		maxSystemID, _ := mediaDB.GetMaxSystemID()
		maxTitleID, _ := mediaDB.GetMaxTitleID()
		maxMediaID, _ := mediaDB.GetMaxMediaID()

		assert.Equal(t, int64(originalSystemsIndex+1), maxSystemID, "Should have one more system")
		assert.Equal(t, int64(originalTitlesIndex+2), maxTitleID, "Should have 2 more titles")
		assert.Equal(t, int64(originalMediaIndex+2), maxMediaID, "Should have 2 more media")
	})

	t.Run("ReindexingExistingSystemWithBatchMode", func(t *testing.T) {
		// This tests the critical scenario where we re-index an existing system
		// The scanState maps should prevent duplicate inserts
		// If they fail, the database UNIQUE constraints should catch it

		// Create scan state populated from database
		reindexState := &database.ScanState{
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

		// Populate global state from database (systems, tags, max IDs)
		err := PopulateScanStateFromDB(ctx, mediaDB, reindexState)
		require.NoError(t, err)

		// Lazy load NES system data (titles and media) since PopulateScanStateFromDB
		// no longer loads these upfront for performance reasons
		err = PopulateScanStateForSystem(ctx, mediaDB, reindexState, "NES")
		require.NoError(t, err)

		// Get current counts
		originalSystemCount, _ := mediaDB.GetMaxSystemID()
		originalTitleCount, _ := mediaDB.GetMaxTitleID()
		originalMediaCount, _ := mediaDB.GetMaxMediaID()

		// Begin transaction with batch mode
		err = mediaDB.BeginTransaction(true)
		require.NoError(t, err)

		// Try to add the same NES games again (simulating re-indexing)
		// This should NOT create duplicates thanks to scanState maps
		nesEntries := batch.Entries["NES"]
		for _, entry := range nesEntries {
			_, _, addErr := AddMediaPath(mediaDB, reindexState, "NES", entry.Path, false, false, nil, "")
			require.NoError(t, addErr, "Re-indexing should not fail")
		}

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify no duplicates were created
		finalSystemCount, _ := mediaDB.GetMaxSystemID()
		finalTitleCount, _ := mediaDB.GetMaxTitleID()
		finalMediaCount, _ := mediaDB.GetMaxMediaID()

		assert.Equal(t, originalSystemCount, finalSystemCount, "System count should not change")
		assert.Equal(t, originalTitleCount, finalTitleCount, "Title count should not change")
		assert.Equal(t, originalMediaCount, finalMediaCount, "Media count should not change")
	})
}

// TestBatchModeSelectiveIndexing tests selective indexing (truncate + reindex specific systems)
// with batch mode enabled. This scenario is critical because it involves:
// 1. Truncating specific systems from the database
// 2. Re-populating scanState from remaining data
// 3. Re-indexing the truncated systems with batch mode
//
// This is another scenario where the INSERT OR IGNORE bug would manifest.
func TestBatchModeSelectiveIndexing(t *testing.T) {
	// Create in-memory SQLite database
	sqlDB, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	// Create MediaDB instance
	ctx := context.Background()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")

	mediaDB := &mediadb.MediaDB{}
	err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
	require.NoError(t, err)

	// Generate test data for 3 systems
	testSystems := []string{"NES", "SNES", "Genesis"}
	batch := testdata.CreateReproducibleBatch(testSystems, 3) // 3 games per system = 9 total

	// Seed canonical tags ONCE for the entire test (shared across all subtests)
	sharedScanState := &database.ScanState{
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
	err = SeedCanonicalTags(mediaDB, sharedScanState)
	require.NoError(t, err, "Failed to seed canonical tags")

	// Initial index of all systems with batch mode
	t.Run("InitialFullIndex", func(t *testing.T) {
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

		// Copy tag state from shared state (tags were already seeded)
		scanState.TagTypesIndex = sharedScanState.TagTypesIndex
		scanState.TagsIndex = sharedScanState.TagsIndex
		for k, v := range sharedScanState.TagTypeIDs {
			scanState.TagTypeIDs[k] = v
		}
		for k, v := range sharedScanState.TagIDs {
			scanState.TagIDs[k] = v
		}

		// Index all systems with batch mode
		err = mediaDB.BeginTransaction(true)
		require.NoError(t, err)

		for _, systemID := range testSystems {
			entries := batch.Entries[systemID]
			for _, entry := range entries {
				_, _, addErr := AddMediaPath(mediaDB, scanState, systemID, entry.Path, false, false, nil, "")
				require.NoError(t, addErr)
			}
		}

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify initial state
		maxSystemID, _ := mediaDB.GetMaxSystemID()
		assert.Equal(t, int64(3), maxSystemID, "Should have 3 systems")

		maxMediaID, _ := mediaDB.GetMaxMediaID()
		assert.Equal(t, int64(9), maxMediaID, "Should have 9 media entries")
	})

	// Selective indexing: truncate NES, keep SNES and Genesis, then re-index NES
	t.Run("SelectiveReindex_NES_Only", func(t *testing.T) {
		// Truncate NES data
		err = mediaDB.TruncateSystems([]string{"NES"})
		require.NoError(t, err)

		// Verify NES data is gone
		var nesCount int
		err = sqlDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM Media m
			INNER JOIN MediaTitles mt ON mt.DBID = m.MediaTitleDBID
			INNER JOIN Systems s ON s.DBID = mt.SystemDBID
			WHERE s.SystemID = 'NES'
		`).Scan(&nesCount)
		require.NoError(t, err)
		assert.Equal(t, 0, nesCount, "NES data should be truncated")

		// Verify SNES and Genesis data remains
		var otherCount int
		err = sqlDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM Media m
			INNER JOIN MediaTitles mt ON mt.DBID = m.MediaTitleDBID
			INNER JOIN Systems s ON s.DBID = mt.SystemDBID
			WHERE s.SystemID IN ('SNES', 'Genesis')
		`).Scan(&otherCount)
		require.NoError(t, err)
		assert.Equal(t, 6, otherCount, "SNES and Genesis data should remain (6 games)")

		// Create scan state excluding NES (simulating selective indexing)
		selectiveState := &database.ScanState{
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

		// Populate from database (should only have SNES and Genesis)
		err = PopulateScanStateFromDB(ctx, mediaDB, selectiveState)
		require.NoError(t, err)

		// Verify scanState has SNES and Genesis but not NES
		_, hasSNES := selectiveState.SystemIDs["SNES"]
		_, hasGenesis := selectiveState.SystemIDs["Genesis"]
		_, hasNES := selectiveState.SystemIDs["NES"]

		assert.True(t, hasSNES, "scanState should have SNES")
		assert.True(t, hasGenesis, "scanState should have Genesis")
		assert.False(t, hasNES, "scanState should NOT have NES after truncation")

		// Record state before re-indexing
		beforeSystems := selectiveState.SystemsIndex
		beforeTitles := selectiveState.TitlesIndex
		beforeMedia := selectiveState.MediaIndex

		// Re-index NES with batch mode
		err = mediaDB.BeginTransaction(true)
		require.NoError(t, err)

		nesEntries := batch.Entries["NES"]
		for _, entry := range nesEntries {
			titleIndex, mediaIndex, addErr := AddMediaPath(
				mediaDB, selectiveState, "NES", entry.Path, false, false, nil, "",
			)
			require.NoError(t, addErr)

			// Verify new IDs are sequential
			assert.Greater(t, titleIndex, beforeTitles, "New NES titles should get next available IDs")
			assert.Greater(t, mediaIndex, beforeMedia, "New NES media should get next available IDs")
		}

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify final state: should have all 3 systems and 9 media entries again
		maxSystemID, _ := mediaDB.GetMaxSystemID()
		assert.Equal(t, int64(beforeSystems+1), maxSystemID, "Should have one more system (NES re-added)")

		// Check the COUNT of media entries, not max ID
		// (max ID will be higher since we continue from where we left off)
		var totalMediaCount int
		err = sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&totalMediaCount)
		require.NoError(t, err)
		assert.Equal(t, 9, totalMediaCount, "Should have 9 total media entries (3 per system)")

		// Verify NES data is back
		err = sqlDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM Media m
			INNER JOIN MediaTitles mt ON mt.DBID = m.MediaTitleDBID
			INNER JOIN Systems s ON s.DBID = mt.SystemDBID
			WHERE s.SystemID = 'NES'
		`).Scan(&nesCount)
		require.NoError(t, err)
		assert.Equal(t, 3, nesCount, "NES should have 3 games again")

		// Critical verification: No FK constraint violations
		// If the INSERT OR IGNORE bug existed, we'd have corrupt DBIDs causing FK errors
		var fkErrors int
		err = sqlDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM Media m
			LEFT JOIN MediaTitles mt ON mt.DBID = m.MediaTitleDBID
			WHERE mt.DBID IS NULL
		`).Scan(&fkErrors)
		require.NoError(t, err)
		assert.Equal(t, 0, fkErrors, "Should have no FK constraint violations")
	})
}

// TestBatchMode_DuplicateDetection tests that duplicate detection works correctly
// with batch mode enabled. This verifies that scanState maps prevent duplicates
// and that database UNIQUE constraints provide fail-fast behavior if maps fail.
func TestBatchMode_DuplicateDetection(t *testing.T) {
	// Create in-memory SQLite database
	sqlDB, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	// Create MediaDB instance
	ctx := context.Background()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")

	mediaDB := &mediadb.MediaDB{}
	err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
	require.NoError(t, err)

	// Seed canonical tags ONCE for the entire test (shared across all subtests)
	sharedScanState := &database.ScanState{
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
	err = SeedCanonicalTags(mediaDB, sharedScanState)
	require.NoError(t, err, "Failed to seed canonical tags")

	t.Run("IntraBatchDuplicates", func(t *testing.T) {
		// Test adding the same file twice within a single batch
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

		// Copy tag state from shared state (tags were already seeded)
		scanState.TagTypesIndex = sharedScanState.TagTypesIndex
		scanState.TagsIndex = sharedScanState.TagsIndex
		for k, v := range sharedScanState.TagTypeIDs {
			scanState.TagTypeIDs[k] = v
		}
		for k, v := range sharedScanState.TagIDs {
			scanState.TagIDs[k] = v
		}

		// Begin transaction with batch mode
		err = mediaDB.BeginTransaction(true)
		require.NoError(t, err)

		// Add same file twice
		testPath := "/roms/nes/game1.nes"
		title1, media1, addErr1 := AddMediaPath(mediaDB, scanState, "NES", testPath, false, false, nil, "")
		require.NoError(t, addErr1)

		title2, media2, addErr2 := AddMediaPath(mediaDB, scanState, "NES", testPath, false, false, nil, "")
		require.NoError(t, addErr2)

		// Second attempt should return same IDs (no duplicate insert)
		assert.Equal(t, title1, title2, "Same file should return same title ID")
		assert.Equal(t, media1, media2, "Same file should return same media ID")

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify only one entry in database
		var count int
		err = sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media WHERE Path = ?", testPath).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "Should have exactly one media entry")
	})

	t.Run("InterBatchDuplicates", func(t *testing.T) {
		// Test adding the same file across different batches/transactions
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

		// Populate global state from database
		err = PopulateScanStateFromDB(ctx, mediaDB, scanState)
		require.NoError(t, err)

		// Lazy load NES system data since PopulateScanStateFromDB no longer loads per-system data
		err = PopulateScanStateForSystem(ctx, mediaDB, scanState, "NES")
		require.NoError(t, err)

		beforeMedia := scanState.MediaIndex

		// Begin new transaction with batch mode
		err = mediaDB.BeginTransaction(true)
		require.NoError(t, err)

		// Try to add the same file again (it's in the database and scanState)
		testPath := "/roms/nes/game1.nes"
		_, media, err := AddMediaPath(mediaDB, scanState, "NES", testPath, false, false, nil, "")
		require.NoError(t, err)

		// Should not create a new entry
		assert.LessOrEqual(t, media, beforeMedia, "Should reuse existing media ID")

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify still only one entry in database
		var count int
		err = sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media WHERE Path = ?", testPath).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "Should still have exactly one media entry")
	})
}

// TestResumeTruncatesPartialSystem verifies that truncating the partially-indexed
// lastIndexedSystem before re-processing it allows the resume to complete without
// UNIQUE constraint violations.
//
// Scenario reproduced here:
//  1. Index 5 of a system's 10 files and commit (simulating a mid-system commit
//     that sets lastIndexedSystemID).
//  2. Simulate resume: truncate the system, re-populate scanState from DB (now empty
//     for this system), then re-index all 10 files.
//  3. Assert no errors and that Media rows == total files for the system (not doubled).
func TestResumeTruncatesPartialSystem(t *testing.T) {
	sqlDB, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")

	mediaDB := &mediadb.MediaDB{}
	err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
	require.NoError(t, err)

	const systemID = "NES"
	const totalFiles = 10
	const partialFiles = 5 // files committed in the "interrupted" first run

	allEntries := testdata.CreateReproducibleBatch([]string{systemID}, totalFiles).Entries[systemID]
	require.Len(t, allEntries, totalFiles)

	// Shared base scan state (tags, etc.)
	baseState := &database.ScanState{
		SystemIDs:  make(map[string]int),
		TitleIDs:   make(map[string]int),
		MediaIDs:   make(map[string]int),
		TagTypeIDs: make(map[string]int),
		TagIDs:     make(map[string]int),
	}
	require.NoError(t, SeedCanonicalTags(mediaDB, baseState))

	// Step 1: index the first partialFiles and commit (interrupted mid-system run).
	firstRunState := &database.ScanState{
		SystemIDs:     make(map[string]int),
		TitleIDs:      make(map[string]int),
		MediaIDs:      make(map[string]int),
		TagTypeIDs:    baseState.TagTypeIDs,
		TagIDs:        baseState.TagIDs,
		TagTypesIndex: baseState.TagTypesIndex,
		TagsIndex:     baseState.TagsIndex,
	}
	require.NoError(t, mediaDB.BeginTransaction(true))
	for _, entry := range allEntries[:partialFiles] {
		_, _, addErr := AddMediaPath(mediaDB, firstRunState, systemID, entry.Path, false, false, nil, "")
		require.NoError(t, addErr, "first partial index should succeed")
	}
	require.NoError(t, mediaDB.CommitTransaction())

	// Confirm partial state in DB: partialFiles Media rows exist.
	var beforeCount int64
	row := sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media")
	require.NoError(t, row.Scan(&beforeCount))
	require.Equal(t, int64(partialFiles), beforeCount, "partial index should have inserted %d rows", partialFiles)

	// Step 2: simulate resume — mirrors production ordering in mediascanner.go:
	// PopulateScanStateFromDB runs first (before the main loop), then TruncateSystems
	// fires inside the loop for the partial system. This order populates SystemIDs and
	// TagIDs with pre-truncate DBIDs, which become stale after the truncate.
	resumeState := &database.ScanState{
		SystemIDs:     make(map[string]int),
		TitleIDs:      make(map[string]int),
		MediaIDs:      make(map[string]int),
		TagTypeIDs:    baseState.TagTypeIDs,
		TagIDs:        baseState.TagIDs,
		TagTypesIndex: baseState.TagTypesIndex,
		TagsIndex:     baseState.TagsIndex,
	}
	// Populate BEFORE truncate (production order). After this, resumeState.SystemIDs
	// contains the stale DBID for systemID, and resumeState.TagIDs may contain DBIDs
	// for tags that the orphan-cleanup inside TruncateSystems will delete.
	require.NoError(t, PopulateScanStateFromDB(ctx, mediaDB, resumeState))

	require.NoError(t, mediaDB.TruncateSystems([]string{systemID}))

	// Reconcile stale scan state after truncate — mirrors the fix in mediascanner.go.
	// Without this block, AddMediaPath would use the stale SystemIDs DBID for InsertMediaTitle
	// and any stale TagIDs DBIDs for InsertMediaTag, causing FK violations.
	delete(resumeState.SystemIDs, systemID)
	allTags, tagsErr := mediaDB.GetAllTags()
	require.NoError(t, tagsErr)
	tagTypeByDBID := make(map[int64]string, len(resumeState.TagTypeIDs))
	for tt, id := range resumeState.TagTypeIDs {
		tagTypeByDBID[int64(id)] = tt
	}
	resumeState.TagIDs = make(map[string]int, len(allTags))
	for _, tag := range allTags {
		resumeState.TagIDs[database.TagKey(tagTypeByDBID[tag.TypeDBID], tag.Tag)] = int(tag.DBID)
	}
	require.NoError(t, SeedCanonicalTags(mediaDB, resumeState))

	// After truncation, this system has no rows — population should find nothing.
	require.NoError(t, PopulateScanStateForSystem(ctx, mediaDB, resumeState, systemID))
	assert.Empty(t, resumeState.MediaIDs, "MediaIDs should be empty after truncating the partial system")

	// Re-index all files from scratch — must not produce UNIQUE violations.
	require.NoError(t, mediaDB.BeginTransaction(true))
	for _, entry := range allEntries {
		_, _, addErr := AddMediaPath(mediaDB, resumeState, systemID, entry.Path, false, false, nil, "")
		require.NoError(t, addErr, "resume re-index must not fail with UNIQUE constraint")
	}
	require.NoError(t, mediaDB.CommitTransaction())

	// Final check: exactly totalFiles Media rows — no duplicates from the partial run.
	var afterCount int64
	row2 := sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media")
	require.NoError(t, row2.Scan(&afterCount))
	assert.Equal(t, int64(totalFiles), afterCount,
		"after resume, Media count must equal totalFiles (no duplicates from partial run)")
}
