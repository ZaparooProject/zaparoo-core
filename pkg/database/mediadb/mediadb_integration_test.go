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
	"os"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
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
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
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
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Test that transaction BEGIN/COMMIT cycle works correctly with PRAGMA optimizations
	// This is a regression test for the double-BEGIN issue
	err := mediaDB.BeginTransaction(false)
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
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Test multiple transaction cycles work correctly
	// This ensures PRAGMA restoration works properly
	for i := range 3 {
		err := mediaDB.BeginTransaction(false)
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
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
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
		Slug:       slugs.Slugify(slugs.MediaTypeGame, "Test Game"),
		Name:       "Test Game",
	}

	insertedTitle, err := mediaDB.FindOrInsertMediaTitle(&mediaTitle)
	require.NoError(t, err)
	assert.Positive(t, insertedTitle.DBID, "MediaTitle should have assigned DBID")
	assert.Equal(t, mediaTitle.Name, insertedTitle.Name)

	// Test media insertion
	media := database.Media{
		SystemDBID:     insertedSystem.DBID,
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
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
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
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Insert test data with transaction for better performance
	err := mediaDB.BeginTransaction(false)
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
			Slug:       slugs.Slugify(slugs.MediaTypeGame, game.name),
			Name:       game.name,
		}
		insertedTitle, titleErr := mediaDB.InsertMediaTitle(&title)
		require.NoError(t, titleErr)

		media := database.Media{
			SystemDBID:     insertedSystem.DBID,
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

func TestMediaDB_RandomGame_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Insert test data
	err := mediaDB.BeginTransaction(false)
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
			Slug:       slugs.Slugify(slugs.MediaTypeGame, "Test Game "+string(rune('0'+i))),
			Name:       "Test Game " + string(rune('0'+i)),
		}
		insertedTitle, titleErr := mediaDB.InsertMediaTitle(&title)
		require.NoError(t, titleErr)

		media := database.Media{
			SystemDBID:     insertedSystem.DBID,
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
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Insert test data
	err := mediaDB.BeginTransaction(false)
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
		Slug:       slugs.Slugify(slugs.MediaTypeGame, "Test Game"),
		Name:       "Test Game",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(&title)
	require.NoError(t, err)

	media := database.Media{
		SystemDBID:     insertedSystem.DBID,
		MediaTitleDBID: insertedTitle.DBID,
		Path:           "/games/test-game.rom",
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
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Insert test data for multiple systems
	err := mediaDB.BeginTransaction(false)
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
		Slug:       slugs.Slugify(slugs.MediaTypeGame, helpers.FilenameFromPath("/roms/nes/game.nes")),
		Name:       "NES Game",
	}
	insertedNESTitle, err := mediaDB.InsertMediaTitle(&nesTitle)
	require.NoError(t, err)

	nesMedia := database.Media{
		SystemDBID:     insertedNES.DBID,
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
		Slug:       slugs.Slugify(slugs.MediaTypeGame, helpers.FilenameFromPath("/roms/snes/game.sfc")),
		Name:       "SNES Game",
	}
	insertedSNESTitle, err := mediaDB.InsertMediaTitle(&snesTitle)
	require.NoError(t, err)

	snesMedia := database.Media{
		SystemDBID:     insertedSNES.DBID,
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
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
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
	err = mediaDB.BeginTransaction(false)
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
		Slug:       slugs.Slugify(slugs.MediaTypeGame, "Super Mario Bros"),
		Name:       "Super Mario Bros",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(&title)
	require.NoError(t, err)

	media := database.Media{
		SystemDBID:     insertedSystem.DBID,
		MediaTitleDBID: insertedTitle.DBID,
		Path:           "/roms/nes/super-mario-bros.nes",
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
		Tags:    []zapscript.TagFilter{{Type: "Genre", Value: "Action"}},
		Limit:   10,
	}
	results, err := mediaDB.SearchMediaWithFilters(ctx, filters)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Super Mario Bros", results[0].Name) // Name comes from MediaTitles.Name
}

func TestMediaDB_RollbackTransaction_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Begin transaction
	err := mediaDB.BeginTransaction(false)
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
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Insert test data
	err := mediaDB.BeginTransaction(false)
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
			Slug:       slugs.Slugify(slugs.MediaTypeGame, "Test Game "+string(rune('0'+i))),
			Name:       "Test Game " + string(rune('0'+i)),
		}
		insertedTitle, titleErr := mediaDB.InsertMediaTitle(&title)
		require.NoError(t, titleErr)

		media := database.Media{
			SystemDBID:     insertedSystem.DBID,
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

func TestMediaDB_SearchMediaBySlug_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create tag types BEFORE transaction (TagType doesn't support transactions properly)
	regionTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "region"})
	require.NoError(t, err)

	genreTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "genre"})
	require.NoError(t, err)

	// Insert test data with transaction for better performance
	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	// Create test systems
	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	// Insert SNES system
	snesDBSystem := database.System{
		SystemID: snesSystem.ID,
		Name:     "SNES",
	}
	insertedSNESSystem, err := mediaDB.InsertSystem(snesDBSystem)
	require.NoError(t, err)

	// Insert NES system
	nesDBSystem := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedNESSystem, err := mediaDB.InsertSystem(nesDBSystem)
	require.NoError(t, err)

	// Create test media titles and media with various slug patterns
	testGames := []struct {
		systemID   string
		name       string
		path       string
		tags       []database.TagInfo
		systemDBID int64
	}{
		{
			systemID:   snesSystem.ID,
			systemDBID: insertedSNESSystem.DBID,
			name:       "Super Mario World",
			path:       "/roms/snes/Super Mario World.smc",
			tags:       []database.TagInfo{{Type: "region", Tag: "usa"}, {Type: "genre", Tag: "platform"}},
		},
		{
			systemID:   snesSystem.ID,
			systemDBID: insertedSNESSystem.DBID,
			name:       "Super Mario World 2: Yoshi's Island",
			path:       "/roms/snes/Super Mario World 2 - Yoshi's Island.smc",
			tags:       []database.TagInfo{{Type: "region", Tag: "usa"}, {Type: "genre", Tag: "platform"}},
		},
		{
			systemID:   snesSystem.ID,
			systemDBID: insertedSNESSystem.DBID,
			name:       "The Legend of Zelda: A Link to the Past",
			path:       "/roms/snes/Zelda - A Link to the Past.smc",
			tags:       []database.TagInfo{{Type: "region", Tag: "usa"}, {Type: "genre", Tag: "adventure"}},
		},
		{
			systemID:   nesSystem.ID,
			systemDBID: insertedNESSystem.DBID,
			name:       "Super Mario Bros",
			path:       "/roms/nes/Super Mario Bros.nes",
			tags:       []database.TagInfo{{Type: "region", Tag: "usa"}, {Type: "genre", Tag: "platform"}},
		},
		{
			systemID:   nesSystem.ID,
			systemDBID: insertedNESSystem.DBID,
			name:       "Super Mario Bros 2",
			path:       "/roms/nes/Super Mario Bros 2.nes",
			tags:       []database.TagInfo{{Type: "region", Tag: "japan"}, {Type: "genre", Tag: "platform"}},
		},
		{
			systemID:   nesSystem.ID,
			systemDBID: insertedNESSystem.DBID,
			name:       "Dr. Mario",
			path:       "/roms/nes/Dr. Mario.nes",
			tags:       []database.TagInfo{{Type: "region", Tag: "usa"}, {Type: "genre", Tag: "puzzle"}},
		},
		{
			systemID:   nesSystem.ID,
			systemDBID: insertedNESSystem.DBID,
			name:       "Ms. Pac-Man",
			path:       "/roms/nes/Ms. Pac-Man.nes",
			tags:       []database.TagInfo{{Type: "region", Tag: "usa"}, {Type: "genre", Tag: "maze"}},
		},
	}

	for _, game := range testGames {
		title := database.MediaTitle{
			SystemDBID: game.systemDBID,
			Slug:       slugs.Slugify(slugs.MediaTypeGame, game.name),
			Name:       game.name,
		}
		insertedTitle, titleErr := mediaDB.InsertMediaTitle(&title)
		require.NoError(t, titleErr)

		media := database.Media{
			SystemDBID:     game.systemDBID,
			MediaTitleDBID: insertedTitle.DBID,
			Path:           game.path,
		}
		insertedMedia, mediaErr := mediaDB.InsertMedia(media)
		require.NoError(t, mediaErr)

		// Add tags if specified using the proper tag insertion workflow
		for _, tag := range game.tags {
			var tagTypeDBID int64
			switch tag.Type {
			case "region":
				tagTypeDBID = regionTagType.DBID
			case "genre":
				tagTypeDBID = genreTagType.DBID
			}

			// Create the tag
			dbTag := database.Tag{
				TypeDBID: tagTypeDBID,
				Tag:      tag.Tag,
			}
			insertedTag, tagErr := mediaDB.FindOrInsertTag(dbTag)
			require.NoError(t, tagErr)

			// Associate tag with media
			mediaTag := database.MediaTag{
				MediaDBID: insertedMedia.DBID,
				TagDBID:   insertedTag.DBID,
			}
			_, tagErr = mediaDB.InsertMediaTag(mediaTag)
			require.NoError(t, tagErr)
		}
	}

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Test 1: Basic slug search - exact match
	results, err := mediaDB.SearchMediaBySlug(ctx, "SNES", "supermarioworld", nil)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Super Mario World", results[0].Name)
	assert.Equal(t, "SNES", results[0].SystemID)
	assert.Equal(t, "/roms/snes/Super Mario World.smc", results[0].Path)

	// Test 2: Slug search with exact match - only one exact match
	results, err = mediaDB.SearchMediaBySlug(ctx, "SNES", "supermarioworld", nil)
	require.NoError(t, err)
	assert.Len(t, results, 1) // Only Super Mario World (exact match)

	// Test 3: Slug search with tag filtering
	tags := []zapscript.TagFilter{{Type: "region", Value: "usa"}}
	results, err = mediaDB.SearchMediaBySlug(ctx, "SNES", "supermarioworld", tags)
	require.NoError(t, err)
	assert.Len(t, results, 1) // Only Super Mario World (exact match) and it's USA region

	// Test 4: Slug search with restrictive tag filtering
	tags = []zapscript.TagFilter{{Type: "region", Value: "japan"}}
	results, err = mediaDB.SearchMediaBySlug(ctx, "SNES", "supermarioworld", tags)
	require.NoError(t, err)
	assert.Empty(t, results) // No Japanese SNES Mario games

	// Test 5: Slug search with multiple tag filters (AND logic)
	tags = []zapscript.TagFilter{
		{Type: "region", Value: "usa"},
		{Type: "genre", Value: "platform"},
	}
	results, err = mediaDB.SearchMediaBySlug(ctx, "SNES", "supermarioworld", tags)
	require.NoError(t, err)
	assert.Len(t, results, 1) // Only Super Mario World matches USA AND platform

	// Test 6: Slug search across different systems
	results, err = mediaDB.SearchMediaBySlug(ctx, "NES", "supermariobrothers", nil)
	require.NoError(t, err)
	assert.Len(t, results, 1) // Only Super Mario Bros (exact match, not Super Mario Bros 2)

	// Test 7: Slug search with dots in name (Dr. Mario)
	results, err = mediaDB.SearchMediaBySlug(ctx, "NES", "Dr. Mario", nil)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Dr. Mario", results[0].Name)

	// Test 8: Slug search with dots and special characters (Ms. Pac-Man)
	results, err = mediaDB.SearchMediaBySlug(ctx, "NES", "Ms. Pac-Man", nil)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Ms. Pac-Man", results[0].Name)

	// Test 9: Slug search with complex title (Zelda)
	results, err = mediaDB.SearchMediaBySlug(ctx, "SNES", "The Legend of Zelda Link to the Past", nil)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "The Legend of Zelda: A Link to the Past", results[0].Name)

	// Test 10: No results found
	results, err = mediaDB.SearchMediaBySlug(ctx, "SNES", "nonexistentgame", nil)
	require.NoError(t, err)
	assert.Empty(t, results)

	// Test 11: Wrong system
	results, err = mediaDB.SearchMediaBySlug(ctx, "genesis", "supermarioworld", nil)
	require.NoError(t, err)
	assert.Empty(t, results)

	// Test 12: Verify tags are populated in results
	tags = []zapscript.TagFilter{{Type: "genre", Value: "puzzle"}}
	results, err = mediaDB.SearchMediaBySlug(ctx, "NES", "Doctor Mario", tags)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Len(t, results[0].Tags, 2) // region:usa and genre:puzzle
	assert.Contains(t, results[0].Tags, database.TagInfo{Type: "region", Tag: "usa"})
	assert.Contains(t, results[0].Tags, database.TagInfo{Type: "genre", Tag: "puzzle"})

	// Test 13: Lowercase slug matching (slugs are always normalized to lowercase)
	results, err = mediaDB.SearchMediaBySlug(ctx, "SNES", "supermarioworld", nil)
	require.NoError(t, err)
	assert.Len(t, results, 1) // Should find the result with lowercase slug

	// Test 14: Empty slug
	results, err = mediaDB.SearchMediaBySlug(ctx, "SNES", "", nil)
	require.NoError(t, err)
	assert.Empty(t, results)

	// Test 15: Empty system ID
	results, err = mediaDB.SearchMediaBySlug(ctx, "", "supermarioworld", nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestMediaDB_CacheInvalidation_OnInsert_Integration tests that caches are properly invalidated on inserts
func TestMediaDB_CacheInvalidation_OnInsert_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create tag type before transaction
	regionTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "region"})
	require.NoError(t, err)

	// Setup initial data
	err = mediaDB.BeginTransaction(false)
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
		Slug:       slugs.Slugify(slugs.MediaTypeGame, "Mario"),
		Name:       "Super Mario Bros",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(&title)
	require.NoError(t, err)

	media := database.Media{
		SystemDBID:     insertedSystem.DBID,
		MediaTitleDBID: insertedTitle.DBID,
		Path:           "/roms/mario.nes",
	}
	insertedMedia, err := mediaDB.InsertMedia(media)
	require.NoError(t, err)

	usaTag := database.Tag{
		TypeDBID: regionTagType.DBID,
		Tag:      "usa",
	}
	insertedTag, err := mediaDB.FindOrInsertTag(usaTag)
	require.NoError(t, err)

	// Associate tag with media
	mediaTag := database.MediaTag{
		MediaDBID: insertedMedia.DBID,
		TagDBID:   insertedTag.DBID,
	}
	_, err = mediaDB.InsertMediaTag(mediaTag)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Populate all caches
	err = mediaDB.PopulateSystemTagsCache(ctx)
	require.NoError(t, err)

	err = mediaDB.SetCachedSlugResolution(ctx, nesSystem.ID, "mario", nil, insertedMedia.DBID, "exact")
	require.NoError(t, err)

	// Verify caches are populated
	_, _, slugCacheFound := mediaDB.GetCachedSlugResolution(ctx, nesSystem.ID, "mario", nil)
	assert.True(t, slugCacheFound, "slug cache should be populated")

	tags, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)
	assert.NotEmpty(t, tags, "system tags cache should be populated")

	// Insert a new media title (outside transaction to trigger invalidation)
	newTitle := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       slugs.Slugify(slugs.MediaTypeGame, "Zelda"),
		Name:       "The Legend of Zelda",
	}
	_, err = mediaDB.InsertMediaTitle(&newTitle)
	require.NoError(t, err)

	// Verify slug cache was invalidated (InsertMediaTitle uses AllSystems scope)
	_, _, slugCacheAfter := mediaDB.GetCachedSlugResolution(ctx, nesSystem.ID, "mario", nil)
	assert.False(t, slugCacheAfter, "slug cache should be invalidated after InsertMediaTitle")
}

