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
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNullableFields_SecondarySlug is a regression test for the bug where
// SecondarySlug was defined as string instead of sql.NullString, causing
// all database scans to fail when encountering NULL values.
//
// This test ensures we can INSERT and SELECT titles with NULL SecondarySlug,
// which is the default state for 100% of titles that don't have a secondary
// title (e.g., games without colons/dashes in their names).
func TestNullableFields_SecondarySlug(t *testing.T) {
	t.Parallel()

	// Create temp directory for test database
	tempDir, err := os.MkdirTemp("", "zaparoo-test-nullable-*")
	require.NoError(t, err)
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	// Create mock platform
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: tempDir,
	})

	ctx := context.Background()
	db, err := OpenMediaDB(ctx, mockPlatform)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	// Seed a test system
	system := database.System{Name: "Test System", SystemID: "test"}
	insertedSystem, err := db.InsertSystem(system)
	require.NoError(t, err)

	// Test 1: Insert title with NULL SecondarySlug (most common case)
	titleWithNull := database.MediaTitle{
		SystemDBID:    insertedSystem.DBID,
		Slug:          "testgame",
		Name:          "Test Game",
		SlugLength:    8,
		SlugWordCount: 2,
		SecondarySlug: sql.NullString{Valid: false}, // NULL
	}

	inserted, err := db.InsertMediaTitle(&titleWithNull)
	require.NoError(t, err, "should be able to insert title with NULL SecondarySlug")
	assert.Positive(t, inserted.DBID)

	// Test 2: Insert title with non-NULL SecondarySlug
	titleWithValue := database.MediaTitle{
		SystemDBID:    insertedSystem.DBID,
		Slug:          "gamewithsubtitle",
		Name:          "Game: The Subtitle",
		SlugLength:    17,
		SlugWordCount: 3,
		SecondarySlug: sql.NullString{String: "thesubtitle", Valid: true}, // NOT NULL
	}

	inserted2, err := db.InsertMediaTitle(&titleWithValue)
	require.NoError(t, err, "should be able to insert title with non-NULL SecondarySlug")
	assert.Positive(t, inserted2.DBID)

	// Test 3: Query with pre-filter (this was the failing path)
	// Pre-filter queries ALL titles and scans SecondarySlug
	candidates, err := db.GetTitlesWithPreFilter(ctx, "test", 0, 20, 1, 5)
	require.NoError(t, err, "pre-filter should handle NULL SecondarySlug gracefully")
	assert.Len(t, candidates, 2, "should return both titles")

	// Verify NULL vs non-NULL values were preserved correctly
	var nullTitle, nonNullTitle database.MediaTitle
	for _, title := range candidates {
		switch title.Slug {
		case "testgame":
			nullTitle = title
		case "gamewithsubtitle":
			nonNullTitle = title
		}
	}

	assert.False(t, nullTitle.SecondarySlug.Valid, "NULL SecondarySlug should have Valid=false")
	assert.Empty(t, nullTitle.SecondarySlug.String, "NULL SecondarySlug String should be empty")

	assert.True(t, nonNullTitle.SecondarySlug.Valid, "non-NULL SecondarySlug should have Valid=true")
	assert.Equal(t, "thesubtitle", nonNullTitle.SecondarySlug.String)

	// Test 4: GetAllMediaTitles (another path that scans SecondarySlug)
	allTitles, err := db.GetAllMediaTitles()
	require.NoError(t, err, "GetAllMediaTitles should handle NULL SecondarySlug")
	assert.GreaterOrEqual(t, len(allTitles), 2)

	// Test 5: FindMediaTitle by DBID (another scan path)
	found, err := db.FindMediaTitle(&database.MediaTitle{DBID: inserted.DBID})
	require.NoError(t, err, "FindMediaTitle should handle NULL SecondarySlug")
	assert.False(t, found.SecondarySlug.Valid)

	// Test 6: FindMediaTitle by Slug (another scan path)
	found2, err := db.FindMediaTitle(&database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       "gamewithsubtitle",
	})
	require.NoError(t, err, "FindMediaTitle by slug should handle non-NULL SecondarySlug")
	assert.True(t, found2.SecondarySlug.Valid)
	assert.Equal(t, "thesubtitle", found2.SecondarySlug.String)
}

// TestNullableFields_RoundTrip ensures that NULL values survive INSERT -> SELECT cycles
func TestNullableFields_RoundTrip(t *testing.T) {
	t.Parallel()

	// Create temp directory for test database
	tempDir, err := os.MkdirTemp("", "zaparoo-test-roundtrip-*")
	require.NoError(t, err)
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	// Create mock platform
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: tempDir,
	})

	ctx := context.Background()
	db, err := OpenMediaDB(ctx, mockPlatform)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	// Seed system
	system := database.System{Name: "Round Trip System", SystemID: "roundtrip"}
	insertedSystem, err := db.InsertSystem(system)
	require.NoError(t, err)

	// Insert with NULL
	original := database.MediaTitle{
		SystemDBID:    insertedSystem.DBID,
		Slug:          "original",
		Name:          "Original",
		SlugLength:    8,
		SlugWordCount: 1,
		SecondarySlug: sql.NullString{Valid: false},
	}

	inserted, err := db.InsertMediaTitle(&original)
	require.NoError(t, err)

	// Read back
	retrieved, err := db.FindMediaTitle(&database.MediaTitle{DBID: inserted.DBID})
	require.NoError(t, err)

	// Verify NULL survived the round trip
	assert.False(t, retrieved.SecondarySlug.Valid, "NULL should survive INSERT -> SELECT")
	assert.Empty(t, retrieved.SecondarySlug.String)
	assert.Equal(t, original.Slug, retrieved.Slug)
	assert.Equal(t, original.Name, retrieved.Name)
}
