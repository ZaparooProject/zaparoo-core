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
	"fmt"
	"os"
	"path/filepath"
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

// insertNESGameWithTag inserts a single NES system with a "Super Mario Bros" title,
// one media file, and a "genre:platform" tag on that file inside a committed transaction.
// Used by tests that need a minimal tagged-media fixture without caring about the returned entities.
func insertNESGameWithTag(t *testing.T, mediaDB *MediaDB) {
	t.Helper()

	genreTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "genre"})
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	insertedSystem, err := mediaDB.InsertSystem(database.System{SystemID: nesSystem.ID, Name: "NES"})
	require.NoError(t, err)

	insertedTitle, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       "supermariobros",
		Name:       "Super Mario Bros",
	})
	require.NoError(t, err)

	insertedMedia, err := mediaDB.InsertMedia(database.Media{
		SystemDBID:     insertedSystem.DBID,
		MediaTitleDBID: insertedTitle.DBID,
		Path:           filepath.Join("roms", "nes", "smb.nes"),
	})
	require.NoError(t, err)

	platformTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: genreTagType.DBID, Tag: "platform"})
	require.NoError(t, err)

	_, err = mediaDB.InsertMediaTag(database.MediaTag{MediaDBID: insertedMedia.DBID, TagDBID: platformTag.DBID})
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)
}

func insertTaggedGame(
	t *testing.T,
	mediaDB *MediaDB,
	systemID string,
	titleName string,
	relPath string,
	tagType string,
	tagValue string,
) {
	t.Helper()

	insertedTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: tagType})
	require.NoError(t, err)

	system, err := systemdefs.GetSystem(systemID)
	require.NoError(t, err)

	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	insertedSystem, err := mediaDB.FindOrInsertSystem(database.System{SystemID: system.ID, Name: system.ID})
	require.NoError(t, err)

	insertedTitle, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       slugs.Slugify(system.GetMediaType(), titleName),
		Name:       titleName,
	})
	require.NoError(t, err)

	insertedMedia, err := mediaDB.InsertMedia(database.Media{
		SystemDBID:     insertedSystem.DBID,
		MediaTitleDBID: insertedTitle.DBID,
		Path:           relPath,
	})
	require.NoError(t, err)

	insertedTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: insertedTagType.DBID, Tag: tagValue})
	require.NoError(t, err)

	_, err = mediaDB.InsertMediaTag(database.MediaTag{MediaDBID: insertedMedia.DBID, TagDBID: insertedTag.DBID})
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)
}

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