// TestMediaDB_CacheInvalidation_OnTransaction_Integration tests cache invalidation during transactions
func TestMediaDB_CacheInvalidation_OnTransaction_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Setup initial data
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	// Insert initial data in transaction
	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	system := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	title := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       slugs.Slugify(slugs.MediaTypeGame, "Mario"),
		Name:       "Super Mario Bros",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(&title)
	require.NoError(t, err)

	media := database.Media{
		SystemDBID:     insertedSystem.DBID,
		MediaTitleDBID: insertedTitle.DBID,
		Path:           "/roms/mario.nes",
	}
	insertedMedia, err := mediaDB.InsertMedia(media)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Cache a slug resolution
	err = mediaDB.SetCachedSlugResolution(ctx, nesSystem.ID, "mario", nil, insertedMedia.DBID, "exact")
	require.NoError(t, err)

	// Verify cache exists
	_, _, found := mediaDB.GetCachedSlugResolution(ctx, nesSystem.ID, "mario", nil)
	assert.True(t, found, "cache should exist before transaction")

	// Start new transaction and insert more data
	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	// Insert should NOT invalidate cache during transaction
	newTitle := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       slugs.Slugify(slugs.MediaTypeGame, "Zelda"),
		Name:       "The Legend of Zelda",
	}
	_, err = mediaDB.InsertMediaTitle(&newTitle)
	require.NoError(t, err)

	// Cache should still exist during transaction
	_, _, foundDuring := mediaDB.GetCachedSlugResolution(ctx, nesSystem.ID, "mario", nil)
	assert.True(t, foundDuring, "cache should NOT be invalidated during transaction")

	// Commit transaction
	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// After commit, caches are invalidated
	_, _, foundAfter := mediaDB.GetCachedSlugResolution(ctx, nesSystem.ID, "mario", nil)
	assert.False(t, foundAfter, "cache should be invalidated after transaction commit")
}

