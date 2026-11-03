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

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type groupedTestFile struct {
	path   string
	region string
}

// insertGroupedTitle inserts one title with several media rows, the shape
// reconcile gives regional versions and copies of one game.
func insertGroupedTitle(
	t *testing.T, mediaDB *MediaDB, system database.System, name string, files ...groupedTestFile,
) int64 {
	t.Helper()

	sys, err := systemdefs.GetSystem(system.SystemID)
	require.NoError(t, err)
	regionType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "region"})
	require.NoError(t, err)

	require.NoError(t, mediaDB.BeginTransaction(false))
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: system.DBID,
		Slug:       slugs.Slugify(sys.GetMediaType(), name),
		Name:       name,
	})
	require.NoError(t, err)
	for _, file := range files {
		media, insertErr := mediaDB.InsertMedia(database.Media{
			SystemDBID:     system.DBID,
			MediaTitleDBID: title.DBID,
			Path:           file.path,
			ParentDir:      "/roms/" + system.SystemID + "/",
			SortName:       name,
		})
		require.NoError(t, insertErr)
		if file.region == "" {
			continue
		}
		tag, tagErr := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: regionType.DBID, Tag: file.region})
		require.NoError(t, tagErr)
		_, tagErr = mediaDB.InsertMediaTag(database.MediaTag{MediaDBID: media.DBID, TagDBID: tag.DBID})
		require.NoError(t, tagErr)
	}
	require.NoError(t, mediaDB.CommitTransaction())
	return title.DBID
}

func groupTitleIDs(groups []database.SearchTitleGroup) []int64 {
	ids := make([]int64, len(groups))
	for i := range groups {
		ids[i] = groups[i].TitleDBID
	}
	return ids
}

