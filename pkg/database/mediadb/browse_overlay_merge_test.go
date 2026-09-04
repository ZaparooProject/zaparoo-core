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
	"slices"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mergeFixture builds a multi-route overlay over hand-placed media so each rule
// the merge applies can be checked against a result worked out by hand.
type mergeFixture struct {
	mediaDB *MediaDB
	insert  func(name, path string)
	roots   []string
	system  systemdefs.System
}

func setupMergeFixture(t *testing.T, routes int) (fixture *mergeFixture, cleanup func()) {
	t.Helper()
	mediaDB, cleanup := setupTempMediaDB(t)

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	roots := make([]string, routes)
	for i := range roots {
		roots[i] = browseTestDir(string(rune('a'+i))+"root", "NES")
	}

	require.NoError(t, mediaDB.BeginTransaction(false))
	insert := func(name, path string) {
		t.Helper()
		title, titleErr := mediaDB.InsertMediaTitle(&database.MediaTitle{
			SystemDBID: system.DBID,
			Slug:       slugs.Slugify(nesSystem.GetMediaType(), name+path),
			Name:       name,
		})
		require.NoError(t, titleErr)
		_, mediaErr := mediaDB.InsertMedia(database.Media{
			SystemDBID:     system.DBID,
			MediaTitleDBID: title.DBID,
			Path:           path,
			ParentDir:      filepath.ToSlash(filepath.Dir(path)) + "/",
			SortName:       name,
		})
		require.NoError(t, mediaErr)
	}
	return &mergeFixture{mediaDB: mediaDB, insert: insert, roots: roots, system: *nesSystem}, cleanup
}

// commit closes the fixture's transaction and, when cached is set, builds the
// browse cache. The two-route count is answered from the cache when it can be
// and by the statement otherwise, so every rule is checked through both.
func (f *mergeFixture) commit(t *testing.T, cached bool) {
	t.Helper()
	require.NoError(t, f.mediaDB.CommitTransaction())
	if cached {
		require.NoError(t, sqlPopulateBrowseCache(context.Background(), f.mediaDB.sql.Load()))
	}
}

func (f *mergeFixture) overlay() *database.BrowseOverlay {
	sources := make([]database.BrowseSource, len(f.roots))
	for i, r := range f.roots {
		sources[i] = database.BrowseSource{PathPrefix: r, IncludeDirs: true}
	}
	return &database.BrowseOverlay{Sources: sources}
}

// mergeResult is what the two merge statements say about the same overlay: the
// paths the listing returns and the total the count reports. They are read
// together because the API shows both at once, so a rule applied by one and not
// the other is a user-visible inconsistency, not an internal detail.
func (f *mergeFixture) mergeResult(t *testing.T) (paths []string, total int) {
	t.Helper()
	ctx := context.Background()
	files, err := f.mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
		Overlay: f.overlay(),
		Systems: []systemdefs.System{f.system},
		Limit:   100,
	})
	require.NoError(t, err)
	for i := range files {
		paths = append(paths, files[i].Path)
	}
	total, err = f.mediaDB.BrowseFileCount(ctx, database.BrowseFileCountOptions{
		Overlay: f.overlay(),
		Systems: []systemdefs.System{f.system},
	})
	require.NoError(t, err)
	assert.Len(t, paths, total, "the count must agree with the rows the listing returns")
	return paths, total
}

// TestBrowseOverlayMerge_Rules pins every rule the merge applies, now that the
// ROW_NUMBER ranking that used to apply them has been replaced by predicates
// (#1398). Each case states the answer worked out by hand rather than comparing
// two implementations, so the test still means something if both change.
func TestBrowseOverlayMerge_Rules(t *testing.T) {
	t.Parallel()
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cache=%t", cached), func(t *testing.T) {
			t.Parallel()
			testBrowseOverlayMergeRules(t, cached)
		})
	}
}

