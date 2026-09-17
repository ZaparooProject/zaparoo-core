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
	"errors"
	"os"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateMediaTagsUpdatesOnlyAffectedCaches(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	insertNESGameWithTag(t, mediaDB)

	ctx := context.Background()
	mediaRows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, mediaRows, 1)
	mediaDBID := mediaRows[0].DBID
	require.NoError(t, mediaDB.PopulateSystemTagsCache(ctx))

	rawDB := mediaDB.UnsafeGetSQLDb()
	utilityTagsBefore, err := resolveUtilityTagDBIDs(ctx, rawDB)
	require.NoError(t, err)
	require.Empty(t, utilityTagsBefore)
	genreCountBefore := cachedTagCount(t, rawDB, "NES", string(tags.TagTypeGenre), "platform")
	require.Positive(t, genreCountBefore)

	_, err = rawDB.ExecContext(ctx, `
		INSERT INTO MediaCountCache (QueryHash, QueryParams, Count, MinDBID, MaxDBID, LastUpdated)
		VALUES ('stale', '{}', 1, 1, 1, 1)`)
	require.NoError(t, err)
	favoriteFilter := []zapscript.TagFilter{{
		Type:  string(tags.TagTypeUser),
		Value: string(tags.TagUserFavorite),
	}}
	require.NoError(t, mediaDB.SetCachedSlugResolution(
		ctx, "NES", "favorite-game", favoriteFilter, mediaDBID, "test",
	))
	require.NoError(t, mediaDB.SetCachedSlugResolution(
		ctx, "SNES", "other-game", favoriteFilter, mediaDBID, "test",
	))

	require.NoError(t, mediaDB.RebuildTagCache())
	require.NotNil(t, mediaDB.inMemoryTagCache.Load())
	require.NoError(t, mediaDB.PersistTagCache())
	_, err = os.Stat(mediaDB.tagCachePath())
	require.NoError(t, err)

	favoriteRef := database.MediaTagRef{
		Type: string(tags.TagTypeUser),
		Tag:  string(tags.TagUserFavorite),
	}
	customGenreRef := database.MediaTagRef{Type: string(tags.TagTypeGenre), Tag: "custom"}
	refs := []database.MediaTagRef{favoriteRef, customGenreRef}
	require.NoError(t, mediaDB.UpdateMediaTags(ctx, mediaDBID, nil, refs))
	assert.Equal(t, int64(1), cachedTagCount(t, rawDB, "NES", favoriteRef.Type, favoriteRef.Tag))
	assert.Equal(t, int64(1), cachedTagCount(t, rawDB, "NES", customGenreRef.Type, customGenreRef.Tag))
	assert.Equal(t, genreCountBefore, cachedTagCount(t, rawDB, "NES", string(tags.TagTypeGenre), "platform"))
	assertMediaHasTag(t, mediaDB, mediaDBID, favoriteRef, true)
	assertMediaHasTag(t, mediaDB, mediaDBID, customGenreRef, true)
	utilityTagsAfter, utilityErr := resolveUtilityTagDBIDs(ctx, rawDB)
	require.NoError(t, utilityErr)
	assert.Len(t, utilityTagsAfter, 1)
	assert.Nil(t, mediaDB.inMemoryTagCache.Load())
	_, err = os.Stat(mediaDB.tagCachePath())
	require.ErrorIs(t, err, os.ErrNotExist)

	var countCacheRows int
	require.NoError(t, rawDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM MediaCountCache").Scan(&countCacheRows))
	assert.Zero(t, countCacheRows)
	_, _, found := mediaDB.GetCachedSlugResolution(ctx, "NES", "favorite-game", favoriteFilter)
	assert.False(t, found)
	_, _, found = mediaDB.GetCachedSlugResolution(ctx, "SNES", "other-game", favoriteFilter)
	assert.True(t, found)

	// Removals run before additions, so overlapping updates remain present and
	// do not inflate aggregate counts.
	require.NoError(t, mediaDB.UpdateMediaTags(ctx, mediaDBID, refs, refs))
	assert.Equal(t, int64(1), cachedTagCount(t, rawDB, "NES", favoriteRef.Type, favoriteRef.Tag))
	assert.Equal(t, int64(1), cachedTagCount(t, rawDB, "NES", customGenreRef.Type, customGenreRef.Tag))

	require.NoError(t, mediaDB.UpdateMediaTags(ctx, mediaDBID, refs, nil))
	assert.Zero(t, cachedTagCount(t, rawDB, "NES", favoriteRef.Type, favoriteRef.Tag))
	assert.Zero(t, cachedTagCount(t, rawDB, "NES", customGenreRef.Type, customGenreRef.Tag))
	assert.Equal(t, genreCountBefore, cachedTagCount(t, rawDB, "NES", string(tags.TagTypeGenre), "platform"))
	assertMediaHasTag(t, mediaDB, mediaDBID, favoriteRef, false)
	assertMediaHasTag(t, mediaDB, mediaDBID, customGenreRef, false)
}

