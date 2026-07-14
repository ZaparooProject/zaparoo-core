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
	"errors"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchAndAttachTags_EmptyResults(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Empty results should return immediately without any DB operations
	results := []database.SearchResultWithCursor{}

	err = fetchAndAttachTags(context.Background(), db, results)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachTags_SingleResultNoTags(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "nes",
			Name:     "Super Mario Bros",
			Path:     "/games/mario.nes",
		},
	}

	// Mock the tags query - no rows returned
	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type", "DisplayName"}))

	err = fetchAndAttachTags(context.Background(), db, results)
	require.NoError(t, err)
	assert.NotNil(t, results[0].Tags)
	assert.Empty(t, results[0].Tags)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachTags_SingleResultWithTags(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "nes",
			Name:     "Super Mario Bros",
			Path:     "/games/mario.nes",
		},
	}

	// Mock the tags query with multiple tags
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type", "DisplayName"}).
		AddRow(int64(1), "Action", "genre", "Action").
		AddRow(int64(1), "Nintendo", "publisher", "Nintendo").
		AddRow(int64(1), "1985", "year", "1985")

	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(1)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results)
	require.NoError(t, err)
	assert.Len(t, results[0].Tags, 3)
	assert.Equal(t, "Action", results[0].Tags[0].Tag)
	assert.Equal(t, "genre", results[0].Tags[0].Type)
	assert.Equal(t, "Nintendo", results[0].Tags[1].Tag)
	assert.Equal(t, "publisher", results[0].Tags[1].Type)
	assert.Equal(t, "Nintendo", results[0].Tags[1].Label)
	assert.Equal(t, "1985", results[0].Tags[2].Tag)
	assert.Equal(t, "year", results[0].Tags[2].Type)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachTags_YearTagPresent(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "nes",
			Name:     "Super Mario Bros",
			Path:     "/games/mario.nes",
		},
	}

	// Mock the tags query with a 4-digit year tag
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type", "DisplayName"}).
		AddRow(int64(1), "Action", "genre", "Action").
		AddRow(int64(1), "1985", "year", "1985").
		AddRow(int64(1), "Nintendo", "publisher", "Nintendo")

	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(1)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results)
	require.NoError(t, err)
	assert.Len(t, results[0].Tags, 3)
	// Year tag is present somewhere in the slice; order is type+tag sorted.
	var yearTag *database.TagInfo
	for i := range results[0].Tags {
		if results[0].Tags[i].Type == "year" {
			yearTag = &results[0].Tags[i]
			break
		}
	}
	require.NotNil(t, yearTag)
	assert.Equal(t, "1985", yearTag.Tag)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachTags_VariousYearTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		yearTag  string
		yearType string
	}{
		{
			name:     "non-4-digit year",
			yearTag:  "85",
			yearType: "year",
		},
		{
			name:     "year with non-numeric characters",
			yearTag:  "198X",
			yearType: "year",
		},
		{
			name:     "year tag not type year",
			yearTag:  "1985",
			yearType: "release",
		},
		{
			name:     "empty year tag",
			yearTag:  "",
			yearType: "year",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, mock, err := testsqlmock.NewSQLMock()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			results := []database.SearchResultWithCursor{
				{
					MediaID:  1,
					SystemID: "nes",
					Name:     "Test Game",
					Path:     "/games/test.nes",
				},
			}

			tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type", "DisplayName"}).
				AddRow(int64(1), tt.yearTag, tt.yearType, tt.yearTag)

			mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
				ExpectQuery().
				WithArgs(int64(1), int64(1)).
				WillReturnRows(tagRows)

			err = fetchAndAttachTags(context.Background(), db, results)
			require.NoError(t, err)
			// Tags are always attached regardless of year validity
			assert.Len(t, results[0].Tags, 1)
			assert.Equal(t, tt.yearTag, results[0].Tags[0].Tag)
			assert.Equal(t, tt.yearType, results[0].Tags[0].Type)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestFetchAndAttachTags_MultipleResults(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "nes",
			Name:     "Super Mario Bros",
			Path:     "/games/mario.nes",
		},
		{
			MediaID:  2,
			SystemID: "nes",
			Name:     "The Legend of Zelda",
			Path:     "/games/zelda.nes",
		},
		{
			MediaID:  3,
			SystemID: "snes",
			Name:     "Super Mario World",
			Path:     "/games/smw.sfc",
		},
	}

	// Mock the tags query with tags for multiple media items
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type", "DisplayName"}).
		AddRow(int64(1), "Action", "genre", "Action").
		AddRow(int64(1), "1985", "year", "1985").
		AddRow(int64(2), "Adventure", "genre", "Adventure").
		AddRow(int64(2), "1986", "year", "1986").
		AddRow(int64(3), "Platform", "genre", "Platform").
		AddRow(int64(3), "1990", "year", "1990")

	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(2), int64(3), int64(1), int64(2), int64(3)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results)
	require.NoError(t, err)

	// Verify first result
	assert.Len(t, results[0].Tags, 2)
	assert.Equal(t, "Action", results[0].Tags[0].Tag)
	assert.Equal(t, "1985", results[0].Tags[1].Tag)
	assert.Equal(t, "year", results[0].Tags[1].Type)

	// Verify second result
	assert.Len(t, results[1].Tags, 2)
	assert.Equal(t, "Adventure", results[1].Tags[0].Tag)
	assert.Equal(t, "1986", results[1].Tags[1].Tag)
	assert.Equal(t, "year", results[1].Tags[1].Type)

	// Verify third result
	assert.Len(t, results[2].Tags, 2)
	assert.Equal(t, "Platform", results[2].Tags[0].Tag)
	assert.Equal(t, "1990", results[2].Tags[1].Tag)
	assert.Equal(t, "year", results[2].Tags[1].Type)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachTags_ResultTitleIDsAvoidMediaJoin(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{MediaID: 1, MediaTitleID: 10, SystemID: "nes", Name: "Game", Path: filepath.Join("games", "a.nes")},
		{MediaID: 2, MediaTitleID: 10, SystemID: "nes", Name: "Game", Path: filepath.Join("games", "b.nes")},
		{MediaID: 3, MediaTitleID: 30, SystemID: "snes", Name: "Other", Path: filepath.Join("games", "c.sfc")},
	}

	tagRows := sqlmock.NewRows([]string{"SourceKind", "SourceDBID", "Tag", "Type", "DisplayName"}).
		AddRow(0, int64(1), "Rev A", "rev", "Revision A").
		AddRow(1, int64(10), "Action", "genre", "Action").
		AddRow(1, int64(30), "1990", "year", "1990")

	mock.ExpectQuery(`SELECT EXISTS.*MediaTags.*MediaTitleTags`).
		WithArgs(int64(1), int64(2), int64(3), int64(10), int64(30)).
		WillReturnRows(sqlmock.NewRows([]string{"hasTags"}).AddRow(true))
	mock.ExpectPrepare(`SELECT.*SourceKind.*SourceDBID.*FROM MediaTags.*FROM MediaTitleTags`).
		ExpectQuery().
		WithArgs(int64(1), int64(2), int64(3), int64(10), int64(30)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results)
	require.NoError(t, err)

	assert.Equal(t, []database.TagInfo{
		{Tag: "Action", Type: "genre", Label: "Action"},
		{Tag: "Rev A", Type: "rev", Label: "Revision A"},
	}, results[0].Tags)
	assert.Equal(t, []database.TagInfo{{Tag: "Action", Type: "genre", Label: "Action"}}, results[1].Tags)
	assert.Equal(t, []database.TagInfo{{Tag: "1990", Type: "year", Label: "1990"}}, results[2].Tags)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAppendTagInfo_DedupesByTypeTagAndKeepsLabel(t *testing.T) {
	t.Parallel()

	tagsMap := make(map[int64][]database.TagInfo)
	seen := make(map[int64]map[tagKey]int)

	appendTagInfo(tagsMap, seen, 1, "nintendo", "publisher", "")
	appendTagInfo(tagsMap, seen, 1, "nintendo", "publisher", "Nintendo")
	appendTagInfo(tagsMap, seen, 1, "nintendo", "publisher", "Nintendo Co.")

	require.Len(t, tagsMap[1], 1)
	assert.Equal(t, database.TagInfo{Tag: "nintendo", Type: "publisher", Label: "Nintendo"}, tagsMap[1][0])
}

