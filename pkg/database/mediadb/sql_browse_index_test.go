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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// browseIndexTestDir is the ParentDir/PathPrefix the index test media live
// under, matching how insertSystemMedia derives ParentDir from the media path.
const browseIndexTestDir = "roms/nes/"

func seedBrowseIndexMedia(t *testing.T, mediaDB *MediaDB, systemID string, titles []string) {
	t.Helper()
	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: systemID, Name: systemID})
	require.NoError(t, err)
	for _, title := range titles {
		insertSystemMedia(t, mediaDB, system, title, filepath.Join("roms", "nes", title+".nes"))
	}
}

func browseIndexTestSystems(t *testing.T, systemIDs ...string) []systemdefs.System {
	t.Helper()
	systems := make([]systemdefs.System, 0, len(systemIDs))
	for _, id := range systemIDs {
		sys, err := systemdefs.GetSystem(id)
		require.NoError(t, err)
		systems = append(systems, *sys)
	}
	return systems
}

// firstBrowsedBucketForCursor seeks media.browse to the bucket's cursor and
// returns the canonical bucket of the first row of the resulting page.
func firstBrowsedBucketForCursor(
	t *testing.T, mediaDB *MediaDB, bucket *database.BrowseIndexBucket, sortMode string,
) string {
	t.Helper()
	opts := &database.BrowseFilesOptions{
		PathPrefix: browseIndexTestDir,
		Sort:       sortMode,
		Limit:      100,
	}
	if !bucket.AtStart {
		opts.Cursor = &database.BrowseCursor{
			SortValue: bucket.SortValue,
			LastID:    bucket.LastID,
			SortMode:  sortMode,
		}
	}
	files, err := mediaDB.BrowseFiles(context.Background(), opts)
	require.NoError(t, err)
	require.NotEmpty(t, files, "expected a page for bucket %q", bucket.Key)
	return BrowseNameFirstChar(files[0].Name)
}

func TestBrowseIndex_BucketsCountsAndSeek(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Binary ascending order: '#'(35) < '3'(51) < 'A'(65) < 'B' < 'Z'.
	seedBrowseIndexMedia(t, mediaDB, "NES", []string{
		"#Hash", "3D World", "Alpha", "Apex", "Bravo", "Zelda",
	})

	result, err := mediaDB.BrowseIndex(context.Background(), database.BrowseIndexOptions{
		PathPrefix: browseIndexTestDir,
		Sort:       "name-asc",
	})
	require.NoError(t, err)

	assert.Equal(t, "latin", result.Scheme)
	assert.Equal(t, "name-asc", result.SortMode)
	assert.Equal(t, 6, result.TotalFiles)

	keys := make([]string, len(result.Buckets))
	counts := make(map[string]int, len(result.Buckets))
	offsets := make(map[string]int, len(result.Buckets))
	for i, b := range result.Buckets {
		keys[i] = b.Key
		counts[b.Key] = b.Count
		offsets[b.Key] = b.Offset
	}
	assert.Equal(t, []string{"#", "0-9", "A", "B", "Z"}, keys, "buckets in scroll order")
	assert.Equal(t, map[string]int{"#": 1, "0-9": 1, "A": 2, "B": 1, "Z": 1}, counts)
	// Offset is the bucket's 0-based file position = cumulative count of earlier
	// buckets (#:0, 0-9:1, A:2, B:4, Z:5).
	assert.Equal(t, map[string]int{"#": 0, "0-9": 1, "A": 2, "B": 4, "Z": 5}, offsets)

	// The first bucket in the list begins the list (no preceding row).
	require.True(t, result.Buckets[0].AtStart)
	for _, b := range result.Buckets[1:] {
		assert.False(t, b.AtStart, "non-leading bucket %q should carry a cursor", b.Key)
	}

	// Each bucket's cursor must seek a media.browse page to that bucket's first row.
	for _, b := range result.Buckets {
		assert.Equalf(t, b.Key, firstBrowsedBucketForCursor(t, mediaDB, &b, result.SortMode),
			"cursor for bucket %q should land on bucket %q", b.Key, b.Key)
	}
}

