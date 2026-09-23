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
package mediadb_test

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// vocabularyTestMedia stages and reconciles one file and returns its Media
// and MediaTitle DBIDs.
func vocabularyTestMedia(t *testing.T, mediaDB *mediadb.MediaDB) (mediaID, titleID int64) {
	t.Helper()
	stageVariantTestMedia(t, mediaDB, "NES", map[string][]database.ScanStagedTag{"game": nil})
	require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(context.Background(),
		"SELECT DBID, MediaTitleDBID FROM Media LIMIT 1").Scan(&mediaID, &titleID))
	return mediaID, titleID
}

// storedTags lists "type:value" for every tag linked to the media or title.
func storedTags(t *testing.T, mediaDB *mediadb.MediaDB, table, column string, id int64) []string {
	t.Helper()
	//nolint:gosec // table and column are test constants.
	rows, err := mediaDB.UnsafeGetSQLDb().QueryContext(context.Background(), `
		SELECT tt.Type || ':' || t.Tag FROM `+table+` l
		JOIN Tags t ON t.DBID = l.TagDBID JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE l.`+column+` = ? ORDER BY 1`, id)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	require.NoError(t, rows.Err())
	return out
}

func countRows(t *testing.T, mediaDB *mediadb.MediaDB, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(context.Background(), query, args...).Scan(&n))
	return n
}

// offVocabularyWrite mixes tags the vocabulary accepts with ones it refuses:
// a free-form genre, an unknown type, a non-canonical region and a label on a
// closed type.
func offVocabularyWrite() *database.ScrapeWrite {
	return &database.ScrapeWrite{
		Sentinel: database.TagInfo{Type: string(tags.ScraperType("test")), Tag: string(tags.TagScraperScraped)},
		TitleTags: []database.TagInfo{
			{Type: string(tags.TagTypeGenre), Tag: "shmup:v", Label: "Shoot'em Up / Vertical"},
			{Type: string(tags.TagTypeGenre), Tag: "shootem-up"},
			{Type: "gamefamily", Tag: "mario"},
			{Type: string(tags.TagTypeDeveloper), Tag: "t-and-e-soft", Label: "T&E Soft"},
		},
		MediaTags: []database.TagInfo{
			{Type: string(tags.TagTypeRegion), Tag: "usa"},
			{Type: string(tags.TagTypeRegion), Tag: "us"},
		},
	}
}

func requireOnlyAcceptedTagsStored(t *testing.T, mediaDB *mediadb.MediaDB, mediaID, titleID int64) {
	t.Helper()
	assert.Equal(t, []string{"developer:t-and-e-soft", "genre:shmup:v"},
		storedTags(t, mediaDB, "MediaTitleTags", "MediaTitleDBID", titleID))
	assert.Equal(t, []string{"region:us", "scraper.test:scraped"},
		storedTags(t, mediaDB, "MediaTags", "MediaDBID", mediaID))
	assert.Zero(t, countRows(t, mediaDB, "SELECT COUNT(*) FROM TagTypes WHERE Type = 'gamefamily'"),
		"an unknown type must not be created")
	assert.Zero(t, countRows(t, mediaDB, "SELECT COUNT(*) FROM Tags WHERE Tag IN ('shootem-up', 'usa')"),
		"a refused value must not be created")
	assert.Equal(t, 1, countRows(t, mediaDB,
		"SELECT COUNT(*) FROM Tags WHERE Tag = 't-and-e-soft' AND DisplayName = 'T&E Soft'"),
		"a free-text tag keeps its label")
	assert.Zero(t, countRows(t, mediaDB,
		"SELECT COUNT(*) FROM Tags WHERE Tag = 'shmup:v' AND DisplayName != ''"),
		"a closed tag is shared by every title, so one source's label must not name it")
}

// TestApplyScrapeResultKeepsOnlyVocabularyTags is the write-path guard: a
// scraper that emits a value the vocabulary refuses loses that tag, not the
// write, and never creates the type or value.
func TestApplyScrapeResultKeepsOnlyVocabularyTags(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	mediaID, titleID := vocabularyTestMedia(t, mediaDB)

	require.NoError(t, mediaDB.ApplyScrapeResult(context.Background(), mediaID, titleID, offVocabularyWrite()))
	requireOnlyAcceptedTagsStored(t, mediaDB, mediaID, titleID)
}

func TestApplyScrapeResultsKeepsOnlyVocabularyTags(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	mediaID, titleID := vocabularyTestMedia(t, mediaDB)
	write := offVocabularyWrite()
	before := len(write.TitleTags)

	require.NoError(t, mediaDB.ApplyScrapeResults(context.Background(), []database.ScrapeWriteTarget{
		{MediaDBID: mediaID, MediaTitleDBID: titleID, Write: write},
	}))
	requireOnlyAcceptedTagsStored(t, mediaDB, mediaID, titleID)
	assert.Len(t, write.TitleTags, before, "the caller's write must not be modified")
}

func TestApplyScrapeResultFillMissingKeepsOnlyVocabularyTags(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	mediaID, titleID := vocabularyTestMedia(t, mediaDB)
	write := offVocabularyWrite()
	write.FillMissing = true

	require.NoError(t, mediaDB.ApplyScrapeResult(context.Background(), mediaID, titleID, write))
	requireOnlyAcceptedTagsStored(t, mediaDB, mediaID, titleID)
}