func TestFetchAndAttachTags_ResultTitleIDsNoTagsSkipsFullFetch(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{MediaID: 1, MediaTitleID: 10, SystemID: "nes", Name: "Game A", Path: filepath.Join("games", "a.nes")},
		{MediaID: 2, MediaTitleID: 20, SystemID: "nes", Name: "Game B", Path: filepath.Join("games", "b.nes")},
	}

	mock.ExpectQuery(`SELECT EXISTS.*MediaTags.*MediaTitleTags`).
		WithArgs(int64(1), int64(2), int64(10), int64(20)).
		WillReturnRows(sqlmock.NewRows([]string{"hasTags"}).AddRow(false))

	err = fetchAndAttachTags(context.Background(), db, results)
	require.NoError(t, err)
	assert.Empty(t, results[0].Tags)
	assert.Empty(t, results[1].Tags)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachTags_TagsWithMissingTypeDBID(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "nes",
			Name:     "Test Game",
			Path:     "/games/test.nes",
		},
	}

	// Mock tags where some have empty type (from LEFT JOIN with missing TypeDBID)
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type", "DisplayName"}).
		AddRow(int64(1), "untyped-tag", "", "Untyped Tag").
		AddRow(int64(1), "genre-tag", "genre", "Genre Tag")

	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(1)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results)
	require.NoError(t, err)
	assert.Len(t, results[0].Tags, 2)
	assert.Equal(t, "untyped-tag", results[0].Tags[0].Tag)
	assert.Empty(t, results[0].Tags[0].Type) // Empty type from LEFT JOIN
	assert.Equal(t, "genre-tag", results[0].Tags[1].Tag)
	assert.Equal(t, "genre", results[0].Tags[1].Type)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachTags_PrepareError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{MediaID: 1, SystemID: "nes", Name: "Test", Path: "/test"},
	}

	prepareErr := errors.New("prepare failed")
	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		WillReturnError(prepareErr)

	err = fetchAndAttachTags(context.Background(), db, results)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to prepare tags query")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachTags_QueryError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{MediaID: 1, SystemID: "nes", Name: "Test", Path: "/test"},
	}

	queryErr := errors.New("query execution failed")
	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(1)).
		WillReturnError(queryErr)

	err = fetchAndAttachTags(context.Background(), db, results)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute tags query")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachTags_ScanError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{MediaID: 1, SystemID: "nes", Name: "Test", Path: "/test"},
	}

	// Return row with wrong column types to cause scan error
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type", "DisplayName"}).
		AddRow("invalid", "tag", "type", "label") // MediaDBID should be int64, not string

	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(1)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to scan tags result")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachTags_RowsError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{MediaID: 1, SystemID: "nes", Name: "Test", Path: "/test"},
	}

	rowsErr := errors.New("rows iteration error")
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type", "DisplayName"}).
		AddRow(int64(1), "tag1", "type1", "Tag 1").
		RowError(0, rowsErr)

	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(1)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tags rows iteration error")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachTags_MultipleYearTags(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "nes",
			Name:     "Test Game",
			Path:     "/games/test.nes",
		},
	}

	// Multiple year tags - all should be attached
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type", "DisplayName"}).
		AddRow(int64(1), "Action", "genre", "Action").
		AddRow(int64(1), "1985", "year", "1985").
		AddRow(int64(1), "1986", "year", "1986").
		AddRow(int64(1), "1987", "year", "1987")

	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(1)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results)
	require.NoError(t, err)
	assert.Len(t, results[0].Tags, 4) // All tags are attached
	assert.Equal(t, "1985", results[0].Tags[1].Tag)
	assert.Equal(t, "year", results[0].Tags[1].Type)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaWithFilters_IntegrationWithTags(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systems := []systemdefs.System{{ID: "nes"}}
	variantGroups := [][]string{{"mario"}} // Single word with single variant
	rawWords := []string{"mario"}
	tags := []zapscript.TagFilter{}
	limit := 10
	includeName := false

	// Mock the main media query
	mediaRows := sqlmock.NewRows([]string{"SystemID", "Name", "Path", "DBID", "DisambiguationTypes"}).
		AddRow("nes", "Super Mario Bros", "/games/mario.nes", int64(1), "")

	mock.ExpectPrepare(`SELECT.*FROM Systems.*WHERE Systems.SystemID IN`).
		ExpectQuery().
		WithArgs("nes", "%mario%", "%mario%", limit). // Slug LIKE, SecondarySlug LIKE
		WillReturnRows(mediaRows)

	// Mock the tags query
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type", "DisplayName"}).
		AddRow(int64(1), "Action", "genre", "Action").
		AddRow(int64(1), "1985", "year", "1985")

	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(1)).
		WillReturnRows(tagRows)

	results, err := sqlSearchMediaWithFilters(
		context.Background(), db, systems, variantGroups, rawWords, tags, nil, nil, limit, includeName,
	)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Super Mario Bros", results[0].Name)
	assert.Len(t, results[0].Tags, 2)
	assert.Equal(t, "Action", results[0].Tags[0].Tag)
	assert.Equal(t, "1985", results[0].Tags[1].Tag)
	assert.Equal(t, "year", results[0].Tags[1].Type)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlug_IntegrationWithTags(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Mock the main search query
	mediaRows := sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID", "DisambiguationTypes"}).
		AddRow("nes", "Super Mario Bros", "/games/mario.nes", int64(1), "")

	mock.ExpectPrepare(`SELECT.*FROM Systems.*WHERE Systems.SystemID = \?.*AND MediaTitles.Slug = \?`).
		ExpectQuery().
		WithArgs("nes", "supermariobros"). // Slugified version (hyphens removed)
		WillReturnRows(mediaRows)

	// Mock the tags query
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type", "DisplayName"}).
		AddRow(int64(1), "Platform", "genre", "Platform")

	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(1)).
		WillReturnRows(tagRows)

	results, err := sqlSearchMediaBySlug(
		context.Background(), db, "nes", "super-mario-bros", nil,
	)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Super Mario Bros", results[0].Name)
	assert.Len(t, results[0].Tags, 1)
	assert.Equal(t, "Platform", results[0].Tags[0].Tag)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySecondarySlug_IntegrationWithTags(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Mock the main search query
	mediaRows := sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID", "DisambiguationTypes"}).
		AddRow("nes", "Super Mario Bros", "/games/smb.nes", int64(1), "")

	mock.ExpectPrepare(`SELECT.*FROM Systems.*WHERE Systems.SystemID = \?.*AND MediaTitles.SecondarySlug = \?`).
		ExpectQuery().
		WithArgs("nes", "smb"). // Already single word, no change needed
		WillReturnRows(mediaRows)

	// Mock the tags query
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type", "DisplayName"}).
		AddRow(int64(1), "Nintendo", "publisher", "Nintendo")

	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(1)).
		WillReturnRows(tagRows)

	results, err := sqlSearchMediaBySecondarySlug(
		context.Background(), db, "nes", "smb", nil,
	)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Super Mario Bros", results[0].Name)
	assert.Len(t, results[0].Tags, 1)
	assert.Equal(t, "Nintendo", results[0].Tags[0].Tag)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlugPrefix_IntegrationWithTags(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Mock the main search query
	mediaRows := sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID", "DisambiguationTypes"}).
		AddRow("nes", "Super Mario Bros", "/games/mario.nes", int64(1), "").
		AddRow("nes", "Super Mario Bros 2", "/games/mario2.nes", int64(2), "")

	mock.ExpectPrepare(`SELECT.*FROM Systems.*WHERE Systems.SystemID = \?.*AND MediaTitles.Slug LIKE \?`).
		ExpectQuery().
		WithArgs("nes", "supermario%"). // Slugified version (hyphens removed)
		WillReturnRows(mediaRows)

	// Mock the tags query
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type", "DisplayName"}).
		AddRow(int64(1), "Platform", "genre", "Platform").
		AddRow(int64(2), "Platform", "genre", "Platform").
		AddRow(int64(2), "Sequel", "series", "Sequel")

	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(2), int64(1), int64(2)).
		WillReturnRows(tagRows)

	results, err := sqlSearchMediaBySlugPrefix(
		context.Background(), db, "nes", "super-mario", nil,
	)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Len(t, results[0].Tags, 1)
	assert.Len(t, results[1].Tags, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlugIn_IntegrationWithTags(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	slugList := []string{"super-mario-bros", "the-legend-of-zelda"}

	// Mock the main search query
	mediaRows := sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID", "DisambiguationTypes"}).
		AddRow("nes", "Super Mario Bros", "/games/mario.nes", int64(1), "").
		AddRow("nes", "The Legend of Zelda", "/games/zelda.nes", int64(2), "")

	mock.ExpectPrepare(`SELECT.*FROM Systems.*WHERE Systems.SystemID = \?.*AND MediaTitles.Slug IN`).
		ExpectQuery().
		WithArgs("nes", "supermariobros", "thelegendofzelda"). // Slugified versions (hyphens removed)
		WillReturnRows(mediaRows)

	// Mock the tags query
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type", "DisplayName"}).
		AddRow(int64(1), "Platform", "genre", "Platform").
		AddRow(int64(2), "Adventure", "genre", "Adventure").
		AddRow(int64(2), "RPG", "genre", "RPG")

	mock.ExpectPrepare(`SELECT.*MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(2), int64(1), int64(2)).
		WillReturnRows(tagRows)

	results, err := sqlSearchMediaBySlugIn(
		context.Background(), db, "nes", slugList, nil,
	)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Len(t, results[0].Tags, 1)
	assert.Equal(t, "Platform", results[0].Tags[0].Tag)
	assert.Len(t, results[1].Tags, 2)
	assert.Equal(t, "Adventure", results[1].Tags[0].Tag)
	assert.Equal(t, "RPG", results[1].Tags[1].Tag)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlugIn_EmptySlugList(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Empty slug list should return early without any DB operations
	results, err := sqlSearchMediaBySlugIn(
		context.Background(), db, "nes", []string{}, nil,
	)
	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlugIn_AllEmptySlugs(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Slugs that all slugify to empty strings should return early
	results, err := sqlSearchMediaBySlugIn(
		context.Background(), db, "nes", []string{"", "   ", ""}, nil,
	)
	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Disambiguation behavior is exercised end-to-end against a real database in
// disambiguation_test.go (RecomputeSystemDisambiguation + attachZapScriptTags),
// since the logic now lives in stored per-title types rather than in-memory
// page grouping.
