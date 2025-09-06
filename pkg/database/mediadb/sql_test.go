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
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/pkg/testing/sqlmock"
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
