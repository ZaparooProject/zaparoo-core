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
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var browseAnchorFileSorts = []string{"", "name-asc", "name-desc", "filename-asc", "filename-desc"}

func anchorListedPaths(
	t *testing.T, mediaDB *MediaDB, opts *database.BrowseFilesOptions,
) []string {
	t.Helper()
	opts.Limit = 10000
	rows, err := mediaDB.BrowseFiles(context.Background(), opts)
	require.NoError(t, err)
	paths := make([]string, len(rows))
	for i := range rows {
		paths[i] = rows[i].Path
	}
	return paths
}

// Ties in the sort value are where a keyset count is most likely to disagree
// with the listing, so every title here repeats.
func TestBrowseFileAnchorMatchesListingOverTies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	parentDir := browseTestDir("roms", "ties")

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	for title := range 15 {
		for copyIdx := range 3 {
			insertSystemMedia(t, mediaDB, system, fmt.Sprintf("Game %02d", title),
				fmt.Sprintf("%sGame %02d (copy %d).nes", parentDir, title, copyIdx))
		}
	}

	for _, sortOrder := range browseAnchorFileSorts {
		listed := anchorListedPaths(t, mediaDB, &database.BrowseFilesOptions{PathPrefix: parentDir, Sort: sortOrder})
		require.Len(t, listed, 45)
		for i, path := range listed {
			result, anchorErr := mediaDB.BrowseAnchorPosition(ctx, &database.BrowseAnchorOptions{
				PathPrefix: parentDir, Path: path, Sort: sortOrder,
			})
			require.NoError(t, anchorErr)
			assert.Equal(t, database.BrowseAnchorResult{Found: true, Position: i}, result, "%s %s", sortOrder, path)
		}
	}
}

// UNIQUE(SystemDBID, Path) lets one path repeat across systems. Without a system
// filter the path stands for its first row in listing order.
func TestBrowseFileAnchorPathInTwoSystemsIsFirstInListingOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	parentDir := browseTestDir("roms", "both")
	shared := parentDir + "Shared.rom"

	nes, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	snes, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "SNES", Name: "SNES"})
	require.NoError(t, err)
	insertSystemMedia(t, mediaDB, nes, "Aardvark", parentDir+"Aardvark.rom")
	insertSystemMedia(t, mediaDB, nes, "Middle", shared)
	insertSystemMedia(t, mediaDB, snes, "Zebra", shared)
	insertSystemMedia(t, mediaDB, snes, "Yak", parentDir+"Yak.rom")

	for _, tc := range []struct {
		sortOrder string
		position  int
	}{
		{"name-asc", 1},
		{"name-desc", 0},
	} {
		listed := anchorListedPaths(t, mediaDB, &database.BrowseFilesOptions{PathPrefix: parentDir, Sort: tc.sortOrder})
		require.Len(t, listed, 4)
		require.Equal(t, shared, listed[tc.position], tc.sortOrder)
		result, anchorErr := mediaDB.BrowseAnchorPosition(ctx, &database.BrowseAnchorOptions{
			PathPrefix: parentDir, Path: shared, Sort: tc.sortOrder,
		})
		require.NoError(t, anchorErr)
		assert.Equal(t, database.BrowseAnchorResult{Found: true, Position: tc.position}, result, tc.sortOrder)
	}

	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)
	filtered, err := mediaDB.BrowseAnchorPosition(ctx, &database.BrowseAnchorOptions{
		PathPrefix: parentDir, Path: shared, Sort: "name-asc", Systems: []systemdefs.System{*snesSystem},
	})
	require.NoError(t, err)
	assert.Equal(t, database.BrowseAnchorResult{Found: true, Position: 1}, filtered,
		"filtered to SNES, the Yak row is the only one ahead of Zebra")
}

