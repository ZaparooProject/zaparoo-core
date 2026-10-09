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
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupScrapeBulkTestDB extends the scraper fixture with a title shared by two
// media rows and with fields already stored, so a batch meets every case the
// write policies distinguish.
func setupScrapeBulkTestDB(t *testing.T) (mediaDB *MediaDB, cleanup func()) {
	t.Helper()
	mediaDB, cleanup = setupScraperTestDB(t)
	_, err := mediaDB.sql.Load().ExecContext(context.Background(), `
		INSERT INTO TagTypes (DBID, Type, IsExclusive) VALUES (4, 'year', 1), (5, 'region', 0);
		INSERT INTO Tags (DBID, TypeDBID, Tag, DisplayName) VALUES
		    (3, 3, 'image-screenshot', ''),
		    (4, 3, 'image-titleshot', ''),
		    (5, 2, 'original', 'Original'),
		    (6, 5, 'us', '');
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
		    (2, 1, 'zelda', 'Zelda'), (3, 1, 'metroid', 'Metroid');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES
		    (2, 2, 1, ?), (3, 2, 1, ?), (4, 3, 1, ?);
		INSERT INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES (1, 5);
		INSERT INTO MediaProperties (MediaDBID, TypeTagDBID, Text) VALUES (2, 2, 'old.png');
		INSERT INTO MediaTitleProperties (MediaTitleDBID, TypeTagDBID, Text) VALUES (3, 1, 'Stored');
		INSERT INTO MediaTags (MediaDBID, TagDBID) VALUES (4, 6);
	`,
		filepath.ToSlash(filepath.Join("roms", "zelda (usa).nes")),
		filepath.ToSlash(filepath.Join("roms", "zelda (europe).nes")),
		filepath.ToSlash(filepath.Join("roms", "metroid.nes")),
	)
	require.NoError(t, err)
	return mediaDB, cleanup
}

// scrapeBulkTargets is a batch in which targets share a title, repeat a media
// row, offer fields that are already stored and introduce new tag values.
func scrapeBulkTargets(fillMissing bool) []database.ScrapeWriteTarget {
	sentinel := database.TagInfo{Type: "scraper.test", Tag: "scraped"}
	prop := func(kind, text string) database.MediaProperty {
		return database.MediaProperty{TypeTag: "property:" + kind, Text: text}
	}
	writes := []struct {
		write   database.ScrapeWrite
		mediaID int64
		titleID int64
	}{
		{mediaID: 1, titleID: 1, write: database.ScrapeWrite{
			MediaTags: []database.TagInfo{{Type: "region", Tag: "us"}, {Type: "region", Tag: "eu"}},
			TitleTags: []database.TagInfo{
				{Type: "developer", Tag: "replacement", Label: "Replacement"},
				{Type: "genre", Tag: "puzzle"},
				{Type: "year", Tag: "1991"},
			},
			MediaProps: []database.MediaProperty{prop("image-boxart", "a.png"), prop("image-screenshot", "b.png")},
		}},
		{mediaID: 2, titleID: 2, write: database.ScrapeWrite{
			TitleTags:  []database.TagInfo{{Type: "developer", Tag: "nintendo", Label: "Nintendo"}},
			TitleProps: []database.MediaProperty{prop("description", "first")},
			MediaProps: []database.MediaProperty{prop("image-boxart", "new.png")},
		}},
		{mediaID: 3, titleID: 2, write: database.ScrapeWrite{
			MediaTags: []database.TagInfo{{Type: "region", Tag: "eu"}},
			TitleTags: []database.TagInfo{
				{Type: "developer", Tag: "other", Label: "Other"},
				{Type: "genre", Tag: "action"},
				{Type: "genre", Tag: "puzzle"},
			},
			TitleProps: []database.MediaProperty{prop("description", "second"), prop("image-titleshot", "t.png")},
		}},
		{mediaID: 1, titleID: 1, write: database.ScrapeWrite{
			TitleTags:  []database.TagInfo{{Type: "year", Tag: "1992"}},
			MediaProps: []database.MediaProperty{prop("image-screenshot", "c.png"), prop("image-titleshot", "d.png")},
		}},
		{mediaID: 4, titleID: 3, write: database.ScrapeWrite{
			MediaTags:  []database.TagInfo{{Type: "region", Tag: "us"}},
			TitleProps: []database.MediaProperty{prop("description", "Offered")},
		}},
	}
	targets := make([]database.ScrapeWriteTarget, 0, len(writes))
	for i := range writes {
		write := writes[i].write
		write.Sentinel = sentinel
		write.FillMissing = fillMissing
		targets = append(targets, database.ScrapeWriteTarget{
			MediaDBID: writes[i].mediaID, MediaTitleDBID: writes[i].titleID, Write: &write,
		})
	}
	return targets
}

