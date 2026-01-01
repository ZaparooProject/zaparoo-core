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
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner/testdata"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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

		err = mediaDB.BeginTransaction(false)
		require.NoError(t, err)

		// Add systems and media
		for _, systemID := range testSystems {
			entries := batch.Entries[systemID]
			for _, entry := range entries {
				titleIndex, mediaIndex, _ := AddMediaPath(mediaDB, scanState, systemID, entry.Path, false, false, nil)
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
		err = mediaDB.BeginTransaction(false)
		require.NoError(t, err)

		// Add one more system with games
		newEntry := testdata.NewTestDataGenerator(12345).GenerateMediaEntry("Gameboy")
		titleIndex, mediaIndex, _ := AddMediaPath(mediaDB, resumeState, "Gameboy", newEntry.Path, false, false, nil)

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

		err = mediaDB.BeginTransaction(false)
		require.NoError(t, err)

		// Add a few systems
		testEntry1 := testdata.NewTestDataGenerator(11111).GenerateMediaEntry("NES")
		testEntry2 := testdata.NewTestDataGenerator(22222).GenerateMediaEntry("SNES")

		_, _, _ = AddMediaPath(mediaDB, scanState, "NES", testEntry1.Path, false, false, nil)
		_, _, _ = AddMediaPath(mediaDB, scanState, "SNES", testEntry2.Path, false, false, nil)

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
		err = mediaDB.BeginTransaction(false)
		require.NoError(t, err)

		// Add more entries - these should not conflict
		testEntry3 := testdata.NewTestDataGenerator(33333).GenerateMediaEntry("Genesis")
		testEntry4 := testdata.NewTestDataGenerator(44444).GenerateMediaEntry("NES")

		// This used to cause "UNIQUE constraint failed: Systems.DBID" because
		// PopulateScanStateFromDB wasn't working and indexes started from 0 again
		titleIndex1, mediaIndex1, _ := AddMediaPath(mediaDB, resumeState, "Genesis", testEntry3.Path, false, false, nil)
		titleIndex2, mediaIndex2, _ := AddMediaPath(mediaDB, resumeState, "NES", testEntry4.Path, false, false, nil)

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

		err = mediaDB.BeginTransaction(false)
		require.NoError(t, err)

		// Add specific test data
		entry := testdata.NewTestDataGenerator(55555).GenerateMediaEntry("PSX")
		_, _, _ = AddMediaPath(mediaDB, scanState, "PSX", entry.Path, false, false, nil)

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

		err = mediaDB.BeginTransaction(false)
		require.NoError(t, err)

		// Add media for all systems
		for _, systemID := range testSystems {
			entries := batch.Entries[systemID]
			for _, entry := range entries {
				_, _, addErr := AddMediaPath(mediaDB, scanState, systemID, entry.Path, false, false, nil)
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
		err = mediaDB.BeginTransaction(false)
		require.NoError(t, err)

		amigaEntries := batch.Entries["Amiga"]
		for _, entry := range amigaEntries {
			_, _, addErr := AddMediaPath(mediaDB, reindexState, "Amiga", entry.Path, false, false, nil)
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
		if beginErr := mediaDB.BeginTransaction(false); beginErr != nil {
			return fmt.Errorf("failed to begin transaction: %w", beginErr)
		}

		amigaEntries := batch.Entries["Amiga"]
		for _, entry := range amigaEntries {
			if _, _, addErr := AddMediaPath(mediaDB, scanState, "Amiga", entry.Path, false, false, nil); addErr != nil {
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

// TestAutomaticNumberStrippingDetection tests the automatic detection of numbered playlists.
// This tests the threshold-based heuristic that analyzes directories to determine if leading
// numbers should be stripped (e.g., "01. ", "02 - ") based on how many files match the pattern.
func TestAutomaticNumberStrippingDetection(t *testing.T) {
	tests := []struct {
		name              string
		description       string
		files             []platforms.ScanResult
		expectedDetection bool
	}{
		{
			name: "numbered playlist - exceeds threshold",
			files: []platforms.ScanResult{
				{Path: "/roms/nes/favorites/01. Super Mario Bros.nes"},
				{Path: "/roms/nes/favorites/02. Zelda.nes"},
				{Path: "/roms/nes/favorites/03. Metroid.nes"},
				{Path: "/roms/nes/favorites/04. Mega Man.nes"},
				{Path: "/roms/nes/favorites/05. Castlevania.nes"},
			},
			expectedDetection: true,
			description:       "5/5 files match (100% > 50% threshold, ≥5 files)",
		},
		{
			name: "numbered playlist with dash separator",
			files: []platforms.ScanResult{
				{Path: "/roms/snes/01 - Super Mario World.snes"},
				{Path: "/roms/snes/02 - Zelda ALTTP.snes"},
				{Path: "/roms/snes/03 - Super Metroid.snes"},
				{Path: "/roms/snes/04 - Chrono Trigger.snes"},
				{Path: "/roms/snes/05 - Final Fantasy VI.snes"},
				{Path: "/roms/snes/06 - Earthbound.snes"},
			},
			expectedDetection: true,
			description:       "6/6 files match (100% > 50% threshold, ≥5 files)",
		},
		{
			name: "mixed - just over threshold",
			files: []platforms.ScanResult{
				{Path: "/roms/nes/01. Game One.nes"},
				{Path: "/roms/nes/02. Game Two.nes"},
				{Path: "/roms/nes/03. Game Three.nes"},
				{Path: "/roms/nes/1942.nes"},          // Legitimate game name
				{Path: "/roms/nes/Contra.nes"},        // Regular name
				{Path: "/roms/nes/04. Game Four.nes"}, // Added 4th numbered to tip over 50%
			},
			expectedDetection: true,
			description:       "4/6 files match (67% > 50% threshold, ≥5 files)",
		},
		{
			name: "mixed - below threshold",
			files: []platforms.ScanResult{
				{Path: "/roms/nes/01. Game One.nes"},
				{Path: "/roms/nes/02. Game Two.nes"},
				{Path: "/roms/nes/1942.nes"},
				{Path: "/roms/nes/Contra.nes"},
				{Path: "/roms/nes/Castlevania.nes"},
				{Path: "/roms/nes/Metroid.nes"},
			},
			expectedDetection: false,
			description:       "2/6 files match (33% < 50% threshold)",
		},
		{
			name: "no numbered files",
			files: []platforms.ScanResult{
				{Path: "/roms/nes/Super Mario Bros.nes"},
				{Path: "/roms/nes/Zelda.nes"},
				{Path: "/roms/nes/Metroid.nes"},
				{Path: "/roms/nes/Mega Man.nes"},
				{Path: "/roms/nes/Castlevania.nes"},
			},
			expectedDetection: false,
			description:       "0/5 files match (0% < 50% threshold)",
		},
		{
			name: "too few files - should not detect",
			files: []platforms.ScanResult{
				{Path: "/roms/nes/01. Game.nes"},
				{Path: "/roms/nes/02. Game.nes"},
				{Path: "/roms/nes/03. Game.nes"},
				{Path: "/roms/nes/04. Game.nes"},
			},
			expectedDetection: false,
			description:       "4/4 files match but <5 files (below minFiles threshold)",
		},
		{
			name: "legitimate number games",
			files: []platforms.ScanResult{
				{Path: "/roms/nes/1942.nes"},
				{Path: "/roms/nes/007.nes"},
				{Path: "/roms/nes/3D Worldrunner.nes"},
				{Path: "/roms/nes/720 Degrees.nes"},
				{Path: "/roms/nes/8 Eyes.nes"},
			},
			expectedDetection: true,
			description:       "5/5 files match because '007.nes' matches pattern (regex sees dot from extension)",
		},
		{
			name: "exactly at threshold boundary",
			files: []platforms.ScanResult{
				{Path: "/roms/nes/01. Game.nes"},
				{Path: "/roms/nes/02. Game.nes"},
				{Path: "/roms/nes/03. Game.nes"},
				{Path: "/roms/nes/Contra.nes"},
				{Path: "/roms/nes/Metroid.nes"},
				{Path: "/roms/nes/Castlevania.nes"},
			},
			expectedDetection: false,
			description:       "3/6 files match (50% = 50%, NOT > 50%, so returns false)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test with threshold=0.5, minFiles=5 (production values)
			result := detectNumberingPattern(tt.files, 0.5, 5)
			assert.Equal(t, tt.expectedDetection, result,
				"Detection failed: %s", tt.description)
		})
	}
}

// TestSlugGenerationPipeline tests the complete slug generation from file path to database.
// This integration test ensures that the context-aware leading number stripping works correctly
// throughout the entire indexing pipeline (file path → parsed title → slug → database storage).
func TestSlugGenerationPipeline(t *testing.T) {
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	tests := []struct {
		name                string
		systemID            string
		path                string
		expectedTitle       string
		expectedSlug        string
		stripLeadingNumbers bool
	}{
		// Numbered playlist scenarios (where leading number stripping is context-dependent)
		{
			name:                "numbered playlist - strips leading number with period",
			systemID:            "NES",
			path:                "/roms/nes/playlists/favorites/01. Super Mario Bros (USA).nes",
			stripLeadingNumbers: true,
			expectedTitle:       "Super Mario Bros",
			expectedSlug:        "supermariobrothers",
		},
		{
			name:                "numbered playlist - strips leading number with dash",
			systemID:            "NES",
			path:                "/roms/nes/playlists/best/42 - Zelda II (USA).nes",
			stripLeadingNumbers: true,
			expectedTitle:       "Zelda II",
			expectedSlug:        "zelda2",
		},
		{
			name:                "numbered playlist - strips leading number with space",
			systemID:            "SNES",
			path:                "/roms/snes/03 Super Metroid (USA).snes",
			stripLeadingNumbers: true,
			expectedTitle:       "Super Metroid",
			expectedSlug:        "supermetroid",
		},
		{
			name:                "numbered playlist - preserves when disabled",
			systemID:            "NES",
			path:                "/roms/nes/01. Super Mario Bros (USA).nes",
			stripLeadingNumbers: false,
			expectedTitle:       "01. Super Mario Bros",
			expectedSlug:        "01supermariobrothers",
		},

		// Games that naturally start with numbers (always preserved regardless of context)
		{
			name:                "game starting with number - 1942",
			systemID:            "NES",
			path:                "/roms/nes/1942 (USA).nes",
			stripLeadingNumbers: false, // Even with true, "1942" alone won't be stripped by the regex
			expectedTitle:       "1942",
			expectedSlug:        "1942",
		},
		{
			name:                "game starting with number - 3D Worldrunner",
			systemID:            "NES",
			path:                "/roms/nes/3D Worldrunner (USA).nes",
			stripLeadingNumbers: true,
			expectedTitle:       "3D Worldrunner",
			expectedSlug:        "3dworldrunner",
		},
		{
			name:                "game starting with number - 7th Saga",
			systemID:            "SNES",
			path:                "/roms/snes/7th Saga (USA).snes",
			stripLeadingNumbers: true,
			expectedTitle:       "7th Saga",
			expectedSlug:        "7saga",
		},

		// Leading article with numbered playlist
		{
			name:                "numbered playlist with leading article - strips number keeps article",
			systemID:            "NES",
			path:                "/roms/nes/playlists/01. The Legend of Zelda (USA).nes",
			stripLeadingNumbers: true,
			expectedTitle:       "The Legend of Zelda",
			expectedSlug:        "legendofzelda", // Slug still strips "The"
		},
		{
			name:                "numbered playlist with leading article - preserves number and article in title",
			systemID:            "NES",
			path:                "/roms/nes/playlists/02. The Legend of Zelda (USA).nes",
			stripLeadingNumbers: false,
			expectedTitle:       "02. The Legend of Zelda",
			expectedSlug:        "02thelegendofzelda", // Slug keeps number, strips "The"
		},

		// Complex filenames with metadata
		{
			name:                "metadata stripped but number stripped in playlist",
			systemID:            "NES",
			path:                "/roms/nes/01 - Super Mario Bros (USA) (Rev 1) [!].nes",
			stripLeadingNumbers: true,
			expectedTitle:       "Super Mario Bros",
			expectedSlug:        "supermariobrothers",
		},
		{
			name:                "edition suffix removed with slug generation",
			systemID:            "PS1",
			path:                "/roms/ps1/Final Fantasy VII Deluxe Edition (USA).bin",
			stripLeadingNumbers: false,
			expectedTitle:       "Final Fantasy VII Deluxe Edition",
			expectedSlug:        "finalfantasy7deluxe",
		},

		// Unicode and special characters
		{
			name:                "unicode with numbered playlist",
			systemID:            "NES",
			path:                "/roms/nes/01. Pokémon Red (USA).nes",
			stripLeadingNumbers: true,
			expectedTitle:       "Pokémon Red",
			expectedSlug:        "pokemonred",
		},
		{
			name:                "ampersand preserved in title",
			systemID:            "Genesis",
			path:                "/roms/genesis/Sonic & Knuckles (USA).md",
			stripLeadingNumbers: false,
			expectedTitle:       "Sonic & Knuckles",
			expectedSlug:        "sonicandknuckles",
		},

		// Roman numerals
		{
			name:                "roman numeral conversion",
			systemID:            "PS1",
			path:                "/roms/ps1/Final Fantasy VII (USA) (Disc 1).bin",
			stripLeadingNumbers: false,
			expectedTitle:       "Final Fantasy VII",
			expectedSlug:        "finalfantasy7",
		},

		// Trailing article format - The filename tag parser normalizes "Title, The" to "The Title"
		// This happens during tag extraction, before slug generation
		{
			name:                "trailing article format - normalized by tag parser",
			systemID:            "NES",
			path:                "/roms/nes/Legend of Zelda, The (USA).nes",
			stripLeadingNumbers: false,
			expectedTitle:       "The Legend of Zelda", // Normalized by filename tag parser
			expectedSlug:        "legendofzelda",       // Slug removes "The"
		},

		// Subtitle handling - filename tag parser normalizes subtitle delimiters
		{
			name:                "subtitle with colon",
			systemID:            "NES",
			path:                "/roms/nes/Zelda: Link's Awakening (USA).nes",
			stripLeadingNumbers: false,
			expectedTitle:       "Zelda: Link's Awakening", // Title preserves colon
			expectedSlug:        "zeldalinksawakening",     // Slug removes separator
		},
		{
			name:                "subtitle with dash - normalized to colon by tag parser",
			systemID:            "NES",
			path:                "/roms/nes/Zelda - Link's Awakening (USA).nes",
			stripLeadingNumbers: false,
			expectedTitle:       "Zelda: Link's Awakening", // Dash normalized to colon by tag parser
			expectedSlug:        "zeldalinksawakening",     // Slug removes separator
		},

		// Edge cases
		{
			name:                "multiple digit prefix stripped",
			systemID:            "NES",
			path:                "/roms/nes/123. Game Title (USA).nes",
			stripLeadingNumbers: true,
			expectedTitle:       "Game Title",
			expectedSlug:        "gametitle",
		},
		{
			name:                "zero-padded number stripped",
			systemID:            "SNES",
			path:                "/roms/snes/003 - Chrono Trigger (USA).snes",
			stripLeadingNumbers: true,
			expectedTitle:       "Chrono Trigger",
			expectedSlug:        "chronotrigger",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh scan state for each test
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

			// Populate from database if there's existing data
			err := PopulateScanStateFromDB(ctx, mediaDB, scanState)
			require.NoError(t, err)

			// Seed canonical tags if needed
			if scanState.TagTypesIndex == 0 {
				err = SeedCanonicalTags(mediaDB, scanState)
				require.NoError(t, err)
			}

			err = mediaDB.BeginTransaction(false)
			require.NoError(t, err)

			titleIndex, mediaIndex, addErr := AddMediaPath(
				mediaDB, scanState, tt.systemID, tt.path,
				false, tt.stripLeadingNumbers, nil,
			)

			err = mediaDB.CommitTransaction()
			require.NoError(t, err)

			// Verify the media was added successfully
			require.NoError(t, addErr, "AddMediaPath should not error")
			require.NotZero(t, titleIndex, "titleIndex should not be 0")
			require.NotZero(t, mediaIndex, "mediaIndex should not be 0")

			// Verify the title was created with correct name and slug
			title, err := mediaDB.FindMediaTitle(&database.MediaTitle{DBID: int64(titleIndex)})
			require.NoError(t, err, "Should be able to retrieve title from database")
			assert.Equal(t, tt.expectedTitle, title.Name, "Title name mismatch")
			assert.Equal(t, tt.expectedSlug, title.Slug, "Slug mismatch")

			// Verify the media points to the correct title
			media, err := mediaDB.FindMedia(database.Media{DBID: int64(mediaIndex)})
			require.NoError(t, err, "Should be able to retrieve media from database")
			assert.Equal(t, int64(titleIndex), media.MediaTitleDBID, "Media should point to correct title")
			assert.Equal(t, tt.path, media.Path, "Media path should match")
		})
	}
}