func assertIndexExists(t *testing.T, mediaDB *MediaDB, indexName string) {
	t.Helper()

	var found string
	err := mediaDB.UnsafeGetSQLDb().QueryRowContext(
		context.Background(),
		"SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?",
		indexName,
	).Scan(&found)
	require.NoError(t, err)
	assert.Equal(t, indexName, found)
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

func TestMediaDB_Open_RequiredSecondaryIndexesExist_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	assertIndexExists(t, mediaDB, "media_path_idx")
	assertIndexExists(t, mediaDB, "idx_media_parentdir")
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

// TestMediaDB_CacheFastPath_MatchesSQL_Integration verifies that cache fast-paths
// produce the same results as the SQL-only fallback paths. Runs each query twice:
// once without the slug search cache (SQL path) and once with it (cache path).
func TestMediaDB_CacheFastPath_MatchesSQL_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create tag types before transaction
	regionTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "region"})
	require.NoError(t, err)
	genreTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "genre"})
	require.NoError(t, err)

	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	insertedNES, err := mediaDB.InsertSystem(database.System{SystemID: nesSystem.ID, Name: "NES"})
	require.NoError(t, err)
	insertedSNES, err := mediaDB.InsertSystem(database.System{SystemID: snesSystem.ID, Name: "SNES"})
	require.NoError(t, err)

	type testGame struct {
		name       string
		secSlug    string
		path       string
		tags       []database.TagInfo
		systemDBID int64
	}

	games := []testGame{
		{
			systemDBID: insertedNES.DBID, name: "Super Mario Bros", path: "/roms/nes/smb.nes",
			tags: []database.TagInfo{{Type: "region", Tag: "usa"}, {Type: "genre", Tag: "platform"}},
		},
		{
			systemDBID: insertedNES.DBID, name: "Super Mario Bros 2", path: "/roms/nes/smb2.nes",
			tags: []database.TagInfo{{Type: "region", Tag: "japan"}, {Type: "genre", Tag: "platform"}},
		},
		{
			systemDBID: insertedNES.DBID, name: "Dr. Mario", path: "/roms/nes/dr_mario.nes",
			tags: []database.TagInfo{{Type: "region", Tag: "usa"}, {Type: "genre", Tag: "puzzle"}},
		},
		{
			systemDBID: insertedSNES.DBID, name: "Super Mario World", path: "/roms/snes/smw.smc",
			tags: []database.TagInfo{{Type: "region", Tag: "usa"}, {Type: "genre", Tag: "platform"}},
		},
		{
			systemDBID: insertedSNES.DBID, name: "The Legend of Zelda: A Link to the Past", secSlug: "zelda3",
			path: "/roms/snes/zelda_lttp.smc",
			tags: []database.TagInfo{{Type: "region", Tag: "usa"}, {Type: "genre", Tag: "adventure"}},
		},
	}

	for _, game := range games {
		var secSlug sql.NullString
		if game.secSlug != "" {
			secSlug = sql.NullString{String: game.secSlug, Valid: true}
		}
		title := database.MediaTitle{
			SystemDBID:    game.systemDBID,
			Slug:          slugs.Slugify(slugs.MediaTypeGame, game.name),
			Name:          game.name,
			SecondarySlug: secSlug,
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

		for _, tag := range game.tags {
			var tagTypeDBID int64
			switch tag.Type {
			case "region":
				tagTypeDBID = regionTagType.DBID
			case "genre":
				tagTypeDBID = genreTagType.DBID
			}
			dbTag, tagErr := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: tagTypeDBID, Tag: tag.Tag})
			require.NoError(t, tagErr)
			_, tagErr = mediaDB.InsertMediaTag(database.MediaTag{MediaDBID: insertedMedia.DBID, TagDBID: dbTag.DBID})
			require.NoError(t, tagErr)
		}
	}

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// --- Phase 1: Run queries WITHOUT cache (SQL path) ---
	// Cache should be nil since setupTempMediaDB creates a new DB
	assert.Nil(t, mediaDB.slugSearchCache.Load(), "cache should be nil before build")

	type searchQuery struct {
		fn   func() ([]database.SearchResultWithCursor, error)
		name string
	}

	queries := []searchQuery{
		{name: "SearchMediaBySlug NES supermariobrothers", fn: func() ([]database.SearchResultWithCursor, error) {
			return mediaDB.SearchMediaBySlug(ctx, "NES", "Super Mario Bros", nil)
		}},
		{name: "SearchMediaBySlug SNES supermarioworld", fn: func() ([]database.SearchResultWithCursor, error) {
			return mediaDB.SearchMediaBySlug(ctx, "SNES", "Super Mario World", nil)
		}},
		{name: "SearchMediaBySlug NES drmario with tag", fn: func() ([]database.SearchResultWithCursor, error) {
			return mediaDB.SearchMediaBySlug(ctx, "NES", "Dr. Mario",
				[]zapscript.TagFilter{{Type: "genre", Value: "puzzle"}})
		}},
		{name: "SearchMediaBySlug no match", fn: func() ([]database.SearchResultWithCursor, error) {
			return mediaDB.SearchMediaBySlug(ctx, "NES", "nonexistent", nil)
		}},
		{name: "SearchMediaBySlug wrong system", fn: func() ([]database.SearchResultWithCursor, error) {
			return mediaDB.SearchMediaBySlug(ctx, "Genesis", "Super Mario Bros", nil)
		}},
		{name: "SearchMediaBySecondarySlug zelda3", fn: func() ([]database.SearchResultWithCursor, error) {
			return mediaDB.SearchMediaBySecondarySlug(ctx, "SNES", "zelda3", nil)
		}},
		{name: "SearchMediaBySecondarySlug no match", fn: func() ([]database.SearchResultWithCursor, error) {
			return mediaDB.SearchMediaBySecondarySlug(ctx, "SNES", "nonexistent", nil)
		}},
		{name: "SearchMediaBySlugPrefix supermario NES", fn: func() ([]database.SearchResultWithCursor, error) {
			return mediaDB.SearchMediaBySlugPrefix(ctx, "NES", "Super Mario", nil)
		}},
		{name: "SearchMediaBySlugPrefix no match", fn: func() ([]database.SearchResultWithCursor, error) {
			return mediaDB.SearchMediaBySlugPrefix(ctx, "NES", "zzz", nil)
		}},
		{name: "SearchMediaBySlugIn NES multi", fn: func() ([]database.SearchResultWithCursor, error) {
			return mediaDB.SearchMediaBySlugIn(ctx, "NES",
				[]string{"Super Mario Bros", "Dr. Mario"}, nil)
		}},
		{name: "SearchMediaBySlugIn no match", fn: func() ([]database.SearchResultWithCursor, error) {
			return mediaDB.SearchMediaBySlugIn(ctx, "NES", []string{"nonexistent"}, nil)
		}},
	}

	sqlResults := make([][]database.SearchResultWithCursor, len(queries))
	for i, q := range queries {
		result, queryErr := q.fn()
		require.NoError(t, queryErr, "SQL path failed for %s", q.name)
		sqlResults[i] = result
	}

	// --- Phase 2: Build cache and run the same queries (cache path) ---
	err = mediaDB.RebuildSlugSearchCache()
	require.NoError(t, err)
	require.NotNil(t, mediaDB.slugSearchCache.Load(), "cache should be built")

	for i, q := range queries {
		cacheResult, queryErr := q.fn()
		require.NoError(t, queryErr, "cache path failed for %s", q.name)

		// Compare result counts
		assert.Len(t, cacheResult, len(sqlResults[i]),
			"result count mismatch for %s: SQL=%d cache=%d",
			q.name, len(sqlResults[i]), len(cacheResult))

		// Compare result sets (order may differ between SQL and cache paths)
		if len(cacheResult) == len(sqlResults[i]) {
			sqlPaths := make([]string, len(sqlResults[i]))
			cachePaths := make([]string, len(cacheResult))
			for j := range sqlResults[i] {
				sqlPaths[j] = sqlResults[i][j].Path
			}
			for j := range cacheResult {
				cachePaths[j] = cacheResult[j].Path
			}
			assert.ElementsMatch(t, sqlPaths, cachePaths,
				"path set mismatch for %s", q.name)
		}
	}

	// --- Phase 3: Verify RandomGame and RandomGameWithQuery via cache ---
	result, err := mediaDB.RandomGame([]systemdefs.System{*nesSystem})
	require.NoError(t, err)
	assert.Equal(t, nesSystem.ID, result.SystemID)
	assert.NotEmpty(t, result.Path)

	result, err = mediaDB.RandomGameWithQuery(&database.MediaQuery{
		Systems: []string{nesSystem.ID},
	})
	require.NoError(t, err)
	assert.Equal(t, nesSystem.ID, result.SystemID)
	assert.NotEmpty(t, result.Path)

	// RandomGameWithQuery with multiple systems
	result, err = mediaDB.RandomGameWithQuery(&database.MediaQuery{
		Systems: []string{nesSystem.ID, snesSystem.ID},
	})
	require.NoError(t, err)
	assert.Contains(t, []string{nesSystem.ID, snesSystem.ID}, result.SystemID)
	assert.NotEmpty(t, result.Path)

	// RandomGame with empty system filter should fail
	_, err = mediaDB.RandomGame(nil)
	require.Error(t, err)
}