// dumpScrapeTables renders every table a scrape write touches in natural
// keys, so two databases compare equal whatever DBIDs their new tags took.
func dumpScrapeTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	queries := map[string]string{
		"Tags": `SELECT tt.Type || ':' || t.Tag || ' label=' || t.DisplayName
			FROM Tags t JOIN TagTypes tt ON tt.DBID = t.TypeDBID`,
		"MediaTags": `SELECT x.MediaDBID || ' ' || tt.Type || ':' || t.Tag
			FROM MediaTags x JOIN Tags t ON t.DBID = x.TagDBID JOIN TagTypes tt ON tt.DBID = t.TypeDBID`,
		"MediaTitleTags": `SELECT x.MediaTitleDBID || ' ' || tt.Type || ':' || t.Tag
			FROM MediaTitleTags x JOIN Tags t ON t.DBID = x.TagDBID JOIN TagTypes tt ON tt.DBID = t.TypeDBID`,
		"MediaProperties": `SELECT p.MediaDBID || ' ' || t.Tag || '=' || p.Text || ' blob=' || COALESCE(p.BlobDBID, '')
			FROM MediaProperties p JOIN Tags t ON t.DBID = p.TypeTagDBID`,
		"MediaTitleProperties": `SELECT p.MediaTitleDBID || ' ' || t.Tag || '=' || p.Text || ' blob=' ||
			COALESCE(p.BlobDBID, '')
			FROM MediaTitleProperties p JOIN Tags t ON t.DBID = p.TypeTagDBID`,
		"DisambiguationTypes": `SELECT DBID || ' ' || DisambiguationTypes FROM MediaTitles`,
	}
	dump := make([]string, 0, 64)
	for table, query := range queries {
		dump = append(dump, dumpScrapeTable(t, db, table, query)...)
	}
	sort.Strings(dump)
	return dump
}

func dumpScrapeTable(t *testing.T, db *sql.DB, table, query string) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), query)
	require.NoError(t, err, table)
	defer func() { require.NoError(t, rows.Close(), table) }()
	var dump []string
	for rows.Next() {
		var row string
		require.NoError(t, rows.Scan(&row), table)
		dump = append(dump, table+": "+row)
	}
	require.NoError(t, rows.Err(), table)
	return dump
}

// applyScrapeTargetsOneByOne is the reference: each target written by the
// per-target path, in input order, in one transaction.
func applyScrapeTargetsOneByOne(t *testing.T, db *MediaDB, targets []database.ScrapeWriteTarget) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.sql.Load().BeginTx(ctx, nil)
	require.NoError(t, err)
	writeCtx := newScrapeWriteTxContext(tx)
	for i := range targets {
		require.NoError(t, applyScrapeWriteTarget(ctx, writeCtx, targets[i]))
	}
	require.NoError(t, tx.Commit())
	titleIDs := make([]int64, 0, len(targets))
	for i := range targets {
		titleIDs = append(titleIDs, targets[i].MediaTitleDBID)
	}
	require.NoError(t, db.RecomputeTitleDisambiguation(ctx, titleIDs))
}

func TestApplyScrapeResults_BulkStoresWhatPerTargetWritesStore(t *testing.T) {
	t.Parallel()
	for _, fillMissing := range []bool{true, false} {
		t.Run(fmt.Sprintf("fillMissing=%v", fillMissing), func(t *testing.T) {
			t.Parallel()
			reference, cleanupReference := setupScrapeBulkTestDB(t)
			defer cleanupReference()
			bulk, cleanupBulk := setupScrapeBulkTestDB(t)
			defer cleanupBulk()
			before := dumpScrapeTables(t, bulk.sql.Load())

			applyScrapeTargetsOneByOne(t, reference, scrapeBulkTargets(fillMissing))
			require.NoError(t, bulk.ApplyScrapeResults(t.Context(), scrapeBulkTargets(fillMissing)))

			want := dumpScrapeTables(t, reference.sql.Load())
			got := dumpScrapeTables(t, bulk.sql.Load())
			require.NotEqual(t, before, got, "the batch must have written something")
			assert.Equal(t, want, got)

			joined := strings.Join(got, "\n")
			if fillMissing {
				// Stored fields survive, and the first offer wins a shared title.
				assert.Contains(t, joined, "MediaTitleTags: 1 developer:original")
				assert.NotContains(t, joined, "developer:replacement")
				assert.Contains(t, joined, "MediaTitleTags: 1 year:1991")
				assert.Contains(t, joined, "MediaTitleTags: 2 developer:nintendo")
				assert.Contains(t, joined, "MediaProperties: 2 image-boxart=old.png")
				assert.Contains(t, joined, "MediaProperties: 1 image-screenshot=b.png")
				assert.Contains(t, joined, "MediaTitleProperties: 2 description=first")
				assert.Contains(t, joined, "MediaTitleProperties: 3 description=Stored")
			} else {
				// The last write wins, and an exclusive type is replaced.
				assert.Contains(t, joined, "MediaTitleTags: 1 developer:replacement")
				assert.Contains(t, joined, "MediaTitleTags: 1 year:1992")
				assert.Contains(t, joined, "MediaTitleTags: 2 developer:other")
				assert.Contains(t, joined, "MediaProperties: 2 image-boxart=new.png")
				assert.Contains(t, joined, "MediaProperties: 1 image-screenshot=c.png")
				assert.Contains(t, joined, "MediaTitleProperties: 2 description=second")
			}
			// Regions now differ between the two Zelda dumps.
			assert.Contains(t, joined, "DisambiguationTypes: 2 region")
		})
	}
}

