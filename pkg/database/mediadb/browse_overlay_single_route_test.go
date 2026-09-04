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

// singleRouteFixture is one system with media under the route the overlay
// browses and under an unrelated directory that must never leak into a result,
// plus an empty route used only as the merge oracle's second source.
type singleRouteFixture struct {
	mediaDB *MediaDB
	route   string
	empty   string
	system  systemdefs.System
}

// The empty second route is the oracle for every collapse assertion below. It
// holds no media, so it can neither shadow nor dedupe anything, which makes a
// two-route overlay over it exactly the one-route overlay - except that it goes
// through the merge statement rather than the collapsed one. Comparing the two
// runs the production merge against the collapsed shape on the same data.
func (f *singleRouteFixture) sole() *database.BrowseOverlay {
	return &database.BrowseOverlay{Sources: []database.BrowseSource{
		{PathPrefix: f.route, IncludeDirs: true},
	}}
}

func (f *singleRouteFixture) merged() *database.BrowseOverlay {
	return &database.BrowseOverlay{Sources: []database.BrowseSource{
		{PathPrefix: f.route, IncludeDirs: true},
		{PathPrefix: f.empty, IncludeDirs: true},
	}}
}

func (f *singleRouteFixture) systems() []systemdefs.System {
	return []systemdefs.System{f.system}
}

func setupSingleRouteFixture(t *testing.T) (fixture *singleRouteFixture, cleanup func()) {
	t.Helper()
	mediaDB, cleanup := setupTempMediaDB(t)

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	route := browseTestDir("games", "NES")
	empty := browseTestDir("empty", "NES")
	elsewhere := browseTestDir("elsewhere", "NES")

	require.NoError(t, mediaDB.BeginTransaction(false))
	regionType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "region"})
	require.NoError(t, err)
	usa, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: regionType.DBID, Tag: "usa"})
	require.NoError(t, err)

	insert := func(name, path string, tagged bool) {
		t.Helper()
		title, titleErr := mediaDB.InsertMediaTitle(&database.MediaTitle{
			SystemDBID: system.DBID,
			Slug:       slugs.Slugify(nesSystem.GetMediaType(), name+path),
			Name:       name,
		})
		require.NoError(t, titleErr)
		row, mediaErr := mediaDB.InsertMedia(database.Media{
			SystemDBID:     system.DBID,
			MediaTitleDBID: title.DBID,
			Path:           path,
			ParentDir:      filepath.ToSlash(filepath.Dir(path)) + "/",
			SortName:       name,
		})
		require.NoError(t, mediaErr)
		if !tagged {
			return
		}
		_, tagErr := mediaDB.InsertMediaTag(database.MediaTag{MediaDBID: row.DBID, TagDBID: usa.DBID})
		require.NoError(t, tagErr)
	}

	// Direct files, spread across letters so the letter filter has something to
	// exclude, and one of them tagged so the tag filter does too.
	insert("Astro Blaster", route+"Astro Blaster.nes", true)
	insert("Aqua Jet", route+"Aqua Jet.nes", false)
	insert("Bubble Racer", route+"Bubble Racer.nes", false)
	insert("Comet Chase", route+"Comet Chase.nes", true)
	// A subdirectory, and a directory whose name is also a direct file. With one
	// route nothing can shadow either, and both must survive the collapse.
	insert("Inside Collection", route+"Collection/Inside Collection.nes", false)
	insert("Shadow", route+"Shadow", false)
	insert("Inside Shadow", route+"Shadow/Inside Shadow.nes", false)
	insert("Unrelated", elsewhere+"Unrelated.nes", false)
	require.NoError(t, mediaDB.CommitTransaction())

	return &singleRouteFixture{
		mediaDB: mediaDB,
		system:  *nesSystem,
		route:   route,
		empty:   empty,
	}, cleanup
}

