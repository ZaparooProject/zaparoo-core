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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSearchKeysetPagingCoversResultsExactlyOnce is the correctness guard on the
// shape of the search keyset predicate.
//
// searchCursorPredicate is written as a bound plus a tie-break rather than the
// row-value comparison it replaces, so the planner can turn it into an index
// range. The two forms have to select the same rows, and the case that
// separates a correct rewrite from a wrong one is a run of rows whose sort
// values compare equal: get the tie-break wrong and paging either repeats that
// run forever or steps over its tail at a page boundary.
//
// Every title here is duplicated across several media rows, so a name sort has
// ties straddling page boundaries at every page size tried.
func TestSearchKeysetPagingCoversResultsExactlyOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	nes, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	const (
		titles       = 60
		copiesPerRow = 3
		total        = titles * copiesPerRow
	)
	dir := browseTestDir("roms", "search")
	for title := range titles {
		name := fmt.Sprintf("Paged Game %03d", title)
		for copyIdx := range copiesPerRow {
			insertSystemMedia(t, mediaDB, system, name,
				fmt.Sprintf("%s%s (copy %d).nes", dir, name, copyIdx))
		}
	}

	for _, sortOrder := range []string{"name-asc", "name-desc", "filename-asc", "filename-desc"} {
		t.Run(sortOrder, func(t *testing.T) {
			unpaged, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
				Query:   "Paged",
				Systems: []systemdefs.System{*nes},
				Limit:   total,
				Sort:    sortOrder,
			})
			require.NoError(t, err)
			require.Len(t, unpaged, total)

			want := make([]int64, len(unpaged))
			for i := range unpaged {
				want[i] = unpaged[i].MediaID
			}

			for _, limit := range []int{1, 2, 5, 7} {
				var (
					got    []int64
					cursor *database.SearchCursor
				)
				for page := 0; ; page++ {
					require.Less(t, page, total+2, "paging did not terminate at limit %d", limit)
					rows, pageErr := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
						Query:      "Paged",
						Systems:    []systemdefs.System{*nes},
						Limit:      limit,
						Sort:       sortOrder,
						SortCursor: cursor,
					})
					require.NoError(t, pageErr)
					if len(rows) == 0 {
						break
					}
					for i := range rows {
						got = append(got, rows[i].MediaID)
					}
					last := rows[len(rows)-1]
					cursor = &database.SearchCursor{
						Sort:      sortOrder,
						SortValue: last.SortValue,
						LastID:    last.MediaID,
					}
				}
				assert.Equal(t, want, got,
					"paging at limit %d must return the same rows in the same order as one read",
					limit)
			}
		})
	}
}
