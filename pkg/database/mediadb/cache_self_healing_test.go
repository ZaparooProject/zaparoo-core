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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetSystemTagsCached_AutoPopulate tests self-healing auto-population when cache is empty
func TestGetSystemTagsCached_AutoPopulate_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create tag type BEFORE transaction
	genreTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "genre"})
	require.NoError(t, err)

	// Setup test data
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

	// Create media with tags
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

	// Add a tag
	platformTag := database.Tag{
		TypeDBID: genreTagType.DBID,
		Tag:      "action:platformer",
	}
	insertedTag, err := mediaDB.FindOrInsertTag(platformTag)
	require.NoError(t, err)

	_, err = mediaDB.InsertMediaTag(database.MediaTag{
		MediaDBID: insertedMedia.DBID,
		TagDBID:   insertedTag.DBID,
	})
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Cache should be empty at this point - GetSystemTagsCached should auto-populate
	tags, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)
	assert.Len(t, tags, 1, "should have auto-populated cache and returned 1 tag")
	assert.Equal(t, "genre", tags[0].Type)
	assert.Equal(t, "action:platformer", tags[0].Tag)

	// Second call should use the populated cache
	tags2, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)
	assert.Len(t, tags2, 1, "should use cached value")
	assert.Equal(t, tags, tags2, "should return same result from cache")
}

// TestPopulateSystemTagsCacheForSystems tests selective cache population
func TestPopulateSystemTagsCacheForSystems_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create tag types BEFORE transaction
	genreTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "genre"})
	require.NoError(t, err)

	regionTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "region"})
	require.NoError(t, err)

	// Setup test data for multiple systems
	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	// Insert NES system and media
	nesSystemDB := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedNES, err := mediaDB.InsertSystem(nesSystemDB)
	require.NoError(t, err)

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

	// Add NES tags
	platformTag := database.Tag{
		TypeDBID: genreTagType.DBID,
		Tag:      "action:platformer",
	}
	insertedPlatformTag, err := mediaDB.FindOrInsertTag(platformTag)
	require.NoError(t, err)

	usaTag := database.Tag{
		TypeDBID: regionTagType.DBID,
		Tag:      "us",
	}
	insertedUSATag, err := mediaDB.FindOrInsertTag(usaTag)
	require.NoError(t, err)

	_, err = mediaDB.InsertMediaTag(database.MediaTag{
		MediaDBID: insertedNESMedia.DBID,
		TagDBID:   insertedPlatformTag.DBID,
	})
	require.NoError(t, err)

	_, err = mediaDB.InsertMediaTag(database.MediaTag{
		MediaDBID: insertedNESMedia.DBID,
		TagDBID:   insertedUSATag.DBID,
	})
	require.NoError(t, err)

	// Insert SNES system and media
	snesSystemDB := database.System{
		SystemID: snesSystem.ID,
		Name:     "SNES",
	}
	insertedSNES, err := mediaDB.InsertSystem(snesSystemDB)
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

	// Add SNES tags
	adventureTag := database.Tag{
		TypeDBID: genreTagType.DBID,
		Tag:      "adventure",
	}
	insertedAdventureTag, err := mediaDB.FindOrInsertTag(adventureTag)
	require.NoError(t, err)

	_, err = mediaDB.InsertMediaTag(database.MediaTag{
		MediaDBID: insertedSNESMedia.DBID,
		TagDBID:   insertedAdventureTag.DBID,
	})
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Populate cache for only NES system
	err = mediaDB.PopulateSystemTagsCacheForSystems(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)

	// Verify NES cache is populated
	nesTags, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)
	assert.Len(t, nesTags, 2, "NES should have 2 cached tags")

	// Verify tag values
	tagMap := make(map[string]string)
	for _, tag := range nesTags {
		tagMap[tag.Type] = tag.Tag
	}
	assert.Contains(t, tagMap, "genre")
	assert.Contains(t, tagMap, "region")

	// SNES cache should still be empty (will auto-populate on access)
	// but we can verify it has tags by querying directly
	snesTags, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{*snesSystem})
	require.NoError(t, err)
	assert.Len(t, snesTags, 1, "SNES should have 1 tag (auto-populated)")
}