// TestMediaDB_RandomGame_MixedSystems_Integration verifies that RandomGame and
// RandomGameWithQuery correctly handle a system list that includes systems with
// no indexed content. Both cache and SQL fallback paths are tested.
func TestMediaDB_RandomGame_MixedSystems_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Only NES and SNES get indexed content; Genesis and GameBoy do not.
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)
	genesisSystem, err := systemdefs.GetSystem("Genesis")
	require.NoError(t, err)
	gbSystem, err := systemdefs.GetSystem("Gameboy")
	require.NoError(t, err)

	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	insertedNES, err := mediaDB.InsertSystem(database.System{SystemID: nesSystem.ID, Name: "NES"})
	require.NoError(t, err)
	insertedSNES, err := mediaDB.InsertSystem(database.System{SystemID: snesSystem.ID, Name: "SNES"})
	require.NoError(t, err)

	for i := 1; i <= 5; i++ {
		name := fmt.Sprintf("NES Game %d", i)
		title := database.MediaTitle{
			SystemDBID: insertedNES.DBID,
			Slug:       slugs.Slugify(slugs.MediaTypeGame, name),
			Name:       name,
		}
		insertedTitle, titleErr := mediaDB.InsertMediaTitle(&title)
		require.NoError(t, titleErr)
		_, mediaErr := mediaDB.InsertMedia(database.Media{
			SystemDBID:     insertedNES.DBID,
			MediaTitleDBID: insertedTitle.DBID,
			Path:           fmt.Sprintf("/roms/nes/game%d.nes", i),
		})
		require.NoError(t, mediaErr)
	}
	for i := 1; i <= 5; i++ {
		name := fmt.Sprintf("SNES Game %d", i)
		title := database.MediaTitle{
			SystemDBID: insertedSNES.DBID,
			Slug:       slugs.Slugify(slugs.MediaTypeGame, name),
			Name:       name,
		}
		insertedTitle, titleErr := mediaDB.InsertMediaTitle(&title)
		require.NoError(t, titleErr)
		_, mediaErr := mediaDB.InsertMedia(database.Media{
			SystemDBID:     insertedSNES.DBID,
			MediaTitleDBID: insertedTitle.DBID,
			Path:           fmt.Sprintf("/roms/snes/game%d.sfc", i),
		})
		require.NoError(t, mediaErr)
	}

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	mixedSystems := []systemdefs.System{*nesSystem, *snesSystem, *genesisSystem, *gbSystem}
	indexedIDs := map[string]bool{nesSystem.ID: true, snesSystem.ID: true}

	// --- SQL fallback path (no cache) ---
	assert.Nil(t, mediaDB.slugSearchCache.Load(), "cache should be nil before build")

	for i := range 20 {
		result, randErr := mediaDB.RandomGame(mixedSystems)
		require.NoError(t, randErr, "RandomGame SQL path iteration %d", i)
		assert.True(t, indexedIDs[result.SystemID],
			"RandomGame SQL path returned non-indexed system %s", result.SystemID)
	}

	mixedSystemIDs := []string{nesSystem.ID, snesSystem.ID, genesisSystem.ID, gbSystem.ID}
	for i := range 20 {
		result, randErr := mediaDB.RandomGameWithQuery(&database.MediaQuery{Systems: mixedSystemIDs})
		require.NoError(t, randErr, "RandomGameWithQuery SQL path iteration %d", i)
		assert.True(t, indexedIDs[result.SystemID],
			"RandomGameWithQuery SQL path returned non-indexed system %s", result.SystemID)
	}

	// --- Cache path ---
	err = mediaDB.RebuildSlugSearchCache()
	require.NoError(t, err)
	require.NotNil(t, mediaDB.slugSearchCache.Load())

	for i := range 20 {
		result, randErr := mediaDB.RandomGame(mixedSystems)
		require.NoError(t, randErr, "RandomGame cache path iteration %d", i)
		assert.True(t, indexedIDs[result.SystemID],
			"RandomGame cache path returned non-indexed system %s", result.SystemID)
	}

	for i := range 20 {
		result, randErr := mediaDB.RandomGameWithQuery(&database.MediaQuery{Systems: mixedSystemIDs})
		require.NoError(t, randErr, "RandomGameWithQuery cache path iteration %d", i)
		assert.True(t, indexedIDs[result.SystemID],
			"RandomGameWithQuery cache path returned non-indexed system %s", result.SystemID)
	}

	// --- Edge case: ALL systems non-indexed ---
	nonIndexedSystems := []systemdefs.System{*genesisSystem, *gbSystem}
	_, err = mediaDB.RandomGame(nonIndexedSystems)
	require.ErrorIs(t, err, sql.ErrNoRows, "RandomGame with all non-indexed systems should return ErrNoRows")

	_, err = mediaDB.RandomGameWithQuery(&database.MediaQuery{
		Systems: []string{genesisSystem.ID, gbSystem.ID},
	})
	require.ErrorIs(t, err, sql.ErrNoRows, "RandomGameWithQuery with all non-indexed systems should return ErrNoRows")

	// --- Edge case: single non-indexed system ---
	_, err = mediaDB.RandomGame([]systemdefs.System{*genesisSystem})
	require.ErrorIs(t, err, sql.ErrNoRows, "RandomGame with single non-indexed system should return ErrNoRows")

	_, err = mediaDB.RandomGameWithQuery(&database.MediaQuery{
		Systems: []string{genesisSystem.ID},
	})
	require.ErrorIs(t, err, sql.ErrNoRows, "RandomGameWithQuery with single non-indexed system should return ErrNoRows")
}

