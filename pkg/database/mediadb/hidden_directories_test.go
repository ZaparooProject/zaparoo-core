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

// A projection this build cannot read hides nothing, and the next sync
// replaces it. A database that is closed or mid-transaction refuses the write.
func TestHiddenDirectoriesProjectionEdges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	f.insert("Game", root+"Folder/Game.nes")

	// The fixture's transaction is still open.
	_, err := f.mediaDB.ReplaceHiddenDirectories(ctx, nil)
	require.ErrorIs(t, err, ErrTransactionActive)
	f.commit(t, true)

	_, err = f.mediaDB.sql.Load().ExecContext(ctx,
		`INSERT INTO DBConfig(Name, Value) VALUES (?, 'not json')`, DBConfigHiddenDirectories)
	require.NoError(t, err)
	dirs, err := f.mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
		PathPrefix: root, ExcludeHidden: true,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Folder"}, dirNames(dirs))

	// Entries with no system or no path are dropped, and a repeat is one.
	changed, err := f.mediaDB.ReplaceHiddenDirectories(ctx, []database.HiddenDirectory{
		{SystemID: "NES", Path: root + "Folder"},
		{SystemID: "NES", Path: root + "Folder/"},
		{SystemID: "", Path: root + "Folder"},
		{SystemID: "NES", Path: ""},
	})
	require.NoError(t, err)
	assert.True(t, changed)
	dirs, err = f.mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
		PathPrefix: root, ExcludeHidden: true,
	})
	require.NoError(t, err)
	assert.Empty(t, dirs)

	closed := &MediaDB{}
	_, err = closed.ReplaceHiddenDirectories(ctx, nil)
	require.ErrorIs(t, err, ErrNullSQL)
	_, err = closed.HasMediaUnderDirectory(ctx, 1, root)
	require.ErrorIs(t, err, ErrNullSQL)
}

// Title candidates treat media under a hidden folder like media hidden one
// file at a time: it no longer makes its title eligible.
func TestTitleCandidatesSkipTitlesOnlyUnderHiddenDirectories(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	f.insert("Zelda Hidden", root+"Folder/Zelda Hidden.nes")
	f.insert("Zelda Shown", root+"Zelda Shown.nes")
	f.commit(t, true)

	names := func() []string {
		candidates, err := f.mediaDB.TitleCandidates(ctx, "NES", "Zelda", 5)
		require.NoError(t, err)
		got := make([]string, 0, len(candidates))
		for i := range candidates {
			got = append(got, candidates[i].Name)
		}
		return got
	}
	assert.Contains(t, names(), "Zelda Hidden")
	hideDirs(t, f.mediaDB, root+"Folder")
	got := names()
	assert.NotContains(t, got, "Zelda Hidden")
	assert.Contains(t, got, "Zelda Shown")
}