// TestPopulateSystemTagsCacheForSystems_UpdateExisting tests re-population updates existing cache
func TestPopulateSystemTagsCacheForSystems_UpdateExisting_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create tag type BEFORE transaction
	genreTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "genre"})
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

	// Add initial tag
	platformTag := database.Tag{
		TypeDBID: genreTagType.DBID,
		Tag:      "action:platformer",
	}
	insertedTag, err := mediaDB.FindOrInsertTag(platformTag)
	require.NoError(t, err)

	_, err = mediaDB.InsertMediaTag(database.MediaTag{
		MediaDBID: insertedMedia.DBID,
		TagDBID:   insertedTag.DBID,
	})
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Populate initial cache
	err = mediaDB.PopulateSystemTagsCacheForSystems(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)

	// Verify initial cache
	tags1, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)
	assert.Len(t, tags1, 1, "should have 1 initial cached tag")

	// Add another tag (outside transaction for simplicity)
	actionTag := database.Tag{
		TypeDBID: genreTagType.DBID,
		Tag:      "action",
	}
	insertedActionTag, err := mediaDB.FindOrInsertTag(actionTag)
	require.NoError(t, err)

	_, err = mediaDB.InsertMediaTag(database.MediaTag{
		MediaDBID: insertedMedia.DBID,
		TagDBID:   insertedActionTag.DBID,
	})
	require.NoError(t, err)

	// Re-populate cache - should update with new tag
	err = mediaDB.PopulateSystemTagsCacheForSystems(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)

	// Verify updated cache
	tags2, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)
	assert.Len(t, tags2, 2, "should have 2 cached tags after update")

	// Verify both tags are present
	tagValues := make([]string, len(tags2))
	for i, tag := range tags2 {
		tagValues[i] = tag.Tag
	}
	assert.Contains(t, tagValues, "action:platformer")
	assert.Contains(t, tagValues, "action")
}

// TestPopulateSystemTagsCacheForSystems_NoInterference tests that populating one system doesn't affect others
func TestPopulateSystemTagsCacheForSystems_NoInterference_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create tag type BEFORE transaction
	genreTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "genre"})
	require.NoError(t, err)

	// Setup test data for multiple systems
	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	// Insert NES system and media
	nesSystemDB := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedNES, err := mediaDB.InsertSystem(nesSystemDB)
	require.NoError(t, err)

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

	// Add NES tag
	platformTag := database.Tag{
		TypeDBID: genreTagType.DBID,
		Tag:      "action:platformer",
	}
	insertedPlatformTag, err := mediaDB.FindOrInsertTag(platformTag)
	require.NoError(t, err)

	_, err = mediaDB.InsertMediaTag(database.MediaTag{
		MediaDBID: insertedNESMedia.DBID,
		TagDBID:   insertedPlatformTag.DBID,
	})
	require.NoError(t, err)

	// Insert SNES system and media
	snesSystemDB := database.System{
		SystemID: snesSystem.ID,
		Name:     "SNES",
	}
	insertedSNES, err := mediaDB.InsertSystem(snesSystemDB)
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

	// Add SNES tag
	adventureTag := database.Tag{
		TypeDBID: genreTagType.DBID,
		Tag:      "adventure",
	}
	insertedAdventureTag, err := mediaDB.FindOrInsertTag(adventureTag)
	require.NoError(t, err)

	_, err = mediaDB.InsertMediaTag(database.MediaTag{
		MediaDBID: insertedSNESMedia.DBID,
		TagDBID:   insertedAdventureTag.DBID,
	})
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Populate cache for both systems
	err = mediaDB.PopulateSystemTagsCacheForSystems(ctx, []systemdefs.System{*nesSystem, *snesSystem})
	require.NoError(t, err)

	// Verify NES cache
	nesTags, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)
	assert.Len(t, nesTags, 1)
	assert.Equal(t, "action:platformer", nesTags[0].Tag)

	// Verify SNES cache
	snesTags, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{*snesSystem})
	require.NoError(t, err)
	assert.Len(t, snesTags, 1)
	assert.Equal(t, "adventure", snesTags[0].Tag)

	// Re-populate only NES - SNES should remain unchanged
	err = mediaDB.PopulateSystemTagsCacheForSystems(ctx, []systemdefs.System{*nesSystem})
	require.NoError(t, err)

	// Verify SNES cache is still intact
	snesTags2, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{*snesSystem})
	require.NoError(t, err)
	assert.Len(t, snesTags2, 1)
	assert.Equal(t, "adventure", snesTags2[0].Tag)
}

// TestGetSystemTagsCached_EmptySystemList tests error handling for empty systems list
func TestGetSystemTagsCached_EmptySystemList(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Try to get tags with empty systems list
	_, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{})
	require.Error(t, err, "should error for empty systems list")
	assert.Contains(t, err.Error(), "no systems provided")
}

