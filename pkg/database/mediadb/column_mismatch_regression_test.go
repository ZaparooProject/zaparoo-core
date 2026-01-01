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

package mediadb

import (
	"database/sql"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetAllMedia_SystemDBID_Regression verifies that GetAllMedia correctly populates
// the SystemDBID field for all Media records.
//
// This is a regression test for a bug where sqlGetAllMedia was missing the SystemDBID
// column in its SELECT statement, causing all returned Media structs to have SystemDBID = 0.
//
// The bug was:
//
//	SELECT DBID, Path, MediaTitleDBID FROM Media  -- Missing SystemDBID
//
// Should be:
//
//	SELECT DBID, MediaTitleDBID, SystemDBID, Path FROM Media  -- All 4 columns
func TestGetAllMedia_SystemDBID_Regression(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Create test data with specific SystemDBID values
	err := mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	// Create two different systems to verify SystemDBID is populated correctly
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	// Use distinct values for SystemID and Name to detect column order bugs
	system1 := database.System{
		SystemID: nesSystem.ID,
		Name:     "Nintendo Entertainment System", // Different from ID to catch column swaps
	}
	insertedSystem1, err := mediaDB.InsertSystem(system1)
	require.NoError(t, err)
	require.NotZero(t, insertedSystem1.DBID, "System1 DBID should be set")

	system2 := database.System{
		SystemID: snesSystem.ID,
		Name:     "Super Nintendo Entertainment System", // Different from ID to catch column swaps
	}
	insertedSystem2, err := mediaDB.InsertSystem(system2)
	require.NoError(t, err)
	require.NotZero(t, insertedSystem2.DBID, "System2 DBID should be set")

	// Insert titles for each system
	title1 := &database.MediaTitle{
		SystemDBID: insertedSystem1.DBID,
		Slug:       "super-mario-bros",
		Name:       "Super Mario Bros",
	}
	insertedTitle1, err := mediaDB.InsertMediaTitle(title1)
	require.NoError(t, err)

	title2 := &database.MediaTitle{
		SystemDBID: insertedSystem2.DBID,
		Slug:       "super-metroid",
		Name:       "Super Metroid",
	}
	insertedTitle2, err := mediaDB.InsertMediaTitle(title2)
	require.NoError(t, err)

	// Insert media items with different SystemDBID values
	media1 := database.Media{
		MediaTitleDBID: insertedTitle1.DBID,
		SystemDBID:     insertedSystem1.DBID, // NES
		Path:           "/roms/nes/mario.nes",
	}
	_, err = mediaDB.InsertMedia(media1)
	require.NoError(t, err)

	media2 := database.Media{
		MediaTitleDBID: insertedTitle2.DBID,
		SystemDBID:     insertedSystem2.DBID, // SNES
		Path:           "/roms/snes/metroid.sfc",
	}
	_, err = mediaDB.InsertMedia(media2)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// THE CRITICAL REGRESSION CHECK:
	// Call GetAllMedia and verify SystemDBID is populated correctly
	// Before the fix, this query was:
	//   SELECT DBID, Path, MediaTitleDBID FROM Media
	// Which caused all SystemDBID values to be 0 (missing from SELECT)
	allMedia, err := mediaDB.GetAllMedia()
	require.NoError(t, err)
	require.Len(t, allMedia, 2, "should retrieve 2 media items")

	// Build map for verification
	mediaByPath := make(map[string]database.Media)
	for _, m := range allMedia {
		mediaByPath[m.Path] = m
	}

	// Verify first media item - check against EXPECTED values, not echoed insertedMedia1
	retrieved1, found1 := mediaByPath[media1.Path]
	require.True(t, found1, "should find first media item by path")
	assert.NotZero(t, retrieved1.DBID, "DBID should be populated")
	assert.Equal(t, insertedTitle1.DBID, retrieved1.MediaTitleDBID,
		"MediaTitleDBID should match title1")
	assert.Equal(t, media1.Path, retrieved1.Path, "Path should match")
	// THE KEY REGRESSION CHECK: SystemDBID must be from database, not zero
	assert.Equal(t, insertedSystem1.DBID, retrieved1.SystemDBID,
		"SystemDBID should be System1 DBID (NES) - BUG: this was 0 when column was missing from SELECT!")
	assert.NotZero(t, retrieved1.SystemDBID,
		"SystemDBID must not be zero - this proves the column was in the SELECT statement")

	// Verify second media item
	retrieved2, found2 := mediaByPath[media2.Path]
	require.True(t, found2, "should find second media item by path")
	assert.NotZero(t, retrieved2.DBID, "DBID should be populated")
	assert.Equal(t, insertedTitle2.DBID, retrieved2.MediaTitleDBID,
		"MediaTitleDBID should match title2")
	assert.Equal(t, media2.Path, retrieved2.Path, "Path should match")
	// THE KEY REGRESSION CHECK: SystemDBID must be from database, not zero
	assert.Equal(t, insertedSystem2.DBID, retrieved2.SystemDBID,
		"SystemDBID should be System2 DBID (SNES) - BUG: this was 0 when column was missing from SELECT!")
	assert.NotZero(t, retrieved2.SystemDBID,
		"SystemDBID must not be zero - this proves the column was in the SELECT statement")

	// Extra verification: Different systems have different SystemDBID values
	assert.NotEqual(t, retrieved1.SystemDBID, retrieved2.SystemDBID,
		"Media from different systems must have different SystemDBID values")
	assert.NotEqual(t, insertedSystem1.DBID, insertedSystem2.DBID,
		"Sanity check: the two systems have different DBIDs")
}

// TestBatchInsert_MediaTitle_SecondarySlug_Regression verifies that batch insertion
// of MediaTitles correctly preserves the SecondarySlug field.
//
// This is a regression test for a bug where the batch inserter for MediaTitles
// was missing the SecondarySlug column, causing all SecondarySlug values to be NULL
// even when explicitly set during batch indexing.
//
// The bug was in mediadb.go BeginTransaction where batch inserter was created with:
//
//	[]string{"DBID", "SystemDBID", "Slug", "Name", "SlugLength", "SlugWordCount"}
//
// Should be:
//
//	[]string{"DBID", "SystemDBID", "Slug", "Name", "SlugLength", "SlugWordCount", "SecondarySlug"}
func TestBatchInsert_MediaTitle_SecondarySlug_Regression(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Enable batch mode to trigger the batch inserter code path
	err := mediaDB.BeginTransaction(true) // true = batch mode
	require.NoError(t, err, "should begin batch transaction")

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	// Use distinct values for SystemID and Name to detect column order bugs
	system := database.System{
		DBID:     1, // Set explicitly for batch mode
		SystemID: nesSystem.ID,
		Name:     "Nintendo Entertainment System",
	}
	_, err = mediaDB.InsertSystem(system)
	require.NoError(t, err)

	// Insert MediaTitles with SecondarySlug values via batch inserter
	testCases := []struct {
		slug          string
		name          string
		secondarySlug sql.NullString
	}{
		{
			slug:          "zelda",
			name:          "The Legend of Zelda",
			secondarySlug: sql.NullString{String: "legend-of-zelda", Valid: true},
		},
		{
			slug:          "metroid",
			name:          "Metroid",
			secondarySlug: sql.NullString{String: "metroid-nes", Valid: true},
		},
		{
			slug:          "mario",
			name:          "Super Mario Bros",
			secondarySlug: sql.NullString{Valid: false}, // NULL SecondarySlug
		},
	}

	insertedTitles := make([]database.MediaTitle, 0, len(testCases))
	for i, tc := range testCases {
		title := &database.MediaTitle{
			DBID:          int64(i + 1), // Set explicit DBID for batch mode
			SystemDBID:    system.DBID,
			Slug:          tc.slug,
			Name:          tc.name,
			SecondarySlug: tc.secondarySlug,
			SlugLength:    len(tc.slug),
			SlugWordCount: 1,
		}

		// This goes through batch inserter when in batch mode
		inserted, insertErr := mediaDB.InsertMediaTitle(title)
		require.NoError(t, insertErr)
		insertedTitles = append(insertedTitles, inserted)
	}

	err = mediaDB.CommitTransaction()
	require.NoError(t, err, "should commit batch transaction")

	// Retrieve all titles and verify SecondarySlug was preserved
	allTitles, err := mediaDB.GetAllMediaTitles()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(allTitles), len(testCases),
		"should retrieve at least the inserted titles")

	// Build map for easy lookup
	titlesBySlug := make(map[string]database.MediaTitle)
	for _, title := range allTitles {
		titlesBySlug[title.Slug] = title
	}

	// REGRESSION CHECK: Verify SecondarySlug values were preserved during batch insert
	for i, tc := range testCases {
		retrieved, found := titlesBySlug[tc.slug]
		require.True(t, found, "should find title with slug: %s", tc.slug)

		assert.Equal(t, insertedTitles[i].DBID, retrieved.DBID, "DBID should match")
		assert.Equal(t, tc.slug, retrieved.Slug, "Slug should match")
		assert.Equal(t, tc.name, retrieved.Name, "Name should match")

		if tc.secondarySlug.Valid {
			assert.True(t, retrieved.SecondarySlug.Valid,
				"SecondarySlug should be valid for %s - batch inserter must include this column!", tc.slug)
			assert.Equal(t, tc.secondarySlug.String, retrieved.SecondarySlug.String,
				"SecondarySlug value should match for %s - this was the bug!", tc.slug)
		} else {
			assert.False(t, retrieved.SecondarySlug.Valid,
				"SecondarySlug should be NULL for %s", tc.slug)
		}
	}

	// Extra verification: Ensure the batch inserter is actually being used
	// by checking that at least one title has a valid SecondarySlug
	foundValidSecondarySlug := false
	for _, title := range allTitles {
		if title.SecondarySlug.Valid {
			foundValidSecondarySlug = true
			break
		}
	}
	assert.True(t, foundValidSecondarySlug,
		"At least one title should have a valid SecondarySlug to confirm the test is working")
}

