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
	"path/filepath"
	"testing"

	zapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHiddenOverlayDoesNotShadowVisibleEntries(t *testing.T) {
	t.Parallel()
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cached=%t", cached), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f, cleanup := setupMergeFixture(t, 2)
			t.Cleanup(cleanup)
			hidden := []string{
				filepath.Join(f.roots[0], "Duplicate.nes"),
				filepath.Join(f.roots[0], "Shadow.nes", "Child.nes"),
				filepath.Join(f.roots[0], "Blocking.nes"),
			}
			visible := []string{
				filepath.Join(f.roots[1], "Duplicate.nes"),
				filepath.Join(f.roots[1], "Shadow.nes"),
			}
			for _, path := range append(append([]string{}, hidden...), visible...) {
				f.insert(filepath.Base(path), filepath.ToSlash(path))
			}
			f.insert("Child", filepath.ToSlash(filepath.Join(f.roots[1], "Blocking.nes", "Child.nes")))
			f.commit(t, cached)
			system, err := f.mediaDB.FindSystemBySystemID("NES")
			require.NoError(t, err)
			for _, path := range hidden {
				media, lookupErr := f.mediaDB.FindMediaBySystemAndPath(ctx, system.DBID, filepath.ToSlash(path))
				require.NoError(t, lookupErr)
				require.NoError(t, f.mediaDB.UpdateMediaTags(ctx, media.DBID, nil,
					[]database.MediaTagRef{{Type: "user", Tag: "hidden"}}))
			}
			systems := []systemdefs.System{f.system}
			files, err := f.mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
				Overlay: f.overlay(), Systems: systems, ExcludeHidden: true, Limit: 10,
			})
			require.NoError(t, err)
			paths := make([]string, 0, len(files))
			for _, file := range files {
				paths = append(paths, file.Path)
			}
			assert.ElementsMatch(t, []string{filepath.ToSlash(visible[0]), filepath.ToSlash(visible[1])}, paths)
			count, err := f.mediaDB.BrowseFileCount(ctx, database.BrowseFileCountOptions{
				Overlay: f.overlay(), Systems: systems, ExcludeHidden: true,
			})
			require.NoError(t, err)
			assert.Equal(t, 2, count)
			for _, sort := range []string{"name-asc", "filename-asc"} {
				index, indexErr := f.mediaDB.BrowseIndex(ctx, database.BrowseIndexOptions{
					Overlay: f.overlay(), Systems: systems, ExcludeHidden: true, Sort: sort,
				})
				require.NoError(t, indexErr)
				assert.Equal(t, 2, index.TotalFiles)
			}
			dirs, err := f.mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
				Overlay: f.overlay(), Systems: systems, ExcludeHidden: true,
			})
			require.NoError(t, err)
			require.Len(t, dirs, 1)
			assert.Equal(t, "Blocking.nes", dirs[0].Name)
			assert.Equal(t, 1, dirs[0].FileCount)
			dirCount, err := f.mediaDB.BrowseDirCount(ctx, database.BrowseDirCountOptions{
				Overlay: f.overlay(), Systems: systems, ExcludeHidden: true,
			})
			require.NoError(t, err)
			assert.Equal(t, 1, dirCount)
		})
	}
}

// hideMediaPaths marks each path hidden through the same projection the API
// writes, so the tests read what a real hide leaves behind.
func hideMediaPaths(t *testing.T, mediaDB *MediaDB, paths ...string) {
	t.Helper()
	ctx := context.Background()
	system, err := mediaDB.FindSystemBySystemID("NES")
	require.NoError(t, err)
	for _, path := range paths {
		media, lookupErr := mediaDB.FindMediaBySystemAndPath(ctx, system.DBID, path)
		require.NoError(t, lookupErr)
		require.NoError(t, mediaDB.UpdateMediaTags(ctx, media.DBID, nil,
			[]database.MediaTagRef{{Type: "user", Tag: "hidden"}}))
	}
}