func TestSearchMediaTitleGroups_OneGroupPerTitleWithAllRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	nesRow, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	snesRow, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "SNES", Name: "SNES"})
	require.NoError(t, err)
	nes, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snes, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	marioNES := insertGroupedTitle(t, mediaDB, nesRow, "Super Mario Bros",
		groupedTestFile{path: "/roms/NES/Super Mario Bros (USA).nes", region: "us"},
		groupedTestFile{path: "/roms/NES/Super Mario Bros (Europe).nes", region: "eu"},
		groupedTestFile{path: "/roms/NES/Super Mario Bros (Japan).nes", region: "jp"},
	)
	marioSNES := insertGroupedTitle(t, mediaDB, snesRow, "Super Mario Bros",
		groupedTestFile{path: "/roms/SNES/Super Mario Bros.sfc"},
	)
	insertGroupedTitle(t, mediaDB, nesRow, "Zelda",
		groupedTestFile{path: "/roms/NES/Zelda.nes"},
	)

	groups, err := mediaDB.SearchMediaTitleGroups(ctx, &database.SearchFilters{
		Query:   "mario",
		Systems: []systemdefs.System{*nes, *snes},
		Sort:    "name-asc",
		Limit:   10,
	})
	require.NoError(t, err)
	require.Len(t, groups, 2, "the same name on two systems is two titles")
	assert.ElementsMatch(t, []int64{marioNES, marioSNES}, groupTitleIDs(groups))
	for i := range groups {
		for j := range groups[i].Rows {
			assert.Equal(t, groups[i].TitleDBID, groups[i].Rows[j].MediaTitleID)
		}
		if groups[i].TitleDBID == marioNES {
			assert.Len(t, groups[i].Rows, 3)
			assert.NotEmpty(t, groups[i].Rows[0].Tags, "rows carry tags for version selection")
		}
	}

	// A tag filter narrows the rows inside a title, not only which titles match.
	groups, err = mediaDB.SearchMediaTitleGroups(ctx, &database.SearchFilters{
		Query:   "mario",
		Systems: []systemdefs.System{*nes},
		Tags:    []zapscript.TagFilter{{Type: "region", Value: "eu", Operator: zapscript.TagOperatorAND}},
		Limit:   10,
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Rows, 1)
	assert.Equal(t, "/roms/NES/Super Mario Bros (Europe).nes", groups[0].Rows[0].Path)

	groups, err = mediaDB.SearchMediaTitleGroups(ctx, &database.SearchFilters{
		Query:   "mario",
		Systems: []systemdefs.System{*nes},
		Tags:    []zapscript.TagFilter{{Type: "region", Value: "jp", Operator: zapscript.TagOperatorNOT}},
		Limit:   10,
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Len(t, groups[0].Rows, 2)

	groups, err = mediaDB.SearchMediaTitleGroups(ctx, &database.SearchFilters{
		Systems:    []systemdefs.System{*nes, *snes},
		PathPrefix: "/roms/SNES",
		Limit:      10,
	})
	require.NoError(t, err)
	assert.Equal(t, []int64{marioSNES}, groupTitleIDs(groups))

	rows, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
		TitleDBID: marioNES,
		Systems:   []systemdefs.System{*snes},
		Query:     "unrelated",
		Sort:      "filename-asc",
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 3, "a title search ignores query and systems")
	assert.Equal(t, "/roms/NES/Super Mario Bros (Europe).nes", rows[0].Path)
}

// TestSearchMediaTitleGroups_PagesCoverTitlesExactlyOnce pages titles whose
// rows would straddle every row-page boundary, in each grouped order, with
// and without the slug cache.
func TestSearchMediaTitleGroups_PagesCoverTitlesExactlyOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	nes, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	const titles = 25
	for i := range titles {
		// Every few titles share a name, so name sorts tie at page edges.
		name := fmt.Sprintf("Paged Game %02d", i/3)
		files := make([]groupedTestFile, 0, 3)
		for c := range i%3 + 1 {
			files = append(files, groupedTestFile{path: fmt.Sprintf("/roms/NES/%s %d-%d.nes", name, i, c)})
		}
		insertGroupedTitle(t, mediaDB, system, name, files...)
	}
	insertGroupedTitle(t, mediaDB, system, "Unrelated", groupedTestFile{path: "/roms/NES/Unrelated.nes"})

	for _, cached := range []bool{false, true} {
		if cached {
			require.NoError(t, mediaDB.RebuildSlugSearchCache())
			require.True(t, mediaDB.slugSearchCache.Load().CanServeSystems([]string{"NES"}))
		}
		for _, query := range []string{"", "paged"} {
			for _, sortOrder := range []string{"", "name-asc", "name-desc"} {
				name := fmt.Sprintf("cached=%v query=%q sort=%q", cached, query, sortOrder)
				all, searchErr := mediaDB.SearchMediaTitleGroups(ctx, &database.SearchFilters{
					Query: query, Systems: []systemdefs.System{*nes}, Sort: sortOrder, Limit: 100,
				})
				require.NoError(t, searchErr, name)
				wantCount := titles
				if query == "" {
					wantCount++
				}
				require.Len(t, all, wantCount, name)
				for i := 1; i < len(all) && sortOrder != ""; i++ {
					order := compareTitleGroupKeys(
						titleGroupKey{sortValue: all[i-1].SortValue, titleDBID: all[i-1].TitleDBID},
						titleGroupKey{sortValue: all[i].SortValue, titleDBID: all[i].TitleDBID},
						sortOrder)
					assert.Negative(t, order, "%s: titles out of order at %d", name, i)
				}

				for _, limit := range []int{1, 2, 4, 7} {
					var got []int64
					var cursor *database.SearchCursor
					for page := 0; ; page++ {
						require.Less(t, page, wantCount+2, "%s: paging did not end", name)
						groups, pageErr := mediaDB.SearchMediaTitleGroups(ctx, &database.SearchFilters{
							Query: query, Systems: []systemdefs.System{*nes}, Sort: sortOrder,
							Limit: limit, SortCursor: cursor,
						})
						require.NoError(t, pageErr, name)
						if len(groups) == 0 {
							break
						}
						got = append(got, groupTitleIDs(groups)...)
						last := groups[len(groups)-1]
						cursor = &database.SearchCursor{
							Sort: sortOrder, SortValue: last.SortValue, LastID: last.TitleDBID,
						}
					}
					assert.Equal(t, groupTitleIDs(all), got, "%s limit %d", name, limit)
				}
			}
		}
	}
}

func TestSearchMediaTitleGroups_RejectsFilenameSort(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	nes, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	_, err = mediaDB.SearchMediaTitleGroups(context.Background(), &database.SearchFilters{
		Systems: []systemdefs.System{*nes}, Sort: "filename-asc", Limit: 10,
	})
	require.Error(t, err)
}
