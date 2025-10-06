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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
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

func TestMediaDB_TransactionCycle_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Test that transaction BEGIN/COMMIT cycle works correctly with PRAGMA optimizations
	// This is a regression test for the double-BEGIN issue
	err := mediaDB.BeginTransaction()
	require.NoError(t, err, "BeginTransaction should succeed")

	// Insert some test data within transaction
	system := database.System{
		DBID:     1,
		SystemID: "test-system",
		Name:     "Test System",
	}

	_, err = mediaDB.InsertSystem(system)
	require.NoError(t, err, "Insert within transaction should succeed")

	// Commit should work without errors
	err = mediaDB.CommitTransaction()
	require.NoError(t, err, "CommitTransaction should succeed")

	// Verify data was committed
	foundSystem, err := mediaDB.FindSystemBySystemID("test-system")
	require.NoError(t, err)
	assert.Equal(t, "test-system", foundSystem.SystemID)
}

func TestMediaDB_MultipleTransactionCycles_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Test multiple transaction cycles work correctly
	// This ensures PRAGMA restoration works properly
	for i := range 3 {
		err := mediaDB.BeginTransaction()
		require.NoError(t, err, "BeginTransaction cycle %d should succeed", i)

		systemDef := systemdefs.AllSystems()[i]
		system := database.System{
			DBID:     int64(i + 1),
			SystemID: systemDef.ID,
			Name:     systemDef.ID, // Use ID as name for test
		}

		_, err = mediaDB.InsertSystem(system)
		require.NoError(t, err, "Insert in cycle %d should succeed", i)

		err = mediaDB.CommitTransaction()
		require.NoError(t, err, "CommitTransaction cycle %d should succeed", i)
	}

	// Verify all systems were committed
	for i := range 3 {
		_, err := mediaDB.FindSystemBySystemID(systemdefs.AllSystems()[i].ID)
		require.NoError(t, err, "System %d should be findable after commit", i)
	}
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
		Slug:       helpers.SlugifyString("Test Game"),
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
	systemdefsSystems := []systemdefs.System{{ID: "NES"}}
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

func TestMediaDB_SearchMediaPathExact_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Insert test data with transaction for better performance
	err := mediaDB.BeginTransaction()
	require.NoError(t, err)

	// Create a test system
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	system := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	// Create test media titles and media
	testGames := []struct {
		name string
		path string
	}{
		{"Super Mario Bros", "/roms/nes/Super Mario Bros.nes"},
		{"Super Mario Bros 2", "/roms/nes/Super Mario Bros 2.nes"},
		{"Mega Man", "/roms/nes/Mega Man.nes"},
		{"Mega Man 2", "/roms/nes/Mega Man 2.nes"},
	}

	for _, game := range testGames {
		title := database.MediaTitle{
			SystemDBID: insertedSystem.DBID,
			Slug:       helpers.SlugifyString(game.name),
			Name:       game.name,
		}
		insertedTitle, titleErr := mediaDB.InsertMediaTitle(title)
		require.NoError(t, titleErr)

		media := database.Media{
			MediaTitleDBID: insertedTitle.DBID,
			Path:           game.path,
		}
		_, mediaErr := mediaDB.InsertMedia(media)
		require.NoError(t, mediaErr)
	}

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Test exact search - must match full path
	results, err := mediaDB.SearchMediaPathExact([]systemdefs.System{*nesSystem}, "/roms/nes/Super Mario Bros.nes")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Super Mario Bros", results[0].Name)

	// Test case-insensitive search - must match full path
	results, err = mediaDB.SearchMediaPathExact([]systemdefs.System{*nesSystem}, "/roms/nes/Mega Man.nes")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Mega Man", results[0].Name)

	// Test no match
	results, err = mediaDB.SearchMediaPathExact([]systemdefs.System{*nesSystem}, "/roms/nes/Zelda.nes")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestMediaDB_SearchMediaPathWords_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Insert test data
	err := mediaDB.BeginTransaction()
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	system := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	testGames := []struct {
		name string
		path string
	}{
		{"Super Mario Bros", "/roms/nes/Super Mario Bros.nes"},
		{"Super Mario Bros 2", "/roms/nes/Super Mario Bros 2.nes"},
		{"Mario is Missing", "/roms/nes/Mario is Missing.nes"},
	}

	for _, game := range testGames {
		title := database.MediaTitle{
			SystemDBID: insertedSystem.DBID,
			Slug:       helpers.SlugifyString(game.name),
			Name:       game.name,
		}
		insertedTitle, titleErr := mediaDB.InsertMediaTitle(title)
		require.NoError(t, titleErr)

		media := database.Media{
			MediaTitleDBID: insertedTitle.DBID,
			Path:           game.path,
		}
		_, mediaErr := mediaDB.InsertMedia(media)
		require.NoError(t, mediaErr)
	}

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Test word search - should find all games with both "mario" and "bros"
	results, err := mediaDB.SearchMediaPathWords([]systemdefs.System{*nesSystem}, "mario bros")
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Test word search with cursor
	var cursor *int64
	resultsWithCursor, err := mediaDB.SearchMediaPathWordsWithCursor(
		ctx, []systemdefs.System{*nesSystem}, "mario", cursor, 2,
	)
	require.NoError(t, err)
	assert.Len(t, resultsWithCursor, 2)
	assert.Positive(t, resultsWithCursor[0].MediaID)
}