func TestMediaDB_RefreshSlugSearchCacheForSystems_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	insertGame := func(system *systemdefs.System, name, relPath string) {
		t.Helper()

		require.NoError(t, mediaDB.BeginTransaction(false))

		insertedSystem, insertErr := mediaDB.FindOrInsertSystem(database.System{SystemID: system.ID, Name: system.ID})
		require.NoError(t, insertErr)

		insertedTitle, insertErr := mediaDB.InsertMediaTitle(&database.MediaTitle{
			SystemDBID: insertedSystem.DBID,
			Slug:       slugs.Slugify(system.GetMediaType(), name),
			Name:       name,
		})
		require.NoError(t, insertErr)

		_, insertErr = mediaDB.InsertMedia(database.Media{
			SystemDBID:     insertedSystem.DBID,
			MediaTitleDBID: insertedTitle.DBID,
			Path:           relPath,
		})
		require.NoError(t, insertErr)

		require.NoError(t, mediaDB.CommitTransaction())
	}

	insertGame(nesSystem, "Super Mario Bros", filepath.Join("roms", "nes", "smb.nes"))
	insertGame(snesSystem, "The Legend of Zelda", filepath.Join("roms", "snes", "zelda.sfc"))

	err = mediaDB.RebuildSlugSearchCache()
	require.NoError(t, err)

	cache := mediaDB.slugSearchCache.Load()
	require.NotNil(t, cache)
	assert.True(t, cache.complete)
	assert.True(t, cache.CanServeSystems([]string{nesSystem.ID, snesSystem.ID}))

	results, err := mediaDB.SearchMediaBySlug(ctx, nesSystem.ID, "Super Mario Bros", nil)
	require.NoError(t, err)
	assert.Len(t, results, 1)

	err = mediaDB.TruncateSystems([]string{nesSystem.ID})
	require.NoError(t, err)

	cache = mediaDB.slugSearchCache.Load()
	require.NotNil(t, cache)
	assert.False(t, cache.complete)
	assert.False(t, cache.CanServeSystems([]string{nesSystem.ID}))
	assert.True(t, cache.CanServeSystems([]string{snesSystem.ID}))

	results, err = mediaDB.SearchMediaBySlug(ctx, snesSystem.ID, "The Legend of Zelda", nil)
	require.NoError(t, err)
	assert.Len(t, results, 1)

	require.NoError(t, mediaDB.SetIndexingSystems([]string{nesSystem.ID}))
	require.NoError(t, mediaDB.SetIndexingStatus(IndexingStatusRunning))
	defer func() {
		require.NoError(t, mediaDB.SetIndexingStatus(""))
		require.NoError(t, mediaDB.SetIndexingSystems(nil))
	}()

	insertGame(nesSystem, "Super Mario Bros Redux", filepath.Join("roms", "nes", "smb-redux.nes"))

	cache = mediaDB.slugSearchCache.Load()
	require.NotNil(t, cache)
	assert.False(t, cache.complete)
	assert.False(t, cache.CanServeSystems([]string{nesSystem.ID}))
	assert.True(t, cache.CanServeSystems([]string{snesSystem.ID}))

	err = mediaDB.RefreshSlugSearchCacheForSystems(ctx, []string{nesSystem.ID})
	require.NoError(t, err)

	cache = mediaDB.slugSearchCache.Load()
	require.NotNil(t, cache)
	assert.False(t, cache.complete)
	assert.True(t, cache.CanServeSystems([]string{nesSystem.ID}))
	assert.True(t, cache.CanServeSystems([]string{snesSystem.ID}))
	assert.True(t, cache.CanServeSystems([]string{nesSystem.ID, snesSystem.ID}))

	results, err = mediaDB.SearchMediaBySlug(ctx, nesSystem.ID, "Super Mario Bros Redux", nil)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, filepath.Join("roms", "nes", "smb-redux.nes"), results[0].Path)

	results, err = mediaDB.SearchMediaBySlug(ctx, nesSystem.ID, "Super Mario Bros", nil)
	require.NoError(t, err)
	assert.Empty(t, results)

	results, err = mediaDB.SearchMediaBySlug(ctx, snesSystem.ID, "The Legend of Zelda", nil)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, filepath.Join("roms", "snes", "zelda.sfc"), results[0].Path)
}

