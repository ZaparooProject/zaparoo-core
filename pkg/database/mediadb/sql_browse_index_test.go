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
	t *testing.T, mediaDB *MediaDB, bucket database.BrowseIndexBucket, sortMode string,
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
	for i, b := range result.Buckets {
		keys[i] = b.Key
		counts[b.Key] = b.Count
	}
	assert.Equal(t, []string{"#", "0-9", "A", "B", "Z"}, keys, "buckets in scroll order")
	assert.Equal(t, map[string]int{"#": 1, "0-9": 1, "A": 2, "B": 1, "Z": 1}, counts)

	// The first bucket in the list begins the list (no preceding row).
	require.True(t, result.Buckets[0].AtStart)
	for _, b := range result.Buckets[1:] {
		assert.False(t, b.AtStart, "non-leading bucket %q should carry a cursor", b.Key)
	}

	// Each bucket's cursor must seek a media.browse page to that bucket's first row.
	for _, b := range result.Buckets {
		assert.Equalf(t, b.Key, firstBrowsedBucketForCursor(t, mediaDB, b, result.SortMode),
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
	for i, b := range result.Buckets {
		keys[i] = b.Key
	}
	assert.Equal(t, []string{"Z", "B", "A", "0-9", "#"}, keys, "reversed for name-desc")
	require.True(t, result.Buckets[0].AtStart, "Z begins a descending list")

	for _, b := range result.Buckets {
		assert.Equalf(t, b.Key, firstBrowsedBucketForCursor(t, mediaDB, b, result.SortMode),
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
