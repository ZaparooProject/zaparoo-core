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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type browseTitleFixture struct {
	title    string
	filename string
}

var pandemoniumBrowseFixtures = []browseTitleFixture{
	{title: "Pandemonium 10", filename: "m-ten.nes"},
	{title: "pandemonium 2", filename: "a-two.nes"},
	{title: "Pandemonium: Bonus", filename: "c-bonus.nes"},
	{title: "Pandemonium?", filename: "y-question.nes"},
	{title: "Pandemonium!", filename: "z-original.nes"},
	{title: "Pandemonium 02", filename: "b-zero-two.nes"},
}

func browseSortIndexForTest(t testing.TB) secondaryIndex {
	t.Helper()
	for _, idx := range secondaryIndexes {
		if idx.name == browseSortIndexName {
			return idx
		}
	}
	t.Fatalf("secondary index %s not found", browseSortIndexName)
	return secondaryIndex{}
}

func replaceBrowseSortIndexWithBinary(t testing.TB, mediaDB *MediaDB) {
	t.Helper()
	_, err := mediaDB.sql.Load().ExecContext(context.Background(),
		"DROP INDEX IF EXISTS "+browseSortIndexName)
	require.NoError(t, err)
	_, err = mediaDB.sql.Load().ExecContext(context.Background(),
		"CREATE INDEX "+browseSortIndexName+" ON Media(ParentDir, IsMissing, SortName, DBID)")
	require.NoError(t, err)
}

func TestCompareBrowseTitles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{name: "punctuated original before sequel", left: "Pandemonium!", right: "Pandemonium 2"},
		{name: "numeric suffix", left: "Pandemonium 2", right: "Pandemonium 10"},
		{name: "leading numeric title", left: "2 Fast", right: "10 Fast"},
		{name: "case insensitive primary order", left: "alpha 2", right: "Alpha 10"},
		{name: "fewer leading zeroes first", left: "Game 2", right: "Game 02"},
		{name: "numbered sequel before subtitle", left: "Game 2", right: "Game: Bonus"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Negative(t, compareBrowseTitles(tt.left, tt.right))
			assert.Positive(t, compareBrowseTitles(tt.right, tt.left))
		})
	}

	assert.Zero(t, compareBrowseTitles("Pandemonium!", "Pandemonium!"))
	assert.Negative(t, compareBrowseTitles("Alpha", "alpha"),
		"raw title must provide a stable tiebreaker after case folding")
	assert.Negative(t,
		compareBrowseDirectoryNames("Pandemonium! (USA) (Rev 1)", "Pandemonium 2 (USA)"),
		"directory metadata must not outrank the punctuated base title")
}

func TestBrowseFiles_NaturalTitleOrderAndKeysetPagination(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()
	parentDir := browseTestDir("roms", "psx")

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "PSX", Name: "PSX"})
	require.NoError(t, err)
	for _, fixture := range pandemoniumBrowseFixtures {
		insertSystemMedia(t, mediaDB, system, fixture.title, parentDir+fixture.filename)
	}

	want := []string{
		"Pandemonium!",
		"Pandemonium?",
		"pandemonium 2",
		"Pandemonium 02",
		"Pandemonium 10",
		"Pandemonium: Bonus",
	}

	var (
		got    []string
		cursor *database.BrowseCursor
	)
	for {
		page, browseErr := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
			PathPrefix: parentDir,
			Cursor:     cursor,
			Limit:      2,
		})
		require.NoError(t, browseErr)
		if len(page) == 0 {
			break
		}
		for i := range page {
			got = append(got, page[i].Name)
		}
		last := page[len(page)-1]
		assert.Equal(t, last.Name, last.SortValue, "cursor keeps raw title compatibility")
		cursor = &database.BrowseCursor{
			SortValue: last.SortValue,
			SortMode:  last.SortMode,
			LastID:    last.MediaID,
		}
	}
	assert.Equal(t, want, got)

	desc, err := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
		PathPrefix: parentDir,
		Sort:       "name-desc",
		Limit:      len(want),
	})
	require.NoError(t, err)
	descNames := make([]string, len(desc))
	for i := range desc {
		descNames[i] = desc[i].Name
	}
	assert.Equal(t, []string{
		"Pandemonium: Bonus",
		"Pandemonium 10",
		"Pandemonium 02",
		"pandemonium 2",
		"Pandemonium?",
		"Pandemonium!",
	}, descNames)

	byFilename, err := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
		PathPrefix: parentDir,
		Sort:       "filename-asc",
		Limit:      len(want),
	})
	require.NoError(t, err)
	filenameNames := make([]string, len(byFilename))
	for i := range byFilename {
		filenameNames[i] = byFilename[i].Name
	}
	assert.Equal(t, []string{
		"pandemonium 2",
		"Pandemonium 02",
		"Pandemonium: Bonus",
		"Pandemonium 10",
		"Pandemonium?",
		"Pandemonium!",
	}, filenameNames, "explicit filename ordering must not use title collation")
}