// System root candidates probe Media for anything still visible, so the
// probe has to leave a hidden folder's media out as the counts do.
func TestHiddenDirectoryLeavesRootCandidates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 2)
	t.Cleanup(cleanup)
	f.insert("Inside", f.roots[0]+"Folder/Inside.nes")
	f.insert("Kept", f.roots[0]+"Kept/Kept.nes")
	f.insert("Only", f.roots[1]+"Whole/Only.nes")
	f.commit(t, true)
	hideDirs(t, f.mediaDB, f.roots[0]+"Folder", f.roots[1]+"Whole")

	candidates, _, err := f.mediaDB.BrowseSystemRootCandidates(ctx, database.BrowseSystemRootCandidatesOptions{
		Roots: f.roots, Systems: []systemdefs.System{f.system}, ExcludeHidden: true,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Kept"}, candidates.Children[f.roots[0]])
	assert.True(t, candidates.HasMedia[f.roots[0]])
	assert.Empty(t, candidates.Children[f.roots[1]])
	assert.False(t, candidates.HasMedia[f.roots[1]], "a root with nothing visible left is no candidate")
}

func TestHiddenDirCoversEverySystemOfTheRow(t *testing.T) {
	t.Parallel()
	dirs := []hiddenDir{{SystemID: "NES", Prefix: "/roms/Shared/"}}
	nes := []systemdefs.System{{ID: "NES"}}

	assert.True(t, hiddenDirCovers(dirs, "/roms/Shared/", []string{"NES"}, nil))
	assert.False(t, hiddenDirCovers(dirs, "/roms/Shared/", []string{"NES", "SNES"}, nil),
		"a folder that also holds another system's media stays")
	// A row that reports no systems falls back to the listing's scope.
	assert.True(t, hiddenDirCovers(dirs, "/roms/Shared/", nil, nes))
	assert.False(t, hiddenDirCovers(dirs, "/roms/Shared/", nil, []systemdefs.System{{ID: "SNES"}}))
	assert.True(t, hiddenDirCovers(dirs, "/roms/Shared/", nil, nil))
	assert.False(t, hiddenDirCovers(dirs, "/roms/Other/", nil, nil))
}

// A hidden folder inside another hidden folder holds media the outer one
// already gives back, so every count above both gives it back once. Browsing
// inside the outer folder addresses it directly, and there the inner folder
// is the only one that hides anything.
func TestNestedHiddenDirectoriesAreCountedOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	f.insert("DeepOne", root+"Outer/Inner/DeepOne.nes")
	f.insert("DeepTwo", root+"Outer/Inner/DeepTwo.nes")
	f.insert("Shallow", root+"Outer/Shallow.nes")
	f.insert("Visible", root+"Visible.nes")
	f.commit(t, true)
	hideDirs(t, f.mediaDB, root+"Outer/Inner", root+"Outer")
	systems := []systemdefs.System{f.system}

	systemCounts, err := f.mediaDB.SystemMediaCounts(ctx, nil, true)
	require.NoError(t, err)
	require.Len(t, systemCounts, 1, "the system still has a visible game")
	assert.Equal(t, 1, systemCounts[0].Count)

	rootCounts, err := f.mediaDB.BrowseRootCounts(ctx, []string{root}, true)
	require.NoError(t, err)
	require.NotNil(t, rootCounts[root])
	assert.Equal(t, 1, *rootCounts[root])

	routes, err := f.mediaDB.BrowseRouteCounts(ctx, database.BrowseRouteCountsOptions{
		Routes: []string{root}, Systems: systems, ExcludeHidden: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, routes[root].FileCount)

	inside, err := f.mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
		PathPrefix: root + "Outer/", Systems: systems, ExcludeHidden: true,
	})
	require.NoError(t, err)
	assert.Empty(t, inside, "the inner folder stays hidden inside the outer one")
	files, err := f.mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
		PathPrefix: root + "Outer/", Systems: systems, ExcludeHidden: true, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "Shallow", files[0].Name)
}

// Every browse and system listing needs each hidden folder's count, so the
// count is taken once and kept until media rows or visibility change.
func TestHiddenDirectoryCountIsKeptUntilSomethingChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	f.insert("One", root+"Folder/One.nes")
	f.insert("Two", root+"Folder/Two.nes")
	f.insert("Outside", root+"Outside.nes")
	f.commit(t, true)
	hideDirs(t, f.mediaDB, root+"Folder")
	visible := func() int {
		t.Helper()
		counts, err := f.mediaDB.BrowseRootCounts(ctx, []string{root}, true)
		require.NoError(t, err)
		require.NotNil(t, counts[root])
		return *counts[root]
	}
	require.Equal(t, 1, visible())

	// A row removed behind the database's back is not seen: the count was
	// not taken again.
	_, err := f.mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
		"UPDATE Media SET IsMissing = 1 WHERE Path = ?", root+"Folder/Two.nes")
	require.NoError(t, err)
	hiddenDirCountCacheMu.Lock()
	cached := len(hiddenDirCountCacheMap[f.mediaDB.sql.Load()].counts)
	hiddenDirCountCacheMu.Unlock()
	assert.Equal(t, 1, cached)

	// A change to media rows drops it.
	f.mediaDB.invalidateCaches(invalidationScope{AllSystems: true, MediaRowsChanged: true})
	dirs, err := loadHiddenDirs(ctx, f.mediaDB.sql.Load())
	require.NoError(t, err)
	require.NoError(t, loadHiddenDirCounts(ctx, f.mediaDB.sql.Load(), dirs))
	require.Len(t, dirs, 1)
	assert.Equal(t, 1, dirs[0].Count, "the folder now holds one present file")

	// So does a file hidden inside the folder, which the folder stops counting.
	hideMediaPaths(t, f.mediaDB, root+"Folder/One.nes")
	dirs, err = loadHiddenDirs(ctx, f.mediaDB.sql.Load())
	require.NoError(t, err)
	require.NoError(t, loadHiddenDirCounts(ctx, f.mediaDB.sql.Load(), dirs))
	assert.Equal(t, 0, dirs[0].Count)

	// And a change to the hidden folders themselves.
	hideDirs(t, f.mediaDB, root+"Folder", root+"Elsewhere")
	dirs, err = loadHiddenDirs(ctx, f.mediaDB.sql.Load())
	require.NoError(t, err)
	require.NoError(t, loadHiddenDirCounts(ctx, f.mediaDB.sql.Load(), dirs))
	require.Len(t, dirs, 2)
}

