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

package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	phelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// dirIndexFixture is a real MediaDB behind the browse handlers, holding a PSX
// library spread over two roots the way a CD system is laid out: one folder
// per game.
type dirIndexFixture struct {
	roots []string
	env   requests.RequestEnv
}

// newDirIndexFixture indexes one disc image inside a folder of its own for
// every name in games, under roots[i]/PSX, plus any loose files given as
// paths relative to a root.
func newDirIndexFixture(t *testing.T, games, loose [][]string) *dirIndexFixture {
	t.Helper()
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	roots := make([]string, len(games))
	var paths []string
	for i := range games {
		roots[i] = t.TempDir()
		for _, game := range games[i] {
			paths = append(paths, filepath.Join(roots[i], "PSX", game, game+".chd"))
		}
		if i < len(loose) {
			for _, file := range loose[i] {
				paths = append(paths, filepath.Join(roots[i], "PSX", file))
			}
		}
	}
	scantest.IndexMediaPaths(t, mediaDB, "PSX", paths...)
	require.NoError(t, mediaDB.PopulateBrowseCache(ctx))

	platform := mocks.NewMockPlatform()
	platform.On("RootDirs", mock.Anything).Return(roots)
	platform.On("SupportedReaders", mock.Anything).Return(nil)
	launchers := []platforms.Launcher{{ID: "PSX", SystemID: "PSX", Folders: []string{"PSX"}}}
	platform.On("Launchers", mock.Anything).Return(launchers)
	cache := &phelpers.LauncherCache{}
	cache.InitializeFromSlice(launchers)

	return &dirIndexFixture{
		roots: roots,
		env: requests.RequestEnv{
			Context:       ctx,
			Database:      &database.Database{MediaDB: mediaDB, UserDB: userDB},
			Platform:      platform,
			Config:        &config.Instance{},
			LauncherCache: cache,
		},
	}
}

func (f *dirIndexFixture) call(
	t *testing.T, handler func(requests.RequestEnv) (any, error), scope map[string]any, extra map[string]any,
) any {
	t.Helper()
	params := make(map[string]any, len(scope)+len(extra))
	for key, value := range scope {
		params[key] = value
	}
	for key, value := range extra {
		params[key] = value
	}
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	result, err := handler(withParams(&f.env, string(raw)))
	require.NoError(t, err)
	return result
}

func (f *dirIndexFixture) index(t *testing.T, scope map[string]any) models.BrowseIndexResults {
	t.Helper()
	result, ok := f.call(t, HandleMediaBrowseIndex, scope, nil).(models.BrowseIndexResults)
	require.True(t, ok)
	return result
}

func (f *dirIndexFixture) browse(t *testing.T, scope, extra map[string]any) models.BrowseResults {
	t.Helper()
	result, ok := f.call(t, HandleMediaBrowse, scope, extra).(models.BrowseResults)
	require.True(t, ok)
	return result
}

// walk pages media.browse to the end, a few entries at a time so the walk
// crosses several directory pages, and returns every entry in served order.
func (f *dirIndexFixture) walk(t *testing.T, scope map[string]any) []models.BrowseEntry {
	t.Helper()
	var entries []models.BrowseEntry
	extra := map[string]any{"maxResults": 2}
	for range 100 {
		page := f.browse(t, scope, extra)
		entries = append(entries, page.Entries...)
		if page.Pagination == nil || page.Pagination.NextCursor == nil {
			return entries
		}
		extra["cursor"] = *page.Pagination.NextCursor
	}
	require.Fail(t, "media.browse walk did not end")
	return nil
}

func entriesOfType(entries []models.BrowseEntry, entryType string) []models.BrowseEntry {
	var matched []models.BrowseEntry
	for i := range entries {
		if entries[i].Type == entryType {
			matched = append(matched, entries[i])
		}
	}
	return matched
}

func indexGroupKeys(groups []models.BrowseIndexGroup) []string {
	keys := make([]string, len(groups))
	for i := range groups {
		keys[i] = groups[i].Key
	}
	return keys
}