// Offset pages read the same rows as a cursor walk, including the prefix sort
// modes a folder of numbered files turns on, which have no equivalent in the
// merge.
func TestBrowseFilesOffsetAndAnchorFollowPrefixSort(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	parentDir := browseTestDir("roms", "ranked")

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	// Named so the rank order differs from the plain title order.
	for rank := 20; rank > 0; rank-- {
		name := fmt.Sprintf("%d. %c Game", rank, rune('a'+20-rank))
		insertSystemMedia(t, mediaDB, system, name, parentDir+name+".nes")
	}

	for _, tc := range []struct {
		sortOrder string
		mode      string
	}{
		{"name-asc", browseSortRankPrefixAsc},
		{"name-desc", browseSortRankPrefixDesc},
	} {
		page, pageErr := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
			PathPrefix: parentDir, Sort: tc.sortOrder, Limit: 1,
		})
		require.NoError(t, pageErr)
		require.Len(t, page, 1)
		require.Equal(t, tc.mode, page[0].SortMode, "the fixture must reach the prefix mode")

		listed := anchorListedPaths(t, mediaDB, &database.BrowseFilesOptions{PathPrefix: parentDir, Sort: tc.sortOrder})
		for i, path := range listed {
			result, anchorErr := mediaDB.BrowseAnchorPosition(ctx, &database.BrowseAnchorOptions{
				PathPrefix: parentDir, Path: path, Sort: tc.sortOrder,
			})
			require.NoError(t, anchorErr)
			assert.Equal(t, i, result.Position, path)
			assert.True(t, result.Found, path)

			rows, offsetErr := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
				PathPrefix: parentDir, Sort: tc.sortOrder, Limit: 2, Offset: i,
			})
			require.NoError(t, offsetErr)
			require.NotEmpty(t, rows)
			assert.Equal(t, path, rows[0].Path)
		}
	}
}

// Every route's survivors count toward a merged position, whichever route the
// anchor is on, with ties across routes broken the way the merge breaks them.
func TestBrowseOverlayAnchorMatchesTheMerge(t *testing.T) {
	t.Parallel()
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cached=%t", cached), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f, cleanup := setupMergeFixture(t, 3)
			defer cleanup()
			for i, root := range f.roots {
				for n := range 8 {
					// Tied titles in every route, distinct file names.
					name := fmt.Sprintf("Game %d", n)
					f.insert(name, fmt.Sprintf("%s%s r%d.rom", root, name, i))
				}
				f.insert("Shadowed", root+"Shadowed.rom")
				f.insert("Folder Game", root+"Folder/Folder Game.rom")
			}
			f.commit(t, cached)
			systems := []systemdefs.System{f.system}

			for _, sortOrder := range browseAnchorFileSorts {
				listed := anchorListedPaths(t, f.mediaDB, &database.BrowseFilesOptions{
					Overlay: f.overlay(), Systems: systems, Sort: sortOrder,
				})
				require.Len(t, listed, 25, "the shared name survives once")
				for i, path := range listed {
					result, err := f.mediaDB.BrowseAnchorPosition(ctx, &database.BrowseAnchorOptions{
						Overlay: f.overlay(), Path: path, Sort: sortOrder, Systems: systems, ListDirectories: true,
					})
					require.NoError(t, err)
					assert.Equal(t, database.BrowseAnchorResult{Found: true, Position: i}, result,
						"%s %s", sortOrder, path)
				}

				for i := range listed {
					rows, err := f.mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
						Overlay: f.overlay(), Systems: systems, Sort: sortOrder, Limit: 4, Offset: i,
					})
					require.NoError(t, err)
					got := make([]string, len(rows))
					for j := range rows {
						got[j] = rows[j].Path
					}
					assert.Equal(t, listed[i:min(i+4, len(listed))], got, "%s offset %d", sortOrder, i)
				}
			}

			for _, root := range f.roots {
				result, err := f.mediaDB.BrowseAnchorPosition(ctx, &database.BrowseAnchorOptions{
					Overlay:         f.overlay(),
					Path:            root + "Folder/Folder Game.rom",
					Systems:         systems,
					ListDirectories: true,
				})
				require.NoError(t, err)
				assert.Equal(t, database.BrowseAnchorResult{Found: true, IsDirectory: true}, result, root)

				filesOnly, err := f.mediaDB.BrowseAnchorPosition(ctx, &database.BrowseAnchorOptions{
					Overlay: f.overlay(), Path: root + "Folder/Folder Game.rom", Systems: systems,
				})
				require.NoError(t, err)
				assert.False(t, filesOnly.IsDirectory, "a listing without folders has none to land on")
			}

			outside, err := f.mediaDB.BrowseAnchorPosition(ctx, &database.BrowseAnchorOptions{
				Overlay: f.overlay(), Path: browseTestDir("elsewhere") + "Game 1 r0.rom", Systems: systems,
			})
			require.NoError(t, err)
			assert.False(t, outside.Found)
		})
	}
}

