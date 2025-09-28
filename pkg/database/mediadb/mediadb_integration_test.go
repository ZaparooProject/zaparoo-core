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

package mediadb

import (
	"context"
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTempMediaDB(t *testing.T) (db *MediaDB, cleanup func()) {
	// Create temp directory that the mock platform will use
	tempDir, err := os.MkdirTemp("", "zaparoo-test-mediadb-*")
	require.NoError(t, err)

	// Create a mock platform that returns our temp directory for Settings().DataDir
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: tempDir,
	})

	// Use OpenMediaDB with context and the mock platform
	ctx := context.Background()
	db, err = OpenMediaDB(ctx, mockPlatform)
	require.NoError(t, err)

	cleanup = func() {
		if db != nil {
			_ = db.Close()
		}
		_ = os.RemoveAll(tempDir)
	}

	return db, cleanup
}

func TestMediaDB_OpenClose_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Database should be functional - test with a simple operation
	// Try updating last generated (which should work if DB is open)
	err := mediaDB.UpdateLastGenerated()
	require.NoError(t, err)

	// Should be able to close cleanly
	err = mediaDB.Close()
	require.NoError(t, err)

	// After close, operations should fail with database closed error
	err = mediaDB.UpdateLastGenerated()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database is closed")
}

func TestMediaDB_BulkInsert_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Test system insertion
	system := database.System{
		SystemID: "test-system",
		Name:     "Test System",
	}

	insertedSystem, err := mediaDB.FindOrInsertSystem(system)
	require.NoError(t, err)
	assert.Positive(t, insertedSystem.DBID, "System should have assigned DBID")
	assert.Equal(t, system.SystemID, insertedSystem.SystemID)

	// Test media title insertion
	mediaTitle := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       "test-game",
		Name:       "Test Game",
	}

	insertedTitle, err := mediaDB.FindOrInsertMediaTitle(mediaTitle)
	require.NoError(t, err)
	assert.Positive(t, insertedTitle.DBID, "MediaTitle should have assigned DBID")
	assert.Equal(t, mediaTitle.Name, insertedTitle.Name)

	// Test media insertion
	media := database.Media{
		MediaTitleDBID: insertedTitle.DBID,
		Path:           "/games/test-game.rom",
	}

	insertedMedia, err := mediaDB.FindOrInsertMedia(media)
	require.NoError(t, err)
	assert.Positive(t, insertedMedia.DBID, "Media should have assigned DBID")
	assert.Equal(t, media.Path, insertedMedia.Path)

	// Verify data was actually inserted by checking IDs are populated
	assert.Positive(t, insertedSystem.DBID, "System should be inserted")
	assert.Positive(t, insertedTitle.DBID, "MediaTitle should be inserted")
	assert.Positive(t, insertedMedia.DBID, "Media should be inserted")

	// Verify the relationships are correct
	assert.Equal(t, insertedSystem.DBID, insertedTitle.SystemDBID, "MediaTitle should reference System")
	assert.Equal(t, insertedTitle.DBID, insertedMedia.MediaTitleDBID, "Media should reference MediaTitle")
}

func TestMediaDB_SystemTagsCache_Integration(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Test cache population with empty database - should succeed without error
	err := mediaDB.PopulateSystemTagsCache(ctx)
	require.NoError(t, err)

	// Test cached tag retrieval with non-existent system - should return empty results
	systemdefsSystems := []systemdefs.System{{ID: "nes"}}
	cachedTags, err := mediaDB.GetSystemTagsCached(ctx, systemdefsSystems)
	require.NoError(t, err)
	assert.Empty(t, cachedTags) // Should be empty for non-existent system

	// Test cache invalidation with non-existent system - should succeed
	err = mediaDB.InvalidateSystemTagsCache(ctx, systemdefsSystems)
	require.NoError(t, err)

	// Test fallback to optimized query when cache is empty
	tagsAfterInvalidation, err := mediaDB.GetSystemTagsCached(ctx, systemdefsSystems)
	require.NoError(t, err)
	assert.Empty(t, tagsAfterInvalidation) // Should still be empty

	// Test with empty systems list
	emptyTags, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{})
	require.Error(t, err) // Should return error for empty systems
	assert.Nil(t, emptyTags)
}
