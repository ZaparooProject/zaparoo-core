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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupScraperTestDB creates a minimal MediaDB with:
//   - Systems: "NES" (DBID=1)
//   - TagTypes: "scraper.test" (additive, DBID=1), "developer" (exclusive, DBID=2), "property" (additive, DBID=3)
//   - MediaTitles: "mario" (DBID=1)
//   - Media: "roms/mario.nes" (DBID=1) linked to MediaTitle 1
//   - Tags: "property:description" seeded (DBID=1)
func setupScraperTestDB(t *testing.T) (mediaDB *MediaDB, cleanup func()) {
	t.Helper()
	mediaDB, cleanup = setupTempMediaDB(t)
	ctx := context.Background()
	db := mediaDB.sql

	_, err := db.ExecContext(ctx, `
		INSERT INTO TagTypes (DBID, Type, IsExclusive) VALUES
		    (1, 'scraper.test', 0),
		    (2, 'developer',    1),
		    (3, 'property',     0);
		INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES
		    (1, 3, 'description'),
		    (2, 3, 'image-boxart');
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'NES', 'Nintendo');
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (1, 1, 'mario', 'Mario');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (1, 1, 1, 'roms/mario.nes');
	`)
	require.NoError(t, err)

	return mediaDB, cleanup
}

// --- FindMediaBySystemAndPath ---

func TestFindMediaBySystemAndPath_Found(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	m, err := mediaDB.FindMediaBySystemAndPath(context.Background(), 1, "roms/mario.nes")
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, int64(1), m.DBID)
	assert.Equal(t, int64(1), m.MediaTitleDBID)
	assert.Equal(t, "roms/mario.nes", m.Path)
}

func TestFindMediaBySystemAndPath_NotFound(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	m, err := mediaDB.FindMediaBySystemAndPath(context.Background(), 1, "roms/nonexistent.nes")
	require.NoError(t, err)
	assert.Nil(t, m)
}

func TestFindMediaBySystemAndPath_WrongSystem(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	m, err := mediaDB.FindMediaBySystemAndPath(context.Background(), 99, "roms/mario.nes")
	require.NoError(t, err)
	assert.Nil(t, m, "path exists but systemDBID doesn't match")
}

// --- FindMediaBySystemAndPathFold ---

func TestFindMediaBySystemAndPathFold_ExactMatch(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	m, err := mediaDB.FindMediaBySystemAndPathFold(context.Background(), 1, "roms/mario.nes")
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, int64(1), m.DBID)
}

func TestFindMediaBySystemAndPathFold_CaseInsensitive(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	// DB has "roms/mario.nes"; query with mixed-case path components as a
	// Windows scraper would produce when the system directory casing in the
	// resolver differs from the on-disk casing the indexer recorded.
	m, err := mediaDB.FindMediaBySystemAndPathFold(context.Background(), 1, "ROMS/Mario.NES")
	require.NoError(t, err)
	require.NotNil(t, m, "case-insensitive query must find the row")
	assert.Equal(t, int64(1), m.DBID)
}

func TestFindMediaBySystemAndPathFold_NotFound(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	m, err := mediaDB.FindMediaBySystemAndPathFold(context.Background(), 1, "roms/nonexistent.nes")
	require.NoError(t, err)
	assert.Nil(t, m)
}

func TestFindMediaBySystemAndPathFold_WrongSystem(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	m, err := mediaDB.FindMediaBySystemAndPathFold(context.Background(), 99, "roms/mario.nes")
	require.NoError(t, err)
	assert.Nil(t, m, "path exists but systemDBID doesn't match")
}

// --- MediaHasTag ---

func TestMediaHasTag_True(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Insert tag DBID=1 (property:description) on media DBID=1.
	_, err := mediaDB.sql.ExecContext(ctx,
		"INSERT INTO MediaTags (MediaDBID, TagDBID) VALUES (1, 1)")
	require.NoError(t, err)

	// MediaHasTag splits on the first colon: "property" → TagTypes.Type, "description" → Tags.Tag.
	has, err := mediaDB.MediaHasTag(ctx, 1, "property:description")
	require.NoError(t, err)
	assert.True(t, has)
}

func TestMediaHasTag_True_Sentinel(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Write the sentinel tag via UpsertMediaTags (Type="scraper.test", Tag="scraped").
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{
		{Type: "scraper.test", Tag: "scraped"},
	}))

	// MediaHasTag must find it using the "type:value" combined string.
	has, err := mediaDB.MediaHasTag(ctx, 1, "scraper.test:scraped")
	require.NoError(t, err)
	assert.True(t, has)
}

func TestMediaHasTag_False(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	has, err := mediaDB.MediaHasTag(context.Background(), 1, "scraper.test:scraped")
	require.NoError(t, err)
	assert.False(t, has)
}