func TestUpdateMediaTagsReplacesExclusiveType(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	insertNESGameWithTag(t, mediaDB)

	ctx := context.Background()
	mediaRows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, mediaRows, 1)
	mediaDBID := mediaRows[0].DBID
	require.NoError(t, mediaDB.PopulateSystemTagsCache(ctx))

	const exclusiveType = "custom-exclusive"
	rawDB := mediaDB.UnsafeGetSQLDb()
	_, err = rawDB.ExecContext(ctx,
		"INSERT INTO TagTypes (Type, IsExclusive) VALUES (?, 1)", exclusiveType,
	)
	require.NoError(t, err)
	firstRef := database.MediaTagRef{Type: exclusiveType, Tag: "first"}
	secondRef := database.MediaTagRef{Type: exclusiveType, Tag: "second"}

	require.NoError(t, mediaDB.UpdateMediaTags(ctx, mediaDBID, nil, []database.MediaTagRef{firstRef}))
	require.NoError(t, mediaDB.UpdateMediaTags(ctx, mediaDBID, nil, []database.MediaTagRef{secondRef}))
	assertMediaHasTag(t, mediaDB, mediaDBID, firstRef, false)
	assertMediaHasTag(t, mediaDB, mediaDBID, secondRef, true)
	assert.Zero(t, cachedTagCount(t, rawDB, "NES", firstRef.Type, firstRef.Tag))
	assert.Equal(t, int64(1), cachedTagCount(t, rawDB, "NES", secondRef.Type, secondRef.Tag))
}

func TestUpdateMediaTagsRollsBackExclusiveReplacementFailure(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	insertNESGameWithTag(t, mediaDB)

	ctx := context.Background()
	mediaRows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, mediaRows, 1)
	mediaDBID := mediaRows[0].DBID
	require.NoError(t, mediaDB.PopulateSystemTagsCache(ctx))

	const exclusiveType = "failing-exclusive"
	rawDB := mediaDB.UnsafeGetSQLDb()
	_, err = rawDB.ExecContext(ctx,
		"INSERT INTO TagTypes (Type, IsExclusive) VALUES (?, 1)", exclusiveType,
	)
	require.NoError(t, err)
	firstRef := database.MediaTagRef{Type: exclusiveType, Tag: "first"}
	secondRef := database.MediaTagRef{Type: exclusiveType, Tag: "second"}
	require.NoError(t, mediaDB.UpdateMediaTags(ctx, mediaDBID, nil, []database.MediaTagRef{firstRef}))
	_, err = rawDB.ExecContext(ctx, `
		CREATE TRIGGER fail_exclusive_media_tag_delete
		BEFORE DELETE ON MediaTags
		BEGIN
			SELECT RAISE(ABORT, 'injected exclusive replacement failure');
		END`)
	require.NoError(t, err)

	err = mediaDB.UpdateMediaTags(ctx, mediaDBID, nil, []database.MediaTagRef{secondRef})
	require.ErrorContains(t, err, "injected exclusive replacement failure")
	assertMediaHasTag(t, mediaDB, mediaDBID, firstRef, true)
	assertMediaHasTag(t, mediaDB, mediaDBID, secondRef, false)
	assert.Equal(t, int64(1), cachedTagCount(t, rawDB, "NES", firstRef.Type, firstRef.Tag))
	assert.Zero(t, cachedTagCount(t, rawDB, "NES", secondRef.Type, secondRef.Tag))
}