// assertGroupsMatchWalk checks a directory facet against what media.browse
// actually serves for the same scope: the groups tile the directory entries
// in order, each offset is the position media.browse returned that directory
// at, and each cursor opens a page that starts there and runs to the end.
func (f *dirIndexFixture) assertGroupsMatchWalk(
	t *testing.T, scope map[string]any, index models.BrowseIndexResults,
) []models.BrowseEntry {
	t.Helper()
	dirs := entriesOfType(f.walk(t, scope), "directory")
	require.NotEmpty(t, dirs)

	next := 0
	for i, group := range index.Groups {
		assert.Equalf(t, next, group.Offset, "group %q offset", group.Key)
		require.LessOrEqualf(t, group.Offset+group.Count, len(dirs), "group %q runs past the listing", group.Key)
		next = group.Offset + group.Count

		if i == 0 {
			assert.Empty(t, group.Cursor, "the bucket that starts the list has no cursor")
			continue
		}
		require.NotEmptyf(t, group.Cursor, "group %q cursor", group.Key)
		rest := entriesOfType(
			f.walk(t, mergeParams(scope, map[string]any{"cursor": group.Cursor})), "directory",
		)
		assert.Equalf(t, entryPaths(dirs[group.Offset:]), entryPaths(rest),
			"group %q cursor must resume at its offset and list every directory after it", group.Key)

		// The cursor is the one media.browse itself hands out after the
		// preceding directory, not a lookalike.
		page := f.browse(t, scope, map[string]any{"maxResults": group.Offset})
		require.NotNil(t, page.Pagination)
		require.NotNil(t, page.Pagination.NextCursor)
		assert.Equalf(t, *page.Pagination.NextCursor, group.Cursor, "group %q cursor", group.Key)
	}
	assert.Equal(t, len(dirs), next, "groups must cover every directory entry")
	return dirs
}

func mergeParams(base, extra map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(extra))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

func entryPaths(entries []models.BrowseEntry) []string {
	paths := make([]string, len(entries))
	for i := range entries {
		paths[i] = entries[i].Path
	}
	return paths
}

var dirIndexGames = []string{
	"Ape Escape (USA)",
	"alundra (USA)",
	"Bushido Blade (USA)",
	"Crash Bandicoot (USA)",
	"crash team racing (USA)",
	"[T-En]Mizzurna Falls (Japan)",
	"007 Racing (USA)",
	"Metal Gear Solid (USA)",
	"Zanac X Zanac (Japan)",
}

func TestHandleMediaBrowseIndex_FolderOfContainerDirectories(t *testing.T) {
	t.Parallel()
	f := newDirIndexFixture(t, [][]string{dirIndexGames}, nil)
	scope := map[string]any{"path": filepath.Join(f.roots[0], "PSX"), "systems": []string{"PSX"}}

	index := f.index(t, scope)
	assert.Equal(t, "latin", index.Scheme)
	assert.Equal(t, models.BrowseIndexEntryTypeDirectory, index.EntryType)
	assert.Equal(t, 0, index.TotalFiles)
	// The bracketed prefix is metadata to the directory order, so that folder
	// sorts and buckets under M.
	assert.Equal(t, []string{"0-9", "A", "B", "C", "M", "Z"}, indexGroupKeys(index.Groups))
	counts := make(map[string]int, len(index.Groups))
	for _, group := range index.Groups {
		counts[group.Key] = group.Count
		assert.Equal(t, group.Key, group.Label)
	}
	assert.Equal(t, map[string]int{"0-9": 1, "A": 2, "B": 1, "C": 2, "M": 2, "Z": 1}, counts)

	dirs := f.assertGroupsMatchWalk(t, scope, index)
	require.Len(t, dirs, len(dirIndexGames))
	// These are the entries the facet exists for: directories that stand for
	// the one game inside them.
	for i := range dirs {
		assert.NotZerof(t, dirs[i].MediaID, "directory %q should carry its game's media id", dirs[i].Path)
	}
}

func TestHandleMediaBrowseIndex_DirectoryOrderIgnoresFileSort(t *testing.T) {
	t.Parallel()
	f := newDirIndexFixture(t, [][]string{dirIndexGames}, nil)

	for _, sortOrder := range []string{"name-desc", "filename-asc"} {
		scope := map[string]any{
			"path": filepath.Join(f.roots[0], "PSX"), "systems": []string{"PSX"}, "sort": sortOrder,
		}
		index := f.index(t, scope)
		assert.Equal(t, "latin", index.Scheme, sortOrder)
		assert.Equal(t, models.BrowseIndexEntryTypeDirectory, index.EntryType, sortOrder)
		// media.browse lists directories ascending whatever the file sort is,
		// and the facet follows the listing rather than the sort parameter.
		assert.Equal(t, []string{"0-9", "A", "B", "C", "M", "Z"}, indexGroupKeys(index.Groups), sortOrder)
		f.assertGroupsMatchWalk(t, scope, index)
	}
}

