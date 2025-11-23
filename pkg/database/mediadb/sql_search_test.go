// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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

	err = fetchAndAttachTags(context.Background(), db, results, false)
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
	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		ExpectQuery().
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}))

	err = fetchAndAttachTags(context.Background(), db, results, false)
	require.NoError(t, err)
	assert.NotNil(t, results[0].Tags)
	assert.Empty(t, results[0].Tags)
	assert.Nil(t, results[0].Year)
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
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Action", "genre").
		AddRow(int64(1), "Nintendo", "publisher").
		AddRow(int64(1), "1985", "year")

	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		ExpectQuery().
		WithArgs(int64(1)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results, false)
	require.NoError(t, err)
	assert.Len(t, results[0].Tags, 3)
	assert.Equal(t, "Action", results[0].Tags[0].Tag)
	assert.Equal(t, "genre", results[0].Tags[0].Type)
	assert.Equal(t, "Nintendo", results[0].Tags[1].Tag)
	assert.Equal(t, "publisher", results[0].Tags[1].Type)
	assert.Equal(t, "1985", results[0].Tags[2].Tag)
	assert.Equal(t, "year", results[0].Tags[2].Type)
	assert.Nil(t, results[0].Year) // extractYear was false
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachTags_YearExtractionEnabled(t *testing.T) {
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
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Action", "genre").
		AddRow(int64(1), "1985", "year").
		AddRow(int64(1), "Nintendo", "publisher")

	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		ExpectQuery().
		WithArgs(int64(1)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results, true)
	require.NoError(t, err)
	assert.Len(t, results[0].Tags, 3)
	require.NotNil(t, results[0].Year)
	assert.Equal(t, "1985", *results[0].Year)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachTags_YearExtractionInvalidYear(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		yearTag  string
		yearType string
		wantYear bool
	}{
		{
			name:     "non-4-digit year",
			yearTag:  "85",
			yearType: "year",
			wantYear: false,
		},
		{
			name:     "year with non-numeric characters",
			yearTag:  "198X",
			yearType: "year",
			wantYear: false,
		},
		{
			name:     "year tag not type year",
			yearTag:  "1985",
			yearType: "release",
			wantYear: false,
		},
		{
			name:     "empty year tag",
			yearTag:  "",
			yearType: "year",
			wantYear: false,
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

			tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
				AddRow(int64(1), tt.yearTag, tt.yearType)

			mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
				ExpectQuery().
				WithArgs(int64(1)).
				WillReturnRows(tagRows)

			err = fetchAndAttachTags(context.Background(), db, results, true)
			require.NoError(t, err)
			if tt.wantYear {
				require.NotNil(t, results[0].Year)
				assert.Equal(t, tt.yearTag, *results[0].Year)
			} else {
				assert.Nil(t, results[0].Year)
			}
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
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Action", "genre").
		AddRow(int64(1), "1985", "year").
		AddRow(int64(2), "Adventure", "genre").
		AddRow(int64(2), "1986", "year").
		AddRow(int64(3), "Platform", "genre").
		AddRow(int64(3), "1990", "year")

	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		ExpectQuery().
		WithArgs(int64(1), int64(2), int64(3)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results, true)
	require.NoError(t, err)

	// Verify first result
	assert.Len(t, results[0].Tags, 2)
	assert.Equal(t, "Action", results[0].Tags[0].Tag)
	require.NotNil(t, results[0].Year)
	assert.Equal(t, "1985", *results[0].Year)

	// Verify second result
	assert.Len(t, results[1].Tags, 2)
	assert.Equal(t, "Adventure", results[1].Tags[0].Tag)
	require.NotNil(t, results[1].Year)
	assert.Equal(t, "1986", *results[1].Year)

	// Verify third result
	assert.Len(t, results[2].Tags, 2)
	assert.Equal(t, "Platform", results[2].Tags[0].Tag)
	require.NotNil(t, results[2].Year)
	assert.Equal(t, "1990", *results[2].Year)

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
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "untyped-tag", "").
		AddRow(int64(1), "genre-tag", "genre")

	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		ExpectQuery().
		WithArgs(int64(1)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results, false)
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
	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		WillReturnError(prepareErr)

	err = fetchAndAttachTags(context.Background(), db, results, false)
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
	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		ExpectQuery().
		WithArgs(int64(1)).
		WillReturnError(queryErr)

	err = fetchAndAttachTags(context.Background(), db, results, false)
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
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow("invalid", "tag", "type") // MediaDBID should be int64, not string

	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		ExpectQuery().
		WithArgs(int64(1)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results, false)
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
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "tag1", "type1").
		RowError(0, rowsErr)

	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		ExpectQuery().
		WithArgs(int64(1)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tags rows iteration error")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachTags_FirstYearTagUsed(t *testing.T) {
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

	// Multiple year tags - should use the first valid one
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Action", "genre").
		AddRow(int64(1), "1985", "year").
		AddRow(int64(1), "1986", "year"). // Second year tag should be ignored
		AddRow(int64(1), "1987", "year")

	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		ExpectQuery().
		WithArgs(int64(1)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results, true)
	require.NoError(t, err)
	require.NotNil(t, results[0].Year)
	assert.Equal(t, "1985", *results[0].Year) // First valid year tag
	assert.Len(t, results[0].Tags, 4)         // All tags are still attached
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
	tags := []database.TagFilter{}
	limit := 10
	includeName := false

	// Mock the main media query
	mediaRows := sqlmock.NewRows([]string{"SystemID", "Name", "Path", "DBID"}).
		AddRow("nes", "Super Mario Bros", "/games/mario.nes", int64(1))

	mock.ExpectPrepare(`SELECT.*FROM Systems.*WHERE Systems.SystemID IN`).
		ExpectQuery().
		WithArgs("nes", "%mario%", "%mario%", limit). // Slug LIKE, SecondarySlug LIKE
		WillReturnRows(mediaRows)

	// Mock the tags query
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Action", "genre").
		AddRow(int64(1), "1985", "year")

	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		ExpectQuery().
		WithArgs(int64(1)).
		WillReturnRows(tagRows)

	results, err := sqlSearchMediaWithFilters(
		context.Background(), db, systems, variantGroups, rawWords, tags, nil, nil, limit, includeName,
	)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Super Mario Bros", results[0].Name)
	assert.Len(t, results[0].Tags, 2)
	assert.Equal(t, "Action", results[0].Tags[0].Tag)
	require.NotNil(t, results[0].Year)
	assert.Equal(t, "1985", *results[0].Year)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlug_IntegrationWithTags(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Mock the main search query
	mediaRows := sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID"}).
		AddRow("nes", "Super Mario Bros", "/games/mario.nes", int64(1))

	mock.ExpectPrepare(`SELECT.*FROM Systems.*WHERE Systems.SystemID = \?.*AND MediaTitles.Slug = \?`).
		ExpectQuery().
		WithArgs("nes", "supermariobros"). // Slugified version (hyphens removed)
		WillReturnRows(mediaRows)

	// Mock the tags query
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Platform", "genre")

	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		ExpectQuery().
		WithArgs(int64(1)).
		WillReturnRows(tagRows)

	results, err := sqlSearchMediaBySlug(
		context.Background(), db, "nes", "super-mario-bros", nil,
	)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Super Mario Bros", results[0].Name)
	assert.Len(t, results[0].Tags, 1)
	assert.Equal(t, "Platform", results[0].Tags[0].Tag)
	assert.Nil(t, results[0].Year) // extractYear is false
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySecondarySlug_IntegrationWithTags(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Mock the main search query
	mediaRows := sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID"}).
		AddRow("nes", "Super Mario Bros", "/games/smb.nes", int64(1))

	mock.ExpectPrepare(`SELECT.*FROM Systems.*WHERE Systems.SystemID = \?.*AND MediaTitles.SecondarySlug = \?`).
		ExpectQuery().
		WithArgs("nes", "smb"). // Already single word, no change needed
		WillReturnRows(mediaRows)

	// Mock the tags query
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Nintendo", "publisher")

	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		ExpectQuery().
		WithArgs(int64(1)).
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
	mediaRows := sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID"}).
		AddRow("nes", "Super Mario Bros", "/games/mario.nes", int64(1)).
		AddRow("nes", "Super Mario Bros 2", "/games/mario2.nes", int64(2))

	mock.ExpectPrepare(`SELECT.*FROM Systems.*WHERE Systems.SystemID = \?.*AND MediaTitles.Slug LIKE \?`).
		ExpectQuery().
		WithArgs("nes", "supermario%"). // Slugified version (hyphens removed)
		WillReturnRows(mediaRows)

	// Mock the tags query
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Platform", "genre").
		AddRow(int64(2), "Platform", "genre").
		AddRow(int64(2), "Sequel", "series")

	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		ExpectQuery().
		WithArgs(int64(1), int64(2)).
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
	mediaRows := sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID"}).
		AddRow("nes", "Super Mario Bros", "/games/mario.nes", int64(1)).
		AddRow("nes", "The Legend of Zelda", "/games/zelda.nes", int64(2))

	mock.ExpectPrepare(`SELECT.*FROM Systems.*WHERE Systems.SystemID = \?.*AND MediaTitles.Slug IN`).
		ExpectQuery().
		WithArgs("nes", "supermariobros", "thelegendofzelda"). // Slugified versions (hyphens removed)
		WillReturnRows(mediaRows)

	// Mock the tags query
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Platform", "genre").
		AddRow(int64(2), "Adventure", "genre").
		AddRow(int64(2), "RPG", "genre")

	mock.ExpectPrepare(`SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN`).
		ExpectQuery().
		WithArgs(int64(1), int64(2)).
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
