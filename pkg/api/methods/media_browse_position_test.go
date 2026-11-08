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
	"slices"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	phelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var browsePositionSorts = []string{"", "name-asc", "name-desc", "filename-asc", "filename-desc"}

// newBrowsePositionFixture lists folders ahead of loose files in two roots. The
// roots share a folder and a file name, which the merged root view folds, and
// the file names sort differently by title and by filename only in part.
func newBrowsePositionFixture(t *testing.T) *dirIndexFixture {
	t.Helper()
	return newDirIndexFixture(t,
		[][]string{
			{"Ape Escape (USA)", "Bushido Blade (USA)", "Shared Folder", "Metal Gear (USA)", "alundra (USA)"},
			{"Shared Folder", "Tekken (USA)", "Crash (USA)"},
		},
		[][]string{
			{
				"Duke (USA).chd", "Zed (USA).chd", "Shared Game.chd", "Halo (USA).chd",
				"Halo (Europe).chd", "007 Racing.chd", "alpha (USA).chd",
			},
			{"Shared Game.chd", "Yoshi (USA).chd", "Beta (USA).chd", "Wario (USA).chd"},
		},
	)
}

// addVirtualRoute indexes a flat virtual scheme for PSX and teaches the launcher
// cache about it.
func addVirtualRoute(t *testing.T, f *dirIndexFixture, paths ...string) string {
	t.Helper()
	// A full scan of the system would mark the fixture's own rows missing.
	scantest.IndexMediaPathsWithOpts(t, f.env.Database.MediaDB, "PSX",
		database.ScanReconcileOpts{IncompleteScan: true}, paths...)
	require.NoError(t, f.env.Database.MediaDB.PopulateBrowseCache(context.Background()))
	launchers := append([]platforms.Launcher{}, f.env.LauncherCache.GetAllLaunchers()...)
	launchers = append(launchers, platforms.Launcher{ID: "Steam", SystemID: "PSX", Schemes: []string{"steam"}})
	cache := &phelpers.LauncherCache{}
	cache.InitializeFromSlice(launchers)
	f.env.LauncherCache = cache
	return "steam://"
}

// walkPages pages a listing by cursor at one page size.
func walkPages(t *testing.T, f *dirIndexFixture, scope map[string]any, size int) []models.BrowseResults {
	t.Helper()
	var pages []models.BrowseResults
	extra := map[string]any{"maxResults": size}
	for range 1000 {
		page := f.browse(t, scope, extra)
		pages = append(pages, page)
		if page.Pagination == nil || page.Pagination.NextCursor == nil {
			return pages
		}
		extra["cursor"] = *page.Pagination.NextCursor
	}
	require.Fail(t, "media.browse walk did not end")
	return nil
}

func flattenPages(pages []models.BrowseResults) []models.BrowseEntry {
	var entries []models.BrowseEntry
	for i := range pages {
		entries = append(entries, pages[i].Entries...)
	}
	return entries
}

func followCursors(
	t *testing.T, f *dirIndexFixture, scope map[string]any, size int, page *models.BrowseResults,
) []models.BrowseEntry {
	t.Helper()
	entries := append([]models.BrowseEntry{}, page.Entries...)
	next := page.Pagination
	for range 1000 {
		if next == nil || next.NextCursor == nil {
			return entries
		}
		following := f.browse(t, scope, map[string]any{"maxResults": size, "cursor": *next.NextCursor})
		entries = append(entries, following.Entries...)
		next = following.Pagination
	}
	require.Fail(t, "media.browse walk did not end")
	return nil
}

func withSort(scope map[string]any, sortOrder string) map[string]any {
	if sortOrder == "" {
		return scope
	}
	return mergeParams(scope, map[string]any{"sort": sortOrder})
}