func TestHandleMediaBrowseIndex_MixedFolderKeepsFileBuckets(t *testing.T) {
	t.Parallel()
	f := newDirIndexFixture(t,
		[][]string{{"Ape Escape (USA)", "Bushido Blade (USA)"}},
		[][]string{{"Demo Disc.chd", "Wipeout.chd", "Xevious.chd"}},
	)
	scope := map[string]any{"path": filepath.Join(f.roots[0], "PSX"), "systems": []string{"PSX"}}

	index := f.index(t, scope)
	assert.Equal(t, "latin", index.Scheme)
	assert.Equal(t, models.BrowseIndexEntryTypeMedia, index.EntryType)
	assert.Equal(t, 3, index.TotalFiles)
	assert.Equal(t, []string{"D", "W", "X"}, indexGroupKeys(index.Groups))

	// File offsets still exclude the leading directories.
	entries := f.walk(t, scope)
	dirs := entriesOfType(entries, "directory")
	require.Len(t, dirs, 2)
	files := entries[len(dirs):]
	require.Len(t, files, 3)
	for i, group := range index.Groups {
		assert.Equal(t, i, group.Offset)
		assert.Equal(t, 1, group.Count)
		if group.Cursor == "" {
			continue
		}
		page := f.browse(t, scope, map[string]any{"cursor": group.Cursor})
		require.NotEmpty(t, page.Entries)
		assert.Equal(t, files[group.Offset].Path, page.Entries[0].Path)
	}
}

func TestHandleMediaBrowseIndex_RootContentsOfContainerDirectories(t *testing.T) {
	t.Parallel()
	// Two roots merged through the overlay, with one game present in both so
	// the merge has a duplicate name to collapse.
	f := newDirIndexFixture(t, [][]string{
		{"Ape Escape (USA)", "Crash Bandicoot (USA)", "Metal Gear Solid (USA)"},
		{"alundra (USA)", "Crash Bandicoot (USA)", "Castlevania (USA)", "Zanac X Zanac (Japan)"},
	}, nil)
	scope := map[string]any{"rootView": browseRootViewContents, "systems": []string{"PSX"}}

	index := f.index(t, scope)
	assert.Equal(t, "latin", index.Scheme)
	assert.Equal(t, models.BrowseIndexEntryTypeDirectory, index.EntryType)
	assert.Equal(t, []string{"A", "C", "M", "Z"}, indexGroupKeys(index.Groups))

	dirs := f.assertGroupsMatchWalk(t, scope, index)
	assert.Len(t, dirs, 6, "the game present in both roots is listed once")

	cursor, err := decodeBrowseCursor(index.Groups[1].Cursor)
	require.NoError(t, err)
	require.NotNil(t, cursor)
	assert.Equal(t, browsePhaseDirs, cursor.Phase)
	assert.Equal(t, browseRootViewContents, cursor.RootView)
	assert.Len(t, cursor.Sources, 2, "the cursor carries the resolved routes like any contents cursor")
}

func TestHandleMediaBrowseIndex_DirectoryBucketsSkipHiddenFolders(t *testing.T) {
	t.Parallel()
	f := newDirIndexFixture(t, [][]string{dirIndexGames}, nil)
	scope := map[string]any{"path": filepath.Join(f.roots[0], "PSX"), "systems": []string{"PSX"}}

	before := entriesOfType(f.walk(t, scope), "directory")
	var hidden *models.BrowseEntry
	for i := range before {
		if filepath.Base(before[i].Path) == "Bushido Blade (USA)" {
			hidden = &before[i]
		}
	}
	require.NotNil(t, hidden)
	require.NotZero(t, hidden.MediaID)
	_, err := HandleMediaTagsUpdate(withParams(&f.env, fmt.Sprintf(
		`{"mediaId":%d,"add":["user:hidden"]}`, hidden.MediaID)))
	require.NoError(t, err)

	index := f.index(t, scope)
	assert.Equal(t, []string{"0-9", "A", "C", "M", "Z"}, indexGroupKeys(index.Groups),
		"a folder holding only hidden media leaves the listing and its bucket")
	dirs := f.assertGroupsMatchWalk(t, scope, index)
	assert.Len(t, dirs, len(dirIndexGames)-1)

	// Asking for hidden entries brings the folder and its bucket back.
	withHidden := mergeParams(scope, map[string]any{"includeHidden": true})
	index = f.index(t, withHidden)
	assert.Equal(t, []string{"0-9", "A", "B", "C", "M", "Z"}, indexGroupKeys(index.Groups))
	f.assertGroupsMatchWalk(t, withHidden, index)
}

func TestHandleMediaBrowseIndex_EntryTypeDefaultsToMedia(t *testing.T) {
	t.Parallel()
	f := newDirIndexFixture(t, [][]string{dirIndexGames}, nil)

	// A route listing has no rail and no directory facet.
	routes := f.index(t, map[string]any{"systems": []string{"PSX"}})
	assert.Equal(t, "none", routes.Scheme)
	assert.Equal(t, models.BrowseIndexEntryTypeMedia, routes.EntryType)
	assert.Empty(t, routes.Groups)

	// A folder with neither files nor directories stays an empty file facet.
	empty := f.index(t, map[string]any{"path": filepath.Join(f.roots[0], "Saturn")})
	assert.Equal(t, models.BrowseIndexEntryTypeMedia, empty.EntryType)
	assert.Empty(t, empty.Groups)

	raw, err := json.Marshal(empty)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"entryType":"media"`)
}