func TestBrowseDirectoriesOffsetSlicesTheListing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	parentDir := browseTestDir("roms", "folders")

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	for i := range 6 {
		name := fmt.Sprintf("Folder %d", i)
		insertSystemMedia(t, mediaDB, system, name, parentDir+name+"/"+name+".nes")
	}

	all, err := mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{PathPrefix: parentDir})
	require.NoError(t, err)
	require.Len(t, all, 6)
	for offset := 1; offset <= 8; offset++ {
		rows, offsetErr := mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
			PathPrefix: parentDir, Offset: offset, Limit: 2,
		})
		require.NoError(t, offsetErr)
		start := min(offset, len(all))
		assert.Equal(t, all[start:min(start+2, len(all))], rows, "offset %d", offset)
	}
	for i := range all {
		result, anchorErr := mediaDB.BrowseAnchorPosition(ctx, &database.BrowseAnchorOptions{
			PathPrefix: parentDir, Path: parentDir + all[i].Name, ListDirectories: true,
		})
		require.NoError(t, anchorErr)
		assert.Equal(t, database.BrowseAnchorResult{Found: true, IsDirectory: true, Position: i}, result)

		flat, anchorErr := mediaDB.BrowseAnchorPosition(ctx, &database.BrowseAnchorOptions{
			PathPrefix: parentDir, Path: parentDir + all[i].Name,
		})
		require.NoError(t, anchorErr)
		assert.False(t, flat.Found, "a listing without directories has no folder to find")
	}
}

func TestBrowseAnchorBelow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path, prefix, rest string
		ok                 bool
	}{
		{"/a/b.rom", "/a/", "b.rom", true},
		{"/a/b/c.rom", "/a/", "b/c.rom", true},
		{"/a/b/", "/a/", "b/", true},
		{"/a/", "/a/", "", false},
		{"/a//", "/a/", "", false},
		{"/a", "/a/", "", false},
		{"/ab/c.rom", "/a/", "", false},
		{"steam://1/Name", "steam://", "1/Name", true},
	} {
		rest, ok := browseAnchorBelow(tc.path, tc.prefix)
		assert.Equal(t, tc.ok, ok, tc.path)
		assert.Equal(t, tc.rest, rest, tc.path)
	}
	assert.Equal(t, "b", browseAnchorDirName("b/c/d.rom"))
	assert.Equal(t, "b", browseAnchorDirName("b/"))
	assert.Equal(t, "b.rom", browseAnchorDirName("b.rom"))
}