// assertOffsetsMatchWalk is the core guarantee: at every depth, a page requested
// by position is the page a cursor walk reaches, including its totals and its
// next cursor, and following that cursor finishes the listing without a gap.
func assertOffsetsMatchWalk(t *testing.T, f *dirIndexFixture, scope map[string]any, size int) {
	t.Helper()
	pages := walkPages(t, f, scope, size)
	flat := flattenPages(pages)
	require.NotEmpty(t, flat)

	for i := range pages {
		got := f.browse(t, scope, map[string]any{"maxResults": size, "offset": i * size})
		require.NotNil(t, got.Pagination)
		assert.Equal(t, pages[i].Entries, got.Entries, "page %d", i)
		assert.Equal(t, pages[i].TotalDirs, got.TotalDirs, "page %d", i)
		assert.Equal(t, pages[i].TotalFiles, got.TotalFiles, "page %d", i)
		assert.Equal(t, pages[i].Pagination.HasNextPage, got.Pagination.HasNextPage, "page %d", i)
		assert.Equal(t, pages[i].Pagination.NextCursor, got.Pagination.NextCursor, "page %d", i)
		assert.Equal(t, size, got.Pagination.PageSize, "page %d", i)
		require.NotNil(t, got.Pagination.Offset)
		assert.Equal(t, i*size, *got.Pagination.Offset, "page %d", i)
		assert.Nil(t, got.Pagination.AnchorIndex, "page %d", i)
	}

	// Positions between page boundaries, and past the end.
	for offset := range len(flat) + 2 {
		got := f.browse(t, scope, map[string]any{"maxResults": size, "offset": offset})
		require.NotNil(t, got.Pagination, "offset %d", offset)
		require.NotNil(t, got.Pagination.Offset, "offset %d", offset)
		assert.Equal(t, offset, *got.Pagination.Offset, "offset %d", offset)
		assert.Equal(t, pages[0].TotalDirs, got.TotalDirs, "offset %d", offset)
		assert.Equal(t, pages[0].TotalFiles, got.TotalFiles, "offset %d", offset)
		if offset >= len(flat) {
			assert.NotNil(t, got.Entries, "offset %d", offset)
			assert.Empty(t, got.Entries, "offset %d", offset)
			assert.False(t, got.Pagination.HasNextPage, "offset %d", offset)
			assert.Nil(t, got.Pagination.NextCursor, "offset %d", offset)
			continue
		}
		assert.Equal(t, flat[offset:min(offset+size, len(flat))], got.Entries, "offset %d", offset)
		assert.Equal(t, flat[offset:], followCursors(t, f, scope, size, &got), "offset %d", offset)
	}
}

func TestBrowseOffsetMatchesCursorWalk(t *testing.T) {
	t.Parallel()
	f := newBrowsePositionFixture(t)
	virtual := addVirtualRoute(t, f, "steam://300/Zeta", "steam://100/Alpha", "steam://200/Mid", "steam://400/Beta")

	listings := map[string]map[string]any{
		"single path": {"path": filepath.Join(f.roots[0], "PSX"), "systems": []string{"PSX"}},
		"no system":   {"path": filepath.Join(f.roots[1], "PSX")},
		"virtual":     {"path": virtual},
	}
	for name, scope := range listings {
		for _, sortOrder := range browsePositionSorts {
			for _, size := range []int{1, 2, 3, 5, 100} {
				t.Run(fmt.Sprintf("%s/%s/%d", name, sortOrder, size), func(t *testing.T) {
					t.Parallel()
					assertOffsetsMatchWalk(t, f, withSort(scope, sortOrder), size)
				})
			}
		}
	}
}

func TestBrowseOffsetMatchesCursorWalkOnMergedRoot(t *testing.T) {
	t.Parallel()
	f := newBrowsePositionFixture(t)
	scope := map[string]any{"systems": []string{"PSX"}, "rootView": browseRootViewContents}
	for _, sortOrder := range browsePositionSorts {
		for _, size := range []int{1, 2, 3, 5, 100} {
			t.Run(fmt.Sprintf("%s/%d", sortOrder, size), func(t *testing.T) {
				t.Parallel()
				assertOffsetsMatchWalk(t, f, withSort(scope, sortOrder), size)
			})
		}
	}
}

