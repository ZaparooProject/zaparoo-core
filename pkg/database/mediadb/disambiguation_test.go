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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// disambTitleMedia describes one media row of a title for the test helper: its
// path and the (type, value) tags to attach.
type disambTitleMedia struct {
	tags map[string]string
	path string
}

// setupDisambTitle inserts a system, one title, and its media+tags, returning the
// system DBID, title DBID, and the inserted media DBIDs in input order.
func setupDisambTitle(
	t *testing.T, mediaDB *MediaDB, systemID, titleName string, media []disambTitleMedia,
) (systemDBID, titleDBID int64, mediaDBIDs []int64) {
	t.Helper()

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: systemID, Name: systemID})
	require.NoError(t, err)

	// Tag types must exist before the write transaction (mirrors other tests).
	typeDBIDs := make(map[string]int64)
	for i := range media {
		for tagType := range media[i].tags {
			if _, ok := typeDBIDs[tagType]; ok {
				continue
			}
			tt, ttErr := mediaDB.FindOrInsertTagType(database.TagType{Type: tagType})
			require.NoError(t, ttErr)
			typeDBIDs[tagType] = tt.DBID
		}
	}

	require.NoError(t, mediaDB.BeginTransaction(false))
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: system.DBID,
		Slug:       slugs.Slugify(slugs.MediaTypeGame, titleName),
		Name:       titleName,
	})
	require.NoError(t, err)

	mediaDBIDs = make([]int64, len(media))
	for i := range media {
		row, mErr := mediaDB.InsertMedia(database.Media{
			SystemDBID:     system.DBID,
			MediaTitleDBID: title.DBID,
			Path:           media[i].path,
			ParentDir:      ParentDirForMediaPath(media[i].path),
			SortName:       titleName,
		})
		require.NoError(t, mErr)
		mediaDBIDs[i] = row.DBID

		for tagType, value := range media[i].tags {
			tag, tErr := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: typeDBIDs[tagType], Tag: value})
			require.NoError(t, tErr)
			_, tErr = mediaDB.InsertMediaTag(database.MediaTag{MediaDBID: row.DBID, TagDBID: tag.DBID})
			require.NoError(t, tErr)
		}
	}
	require.NoError(t, mediaDB.CommitTransaction())

	return system.DBID, title.DBID, mediaDBIDs
}

func titleDisambiguationTypes(t *testing.T, mediaDB *MediaDB, titleDBID int64) string {
	t.Helper()
	var types string
	err := mediaDB.sql.QueryRowContext(
		context.Background(), `SELECT DisambiguationTypes FROM MediaTitles WHERE DBID = ?`, titleDBID,
	).Scan(&types)
	require.NoError(t, err)
	return types
}

func TestRecomputeSystemDisambiguation_DifferingTagDisambiguates(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "NES", "Sonic", []disambTitleMedia{
		{path: "/roms/nes/sonic-usa.nes", tags: map[string]string{"release": "USA"}},
		{path: "/roms/nes/sonic-eur.nes", tags: map[string]string{"release": "Europe"}},
	})

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Equal(t, "release", titleDisambiguationTypes(t, mediaDB, titleDBID))

	// The main browse/search query supplies DisambiguationTypes; simulate that.
	results := []database.SearchResultWithCursor{
		{MediaID: mediaIDs[0], Name: "Sonic", SystemID: "NES", DisambiguationTypes: "release"},
		{MediaID: mediaIDs[1], Name: "Sonic", SystemID: "NES", DisambiguationTypes: "release"},
	}
	require.NoError(t, attachZapScriptTags(ctx, mediaDB.sql, results))
	require.Len(t, results[0].ZapScriptTags, 1)
	assert.Equal(t, database.TagInfo{Type: "release", Tag: "USA"}, results[0].ZapScriptTags[0])
	require.Len(t, results[1].ZapScriptTags, 1)
	assert.Equal(t, database.TagInfo{Type: "release", Tag: "Europe"}, results[1].ZapScriptTags[0])
}

func TestRecomputeSystemDisambiguation_IdenticalTagsDoNotDisambiguate(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "NES", "Tetris", []disambTitleMedia{
		{path: "/roms/nes/tetris-a.nes", tags: map[string]string{"year": "1989"}},
		{path: "/roms/nes/tetris-b.nes", tags: map[string]string{"year": "1989"}},
	})

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Empty(t, titleDisambiguationTypes(t, mediaDB, titleDBID))

	results := []database.SearchResultWithCursor{
		{MediaID: mediaIDs[0], Name: "Tetris", SystemID: "NES"},
		{MediaID: mediaIDs[1], Name: "Tetris", SystemID: "NES"},
	}
	require.NoError(t, attachZapScriptTags(ctx, mediaDB.sql, results))
	assert.Empty(t, results[0].ZapScriptTags)
	assert.NotNil(t, results[0].ZapScriptTags, "ZapScriptTags should be a non-nil empty slice")
	assert.Empty(t, results[1].ZapScriptTags)
}

func TestRecomputeSystemDisambiguation_OnlyDifferingTypeSelected(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	// Same year, different players — only players disambiguates.
	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "Arcade", "Street Fighter", []disambTitleMedia{
		{path: "/roms/arcade/sf-2p.zip", tags: map[string]string{"year": "1992", "players": "2"}},
		{path: "/roms/arcade/sf-4p.zip", tags: map[string]string{"year": "1992", "players": "4"}},
	})

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Equal(t, "players", titleDisambiguationTypes(t, mediaDB, titleDBID))

	results := []database.SearchResultWithCursor{
		{MediaID: mediaIDs[0], Name: "Street Fighter", SystemID: "Arcade", DisambiguationTypes: "players"},
	}
	require.NoError(t, attachZapScriptTags(ctx, mediaDB.sql, results))
	require.Len(t, results[0].ZapScriptTags, 1)
	assert.Equal(t, database.TagInfo{Type: "players", Tag: "2"}, results[0].ZapScriptTags[0])
}