func TestMediaDB_CommitTransaction_SelectiveIndexingPreservesUnchangedSlugCache_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	insertGame := func(system *systemdefs.System, titleName string, relPath string) {
		t.Helper()

		err = mediaDB.BeginTransaction(false)
		require.NoError(t, err)

		insertedSystem, insertErr := mediaDB.FindOrInsertSystem(database.System{SystemID: system.ID, Name: system.ID})
		require.NoError(t, insertErr)

		insertedTitle, insertErr := mediaDB.InsertMediaTitle(&database.MediaTitle{
			SystemDBID: insertedSystem.DBID,
			Slug:       slugs.Slugify(slugs.MediaTypeGame, titleName),
			Name:       titleName,
		})
		require.NoError(t, insertErr)

		_, insertErr = mediaDB.InsertMedia(database.Media{
			SystemDBID:     insertedSystem.DBID,
			MediaTitleDBID: insertedTitle.DBID,
			Path:           relPath,
		})
		require.NoError(t, insertErr)

		require.NoError(t, mediaDB.CommitTransaction())
	}

	insertGame(nesSystem, "Super Mario Bros", filepath.Join("roms", "nes", "smb.nes"))
	insertGame(snesSystem, "The Legend of Zelda", filepath.Join("roms", "snes", "zelda.sfc"))

	err = mediaDB.RebuildSlugSearchCache()
	require.NoError(t, err)

	cache := mediaDB.slugSearchCache.Load()
	require.NotNil(t, cache)
	assert.True(t, cache.complete)
	assert.True(t, cache.CanServeSystems([]string{nesSystem.ID, snesSystem.ID}))

	require.NoError(t, mediaDB.SetIndexingSystems([]string{nesSystem.ID}))
	require.NoError(t, mediaDB.SetIndexingStatus(IndexingStatusRunning))
	defer func() {
		require.NoError(t, mediaDB.SetIndexingStatus(""))
		require.NoError(t, mediaDB.SetIndexingSystems(nil))
	}()

	insertGame(nesSystem, "Super Mario Bros Redux", filepath.Join("roms", "nes", "smb-redux.nes"))

	cache = mediaDB.slugSearchCache.Load()
	require.NotNil(t, cache)
	assert.False(t, cache.complete)
	assert.False(t, cache.CanServeSystems([]string{nesSystem.ID}))
	assert.True(t, cache.CanServeSystems([]string{snesSystem.ID}))
}