// A system's route is the same folder a parent listing shows as a directory,
// so a listing that includes hidden entries says a hidden one is hidden there
// too: it is where a client enters a system and offers to unhide it.
func TestHiddenDirectoryIsMarkedOnItsRoute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	f.insert("One", root+"Hidden/One.nes")
	f.insert("Two", root+"Shown/Two.nes")
	f.commit(t, true)
	hideDirs(t, f.mediaDB, root+"Hidden")
	systems := []systemdefs.System{f.system}
	hiddenRoute, shownRoute := root+"Hidden", root+"Shown"

	all, err := f.mediaDB.BrowseRouteCounts(ctx, database.BrowseRouteCountsOptions{
		Routes: []string{hiddenRoute, shownRoute}, Systems: systems,
	})
	require.NoError(t, err)
	require.Contains(t, all, hiddenRoute)
	assert.True(t, all[hiddenRoute].Hidden)
	assert.Equal(t, 1, all[hiddenRoute].FileCount)
	require.Contains(t, all, shownRoute)
	assert.False(t, all[shownRoute].Hidden)

	visible, err := f.mediaDB.BrowseRouteCounts(ctx, database.BrowseRouteCountsOptions{
		Routes: []string{hiddenRoute, shownRoute}, Systems: systems, ExcludeHidden: true,
	})
	require.NoError(t, err)
	assert.NotContains(t, visible, hiddenRoute)
	require.Contains(t, visible, shownRoute)
	assert.False(t, visible[shownRoute].Hidden)
}

// Whether a candidate still has something to show is the cached count under
// it less what is hidden there, for hidden files and hidden folders alike.
func TestRootCandidatesFollowHiddenCounts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 1)
	t.Cleanup(cleanup)
	root := f.roots[0]
	f.insert("PartA", root+"Partly/A.nes")
	f.insert("PartB", root+"Partly/B.nes")
	f.insert("FileOnly", root+"FilesHidden/Only.nes")
	f.insert("Deep", root+"FolderHidden/Deep/Deep.nes")
	f.insert("Mixed1", root+"Mixed/Sub/One.nes")
	f.insert("Mixed2", root+"Mixed/Two.nes")
	f.commit(t, true)
	hideMediaPaths(t, f.mediaDB, root+"Partly/A.nes", root+"FilesHidden/Only.nes", root+"Mixed/Two.nes")
	hideDirs(t, f.mediaDB, root+"FolderHidden/Deep", root+"Mixed/Sub")

	candidates, ready, err := f.mediaDB.BrowseSystemRootCandidates(ctx, database.BrowseSystemRootCandidatesOptions{
		Roots: f.roots, Systems: []systemdefs.System{f.system}, ExcludeHidden: true,
	})
	require.NoError(t, err)
	require.True(t, ready)
	assert.Equal(t, []string{"Partly"}, candidates.Children[root],
		"only the folder with a visible file left is a candidate")
	assert.True(t, candidates.HasMedia[root])

	all, _, err := f.mediaDB.BrowseSystemRootCandidates(ctx, database.BrowseSystemRootCandidatesOptions{
		Roots: f.roots, Systems: []systemdefs.System{f.system},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"FilesHidden", "FolderHidden", "Mixed", "Partly"}, all.Children[root])
}