// TestBrowseOverlaySingleRoute_MatchesTheMergeStatement is the correctness guard
// for the #1398 collapse: a one-route overlay skips the merge machinery, so the
// results have to be the ones the merge would have produced.
func TestBrowseOverlaySingleRoute_MatchesTheMergeStatement(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	letter := "A"
	tags := []zapscript.TagFilter{{Type: "region", Value: "usa"}}

	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cache=%t", cached), func(t *testing.T) {
			t.Parallel()
			fixture, cleanup := setupSingleRouteFixture(t)
			defer cleanup()
			if cached {
				require.NoError(t, sqlPopulateBrowseCache(ctx, fixture.mediaDB.sql.Load()))
			}
			db := fixture.mediaDB

			t.Run("file count", func(t *testing.T) {
				for _, tc := range []struct {
					letter *string
					name   string
					tags   []zapscript.TagFilter
				}{
					{name: "unfiltered"},
					{name: "letter", letter: &letter},
					{name: "tags", tags: tags},
				} {
					opts := func(overlay *database.BrowseOverlay) database.BrowseFileCountOptions {
						return database.BrowseFileCountOptions{
							Overlay: overlay,
							Letter:  tc.letter,
							Tags:    tc.tags,
							Systems: fixture.systems(),
						}
					}
					merged, err := db.BrowseFileCount(ctx, opts(fixture.merged()))
					require.NoError(t, err)
					sole, err := db.BrowseFileCount(ctx, opts(fixture.sole()))
					require.NoError(t, err)
					assert.Equal(t, merged, sole, "%s file count", tc.name)

					plain, err := db.BrowseFileCount(ctx, database.BrowseFileCountOptions{
						PathPrefix: fixture.route,
						Letter:     tc.letter,
						Tags:       tc.tags,
						Systems:    fixture.systems(),
					})
					require.NoError(t, err)
					assert.Equal(t, plain, sole, "%s file count against the plain browse", tc.name)
				}
			})

			t.Run("dir count", func(t *testing.T) {
				merged, err := db.BrowseDirCount(ctx, database.BrowseDirCountOptions{
					Overlay: fixture.merged(),
					Systems: fixture.systems(),
				})
				require.NoError(t, err)
				sole, err := db.BrowseDirCount(ctx, database.BrowseDirCountOptions{
					Overlay: fixture.sole(),
					Systems: fixture.systems(),
				})
				require.NoError(t, err)
				assert.Equal(t, merged, sole)
				assert.Equal(t, 2, sole, "Collection and Shadow are both directories under the route")
			})

			t.Run("directories", func(t *testing.T) {
				merged, err := db.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
					Overlay: fixture.merged(),
					Systems: fixture.systems(),
				})
				require.NoError(t, err)
				sole, err := db.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
					Overlay: fixture.sole(),
					Systems: fixture.systems(),
				})
				require.NoError(t, err)
				assert.Equal(t, merged, sole)
				require.Len(t, sole, 2)
				assert.Equal(t, fixture.route+"Collection", sole[0].Path,
					"the collapsed statement still returns the route-qualified path")
			})

			t.Run("directories paged", func(t *testing.T) {
				opts := func(overlay *database.BrowseOverlay) database.BrowseDirectoriesOptions {
					return database.BrowseDirectoriesOptions{
						Overlay:   overlay,
						AfterName: "Collection",
						Systems:   fixture.systems(),
						Limit:     1,
					}
				}
				merged, err := db.BrowseDirectories(ctx, opts(fixture.merged()))
				require.NoError(t, err)
				sole, err := db.BrowseDirectories(ctx, opts(fixture.sole()))
				require.NoError(t, err)
				assert.Equal(t, merged, sole)
				require.Len(t, sole, 1)
				assert.Equal(t, "Shadow", sole[0].Name)
			})

			t.Run("directories excluded route", func(t *testing.T) {
				excluded := &database.BrowseOverlay{Sources: []database.BrowseSource{
					{PathPrefix: fixture.route, IncludeDirs: false},
				}}
				dirs, err := db.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
					Overlay: excluded,
					Systems: fixture.systems(),
				})
				require.NoError(t, err)
				assert.Empty(t, dirs, "a route excluded from the listing contributes no directories")

				count, err := db.BrowseDirCount(ctx, database.BrowseDirCountOptions{
					Overlay: excluded,
					Systems: fixture.systems(),
				})
				require.NoError(t, err)
				assert.Equal(t, 0, count)
			})

			t.Run("files", func(t *testing.T) {
				for _, tc := range []struct {
					letter *string
					name   string
					sort   string
					tags   []zapscript.TagFilter
				}{
					{name: "unfiltered"},
					{name: "filename order", sort: "filename-asc"},
					{name: "descending", sort: "name-desc"},
					{name: "letter", letter: &letter},
					{name: "tags", tags: tags},
				} {
					opts := func(overlay *database.BrowseOverlay) *database.BrowseFilesOptions {
						return &database.BrowseFilesOptions{
							Overlay: overlay,
							Letter:  tc.letter,
							Tags:    tc.tags,
							Sort:    tc.sort,
							Systems: fixture.systems(),
							Limit:   10,
						}
					}
					merged, err := db.BrowseFiles(ctx, opts(fixture.merged()))
					require.NoError(t, err)
					sole, err := db.BrowseFiles(ctx, opts(fixture.sole()))
					require.NoError(t, err)
					assert.Equal(t, merged, sole, "%s files", tc.name)
					assert.NotEmpty(t, sole, "%s files", tc.name)
				}
			})

			t.Run("files paged", func(t *testing.T) {
				first, err := db.BrowseFiles(ctx, &database.BrowseFilesOptions{
					Overlay: fixture.sole(),
					Systems: fixture.systems(),
					Limit:   2,
				})
				require.NoError(t, err)
				require.Len(t, first, 2)

				opts := func(overlay *database.BrowseOverlay) *database.BrowseFilesOptions {
					return &database.BrowseFilesOptions{
						Overlay: overlay,
						Cursor: &database.BrowseCursor{
							SortValue: first[1].SortValue,
							LastID:    first[1].MediaID,
						},
						Systems: fixture.systems(),
						Limit:   10,
					}
				}
				merged, err := db.BrowseFiles(ctx, opts(fixture.merged()))
				require.NoError(t, err)
				sole, err := db.BrowseFiles(ctx, opts(fixture.sole()))
				require.NoError(t, err)
				assert.Equal(t, merged, sole)
				assert.NotEmpty(t, sole)
			})
		})
	}
}

