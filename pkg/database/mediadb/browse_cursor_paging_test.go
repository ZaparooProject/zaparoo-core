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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// walkBrowsePages pages through a folder the way a client does and returns the
// media IDs in the order they arrive.
func walkBrowsePages(t *testing.T, mediaDB *MediaDB, parentDir, sort string, limit int) []int64 {
	t.Helper()
	ctx := context.Background()

	var (
		ids    []int64
		cursor *database.BrowseCursor
	)
	for page := 0; ; page++ {
		require.Less(t, page, 10000, "paging did not terminate")
		rows, err := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
			PathPrefix: parentDir,
			Cursor:     cursor,
			Limit:      limit,
			Sort:       sort,
		})
		require.NoError(t, err)
		if len(rows) == 0 {
			return ids
		}
		for i := range rows {
			ids = append(ids, rows[i].MediaID)
		}
		last := rows[len(rows)-1]
		cursor = &database.BrowseCursor{
			SortValue: last.SortValue,
			SortMode:  last.SortMode,
			LastID:    last.MediaID,
		}
	}
}

// TestBrowseFilesKeysetPagingCoversFolderExactlyOnce is the correctness guard
// on the shape of the keyset predicate.
//
// The predicate is written as a bound on the sort column plus a tie-break
// rather than the row-value comparison it replaces, so that the planner can
// turn it into an index range (see browseCursorCondition). The two forms have
// to select the same rows, and the case that separates a correct rewrite from a
// wrong one is a run of rows whose sort values compare equal: get the tie-break
// wrong and paging either repeats them forever or steps over the tail of the
// run at a page boundary.
//
// Every title here is duplicated, so ties straddle page boundaries at every
// page size tried.
func TestBrowseFilesKeysetPagingCoversFolderExactlyOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()

	const (
		titles       = 400
		copiesPerRow = 3
		total        = titles * copiesPerRow
	)
	parentDir := browseTestDir("roms", "dupes")

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	for title := range titles {
		name := fmt.Sprintf("Duplicated Game %03d", title)
		for copyIdx := range copiesPerRow {
			insertSystemMedia(t, mediaDB, system, name,
				fmt.Sprintf("%s%s (copy %d).nes", parentDir, name, copyIdx))
		}
	}

	for _, sortOrder := range []string{"name-asc", "name-desc"} {
		t.Run(sortOrder, func(t *testing.T) {
			unpaged, err := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
				PathPrefix: parentDir,
				Limit:      total,
				Sort:       sortOrder,
			})
			require.NoError(t, err)
			require.Len(t, unpaged, total)

			want := make([]int64, len(unpaged))
			for i := range unpaged {
				want[i] = unpaged[i].MediaID
			}

			// Page sizes that do and do not divide the run length, so a tie is
			// split at every offset within it.
			for _, limit := range []int{1, 2, 3, 6, 7} {
				got := walkBrowsePages(t, mediaDB, parentDir, sortOrder, limit)
				assert.Equal(t, want, got,
					"paging at limit %d must return the same rows in the same order as one read",
					limit)
			}
		})
	}
}