func TestBrowseIndex_DescOrder(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	seedBrowseIndexMedia(t, mediaDB, "NES", []string{
		"#Hash", "3D World", "Alpha", "Apex", "Bravo", "Zelda",
	})

	result, err := mediaDB.BrowseIndex(context.Background(), database.BrowseIndexOptions{
		PathPrefix: browseIndexTestDir,
		Sort:       "name-desc",
	})
	require.NoError(t, err)

	assert.Equal(t, "latin", result.Scheme)
	keys := make([]string, len(result.Buckets))
	offsets := make(map[string]int, len(result.Buckets))
	for i, b := range result.Buckets {
		keys[i] = b.Key
		offsets[b.Key] = b.Offset
	}
	assert.Equal(t, []string{"Z", "B", "A", "0-9", "#"}, keys, "reversed for name-desc")
	// Offset is the bucket's 0-based position in the descending listing =
	// cumulative count of earlier buckets (Z:0, B:1, A:2, 0-9:4, #:5).
	assert.Equal(t, map[string]int{"Z": 0, "B": 1, "A": 2, "0-9": 4, "#": 5}, offsets)
	require.True(t, result.Buckets[0].AtStart, "Z begins a descending list")

	for _, b := range result.Buckets {
		assert.Equalf(t, b.Key, firstBrowsedBucketForCursor(t, mediaDB, &b, result.SortMode),
			"desc cursor for bucket %q should land on bucket %q", b.Key, b.Key)
	}
}

func TestBrowseIndex_SystemScoping(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	seedBrowseIndexMedia(t, mediaDB, "NES", []string{"Alpha", "Bravo"})
	seedBrowseIndexMedia(t, mediaDB, "SNES", []string{"Charlie", "Delta", "Echo"})

	result, err := mediaDB.BrowseIndex(context.Background(), database.BrowseIndexOptions{
		PathPrefix: browseIndexTestDir,
		Sort:       "name-asc",
		Systems:    browseIndexTestSystems(t, "SNES"),
	})
	require.NoError(t, err)

	assert.Equal(t, 3, result.TotalFiles, "only SNES media counted")
	keys := make([]string, len(result.Buckets))
	for i, b := range result.Buckets {
		keys[i] = b.Key
	}
	assert.Equal(t, []string{"C", "D", "E"}, keys)
}

func TestBrowseIndex_NonAlphabeticalSortReturnsNone(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	seedBrowseIndexMedia(t, mediaDB, "NES", []string{"Alpha", "Bravo", "Charlie"})

	// filename-asc resolves to a non-SortName ordering, so a first-character rail
	// would not match the displayed order: the method reports scheme "none".
	result, err := mediaDB.BrowseIndex(context.Background(), database.BrowseIndexOptions{
		PathPrefix: browseIndexTestDir,
		Sort:       "filename-asc",
	})
	require.NoError(t, err)

	assert.Equal(t, "none", result.Scheme)
	assert.Empty(t, result.Buckets)
	assert.Equal(t, 3, result.TotalFiles, "total still reported for the scope")
}

func TestBrowseIndex_EmptyDirectory(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	result, err := mediaDB.BrowseIndex(context.Background(), database.BrowseIndexOptions{
		PathPrefix: "roms/empty/",
		Sort:       "name-asc",
	})
	require.NoError(t, err)

	assert.Equal(t, "latin", result.Scheme)
	assert.Empty(t, result.Buckets)
	assert.Equal(t, 0, result.TotalFiles)
}

// seedBrowseIndexGameDirs indexes one game per directory under parent, the
// layout of a CD system.
func seedBrowseIndexGameDirs(t *testing.T, mediaDB *MediaDB, systemID, parent string, names []string) {
	t.Helper()
	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: systemID, Name: systemID})
	require.NoError(t, err)
	for _, name := range names {
		// Media paths are stored with forward slashes on every platform.
		insertSystemMedia(t, mediaDB, system, name, filepath.ToSlash(filepath.Join(parent, name, name+".chd")))
	}
}