// A folder of numbered files turns on the prefix sort modes, whose order an
// offset and an anchor have to follow as well.
func TestBrowsePositionFollowsPrefixSortModes(t *testing.T) {
	t.Parallel()
	f := newDirIndexFixture(t, [][]string{{}}, [][]string{{
		"10 - Tenth.chd", "02 - Second.chd", "01 - First.chd", "03 - Third.chd", "11 - Eleventh.chd",
		"04 - Fourth.chd", "05 - Fifth.chd", "06 - Sixth.chd", "07 - Seventh.chd", "08 - Eighth.chd",
	}})
	scope := map[string]any{"path": filepath.Join(f.roots[0], "PSX"), "systems": []string{"PSX"}}
	for _, sortOrder := range []string{"", "name-desc"} {
		sorted := withSort(scope, sortOrder)
		assertOffsetsMatchWalk(t, f, sorted, 3)
		flat := flattenPages(walkPages(t, f, sorted, 3))
		assertAnchorsFindEveryEntry(t, f, sorted, flat, 3)
	}
}

func assertAnchorFound(
	t *testing.T, f *dirIndexFixture, scope map[string]any, flat []models.BrowseEntry, anchor string, index, size int,
) {
	t.Helper()
	got := f.browse(t, scope, map[string]any{"maxResults": size, "anchorPath": anchor})
	require.NotNil(t, got.Pagination, anchor)
	require.NotNil(t, got.Pagination.AnchorIndex, anchor)
	assert.Equal(t, index, *got.Pagination.AnchorIndex, anchor)
	start := index / size * size
	require.NotNil(t, got.Pagination.Offset, anchor)
	assert.Equal(t, start, *got.Pagination.Offset, anchor)
	assert.Equal(t, flat[start:min(start+size, len(flat))], got.Entries, anchor)
	assert.Equal(t, flat[start:], followCursors(t, f, scope, size, &got), anchor)
}

func assertAnchorsFindEveryEntry(
	t *testing.T, f *dirIndexFixture, scope map[string]any, flat []models.BrowseEntry, size int,
) {
	t.Helper()
	for i := range flat {
		assertAnchorFound(t, f, scope, flat, flat[i].Path, i, size)
		if flat[i].Type == "directory" {
			// A saved path may be below a folder collapsed to one game.
			base := filepath.Base(flat[i].Path)
			assertAnchorFound(t, f, scope, flat, filepath.Join(flat[i].Path, base+".chd"), i, size)
			assertAnchorFound(t, f, scope, flat, filepath.Join(flat[i].Path, "Disc 1", "Deep.chd"), i, size)
			assertAnchorFound(t, f, scope, flat, flat[i].Path+"/", i, size)
		}
	}
}

func TestBrowseAnchorFindsEveryEntry(t *testing.T) {
	t.Parallel()
	f := newBrowsePositionFixture(t)
	virtual := addVirtualRoute(t, f, "steam://300/Zeta", "steam://100/Alpha", "steam://200/Mid")

	listings := map[string]map[string]any{
		"single path": {"path": filepath.Join(f.roots[0], "PSX"), "systems": []string{"PSX"}},
		"merged root": {"systems": []string{"PSX"}, "rootView": browseRootViewContents},
		"virtual":     {"path": virtual},
	}
	for name, scope := range listings {
		for _, sortOrder := range browsePositionSorts {
			t.Run(name+"/"+sortOrder, func(t *testing.T) {
				t.Parallel()
				sorted := withSort(scope, sortOrder)
				// The merged root's virtual route leads its first page but is
				// outside the listing, so it has no position to find.
				flat := slices.DeleteFunc(flattenPages(walkPages(t, f, sorted, 100)), func(e models.BrowseEntry) bool {
					return e.Path == virtual
				})
				require.NotEmpty(t, flat)
				for _, size := range []int{1, 3, 7} {
					assertAnchorsFindEveryEntry(t, f, sorted, flat, size)
				}
			})
		}
	}
}