func TestUpdateMediaTagsMissingRemovalDoesNotInvalidateCaches(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	insertNESGameWithTag(t, mediaDB)

	ctx := context.Background()
	mediaRows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, mediaRows, 1)
	require.NoError(t, mediaDB.PopulateSystemTagsCache(ctx))
	require.NoError(t, mediaDB.RebuildTagCache())
	initialCache := mediaDB.inMemoryTagCache.Load()
	require.NotNil(t, initialCache)

	rawDB := mediaDB.UnsafeGetSQLDb()
	_, err = rawDB.ExecContext(ctx, `
		INSERT INTO MediaCountCache (QueryHash, QueryParams, Count, MinDBID, MaxDBID, LastUpdated)
		VALUES ('keep', '{}', 1, 1, 1, 1)`)
	require.NoError(t, err)
	remove := []database.MediaTagRef{
		{Type: "missing-type", Tag: "missing"},
		{Type: string(tags.TagTypeGenre), Tag: "missing"},
	}

	require.NoError(t, mediaDB.UpdateMediaTags(ctx, mediaRows[0].DBID, remove, nil))
	assert.Same(t, initialCache, mediaDB.inMemoryTagCache.Load())
	var countCacheRows int
	require.NoError(t, rawDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM MediaCountCache").Scan(&countCacheRows))
	assert.Equal(t, 1, countCacheRows)
}

func TestUpdateMediaTagsDoesNotCreatePartialSystemCache(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	insertNESGameWithTag(t, mediaDB)

	ctx := context.Background()
	mediaRows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, mediaRows, 1)
	favoriteRef := database.MediaTagRef{
		Type: string(tags.TagTypeUser),
		Tag:  string(tags.TagUserFavorite),
	}

	require.NoError(t, mediaDB.UpdateMediaTags(ctx, mediaRows[0].DBID, nil, []database.MediaTagRef{favoriteRef}))
	var cacheRows int
	require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(
		ctx, "SELECT COUNT(*) FROM SystemTagsCache",
	).Scan(&cacheRows))
	assert.Zero(t, cacheRows)
	assertMediaHasTag(t, mediaDB, mediaRows[0].DBID, favoriteRef, true)
}

func TestUpdateMediaTagsRollsBackWhenCacheRefreshFails(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	insertNESGameWithTag(t, mediaDB)

	ctx := context.Background()
	mediaRows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, mediaRows, 1)
	require.NoError(t, mediaDB.PopulateSystemTagsCache(ctx))
	_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx, `
		CREATE TRIGGER fail_media_tag_cache_refresh
		BEFORE INSERT ON SystemTagsCache
		BEGIN
			SELECT RAISE(ABORT, 'injected cache refresh failure');
		END`)
	require.NoError(t, err)
	favoriteRef := database.MediaTagRef{
		Type: string(tags.TagTypeUser),
		Tag:  string(tags.TagUserFavorite),
	}

	err = mediaDB.UpdateMediaTags(ctx, mediaRows[0].DBID, nil, []database.MediaTagRef{favoriteRef})
	require.ErrorContains(t, err, "injected cache refresh failure")
	assertMediaHasTag(t, mediaDB, mediaRows[0].DBID, favoriteRef, false)
}

func TestUpdateMediaTagsRejectsInvalidTag(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	insertNESGameWithTag(t, mediaDB)

	mediaRows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, mediaRows, 1)
	invalidRef := database.MediaTagRef{Type: string(tags.TagTypeUser)}

	err = mediaDB.UpdateMediaTags(
		context.Background(), mediaRows[0].DBID, nil, []database.MediaTagRef{invalidRef},
	)
	require.ErrorContains(t, err, "media tag type and value are required")
}