// TestBrowseOverlayMultiRoute_StillMerges is the other half of the collapse: the
// merge only becomes optional because one route has nothing to merge with, so a
// real two-route overlay must still resolve names by priority.
func TestBrowseOverlayMultiRoute_StillMerges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	first := browseTestDir("first", "NES")
	second := browseTestDir("second", "NES")

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
	insert("Winner", first+"Duplicate.nes")
	insert("Loser", second+"Duplicate.nes")
	insert("Only Second", second+"Unique.nes")
	require.NoError(t, mediaDB.CommitTransaction())

	overlay := &database.BrowseOverlay{Sources: []database.BrowseSource{
		{PathPrefix: first, IncludeDirs: true},
		{PathPrefix: second, IncludeDirs: true},
	}}
	count, err := mediaDB.BrowseFileCount(ctx, database.BrowseFileCountOptions{
		Overlay: overlay,
		Systems: []systemdefs.System{*nesSystem},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, count, "the duplicate filename counts once across the two routes")

	files, err := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
		Overlay: overlay,
		Systems: []systemdefs.System{*nesSystem},
		Limit:   10,
	})
	require.NoError(t, err)
	require.Len(t, files, 2)
	paths := []string{files[0].Path, files[1].Path}
	assert.Contains(t, paths, first+"Duplicate.nes", "the higher-priority route wins the name")
	assert.NotContains(t, paths, second+"Duplicate.nes")
}
