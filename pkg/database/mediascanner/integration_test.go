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
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner/testdata"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResumeWithRealDatabase tests the resume functionality using an actual SQLite database
// This test was created because the original resume functionality was completely broken
// but tests were passing because they only verified mock behavior, not actual functionality.
func TestResumeWithRealDatabase(t *testing.T) {
	ctx := context.Background()

	// Create in-memory database with shared cache for transaction visibility
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	// Generate test data - small batch for focused testing
	testSystems := []string{"NES", "SNES", "Genesis"}
	batch := testdata.CreateReproducibleBatch(testSystems, 5) // 5 games per system = 15 total

	t.Run("Fresh Index Creates Correct Data", func(t *testing.T) {
		// Test fresh indexing first
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

		// Seed known tags BEFORE transaction
		err := SeedCanonicalTags(mediaDB, scanState)
		require.NoError(t, err)

		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		// Add systems and media
		for _, systemID := range testSystems {
			entries := batch.Entries[systemID]
			for _, entry := range entries {
				titleIndex, mediaIndex, _ := AddMediaPath(mediaDB, scanState, systemID, entry.Path)
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

	t.Run("PopulateScanStateFromDB Works Correctly", func(t *testing.T) {
		// Create fresh scan state
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

		// This is the critical function that was broken
		err := PopulateScanStateFromDB(ctx, mediaDB, resumeState)
		require.NoError(t, err)

		// Verify scan state was populated correctly from database
		assert.Equal(t, len(testSystems), resumeState.SystemsIndex, "SystemsIndex should match database")
		assert.Equal(t, batch.Total, resumeState.TitlesIndex, "TitlesIndex should match database")
		assert.Equal(t, batch.Total, resumeState.MediaIndex, "MediaIndex should match database")
		assert.Positive(t, resumeState.TagTypesIndex, "TagTypesIndex should be populated")
		assert.Positive(t, resumeState.TagsIndex, "TagsIndex should be populated")

		// Verify the indexes match what's actually in the database
		maxSystemID, _ := mediaDB.GetMaxSystemID()
		maxTitleID, _ := mediaDB.GetMaxTitleID()
		maxMediaID, _ := mediaDB.GetMaxMediaID()
		maxTagTypeID, _ := mediaDB.GetMaxTagTypeID()
		maxTagID, _ := mediaDB.GetMaxTagID()

		assert.Equal(t, int(maxSystemID), resumeState.SystemsIndex)
		assert.Equal(t, int(maxTitleID), resumeState.TitlesIndex)
		assert.Equal(t, int(maxMediaID), resumeState.MediaIndex)
		assert.Equal(t, int(maxTagTypeID), resumeState.TagTypesIndex)
		assert.Equal(t, int(maxTagID), resumeState.TagsIndex)
	})

	t.Run("Resume Continues From Correct IDs", func(t *testing.T) {
		// Create scan state populated from database
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

		err := PopulateScanStateFromDB(ctx, mediaDB, resumeState)
		require.NoError(t, err)

		// Record the state after population
		originalSystemsIndex := resumeState.SystemsIndex
		originalTitlesIndex := resumeState.TitlesIndex
		originalMediaIndex := resumeState.MediaIndex

		// Now add more data (simulating resume)
		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		// Add one more system with games
		newEntry := testdata.NewTestDataGenerator(12345).GenerateMediaEntry("Gameboy")
		titleIndex, mediaIndex, _ := AddMediaPath(mediaDB, resumeState, "Gameboy", newEntry.Path)

		// Verify the new IDs are sequential from where we left off
		assert.Equal(t, originalTitlesIndex+1, titleIndex, "New title should get next available ID")
		assert.Equal(t, originalMediaIndex+1, mediaIndex, "New media should get next available ID")

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify database has the new data with correct IDs
		maxSystemID, _ := mediaDB.GetMaxSystemID()
		maxTitleID, _ := mediaDB.GetMaxTitleID()
		maxMediaID, _ := mediaDB.GetMaxMediaID()

		assert.Equal(t, int64(originalSystemsIndex+1), maxSystemID, "Should have one more system")
		assert.Equal(t, int64(originalTitlesIndex+1), maxTitleID, "Should have one more title")
		assert.Equal(t, int64(originalMediaIndex+1), maxMediaID, "Should have one more media")
	})
}

// TestUniqueConstraintHandling tests that UNIQUE constraint violations are handled correctly
// This specifically tests the scenario that was causing "UNIQUE constraint failed: Systems.DBID"
func TestUniqueConstraintHandling(t *testing.T) {
	ctx := context.Background()

	// Create in-memory database with shared cache for transaction visibility
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	t.Run("No Constraint Violations With Proper Resume", func(t *testing.T) {
		// First indexing run
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

		// Seed known tags BEFORE transaction
		err := SeedCanonicalTags(mediaDB, scanState)
		require.NoError(t, err)

		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		// Add a few systems
		testEntry1 := testdata.NewTestDataGenerator(11111).GenerateMediaEntry("NES")
		testEntry2 := testdata.NewTestDataGenerator(22222).GenerateMediaEntry("SNES")

		_, _, _ = AddMediaPath(mediaDB, scanState, "NES", testEntry1.Path)
		_, _, _ = AddMediaPath(mediaDB, scanState, "SNES", testEntry2.Path)

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Simulate resume - this is where the bug occurred
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

		// This call was broken and would lead to constraint violations
		err = PopulateScanStateFromDB(ctx, mediaDB, resumeState)
		require.NoError(t, err)

		// Now continue indexing - this should not cause constraint violations
		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		// Add more entries - these should not conflict
		testEntry3 := testdata.NewTestDataGenerator(33333).GenerateMediaEntry("Genesis")
		testEntry4 := testdata.NewTestDataGenerator(44444).GenerateMediaEntry("NES")

		// This used to cause "UNIQUE constraint failed: Systems.DBID" because
		// PopulateScanStateFromDB wasn't working and indexes started from 0 again
		titleIndex1, mediaIndex1, _ := AddMediaPath(mediaDB, resumeState, "Genesis", testEntry3.Path)
		titleIndex2, mediaIndex2, _ := AddMediaPath(mediaDB, resumeState, "NES", testEntry4.Path)

		// Verify no constraint violations and IDs are sequential
		assert.Greater(t, titleIndex1, 2, "New title should have ID > 2")
		assert.Greater(t, mediaIndex1, 2, "New media should have ID > 2")
		assert.Greater(t, titleIndex2, titleIndex1, "Second title should have higher ID")
		assert.Greater(t, mediaIndex2, mediaIndex1, "Second media should have higher ID")

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify final state is correct
		maxSystemID, _ := mediaDB.GetMaxSystemID()
		maxTitleID, _ := mediaDB.GetMaxTitleID()
		maxMediaID, _ := mediaDB.GetMaxMediaID()

		assert.Equal(t, int64(3), maxSystemID, "Should have 3 systems (NES, SNES, Genesis)")
		assert.Equal(t, int64(4), maxTitleID, "Should have 4 titles")
		assert.Equal(t, int64(4), maxMediaID, "Should have 4 media entries")
	})
}

// TestDatabaseStateConsistency verifies database state remains consistent during operations
func TestDatabaseStateConsistency(t *testing.T) {
	ctx := context.Background()

	// Create in-memory database with shared cache for transaction visibility
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	t.Run("GetMax Methods Return Consistent Results", func(t *testing.T) {
		// Test empty database
		maxSystemID, err := mediaDB.GetMaxSystemID()
		require.NoError(t, err)
		assert.Equal(t, int64(0), maxSystemID, "Empty DB should return 0")

		maxTitleID, err := mediaDB.GetMaxTitleID()
		require.NoError(t, err)
		assert.Equal(t, int64(0), maxTitleID, "Empty DB should return 0")

		// Add some data
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

		err = SeedCanonicalTags(mediaDB, scanState)
		require.NoError(t, err)

		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		// Add specific test data
		entry := testdata.NewTestDataGenerator(55555).GenerateMediaEntry("PSX")
		_, _, _ = AddMediaPath(mediaDB, scanState, "PSX", entry.Path)

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify GetMax methods return expected values
		maxSystemID, err = mediaDB.GetMaxSystemID()
		require.NoError(t, err)
		assert.Equal(t, int64(1), maxSystemID, "Should have 1 system")

		maxTitleID, err = mediaDB.GetMaxTitleID()
		require.NoError(t, err)
		assert.Equal(t, int64(1), maxTitleID, "Should have 1 title")

		// Test PopulateScanStateFromDB returns same values
		testState := &database.ScanState{
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

		err = PopulateScanStateFromDB(ctx, mediaDB, testState)
		require.NoError(t, err)

		assert.Equal(t, int(maxSystemID), testState.SystemsIndex, "PopulateScanStateFromDB should match GetMaxSystemID")
		assert.Equal(t, int(maxTitleID), testState.TitlesIndex, "PopulateScanStateFromDB should match GetMaxTitleID")
	})
}

// TestSelectiveIndexingPreservesTagTypes tests that selective reindexing of one system
// does not delete TagTypes that are used by other systems.
// This regression test catches the bug where sqlTruncateSystems() was incorrectly
// deleting global TagTypes during cleanup, causing crashes when trying to add media.
func TestSelectiveIndexingPreservesTagTypes(t *testing.T) {
	ctx := context.Background()
	// Create in-memory database with shared cache for transaction visibility
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	// Generate test data for multiple systems
	testSystems := []string{"NES", "SNES", "Amiga"}
	batch := testdata.CreateReproducibleBatch(testSystems, 3) // 3 games per system

	t.Run("Full Index Creates TagTypes", func(t *testing.T) {
		// Index all systems
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

		// Seed known tags
		err := SeedCanonicalTags(mediaDB, scanState)
		require.NoError(t, err)

		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		// Add media for all systems
		for _, systemID := range testSystems {
			entries := batch.Entries[systemID]
			for _, entry := range entries {
				_, _, addErr := AddMediaPath(mediaDB, scanState, systemID, entry.Path)
				require.NoError(t, addErr, "Should add media without error")
			}
		}

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify TagTypes were created
		initialTagTypeCount, err := mediaDB.GetMaxTagTypeID()
		require.NoError(t, err)
		assert.Positive(t, initialTagTypeCount, "Should have TagTypes after full index")
	})

	t.Run("Selective Reindex Preserves TagTypes", func(t *testing.T) {
		// Get initial TagType count
		initialTagTypeCount, err := mediaDB.GetMaxTagTypeID()
		require.NoError(t, err)

		// Reindex only Amiga system using TruncateSystems
		err = mediaDB.TruncateSystems([]string{"Amiga"})
		require.NoError(t, err)

		// Verify TagTypes were NOT deleted
		afterTruncateTagTypeCount, err := mediaDB.GetMaxTagTypeID()
		require.NoError(t, err)
		assert.Equal(t, initialTagTypeCount, afterTruncateTagTypeCount,
			"TagTypes should be preserved during selective truncation")

		// Re-populate scan state for selective indexing
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

		err = PopulateScanStateForSelectiveIndexing(ctx, mediaDB, reindexState, []string{"Amiga"})
		require.NoError(t, err)

		// Re-add Amiga media - this should NOT crash with "Extension TagType not found"
		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		amigaEntries := batch.Entries["Amiga"]
		for _, entry := range amigaEntries {
			_, _, addErr := AddMediaPath(mediaDB, reindexState, "Amiga", entry.Path)
			require.NoError(t, addErr, "Should add Amiga media without TagType errors")
		}

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify TagTypes still intact after reindexing
		finalTagTypeCount, err := mediaDB.GetMaxTagTypeID()
		require.NoError(t, err)
		assert.Equal(t, initialTagTypeCount, finalTagTypeCount,
			"TagTypes should remain unchanged after selective reindex")

		// Verify other systems' media is still intact
		allSystems, err := mediaDB.GetAllSystems()
		require.NoError(t, err)
		assert.Len(t, allSystems, 3, "Should still have all 3 systems")
	})
}

// TestReindexSameSystemTwice tests that reindexing the same system multiple times
// works correctly without crashes or data corruption.
func TestReindexSameSystemTwice(t *testing.T) {
	ctx := context.Background()

	// Create in-memory database with shared cache for transaction visibility
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	// Generate test data
	testSystems := []string{"Amiga"}
	batch := testdata.CreateReproducibleBatch(testSystems, 5) // 5 games

	// Helper function to index Amiga
	indexAmiga := func() error {
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

		// Check if we need to seed tags
		maxTagTypeID, _ := mediaDB.GetMaxTagTypeID()
		if maxTagTypeID == 0 {
			if seedErr := SeedCanonicalTags(mediaDB, scanState); seedErr != nil {
				return seedErr
			}
		} else {
			// Populate state for existing data
			popErr := PopulateScanStateForSelectiveIndexing(ctx, mediaDB, scanState, []string{"Amiga"})
			if popErr != nil {
				return popErr
			}
		}

		// Begin transaction for media insertion
		if beginErr := mediaDB.BeginTransaction(); beginErr != nil {
			return fmt.Errorf("failed to begin transaction: %w", beginErr)
		}

		amigaEntries := batch.Entries["Amiga"]
		for _, entry := range amigaEntries {
			if _, _, addErr := AddMediaPath(mediaDB, scanState, "Amiga", entry.Path); addErr != nil {
				return addErr
			}
		}

		return mediaDB.CommitTransaction()
	}

	t.Run("First Index", func(t *testing.T) {
		err := indexAmiga()
		require.NoError(t, err)

		// Verify data
		maxMediaID, err := mediaDB.GetMaxMediaID()
		require.NoError(t, err)
		assert.Equal(t, int64(5), maxMediaID, "Should have 5 media entries")
	})

	t.Run("Second Index (Reindex)", func(t *testing.T) {
		// Truncate Amiga system
		err := mediaDB.TruncateSystems([]string{"Amiga"})
		require.NoError(t, err)

		// Reindex
		err = indexAmiga()
		require.NoError(t, err)

		// Verify data exists (IDs continue incrementing after truncation)
		allSystems, err := mediaDB.GetAllSystems()
		require.NoError(t, err)
		assert.Len(t, allSystems, 1, "Should have 1 system (Amiga)")

		// Verify TagTypes weren't duplicated or corrupted
		allTagTypes, err := mediaDB.GetAllTagTypes()
		require.NoError(t, err)
		assert.NotEmpty(t, allTagTypes, "Should have TagTypes")

		// Check for duplicate TagTypes by type
		tagTypeNames := make(map[string]int)
		for _, tt := range allTagTypes {
			tagTypeNames[tt.Type]++
		}
		for typeName, count := range tagTypeNames {
			assert.Equal(t, 1, count, "TagType %s should not be duplicated", typeName)
		}
	})

	t.Run("Third Index (Another Reindex)", func(t *testing.T) {
		// Truncate Amiga system again
		err := mediaDB.TruncateSystems([]string{"Amiga"})
		require.NoError(t, err)

		// Reindex again
		err = indexAmiga()
		require.NoError(t, err)

		// Verify still correct
		allSystems, err := mediaDB.GetAllSystems()
		require.NoError(t, err)
		assert.Len(t, allSystems, 1, "Should have 1 system (Amiga) after third index")
	})
}