// The merged root folds a name both routes hold, so an anchor below either root
// lands on the one listed entry.
func TestBrowseAnchorOnMergedRootFindsFoldedFolderUnderEitherRoute(t *testing.T) {
	t.Parallel()
	f := newBrowsePositionFixture(t)
	scope := map[string]any{"systems": []string{"PSX"}, "rootView": browseRootViewContents}
	flat := flattenPages(walkPages(t, f, scope, 100))

	index := -1
	for i := range flat {
		if filepath.Base(flat[i].Path) == "Shared Folder" {
			require.Equal(t, -1, index, "the merge lists the folder once")
			index = i
		}
	}
	require.GreaterOrEqual(t, index, 0)
	for _, root := range f.roots {
		assertAnchorFound(t, f, scope, flat, filepath.Join(root, "PSX", "Shared Folder"), index, 4)
		assertAnchorFound(t, f, scope, flat, filepath.Join(root, "PSX", "Shared Folder", "Shared Folder.chd"), index, 4)
	}
}

// A file the merge dropped in favor of the higher-priority route is not listed.
func TestBrowseAnchorOnMergedRootTreatsShadowedFileAsNotListed(t *testing.T) {
	t.Parallel()
	f := newBrowsePositionFixture(t)
	scope := map[string]any{"systems": []string{"PSX"}, "rootView": browseRootViewContents}
	flat := flattenPages(walkPages(t, f, scope, 100))

	listed := make(map[string]int, len(flat))
	for i := range flat {
		listed[flat[i].Path] = i
	}
	found := 0
	for _, root := range f.roots {
		path := filepath.Join(root, "PSX", "Shared Game.chd")
		index, ok := listed[path]
		if ok {
			found++
			assertAnchorFound(t, f, scope, flat, path, index, 4)
			continue
		}
		got := f.browse(t, scope, map[string]any{"maxResults": 4, "anchorPath": path})
		assert.Nil(t, got.Pagination.AnchorIndex, path)
	}
	assert.Equal(t, 1, found, "the fixture holds the name in both routes")
}

func TestBrowseAnchorNotListedAnswersAsFirstPage(t *testing.T) {
	t.Parallel()
	f := newBrowsePositionFixture(t)
	path := filepath.Join(f.roots[0], "PSX")
	scope := map[string]any{"path": path, "systems": []string{"PSX"}}
	first := f.browse(t, scope, map[string]any{"maxResults": 3})
	require.NotNil(t, first.Pagination.Offset)
	assert.Equal(t, 0, *first.Pagination.Offset)

	for name, anchor := range map[string]string{
		"missing file":    filepath.Join(path, "Nope (USA).chd"),
		"missing folder":  filepath.Join(path, "Nope", "Nope.chd"),
		"other root":      filepath.Join(f.roots[1], "PSX", "Yoshi (USA).chd"),
		"parent":          filepath.Dir(path),
		"the prefix":      path,
		"prefix slashed":  path + "/",
		"similar sibling": path + "2/Zed (USA).chd",
	} {
		got := f.browse(t, scope, map[string]any{"maxResults": 3, "anchorPath": anchor})
		assert.Equal(t, first, got, name)
	}
}

