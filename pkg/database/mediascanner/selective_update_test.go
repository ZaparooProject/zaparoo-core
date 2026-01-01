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
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSelectiveUpdate_EmptyCache verifies that PopulateScanStateForSelectiveIndexing
// uses empty maps for the optimization (not loading non-reindexed system data).
func TestSelectiveUpdate_EmptyCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	// Create test data for 3 systems
	testSystems := []string{"nes", "snes", "genesis"}
	batch := testdata.CreateReproducibleBatch(testSystems, 10) // 10 games per system

	// Initial full index of all systems
	initialState := &database.ScanState{
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

	// Seed canonical tags
	err := SeedCanonicalTags(db, initialState)
	require.NoError(t, err)

	// Begin transaction and add all systems
	err = db.BeginTransaction(false)
	require.NoError(t, err)

	for _, systemID := range testSystems {
		entries := batch.Entries[systemID]
		for _, entry := range entries {
			_, _, addErr := AddMediaPath(db, initialState, systemID, entry.Path, false, false, nil)
			require.NoError(t, addErr)
		}
	}

	err = db.CommitTransaction()
	require.NoError(t, err)

	// Selective update - truncate and reindex NES only
	err = db.TruncateSystems([]string{"nes"})
	require.NoError(t, err)

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

	// This is the key function being tested - it should pre-populate SystemIDs but keep other maps empty
	err = PopulateScanStateForSelectiveIndexing(ctx, db, selectiveState, []string{"nes"})
	require.NoError(t, err)

	// Verify TitleIDs and MediaIDs are empty (performance optimization - system-scoped keys)
	assert.Empty(t, selectiveState.TitleIDs, "TitleIDs cache should be empty for selective indexing")
	assert.Empty(t, selectiveState.MediaIDs, "MediaIDs cache should be empty for selective indexing")

	// Verify SystemIDs is populated with existing systems (NOT empty - prevents cache collisions)
	// After truncating NES, we should have SNES and Genesis still in the database
	assert.NotEmpty(t, selectiveState.SystemIDs, "SystemIDs cache should be populated to prevent cache key collisions")
	assert.Contains(t, selectiveState.SystemIDs, "snes", "SystemIDs should contain existing snes system")
	assert.Contains(t, selectiveState.SystemIDs, "genesis", "SystemIDs should contain existing genesis system")
	assert.NotContains(t, selectiveState.SystemIDs, "nes", "SystemIDs should not contain truncated nes system")

	// TagTypeIDs and TagIDs can be empty (global entities with UNIQUE constraints)
	assert.Empty(t, selectiveState.TagTypeIDs, "TagTypeIDs cache should be empty for selective indexing")
	assert.Empty(t, selectiveState.TagIDs, "TagIDs cache should be empty for selective indexing")

	// Verify max IDs were set correctly (DBID continuity)
	assert.Positive(t, selectiveState.TitlesIndex, "TitlesIndex should be set from max ID")
	assert.Positive(t, selectiveState.MediaIndex, "MediaIndex should be set from max ID")
	assert.Positive(t, selectiveState.SystemsIndex, "SystemsIndex should be set from max ID")
	assert.Positive(t, selectiveState.TagTypesIndex, "TagTypesIndex should be set from max ID")
	assert.Positive(t, selectiveState.TagsIndex, "TagsIndex should be set from max ID")

	// Re-add NES data to verify the optimization doesn't break indexing
	err = db.BeginTransaction(false)
	require.NoError(t, err)

	nesEntries := batch.Entries["nes"]
	for _, entry := range nesEntries {
		_, _, addErr := AddMediaPath(db, selectiveState, "nes", entry.Path, false, false, nil)
		require.NoError(t, addErr)
	}

	err = db.CommitTransaction()
	require.NoError(t, err)

	// CRITICAL: Check for duplicate MediaTitles (ZERO tolerance)
	// This is the only table without a uniqueness constraint that could have duplicates
	duplicateMediaTitles, err := db.CheckForDuplicateMediaTitles()
	require.NoError(t, err)
	assert.Empty(t, duplicateMediaTitles, "ZERO duplicate MediaTitles allowed (no DB constraint on SystemDBID+Slug)")
}