func testBrowseOverlayMergeRules(t *testing.T, cached bool) {
	t.Helper()

	t.Run("higher priority route wins a duplicate name", func(t *testing.T) {
		t.Parallel()
		f, cleanup := setupMergeFixture(t, 2)
		defer cleanup()
		f.insert("Winner", f.roots[0]+"Dup.nes")
		f.insert("Loser", f.roots[1]+"Dup.nes")
		f.insert("Only Second", f.roots[1]+"Unique.nes")
		f.commit(t, cached)

		paths, total := f.mergeResult(t)
		assert.Equal(t, 2, total)
		assert.Contains(t, paths, f.roots[0]+"Dup.nes")
		assert.NotContains(t, paths, f.roots[1]+"Dup.nes")
		assert.Contains(t, paths, f.roots[1]+"Unique.nes")
	})

	t.Run("a name in three routes survives once from the first", func(t *testing.T) {
		t.Parallel()
		f, cleanup := setupMergeFixture(t, 3)
		defer cleanup()
		f.insert("First", f.roots[0]+"Triple.nes")
		f.insert("Second", f.roots[1]+"Triple.nes")
		f.insert("Third", f.roots[2]+"Triple.nes")
		f.commit(t, cached)

		paths, total := f.mergeResult(t)
		assert.Equal(t, 1, total)
		assert.Equal(t, []string{f.roots[0] + "Triple.nes"}, paths)
	})

	t.Run("a higher priority directory shadows a lower priority file", func(t *testing.T) {
		t.Parallel()
		f, cleanup := setupMergeFixture(t, 2)
		defer cleanup()
		f.insert("Inside", f.roots[0]+"Shadow.nes/inside.nes")
		f.insert("Shadowed", f.roots[1]+"Shadow.nes")
		f.insert("Survivor", f.roots[1]+"Kept.nes")
		f.commit(t, cached)

		paths, total := f.mergeResult(t)
		assert.Equal(t, 1, total, "only the unshadowed file counts as a file")
		assert.Equal(t, []string{f.roots[1] + "Kept.nes"}, paths)
	})

	t.Run("a lower priority directory shadows nothing", func(t *testing.T) {
		t.Parallel()
		f, cleanup := setupMergeFixture(t, 2)
		defer cleanup()
		f.insert("Kept", f.roots[0]+"Name.nes")
		f.insert("Inside", f.roots[1]+"Name.nes/inside.nes")
		f.commit(t, cached)

		paths, total := f.mergeResult(t)
		assert.Equal(t, 1, total)
		assert.Equal(t, []string{f.roots[0] + "Name.nes"}, paths)
	})

	// The interesting one. Ranking by anything other than priority gets this
	// wrong: route 2's file would win the name and then be shadowed away by
	// route 1's directory, losing route 0's file with it.
	t.Run("a shadowed middle route does not take the name from the first", func(t *testing.T) {
		t.Parallel()
		f, cleanup := setupMergeFixture(t, 3)
		defer cleanup()
		f.insert("Kept", f.roots[0]+"Name.nes")
		f.insert("Inside", f.roots[1]+"Name.nes/inside.nes")
		f.insert("Shadowed", f.roots[2]+"Name.nes")
		f.commit(t, cached)

		paths, total := f.mergeResult(t)
		assert.Equal(t, 1, total)
		assert.Equal(t, []string{f.roots[0] + "Name.nes"}, paths,
			"the first route's file is neither deduped nor shadowed")
	})

	t.Run("a missing row neither survives nor shadows", func(t *testing.T) {
		t.Parallel()
		f, cleanup := setupMergeFixture(t, 2)
		defer cleanup()
		f.insert("Present", f.roots[1]+"Dup.nes")
		f.insert("Gone", f.roots[0]+"Dup.nes")
		// Marked missing before the cache is built: a cache built from rows that
		// have since gone missing is stale by construction, which is a property
		// of the browse cache and not of the merge rule under test here.
		f.commit(t, false)
		ctx := context.Background()
		_, err := f.mediaDB.sql.Load().ExecContext(ctx,
			"UPDATE Media SET IsMissing = 1 WHERE Path = ?", f.roots[0]+"Dup.nes")
		require.NoError(t, err)
		if cached {
			require.NoError(t, sqlPopulateBrowseCache(ctx, f.mediaDB.sql.Load()))
		}

		paths, total := f.mergeResult(t)
		assert.Equal(t, 1, total)
		assert.Equal(t, []string{f.roots[1] + "Dup.nes"}, paths,
			"the surviving copy is the present one, not the higher-priority missing one")
	})

	t.Run("a same-named file in another system does not dedupe", func(t *testing.T) {
		t.Parallel()
		f, cleanup := setupMergeFixture(t, 2)
		defer cleanup()
		f.insert("Kept", f.roots[1]+"Shared.nes")
		f.commit(t, cached)

		ctx := context.Background()
		other, err := f.mediaDB.FindOrInsertSystem(database.System{SystemID: "SNES", Name: "SNES"})
		require.NoError(t, err)
		require.NoError(t, f.mediaDB.BeginTransaction(false))
		title, err := f.mediaDB.InsertMediaTitle(&database.MediaTitle{
			SystemDBID: other.DBID, Slug: "shared-other", Name: "Other System",
		})
		require.NoError(t, err)
		_, err = f.mediaDB.InsertMedia(database.Media{
			SystemDBID:     other.DBID,
			MediaTitleDBID: title.DBID,
			Path:           f.roots[0] + "Shared.nes",
			ParentDir:      f.roots[0],
			SortName:       "Other System",
		})
		require.NoError(t, err)
		require.NoError(t, f.mediaDB.CommitTransaction())

		paths, total := f.mergeResult(t)
		assert.Equal(t, 1, total, "the other system's row is outside the filter, so it cannot win the name")
		assert.Equal(t, []string{f.roots[1] + "Shared.nes"}, paths)
		_ = ctx
	})
}