func TestBrowseAnchorRootVirtualEntriesOnlyOnFirstPage(t *testing.T) {
	t.Parallel()
	f := newBrowsePositionFixture(t)
	virtual := addVirtualRoute(t, f, "steam://300/Zeta")
	scope := map[string]any{"systems": []string{"PSX"}, "rootView": browseRootViewContents}

	first := f.browse(t, scope, map[string]any{"maxResults": 3})
	require.Equal(t, virtual, first.Entries[0].Path, "the route leads the first page")
	require.Len(t, first.Entries, 4, "and is outside maxResults")
	assert.Equal(t, 0, *first.Pagination.Offset)
	listed := first.Entries[1:]

	miss := f.browse(t, scope, map[string]any{"maxResults": 3, "anchorPath": virtual + "404/Nope"})
	assert.Equal(t, first, miss, "a miss is a first page, route included")

	atZero := f.browse(t, scope, map[string]any{"maxResults": 3, "offset": 0})
	assert.Equal(t, listed, atZero.Entries, "an offset page carries no route")
	assert.Equal(t, first.TotalDirs, atZero.TotalDirs)
	assert.Equal(t, first.TotalFiles, atZero.TotalFiles)

	hit := f.browse(t, scope, map[string]any{"maxResults": 3, "anchorPath": listed[0].Path})
	assert.Equal(t, listed, hit.Entries, "an anchor hit carries no route")
	assert.Equal(t, 0, *hit.Pagination.AnchorIndex)
}

func TestBrowseAnchorRespectsHiddenRows(t *testing.T) {
	t.Parallel()
	f := newBrowsePositionFixture(t)
	scope := map[string]any{"path": filepath.Join(f.roots[0], "PSX"), "systems": []string{"PSX"}}
	flat := flattenPages(walkPages(t, f, scope, 100))

	var hiddenFile, hiddenDir *models.BrowseEntry
	for i := range flat {
		switch filepath.Base(flat[i].Path) {
		case "Halo (USA).chd":
			hiddenFile = &flat[i]
		case "Bushido Blade (USA)":
			hiddenDir = &flat[i]
		}
	}
	require.NotNil(t, hiddenFile)
	require.NotNil(t, hiddenDir)
	for _, entry := range []*models.BrowseEntry{hiddenFile, hiddenDir} {
		require.NotZero(t, entry.MediaID)
		_, err := HandleMediaTagsUpdate(withParams(&f.env, fmt.Sprintf(
			`{"mediaId":%d,"add":["user:hidden"]}`, entry.MediaID)))
		require.NoError(t, err)
	}

	visible := flattenPages(walkPages(t, f, scope, 100))
	assert.Len(t, visible, len(flat)-2)
	withHidden := mergeParams(scope, map[string]any{"includeHidden": true})
	everything := flattenPages(walkPages(t, f, withHidden, 100))
	assert.Len(t, everything, len(flat))

	for _, anchor := range []string{hiddenFile.Path, hiddenDir.Path} {
		got := f.browse(t, scope, map[string]any{"maxResults": 4, "anchorPath": anchor})
		assert.Nil(t, got.Pagination.AnchorIndex, anchor)
		assert.Equal(t, 0, *got.Pagination.Offset, anchor)
	}
	assertAnchorsFindEveryEntry(t, f, scope, visible, 4)
	assertAnchorsFindEveryEntry(t, f, withHidden, everything, 4)
	assertOffsetsMatchWalk(t, f, scope, 3)
	assertOffsetsMatchWalk(t, f, withHidden, 3)
}