func TestUpdateMediaTagsRejectsActiveTransaction(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	insertNESGameWithTag(t, mediaDB)

	mediaRows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, mediaRows, 1)
	require.NoError(t, mediaDB.BeginTransaction(false))
	favoriteRef := database.MediaTagRef{
		Type: string(tags.TagTypeUser),
		Tag:  string(tags.TagUserFavorite),
	}

	err = mediaDB.UpdateMediaTags(
		context.Background(), mediaRows[0].DBID, nil, []database.MediaTagRef{favoriteRef},
	)
	require.ErrorIs(t, err, ErrTransactionActive)
	require.NoError(t, mediaDB.RollbackTransaction())
}

func TestUpdateMediaTagsHonorsCanceledContext(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	insertNESGameWithTag(t, mediaDB)

	mediaRows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, mediaRows, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	favoriteRef := database.MediaTagRef{
		Type: string(tags.TagTypeUser),
		Tag:  string(tags.TagUserFavorite),
	}

	err = mediaDB.UpdateMediaTags(ctx, mediaRows[0].DBID, nil, []database.MediaTagRef{favoriteRef})
	require.ErrorIs(t, err, context.Canceled)
	assertMediaHasTag(t, mediaDB, mediaRows[0].DBID, favoriteRef, false)
}

func TestUpdateMediaTagsRejectsMissingMedia(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	favoriteRef := database.MediaTagRef{
		Type: string(tags.TagTypeUser),
		Tag:  string(tags.TagUserFavorite),
	}

	err := mediaDB.UpdateMediaTags(context.Background(), 999, nil, []database.MediaTagRef{favoriteRef})
	require.Error(t, err)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

// A batch spanning two systems writes every edit in one transaction and keeps
// each ready system tag cache in step.
func TestUpdateMediaTagsBatchAppliesAllEdits(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	insertTaggedGame(t, mediaDB, "NES", "Mega Man", "nes/megaman.nes", "genre", "action")
	insertTaggedGame(t, mediaDB, "NES", "Mega Man 2", "nes/megaman2.nes", "genre", "action")
	insertTaggedGame(t, mediaDB, "SNES", "Super Metroid", "snes/supermetroid.sfc", "genre", "action")

	ctx := context.Background()
	require.NoError(t, mediaDB.PopulateSystemTagsCache(ctx))
	rawDB := mediaDB.UnsafeGetSQLDb()
	idFor := func(systemID string) []int64 {
		rows, err := mediaDB.GetMediaBySystemID(systemID)
		require.NoError(t, err)
		ids := make([]int64, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.DBID)
		}
		return ids
	}
	nesIDs := idFor("NES")
	require.Len(t, nesIDs, 2)
	snesIDs := idFor("SNES")
	require.Len(t, snesIDs, 1)

	liked := database.MediaTagRef{Type: string(tags.TagTypeUser), Tag: string(tags.TagUserLiked)}
	later := database.MediaTagRef{Type: string(tags.TagTypeUser), Tag: string(tags.TagUserPlayLater)}
	require.NoError(t, mediaDB.UpdateMediaTagsBatch(ctx, []database.MediaTagUpdate{
		{MediaDBID: nesIDs[0], Add: []database.MediaTagRef{liked}},
		{MediaDBID: nesIDs[1], Add: []database.MediaTagRef{liked, later}},
		{MediaDBID: snesIDs[0], Add: []database.MediaTagRef{later}},
		{MediaDBID: snesIDs[0]},
	}))
	assertMediaHasTag(t, mediaDB, nesIDs[0], liked, true)
	assertMediaHasTag(t, mediaDB, nesIDs[1], later, true)
	assertMediaHasTag(t, mediaDB, snesIDs[0], later, true)
	assertMediaHasTag(t, mediaDB, snesIDs[0], liked, false)
	assert.Equal(t, int64(2), cachedTagCount(t, rawDB, "NES", liked.Type, liked.Tag))
	assert.Equal(t, int64(1), cachedTagCount(t, rawDB, "NES", later.Type, later.Tag))
	assert.Equal(t, int64(1), cachedTagCount(t, rawDB, "SNES", later.Type, later.Tag))
	assert.Zero(t, cachedTagCount(t, rawDB, "SNES", liked.Type, liked.Tag))

	require.NoError(t, mediaDB.UpdateMediaTagsBatch(ctx, nil), "an empty batch is a no-op")

	// One bad edit rolls back the whole batch.
	err := mediaDB.UpdateMediaTagsBatch(ctx, []database.MediaTagUpdate{
		{MediaDBID: nesIDs[0], Remove: []database.MediaTagRef{liked}},
		{MediaDBID: 999, Add: []database.MediaTagRef{liked}},
	})
	require.ErrorIs(t, err, sql.ErrNoRows)
	assertMediaHasTag(t, mediaDB, nesIDs[0], liked, true)
	assert.Equal(t, int64(2), cachedTagCount(t, rawDB, "NES", liked.Type, liked.Tag))
}