// The anchor plan is what keeps a restore cheap: one row off the path index, then
// a range count that stays on the sort index, with no table read for the count.
func TestBrowseAnchorQueryPlans(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	parentDir := seedBrowsePlanTestDB(t, mediaDB, 2000)
	seedAnalysisLimitedStats(ctx, t, mediaDB)

	filesOpts := &database.BrowseFilesOptions{PathPrefix: parentDir}
	rowQuery, rowArgs := browseFileAnchorRowQuery(
		filesOpts, parentDir+"anything.rom", browseSortExpr(""), browseSortClause(""), browseTagPlan{},
	)
	rowPlan := browseQueryPlan(ctx, t, mediaDB, rowQuery, rowArgs)
	assert.Contains(t, rowPlan, "media_path_idx", rowPlan)
	assert.NotContains(t, rowPlan, "SCAN", rowPlan)

	countQuery, countArgs := browseFileAnchorCountQuery(
		filesOpts, "", browseAnchorRow{sortValue: "m", dbid: 1}, browseTagPlan{},
	)
	countPlan := browseQueryPlan(ctx, t, mediaDB, countQuery, countArgs)
	assert.Contains(t, countPlan, "COVERING INDEX idx_media_browse_sort", countPlan)
	assert.NotContains(t, countPlan, "TEMP B-TREE", countPlan)

	overlay := &database.BrowseAnchorOptions{Overlay: &database.BrowseOverlay{Sources: []database.BrowseSource{
		{PathPrefix: parentDir, IncludeDirs: true},
		{PathPrefix: browseTestDir("other"), IncludeDirs: true},
	}}}
	sortExpr, direction, _ := browseOverlaySort("")
	mergedRow, mergedRowArgs := browseOverlayFileAnchorRowQuery(overlay, 0, sortExpr, direction, browseTagPlan{})
	assert.Contains(t, browseQueryPlan(ctx, t, mediaDB, mergedRow, mergedRowArgs), "media_path_idx")
	mergedCount, mergedCountArgs := browseOverlayRouteAnchorCountQuery(
		overlay, 0, sortExpr, "<", browseAnchorRow{sortValue: "m", dbid: 1}, browseTagPlan{},
	)
	assert.Contains(t, browseQueryPlan(ctx, t, mediaDB, mergedCount, mergedCountArgs), "idx_media_browse_sort")
}

func TestBrowseOffsetKeepsListingOnSortIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	parentDir := seedBrowsePlanTestDB(t, mediaDB, 2000)
	seedAnalysisLimitedStats(ctx, t, mediaDB)

	query, args := browseFilesQuery(&database.BrowseFilesOptions{
		PathPrefix: parentDir, Limit: 5, Offset: 100,
	}, "", browseTagPlan{})
	plan := browseQueryPlan(ctx, t, mediaDB, query, args)
	assert.Contains(t, plan, "idx_media_browse_sort", plan)
	assert.NotContains(t, plan, "TEMP B-TREE", plan)
}

func TestBrowseListingCacheClearedByMediaChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	parentDir := browseTestDir("roms", "clearing")

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	insertSystemMedia(t, mediaDB, system, "Game", parentDir+"Game.nes")
	var mediaID int64
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx, `SELECT DBID FROM Media LIMIT 1`).Scan(&mediaID))

	cache := mediaDB.BrowseListingCache()
	fill := func() {
		key := database.BrowseListingKey{Path: parentDir}
		cache.StoreTotals(&key, cache.Begin(""), database.BrowseListingTotals{Files: 1})
		require.Equal(t, 1, cache.Len())
	}

	fill()
	require.NoError(t, mediaDB.BeginTransaction(false))
	require.NoError(t, mediaDB.CommitTransaction())
	assert.Zero(t, cache.Len(), "a committed indexing transaction")

	fill()
	require.NoError(t, mediaDB.PopulateBrowseCache(ctx))
	assert.Zero(t, cache.Len(), "a browse cache rebuild")

	fill()
	changed, err := mediaDB.SetMediaTagMembership(
		ctx, database.MediaTagRef{Type: "user", Tag: "hidden"}, []int64{mediaID},
	)
	require.NoError(t, err)
	require.True(t, changed)
	assert.Zero(t, cache.Len(), "a hidden row")

	fill()
	changed, err = mediaDB.ReplaceHiddenDirectories(ctx, []database.HiddenDirectory{
		{SystemID: "NES", Path: parentDir},
	})
	require.NoError(t, err)
	require.True(t, changed)
	assert.Zero(t, cache.Len(), "a hidden folder")
}