func TestBrowsePositionRespectsTagFilters(t *testing.T) {
	t.Parallel()
	f := newBrowsePositionFixture(t)
	scope := map[string]any{"path": filepath.Join(f.roots[0], "PSX"), "systems": []string{"PSX"}}
	flat := flattenPages(walkPages(t, f, scope, 100))

	var tagged, untagged *models.BrowseEntry
	for i := range flat {
		switch filepath.Base(flat[i].Path) {
		case "Zed (USA).chd", "Duke (USA).chd":
			_, err := HandleMediaTagsUpdate(withParams(&f.env, fmt.Sprintf(
				`{"mediaId":%d,"add":["user:favorite"]}`, flat[i].MediaID)))
			require.NoError(t, err)
			tagged = &flat[i]
		case "Halo (USA).chd":
			untagged = &flat[i]
		}
	}
	require.NotNil(t, tagged)
	require.NotNil(t, untagged)

	filtered := mergeParams(scope, map[string]any{"tags": []string{"user:favorite"}})
	listed := flattenPages(walkPages(t, f, filtered, 100))
	assert.Equal(t, 2, f.browse(t, filtered, nil).TotalFiles)

	got := f.browse(t, filtered, map[string]any{"maxResults": 3, "anchorPath": untagged.Path})
	assert.Nil(t, got.Pagination.AnchorIndex, "a file the tags filter out reports no position")

	for i := range listed {
		assertAnchorFound(t, f, filtered, listed, listed[i].Path, i, 3)
	}
	assertOffsetsMatchWalk(t, f, filtered, 2)
}

func TestBrowsePositionSystemFilterHidesOtherSystems(t *testing.T) {
	t.Parallel()
	f := newBrowsePositionFixture(t)
	path := filepath.Join(f.roots[0], "PSX")
	other := filepath.Join(path, "Other System.chd")
	scantest.IndexMediaPathsWithOpts(t, f.env.Database.MediaDB, "SNES",
		database.ScanReconcileOpts{IncompleteScan: true}, other)

	scope := map[string]any{"path": path, "systems": []string{"PSX"}}
	got := f.browse(t, scope, map[string]any{"maxResults": 3, "anchorPath": other})
	assert.Nil(t, got.Pagination.AnchorIndex)

	all := map[string]any{"path": path}
	flat := flattenPages(walkPages(t, f, all, 100))
	for i := range flat {
		if flat[i].Path == other {
			assertAnchorFound(t, f, all, flat, other, i, 3)
			return
		}
	}
	require.Fail(t, "an unfiltered listing shows every system")
}

func TestBrowsePositionRejectedCombinations(t *testing.T) {
	t.Parallel()
	f := newBrowsePositionFixture(t)
	path := filepath.Join(f.roots[0], "PSX")

	for name, params := range map[string]map[string]any{
		"offset with anchor":     {"path": path, "offset": 0, "anchorPath": path + "/a"},
		"offset with cursor":     {"path": path, "offset": 0, "cursor": "abc"},
		"anchor with cursor":     {"path": path, "anchorPath": path + "/a", "cursor": "abc"},
		"offset with letter":     {"path": path, "offset": 0, "letter": "A"},
		"anchor with letter":     {"path": path, "anchorPath": path + "/a", "letter": "A"},
		"offset with live":       {"path": path, "offset": 0, "live": true},
		"anchor with live":       {"path": path, "anchorPath": path + "/a", "live": true},
		"offset on routes":       {"offset": 0},
		"anchor on routes":       {"anchorPath": path + "/a"},
		"offset on system route": {"offset": 0, "systems": []string{"PSX"}, "rootView": browseRootViewRoutes},
		"anchor on empty path":   {"path": "", "anchorPath": path + "/a"},
		"negative offset":        {"path": path, "offset": -1},
		"empty anchor":           {"path": path, "anchorPath": ""},
	} {
		raw, err := json.Marshal(params)
		require.NoError(t, err)
		_, err = HandleMediaBrowse(withParams(&f.env, string(raw)))
		require.Error(t, err, name)
		var clientErr *models.ClientError
		assert.ErrorAsf(t, err, &clientErr, "%s: %v", name, err)
	}
}