func TestMediaDB_SearchMediaWithFilters_SelectiveIndexingKeepsUnchangedSystemsCacheEligible_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	insertGame := func(system *systemdefs.System, titleName string, relPath string) {
		t.Helper()

		require.NoError(t, mediaDB.BeginTransaction(false))

		insertedSystem, insertErr := mediaDB.FindOrInsertSystem(database.System{SystemID: system.ID, Name: system.ID})
		require.NoError(t, insertErr)

		insertedTitle, insertErr := mediaDB.InsertMediaTitle(&database.MediaTitle{
			SystemDBID: insertedSystem.DBID,
			Slug:       slugs.Slugify(system.GetMediaType(), titleName),
			Name:       titleName,
		})
		require.NoError(t, insertErr)

		_, insertErr = mediaDB.InsertMedia(database.Media{
			SystemDBID:     insertedSystem.DBID,
			MediaTitleDBID: insertedTitle.DBID,
			Path:           relPath,
		})
		require.NoError(t, insertErr)

		require.NoError(t, mediaDB.CommitTransaction())
	}

	insertGame(nesSystem, "Super Mario Bros", filepath.Join("roms", "nes", "smb.nes"))
	insertGame(snesSystem, "The Legend of Zelda", filepath.Join("roms", "snes", "zelda.sfc"))

	require.NoError(t, mediaDB.RebuildSlugSearchCache())
	require.NoError(t, mediaDB.SetIndexingSystems([]string{nesSystem.ID}))
	require.NoError(t, mediaDB.SetIndexingStatus(IndexingStatusRunning))
	defer func() {
		require.NoError(t, mediaDB.SetIndexingStatus(""))
		require.NoError(t, mediaDB.SetIndexingSystems(nil))
	}()

	insertGame(nesSystem, "Super Mario Bros Redux", filepath.Join("roms", "nes", "smb-redux.nes"))
	require.NoError(t, mediaDB.RefreshSlugSearchCacheForSystems(ctx, []string{nesSystem.ID}))

	cache := mediaDB.slugSearchCache.Load()
	require.NotNil(t, cache)
	assert.True(t, cache.CanServeSystems([]string{snesSystem.ID}))

	results, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
		Systems: []systemdefs.System{*snesSystem},
		Query:   "Zelda",
		Limit:   10,
	})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, snesSystem.ID, results[0].SystemID)
	assert.Equal(t, filepath.Join("roms", "snes", "zelda.sfc"), results[0].Path)
}

func TestMediaDB_CreateSecondaryIndexes_RecreatesMissingIndexes_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	for _, indexName := range []string{"media_path_idx", "idx_media_parentdir"} {
		_, err := mediaDB.UnsafeGetSQLDb().ExecContext(ctx, "DROP INDEX IF EXISTS "+indexName)
		require.NoError(t, err)
	}

	mediaDB.needsIndexRebuild.Store(false)

	err := mediaDB.CreateSecondaryIndexes()
	require.NoError(t, err)
	assert.False(t, mediaDB.needsIndexRebuild.Load())

	for _, indexName := range []string{"media_path_idx", "idx_media_parentdir"} {
		assertIndexExists(t, mediaDB, indexName)
	}
}

func TestMediaDB_RebuildTagCache_SelectiveIndexingWarmsTouchedAndUntouchedSystems_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	insertTaggedGame(
		t,
		mediaDB,
		nesSystem.ID,
		"Super Mario Bros",
		filepath.Join("roms", "nes", "smb.nes"),
		"genre",
		"platform",
	)
	insertTaggedGame(
		t,
		mediaDB,
		snesSystem.ID,
		"The Legend of Zelda",
		filepath.Join("roms", "snes", "zelda.sfc"),
		"genre",
		"adventure",
	)

	require.NoError(t, mediaDB.PopulateSystemTagsCache(ctx))
	require.NoError(t, mediaDB.RebuildTagCache())

	require.NoError(t, mediaDB.SetIndexingSystems([]string{nesSystem.ID}))
	require.NoError(t, mediaDB.SetIndexingStatus(IndexingStatusRunning))
	defer func() {
		require.NoError(t, mediaDB.SetIndexingStatus(""))
		require.NoError(t, mediaDB.SetIndexingSystems(nil))
	}()

	require.NoError(t, mediaDB.UpdateLastGenerated())
	assert.Nil(t, mediaDB.inMemoryTagCache.Load(), "scoped invalidation should clear in-memory tag cache")

	require.NoError(t, mediaDB.PopulateSystemTagsCacheForSystems(ctx, []systemdefs.System{*nesSystem}))
	require.NoError(t, mediaDB.RebuildTagCache())

	cache := mediaDB.inMemoryTagCache.Load()
	require.NotNil(t, cache)
	assert.Contains(t, cache.bySystem, nesSystem.ID)
	assert.Contains(t, cache.bySystem, snesSystem.ID)

	nesTags, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)
	assert.Contains(t, nesTags, database.TagInfo{Type: "genre", Tag: "platform", Count: 1})

	snesTags, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{*snesSystem})
	require.NoError(t, err)
	assert.Contains(t, snesTags, database.TagInfo{Type: "genre", Tag: "adventure", Count: 1})
}

