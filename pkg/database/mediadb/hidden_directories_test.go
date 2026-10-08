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
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hideDirs projects folders as hidden for NES, the way a real hide does once
// UserDB has recorded it.
func hideDirs(t *testing.T, mediaDB *MediaDB, dirs ...string) {
	t.Helper()
	hidden := make([]database.HiddenDirectory, 0, len(dirs))
	for _, dir := range dirs {
		hidden = append(hidden, database.HiddenDirectory{SystemID: "NES", Path: strings.TrimSuffix(dir, "/")})
	}
	_, err := mediaDB.ReplaceHiddenDirectories(context.Background(), hidden)
	require.NoError(t, err)
}

func dirNames(dirs []database.BrowseDirectoryResult) []string {
	names := make([]string, 0, len(dirs))
	for i := range dirs {
		names = append(names, dirs[i].Name)
	}
	return names
}

func searchNames(t *testing.T, f *mergeFixture, filters *database.SearchFilters) []string {
	t.Helper()
	filters.Systems = []systemdefs.System{f.system}
	filters.Sort = "name-asc"
	filters.Limit = 100
	results, err := f.mediaDB.SearchMediaWithFilters(context.Background(), filters)
	require.NoError(t, err)
	names := make([]string, 0, len(results))
	for i := range results {
		names = append(names, results[i].Name)
	}
	return names
}

// A hidden folder leaves the listing of its parent, gives its media back from
// every count above it, and is one row however much it holds. Both the cached
// aggregates and the Media fallbacks have to agree.
func TestHiddenDirectoryLeavesListingsAndCounts(t *testing.T) {
	t.Parallel()
	for _, cached := range []bool{true, false} {
		name := "media fallback"
		if cached {
			name = "browse cache"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f, cleanup := setupMergeFixture(t, 1)
			t.Cleanup(cleanup)
			root := f.roots[0]
			f.insert("AltOne", root+"_alternatives/Game/One.nes")
			f.insert("AltTwo", root+"_alternatives/Game/Two.nes")
			f.insert("Nested", root+"Keep/Deep/Hidden/Nested.nes")
			f.insert("Kept", root+"Keep/Kept.nes")
			f.insert("Direct", root+"Direct.nes")
			f.commit(t, cached)
			hideDirs(t, f.mediaDB, root+"_alternatives", root+"Keep/Deep/Hidden")
			systems := []systemdefs.System{f.system}

			dirs, err := f.mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
				PathPrefix: root, Systems: systems, ExcludeHidden: true,
			})
			require.NoError(t, err)
			assert.Equal(t, []string{"Keep"}, dirNames(dirs), "the hidden folder is not listed")
			if cached {
				assert.Equal(t, 1, dirs[0].FileCount, "a hidden folder below gives its media back")
				dirCount, countErr := f.mediaDB.BrowseDirCount(ctx, database.BrowseDirCountOptions{
					PathPrefix: root, Systems: systems, ExcludeHidden: true,
				})
				require.NoError(t, countErr)
				assert.Equal(t, 1, dirCount, "the count and the listing must agree")

				routes, routeErr := f.mediaDB.BrowseRouteCounts(ctx, database.BrowseRouteCountsOptions{
					Routes: []string{root}, Systems: systems, ExcludeHidden: true,
				})
				require.NoError(t, routeErr)
				assert.Equal(t, 2, routes[root].FileCount)
			}

			// A listing that includes hidden entries keeps the folder and
			// says which it is.
			all, err := f.mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
				PathPrefix: root, Systems: systems,
			})
			require.NoError(t, err)
			require.Equal(t, []string{"_alternatives", "Keep"}, dirNames(all))
			assert.True(t, all[0].Hidden)
			assert.Equal(t, 2, all[0].FileCount)
			assert.False(t, all[1].Hidden)

			// The folder still browses by its own path, as does one inside it.
			inside, err := f.mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
				PathPrefix: root + "_alternatives/", Systems: systems, ExcludeHidden: true,
			})
			require.NoError(t, err)
			require.Equal(t, []string{"Game"}, dirNames(inside))
			assert.Equal(t, 2, inside[0].FileCount)
			files, err := f.mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
				PathPrefix: root + "_alternatives/Game/", Systems: systems, ExcludeHidden: true, Limit: 10,
			})
			require.NoError(t, err)
			assert.Len(t, files, 2)
		})
	}
}