func TestRecomputeSystemDisambiguation_SingleMediaTitle(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, _ := setupDisambTitle(t, mediaDB, "NES", "Solo", []disambTitleMedia{
		{path: "/roms/nes/solo.nes", tags: map[string]string{"release": "USA"}},
	})

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Empty(t, titleDisambiguationTypes(t, mediaDB, titleDBID))
}

func TestRecomputeSystemDisambiguation_MissingMediaExcluded(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "NES", "Castlevania", []disambTitleMedia{
		{path: "/roms/nes/cv-usa.nes", tags: map[string]string{"release": "USA"}},
		{path: "/roms/nes/cv-eur.nes", tags: map[string]string{"release": "Europe"}},
	})

	// Mark the Europe variant missing: only one present variant remains, so the
	// title no longer disambiguates.
	_, err := mediaDB.sql.ExecContext(ctx, `UPDATE Media SET IsMissing = 1 WHERE DBID = ?`, mediaIDs[1])
	require.NoError(t, err)

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Empty(t, titleDisambiguationTypes(t, mediaDB, titleDBID))
}

// TestAttachZapScriptTags_TitleGlobalAcrossPages proves the page-independence fix:
// a single result on its own page still receives its disambiguating tag because
// the type set is stored per title, not derived from the current page.
func TestAttachZapScriptTags_TitleGlobalAcrossPages(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	parentDir := browseTestDir("roms", "nes")
	systemDBID, _, _ := setupDisambTitle(t, mediaDB, "NES", "Double Dragon", []disambTitleMedia{
		{path: browseTestPath("roms", "nes", "dd-usa.nes"), tags: map[string]string{"release": "USA"}},
		{path: browseTestPath("roms", "nes", "dd-jpn.nes"), tags: map[string]string{"release": "Japan"}},
	})
	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))

	// Limit 1 → only the first sibling is on this page; old page-scoped grouping
	// would have found no sibling and emitted no disambiguating tag.
	results, err := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{PathPrefix: parentDir, Limit: 1})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Len(t, results[0].ZapScriptTags, 1, "lone sibling on a page must still be disambiguated")
	assert.Equal(t, "release", results[0].ZapScriptTags[0].Type)
}

func TestGetZapScriptTagsBySystemAndPath_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	usaPath := "/roms/nes/contra-usa.nes"
	systemDBID, _, _ := setupDisambTitle(t, mediaDB, "NES", "Contra", []disambTitleMedia{
		{path: usaPath, tags: map[string]string{"release": "USA", "year": "1988"}},
		{path: "/roms/nes/contra-jpn.nes", tags: map[string]string{"release": "Japan", "year": "1988"}},
	})
	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))

	got, err := mediaDB.GetZapScriptTagsBySystemAndPath(ctx, "NES", usaPath)
	require.NoError(t, err)
	require.Len(t, got, 1, "only release differs across the two variants")
	assert.Equal(t, database.TagInfo{Type: "release", Tag: "USA"}, got[0])
}

// TestRecomputeSystemDisambiguation_RegionDisambiguates exercises a newly eligible tag
// type: region (us vs jp) now disambiguates same-named regional variants.
func TestRecomputeSystemDisambiguation_RegionDisambiguates(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "Genesis", "Sonic The Hedgehog", []disambTitleMedia{
		{path: "/roms/genesis/sonic-usa.md", tags: map[string]string{"region": "us"}},
		{path: "/roms/genesis/sonic-jpn.md", tags: map[string]string{"region": "jp"}},
	})

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Equal(t, "region", titleDisambiguationTypes(t, mediaDB, titleDBID))

	results := []database.SearchResultWithCursor{
		{MediaID: mediaIDs[0], Name: "Sonic The Hedgehog", SystemID: "Genesis", DisambiguationTypes: "region"},
	}
	require.NoError(t, attachZapScriptTags(ctx, mediaDB.sql, results))
	require.Len(t, results[0].ZapScriptTags, 1)
	assert.Equal(t, database.TagInfo{Type: "region", Tag: "us"}, results[0].ZapScriptTags[0])
}

// TestAttachZapScriptTags_OrdersByDisplayPriority verifies emitted tags come back in
// display-importance order (unfinished › region › rev), not alphabetical, so clients can
// render-and-truncate left to right.
func TestAttachZapScriptTags_OrdersByDisplayPriority(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "Genesis", "Streets of Rage", []disambTitleMedia{
		{path: "/roms/genesis/sor-a.md", tags: map[string]string{"region": "us", "unfinished": "beta", "rev": "a"}},
		{path: "/roms/genesis/sor-b.md", tags: map[string]string{"region": "jp", "unfinished": "proto", "rev": "b"}},
	})
	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	stored := titleDisambiguationTypes(t, mediaDB, titleDBID)

	results := []database.SearchResultWithCursor{
		{MediaID: mediaIDs[0], Name: "Streets of Rage", SystemID: "Genesis", DisambiguationTypes: stored},
	}
	require.NoError(t, attachZapScriptTags(ctx, mediaDB.sql, results))
	require.Len(t, results[0].ZapScriptTags, 3)
	gotOrder := []string{
		results[0].ZapScriptTags[0].Type,
		results[0].ZapScriptTags[1].Type,
		results[0].ZapScriptTags[2].Type,
	}
	assert.Equal(t, []string{"unfinished", "region", "rev"}, gotOrder)
}