// --- UpsertMediaTags ---

func TestUpsertMediaTags_AdditiveType_AccumulatesTags(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// "scraper.test" is additive (IsExclusive=0).
	tags1 := []database.TagInfo{{Type: "scraper.test", Tag: "scraped"}}
	err := mediaDB.UpsertMediaTags(ctx, 1, tags1)
	require.NoError(t, err)

	// Insert a second different tag of the same type.
	tags2 := []database.TagInfo{{Type: "scraper.test", Tag: "extra"}}
	err = mediaDB.UpsertMediaTags(ctx, 1, tags2)
	require.NoError(t, err)

	var count int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaTags WHERE MediaDBID = 1").Scan(&count))
	assert.Equal(t, 2, count, "additive type should keep both tags")
}

func TestUpsertMediaTags_ExclusiveType_ReplacesExisting(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// "developer" is exclusive (IsExclusive=1).
	err := mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{{Type: "developer", Tag: "nintendo"}})
	require.NoError(t, err)

	err = mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{{Type: "developer", Tag: "konami"}})
	require.NoError(t, err)

	// Only "konami" should remain.
	var count int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MediaTags mt
		 JOIN Tags t ON mt.TagDBID = t.DBID
		 JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		 WHERE tt.Type = 'developer' AND mt.MediaDBID = 1`).Scan(&count))
	assert.Equal(t, 1, count, "exclusive type should have exactly one tag")

	var tagVal string
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		`SELECT t.Tag FROM MediaTags mt
		 JOIN Tags t ON mt.TagDBID = t.DBID
		 JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		 WHERE tt.Type = 'developer' AND mt.MediaDBID = 1`).Scan(&tagVal))
	assert.Equal(t, tags.PadTagValue("konami"), tagVal)
}

func TestUpsertMediaTags_Idempotent(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	ti := []database.TagInfo{{Type: "scraper.test", Tag: "scraped"}}
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, ti))
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, ti)) // insert same tag again

	var count int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaTags WHERE MediaDBID = 1").Scan(&count))
	assert.Equal(t, 1, count, "duplicate additive insert should be idempotent")
}

// --- UpsertMediaTitleTags ---

func TestUpsertMediaTitleTags_ExclusiveType_Replaces(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, mediaDB.UpsertMediaTitleTags(ctx, 1, []database.TagInfo{{Type: "developer", Tag: "nintendo"}}))
	require.NoError(t, mediaDB.UpsertMediaTitleTags(ctx, 1, []database.TagInfo{{Type: "developer", Tag: "sega"}}))

	var count int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MediaTitleTags mtt
		 JOIN Tags t ON mtt.TagDBID = t.DBID
		 JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		 WHERE tt.Type = 'developer' AND mtt.MediaTitleDBID = 1`).Scan(&count))
	assert.Equal(t, 1, count, "exclusive type should replace old value")
}

// --- UpsertMediaTitleProperties ---

func TestUpsertMediaTitleProperties_Insert(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	props := []database.MediaProperty{
		{TypeTag: "property:description", Text: "A plumber's adventure.", ContentType: "text/plain"},
	}
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, props))

	var text, ct string
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		"SELECT Text, ContentType FROM MediaTitleProperties WHERE MediaTitleDBID = 1").Scan(&text, &ct))
	assert.Equal(t, "A plumber's adventure.", text)
	assert.Equal(t, "text/plain", ct)
}

func TestUpsertMediaTitleProperties_Update(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	props1 := []database.MediaProperty{
		{TypeTag: "property:description", Text: "First version.", ContentType: "text/plain"},
	}
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, props1))

	props2 := []database.MediaProperty{
		{TypeTag: "property:description", Text: "Updated version.", ContentType: "text/plain"},
	}
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, props2))

	var text string
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		"SELECT Text FROM MediaTitleProperties WHERE MediaTitleDBID = 1").Scan(&text))
	assert.Equal(t, "Updated version.", text, "second upsert should update existing row")

	var count int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaTitleProperties WHERE MediaTitleDBID = 1").Scan(&count))
	assert.Equal(t, 1, count, "upsert must not create duplicate rows")
}

func TestUpsertMediaTitleProperties_UnknownTypeTag_ReturnsError(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	props := []database.MediaProperty{
		{TypeTag: "property:nonexistent", Text: "nope", ContentType: "text/plain"},
	}
	err := mediaDB.UpsertMediaTitleProperties(context.Background(), 1, props)
	assert.Error(t, err, "unknown type tag should return an error")
}

// --- UpsertMediaProperties ---