func cachedTagCount(t *testing.T, rawDB *sql.DB, systemID, tagType, tag string) int64 {
	t.Helper()
	var count int64
	err := rawDB.QueryRowContext(context.Background(), `
		SELECT COALESCE(SUM(stc.Count), 0)
		FROM SystemTagsCache stc
		JOIN Systems s ON s.DBID = stc.SystemDBID
		WHERE s.SystemID = ? AND stc.TagType = ? AND stc.Tag = ?`,
		systemID, tagType, tag,
	).Scan(&count)
	require.NoError(t, err)
	return count
}

func assertMediaHasTag(
	t *testing.T,
	mediaDB *MediaDB,
	mediaDBID int64,
	ref database.MediaTagRef,
	expected bool,
) {
	t.Helper()
	tagList, err := mediaDB.GetMediaTagsByMediaDBID(context.Background(), mediaDBID)
	require.NoError(t, err)
	found := false
	for _, tag := range tagList {
		if tag.Type == ref.Type && tag.Tag == ref.Tag {
			found = true
			break
		}
	}
	assert.Equal(t, expected, found)
}

var _ database.MediaDBI = (*MediaDB)(nil)

// SetMediaTagMembership makes exactly the listed media carry a tag in one
// step: additions, removals and an empty list that clears the tag, with the
// system tag cache kept in step and no write when nothing changes.
func TestSetMediaTagMembership(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	_, _, mediaIDs := setupDisambTitle(t, mediaDB, "NES", "Member", []disambTitleMedia{
		{path: browseTestPath("roms", "nes", "a.nes"), tags: map[string]string{"region": "us"}},
		{path: browseTestPath("roms", "nes", "b.nes"), tags: map[string]string{"region": "eu"}},
		{path: browseTestPath("roms", "nes", "c.nes"), tags: map[string]string{"region": "jp"}},
	})
	require.NoError(t, mediaDB.PopulateSystemTagsCache(ctx))
	rawDB := mediaDB.UnsafeGetSQLDb()
	deck := database.MediaTagRef{Type: string(tags.TagTypeUser), Tag: string(tags.DeckTag("0123456789ab"))}

	changed, err := mediaDB.SetMediaTagMembership(ctx, deck, nil)
	require.NoError(t, err)
	assert.False(t, changed, "clearing a tag that does not exist changes nothing")

	changed, err = mediaDB.SetMediaTagMembership(ctx, deck, []int64{mediaIDs[0], mediaIDs[1], mediaIDs[0]})
	require.NoError(t, err)
	assert.True(t, changed)
	assertMediaHasTag(t, mediaDB, mediaIDs[0], deck, true)
	assertMediaHasTag(t, mediaDB, mediaIDs[1], deck, true)
	assertMediaHasTag(t, mediaDB, mediaIDs[2], deck, false)
	assert.Equal(t, int64(2), cachedTagCount(t, rawDB, "NES", deck.Type, deck.Tag))

	changed, err = mediaDB.SetMediaTagMembership(ctx, deck, []int64{mediaIDs[1], mediaIDs[0]})
	require.NoError(t, err)
	assert.False(t, changed, "the same membership in another order is not a change")

	revisionBefore := projectionRevision(t, rawDB)
	changed, err = mediaDB.SetMediaTagMembership(ctx, deck, []int64{mediaIDs[1], mediaIDs[2]})
	require.NoError(t, err)
	assert.True(t, changed)
	assertMediaHasTag(t, mediaDB, mediaIDs[0], deck, false)
	assertMediaHasTag(t, mediaDB, mediaIDs[1], deck, true)
	assertMediaHasTag(t, mediaDB, mediaIDs[2], deck, true)
	assert.Equal(t, int64(2), cachedTagCount(t, rawDB, "NES", deck.Type, deck.Tag))
	assert.NotEqual(t, revisionBefore, projectionRevision(t, rawDB), "a change invalidates browse cursors")

	changed, err = mediaDB.SetMediaTagMembership(ctx, deck, nil)
	require.NoError(t, err)
	assert.True(t, changed)
	for _, id := range mediaIDs {
		assertMediaHasTag(t, mediaDB, id, deck, false)
	}
	assert.Zero(t, cachedTagCount(t, rawDB, "NES", deck.Type, deck.Tag))

	nes, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	filtered, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
		Systems: []systemdefs.System{*nes},
		Tags:    []zapscript.TagFilter{{Type: deck.Type, Value: deck.Tag, Operator: zapscript.TagOperatorAND}},
		Limit:   10,
	})
	require.NoError(t, err)
	assert.Empty(t, filtered)
}