// Hiding one file must not push browse off its cached aggregates: on a real
// library that swap turns a 100 ms page into a table scan that outlives the
// request deadline. Every assertion here is on the cache-backed path.
func TestHiddenCountsSubtractFromBrowseCache(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	f.insert("OnlyHidden", root+"AllHidden/Game.nes")
	f.insert("MixedHidden", root+"Mixed/Hidden.nes")
	f.insert("MixedVisible", root+"Mixed/Visible.nes")
	f.insert("Direct", root+"Direct.nes")
	f.commit(t, true)

	ready, err := sqlBrowseCacheReady(ctx, f.mediaDB.sql.Load())
	require.NoError(t, err)
	require.True(t, ready, "the cached aggregates are what this test covers")
	hideMediaPaths(t, f.mediaDB, root+"AllHidden/Game.nes", root+"Mixed/Hidden.nes")

	dirs, err := f.mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
		PathPrefix: root, ExcludeHidden: true,
	})
	require.NoError(t, err)
	require.Len(t, dirs, 1, "a directory holding only hidden media disappears")
	assert.Equal(t, "Mixed", dirs[0].Name)
	assert.Equal(t, 1, dirs[0].FileCount)

	dirCount, err := f.mediaDB.BrowseDirCount(ctx, database.BrowseDirCountOptions{
		PathPrefix: root, ExcludeHidden: true,
	})
	require.NoError(t, err)
	assert.Equal(t, len(dirs), dirCount, "the count and the listing must agree")

	counts, err := f.mediaDB.BrowseRootCounts(ctx, []string{root}, true)
	require.NoError(t, err)
	require.NotNil(t, counts[root])
	assert.Equal(t, 2, *counts[root])

	routeCounts, err := f.mediaDB.BrowseRouteCounts(ctx, database.BrowseRouteCountsOptions{
		Routes: []string{root}, Systems: []systemdefs.System{f.system}, ExcludeHidden: true,
	})
	require.NoError(t, err)
	require.Contains(t, routeCounts, root)
	assert.Equal(t, 2, routeCounts[root].FileCount)

	candidates, cacheReady, err := f.mediaDB.BrowseSystemRootCandidates(
		ctx, database.BrowseSystemRootCandidatesOptions{
			Roots: []string{root}, Systems: []systemdefs.System{f.system}, ExcludeHidden: true,
		})
	require.NoError(t, err)
	assert.True(t, cacheReady, "candidates stay cache-backed once media is hidden")
	assert.True(t, candidates.HasMedia[root])
	assert.Equal(t, []string{"Mixed"}, candidates.Children[root])
}

// A directory emptied by hiding must not cost the page a row: the listing
// over-fetches by the number of directories hidden media could empty.
func TestHiddenDirectoryPageStaysFull(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	for _, name := range []string{"A", "B", "C"} {
		f.insert(name+"Game", root+name+"/Game.nes")
	}
	f.commit(t, true)
	hideMediaPaths(t, f.mediaDB, root+"B/Game.nes")

	dirs, err := f.mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
		PathPrefix: root, ExcludeHidden: true, Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, dirs, 2)
	assert.Equal(t, []string{"A", "C"}, []string{dirs[0].Name, dirs[1].Name})
}

// Virtual scheme roots hang off "/" in BrowseDirs. Looking the root up as ""
// found nothing, so a populated cache dropped every virtual root from the
// pathless listing.
func TestVirtualSchemesComeFromBrowseCache(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	f.insert("Hidden", "steam://1/Hidden")
	f.insert("Visible", "steam://2/Visible")
	f.commit(t, true)

	ready, err := sqlBrowseCacheReady(ctx, f.mediaDB.sql.Load())
	require.NoError(t, err)
	require.True(t, ready)

	schemes, err := f.mediaDB.BrowseVirtualSchemes(ctx, database.BrowseVirtualSchemesOptions{})
	require.NoError(t, err)
	require.Len(t, schemes, 1)
	assert.Equal(t, "steam://", schemes[0].Scheme)
	assert.Equal(t, 2, schemes[0].FileCount)

	hideMediaPaths(t, f.mediaDB, "steam://1/Hidden")
	schemes, err = f.mediaDB.BrowseVirtualSchemes(ctx, database.BrowseVirtualSchemesOptions{
		ExcludeHidden: true,
	})
	require.NoError(t, err)
	require.Len(t, schemes, 1)
	assert.Equal(t, 1, schemes[0].FileCount)

	hideMediaPaths(t, f.mediaDB, "steam://2/Visible")
	schemes, err = f.mediaDB.BrowseVirtualSchemes(ctx, database.BrowseVirtualSchemesOptions{
		ExcludeHidden: true,
	})
	require.NoError(t, err)
	assert.Empty(t, schemes, "a fully hidden scheme stops being a browse root")
}