// TestSelectiveUpdate_MultipleSystemsEmpty verifies empty caches work with
// multiple systems being reindexed.
func TestSelectiveUpdate_MultipleSystemsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	// Create test data for 5 systems
	testSystems := []string{"nes", "snes", "genesis", "psx", "n64"}
	batch := testdata.CreateReproducibleBatch(testSystems, 5)

	// Initial full index
	initialState := &database.ScanState{
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

	err := SeedCanonicalTags(db, initialState)
	require.NoError(t, err)

	err = db.BeginTransaction(false)
	require.NoError(t, err)

	for _, systemID := range testSystems {
		entries := batch.Entries[systemID]
		for _, entry := range entries {
			_, _, addErr := AddMediaPath(db, initialState, systemID, entry.Path, false, false, nil)
			require.NoError(t, addErr)
		}
	}

	err = db.CommitTransaction()
	require.NoError(t, err)

	// Selective reindex of multiple systems (NES and SNES)
	systemsToReindex := []string{"nes", "snes"}
	err = db.TruncateSystems(systemsToReindex)
	require.NoError(t, err)

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

	err = PopulateScanStateForSelectiveIndexing(ctx, db, selectiveState, systemsToReindex)
	require.NoError(t, err)

	// Verify empty caches
	assert.Empty(t, selectiveState.TitleIDs, "TitleIDs cache should be empty")
	assert.Empty(t, selectiveState.MediaIDs, "MediaIDs cache should be empty")

	// Re-add both systems
	err = db.BeginTransaction(false)
	require.NoError(t, err)

	for _, systemID := range systemsToReindex {
		entries := batch.Entries[systemID]
		for _, entry := range entries {
			_, _, addErr := AddMediaPath(db, selectiveState, systemID, entry.Path, false, false, nil)
			require.NoError(t, addErr)
		}
	}

	err = db.CommitTransaction()
	require.NoError(t, err)

	// Check for duplicate MediaTitles (only table without uniqueness constraint)
	duplicateMediaTitles, err := db.CheckForDuplicateMediaTitles()
	require.NoError(t, err)
	assert.Empty(t, duplicateMediaTitles, "ZERO duplicate MediaTitles allowed")
}

// TestSelectiveUpdate_LargeDatabase tests the optimization with a larger database
// to demonstrate the performance benefit.
func TestSelectiveUpdate_LargeDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large database test in short mode")
	}

	t.Parallel()

	ctx := context.Background()
	db, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	// Create a larger database (30 systems, 50 games each = 1500 total)
	testSystems := make([]string, 30)
	for i := range 30 {
		testSystems[i] = fmt.Sprintf("system%d", i)
	}

	batch := testdata.CreateReproducibleBatch(testSystems, 50)

	// Initial full index
	initialState := &database.ScanState{
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

	err := SeedCanonicalTags(db, initialState)
	require.NoError(t, err)

	err = db.BeginTransaction(false)
	require.NoError(t, err)

	for _, systemID := range testSystems {
		entries := batch.Entries[systemID]
		for _, entry := range entries {
			_, _, addErr := AddMediaPath(db, initialState, systemID, entry.Path, false, false, nil)
			require.NoError(t, addErr)
		}
	}

	err = db.CommitTransaction()
	require.NoError(t, err)

	t.Log("Created database with 30 systems and 1500 total games")

	// Selective update of just one system
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

	// With the optimization, this should be very fast (<100ms)
	// The old implementation would load 1450 titles (all except system0)
	err = PopulateScanStateForSelectiveIndexing(ctx, db, selectiveState, []string{"system0"})
	require.NoError(t, err)

	// Verify optimization (empty caches)
	assert.Empty(t, selectiveState.TitleIDs, "TitleIDs should be empty (not loading 1450 titles)")
	assert.Empty(t, selectiveState.MediaIDs, "MediaIDs should be empty (not loading 1450 media)")

	// Check for duplicate MediaTitles (only table without uniqueness constraint)
	duplicateMediaTitles, err := db.CheckForDuplicateMediaTitles()
	require.NoError(t, err)
	assert.Empty(t, duplicateMediaTitles, "ZERO duplicate MediaTitles allowed")
}

