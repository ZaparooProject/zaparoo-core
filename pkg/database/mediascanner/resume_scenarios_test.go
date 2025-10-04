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

// TestEndToEndResumeScenarios tests resume functionality using an actual SQLite database
// This test was created because the original resume functionality was completely broken
// but tests were passing because they only verified mock behavior, not actual functionality.
func TestEndToEndResumeScenarios(t *testing.T) {
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
		err = SeedCanonicalTags(mediaDB, scanState)
		require.NoError(t, err)

		err := mediaDB.BeginTransaction()
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
