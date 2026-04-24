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
	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}))

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
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Action", "genre").
		AddRow(int64(1), "Nintendo", "publisher").
		AddRow(int64(1), "1985", "year")

	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
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
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Action", "genre").
		AddRow(int64(1), "1985", "year").
		AddRow(int64(1), "Nintendo", "publisher")

	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(1)).
		WillReturnRows(tagRows)

	err = fetchAndAttachTags(context.Background(), db, results)
	require.NoError(t, err)
	assert.Len(t, results[0].Tags, 3)
	// Verify the year tag is present in Tags
	assert.Equal(t, "1985", results[0].Tags[1].Tag)
	assert.Equal(t, "year", results[0].Tags[1].Type)
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

			tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
				AddRow(int64(1), tt.yearTag, tt.yearType)

			mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
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
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Action", "genre").
		AddRow(int64(1), "1985", "year").
		AddRow(int64(2), "Adventure", "genre").
		AddRow(int64(2), "1986", "year").
		AddRow(int64(3), "Platform", "genre").
		AddRow(int64(3), "1990", "year")

	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
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

	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
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
	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
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
	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
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
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow("invalid", "tag", "type") // MediaDBID should be int64, not string

	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
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
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "tag1", "type1").
		RowError(0, rowsErr)

	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
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
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Action", "genre").
		AddRow(int64(1), "1985", "year").
		AddRow(int64(1), "1986", "year").
		AddRow(int64(1), "1987", "year")

	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
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

	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
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
	mediaRows := sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID"}).
		AddRow("nes", "Super Mario Bros", "/games/mario.nes", int64(1))

	mock.ExpectPrepare(`SELECT.*FROM Systems.*WHERE Systems.SystemID = \?.*AND MediaTitles.Slug = \?`).
		ExpectQuery().
		WithArgs("nes", "supermariobros"). // Slugified version (hyphens removed)
		WillReturnRows(mediaRows)

	// Mock the tags query
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Platform", "genre")

	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
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
	mediaRows := sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID"}).
		AddRow("nes", "Super Mario Bros", "/games/smb.nes", int64(1))

	mock.ExpectPrepare(`SELECT.*FROM Systems.*WHERE Systems.SystemID = \?.*AND MediaTitles.SecondarySlug = \?`).
		ExpectQuery().
		WithArgs("nes", "smb"). // Already single word, no change needed
		WillReturnRows(mediaRows)

	// Mock the tags query
	tagRows := sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
		AddRow(int64(1), "Nintendo", "publisher")

	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
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

	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
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

	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
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

func TestComputeZapScriptTags_Empty(t *testing.T) {
	t.Parallel()
	results := []database.SearchResultWithCursor{}
	computeZapScriptTags(results)
	assert.Empty(t, results)
}

func TestComputeZapScriptTags_SingleResult(t *testing.T) {
	t.Parallel()
	results := []database.SearchResultWithCursor{
		{
			Name: "Tetris", SystemID: "NES", MediaID: 1,
			Tags: []database.TagInfo{
				{Type: "year", Tag: "1989"},
				{Type: "genre", Tag: "Puzzle"},
			},
		},
	}
	computeZapScriptTags(results)
	assert.Empty(t, results[0].ZapScriptTags, "single result should have no disambiguating tags")
	assert.NotNil(t, results[0].ZapScriptTags, "ZapScriptTags should be initialized, not nil")
}

func TestComputeZapScriptTags_SiblingsDifferentYear(t *testing.T) {
	t.Parallel()
	results := []database.SearchResultWithCursor{
		{
			Name: "Tetris", SystemID: "NES", MediaID: 1,
			Tags: []database.TagInfo{
				{Type: "year", Tag: "1989"},
				{Type: "genre", Tag: "Puzzle"},
			},
		},
		{
			Name: "Tetris", SystemID: "NES", MediaID: 2,
			Tags: []database.TagInfo{
				{Type: "year", Tag: "1990"},
				{Type: "genre", Tag: "Puzzle"},
			},
		},
	}
	computeZapScriptTags(results)
	require.Len(t, results[0].ZapScriptTags, 1)
	assert.Equal(t, "year", results[0].ZapScriptTags[0].Type)
	assert.Equal(t, "1989", results[0].ZapScriptTags[0].Tag)
	require.Len(t, results[1].ZapScriptTags, 1)
	assert.Equal(t, "year", results[1].ZapScriptTags[0].Type)
	assert.Equal(t, "1990", results[1].ZapScriptTags[0].Tag)
}