func TestUpsertMediaProperties_Insert(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	props := []database.MediaProperty{
		{TypeTag: "property:image-boxart", Text: "/roms/nes/mario-box.png", ContentType: "image/png"},
	}
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, 1, props))

	var text, ct string
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		"SELECT Text, ContentType FROM MediaProperties WHERE MediaDBID = 1").Scan(&text, &ct))
	assert.Equal(t, "/roms/nes/mario-box.png", text)
	assert.Equal(t, "image/png", ct)
}

// --- GetMediaTitleProperties / GetMediaProperties ---

func TestGetMediaTitleProperties_Empty(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	props, err := mediaDB.GetMediaTitleProperties(context.Background(), 1)
	require.NoError(t, err)
	assert.Empty(t, props)
}

func TestGetMediaTitleProperties_RoundTrip(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	in := []database.MediaProperty{
		{TypeTag: "property:description", Text: "Hello world.", ContentType: "text/plain"},
		{TypeTag: "property:image-boxart", Text: "/art/mario.png", ContentType: "image/png"},
	}
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, in))

	got, err := mediaDB.GetMediaTitleProperties(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 2)
	// Fix 1: TypeTag must be populated from the JOIN, not left as "".
	for _, p := range got {
		assert.NotEmpty(t, p.TypeTag, "TypeTag must be populated (Fix 1)")
	}
}

func TestGetMediaProperties_RoundTrip(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	in := []database.MediaProperty{
		{TypeTag: "property:image-boxart", Text: "/art/mario.png", ContentType: "image/png"},
	}
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, 1, in))

	got, err := mediaDB.GetMediaProperties(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "/art/mario.png", got[0].Text)
	assert.Equal(t, "image/png", got[0].ContentType)
}

// --- FindMediaTitlesWithoutSentinel ---

func TestFindMediaTitlesWithoutSentinel_AllUnseen(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	titles, err := mediaDB.FindMediaTitlesWithoutSentinel(context.Background(), 1, "scraper.test:scraped")
	require.NoError(t, err)
	assert.Len(t, titles, 1, "mario title has no sentinel → should be returned")
}

func TestFindMediaTitlesWithoutSentinel_AfterSentinelWritten(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Write the sentinel tag on media DBID=1.
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{
		{Type: "scraper.test", Tag: "scraped"},
	}))

	titles, err := mediaDB.FindMediaTitlesWithoutSentinel(ctx, 1, "scraper.test:scraped")
	require.NoError(t, err)
	assert.Empty(t, titles, "media has sentinel → title should be excluded")
}

func TestFindMediaTitlesWithoutSentinel_WrongSystem(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	titles, err := mediaDB.FindMediaTitlesWithoutSentinel(context.Background(), 99, "scraper.test:scraped")
	require.NoError(t, err)
	assert.Empty(t, titles, "system 99 has no titles")
}

// --- FindMediaTitleByDBID ---

func TestFindMediaTitleByDBID_Found(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	title, err := mediaDB.FindMediaTitleByDBID(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, title)
	assert.Equal(t, "Mario", title.Name)
	assert.Equal(t, "mario", title.Slug)
}

func TestFindMediaTitleByDBID_NotFound(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	title, err := mediaDB.FindMediaTitleByDBID(context.Background(), 999)
	require.NoError(t, err)
	assert.Nil(t, title)
}

// --- Fix 2: upsertTags auto-creates missing tag types ---

func TestUpsertMediaTags_AutoCreatesTagType(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// "scraper.gamelist.xml" is not pre-seeded in the test DB.
	err := mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{
		{Type: "scraper.gamelist.xml", Tag: "scraped"},
	})
	require.NoError(t, err, "upsertTags must auto-create missing tag type")

	// The TagTypes row must now exist.
	var count int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM TagTypes WHERE Type = 'scraper.gamelist.xml'`).Scan(&count))
	assert.Equal(t, 1, count, "auto-created TagTypes row must exist")

	// The sentinel tag must be reachable.
	has, err := mediaDB.MediaHasTag(ctx, 1, "scraper.gamelist.xml:scraped")
	require.NoError(t, err)
	assert.True(t, has, "sentinel tag must be retrievable after auto-creation")
}

// --- Fix 5: concurrent writes to the same tag must not error ---

func TestUpsertMediaTags_Concurrent(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	const goroutines = 5
	errs := make(chan error, goroutines)
	for range goroutines {
		go func() {
			errs <- mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{
				{Type: "scraper.test", Tag: "concurrent"},
			})
		}()
	}
	for range goroutines {
		require.NoError(t, <-errs, "concurrent tag write must not return an error")
	}

	// Exactly one Tags row and one MediaTags link should exist.
	var tagCount int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM Tags t
		 JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		 WHERE tt.Type = 'scraper.test' AND t.Tag LIKE '%concurrent%'`).Scan(&tagCount))
	assert.Equal(t, 1, tagCount, "concurrent writes must produce exactly one Tags row")
}