func TestApplyScrapeResults_RepeatedFillMissingBatchChangesNothing(t *testing.T) {
	t.Parallel()
	db, cleanup := setupScrapeBulkTestDB(t)
	defer cleanup()
	require.NoError(t, db.ApplyScrapeResults(t.Context(), scrapeBulkTargets(true)))
	first := dumpScrapeTables(t, db.sql.Load())

	tx, err := db.sql.Load().BeginTx(t.Context(), nil)
	require.NoError(t, err)
	writeCtx := newScrapeWriteTxContext(tx)
	stats, err := applyScrapeWriteTargetsBulk(t.Context(), writeCtx, scrapeBulkTargets(true))
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	assert.Equal(t, first, dumpScrapeTables(t, db.sql.Load()))
	assert.True(t, stats.FillMissing)
	assert.False(t, stats.PerTarget)
	assert.Zero(t, stats.MediaTags.InsertRows+stats.TitleTags.InsertRows+stats.Sentinels.InsertRows)
	assert.Zero(t, stats.MediaProps.ChangedRows+stats.TitleProps.ChangedRows)
	assert.Empty(t, writeCtx.changedImageMediaIDs)
	assert.Empty(t, writeCtx.changedImageMediaTitleIDs)
	// One lookup per scope replaces a lookup per offered tag.
	assert.Equal(t, 1, stats.ExistenceQueries)
}

func TestApplyScrapeResults_TracksImagesAFillMissingBatchAdds(t *testing.T) {
	t.Parallel()
	db, cleanup := setupScrapeBulkTestDB(t)
	defer cleanup()
	tx, err := db.sql.Load().BeginTx(t.Context(), nil)
	require.NoError(t, err)
	writeCtx := newScrapeWriteTxContext(tx)
	_, err = applyScrapeWriteTargetsBulk(t.Context(), writeCtx, scrapeBulkTargets(true))
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	// Media 2 already had box art, so only media 1 gained images; title 2
	// gained a title screen.
	assert.Equal(t, map[int64]struct{}{1: {}}, writeCtx.changedImageMediaIDs)
	assert.Equal(t, map[int64]struct{}{2: {}}, writeCtx.changedImageMediaTitleIDs)
}

func TestScrapeDisambiguationTitles_OnlyMediaTagsOfDisambiguatingTypes(t *testing.T) {
	t.Parallel()
	sentinel := database.TagInfo{Type: "scraper.test", Tag: "scraped"}
	targets := []database.ScrapeWriteTarget{
		{MediaDBID: 1, MediaTitleDBID: 10, Write: &database.ScrapeWrite{
			Sentinel:   sentinel,
			MediaTags:  []database.TagInfo{{Type: "scraper-run.test", Tag: "run"}},
			TitleTags:  []database.TagInfo{{Type: "region", Tag: "us"}},
			MediaProps: []database.MediaProperty{{TypeTag: "property:image-boxart", Text: "a.png"}},
		}},
		{MediaDBID: 2, MediaTitleDBID: 20, Write: &database.ScrapeWrite{
			Sentinel: sentinel, MediaTags: []database.TagInfo{{Type: "region", Tag: "us"}},
		}},
		{MediaDBID: 3, MediaTitleDBID: 20, Write: &database.ScrapeWrite{
			Sentinel: sentinel, MediaTags: []database.TagInfo{{Type: "lang", Tag: "en"}},
		}},
	}
	assert.Equal(t, []int64{20}, scrapeDisambiguationTitles(targets))
}