// TestMediaDB_TruncateSystems_SlugCacheInvalidation_Integration tests slug cache invalidation on system truncate
func TestMediaDB_TruncateSystems_SlugCacheInvalidation_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Setup multiple systems
	err := mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	nesSystemDB := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedNES, err := mediaDB.InsertSystem(nesSystemDB)
	require.NoError(t, err)

	snesSystemDB := database.System{
		SystemID: snesSystem.ID,
		Name:     "SNES",
	}
	insertedSNES, err := mediaDB.InsertSystem(snesSystemDB)
	require.NoError(t, err)

	// Add media for both systems
	nesTitle := database.MediaTitle{
		SystemDBID: insertedNES.DBID,
		Slug:       slugs.Slugify(slugs.MediaTypeGame, "Mario"),
		Name:       "Super Mario Bros",
	}
	insertedNESTitle, err := mediaDB.InsertMediaTitle(&nesTitle)
	require.NoError(t, err)

	nesMedia := database.Media{
		SystemDBID:     insertedNES.DBID,
		MediaTitleDBID: insertedNESTitle.DBID,
		Path:           "/roms/nes/mario.nes",
	}
	insertedNESMedia, err := mediaDB.InsertMedia(nesMedia)
	require.NoError(t, err)

	snesTitle := database.MediaTitle{
		SystemDBID: insertedSNES.DBID,
		Slug:       slugs.Slugify(slugs.MediaTypeGame, "Zelda"),
		Name:       "The Legend of Zelda",
	}
	insertedSNESTitle, err := mediaDB.InsertMediaTitle(&snesTitle)
	require.NoError(t, err)

	snesMedia := database.Media{
		SystemDBID:     insertedSNES.DBID,
		MediaTitleDBID: insertedSNESTitle.DBID,
		Path:           "/roms/snes/zelda.smc",
	}
	insertedSNESMedia, err := mediaDB.InsertMedia(snesMedia)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Cache slug resolutions for both systems
	err = mediaDB.SetCachedSlugResolution(ctx, nesSystem.ID, "mario", nil, insertedNESMedia.DBID, "exact")
	require.NoError(t, err)

	err = mediaDB.SetCachedSlugResolution(ctx, snesSystem.ID, "zelda", nil, insertedSNESMedia.DBID, "exact")
	require.NoError(t, err)

	// Verify both caches exist
	_, _, nesFound := mediaDB.GetCachedSlugResolution(ctx, nesSystem.ID, "mario", nil)
	assert.True(t, nesFound, "NES cache should exist")

	_, _, snesFound := mediaDB.GetCachedSlugResolution(ctx, snesSystem.ID, "zelda", nil)
	assert.True(t, snesFound, "SNES cache should exist")

	// Truncate NES system only
	err = mediaDB.TruncateSystems([]string{nesSystem.ID})
	require.NoError(t, err)

	// Verify NES cache is gone but SNES cache remains
	_, _, nesAfter := mediaDB.GetCachedSlugResolution(ctx, nesSystem.ID, "mario", nil)
	assert.False(t, nesAfter, "NES cache should be invalidated")

	_, _, snesAfter := mediaDB.GetCachedSlugResolution(ctx, snesSystem.ID, "zelda", nil)
	assert.True(t, snesAfter, "SNES cache should remain")
}