func TestBrowsePositionFieldsSerialize(t *testing.T) {
	t.Parallel()
	f := newBrowsePositionFixture(t)
	scope := map[string]any{"path": filepath.Join(f.roots[0], "PSX"), "systems": []string{"PSX"}}

	first, err := json.Marshal(f.browse(t, scope, map[string]any{"maxResults": 2}))
	require.NoError(t, err)
	assert.Contains(t, string(first), `"offset":0`, "serialized even at zero")
	assert.NotContains(t, string(first), "anchorIndex")

	page := f.browse(t, scope, map[string]any{"maxResults": 2})
	following := f.browse(t, scope, map[string]any{"maxResults": 2, "cursor": *page.Pagination.NextCursor})
	assert.Nil(t, following.Pagination.Offset, "a cursor page reports no position")

	lettered := f.browse(t, scope, map[string]any{"maxResults": 2, "letter": "H"})
	assert.Nil(t, lettered.Pagination.Offset, "a letter page reports no position")

	search, err := json.Marshal(models.PaginationInfo{PageSize: 2})
	require.NoError(t, err)
	assert.NotContains(t, string(search), "offset", "search shares the type and never sets it")
	assert.NotContains(t, string(search), "anchorIndex")
}

// newBrowseCountMock is a flat listing that counts its total only when asked.
func newBrowseCountMock(t *testing.T) (*helpers.MockMediaDBI, *mocks.MockPlatform) {
	t.Helper()
	platform := mocks.NewMockPlatform()
	platform.On("SupportedReaders", mock.Anything).Return(nil)
	platform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{browseTestAbsPath("roms")})
	platform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{{ID: "Steam", SystemID: "pc", Schemes: []string{"steam"}}})

	mediaDB := helpers.NewMockMediaDBI()
	rows := make([]database.SearchResultWithCursor, 0, 3)
	for i := range 3 {
		rows = append(rows, database.SearchResultWithCursor{
			SystemID: "pc", Name: "Game", Path: fmt.Sprintf("steam://%d", i), MediaID: int64(i + 1),
		})
	}
	mediaDB.On("BrowseFiles", mock.Anything, mock.Anything).Return(rows, nil)
	mediaDB.On("BrowseFileCount", mock.Anything, mock.Anything).Return(50, nil)
	return mediaDB, platform
}

func browseMockPage(
	t *testing.T, mediaDB *helpers.MockMediaDBI, platform *mocks.MockPlatform, params map[string]any,
) models.BrowseResults {
	t.Helper()
	result, err := HandleMediaBrowse(newBrowseEnv(t, mediaDB, platform, params))
	require.NoError(t, err)
	page, ok := result.(models.BrowseResults)
	require.True(t, ok)
	return page
}

// A client detects support from a first page, which may hold only folders.
func TestBrowseFirstPageOfFoldersReportsItsPosition(t *testing.T) {
	t.Parallel()
	f := newDirIndexFixture(t, [][]string{{"Alpha", "Bravo"}}, nil)
	scope := map[string]any{"path": filepath.Join(f.roots[0], "PSX"), "systems": []string{"PSX"}}

	first := f.browse(t, scope, nil)
	require.Len(t, first.Entries, 2)
	require.Zero(t, first.TotalFiles)
	require.NotNil(t, first.Pagination)
	require.NotNil(t, first.Pagination.Offset)
	assert.Equal(t, 0, *first.Pagination.Offset)
	assert.False(t, first.Pagination.HasNextPage)
}

func TestBrowsePositionReusesFirstPageTotals(t *testing.T) {
	t.Parallel()
	mediaDB, platform := newBrowseCountMock(t)
	mediaDB.On("BrowseAnchorPosition", mock.Anything, mock.Anything).
		Return(database.BrowseAnchorResult{Found: true, Position: 7}, nil)
	const path = "steam://"

	first := browseMockPage(t, mediaDB, platform, map[string]any{"path": path, "maxResults": 2})
	assert.Equal(t, 50, first.TotalFiles)
	mediaDB.AssertNumberOfCalls(t, "BrowseFileCount", 1)

	offset := browseMockPage(t, mediaDB, platform, map[string]any{"path": path, "maxResults": 2, "offset": 40})
	assert.Equal(t, 50, offset.TotalFiles)
	anchored := browseMockPage(t, mediaDB, platform, map[string]any{
		"path": path, "maxResults": 2, "anchorPath": "steam://7",
	})
	assert.Equal(t, 50, anchored.TotalFiles)
	assert.Equal(t, 7, *anchored.Pagination.AnchorIndex)
	assert.Equal(t, 6, *anchored.Pagination.Offset)
	mediaDB.AssertNumberOfCalls(t, "BrowseFileCount", 1)
}