// System counts come from the per-generation cache, so hidden media has to be
// subtracted from it rather than re-aggregated behind a NOT filter.
func TestSystemMediaCountsSubtractHidden(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	f.insert("First", root+"First.nes")
	f.insert("Second", root+"Second.nes")
	f.commit(t, true)

	counts, err := f.mediaDB.SystemMediaCounts(ctx, nil, false)
	require.NoError(t, err)
	require.Len(t, counts, 1)
	assert.Equal(t, 2, counts[0].Count)

	hideMediaPaths(t, f.mediaDB, root+"First.nes")
	counts, err = f.mediaDB.SystemMediaCounts(ctx, nil, true)
	require.NoError(t, err)
	require.Len(t, counts, 1)
	assert.Equal(t, 1, counts[0].Count)

	// The shared cache must keep reporting the unfiltered totals.
	counts, err = f.mediaDB.SystemMediaCounts(ctx, nil, false)
	require.NoError(t, err)
	require.Len(t, counts, 1)
	assert.Equal(t, 2, counts[0].Count)

	hideMediaPaths(t, f.mediaDB, root+"Second.nes")
	counts, err = f.mediaDB.SystemMediaCounts(ctx, nil, true)
	require.NoError(t, err)
	assert.Empty(t, counts, "a fully hidden system drops out of the indexed list")
}

// Subtracting hidden media from cached aggregates has to match the scope each
// aggregate was built over, or a neighbouring directory loses files it still
// has. These are the boundaries that get it wrong when the match is sloppy.
func TestHiddenSubtractionRespectsScopeBoundaries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	// "A" and "AB" share a prefix; "A/Deep" nests one level further.
	f.insert("AGame", root+"A/Game.nes")
	f.insert("ADeep", root+"A/Deep/Game.nes")
	f.insert("ABGame", root+"AB/Game.nes")
	// "Outer" holds nothing but a nested hidden file, so the whole branch goes.
	f.insert("Buried", root+"Outer/Inner/Game.nes")
	// A file sitting directly in the browsed directory belongs to no child.
	f.insert("Loose", root+"Loose.nes")
	f.commit(t, true)
	hideMediaPaths(t, f.mediaDB,
		root+"A/Deep/Game.nes", root+"Outer/Inner/Game.nes", root+"Loose.nes")

	dirs, err := f.mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
		PathPrefix: root, ExcludeHidden: true,
	})
	require.NoError(t, err)
	byName := make(map[string]int, len(dirs))
	for _, dir := range dirs {
		byName[dir.Name] = dir.FileCount
	}
	assert.Equal(t, map[string]int{"A": 1, "AB": 1}, byName,
		"a nested hide only reduces its own branch, and a sibling sharing a name prefix is untouched")

	dirCount, err := f.mediaDB.BrowseDirCount(ctx, database.BrowseDirCountOptions{
		PathPrefix: root, ExcludeHidden: true,
	})
	require.NoError(t, err)
	assert.Equal(t, len(dirs), dirCount)

	counts, err := f.mediaDB.BrowseRootCounts(ctx, []string{root}, true)
	require.NoError(t, err)
	require.NotNil(t, counts[root])
	assert.Equal(t, 2, *counts[root], "a loose hidden file still leaves the root total")
}