// TestMediaDB_Truncate_AllCachesCleared_Integration tests full truncate clears all caches
func TestMediaDB_Truncate_AllCachesCleared_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Setup data
	err := mediaDB.BeginTransaction(false)
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
		Slug:       slugs.Slugify(slugs.MediaTypeGame, "Mario"),
		Name:       "Super Mario Bros",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(&title)
	require.NoError(t, err)

	media := database.Media{
		SystemDBID:     insertedSystem.DBID,
		MediaTitleDBID: insertedTitle.DBID,
		Path:           "/roms/mario.nes",
	}
	insertedMedia, err := mediaDB.InsertMedia(media)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Populate all caches
	err = mediaDB.PopulateSystemTagsCache(ctx)
	require.NoError(t, err)

	err = mediaDB.SetCachedSlugResolution(ctx, nesSystem.ID, "mario", nil, insertedMedia.DBID, "exact")
	require.NoError(t, err)

	// Verify caches are populated
	_, _, slugFound := mediaDB.GetCachedSlugResolution(ctx, nesSystem.ID, "mario", nil)
	assert.True(t, slugFound, "slug cache should be populated")

	// Truncate all data
	err = mediaDB.Truncate()
	require.NoError(t, err)

	// Verify slug cache is cleared
	_, _, slugAfter := mediaDB.GetCachedSlugResolution(ctx, nesSystem.ID, "mario", nil)
	assert.False(t, slugAfter, "slug cache should be cleared after truncate")

	// Verify system tags cache is cleared
	tags, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)
	assert.Empty(t, tags, "system tags cache should be cleared after truncate")
}