func TestComputeZapScriptTags_SiblingsSameYear(t *testing.T) {
	t.Parallel()
	results := []database.SearchResultWithCursor{
		{
			Name: "Tetris", SystemID: "NES", MediaID: 1,
			Tags: []database.TagInfo{{Type: "year", Tag: "1989"}},
		},
		{
			Name: "Tetris", SystemID: "NES", MediaID: 2,
			Tags: []database.TagInfo{{Type: "year", Tag: "1989"}},
		},
	}
	computeZapScriptTags(results)
	assert.Empty(t, results[0].ZapScriptTags, "same year across siblings should not disambiguate")
	assert.Empty(t, results[1].ZapScriptTags)
}

func TestComputeZapScriptTags_MixedDisambiguation(t *testing.T) {
	t.Parallel()
	// Same year but different players — only players should disambiguate
	results := []database.SearchResultWithCursor{
		{
			Name: "Street Fighter", SystemID: "Arcade", MediaID: 1,
			Tags: []database.TagInfo{
				{Type: "year", Tag: "1992"},
				{Type: "players", Tag: "2"},
			},
		},
		{
			Name: "Street Fighter", SystemID: "Arcade", MediaID: 2,
			Tags: []database.TagInfo{
				{Type: "year", Tag: "1992"},
				{Type: "players", Tag: "4"},
			},
		},
	}
	computeZapScriptTags(results)
	// Only players should be disambiguating (years are the same)
	require.Len(t, results[0].ZapScriptTags, 1)
	assert.Equal(t, "players", results[0].ZapScriptTags[0].Type)
	assert.Equal(t, "2", results[0].ZapScriptTags[0].Tag)
	require.Len(t, results[1].ZapScriptTags, 1)
	assert.Equal(t, "players", results[1].ZapScriptTags[0].Type)
	assert.Equal(t, "4", results[1].ZapScriptTags[0].Tag)
}

func TestComputeZapScriptTags_DifferentNamesNotGrouped(t *testing.T) {
	t.Parallel()
	// Different names should not be grouped as siblings
	results := []database.SearchResultWithCursor{
		{
			Name: "Tetris", SystemID: "NES", MediaID: 1,
			Tags: []database.TagInfo{{Type: "year", Tag: "1989"}},
		},
		{
			Name: "Dr. Mario", SystemID: "NES", MediaID: 2,
			Tags: []database.TagInfo{{Type: "year", Tag: "1990"}},
		},
	}
	computeZapScriptTags(results)
	assert.Empty(t, results[0].ZapScriptTags, "different names should not trigger disambiguation")
	assert.Empty(t, results[1].ZapScriptTags)
}

func TestComputeZapScriptTags_NonEligibleTagTypesIgnored(t *testing.T) {
	t.Parallel()
	// Genre differs across siblings but is not in ZapScriptTagTypes
	results := []database.SearchResultWithCursor{
		{
			Name: "Tetris", SystemID: "NES", MediaID: 1,
			Tags: []database.TagInfo{{Type: "genre", Tag: "Puzzle"}},
		},
		{
			Name: "Tetris", SystemID: "NES", MediaID: 2,
			Tags: []database.TagInfo{{Type: "genre", Tag: "Action"}},
		},
	}
	computeZapScriptTags(results)
	assert.Empty(t, results[0].ZapScriptTags, "non-eligible tag types should not disambiguate")
	assert.Empty(t, results[1].ZapScriptTags)
}

func TestComputeZapScriptTags_CrossSystemSameNameNotGrouped(t *testing.T) {
	t.Parallel()
	// Same name on different systems should NOT be grouped as siblings
	results := []database.SearchResultWithCursor{
		{
			Name: "Tetris", SystemID: "NES", MediaID: 1,
			Tags: []database.TagInfo{{Type: "year", Tag: "1989"}},
		},
		{
			Name: "Tetris", SystemID: "GB", MediaID: 2,
			Tags: []database.TagInfo{{Type: "year", Tag: "1990"}},
		},
	}
	computeZapScriptTags(results)
	assert.Empty(t, results[0].ZapScriptTags, "cross-system same name should not trigger disambiguation")
	assert.Empty(t, results[1].ZapScriptTags)
}