// Search and random selection read below a single directory, so they are the
// queries a hidden folder has to be excluded from by path. A search scoped to
// the folder itself addresses it directly and still finds its media.
func TestHiddenDirectoryLeavesSearchAndRandom(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	f.insert("Alt One", root+"_alternatives/Game/One.nes")
	f.insert("Alt Two", root+"_alternatives/Game/Two.nes")
	f.insert("Visible", root+"Visible.nes")
	// A sibling whose name only starts like the folder's stays.
	f.insert("Lookalike", root+"_alternatives2/Lookalike.nes")
	f.commit(t, true)
	hideDirs(t, f.mediaDB, root+"_alternatives")

	assert.Equal(t, []string{"Lookalike", "Visible"},
		searchNames(t, f, &database.SearchFilters{ExcludeHidden: true}))
	assert.Equal(t, []string{"Alt One", "Alt Two", "Lookalike", "Visible"},
		searchNames(t, f, &database.SearchFilters{}), "includeHidden finds them again")
	assert.Equal(t, []string{"Alt One", "Alt Two"},
		searchNames(t, f, &database.SearchFilters{ExcludeHidden: true, PathPrefix: root + "_alternatives"}))
	assert.Equal(t, []string{"Alt One", "Alt Two"},
		searchNames(t, f, &database.SearchFilters{ExcludeHidden: true, PathPrefix: root + "_alternatives/Game"}))
	assert.Empty(t, searchNames(t, f, &database.SearchFilters{ExcludeHidden: true, Query: "one"}))

	for range 20 {
		game, err := f.mediaDB.RandomGameWithQuery(ctx, &database.MediaQuery{Systems: []string{"NES"}})
		require.NoError(t, err)
		assert.NotContains(t, game.Path, "/_alternatives/")
	}

	counts, err := f.mediaDB.SystemMediaCounts(ctx, nil, true)
	require.NoError(t, err)
	require.Len(t, counts, 1)
	assert.Equal(t, 2, counts[0].Count)
	counts, err = f.mediaDB.SystemMediaCounts(ctx, nil, false)
	require.NoError(t, err)
	assert.Equal(t, 4, counts[0].Count)
}

// A file hidden on its own inside a hidden folder is given back once, not
// twice, and stays hidden when the folder is browsed directly.
func TestHiddenFileInsideHiddenDirectoryIsCountedOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	f.insert("Inside", root+"Folder/Inside.nes")
	f.insert("AlsoHidden", root+"Folder/AlsoHidden.nes")
	f.insert("Outside", root+"Other/Outside.nes")
	f.commit(t, true)
	hideMediaPaths(t, f.mediaDB, root+"Folder/AlsoHidden.nes")
	hideDirs(t, f.mediaDB, root+"Folder")

	counts, err := f.mediaDB.BrowseRootCounts(ctx, []string{root}, true)
	require.NoError(t, err)
	require.NotNil(t, counts[root])
	assert.Equal(t, 1, *counts[root])

	files, err := f.mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
		PathPrefix: root + "Folder/", Systems: []systemdefs.System{f.system}, ExcludeHidden: true, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "Inside", files[0].Name)
}

// The projection advances the revision that invalidates cursors only when it
// actually changed, so a sync that finds it in line costs clients nothing.
func TestReplaceHiddenDirectoriesAdvancesRevisionOnChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	f.insert("Game", root+"Folder/Game.nes")
	f.commit(t, true)
	revision := func() string {
		value, err := f.mediaDB.MediaPreferencesRevision(ctx)
		require.NoError(t, err)
		return value
	}
	dirs := []database.HiddenDirectory{{SystemID: "NES", Path: root + "Folder"}}

	before := revision()
	changed, err := f.mediaDB.ReplaceHiddenDirectories(ctx, dirs)
	require.NoError(t, err)
	assert.True(t, changed)
	hidden := revision()
	assert.NotEqual(t, before, hidden)

	// The same set again, in a form that only differs by a trailing slash.
	changed, err = f.mediaDB.ReplaceHiddenDirectories(ctx,
		[]database.HiddenDirectory{{SystemID: "NES", Path: root + "Folder/"}})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, hidden, revision())

	changed, err = f.mediaDB.ReplaceHiddenDirectories(ctx, nil)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.NotEqual(t, hidden, revision())

	system, err := f.mediaDB.FindSystemBySystemID("NES")
	require.NoError(t, err)
	has, err := f.mediaDB.HasMediaUnderDirectory(ctx, system.DBID, root+"Folder")
	require.NoError(t, err)
	assert.True(t, has)
	has, err = f.mediaDB.HasMediaUnderDirectory(ctx, system.DBID, root+"Fold")
	require.NoError(t, err)
	assert.False(t, has, "a name that only starts the folder's is not the folder")
}