// assertDirectoryBucketsMatchListing checks a directory facet against the
// listing media.browse pages through for the same scope.
func assertDirectoryBucketsMatchListing(
	t *testing.T,
	mediaDB *MediaDB,
	result *database.BrowseIndexResult,
	scope *database.BrowseDirectoriesOptions,
) {
	t.Helper()
	ctx := context.Background()
	dirs, err := mediaDB.BrowseDirectories(ctx, *scope)
	require.NoError(t, err)
	require.Equal(t, len(dirs), result.TotalDirs)

	next := 0
	for i, bucket := range result.Buckets {
		assert.Equalf(t, next, bucket.Offset, "bucket %q offset", bucket.Key)
		assert.Equalf(t, i == 0, bucket.AtStart, "bucket %q AtStart", bucket.Key)
		next += bucket.Count

		paged := *scope
		paged.AfterName = bucket.AfterDirName
		rest, pageErr := mediaDB.BrowseDirectories(ctx, paged)
		require.NoError(t, pageErr)
		require.Lenf(t, rest, len(dirs)-bucket.Offset, "bucket %q keyset", bucket.Key)
		assert.Equalf(t, dirs[bucket.Offset].Name, rest[0].Name, "bucket %q keyset lands on its first directory",
			bucket.Key)
	}
	assert.Equal(t, len(dirs), next, "buckets cover every directory")
}

func TestBrowseIndex_DirectoryFallback(t *testing.T) {
	t.Parallel()

	names := []string{
		"#1 Hits", "007 Racing", "3Xtreme", "alundra", "Ape Escape", "[T-En]Mizzurna Falls",
		"Metal Gear Solid", "(Demo) Wipeout", "Zanac",
	}
	// "(Demo) Wipeout" strips to " Wipeout", whose leading space is a symbol.
	wantKeys := []string{"#", "0-9", "A", "M", "Z"}
	wantCounts := map[string]int{"#": 2, "0-9": 2, "A": 2, "M": 2, "Z": 1}

	for _, cached := range []bool{false, true} {
		name := "media"
		if cached {
			name = "cache"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mediaDB, cleanup := setupTempMediaDB(t)
			defer cleanup()
			seedBrowseIndexGameDirs(t, mediaDB, "PSX", filepath.Join("roms", "psx"), names)
			if cached {
				require.NoError(t, mediaDB.PopulateBrowseCache(context.Background()))
			}

			for _, sortOrder := range []string{"name-asc", "name-desc", "filename-asc"} {
				result, err := mediaDB.BrowseIndex(context.Background(), database.BrowseIndexOptions{
					PathPrefix:        "roms/psx/",
					Sort:              sortOrder,
					DirectoryFallback: true,
				})
				require.NoError(t, err)
				assert.True(t, result.Directories, sortOrder)
				assert.Equal(t, "latin", result.Scheme, sortOrder)
				assert.Equal(t, 0, result.TotalFiles, sortOrder)

				keys := make([]string, len(result.Buckets))
				counts := make(map[string]int, len(result.Buckets))
				for i, bucket := range result.Buckets {
					keys[i] = bucket.Key
					counts[bucket.Key] = bucket.Count
				}
				// Directories ascend whatever the file sort is.
				assert.Equal(t, wantKeys, keys, sortOrder)
				assert.Equal(t, wantCounts, counts, sortOrder)
				assertDirectoryBucketsMatchListing(t, mediaDB, &result,
					&database.BrowseDirectoriesOptions{PathPrefix: "roms/psx/"})
			}
		})
	}
}

func TestBrowseIndex_DirectoryFallbackScopesBySystem(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	parent := filepath.Join("roms", "cd")
	seedBrowseIndexGameDirs(t, mediaDB, "PSX", parent, []string{"Alundra", "Castlevania"})
	seedBrowseIndexGameDirs(t, mediaDB, "Saturn", parent, []string{"Burning Rangers", "Nights"})
	require.NoError(t, mediaDB.PopulateBrowseCache(context.Background()))

	systems := browseIndexTestSystems(t, "Saturn")
	result, err := mediaDB.BrowseIndex(context.Background(), database.BrowseIndexOptions{
		PathPrefix:        "roms/cd/",
		Systems:           systems,
		DirectoryFallback: true,
	})
	require.NoError(t, err)
	require.True(t, result.Directories)
	keys := make([]string, len(result.Buckets))
	for i, bucket := range result.Buckets {
		keys[i] = bucket.Key
	}
	assert.Equal(t, []string{"B", "N"}, keys, "only the requested system's directories are bucketed")
	assertDirectoryBucketsMatchListing(t, mediaDB, &result,
		&database.BrowseDirectoriesOptions{PathPrefix: "roms/cd/", Systems: systems})
}

