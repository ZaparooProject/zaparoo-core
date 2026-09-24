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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	scrapeTagSentinel = database.TagInfo{Type: "scraper.test", Tag: "scraped"}
	scrapedGenreTag   = database.TagInfo{Type: string(tags.TagTypeGenre), Tag: string(tags.TagGameGenreShootingFPS)}
	scrapedSearchTag  = database.TagInfo{Type: string(tags.TagTypeSearch), Tag: "franchise:castlevania"}
)

type scrapeTagCacheFixture struct {
	db      *MediaDB
	system  systemdefs.System
	mediaID int64
	titleID int64
}

// setupScrapeTagCacheFixture indexes one NES game carrying a single genre tag
// and builds every tag cache layer from it, as a finished index run does.
func setupScrapeTagCacheFixture(t *testing.T) *scrapeTagCacheFixture {
	t.Helper()
	mediaDB, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	sys, err := mediaDB.FindOrInsertSystem(database.System{SystemID: systemdefs.SystemNES, Name: "NES"})
	require.NoError(t, err)
	nesSystem, err := systemdefs.GetSystem(systemdefs.SystemNES)
	require.NoError(t, err)

	require.NoError(t, mediaDB.BeginTransaction(false))
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: sys.DBID,
		Slug:       slugs.Slugify(nesSystem.GetMediaType(), "Castlevania"),
		Name:       "Castlevania",
	})
	require.NoError(t, err)
	media, err := mediaDB.InsertMedia(database.Media{
		SystemDBID:     sys.DBID,
		MediaTitleDBID: title.DBID,
		Path:           filepath.ToSlash(filepath.Join("roms", "nes", "castlevania.nes")),
		ParentDir:      filepath.ToSlash(filepath.Join("roms", "nes")) + "/",
	})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	require.NoError(t, mediaDB.UpsertMediaTags(ctx, media.DBID, []database.TagInfo{
		{Type: string(tags.TagTypeGenre), Tag: string(tags.TagGameGenreShooting)},
	}))
	require.NoError(t, mediaDB.PopulateSystemTagsCache(ctx))
	require.NoError(t, mediaDB.RebuildTagCache())
	require.NoError(t, mediaDB.PersistTagCache())
	require.NotNil(t, mediaDB.inMemoryTagCache.Load(), "the in-memory cache must exist for this test to mean anything")

	return &scrapeTagCacheFixture{db: mediaDB, system: *nesSystem, mediaID: media.DBID, titleID: title.DBID}
}

func (f *scrapeTagCacheFixture) systemTags(t *testing.T) []database.TagInfo {
	t.Helper()
	got, err := f.db.GetSystemTagsCached(context.Background(), []systemdefs.System{f.system})
	require.NoError(t, err)
	return got
}

func (f *scrapeTagCacheFixture) allTags(t *testing.T) []database.TagInfo {
	t.Helper()
	got, err := f.db.GetAllUsedTags(context.Background())
	require.NoError(t, err)
	return got
}

// reloadFromDisk drops the in-memory cache and reinstalls the persisted
// snapshot, as a restart does.
func (f *scrapeTagCacheFixture) reloadFromDisk(t *testing.T) {
	t.Helper()
	f.db.inMemoryTagCache.Store(nil)
	loaded, err := f.db.LoadCachedTagCache()
	require.NoError(t, err)
	require.True(t, loaded, "the persisted tag cache must load")
}

func hasTagInfo(list []database.TagInfo, want database.TagInfo) bool {
	for _, tag := range list {
		if tag.Type == want.Type && tag.Tag == want.Tag {
			return true
		}
	}
	return false
}

// TestApplyScrapeResults_RefreshesTagCache covers tags a scrape writes being
// missing from media.tags until the next reindex. media.tags reads the
// in-memory tag cache and the SystemTagsCache table, which only indexing
// refreshed, and the persisted snapshot kept the stale set across restarts.
// Filtering on the scraped tags already worked because search reads the tag
// tables directly.
func TestApplyScrapeResults_RefreshesTagCache(t *testing.T) {
	f := setupScrapeTagCacheFixture(t)
	ctx := context.Background()

	require.NoError(t, f.db.ApplyScrapeResults(ctx, []database.ScrapeWriteTarget{{
		MediaDBID:      f.mediaID,
		MediaTitleDBID: f.titleID,
		Write: &database.ScrapeWrite{
			Sentinel:  scrapeTagSentinel,
			MediaTags: []database.TagInfo{scrapedGenreTag},
			TitleTags: []database.TagInfo{scrapedSearchTag},
		},
	}}))

	require.False(t, hasTagInfo(f.systemTags(t), scrapedGenreTag),
		"the cache is only refreshed once the scrape ends")

	require.NoError(t, f.db.RefreshScrapeTagCache(ctx))

	for _, want := range []database.TagInfo{scrapedGenreTag, scrapedSearchTag} {
		assert.True(t, hasTagInfo(f.systemTags(t), want), "system tags must include %s:%s", want.Type, want.Tag)
		assert.True(t, hasTagInfo(f.allTags(t), want), "all tags must include %s:%s", want.Type, want.Tag)
	}

	f.reloadFromDisk(t)
	assert.True(t, hasTagInfo(f.allTags(t), scrapedGenreTag), "the persisted snapshot must include scraped tags")
	assert.True(t, hasTagInfo(f.allTags(t), scrapedSearchTag), "the persisted snapshot must include scraped tags")
}