func TestBrowsePositionCountsOnceWithoutAFirstPage(t *testing.T) {
	t.Parallel()
	mediaDB, platform := newBrowseCountMock(t)
	params := map[string]any{"path": "steam://", "maxResults": 2, "offset": 10}

	for range 3 {
		page := browseMockPage(t, mediaDB, platform, params)
		assert.Equal(t, 50, page.TotalFiles)
	}
	mediaDB.AssertNumberOfCalls(t, "BrowseFileCount", 1)

	// Another sort is another listing.
	params["sort"] = "filename-asc"
	browseMockPage(t, mediaDB, platform, params)
	mediaDB.AssertNumberOfCalls(t, "BrowseFileCount", 2)
}

func TestBrowseIndexIsBuiltOncePerListing(t *testing.T) {
	t.Parallel()
	mediaDB, platform := newBrowseCountMock(t)
	mediaDB.On("BrowseIndex", mock.Anything, mock.Anything).Return(database.BrowseIndexResult{
		Scheme: "latin", Buckets: []database.BrowseIndexBucket{{Key: "A", Count: 1}},
	}, nil)
	params := map[string]any{"path": "steam://"}

	for range 3 {
		_, err := HandleMediaBrowseIndex(newBrowseEnv(t, mediaDB, platform, params))
		require.NoError(t, err)
	}
	mediaDB.AssertNumberOfCalls(t, "BrowseIndex", 1)

	params["sort"] = "name-desc"
	_, err := HandleMediaBrowseIndex(newBrowseEnv(t, mediaDB, platform, params))
	require.NoError(t, err)
	mediaDB.AssertNumberOfCalls(t, "BrowseIndex", 2)
}

func TestBrowseListingCacheFollowsVisibilityAndReindex(t *testing.T) {
	t.Parallel()
	f := newBrowsePositionFixture(t)
	path := filepath.Join(f.roots[0], "PSX")
	scope := map[string]any{"path": path, "systems": []string{"PSX"}}
	cache := f.env.Database.MediaDB.BrowseListingCache()

	first := f.browse(t, scope, map[string]any{"maxResults": 3})
	f.index(t, scope)
	require.Equal(t, 1, cache.Len(), "a page and an index of one listing share an entry")

	flat := flattenPages(walkPages(t, f, scope, 100))
	last := flat[len(flat)-1]
	_, err := HandleMediaTagsUpdate(withParams(&f.env, fmt.Sprintf(
		`{"mediaId":%d,"add":["user:hidden"]}`, last.MediaID)))
	require.NoError(t, err)
	assert.Zero(t, cache.Len(), "a visibility change clears the listings")

	afterHide := f.browse(t, scope, map[string]any{"maxResults": 3, "offset": 3})
	assert.Equal(t, first.TotalFiles-1, afterHide.TotalFiles)

	added := filepath.Join(path, "Added Later.chd")
	scantest.IndexMediaPathsWithOpts(t, f.env.Database.MediaDB, "PSX",
		database.ScanReconcileOpts{IncompleteScan: true}, added)
	require.NoError(t, f.env.Database.MediaDB.PopulateBrowseCache(context.Background()))
	assert.Zero(t, cache.Len(), "indexing clears the listings")

	afterIndex := f.browse(t, scope, map[string]any{"maxResults": 3, "anchorPath": added})
	require.NotNil(t, afterIndex.Pagination.AnchorIndex)
	assert.Equal(t, first.TotalFiles, afterIndex.TotalFiles)
}
