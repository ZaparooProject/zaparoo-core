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
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetGetScrapingStatus(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mediaDB := &MediaDB{sql: db, ctx: context.Background()}

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigScrapingStatus, IndexingStatusRunning).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
		WithArgs(DBConfigScrapingStatus).
		WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow(IndexingStatusRunning))

	require.NoError(t, mediaDB.SetScrapingStatus(IndexingStatusRunning))
	status, err := mediaDB.GetScrapingStatus()
	require.NoError(t, err)
	assert.Equal(t, IndexingStatusRunning, status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetScrapingStatusNoRows(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mediaDB := &MediaDB{sql: db, ctx: context.Background()}
	mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
		WithArgs(DBConfigScrapingStatus).
		WillReturnError(sql.ErrNoRows)

	status, err := mediaDB.GetScrapingStatus()
	require.NoError(t, err)
	assert.Empty(t, status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetScrapingOperationNoRows(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mediaDB := &MediaDB{sql: db, ctx: context.Background()}
	mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
		WithArgs(DBConfigScrapingOperation).
		WillReturnError(sql.ErrNoRows)

	operation, found, err := mediaDB.GetScrapingOperation()
	require.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, database.ScrapingOperation{}, operation)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSetGetClearScrapingOperation(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mediaDB := &MediaDB{sql: db, ctx: context.Background()}
	operation := database.ScrapingOperation{
		ScraperID: "gamelistxml",
		Systems:   []string{"snes", "genesis"},
		Force:     true,
	}
	operationJSON := `{"scraperId":"gamelistxml","systems":["snes","genesis"],"force":true}`

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigScrapingOperation, operationJSON).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
		WithArgs(DBConfigScrapingOperation).
		WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow(operationJSON))
	mock.ExpectExec("DELETE FROM DBConfig WHERE Name = ?").
		WithArgs(DBConfigScrapingOperation).
		WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, mediaDB.SetScrapingOperation(operation))
	got, found, err := mediaDB.GetScrapingOperation()
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, operation, got)
	require.NoError(t, mediaDB.ClearScrapingOperation())
	assert.NoError(t, mock.ExpectationsWereMet())
}