func TestBrowseIndex_DirectoryFallbackOnlyWithoutFiles(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	seedBrowseIndexGameDirs(t, mediaDB, "NES", filepath.Join("roms", "nes"), []string{"Alpha Set", "Zeta Set"})
	seedBrowseIndexMedia(t, mediaDB, "NES", []string{"Bravo", "Charlie"})

	// A folder with files of its own keeps the file facet.
	mixed, err := mediaDB.BrowseIndex(context.Background(), database.BrowseIndexOptions{
		PathPrefix:        browseIndexTestDir,
		Sort:              "name-asc",
		DirectoryFallback: true,
	})
	require.NoError(t, err)
	assert.False(t, mixed.Directories)
	assert.Equal(t, 2, mixed.TotalFiles)
	assert.Equal(t, 0, mixed.TotalDirs)
	require.Len(t, mixed.Buckets, 2)
	assert.Equal(t, "B", mixed.Buckets[0].Key)
	assert.Equal(t, 0, mixed.Buckets[0].Offset, "file offsets exclude directories")
	assert.Equal(t, "C", mixed.Buckets[1].Key)
	assert.Equal(t, 1, mixed.Buckets[1].Offset)

	// A scope that lists no directories never falls back, even when empty.
	flat, err := mediaDB.BrowseIndex(context.Background(), database.BrowseIndexOptions{
		PathPrefix: "roms/",
		Sort:       "name-asc",
	})
	require.NoError(t, err)
	assert.False(t, flat.Directories)
	assert.Empty(t, flat.Buckets)

	// With the fallback, the same file-less folder buckets its one directory.
	nested, err := mediaDB.BrowseIndex(context.Background(), database.BrowseIndexOptions{
		PathPrefix:        "roms/",
		Sort:              "name-asc",
		DirectoryFallback: true,
	})
	require.NoError(t, err)
	assert.True(t, nested.Directories)
	require.Len(t, nested.Buckets, 1)
	assert.Equal(t, "N", nested.Buckets[0].Key)

	// Neither files nor directories: the empty file facet, unchanged.
	empty, err := mediaDB.BrowseIndex(context.Background(), database.BrowseIndexOptions{
		PathPrefix:        "roms/empty/",
		Sort:              "name-asc",
		DirectoryFallback: true,
	})
	require.NoError(t, err)
	assert.False(t, empty.Directories)
	assert.Equal(t, "latin", empty.Scheme)
	assert.Empty(t, empty.Buckets)
}

func TestBrowseIndex_DirectoryFallbackMergesOverlayRoutes(t *testing.T) {
	t.Parallel()

	for _, cached := range []bool{false, true} {
		name := "media"
		if cached {
			name = "cache"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mediaDB, cleanup := setupTempMediaDB(t)
			defer cleanup()
			seedBrowseIndexGameDirs(t, mediaDB, "PSX", filepath.Join("usb0", "psx"),
				[]string{"Ape Escape", "Crash Bandicoot", "Metal Gear Solid"})
			seedBrowseIndexGameDirs(t, mediaDB, "PSX", filepath.Join("fat", "psx"),
				[]string{"alundra", "Crash Bandicoot", "Castlevania", "Zanac"})
			if cached {
				require.NoError(t, mediaDB.PopulateBrowseCache(context.Background()))
			}

			overlay := &database.BrowseOverlay{Sources: []database.BrowseSource{
				{PathPrefix: "usb0/psx/", IncludeDirs: true},
				{PathPrefix: "fat/psx/", IncludeDirs: true},
			}}
			systems := browseIndexTestSystems(t, "PSX")
			result, err := mediaDB.BrowseIndex(context.Background(), database.BrowseIndexOptions{
				Overlay:           overlay,
				Systems:           systems,
				DirectoryFallback: true,
			})
			require.NoError(t, err)
			require.True(t, result.Directories)
			assert.Equal(t, 6, result.TotalDirs, "the directory in both routes is counted once")

			keys := make([]string, len(result.Buckets))
			counts := make(map[string]int, len(result.Buckets))
			for i, bucket := range result.Buckets {
				keys[i] = bucket.Key
				counts[bucket.Key] = bucket.Count
			}
			assert.Equal(t, []string{"A", "C", "M", "Z"}, keys)
			assert.Equal(t, map[string]int{"A": 2, "C": 2, "M": 1, "Z": 1}, counts)
			assertDirectoryBucketsMatchListing(t, mediaDB, &result,
				&database.BrowseDirectoriesOptions{Overlay: overlay, Systems: systems})
		})
	}
}
