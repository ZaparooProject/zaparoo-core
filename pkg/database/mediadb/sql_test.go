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
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSqlUpdateLastGenerated_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`INSERT OR REPLACE INTO DBConfig.*LastGeneratedAt`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = sqlUpdateLastGenerated(context.Background(), db)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetLastGenerated_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"Value"}).
		AddRow("1672531200")

	mock.ExpectQuery(`SELECT.*FROM DBConfig WHERE Name.*LastGeneratedAt`).
		WillReturnRows(rows)

	result, err := sqlGetLastGenerated(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, int64(1672531200), result.Unix())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlFindSystem_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	searchSystem := database.System{
		SystemID: "mister",
	}

	expectedSystem := database.System{
		DBID:     1,
		SystemID: "mister",
		Name:     "MiSTer FPGA",
	}

	rows := sqlmock.NewRows([]string{"DBID", "SystemID", "Name"}).
		AddRow(expectedSystem.DBID, expectedSystem.SystemID, expectedSystem.Name)

	mock.ExpectPrepare(`select.*from Systems.*where.*limit`).
		ExpectQuery().
		WithArgs(searchSystem.DBID, searchSystem.SystemID).
		WillReturnRows(rows)

	result, err := sqlFindSystem(context.Background(), db, searchSystem)
	require.NoError(t, err)
	assert.Equal(t, expectedSystem.DBID, result.DBID)
	assert.Equal(t, expectedSystem.SystemID, result.SystemID)
	assert.Equal(t, expectedSystem.Name, result.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlFindSystem_NotFound(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	searchSystem := database.System{
		SystemID: "unknown",
	}

	mock.ExpectPrepare(`select.*from Systems.*where.*limit`).
		ExpectQuery().
		WithArgs(searchSystem.DBID, searchSystem.SystemID).
		WillReturnError(sql.ErrNoRows)

	result, err := sqlFindSystem(context.Background(), db, searchSystem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to scan system row")
	assert.Equal(t, int64(0), result.DBID) // Zero value when error occurs
	assert.Empty(t, result.SystemID)
	assert.Empty(t, result.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlInsertSystem_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	system := database.System{
		SystemID: "test-system",
		Name:     "Test System",
	}

	mock.ExpectPrepare(`INSERT INTO Systems.*VALUES`).
		ExpectExec().
		WithArgs(nil, system.SystemID, system.Name).
		WillReturnResult(sqlmock.NewResult(42, 1))

	result, err := sqlInsertSystem(context.Background(), db, system)
	require.NoError(t, err)
	assert.Equal(t, int64(42), result.DBID)
	assert.Equal(t, system.SystemID, result.SystemID)
	assert.Equal(t, system.Name, result.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaPathExact_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Import the systemdefs package since search functions use it
	systems := []systemdefs.System{{ID: "test-system"}}
	path := "/games/test.rom"

	expectedResults := []database.SearchResult{
		{
			SystemID: "test-system",
			Path:     "/games/test.rom",
		},
	}

	rows := sqlmock.NewRows([]string{"SystemID", "Path"}).
		AddRow(expectedResults[0].SystemID, expectedResults[0].Path)

	// Match the actual SQL query structure
	mock.ExpectPrepare(`select.*from Systems.*inner join.*MediaTitles.*inner join.*Media.*where.*LIMIT`).
		ExpectQuery().
		WithArgs("test-system", sqlmock.AnyArg(), path). // slug will be computed
		WillReturnRows(rows)

	result, err := sqlSearchMediaPathExact(context.Background(), db, systems, path)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, expectedResults[0].SystemID, result[0].SystemID)
	assert.Equal(t, expectedResults[0].Path, result[0].Path)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlInsertSystem_Duplicate(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	system := database.System{
		SystemID: "test-system",
		Name:     "Test System",
	}

	mock.ExpectPrepare(`INSERT INTO Systems.*VALUES`).
		ExpectExec().
		WithArgs(nil, system.SystemID, system.Name).
		WillReturnError(sqlmock.ErrCancelled) // Simulate constraint violation

	result, err := sqlInsertSystem(context.Background(), db, system)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute insert system statement")
	assert.Equal(t, int64(0), result.DBID) // Zero value when error occurs
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlInsertSystemWithPreparedStmt_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	system := database.System{
		SystemID: "test-system",
		Name:     "Test System",
	}

	mock.ExpectPrepare(`INSERT INTO Systems.*VALUES`).
		ExpectExec().
		WithArgs(nil, system.SystemID, system.Name).
		WillReturnResult(sqlmock.NewResult(42, 1))

	// First prepare the statement
	stmt, err := db.PrepareContext(context.Background(), "INSERT INTO Systems (DBID, SystemID, Name) VALUES (?, ?, ?)")
	require.NoError(t, err)
	defer func() { _ = stmt.Close() }()

	result, err := sqlInsertSystemWithPreparedStmt(context.Background(), stmt, system)
	require.NoError(t, err)
	assert.Equal(t, int64(42), result.DBID)
	assert.Equal(t, system.SystemID, result.SystemID)
	assert.Equal(t, system.Name, result.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlTruncateSystems_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemIDs := []string{"NES", "SNES", "Genesis"}

	// Expect Systems deletion (CASCADE handles all related records)
	mock.ExpectExec(`DELETE FROM Systems WHERE SystemID IN \(\?,\?,\?\)`).
		WithArgs("NES", "SNES", "Genesis").
		WillReturnResult(sqlmock.NewResult(0, 3))

	// Expect cleanup of orphaned tags (RESTRICT prevented cascade, so we clean separately)
	// Note: TagTypes are NOT deleted as they are global infrastructure shared across systems
	mock.ExpectExec(`DELETE FROM Tags WHERE DBID NOT IN`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = sqlTruncateSystems(context.Background(), db, systemIDs)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlTruncateSystems_EmptyList(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Should return immediately without any database operations
	err = sqlTruncateSystems(context.Background(), db, []string{})
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlTruncateSystems_SingleSystem(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemIDs := []string{"NES"}

	// Expect Systems deletion (CASCADE handles all related records)
	mock.ExpectExec(`DELETE FROM Systems WHERE SystemID IN \(\?\)`).
		WithArgs("NES").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Expect cleanup of orphaned tags (RESTRICT prevented cascade, so we clean separately)
	// Note: TagTypes are NOT deleted as they are global infrastructure shared across systems
	mock.ExpectExec(`DELETE FROM Tags WHERE DBID NOT IN`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = sqlTruncateSystems(context.Background(), db, systemIDs)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlTruncateSystems_SystemsDeletionFailure(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemIDs := []string{"NES"}

	// Expect Systems deletion to fail
	mock.ExpectExec(`DELETE FROM Systems WHERE SystemID IN \(\?\)`).
		WithArgs("NES").
		WillReturnError(sql.ErrConnDone)

	err = sqlTruncateSystems(context.Background(), db, systemIDs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete systems")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlTruncateSystems_CleanupFailure(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemIDs := []string{"NES"}

	// Expect Systems deletion to succeed
	mock.ExpectExec(`DELETE FROM Systems WHERE SystemID IN \(\?\)`).
		WithArgs("NES").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Expect cleanup to fail
	// Note: TagTypes are NOT deleted as they are global infrastructure shared across systems
	mock.ExpectExec(`DELETE FROM Tags WHERE DBID NOT IN`).
		WillReturnError(sql.ErrConnDone)

	err = sqlTruncateSystems(context.Background(), db, systemIDs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to clean up orphaned tags")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPrepareVariadic_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pattern   string
		separator string
		expected  string
		count     int
	}{
		{
			name:      "question_marks_with_comma",
			pattern:   "?",
			separator: ",",
			count:     3,
			expected:  "?,?,?",
		},
		{
			name:      "single_item",
			pattern:   "?",
			separator: ",",
			count:     1,
			expected:  "?",
		},
		{
			name:      "zero_count",
			pattern:   "?",
			separator: ",",
			count:     0,
			expected:  "",
		},
		{
			name:      "negative_count",
			pattern:   "?",
			separator: ",",
			count:     -1,
			expected:  "",
		},
		{
			name:      "and_separator",
			pattern:   "column = ?",
			separator: " AND ",
			count:     2,
			expected:  "column = ? AND column = ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := prepareVariadic(tt.pattern, tt.separator, tt.count)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestJSONTagsParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []database.TagInfo
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []database.TagInfo{},
		},
		{
			name:     "null string",
			input:    "null",
			expected: []database.TagInfo{},
		},
		{
			name:  "single tag",
			input: `[{"tag":"Action","type":"genre"}]`,
			expected: []database.TagInfo{
				{Tag: "Action", Type: "genre"},
			},
		},
		{
			name: "multiple tags",
			input: `[{"tag":"Action","type":"genre"},{"tag":"2023","type":"year"},` +
				`{"tag":"Nintendo","type":"developer"}]`,
			expected: []database.TagInfo{
				{Tag: "Action", Type: "genre"},
				{Tag: "2023", Type: "year"},
				{Tag: "Nintendo", Type: "developer"},
			},
		},
		{
			name:     "empty array",
			input:    "[]",
			expected: []database.TagInfo{},
		},
		{
			name:  "tags with special characters",
			input: `[{"tag":"Action/Adventure","type":"genre"},{"tag":"Puzzle & Dragons","type":"series"}]`,
			expected: []database.TagInfo{
				{Tag: "Action/Adventure", Type: "genre"},
				{Tag: "Puzzle & Dragons", Type: "series"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var result []database.TagInfo

			// Simulate the logic from sqlSearchMediaPathPartsWithCursor
			if tt.input != "" && tt.input != "null" {
				err := json.Unmarshal([]byte(tt.input), &result)
				if err != nil {
					result = []database.TagInfo{}
				}
			} else {
				result = []database.TagInfo{}
			}

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSqlSearchMediaWithFilters_WithTags(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systems := []systemdefs.System{
		{ID: "NES"},
	}
	parts := []string{"mario"}
	tags := []database.TagFilter{{Type: "genre", Value: "Action"}}

	// Mock first query: get media items (with EXISTS clause - no HAVING COUNT arg needed)
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs("NES", "%mario%", "genre", "Action", 10).
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "Name", "Path", "DBID"}).
			AddRow("NES", "Mario", "/games/mario.nes", 1))

	// Mock second query: get tags for the media items
	mock.ExpectPrepare("SELECT.*MediaTags\\.MediaDBID.*Tags\\.Tag.*").
		ExpectQuery().
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
			AddRow(1, "Action", "genre"))

	results, err := sqlSearchMediaWithFilters(context.Background(), db, systems, parts, tags, nil, nil, 10, false)

	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "NES", results[0].SystemID)
	assert.Equal(t, "/games/mario.nes", results[0].Path)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetTags(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systems := []systemdefs.System{
		{ID: "NES"},
		{ID: "SNES"},
	}

	// Mock the expected query and result
	mock.ExpectPrepare("SELECT DISTINCT TagTypes.Type, Tags.Tag.*FROM TagTypes.*JOIN.*ORDER BY").
		ExpectQuery().
		WithArgs("NES", "SNES").
		WillReturnRows(sqlmock.NewRows([]string{"Type", "Tag"}).
			AddRow("genre", "Action").
			AddRow("genre", "Adventure").
			AddRow("year", "1990"))

	results, err := sqlGetTags(context.Background(), db, systems)

	require.NoError(t, err)
	assert.Len(t, results, 3) // Should have 3 tags

	// Check the tags are returned correctly
	expectedTags := []database.TagInfo{
		{Type: "genre", Tag: "Action"},
		{Type: "genre", Tag: "Adventure"},
		{Type: "year", Tag: "1990"},
	}

	assert.Equal(t, expectedTags, results)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlPopulateSystemTagsCache_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Expect DELETE statement to clear cache
	mock.ExpectPrepare("DELETE FROM SystemTagsCache").
		ExpectExec().
		WillReturnResult(sqlmock.NewResult(0, 5)) // Deleted 5 rows

	// Expect INSERT statement to populate cache
	mock.ExpectPrepare(`INSERT INTO SystemTagsCache.*`).
		ExpectExec().
		WillReturnResult(sqlmock.NewResult(1, 10)) // Inserted 10 rows

	err = sqlPopulateSystemTagsCache(context.Background(), db)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlPopulateSystemTagsCache_ClearError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Expect DELETE statement to fail
	mock.ExpectPrepare("DELETE FROM SystemTagsCache").
		ExpectExec().
		WillReturnError(sql.ErrConnDone)

	err = sqlPopulateSystemTagsCache(context.Background(), db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to clear system tags cache")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetSystemTagsCached_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systems := []systemdefs.System{
		{ID: "nes"},
		{ID: "snes"},
	}

	// Mock system lookups - single prepared statement, multiple queries
	systemStmt := mock.ExpectPrepare("SELECT DBID FROM Systems WHERE SystemID = ?")
	systemStmt.ExpectQuery().WithArgs("nes").
		WillReturnRows(sqlmock.NewRows([]string{"DBID"}).AddRow(1))
	systemStmt.ExpectQuery().WithArgs("snes").
		WillReturnRows(sqlmock.NewRows([]string{"DBID"}).AddRow(2))

	// Mock main query
	mock.ExpectPrepare(`SELECT DISTINCT TagType, Tag FROM SystemTagsCache WHERE SystemDBID IN.*`).
		ExpectQuery().WithArgs(1, 2).
		WillReturnRows(sqlmock.NewRows([]string{"TagType", "Tag"}).
			AddRow("genre", "Action").
			AddRow("genre", "Adventure").
			AddRow("year", "1990"))

	results, err := sqlGetSystemTagsCached(context.Background(), db, systems)

	require.NoError(t, err)
	assert.Len(t, results, 3)

	expectedTags := []database.TagInfo{
		{Type: "genre", Tag: "Action"},
		{Type: "genre", Tag: "Adventure"},
		{Type: "year", Tag: "1990"},
	}

	assert.Equal(t, expectedTags, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetSystemTagsCached_EmptySystems(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systems := []systemdefs.System{}

	_, err = sqlGetSystemTagsCached(context.Background(), db, systems)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no systems provided")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlInvalidateSystemTagsCache_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systems := []systemdefs.System{
		{ID: "nes"},
	}

	// Mock system lookup
	mock.ExpectPrepare("SELECT DBID FROM Systems WHERE SystemID = ?").
		ExpectQuery().WithArgs("nes").
		WillReturnRows(sqlmock.NewRows([]string{"DBID"}).AddRow(1))

	// Mock delete query
	mock.ExpectPrepare("DELETE FROM SystemTagsCache WHERE SystemDBID IN.*").
		ExpectExec().WithArgs(1).
		WillReturnResult(sqlmock.NewResult(0, 5)) // Deleted 5 rows

	err = sqlInvalidateSystemTagsCache(context.Background(), db, systems)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlInvalidateSystemTagsCache_EmptySystems(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systems := []systemdefs.System{}

	err = sqlInvalidateSystemTagsCache(context.Background(), db, systems)
	assert.NoError(t, err) // Should succeed with no-op
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlug_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemID := "snes"
	slug := "supermarioworld"
	tags := []database.TagFilter{}

	// Mock main query
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(systemID, slug).
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID"}).
			AddRow("snes", "Super Mario World", "/games/super-mario-world.smc", 1))

	// Mock tags query (now always called even when no tag filters)
	mock.ExpectPrepare("SELECT.*MediaDBID.*Tags\\.Tag.*TagTypes\\.Type.*").
		ExpectQuery().
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}))

	results, err := sqlSearchMediaBySlug(context.Background(), db, systemID, slug, tags)

	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "snes", results[0].SystemID)
	assert.Equal(t, "Super Mario World", results[0].Name)
	assert.Equal(t, "/games/super-mario-world.smc", results[0].Path)
	assert.Equal(t, int64(1), results[0].MediaID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlug_WithTags(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemID := "snes"
	slug := "supermarioworld"
	tags := []database.TagFilter{{Type: "region", Value: "usa"}, {Type: "genre", Value: "platform"}}

	// Mock main query with tag filtering (with EXISTS clauses - no HAVING COUNT arg)
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(systemID, slug, "region", "usa", "genre", "platform").
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID"}).
			AddRow("snes", "Super Mario World", "/games/super-mario-world-usa.smc", 1))

	// Mock tags query
	mock.ExpectPrepare("SELECT.*MediaDBID.*Tags\\.Tag.*TagTypes\\.Type.*").
		ExpectQuery().
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
			AddRow(1, "usa", "region").
			AddRow(1, "platform", "genre"))

	results, err := sqlSearchMediaBySlug(context.Background(), db, systemID, slug, tags)

	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "snes", results[0].SystemID)
	assert.Equal(t, "Super Mario World", results[0].Name)
	assert.Equal(t, "/games/super-mario-world-usa.smc", results[0].Path)
	assert.Equal(t, int64(1), results[0].MediaID)

	// Check tags are populated
	assert.Len(t, results[0].Tags, 2)
	assert.Contains(t, results[0].Tags, database.TagInfo{Tag: "usa", Type: "region"})
	assert.Contains(t, results[0].Tags, database.TagInfo{Tag: "platform", Type: "genre"})

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlug_MultipleResults(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemID := "genesis"
	slug := "sonic"
	tags := []database.TagFilter{}

	// Mock main query returning multiple results
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(systemID, slug).
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID"}).
			AddRow("genesis", "Sonic the Hedgehog", "/games/sonic.bin", 1).
			AddRow("genesis", "Sonic the Hedgehog 2", "/games/sonic2.bin", 2))

	// Mock tags query (now always called even when no tag filters)
	mock.ExpectPrepare("SELECT.*MediaDBID.*Tags\\.Tag.*TagTypes\\.Type.*").
		ExpectQuery().
		WithArgs(1, 2).
		WillReturnRows(sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}))

	results, err := sqlSearchMediaBySlug(context.Background(), db, systemID, slug, tags)

	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Check first result
	assert.Equal(t, "genesis", results[0].SystemID)
	assert.Equal(t, "Sonic the Hedgehog", results[0].Name)
	assert.Equal(t, "/games/sonic.bin", results[0].Path)
	assert.Equal(t, int64(1), results[0].MediaID)

	// Check second result
	assert.Equal(t, "genesis", results[1].SystemID)
	assert.Equal(t, "Sonic the Hedgehog 2", results[1].Name)
	assert.Equal(t, "/games/sonic2.bin", results[1].Path)
	assert.Equal(t, int64(2), results[1].MediaID)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlug_LoadsTagsWithoutFilters(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemID := "snes"
	slug := "supermarioworld"
	tags := []database.TagFilter{}

	// Mock main query
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(systemID, slug).
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID"}).
			AddRow("snes", "Super Mario World (USA)", "/games/smw-usa.smc", 1).
			AddRow("snes", "Super Mario World (Europe)", "/games/smw-eu.smc", 2))

	// Mock tags query - returns tags for both ROMs
	mock.ExpectPrepare("SELECT.*MediaDBID.*Tags\\.Tag.*TagTypes\\.Type.*").
		ExpectQuery().
		WithArgs(1, 2).
		WillReturnRows(sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}).
			AddRow(1, "en", "lang").
			AddRow(1, "us", "region").
			AddRow(2, "eu", "region"))

	results, err := sqlSearchMediaBySlug(context.Background(), db, systemID, slug, tags)

	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Verify first ROM has tags loaded
	assert.Equal(t, "Super Mario World (USA)", results[0].Name)
	assert.Len(t, results[0].Tags, 2)
	assert.Contains(t, results[0].Tags, database.TagInfo{Tag: "en", Type: "lang"})
	assert.Contains(t, results[0].Tags, database.TagInfo{Tag: "us", Type: "region"})

	// Verify second ROM has tags loaded
	assert.Equal(t, "Super Mario World (Europe)", results[1].Name)
	assert.Len(t, results[1].Tags, 1)
	assert.Contains(t, results[1].Tags, database.TagInfo{Tag: "eu", Type: "region"})

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlug_NoResults(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemID := "nes"
	slug := "nonexistent"
	tags := []database.TagFilter{}

	// Mock main query returning no results
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(systemID, slug).
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID"}))

	results, err := sqlSearchMediaBySlug(context.Background(), db, systemID, slug, tags)

	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlug_WithTagsNoResults(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemID := "snes"
	slug := "supermarioworld"
	tags := []database.TagFilter{{Type: "region", Value: "japan"}}

	// Mock main query returning no results (tag filter too restrictive, with EXISTS - no HAVING COUNT arg)
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(systemID, slug, "region", "japan").
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID"}))

	results, err := sqlSearchMediaBySlug(context.Background(), db, systemID, slug, tags)

	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlug_QueryError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemID := "snes"
	slug := "supermarioworld"
	tags := []database.TagFilter{}

	// Mock main query error
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(systemID, slug).
		WillReturnError(sql.ErrConnDone)

	results, err := sqlSearchMediaBySlug(context.Background(), db, systemID, slug, tags)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute media by slug search query")
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlug_TagsQueryError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemID := "snes"
	slug := "supermarioworld"
	tags := []database.TagFilter{{Type: "region", Value: "usa"}}

	// Mock main query success (with EXISTS clause - no HAVING COUNT arg)
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(systemID, slug, "region", "usa").
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID"}).
			AddRow("snes", "Super Mario World", "/games/super-mario-world.smc", 1))

	// Mock tags query error
	mock.ExpectPrepare("SELECT.*MediaDBID.*Tags\\.Tag.*TagTypes\\.Type.*").
		ExpectQuery().
		WithArgs(1).
		WillReturnError(sql.ErrTxDone)

	results, err := sqlSearchMediaBySlug(context.Background(), db, systemID, slug, tags)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute tags query")
	// Function returns partial results from main query even if tags query fails
	assert.Len(t, results, 1)
	assert.Equal(t, "snes", results[0].SystemID)
	assert.Equal(t, "Super Mario World", results[0].Name)
	assert.Equal(t, "/games/super-mario-world.smc", results[0].Path)
	assert.Equal(t, int64(1), results[0].MediaID)
	// Tags should be empty since tags query failed
	assert.Empty(t, results[0].Tags)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlug_ScanError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemID := "snes"
	slug := "supermarioworld"
	tags := []database.TagFilter{}

	// Mock main query with wrong column count (scan error)
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(systemID, slug).
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "Name"}). // Missing Path and MediaID
										AddRow("snes", "Super Mario World"))

	results, err := sqlSearchMediaBySlug(context.Background(), db, systemID, slug, tags)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to scan search result")
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaBySlug_TagsScanError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemID := "snes"
	slug := "supermarioworld"
	tags := []database.TagFilter{{Type: "region", Value: "usa"}}

	// Mock main query success (with EXISTS - no HAVING COUNT arg)
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(systemID, slug, "region", "usa").
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "Name", "Path", "MediaID"}).
			AddRow("snes", "Super Mario World", "/games/super-mario-world.smc", 1))

	// Mock tags query with wrong column count (scan error)
	mock.ExpectPrepare("SELECT.*MediaDBID.*Tags\\.Tag.*TagTypes\\.Type.*").
		ExpectQuery().
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"MediaDBID"}). // Missing Tag and Type
									AddRow(1))

	results, err := sqlSearchMediaBySlug(context.Background(), db, systemID, slug, tags)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to scan tags result")
	// Function returns partial results from main query even if tags query fails
	assert.Len(t, results, 1)
	assert.Equal(t, "snes", results[0].SystemID)
	assert.Equal(t, "Super Mario World", results[0].Name)
	assert.Equal(t, "/games/super-mario-world.smc", results[0].Path)
	assert.Equal(t, int64(1), results[0].MediaID)
	// Tags should be empty since tags query failed
	assert.Empty(t, results[0].Tags)
	assert.NoError(t, mock.ExpectationsWereMet())
}
