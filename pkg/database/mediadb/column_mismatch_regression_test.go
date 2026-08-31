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
	"context"
	"database/sql"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	rows, err := mediaDB.UnsafeGetSQLDb().QueryContext(context.Background(),
		"SELECT DBID, Slug, Name, SecondarySlug FROM MediaTitles")
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	var allTitles []database.MediaTitle
	for rows.Next() {
		var title database.MediaTitle
		require.NoError(t, rows.Scan(&title.DBID, &title.Slug, &title.Name, &title.SecondarySlug))
		allTitles = append(allTitles, title)
	}
	require.NoError(t, rows.Err())
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