func projectionRevision(t *testing.T, rawDB *sql.DB) string {
	t.Helper()
	var value string
	err := rawDB.QueryRowContext(context.Background(),
		`SELECT Value FROM DBConfig WHERE Name = ?`, database.DeviceStateKeyMediaPreferencesRevision,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	require.NoError(t, err)
	return value
}

// ListMediaTagValues lists one type's tag values under a prefix that some
// file carries, missing files included, and nothing else.
func TestListMediaTagValues(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	_, _, mediaIDs := setupDisambTitle(t, mediaDB, "NES", "Listed", []disambTitleMedia{
		{path: browseTestPath("roms", "nes", "a.nes"), tags: map[string]string{"region": "us"}},
		{path: browseTestPath("roms", "nes", "b.nes"), tags: map[string]string{"region": "eu"}},
	})
	user := string(tags.TagTypeUser)
	for _, value := range []string{"deck:bbbbbbbbbbbb", "deck:aaaaaaaaaaaa", "deckx", "favorite"} {
		_, err := mediaDB.SetMediaTagMembership(ctx, database.MediaTagRef{Type: user, Tag: value}, mediaIDs[:1])
		require.NoError(t, err)
	}
	// A deck tag whose files all lost it is no longer listed, though its tag
	// row stays.
	cleared := database.MediaTagRef{Type: user, Tag: "deck:cccccccccccc"}
	_, err := mediaDB.SetMediaTagMembership(ctx, cleared, mediaIDs[1:])
	require.NoError(t, err)
	_, err = mediaDB.SetMediaTagMembership(ctx, cleared, nil)
	require.NoError(t, err)
	// A deck tag on a missing file still counts.
	onMissing := database.MediaTagRef{Type: user, Tag: "deck:dddddddddddd"}
	_, err = mediaDB.SetMediaTagMembership(ctx, onMissing, mediaIDs[1:])
	require.NoError(t, err)
	_, err = mediaDB.UnsafeGetSQLDb().ExecContext(ctx, `UPDATE Media SET IsMissing = 1 WHERE DBID = ?`, mediaIDs[1])
	require.NoError(t, err)

	values, err := mediaDB.ListMediaTagValues(ctx, user, tags.TagUserDeckPrefix)
	require.NoError(t, err)
	assert.Equal(t, []string{"deck:aaaaaaaaaaaa", "deck:bbbbbbbbbbbb", "deck:dddddddddddd"}, values)

	values, err = mediaDB.ListMediaTagValues(ctx, "region", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"eu", "us"}, values)

	values, err = mediaDB.ListMediaTagValues(ctx, "nosuchtype", "deck:")
	require.NoError(t, err)
	assert.Empty(t, values)
}
