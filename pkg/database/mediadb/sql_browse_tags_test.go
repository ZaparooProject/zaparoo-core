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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowseTags_FilesCountAndIndexStayAligned(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	alphaPath := filepath.Join("roms", "nes", "Alpha.nes")
	bravoPath := filepath.Join("roms", "nes", "Bravo.nes")
	charliePath := filepath.Join("roms", "nes", "Charlie.nes")
	system := insertSystemWithMedia(t, mediaDB, "NES", "Alpha", alphaPath)
	insertSystemMedia(t, mediaDB, system, "Bravo", bravoPath)
	insertSystemMedia(t, mediaDB, system, "Charlie", charliePath)

	alpha, err := mediaDB.FindMedia(database.Media{SystemDBID: system.DBID, Path: alphaPath})
	require.NoError(t, err)
	charlie, err := mediaDB.FindMedia(database.Media{SystemDBID: system.DBID, Path: charliePath})
	require.NoError(t, err)
	require.NoError(t, mediaDB.UpdateMediaTags(ctx, alpha.DBID, nil, []database.MediaTagRef{{
		Type: "user",
		Tag:  "favorite",
	}}))
	require.NoError(t, mediaDB.UpsertMediaTitleTags(ctx, charlie.MediaTitleDBID, []database.TagInfo{{
		Type: "genre",
		Tag:  "rpg",
	}}))

	pathPrefix := filepath.ToSlash(filepath.Dir(alphaPath)) + "/"
	favoriteFilter := []zapscript.TagFilter{{Type: "user", Value: "favorite"}}
	files, err := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
		PathPrefix: pathPrefix,
		Sort:       "name-asc",
		Tags:       favoriteFilter,
		Limit:      10,
	})
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "Alpha", files[0].Name)

	count, err := mediaDB.BrowseFileCount(ctx, database.BrowseFileCountOptions{
		PathPrefix: pathPrefix,
		Tags:       favoriteFilter,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	index, err := mediaDB.BrowseIndex(ctx, database.BrowseIndexOptions{
		PathPrefix: pathPrefix,
		Sort:       "name-asc",
		Tags:       favoriteFilter,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, index.TotalFiles)
	require.Len(t, index.Buckets, 1)
	assert.Equal(t, "A", index.Buckets[0].Key)
	assert.Equal(t, 1, index.Buckets[0].Count)

	filenameIndex, err := mediaDB.BrowseIndex(ctx, database.BrowseIndexOptions{
		PathPrefix: pathPrefix,
		Sort:       "filename-desc",
		Tags:       favoriteFilter,
	})
	require.NoError(t, err)
	assert.Equal(t, "none", filenameIndex.Scheme)
	assert.Empty(t, filenameIndex.Buckets)
	assert.Equal(t, 1, filenameIndex.TotalFiles, "non-matching rows must not contribute to filename index totals")

	titleFilter := []zapscript.TagFilter{{Type: "genre", Value: "rpg"}}
	titleFiles, err := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
		PathPrefix: pathPrefix,
		Tags:       titleFilter,
		Limit:      10,
	})
	require.NoError(t, err)
	require.Len(t, titleFiles, 1)
	assert.Equal(t, "Charlie", titleFiles[0].Name)
}

func TestHasRequiredFavoriteFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		filter zapscript.TagFilter
		name   string
		want   bool
	}{
		{name: "default AND", filter: zapscript.TagFilter{Type: "user", Value: "favorite"}, want: true},
		{name: "explicit AND", filter: zapscript.TagFilter{
			Type: "user", Value: "favorite", Operator: zapscript.TagOperatorAND,
		}, want: true},
		{name: "OR", filter: zapscript.TagFilter{
			Type: "user", Value: "favorite", Operator: zapscript.TagOperatorOR,
		}},
		{name: "NOT", filter: zapscript.TagFilter{
			Type: "user", Value: "favorite", Operator: zapscript.TagOperatorNOT,
		}},
		{name: "other tag", filter: zapscript.TagFilter{Type: "genre", Value: "action"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, hasRequiredFavoriteFilter([]zapscript.TagFilter{testCase.filter}))
		})
	}
}

func TestBuildBrowseTagFilterSQL(t *testing.T) {
	t.Parallel()

	clauses, args := buildBrowseTagFilterSQL([]zapscript.TagFilter{
		{Type: "user", Value: "favorite"},
		{Type: "genre", Value: "action"},
	}, "m")
	require.Len(t, clauses, 2)
	assert.Contains(t, clauses[0], "m.DBID IN")
	assert.Contains(t, clauses[1], "EXISTS")
	assert.Len(t, args, 8)

	candidateClauses, _ := buildBrowseTagFilterSQL([]zapscript.TagFilter{{
		Type: "genre", Value: "action",
	}}, "m")
	require.Len(t, candidateClauses, 1)
	assert.Contains(t, candidateClauses[0], "EXISTS")
	assert.NotContains(t, candidateClauses[0], "m.DBID IN")
}

func TestBuildTagFilterSQL_MediaAlias(t *testing.T) {
	t.Parallel()

	clauses, args := buildTagFilterSQLForRef([]zapscript.TagFilter{
		{Type: "user", Value: "favorite"},
		{Type: "region", Value: "eu", Operator: zapscript.TagOperatorNOT},
		{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
	}, "m")

	require.NotEmpty(t, clauses)
	assert.NotEmpty(t, args)
	for _, clause := range clauses {
		assert.NotContains(t, clause, "Media.DBID")
		assert.NotContains(t, clause, "Media.MediaTitleDBID")
	}
	assert.Contains(t, clauses[0], "m.DBID")
}
