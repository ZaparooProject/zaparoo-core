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

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/require"
)

func BenchmarkFrontendPrerequisites_Query_1k(b *testing.B) {
	benchmarkFrontendPrerequisiteQueries(b, 1_000)
}

func BenchmarkFrontendPrerequisites_Query_50k(b *testing.B) {
	benchmarkFrontendPrerequisiteQueries(b, 50_000)
}

func BenchmarkFrontendPrerequisites_Query_500k(b *testing.B) {
	benchmarkFrontendPrerequisiteQueries(b, 500_000)
}

func benchmarkFrontendPrerequisiteQueries(b *testing.B, rows int) {
	b.Helper()
	ctx := context.Background()
	mediaDB, cleanup := setupBrowseBenchMediaDB(b)
	defer cleanup()
	rootDir := seedRandomGameBenchmark(b, mediaDB, rows)
	folderDir := mediaRecursivePathPrefix(filepath.ToSlash(filepath.Join(rootDir, "folder-000")))
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(b, err)
	searchSystems := []systemdefs.System{*nesSystem}

	favorite := []zapscript.TagFilter{{Type: "user", Value: "favorite"}}
	action := []zapscript.TagFilter{{Type: "genre", Value: "action"}}
	andTags := []zapscript.TagFilter{
		{Type: "user", Value: "favorite"},
		{Type: "genre", Value: "action"},
	}
	orTags := []zapscript.TagFilter{
		{Type: "user", Value: "favorite", Operator: zapscript.TagOperatorOR},
		{Type: "genre", Value: "action", Operator: zapscript.TagOperatorOR},
	}
	notFavorite := []zapscript.TagFilter{{
		Type: "user", Value: "favorite", Operator: zapscript.TagOperatorNOT,
	}}
	tagCases := []struct {
		name string
		tags []zapscript.TagFilter
	}{
		{name: "favorite-sparse", tags: favorite},
		{name: "action-dense", tags: action},
		{name: "favorite-and-action", tags: andTags},
		{name: "favorite-or-action", tags: orTags},
		{name: "not-favorite", tags: notFavorite},
	}

	b.Run("systems", func(b *testing.B) {
		b.Run("all-media", func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				counts, err := mediaDB.SystemMediaCounts(ctx, nil)
				if err != nil {
					b.Fatal(err)
				}
				if len(counts) != 1 {
					b.Fatalf("expected one system count, got %d", len(counts))
				}
			}
		})

		for i := range tagCases {
			testCase := &tagCases[i]
			b.Run(testCase.name, func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					counts, err := mediaDB.SystemMediaCounts(ctx, testCase.tags)
					if err != nil {
						b.Fatal(err)
					}
					if len(counts) != 1 {
						b.Fatalf("expected one system count, got %d", len(counts))
					}
				}
			})
		}
	})

	b.Run("browse-first-page", func(b *testing.B) {
		for i := range tagCases {
			testCase := &tagCases[i]
			b.Run(testCase.name, func(b *testing.B) {
				b.ReportAllocs()
				opts := &database.BrowseFilesOptions{
					PathPrefix: folderDir,
					Sort:       "name",
					Tags:       testCase.tags,
					Limit:      100,
				}
				b.ResetTimer()
				for b.Loop() {
					_, err := mediaDB.BrowseFiles(ctx, opts)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})

	b.Run("browse-cursor-page", func(b *testing.B) {
		b.ReportAllocs()
		firstPageOpts := &database.BrowseFilesOptions{
			PathPrefix: folderDir,
			Sort:       "name",
			Tags:       action,
			Limit:      25,
		}
		firstPage, err := mediaDB.BrowseFiles(ctx, firstPageOpts)
		require.NoError(b, err)
		require.Len(b, firstPage, 25)
		last := firstPage[len(firstPage)-1]
		cursorOpts := &database.BrowseFilesOptions{
			PathPrefix: folderDir,
			Sort:       "name",
			Tags:       action,
			Limit:      25,
			Cursor: &database.BrowseCursor{
				Phase:     "files",
				SortMode:  last.SortMode,
				SortValue: last.SortValue,
				LastID:    last.MediaID,
			},
		}
		b.ResetTimer()
		for b.Loop() {
			_, err = mediaDB.BrowseFiles(ctx, cursorOpts)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("browse-index", func(b *testing.B) {
		for i := range tagCases {
			testCase := &tagCases[i]
			b.Run(testCase.name, func(b *testing.B) {
				b.ReportAllocs()
				opts := database.BrowseIndexOptions{
					PathPrefix: folderDir,
					Sort:       "name",
					Tags:       testCase.tags,
				}
				b.ResetTimer()
				for b.Loop() {
					_, err := mediaDB.BrowseIndex(ctx, opts)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})

	for _, sortOrder := range []string{"name-asc", "name-desc", "filename-asc", "filename-desc"} {
		b.Run("search-"+sortOrder, func(b *testing.B) {
			b.ReportAllocs()
			filters := &database.SearchFilters{
				Sort:    sortOrder,
				Systems: searchSystems,
				Tags:    favorite,
				Limit:   100,
			}
			b.ResetTimer()
			for b.Loop() {
				_, err := mediaDB.SearchMediaWithFilters(ctx, filters)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}

	b.Run("search-name-asc-cursor-page", func(b *testing.B) {
		b.ReportAllocs()
		firstPageFilters := &database.SearchFilters{
			Sort: "name-asc", Systems: searchSystems, Tags: favorite, Limit: 5,
		}
		firstPage, err := mediaDB.SearchMediaWithFilters(ctx, firstPageFilters)
		require.NoError(b, err)
		require.Len(b, firstPage, 5)
		last := firstPage[len(firstPage)-1]
		cursorFilters := &database.SearchFilters{
			Sort:    "name-asc",
			Systems: searchSystems,
			Tags:    favorite,
			Limit:   5,
			SortCursor: &database.SearchCursor{
				Sort:      "name-asc",
				SortValue: last.SortValue,
				LastID:    last.MediaID,
			},
		}
		b.ResetTimer()
		for b.Loop() {
			_, err = mediaDB.SearchMediaWithFilters(ctx, cursorFilters)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