// TestCheckForDuplicateMediaTitles_Integration tests duplicate detection with real database.
func TestCheckForDuplicateMediaTitles_Integration(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Create a system
	system, err := mediaDB.InsertSystem(database.System{
		SystemID: "nes",
		Name:     "Nintendo Entertainment System",
	})
	require.NoError(t, err)

	// Insert duplicate slugs (no unique constraint, so this is allowed)
	_, err = mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: system.DBID,
		Slug:       "mario",
		Name:       "Super Mario Bros",
	})
	require.NoError(t, err)

	_, err = mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: system.DBID,
		Slug:       "mario", // Duplicate!
		Name:       "Super Mario Bros 2",
	})
	require.NoError(t, err)

	// Insert unique slug
	_, err = mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: system.DBID,
		Slug:       "zelda",
		Name:       "The Legend of Zelda",
	})
	require.NoError(t, err)

	// Check for duplicates
	duplicates, err := mediaDB.CheckForDuplicateMediaTitles()
	require.NoError(t, err)

	// Should find exactly one duplicate (mario)
	require.Len(t, duplicates, 1)
	assert.Contains(t, duplicates[0], "mario")
	assert.Contains(t, duplicates[0], "count=2")
}

// TestMediaDB_GetMediaBySystemID_Integration tests retrieving media for a specific system.
func TestMediaDB_GetMediaBySystemID_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Insert test data for multiple systems
	err := mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	// Insert NES system
	nesSystemDB := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedNES, err := mediaDB.InsertSystem(nesSystemDB)
	require.NoError(t, err)

	// Insert SNES system
	snesSystemDB := database.System{
		SystemID: snesSystem.ID,
		Name:     "SNES",
	}
	insertedSNES, err := mediaDB.InsertSystem(snesSystemDB)
	require.NoError(t, err)

	// Add NES games
	nesGames := []struct {
		name string
		path string
	}{
		{"Super Mario Bros", "/roms/nes/mario.nes"},
		{"The Legend of Zelda", "/roms/nes/zelda.nes"},
		{"Metroid", "/roms/nes/metroid.nes"},
	}

	for _, game := range nesGames {
		title := database.MediaTitle{
			SystemDBID: insertedNES.DBID,
			Slug:       slugs.Slugify(slugs.MediaTypeGame, game.name),
			Name:       game.name,
		}
		insertedTitle, titleErr := mediaDB.InsertMediaTitle(&title)
		require.NoError(t, titleErr)

		media := database.Media{
			SystemDBID:     insertedNES.DBID,
			MediaTitleDBID: insertedTitle.DBID,
			Path:           game.path,
		}
		_, mediaErr := mediaDB.InsertMedia(media)
		require.NoError(t, mediaErr)
	}

	// Add SNES games
	snesGames := []struct {
		name string
		path string
	}{
		{"Super Mario World", "/roms/snes/mario_world.smc"},
		{"A Link to the Past", "/roms/snes/zelda_lttp.smc"},
	}

	for _, game := range snesGames {
		title := database.MediaTitle{
			SystemDBID: insertedSNES.DBID,
			Slug:       slugs.Slugify(slugs.MediaTypeGame, game.name),
			Name:       game.name,
		}
		insertedTitle, titleErr := mediaDB.InsertMediaTitle(&title)
		require.NoError(t, titleErr)

		media := database.Media{
			SystemDBID:     insertedSNES.DBID,
			MediaTitleDBID: insertedTitle.DBID,
			Path:           game.path,
		}
		_, mediaErr := mediaDB.InsertMedia(media)
		require.NoError(t, mediaErr)
	}

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Test: Get NES media only
	nesMedia, err := mediaDB.GetMediaBySystemID(nesSystem.ID)
	require.NoError(t, err)
	assert.Len(t, nesMedia, 3, "should return exactly 3 NES games")

	// Verify all returned media are NES
	for _, m := range nesMedia {
		assert.Equal(t, nesSystem.ID, m.SystemID, "all media should be NES")
		assert.NotEmpty(t, m.Path, "path should not be empty")
		assert.NotEmpty(t, m.TitleSlug, "slug should not be empty")
		assert.Positive(t, m.DBID, "DBID should be positive")
		assert.Positive(t, m.MediaTitleDBID, "MediaTitleDBID should be positive")
	}

	// Verify specific paths are present
	nesPaths := make([]string, 0, len(nesMedia))
	for _, m := range nesMedia {
		nesPaths = append(nesPaths, m.Path)
	}
	assert.Contains(t, nesPaths, "/roms/nes/mario.nes")
	assert.Contains(t, nesPaths, "/roms/nes/zelda.nes")
	assert.Contains(t, nesPaths, "/roms/nes/metroid.nes")

	// Test: Get SNES media only
	snesMedia, err := mediaDB.GetMediaBySystemID(snesSystem.ID)
	require.NoError(t, err)
	assert.Len(t, snesMedia, 2, "should return exactly 2 SNES games")

	for _, m := range snesMedia {
		assert.Equal(t, snesSystem.ID, m.SystemID, "all media should be SNES")
	}

	// Test: Non-existent system returns empty slice
	emptyMedia, err := mediaDB.GetMediaBySystemID("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, emptyMedia, "non-existent system should return empty slice")

	// Test: Results are ordered by DBID
	for i := 1; i < len(nesMedia); i++ {
		assert.Greater(t, nesMedia[i].DBID, nesMedia[i-1].DBID, "results should be ordered by DBID")
	}
}