func TestMediaDB_RandomGame_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Insert test data
	err := mediaDB.BeginTransaction()
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	system := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	// Insert several games
	for i := 1; i <= 10; i++ {
		title := database.MediaTitle{
			SystemDBID: insertedSystem.DBID,
			Slug:       helpers.SlugifyString("Test Game " + string(rune('0'+i))),
			Name:       "Test Game " + string(rune('0'+i)),
		}
		insertedTitle, titleErr := mediaDB.InsertMediaTitle(title)
		require.NoError(t, titleErr)

		media := database.Media{
			MediaTitleDBID: insertedTitle.DBID,
			Path:           "/roms/nes/game" + string(rune('0'+i)) + ".nes",
		}
		_, mediaErr := mediaDB.InsertMedia(media)
		require.NoError(t, mediaErr)
	}

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Test random game
	result, err := mediaDB.RandomGame([]systemdefs.System{*nesSystem})
	require.NoError(t, err)
	assert.Equal(t, nesSystem.ID, result.SystemID)
	assert.NotEmpty(t, result.Path)

	// Test RandomGameWithQuery
	query := database.MediaQuery{
		Systems: []string{nesSystem.ID},
	}
	result, err = mediaDB.RandomGameWithQuery(&query)
	require.NoError(t, err)
	assert.Equal(t, nesSystem.ID, result.SystemID)

	// Test that cache is working - second call should use cache
	result2, err := mediaDB.RandomGameWithQuery(&query)
	require.NoError(t, err)
	assert.Equal(t, nesSystem.ID, result2.SystemID)
}

func TestMediaDB_CacheInvalidation_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Insert test data
	err := mediaDB.BeginTransaction()
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	system := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	title := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       helpers.SlugifyString("Test Game"),
		Name:       "Test Game",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(title)
	require.NoError(t, err)

	media := database.Media{
		MediaTitleDBID: insertedTitle.DBID,
		Path:           "/roms/nes/test.nes",
	}
	_, err = mediaDB.InsertMedia(media)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Get initial count
	count1, err := mediaDB.GetTotalMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 1, count1)

	// Create a query and get stats
	query := database.MediaQuery{
		Systems: []string{nesSystem.ID},
	}
	_, err = mediaDB.RandomGameWithQuery(&query)
	require.NoError(t, err)

	// Verify cache exists
	stats, found := mediaDB.GetCachedStats(ctx, &query)
	assert.True(t, found)
	assert.Equal(t, 1, stats.Count)

	// Invalidate cache
	err = mediaDB.InvalidateCountCache()
	require.NoError(t, err)

	// Verify cache is cleared
	_, found = mediaDB.GetCachedStats(ctx, &query)
	assert.False(t, found)
}