// The system filter narrows which hidden rows an aggregate ever counted, so
// subtracting one system's hidden media from another system's total would
// silently shrink it.
func TestHiddenSubtractionRespectsSystemFilter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	snes, err := f.mediaDB.FindOrInsertSystem(database.System{SystemID: "SNES", Name: "SNES"})
	require.NoError(t, err)
	f.insert("NESGame", root+"Shared/NESGame.nes")
	insertForSystem(t, f.mediaDB, snes.DBID, "SNESGame", root+"Shared/SNESGame.sfc")
	f.commit(t, true)
	hideMediaPaths(t, f.mediaDB, root+"Shared/NESGame.nes")

	nesDirs, err := f.mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
		PathPrefix: root, ExcludeHidden: true, Systems: []systemdefs.System{{ID: "SNES"}},
	})
	require.NoError(t, err)
	require.Len(t, nesDirs, 1)
	assert.Equal(t, 1, nesDirs[0].FileCount, "hiding NES media cannot shrink the SNES total")

	bothDirs, err := f.mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
		PathPrefix: root, ExcludeHidden: true,
	})
	require.NoError(t, err)
	require.Len(t, bothDirs, 1)
	assert.Equal(t, 1, bothDirs[0].FileCount)
}

// A hidden row that has gone missing was never in the cached totals, so
// subtracting it again would take the count below the truth.
func TestHiddenMissingMediaIsNotSubtractedTwice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	f.insert("Present", root+"Dir/Present.nes")
	f.insert("Gone", root+"Dir/Gone.nes")
	f.commit(t, true)
	hideMediaPaths(t, f.mediaDB, root+"Dir/Gone.nes")

	system, err := f.mediaDB.FindSystemBySystemID("NES")
	require.NoError(t, err)
	gone, err := f.mediaDB.FindMediaBySystemAndPath(ctx, system.DBID, root+"Dir/Gone.nes")
	require.NoError(t, err)
	_, err = f.mediaDB.sql.Load().ExecContext(ctx,
		`UPDATE Media SET IsMissing = 1 WHERE DBID = ?`, gone.DBID)
	require.NoError(t, err)
	require.NoError(t, sqlPopulateBrowseCache(ctx, f.mediaDB.sql.Load()))

	dirs, err := f.mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
		PathPrefix: root, ExcludeHidden: true,
	})
	require.NoError(t, err)
	require.Len(t, dirs, 1)
	assert.Equal(t, 1, dirs[0].FileCount)
}

// insertForSystem adds one media row for a second system inside the fixture's
// open transaction, so a directory can hold media from more than one system.
func insertForSystem(t *testing.T, mediaDB *MediaDB, systemDBID int64, name, path string) {
	t.Helper()
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: systemDBID,
		Slug:       slugs.Slugify("game", name+path),
		Name:       name,
	})
	require.NoError(t, err)
	_, err = mediaDB.InsertMedia(database.Media{
		SystemDBID:     systemDBID,
		MediaTitleDBID: title.DBID,
		Path:           path,
		ParentDir:      filepath.ToSlash(filepath.Dir(path)) + "/",
		SortName:       name,
	})
	require.NoError(t, err)
}

// Random selection skips hidden media, but a required user:hidden filter is an
// explicit ask for exactly those entries. Forcing the exclusion there would
// build a query that can never match.
func TestRandomHonoursExplicitHiddenFilter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	f.insert("Visible", root+"Visible.nes")
	f.insert("Concealed", root+"Concealed.nes")
	f.commit(t, true)
	hideMediaPaths(t, f.mediaDB, root+"Concealed.nes")

	for range 10 {
		game, err := f.mediaDB.RandomGameWithQuery(ctx, &database.MediaQuery{Systems: []string{"NES"}})
		require.NoError(t, err)
		assert.Equal(t, root+"Visible.nes", game.Path, "ordinary random skips hidden media")
	}

	hiddenOnly := []zapscript.TagFilter{{
		Type: "user", Value: "hidden", Operator: zapscript.TagOperatorAND,
	}}
	for range 10 {
		game, err := f.mediaDB.RandomGameWithQuery(ctx, &database.MediaQuery{
			Systems: []string{"NES"}, Tags: hiddenOnly,
		})
		require.NoError(t, err)
		assert.Equal(t, root+"Concealed.nes", game.Path)
	}
}