// TestSelectiveUpdate_GlobalEntitiesLazyLoad tests that global entities (Systems, TagTypes, Tags)
// are properly lazy-loaded during selective updates with empty caches, preventing duplicates.
func TestSelectiveUpdate_GlobalEntitiesLazyLoad(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	// Create test data with shared tags across multiple systems
	testSystems := []string{"nes", "snes", "genesis"}
	batch := testdata.CreateReproducibleBatch(testSystems, 15)

	// Phase 1: Initial full index - this creates TagTypes and Tags
	initialState := &database.ScanState{
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

	err := SeedCanonicalTags(db, initialState)
	require.NoError(t, err)

	err = db.BeginTransaction(false)
	require.NoError(t, err)

	for _, systemID := range testSystems {
		entries := batch.Entries[systemID]
		for _, entry := range entries {
			_, _, addErr := AddMediaPath(db, initialState, systemID, entry.Path, false, false, nil)
			require.NoError(t, addErr)
		}
	}

	err = db.CommitTransaction()
	require.NoError(t, err)

	// Capture counts of global entities BEFORE selective update
	systemsBefore, err := db.GetAllSystems()
	require.NoError(t, err)
	systemCountBefore := len(systemsBefore)
	t.Logf("Systems before selective update: %d", systemCountBefore)

	tagTypesBefore, err := db.GetAllTagTypes()
	require.NoError(t, err)
	tagTypeCountBefore := len(tagTypesBefore)
	t.Logf("TagTypes before selective update: %d", tagTypeCountBefore)

	tagsBefore, err := db.GetAllTags()
	require.NoError(t, err)
	tagCountBefore := len(tagsBefore)
	t.Logf("Tags before selective update: %d", tagCountBefore)

	// Phase 2: Selective update with EMPTY cache - reindex NES only
	// Tags and TagTypes should NOT be deleted (they're global entities)
	err = db.TruncateSystems([]string{"nes"})
	require.NoError(t, err)

	selectiveState := &database.ScanState{
		SystemIDs:     make(map[string]int),
		TitleIDs:      make(map[string]int),
		MediaIDs:      make(map[string]int),
		TagTypeIDs:    make(map[string]int), // EMPTY - will lazy load
		TagIDs:        make(map[string]int), // EMPTY - will lazy load
		SystemsIndex:  0,
		TitlesIndex:   0,
		MediaIndex:    0,
		TagTypesIndex: 0,
		TagsIndex:     0,
	}

	err = PopulateScanStateForSelectiveIndexing(ctx, db, selectiveState, []string{"nes"})
	require.NoError(t, err)

	// Verify caches are empty
	assert.Empty(t, selectiveState.TagTypeIDs, "TagTypeIDs cache should be empty")
	assert.Empty(t, selectiveState.TagIDs, "TagIDs cache should be empty")

	// Re-index NES - this should lazy-load existing Tags/TagTypes, not create duplicates
	err = db.BeginTransaction(false)
	require.NoError(t, err)

	nesEntries := batch.Entries["nes"]
	for _, entry := range nesEntries {
		_, _, addErr := AddMediaPath(db, selectiveState, "nes", entry.Path, false, false, nil)
		require.NoError(t, addErr)
	}

	err = db.CommitTransaction()
	require.NoError(t, err)

	// Phase 3: Verify NO DUPLICATES were created for global entities
	systemsAfter, err := db.GetAllSystems()
	require.NoError(t, err)
	assert.Len(t, systemsAfter, systemCountBefore,
		"System count should be unchanged (lazy loading should reuse existing Systems)")

	tagTypesAfter, err := db.GetAllTagTypes()
	require.NoError(t, err)
	assert.Len(t, tagTypesAfter, tagTypeCountBefore,
		"TagType count should be unchanged (lazy loading should reuse existing TagTypes)")

	tagsAfter, err := db.GetAllTags()
	require.NoError(t, err)
	t.Logf("Tags after selective update: %d (orphaned tags cleaned up)", len(tagsAfter))

	// Tags WILL decrease because orphaned tags (only used by NES) are cleaned up during TruncateSystems
	// But the key test is: did we create NEW duplicates during lazy loading?
	// To verify this, check that tag count after reindex is <= before (no new duplicates)
	assert.LessOrEqual(t, len(tagsAfter), tagCountBefore,
		"Tag count should be same or less (orphans cleaned, no duplicates created)")

	// Verify Systems NOT being reindexed have identical DBIDs (not recreated)
	// NES was truncated and recreated, so skip checking it
	for _, sys := range systemsBefore {
		// Skip the system that was reindexed
		isReindexed := false
		for _, reindexSys := range []string{"nes"} {
			if sys.SystemID == reindexSys {
				isReindexed = true
				break
			}
		}
		if isReindexed {
			continue
		}

		// Verify preserved systems have same DBID
		found := false
		for _, sysAfter := range systemsAfter {
			if sys.DBID == sysAfter.DBID && sys.SystemID == sysAfter.SystemID {
				found = true
				break
			}
		}
		assert.True(t, found, "Preserved system %s (DBID=%d) should still exist with same DBID", sys.SystemID, sys.DBID)
	}

	for _, tt := range tagTypesBefore {
		found := false
		for _, ttAfter := range tagTypesAfter {
			if tt.DBID == ttAfter.DBID && tt.Type == ttAfter.Type {
				found = true
				break
			}
		}
		assert.True(t, found, "TagType %s (DBID=%d) should still exist with same DBID", tt.Type, tt.DBID)
	}

	// CRITICAL: Check for duplicate MediaTitles (only table without uniqueness constraint)
	duplicateMediaTitles, err := db.CheckForDuplicateMediaTitles()
	require.NoError(t, err)
	assert.Empty(t, duplicateMediaTitles, "ZERO duplicate MediaTitles allowed")

	t.Logf("✓ Verified lazy loading: Systems=%d (unchanged), TagTypes=%d (unchanged), Tags=%d (orphans cleaned)",
		len(systemsAfter), len(tagTypesAfter), len(tagsAfter))
}

// TestSelectiveUpdate_DuplicateTagPrevention specifically tests that duplicate tags
// are prevented through the FindOrInsert pattern during selective updates.
func TestSelectiveUpdate_DuplicateTagPrevention(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	// Phase 1: Create initial data with specific tags
	testSystems := []string{"nes", "snes"}
	batch := testdata.CreateReproducibleBatch(testSystems, 10)

	initialState := &database.ScanState{
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

	err := SeedCanonicalTags(db, initialState)
	require.NoError(t, err)

	err = db.BeginTransaction(false)
	require.NoError(t, err)

	for _, systemID := range testSystems {
		entries := batch.Entries[systemID]
		for _, entry := range entries {
			_, _, addErr := AddMediaPath(db, initialState, systemID, entry.Path, false, false, nil)
			require.NoError(t, addErr)
		}
	}

	err = db.CommitTransaction()
	require.NoError(t, err)

	// Get exact tag counts BEFORE selective update
	tagsBeforeUpdate, err := db.GetAllTags()
	require.NoError(t, err)
	t.Logf("Tags before selective update: %d", len(tagsBeforeUpdate))

	tagTypesBeforeUpdate, err := db.GetAllTagTypes()
	require.NoError(t, err)
	t.Logf("TagTypes before selective update: %d", len(tagTypesBeforeUpdate))

	// Phase 2: Selective reindex with EMPTY cache
	err = db.TruncateSystems([]string{"nes"})
	require.NoError(t, err)

	// After truncation, orphaned tags (only used by NES) will be cleaned up
	tagsAfterTruncate, err := db.GetAllTags()
	require.NoError(t, err)
	t.Logf("Tags after truncate (orphans cleaned): %d", len(tagsAfterTruncate))

	selectiveState := &database.ScanState{
		SystemIDs:     make(map[string]int),
		TitleIDs:      make(map[string]int),
		MediaIDs:      make(map[string]int),
		TagTypeIDs:    make(map[string]int), // EMPTY
		TagIDs:        make(map[string]int), // EMPTY - this is the key test
		SystemsIndex:  0,
		TitlesIndex:   0,
		MediaIndex:    0,
		TagTypesIndex: 0,
		TagsIndex:     0,
	}

	err = PopulateScanStateForSelectiveIndexing(ctx, db, selectiveState, []string{"nes"})
	require.NoError(t, err)

	// Re-index NES - will encounter tags via lazy loading (FindOrInsert pattern)
	err = db.BeginTransaction(false)
	require.NoError(t, err)

	nesEntries := batch.Entries["nes"]
	for _, entry := range nesEntries {
		_, _, addErr := AddMediaPath(db, selectiveState, "nes", entry.Path, false, false, nil)
		require.NoError(t, addErr)
	}

	err = db.CommitTransaction()
	require.NoError(t, err)

	// Phase 3: Verify NO NEW DUPLICATES were created during lazy loading
	tagsAfterReindex, err := db.GetAllTags()
	require.NoError(t, err)
	t.Logf("Tags after reindex: %d", len(tagsAfterReindex))

	tagTypesAfterReindex, err := db.GetAllTagTypes()
	require.NoError(t, err)

	// The critical assertion: Tag/TagType counts should not INCREASE
	// They may stay same or decrease (orphans cleaned), but should never increase
	// (which would indicate duplicates were created)
	assert.LessOrEqual(t, len(tagsAfterReindex), len(tagsBeforeUpdate),
		"Tag count should not increase - lazy loading via FindOrInsert prevents duplicates")

	assert.Len(t, tagTypesAfterReindex, len(tagTypesBeforeUpdate),
		"TagType count should be unchanged - lazy loading should reuse existing TagTypes")

	// CRITICAL: Check for duplicate MediaTitles (only table without uniqueness constraint)
	duplicateMediaTitles, err := db.CheckForDuplicateMediaTitles()
	require.NoError(t, err)
	assert.Empty(t, duplicateMediaTitles, "ZERO duplicate MediaTitles allowed")

	t.Logf("✓ Verified no duplicates: TagTypes=%d (unchanged), Tags %d→%d (orphans cleaned)",
		len(tagTypesAfterReindex), len(tagsBeforeUpdate), len(tagsAfterReindex))
}