func TestMediaDB_TruncateSystems_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Insert test data for multiple systems
	err := mediaDB.BeginTransaction()
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	// Insert NES system and game
	nesSystemDB := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedNES, err := mediaDB.InsertSystem(nesSystemDB)
	require.NoError(t, err)

	nesTitle := database.MediaTitle{
		SystemDBID: insertedNES.DBID,
		Slug:       helpers.SlugifyPath("/roms/nes/game.nes"), // Must match the path's filename slug
		Name:       "NES Game",
	}
	insertedNESTitle, err := mediaDB.InsertMediaTitle(nesTitle)
	require.NoError(t, err)

	nesMedia := database.Media{
		MediaTitleDBID: insertedNESTitle.DBID,
		Path:           "/roms/nes/game.nes",
	}
	_, err = mediaDB.InsertMedia(nesMedia)
	require.NoError(t, err)

	// Insert SNES system and game
	snesSystemDB := database.System{
		SystemID: snesSystem.ID,
		Name:     "SNES",
	}
	insertedSNES, err := mediaDB.InsertSystem(snesSystemDB)
	require.NoError(t, err)

	snesTitle := database.MediaTitle{
		SystemDBID: insertedSNES.DBID,
		Slug:       helpers.SlugifyPath("/roms/snes/game.sfc"), // Must match the path's filename slug
		Name:       "SNES Game",
	}
	insertedSNESTitle, err := mediaDB.InsertMediaTitle(snesTitle)
	require.NoError(t, err)

	snesMedia := database.Media{
		MediaTitleDBID: insertedSNESTitle.DBID,
		Path:           "/roms/snes/game.sfc",
	}
	_, err = mediaDB.InsertMedia(snesMedia)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Verify both systems exist
	count, err := mediaDB.GetTotalMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Truncate only NES system
	err = mediaDB.TruncateSystems([]string{nesSystem.ID})
	require.NoError(t, err)

	// Verify NES is gone but SNES remains
	count, err = mediaDB.GetTotalMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Debug: Check what systems are left
	systems, err := mediaDB.IndexedSystems()
	require.NoError(t, err)
	t.Logf("Indexed systems after truncate: %v", systems)

	results, err := mediaDB.SearchMediaPathExact([]systemdefs.System{*snesSystem}, "/roms/snes/game.sfc")
	require.NoError(t, err)
	t.Logf("Search results: %+v", results)
	assert.Len(t, results, 1)

	results, err = mediaDB.SearchMediaPathExact([]systemdefs.System{*nesSystem}, "/roms/nes/game.nes")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestMediaDB_TagsWorkflow_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	// Create tag type BEFORE transaction (TagType doesn't support transactions properly)
	tagType := database.TagType{
		Type: "Genre",
	}
	insertedTagType, err := mediaDB.FindOrInsertTagType(tagType)
	require.NoError(t, err)

	// Now start transaction for other inserts
	err = mediaDB.BeginTransaction()
	require.NoError(t, err)

	system := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	// Create tags
	actionTag := database.Tag{
		TypeDBID: insertedTagType.DBID,
		Tag:      "Action",
	}
	insertedActionTag, err := mediaDB.FindOrInsertTag(actionTag)
	require.NoError(t, err)

	platformerTag := database.Tag{
		TypeDBID: insertedTagType.DBID,
		Tag:      "Platformer",
	}
	insertedPlatformerTag, err := mediaDB.FindOrInsertTag(platformerTag)
	require.NoError(t, err)

	// Create media with tags
	title := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       helpers.SlugifyString("Super Mario Bros"),
		Name:       "Super Mario Bros",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(title)
	require.NoError(t, err)

	media := database.Media{
		MediaTitleDBID: insertedTitle.DBID,
		Path:           "/roms/nes/mario.nes",
	}
	insertedMedia, err := mediaDB.InsertMedia(media)
	require.NoError(t, err)

	// Associate tags with media
	mediaTag1 := database.MediaTag{
		MediaDBID: insertedMedia.DBID,
		TagDBID:   insertedActionTag.DBID,
	}
	_, err = mediaDB.InsertMediaTag(mediaTag1)
	require.NoError(t, err)

	mediaTag2 := database.MediaTag{
		MediaDBID: insertedMedia.DBID,
		TagDBID:   insertedPlatformerTag.DBID,
	}
	_, err = mediaDB.InsertMediaTag(mediaTag2)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Test GetTags
	tags, err := mediaDB.GetTags(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)
	assert.Len(t, tags, 2)

	// Test GetAllUsedTags
	allTags, err := mediaDB.GetAllUsedTags(ctx)
	require.NoError(t, err)
	assert.Len(t, allTags, 2)

	// Test PopulateSystemTagsCache
	err = mediaDB.PopulateSystemTagsCache(ctx)
	require.NoError(t, err)

	// Test GetSystemTagsCached
	cachedTags, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)
	assert.Len(t, cachedTags, 2)

	// Test SearchMediaWithFilters
	filters := &database.SearchFilters{
		Systems: []systemdefs.System{*nesSystem},
		Query:   "mario",
		Tags:    []database.TagFilter{{Type: "Genre", Value: "Action"}},
		Limit:   10,
	}
	results, err := mediaDB.SearchMediaWithFilters(ctx, filters)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "mario", results[0].Name) // Name comes from filename in path
}

func TestMediaDB_RollbackTransaction_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Begin transaction
	err := mediaDB.BeginTransaction()
	require.NoError(t, err)

	// Insert test data
	system := database.System{
		SystemID: "test-system",
		Name:     "Test System",
	}
	_, err = mediaDB.InsertSystem(system)
	require.NoError(t, err)

	// Rollback transaction
	err = mediaDB.RollbackTransaction()
	require.NoError(t, err)

	// Verify data was not committed
	_, err = mediaDB.FindSystemBySystemID("test-system")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no rows")
}

func TestMediaDB_ConcurrentReads_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Insert test data
	err := mediaDB.BeginTransaction()
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	system := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	for i := 1; i <= 100; i++ {
		title := database.MediaTitle{
			SystemDBID: insertedSystem.DBID,
			Slug:       helpers.SlugifyString("Test Game " + string(rune('0'+i))),
			Name:       "Test Game " + string(rune('0'+i)),
		}
		insertedTitle, titleErr := mediaDB.InsertMediaTitle(title)
		require.NoError(t, titleErr)

		media := database.Media{
			MediaTitleDBID: insertedTitle.DBID,
			Path:           "/roms/nes/game" + string(rune('0'+i)) + ".nes",
		}
		_, mediaErr := mediaDB.InsertMedia(media)
		require.NoError(t, mediaErr)
	}

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Perform concurrent reads
	const numReaders = 10
	errChan := make(chan error, numReaders)

	for range numReaders {
		go func() {
			result, err := mediaDB.RandomGame([]systemdefs.System{*nesSystem})
			if err != nil {
				errChan <- err
				return
			}
			if result.SystemID != nesSystem.ID {
				errChan <- assert.AnError
				return
			}
			errChan <- nil
		}()
	}

	// Wait for all readers
	for range numReaders {
		err := <-errChan
		require.NoError(t, err)
	}
}
