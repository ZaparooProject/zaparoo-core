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

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
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