// TestBrowseOverlayMerge_TwoRouteCountMatchesStatement pins the cached two-route
// total against the statement that reads both routes, which is the only other
// thing that can produce it. They are separate implementations of one rule, so a
// corpus exercising every part of the correction is what keeps them equal.
func TestBrowseOverlayMerge_TwoRouteCountMatchesStatement(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f, cleanup := setupMergeFixture(t, 2)
	defer cleanup()

	// Names only in one route, names in both, a directory in the first route
	// shadowing a file in the second, and a directory in the second shadowing
	// nothing.
	for i := range 30 {
		name := fmt.Sprintf("Both%02d", i)
		f.insert("First "+name, f.roots[0]+name+".nes")
		f.insert("Second "+name, f.roots[1]+name+".nes")
	}
	for i := range 10 {
		f.insert("First only", f.roots[0]+fmt.Sprintf("FirstOnly%02d.nes", i))
		f.insert("Second only", f.roots[1]+fmt.Sprintf("SecondOnly%02d.nes", i))
	}
	f.insert("Inside", f.roots[0]+"Shadowing.nes/inside.nes")
	f.insert("Shadowed", f.roots[1]+"Shadowing.nes")
	f.insert("Inside lower", f.roots[1]+"NotShadowing.nes/inside.nes")
	f.insert("Not shadowed", f.roots[0]+"NotShadowing.nes")
	f.commit(t, true)

	opts := database.BrowseFileCountOptions{
		Overlay: f.overlay(),
		Systems: []systemdefs.System{f.system},
	}

	cached, served, err := sqlBrowseTwoRouteFileCountFromCache(ctx, f.mediaDB.sql.Load(), opts)
	require.NoError(t, err)
	require.True(t, served, "the fixture must leave the cache able to answer the total")

	query, args := browseOverlayFileCountQuery(opts, browseTagPlan{})
	var fromStatement int
	require.NoError(t, f.mediaDB.sql.Load().QueryRowContext(ctx, query, args...).Scan(&fromStatement))

	// 30 shared names counted once, 10 in each route alone, and NotShadowing.nes
	// which only the first route holds as a file. Shadowing.nes is a directory in
	// the first route, so the second route's file of that name is dropped.
	assert.Equal(t, 51, fromStatement, "the statement's total")
	assert.Equal(t, fromStatement, cached, "the cached total must match the statement's")

	paths, total := f.mergeResult(t)
	assert.Equal(t, fromStatement, total)
	assert.Len(t, paths, total)
}