func TestBrowseFiles_NaturalOrderWithLegacyBinaryIndex(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	parentDir := browseTestDir("roms", "psx")
	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "PSX", Name: "PSX"})
	require.NoError(t, err)
	insertSystemMedia(t, mediaDB, system, "Pandemonium 2", parentDir+"sequel.nes")
	insertSystemMedia(t, mediaDB, system, "Pandemonium!", parentDir+"original.nes")
	replaceBrowseSortIndexWithBinary(t, mediaDB)

	files, err := mediaDB.BrowseFiles(context.Background(), &database.BrowseFilesOptions{
		PathPrefix: parentDir,
		Limit:      10,
	})
	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Equal(t, []string{"Pandemonium!", "Pandemonium 2"}, []string{files[0].Name, files[1].Name},
		"natural SQL collation must stay correct while background optimization retains the legacy index")
}

func TestBrowseDirectories_NaturalOrderAndKeysetPagination(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()
	parentDir := browseTestDir("roms", "psx")
	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "PSX", Name: "PSX"})
	require.NoError(t, err)
	for _, name := range []string{
		"Pandemonium 10 (USA)",
		"Pandemonium 2 (USA)",
		"Pandemonium! (USA) (Rev 1)",
	} {
		insertSystemMedia(t, mediaDB, system, name,
			parentDir+name+"/"+name+".chd")
	}
	require.NoError(t, mediaDB.PopulateBrowseCache(ctx))

	systems := []systemdefs.System{{ID: "PSX"}}
	first, err := mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
		PathPrefix: parentDir,
		Systems:    systems,
		Limit:      1,
	})
	require.NoError(t, err)
	require.Len(t, first, 1)
	assert.Equal(t, "Pandemonium! (USA) (Rev 1)", first[0].Name)

	remaining, err := mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
		PathPrefix: parentDir,
		AfterName:  first[0].Name,
		Systems:    systems,
		Limit:      10,
	})
	require.NoError(t, err)
	require.Len(t, remaining, 2)
	assert.Equal(t, []string{"Pandemonium 2 (USA)", "Pandemonium 10 (USA)"},
		[]string{remaining[0].Name, remaining[1].Name})

	fallback, err := sqlBrowseDirectoriesForSystemsFromMedia(ctx, mediaDB.sql.Load(),
		database.BrowseDirectoriesOptions{PathPrefix: parentDir, Systems: systems})
	require.NoError(t, err)
	require.Len(t, fallback, 3)
	assert.Equal(t, []string{
		"Pandemonium! (USA) (Rev 1)",
		"Pandemonium 2 (USA)",
		"Pandemonium 10 (USA)",
	}, []string{fallback[0].Name, fallback[1].Name, fallback[2].Name})
}

func TestBrowseIndex_CaseInsensitiveNaturalOrderStaysContiguous(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	seedBrowseIndexMedia(t, mediaDB, "NES", []string{"Zulu", "bravo", "alpha 2", "Alpha"})

	files, err := mediaDB.BrowseFiles(context.Background(), &database.BrowseFilesOptions{
		PathPrefix: browseIndexTestDir,
		Sort:       "name-asc",
		Limit:      10,
	})
	require.NoError(t, err)
	names := make([]string, len(files))
	for i := range files {
		names[i] = files[i].Name
	}
	assert.Equal(t, []string{"Alpha", "alpha 2", "bravo", "Zulu"}, names)

	index, err := mediaDB.BrowseIndex(context.Background(), database.BrowseIndexOptions{
		PathPrefix: browseIndexTestDir,
		Sort:       "name-asc",
	})
	require.NoError(t, err)
	require.Len(t, index.Buckets, 3)
	assert.Equal(t, []string{"A", "B", "Z"}, []string{
		index.Buckets[0].Key,
		index.Buckets[1].Key,
		index.Buckets[2].Key,
	})
	assert.Equal(t, []int{2, 1, 1}, []int{
		index.Buckets[0].Count,
		index.Buckets[1].Count,
		index.Buckets[2].Count,
	})
	assert.Equal(t, []int{0, 2, 3}, []int{
		index.Buckets[0].Offset,
		index.Buckets[1].Offset,
		index.Buckets[2].Offset,
	})
	for _, bucket := range index.Buckets {
		assert.Equal(t, bucket.Key, firstBrowsedBucketForCursor(t, mediaDB, bucket, index.SortMode))
	}
}

func TestBrowseTitleCollationLeavesSearchOrderingSeparate(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Media.DBID", searchSortExpr(""))
	assert.Equal(t, "MediaTitles.Name COLLATE NOCASE", searchSortExpr("name-asc"))
}