// TestMedia_ColumnOrder_Regression verifies that the column order in Media table
// queries matches the struct field order and schema definition.
//
// This test ensures consistency between:
// 1. Database schema (DBID, MediaTitleDBID, SystemDBID, Path)
// 2. INSERT statements
// 3. SELECT statements
// 4. Scan operations
func TestMedia_ColumnOrder_Regression(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	err := mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	// Use distinct values for SystemID and Name to detect column order bugs
	system := database.System{
		SystemID: nesSystem.ID,
		Name:     "Nintendo Entertainment System",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	title := &database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       "test-game",
		Name:       "Test Game",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(title)
	require.NoError(t, err)

	// Insert with all fields explicitly set
	expectedMedia := database.Media{
		DBID:           999, // Explicit DBID
		MediaTitleDBID: insertedTitle.DBID,
		SystemDBID:     insertedSystem.DBID,
		Path:           "/test/path.nes",
	}

	_, err = mediaDB.InsertMedia(expectedMedia)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Retrieve via GetAllMedia
	allMedia, err := mediaDB.GetAllMedia()
	require.NoError(t, err)
	require.NotEmpty(t, allMedia)

	// Find our inserted media
	var retrieved database.Media
	found := false
	for _, m := range allMedia {
		if m.Path == expectedMedia.Path {
			retrieved = m
			found = true
			break
		}
	}
	require.True(t, found, "should find inserted media")

	// Verify ALL fields match in correct order
	assert.NotZero(t, retrieved.DBID, "DBID should be populated")
	assert.Equal(t, expectedMedia.MediaTitleDBID, retrieved.MediaTitleDBID,
		"MediaTitleDBID should match - column order matters!")
	assert.Equal(t, expectedMedia.SystemDBID, retrieved.SystemDBID,
		"SystemDBID should match - this field was missing in the bug!")
	assert.Equal(t, expectedMedia.Path, retrieved.Path,
		"Path should match - column order matters!")
}