// TestMediaDB_GetMediaByDBID_TitleTags_Integration verifies that GetMediaByDBID
// returns tags from both MediaTags (file-level) and MediaTitleTags (title-level).
func TestMediaDB_GetMediaByDBID_TitleTags_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create tag types before transaction
	regionTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "region"})
	require.NoError(t, err)
	genreTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "genre"})
	require.NoError(t, err)

	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	insertedSystem, err := mediaDB.InsertSystem(database.System{SystemID: nesSystem.ID, Name: "NES"})
	require.NoError(t, err)

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
		Path:           "/roms/nes/smb.nes",
	}
	insertedMedia, err := mediaDB.InsertMedia(media)
	require.NoError(t, err)

	// File-level tag (MediaTags): region:usa
	usaTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: regionTagType.DBID, Tag: "usa"})
	require.NoError(t, err)
	_, err = mediaDB.InsertMediaTag(database.MediaTag{MediaDBID: insertedMedia.DBID, TagDBID: usaTag.DBID})
	require.NoError(t, err)

	// Title-level tag (MediaTitleTags): genre:platform
	platformTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: genreTagType.DBID, Tag: "platform"})
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Insert title-level tag directly — no production write path for
	// MediaTitleTags yet (read queries prepared ahead of indexer support)
	_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
		"INSERT INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES (?, ?)",
		insertedTitle.DBID, platformTag.DBID)
	require.NoError(t, err)

	// Retrieve via GetMediaByDBID
	result, err := mediaDB.GetMediaByDBID(ctx, insertedMedia.DBID)
	require.NoError(t, err)

	assert.Equal(t, nesSystem.ID, result.SystemID)
	assert.Equal(t, "Super Mario Bros", result.Name)
	assert.Len(t, result.Tags, 2, "should have both file-level and title-level tags")
	assert.Contains(t, result.Tags, database.TagInfo{Type: "region", Tag: "usa"})
	assert.Contains(t, result.Tags, database.TagInfo{Type: "genre", Tag: "platform"})

	// --- Title-level only: media with no file-level tags ---
	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	title2 := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       slugs.Slugify(slugs.MediaTypeGame, "Zelda"),
		Name:       "Zelda",
	}
	insertedTitle2, err := mediaDB.InsertMediaTitle(&title2)
	require.NoError(t, err)

	media2 := database.Media{
		SystemDBID:     insertedSystem.DBID,
		MediaTitleDBID: insertedTitle2.DBID,
		Path:           "/roms/nes/zelda.nes",
	}
	insertedMedia2, err := mediaDB.InsertMedia(media2)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
		"INSERT INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES (?, ?)",
		insertedTitle2.DBID, platformTag.DBID)
	require.NoError(t, err)

	result2, err := mediaDB.GetMediaByDBID(ctx, insertedMedia2.DBID)
	require.NoError(t, err)
	assert.Len(t, result2.Tags, 1, "should have only the title-level tag")
	assert.Contains(t, result2.Tags, database.TagInfo{Type: "genre", Tag: "platform"})

	// --- Dedup: same tag at both file and title level ---
	_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
		"INSERT INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES (?, ?)",
		insertedTitle.DBID, usaTag.DBID)
	require.NoError(t, err)

	result3, err := mediaDB.GetMediaByDBID(ctx, insertedMedia.DBID)
	require.NoError(t, err)
	assert.Len(t, result3.Tags, 2, "DISTINCT should deduplicate tag present at both levels")
	assert.Contains(t, result3.Tags, database.TagInfo{Type: "region", Tag: "usa"})
	assert.Contains(t, result3.Tags, database.TagInfo{Type: "genre", Tag: "platform"})
}