// TestMediaDB_GetTitlesBySystemID_Integration tests retrieving titles for a specific system.
func TestMediaDB_GetTitlesBySystemID_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Insert test data for multiple systems
	err := mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	// Insert NES system
	nesSystemDB := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedNES, err := mediaDB.InsertSystem(nesSystemDB)
	require.NoError(t, err)

	// Insert SNES system
	snesSystemDB := database.System{
		SystemID: snesSystem.ID,
		Name:     "SNES",
	}
	insertedSNES, err := mediaDB.InsertSystem(snesSystemDB)
	require.NoError(t, err)

	// Add NES titles
	nesTitles := []struct {
		name string
	}{
		{"Super Mario Bros"},
		{"The Legend of Zelda"},
		{"Metroid"},
	}

	for _, title := range nesTitles {
		mediaTitle := database.MediaTitle{
			SystemDBID: insertedNES.DBID,
			Slug:       slugs.Slugify(slugs.MediaTypeGame, title.name),
			Name:       title.name,
		}
		_, titleErr := mediaDB.InsertMediaTitle(&mediaTitle)
		require.NoError(t, titleErr)
	}

	// Add SNES titles
	snesTitles := []struct {
		name string
	}{
		{"Super Mario World"},
		{"A Link to the Past"},
	}

	for _, title := range snesTitles {
		mediaTitle := database.MediaTitle{
			SystemDBID: insertedSNES.DBID,
			Slug:       slugs.Slugify(slugs.MediaTypeGame, title.name),
			Name:       title.name,
		}
		_, titleErr := mediaDB.InsertMediaTitle(&mediaTitle)
		require.NoError(t, titleErr)
	}

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Test: Get NES titles only
	nesTitleResults, err := mediaDB.GetTitlesBySystemID(nesSystem.ID)
	require.NoError(t, err)
	assert.Len(t, nesTitleResults, 3, "should return exactly 3 NES titles")

	// Verify all returned titles are NES
	for _, title := range nesTitleResults {
		assert.Equal(t, nesSystem.ID, title.SystemID, "all titles should be NES")
		assert.NotEmpty(t, title.Slug, "slug should not be empty")
		assert.NotEmpty(t, title.Name, "name should not be empty")
		assert.Positive(t, title.DBID, "DBID should be positive")
		assert.Positive(t, title.SystemDBID, "SystemDBID should be positive")
	}

	// Verify specific names are present
	nesNames := make([]string, 0, len(nesTitleResults))
	for _, title := range nesTitleResults {
		nesNames = append(nesNames, title.Name)
	}
	assert.Contains(t, nesNames, "Super Mario Bros")
	assert.Contains(t, nesNames, "The Legend of Zelda")
	assert.Contains(t, nesNames, "Metroid")

	// Test: Get SNES titles only
	snesTitleResults, err := mediaDB.GetTitlesBySystemID(snesSystem.ID)
	require.NoError(t, err)
	assert.Len(t, snesTitleResults, 2, "should return exactly 2 SNES titles")

	for _, title := range snesTitleResults {
		assert.Equal(t, snesSystem.ID, title.SystemID, "all titles should be SNES")
	}

	// Test: Non-existent system returns empty slice
	emptyTitles, err := mediaDB.GetTitlesBySystemID("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, emptyTitles, "non-existent system should return empty slice")

	// Test: Results are ordered by DBID
	for i := 1; i < len(nesTitleResults); i++ {
		assert.Greater(t, nesTitleResults[i].DBID, nesTitleResults[i-1].DBID, "results should be ordered by DBID")
	}
}