func TestApplyScrapeResult_RefreshesTagCache(t *testing.T) {
	f := setupScrapeTagCacheFixture(t)
	ctx := context.Background()

	require.NoError(t, f.db.ApplyScrapeResult(ctx, f.mediaID, f.titleID, &database.ScrapeWrite{
		Sentinel:  scrapeTagSentinel,
		TitleTags: []database.TagInfo{scrapedSearchTag},
	}))
	require.NoError(t, f.db.RefreshScrapeTagCache(ctx))

	assert.True(t, hasTagInfo(f.systemTags(t), scrapedSearchTag))
	f.reloadFromDisk(t)
	assert.True(t, hasTagInfo(f.systemTags(t), scrapedSearchTag))
}

// TestRefreshScrapeTagCache_DropsClearedRunMarkers covers the run markers a
// forced scrape writes: they are counted in SystemTagsCache like any tag, so
// clearing them at the end of the run must reach the cache too, or the
// in-memory cache and snapshot keep listing markers that no longer exist.
func TestRefreshScrapeTagCache_DropsClearedRunMarkers(t *testing.T) {
	f := setupScrapeTagCacheFixture(t)
	ctx := context.Background()
	marker := database.TagInfo{Type: string(tags.ScraperRunType("test")), Tag: "run-1"}

	require.NoError(t, f.db.UpsertMediaTags(ctx, f.mediaID, []database.TagInfo{marker}))
	f.db.MarkScrapeTagCacheStale([]string{f.system.ID})
	require.NoError(t, f.db.RefreshScrapeTagCache(ctx))
	require.True(t, hasTagInfo(f.systemTags(t), marker), "the marker must be cached for this test to mean anything")

	require.NoError(t, f.db.ClearScrapeRunMarkers(ctx, "test", "run-1"))
	require.NoError(t, f.db.RefreshScrapeTagCache(ctx))

	assert.False(t, hasTagInfo(f.systemTags(t), marker))
	assert.False(t, hasTagInfo(f.allTags(t), marker))
	f.reloadFromDisk(t)
	assert.False(t, hasTagInfo(f.allTags(t), marker), "the persisted snapshot must not keep a cleared marker")
}

func TestRefreshScrapeTagCache_RetriesAfterFailure(t *testing.T) {
	f := setupScrapeTagCacheFixture(t)

	require.NoError(t, f.db.ApplyScrapeResult(context.Background(), f.mediaID, f.titleID, &database.ScrapeWrite{
		Sentinel:  scrapeTagSentinel,
		MediaTags: []database.TagInfo{scrapedGenreTag},
	}))

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, f.db.RefreshScrapeTagCache(cancelled))
	assert.False(t, hasTagInfo(f.systemTags(t), scrapedGenreTag))

	require.NoError(t, f.db.RefreshScrapeTagCache(context.Background()))
	assert.True(t, hasTagInfo(f.systemTags(t), scrapedGenreTag), "a failed refresh must keep its systems for a retry")
}

// TestMarkScrapeTagCacheStale_AllSystems covers a scrape interrupted by a
// restart: its committed writes were never tracked, so an empty system list
// has to rebuild the cache for every system.
func TestMarkScrapeTagCacheStale_AllSystems(t *testing.T) {
	f := setupScrapeTagCacheFixture(t)
	ctx := context.Background()

	require.NoError(t, f.db.UpsertMediaTags(ctx, f.mediaID, []database.TagInfo{scrapedGenreTag}))
	require.NoError(t, f.db.RefreshScrapeTagCache(ctx))
	require.False(t, hasTagInfo(f.systemTags(t), scrapedGenreTag), "untracked writes are not refreshed")

	f.db.MarkScrapeTagCacheStale(nil)
	require.NoError(t, f.db.RefreshScrapeTagCache(ctx))
	assert.True(t, hasTagInfo(f.systemTags(t), scrapedGenreTag))
}