// TestMediaDB_UpdateLastGenerated_ClearsSystemTagsCache_Integration is a regression
// test for the bug where PopulateSystemTagsCache was called before UpdateLastGenerated
// in NewNamesIndex, so the cache was wiped immediately after population. After the fix,
// caches are populated after UpdateLastGenerated, so they persist across service restarts.
func TestMediaDB_UpdateLastGenerated_ClearsSystemTagsCache_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()
	insertNESGameWithTag(t, mediaDB)

	// Populate cache (as NewNamesIndex does before the fix).
	err := mediaDB.PopulateSystemTagsCache(ctx)
	require.NoError(t, err)

	err = mediaDB.RebuildTagCache()
	require.NoError(t, err)

	// UpdateLastGenerated deletes all SystemTagsCache rows (via invalidateCaches).
	err = mediaDB.UpdateLastGenerated()
	require.NoError(t, err)

	// Directly verify the cache table is empty — this is the invariant the fix relies on.
	var cacheRowCount int
	err = mediaDB.UnsafeGetSQLDb().QueryRowContext(ctx, "SELECT COUNT(*) FROM SystemTagsCache").Scan(&cacheRowCount)
	require.NoError(t, err)
	assert.Equal(t, 0, cacheRowCount, "UpdateLastGenerated must wipe SystemTagsCache rows")

	// Simulate what OpenMediaDB does on a service restart: RebuildTagCache reads
	// from SystemTagsCache, which is now empty after UpdateLastGenerated.
	err = mediaDB.RebuildTagCache()
	require.NoError(t, err)

	// After the fix: RebuildTagCache stores nil when SQL is empty; GetAllUsedTags
	// also guards len(allTags)>0, so both empty and nil caches fall through to
	// sqlGetAllUsedTags, returning the underlying data directly.
	allTags, err := mediaDB.GetAllUsedTags(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, allTags, "GetAllUsedTags must return tags even after UpdateLastGenerated wipes SystemTagsCache")
	assert.Contains(t, allTags, database.TagInfo{Type: "genre", Tag: "platform", Count: 1})

	// Repopulate as the corrected NewNamesIndex does (after UpdateLastGenerated).
	err = mediaDB.PopulateSystemTagsCache(ctx)
	require.NoError(t, err)

	err = mediaDB.RebuildTagCache()
	require.NoError(t, err)

	allTagsAfter, err := mediaDB.GetAllUsedTags(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, allTagsAfter, "GetAllUsedTags must return tags after post-UpdateLastGenerated repopulation")
	assert.Contains(t, allTagsAfter, database.TagInfo{Type: "genre", Tag: "platform", Count: 1})
}

// TestMediaDB_GetAllUsedTags_NilInMemoryCache_Integration is a regression test for
// the bug where a service restart with an empty SystemTagsCache left inMemoryTagCache
// nil, causing GetAllUsedTags to return no results. The fix: RebuildTagCache refuses
// to store an empty cache (leaves it nil), and GetAllUsedTags falls through to SQL
// when the cache is nil.
func TestMediaDB_GetAllUsedTags_NilInMemoryCache_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()
	insertNESGameWithTag(t, mediaDB)

	// RebuildTagCache with an empty SystemTagsCache leaves the in-memory cache nil
	// (no data to store). GetAllUsedTags must fall through to the SQL query.
	err := mediaDB.RebuildTagCache()
	require.NoError(t, err)

	assert.Nil(t, mediaDB.inMemoryTagCache.Load(), "cache must stay nil when SystemTagsCache has no rows")

	allTags, err := mediaDB.GetAllUsedTags(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, allTags, "GetAllUsedTags must fall through to SQL when in-memory cache is nil")
	assert.Contains(t, allTags, database.TagInfo{Type: "genre", Tag: "platform", Count: 1})
}

// TestMediaDB_PopulateSystemTagsCache_CountAggregation_Integration verifies that
// sqlPopulateSystemTagsCache correctly sums contributions from both MediaTags
// (file-level) and MediaTitleTags (title-level) via its UNION ALL + GROUP BY/SUM.
func TestMediaDB_PopulateSystemTagsCache_CountAggregation_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Set up: one tag that appears at both file-level (MediaTags) and title-level
	// (MediaTitleTags). After PopulateSystemTagsCache, Count must equal 2.
	genreTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "genre"})
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	insertedSystem, err := mediaDB.InsertSystem(database.System{SystemID: nesSystem.ID, Name: "NES"})
	require.NoError(t, err)

	insertedTitle, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       "smb",
		Name:       "Super Mario Bros",
	})
	require.NoError(t, err)

	insertedMedia, err := mediaDB.InsertMedia(database.Media{
		SystemDBID:     insertedSystem.DBID,
		MediaTitleDBID: insertedTitle.DBID,
		Path:           filepath.Join("roms", "nes", "smb.nes"),
	})
	require.NoError(t, err)

	platformTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: genreTagType.DBID, Tag: "platform"})
	require.NoError(t, err)

	// File-level contribution (MediaTags): Count += 1
	_, err = mediaDB.InsertMediaTag(database.MediaTag{MediaDBID: insertedMedia.DBID, TagDBID: platformTag.DBID})
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Title-level contribution (MediaTitleTags): Count += 1
	_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
		"INSERT INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES (?, ?)",
		insertedTitle.DBID, platformTag.DBID)
	require.NoError(t, err)

	// Rebuild cache — this runs the UNION ALL + SUM query.
	err = mediaDB.PopulateSystemTagsCache(ctx)
	require.NoError(t, err)

	// Query the cache table directly to verify Count = 2 (1 MediaTags + 1 MediaTitleTags).
	var count int64
	err = mediaDB.UnsafeGetSQLDb().QueryRowContext(ctx,
		"SELECT Count FROM SystemTagsCache WHERE TagDBID = ?", platformTag.DBID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "Count must sum file-level and title-level contributions")
}
