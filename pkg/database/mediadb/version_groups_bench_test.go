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
	"github.com/stretchr/testify/require"
)

// BenchmarkVersionCountPage measures the database half of a browse page's
// version counts: which of the page's titles share a game with another title,
// how many rows each title involved has, and the rows of those that may fold. The library holds 50k titles of two
// files each.
func BenchmarkVersionCountPage(b *testing.B) {
	const pageSize = 100
	page := make([]int64, 0, pageSize)
	for id := int64(1001); len(page) < pageSize; id++ {
		page = append(page, id)
	}
	filters := &database.SearchFilters{ExcludeHidden: true}

	run := func(b *testing.B, mediaDB *MediaDB) {
		b.Helper()
		ctx := context.Background()
		b.ReportAllocs()
		for b.Loop() {
			members, err := mediaDB.GetVersionGroupMembers(ctx, page)
			if err != nil {
				b.Fatal(err)
			}
			var titles []int64
			for _, id := range page {
				if group, linked := members[id]; linked {
					titles = append(titles, group...)
				} else {
					titles = append(titles, id)
				}
			}
			counts, err := mediaDB.CountTitleVersionRows(ctx, titles, filters)
			if err != nil {
				b.Fatal(err)
			}
			var foldable []int64
			for _, id := range titles {
				if counts[id].Rows > 1 && counts[id].Foldable > 0 {
					foldable = append(foldable, id)
				}
			}
			if len(foldable) > 0 {
				if _, err := mediaDB.GetTitleVersionRows(ctx, foldable, filters); err != nil {
					b.Fatal(err)
				}
			}
		}
	}

	b.Run("single_file_titles", func(b *testing.B) {
		mediaDB, cleanup := setupBenchDisambMultiSystemDB(b, 25, 2000)
		defer cleanup()
		_, err := mediaDB.sql.Load().ExecContext(context.Background(),
			`UPDATE Media SET IsMissing = 1 WHERE (DBID % 2) = 0`)
		require.NoError(b, err)
		run(b, mediaDB)
	})

	b.Run("two_files_per_title", func(b *testing.B) {
		mediaDB, cleanup := setupBenchDisambMultiSystemDB(b, 25, 2000)
		defer cleanup()
		run(b, mediaDB)
	})

	b.Run("every_title_linked_to_one_other", func(b *testing.B) {
		mediaDB, cleanup := setupBenchDisambMultiSystemDB(b, 25, 2000)
		defer cleanup()
		conn := mediaDB.sql.Load()
		_, err := conn.ExecContext(context.Background(), versionGroupIndexDDL)
		require.NoError(b, err)
		// Titles pair up with a neighbour outside the page's own range.
		_, err = conn.ExecContext(context.Background(),
			`UPDATE MediaTitles SET VersionGroup = ((DBID - 1) % 1000) + 1 WHERE DBID <= 2000`)
		require.NoError(b, err)
		run(b, mediaDB)
	})
}

// BenchmarkRecomputeVersionGroups_System measures a regroup of one system of
// 2000 titles in a library of 50k that holds no version keys at all: what
// every index run pays on a library with nothing to link.
func BenchmarkRecomputeVersionGroups_System(b *testing.B) {
	mediaDB, cleanup := setupBenchDisambMultiSystemDB(b, 25, 2000)
	defer cleanup()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if err := mediaDB.RecomputeVersionGroups(ctx, []string{"Bench1"}); err != nil {
			b.Fatal(err)
		}
	}
}