// TestBrowseOverlayMerge_FilenameSortPaging covers the sort whose branches seek
// by m.Path rather than by the filename they order on, so a cursor issued in
// filename terms has to be translated per route. Paging is where that would go
// wrong: a mistranslated seek repeats or skips rows rather than erroring.
func TestBrowseOverlayMerge_FilenameSortPaging(t *testing.T) {
	t.Parallel()

	for _, sortOrder := range []string{"filename-asc", "filename-desc"} {
		t.Run(sortOrder, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f, cleanup := setupMergeFixture(t, 2)
			defer cleanup()
			for i := range 25 {
				f.insert(fmt.Sprintf("First %02d", i), f.roots[0]+fmt.Sprintf("game-%02d.nes", i))
			}
			for i := 20; i < 40; i++ {
				f.insert(fmt.Sprintf("Second %02d", i), f.roots[1]+fmt.Sprintf("game-%02d.nes", i))
			}
			f.commit(t, true)

			total, err := f.mediaDB.BrowseFileCount(ctx, database.BrowseFileCountOptions{
				Overlay: f.overlay(),
				Systems: []systemdefs.System{f.system},
			})
			require.NoError(t, err)
			assert.Equal(t, 40, total, "game-20..24 are in both routes and count once")

			var (
				cursor *database.BrowseCursor
				order  []string
			)
			seen := make(map[string]struct{})
			for range 30 {
				page, pageErr := f.mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
					Overlay: f.overlay(),
					Systems: []systemdefs.System{f.system},
					Cursor:  cursor,
					Sort:    sortOrder,
					Limit:   6,
				})
				require.NoError(t, pageErr)
				if len(page) == 0 {
					break
				}
				for i := range page {
					_, dup := seen[page[i].Path]
					require.False(t, dup, "paged twice: %s", page[i].Path)
					seen[page[i].Path] = struct{}{}
					order = append(order, filepath.Base(page[i].Path))
				}
				last := page[len(page)-1]
				cursor = &database.BrowseCursor{SortValue: last.SortValue, LastID: last.MediaID}
			}
			assert.Len(t, seen, total, "paging must enumerate exactly the counted rows")

			sorted := slices.Clone(order)
			if sortOrder == "filename-desc" {
				slices.Sort(sorted)
				slices.Reverse(sorted)
			} else {
				slices.Sort(sorted)
			}
			assert.Equal(t, sorted, order, "pages must come back in filename order")
			// The duplicated names resolve to the first route, so the second
			// route only contributes the names it alone holds.
			assert.Contains(t, seen, f.roots[0]+"game-20.nes")
			assert.NotContains(t, seen, f.roots[1]+"game-20.nes")
		})
	}
}

// TestBrowseOverlayMerge_CountMatchesEnumeration is the invariant that catches a
// merge rule applied by one statement and not the other: media.browse shows the
// count as the total beside the rows it pages through, so they have to agree on
// a corpus with every rule in play at once.
func TestBrowseOverlayMerge_CountMatchesEnumeration(t *testing.T) {
	t.Parallel()

	f, cleanup := setupMergeFixture(t, 3)
	defer cleanup()
	for i := range 40 {
		name := string(rune('A'+i%26)) + string(rune('a'+i/26))
		f.insert("First "+name, f.roots[0]+name+".nes")
		if i%2 == 0 {
			f.insert("Second "+name, f.roots[1]+name+".nes")
		}
		if i%3 == 0 {
			f.insert("Third "+name, f.roots[2]+name+".nes")
		}
	}
	f.insert("Second only", f.roots[1]+"SecondOnly.nes")
	f.insert("Third only", f.roots[2]+"ThirdOnly.nes")
	f.insert("Inside", f.roots[0]+"Shadowed.nes/inside.nes")
	f.insert("Shadowed second", f.roots[1]+"Shadowed.nes")
	f.insert("Shadowed third", f.roots[2]+"Shadowed.nes")
	f.commit(t, true)

	ctx := context.Background()
	total, err := f.mediaDB.BrowseFileCount(ctx, database.BrowseFileCountOptions{
		Overlay: f.overlay(),
		Systems: []systemdefs.System{f.system},
	})
	require.NoError(t, err)
	// 40 names from the first route, plus the two names only later routes hold.
	// Shadowed.nes is a directory in the first route, so neither later route's
	// file of that name survives.
	assert.Equal(t, 42, total)

	seen := make(map[string]struct{})
	var cursor *database.BrowseCursor
	for range 20 {
		page, pageErr := f.mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
			Overlay: f.overlay(),
			Systems: []systemdefs.System{f.system},
			Cursor:  cursor,
			Limit:   7,
		})
		require.NoError(t, pageErr)
		if len(page) == 0 {
			break
		}
		for i := range page {
			_, dup := seen[page[i].Path]
			require.False(t, dup, "paged twice: %s", page[i].Path)
			seen[page[i].Path] = struct{}{}
		}
		last := page[len(page)-1]
		cursor = &database.BrowseCursor{SortValue: last.SortValue, LastID: last.MediaID}
	}
	assert.Len(t, seen, total, "paging must enumerate exactly the counted rows")
}