func TestUpsertMediaTitleTagsKeepsOnlyVocabularyTags(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	_, titleID := vocabularyTestMedia(t, mediaDB)

	require.NoError(t, mediaDB.UpsertMediaTitleTags(context.Background(), titleID, offVocabularyWrite().TitleTags))
	assert.Equal(t, []string{"developer:t-and-e-soft", "genre:shmup:v"},
		storedTags(t, mediaDB, "MediaTitleTags", "MediaTitleDBID", titleID))
}

func TestApplyScrapeResultRefusesAnInvalidSentinel(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	mediaID, titleID := vocabularyTestMedia(t, mediaDB)
	write := offVocabularyWrite()
	write.Sentinel = database.TagInfo{Type: "Not A Scraper", Tag: "scraped"}

	err := mediaDB.ApplyScrapeResult(context.Background(), mediaID, titleID, write)
	require.ErrorIs(t, err, tags.ErrUnknownTagType)
}

// TestReconcileStagedSystemKeepsOnlyVocabularyTags covers the scanner: a staged
// tag the vocabulary refuses never reaches Tags, including one for a type the
// scanner may create values for.
func TestReconcileStagedSystemKeepsOnlyVocabularyTags(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	stageVariantTestMedia(t, mediaDB, "NES", map[string][]database.ScanStagedTag{
		"game": {
			{Type: string(tags.TagTypeRegion), Value: "us"},
			{Type: string(tags.TagTypeRegion), Value: "usa"},
			{Type: string(tags.TagTypeRev), Value: "1-1"},
			{Type: string(tags.TagTypeRev), Value: "not a version"},
			{Type: string(tags.TagTypeDisc), Value: "0"},
			{Type: "unknown-type", Value: "x"},
		},
	})
	var mediaID int64
	require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(context.Background(),
		"SELECT DBID FROM Media LIMIT 1").Scan(&mediaID))
	assert.Equal(t, []string{"region:us", "rev:1-1"}, storedTags(t, mediaDB, "MediaTags", "MediaDBID", mediaID))
	assert.Zero(t, countRows(t, mediaDB, "SELECT COUNT(*) FROM TagTypes WHERE Type = 'unknown-type'"))
	assert.Zero(t, countRows(t, mediaDB, "SELECT COUNT(*) FROM Tags WHERE Tag IN ('not a version', '0000')"))
}

func TestInsertTagRefusesOffVocabularyValues(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(context.Background()))

	_, err := mediaDB.InsertTagType(database.TagType{Type: "gamefamily"})
	require.ErrorIs(t, err, tags.ErrUnknownTagType)

	var genreType int64
	require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(context.Background(),
		"SELECT DBID FROM TagTypes WHERE Type = 'genre'").Scan(&genreType))
	_, err = mediaDB.InsertTag(database.Tag{TypeDBID: genreType, Tag: "shootem-up"})
	require.ErrorIs(t, err, tags.ErrTagNotInVocabulary)
}

// TestSeedingPrunesTagsTheVocabularyNoLongerAccepts is the clean break on
// existing databases: when the vocabulary stamp changes, types Core no longer
// defines and values their type no longer accepts are removed together with
// every link to them, and accepted tags and their links stay.
func TestSeedingPrunesTagsTheVocabularyNoLongerAccepts(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	mediaID, titleID := vocabularyTestMedia(t, mediaDB)
	require.NoError(t, mediaDB.ApplyScrapeResult(ctx, mediaID, titleID, offVocabularyWrite()))

	// What an older build left behind: removed types and free-form values.
	conn := mediaDB.UnsafeGetSQLDb()
	_, err := conn.ExecContext(ctx, `
		INSERT INTO TagTypes (Type, IsExclusive) VALUES ('gamegenre', 0), ('gamefamily', 1);
		INSERT INTO Tags (TypeDBID, Tag) SELECT DBID, 'platform' FROM TagTypes WHERE Type = 'gamegenre';
		INSERT INTO Tags (TypeDBID, Tag) SELECT DBID, 'mario' FROM TagTypes WHERE Type = 'gamefamily';
		INSERT INTO Tags (TypeDBID, Tag)
			SELECT DBID, 'shootem-up-verticalshootem-up' FROM TagTypes WHERE Type = 'genre';
		INSERT INTO Tags (TypeDBID, Tag) SELECT DBID, 'usa' FROM TagTypes WHERE Type = 'region';
		UPDATE Tags SET DisplayName = 'Shoot''em Up / Vertical' WHERE Tag = 'shmup:v';`)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO MediaTitleTags (MediaTitleDBID, TagDBID)
		SELECT ?, t.DBID FROM Tags t JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE tt.Type IN ('gamegenre', 'gamefamily') OR t.Tag = 'shootem-up-verticalshootem-up'`, titleID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO MediaTags (MediaDBID, TagDBID)
		SELECT ?, t.DBID FROM Tags t WHERE t.Tag = 'usa'`, mediaID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, "UPDATE DBConfig SET Value = 'older-build' WHERE Name = ?",
		mediadb.DBConfigCanonicalTagVocabHash)
	require.NoError(t, err)

	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(ctx))

	requireOnlyAcceptedTagsStored(t, mediaDB, mediaID, titleID)
	assert.Zero(t, countRows(t, mediaDB,
		"SELECT COUNT(*) FROM TagTypes WHERE Type IN ('gamegenre', 'gamefamily')"))
	assert.Zero(t, countRows(t, mediaDB,
		"SELECT COUNT(*) FROM Tags WHERE Tag IN ('shootem-up-verticalshootem-up', 'usa', 'platform', 'mario')"))
	assert.Positive(t, countRows(t, mediaDB, "SELECT COUNT(*) FROM Tags WHERE Tag = 'world'"),
		"canonical values stay seeded")
}