// TestPopulateSystemTagsCacheForSystems_EmptyList tests no-op for empty systems list
func TestPopulateSystemTagsCacheForSystems_EmptyList(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Should succeed as no-op
	err := mediaDB.PopulateSystemTagsCacheForSystems(ctx, []systemdefs.System{})
	require.NoError(t, err, "should succeed as no-op for empty systems list")
}

// TestPopulateSystemTagsCache_FailsFastDuringActiveTransaction covers the
// #1279 bug fix: this used to BeginTx unconditionally, and with the DSN's
// _txlock=immediate that grabs SQLite's single WAL writer lock at BEGIN,
// blocking for the full busy_timeout (and pinning a stale reader mark the
// whole time) against an in-progress indexing transaction. It must now fail
// immediately instead, matching applyMediaTagMutations's guard.
func TestPopulateSystemTagsCache_FailsFastDuringActiveTransaction(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, mediaDB.BeginTransaction(false))
	defer func() { _ = mediaDB.RollbackTransaction() }()

	err := mediaDB.PopulateSystemTagsCache(ctx)
	require.ErrorIs(t, err, ErrTransactionActive)

	err = mediaDB.PopulateSystemTagsCacheForSystems(ctx, []systemdefs.System{{ID: "NES"}})
	require.ErrorIs(t, err, ErrTransactionActive)
}

// A full invalidation (a vocabulary prune at startup, orphan cleanup) empties
// SystemTagsCache for every system. Self-healing a request for one system must
// not then build the in-memory cache, which answers for every system and is
// persisted, from a table holding only that one.
func TestGetSystemTagsCached_SelfHealKeepsOtherSystems_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	genreTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "genre"})
	require.NoError(t, err)

	require.NoError(t, mediaDB.BeginTransaction(false))
	systemsByID := make(map[string]systemdefs.System)
	for _, fixture := range []struct{ system, title, path, genre string }{
		{"NES", "Super Mario Bros", "/roms/mario.nes", "action:platformer"},
		{"SNES", "F-Zero", "/roms/fzero.sfc", "racing"},
	} {
		sysDef, sysErr := systemdefs.GetSystem(fixture.system)
		require.NoError(t, sysErr)
		systemsByID[fixture.system] = *sysDef
		insertedSystem, insertErr := mediaDB.InsertSystem(database.System{SystemID: sysDef.ID, Name: fixture.system})
		require.NoError(t, insertErr)
		insertedTitle, insertErr := mediaDB.InsertMediaTitle(&database.MediaTitle{
			SystemDBID: insertedSystem.DBID,
			Slug:       slugs.Slugify(slugs.MediaTypeGame, fixture.title),
			Name:       fixture.title,
		})
		require.NoError(t, insertErr)
		insertedMedia, insertErr := mediaDB.InsertMedia(database.Media{
			SystemDBID: insertedSystem.DBID, MediaTitleDBID: insertedTitle.DBID, Path: fixture.path,
		})
		require.NoError(t, insertErr)
		insertedTag, insertErr := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: genreTagType.DBID, Tag: fixture.genre})
		require.NoError(t, insertErr)
		_, insertErr = mediaDB.InsertMediaTag(database.MediaTag{
			MediaDBID: insertedMedia.DBID, TagDBID: insertedTag.DBID,
		})
		require.NoError(t, insertErr)
	}
	require.NoError(t, mediaDB.CommitTransaction())
	require.Nil(t, mediaDB.inMemoryTagCache.Load(), "the cache must start empty")

	nesTags, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{systemsByID["NES"]})
	require.NoError(t, err)
	require.Len(t, nesTags, 1)

	require.Eventually(t, func() bool { return mediaDB.inMemoryTagCache.Load() != nil },
		5*time.Second, 10*time.Millisecond, "self-healing rebuilds the in-memory cache")

	snesTags, err := mediaDB.GetSystemTagsCached(ctx, []systemdefs.System{systemsByID["SNES"]})
	require.NoError(t, err)
	require.Len(t, snesTags, 1, "a system the request did not name must keep its tags")
	assert.Equal(t, "racing", snesTags[0].Tag)

	allTags, err := mediaDB.GetAllUsedTags(ctx)
	require.NoError(t, err)
	assert.Len(t, allTags, 2, "the all-systems list must cover every system")
}
