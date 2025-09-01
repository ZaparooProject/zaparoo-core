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

package userdb

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSqlAddHistory_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	entry := database.HistoryEntry{
		Time:       time.Now(),
		Type:       "nfc",
		TokenID:    "test-token-id",
		TokenValue: "test-value",
		TokenData:  "test-data",
		Success:    true,
	}

	mock.ExpectPrepare(regexp.QuoteMeta(`
		insert into History(
			Time, Type, TokenID, TokenValue, TokenData, Success
		) values (?, ?, ?, ?, ?, ?);
	`)).
		ExpectExec().
		WithArgs(entry.Time.Unix(), entry.Type, entry.TokenID, entry.TokenValue, entry.TokenData, entry.Success).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = sqlAddHistory(context.Background(), db, entry)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlAddHistory_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	entry := database.HistoryEntry{
		Time:       time.Now(),
		Type:       "nfc",
		TokenID:    "test-token-id",
		TokenValue: "test-value",
		TokenData:  "test-data",
		Success:    true,
	}

	mock.ExpectPrepare(regexp.QuoteMeta(`
		insert into History(
			Time, Type, TokenID, TokenValue, TokenData, Success
		) values (?, ?, ?, ?, ?, ?);
	`)).
		ExpectExec().
		WithArgs(entry.Time.Unix(), entry.Type, entry.TokenID, entry.TokenValue, entry.TokenData, entry.Success).
		WillReturnError(sqlmock.ErrCancelled)

	err = sqlAddHistory(context.Background(), db, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute history insert")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetHistoryWithOffset_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectedEntries := []database.HistoryEntry{
		{
			DBID:       2,
			Time:       time.Unix(1672531200, 0),
			Type:       "nfc",
			TokenID:    "token-2",
			TokenValue: "value-2",
			TokenData:  "data-2",
			Success:    true,
		},
		{
			DBID:       1,
			Time:       time.Unix(1672531100, 0),
			Type:       "barcode",
			TokenID:    "token-1",
			TokenValue: "value-1",
			TokenData:  "data-1",
			Success:    false,
		},
	}

	rows := sqlmock.NewRows([]string{"DBID", "Time", "Type", "TokenID", "TokenValue", "TokenData", "Success"})
	for _, entry := range expectedEntries {
		rows.AddRow(entry.DBID, entry.Time.Unix(), entry.Type, entry.TokenID,
			entry.TokenValue, entry.TokenData, entry.Success)
	}

	mock.ExpectPrepare(regexp.QuoteMeta(`
		select 
		DBID, Time, Type, TokenID, TokenValue, TokenData, Success
		from History
		where DBID < ?
		order by DBID DESC
		limit 25;
	`)).
		ExpectQuery().
		WithArgs(100).
		WillReturnRows(rows)

	result, err := sqlGetHistoryWithOffset(context.Background(), db, 100)
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, expectedEntries[0].DBID, result[0].DBID)
	assert.Equal(t, expectedEntries[0].TokenID, result[0].TokenID)
	assert.Equal(t, expectedEntries[1].DBID, result[1].DBID)
	assert.Equal(t, expectedEntries[1].TokenID, result[1].TokenID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetHistoryWithOffset_NoRows(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"DBID", "Time", "Type", "TokenID", "TokenValue", "TokenData", "Success"})

	mock.ExpectPrepare(regexp.QuoteMeta(`
		select 
		DBID, Time, Type, TokenID, TokenValue, TokenData, Success
		from History
		where DBID < ?
		order by DBID DESC
		limit 25;
	`)).
		ExpectQuery().
		WithArgs(2147483646).
		WillReturnRows(rows)

	result, err := sqlGetHistoryWithOffset(context.Background(), db, 0)
	require.NoError(t, err)
	assert.Empty(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetHistoryWithOffset_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectPrepare(regexp.QuoteMeta(`
		select 
		DBID, Time, Type, TokenID, TokenValue, TokenData, Success
		from History
		where DBID < ?
		order by DBID DESC
		limit 25;
	`)).
		ExpectQuery().
		WithArgs(100).
		WillReturnError(sqlmock.ErrCancelled)

	result, err := sqlGetHistoryWithOffset(context.Background(), db, 100)
	require.Error(t, err)
	assert.NotNil(t, result) // Function returns empty slice, not nil
	assert.Empty(t, result)  // Should be empty slice
	assert.Contains(t, err.Error(), "failed to query history")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Mapping Operation Tests

func TestSqlAddMapping_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mapping := database.Mapping{
		Added:    time.Now().Unix(),
		Label:    "Test Mapping",
		Enabled:  true,
		Type:     "nfc",
		Match:    "exact",
		Pattern:  "test-pattern",
		Override: "test-override",
	}

	mock.ExpectPrepare(regexp.QuoteMeta(`
		insert into Mappings(
			Added, Label, Enabled, Type, Match, Pattern, Override
		) values (?, ?, ?, ?, ?, ?, ?);
	`)).
		ExpectExec().
		WithArgs(mapping.Added, mapping.Label, mapping.Enabled, mapping.Type,
			mapping.Match, mapping.Pattern, mapping.Override).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = sqlAddMapping(context.Background(), db, mapping)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetMapping_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectedMapping := database.Mapping{
		DBID:     123,
		Added:    1672531200,
		Label:    "Test Mapping",
		Enabled:  true,
		Type:     "nfc",
		Match:    "exact",
		Pattern:  "test-pattern",
		Override: "test-override",
	}

	rows := sqlmock.NewRows([]string{"DBID", "Added", "Label", "Enabled", "Type", "Match", "Pattern", "Override"}).
		AddRow(expectedMapping.DBID, expectedMapping.Added, expectedMapping.Label, expectedMapping.Enabled,
			expectedMapping.Type, expectedMapping.Match, expectedMapping.Pattern, expectedMapping.Override)

	mock.ExpectPrepare(regexp.QuoteMeta(`
		select
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings
		where DBID = ?;
	`)).
		ExpectQuery().
		WithArgs(int64(123)).
		WillReturnRows(rows)

	result, err := sqlGetMapping(context.Background(), db, 123)
	require.NoError(t, err)
	assert.Equal(t, expectedMapping.DBID, result.DBID)
	assert.Equal(t, expectedMapping.Label, result.Label)
	assert.Equal(t, expectedMapping.Type, result.Type)
	assert.Equal(t, expectedMapping.Enabled, result.Enabled)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlUpdateMapping_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mapping := database.Mapping{
		Added:    time.Now().Unix(),
		Label:    "Updated Mapping",
		Enabled:  false,
		Type:     "barcode",
		Match:    "glob",
		Pattern:  "updated-pattern",
		Override: "updated-override",
	}

	mock.ExpectPrepare(regexp.QuoteMeta(`
		update Mappings set
			Added = ?,
			Label = ?,
			Enabled = ?,
			Type = ?,
			Match = ?,
			Pattern = ?,
			Override = ?
		where
			DBID = ?;
	`)).
		ExpectExec().
		WithArgs(mapping.Added, mapping.Label, mapping.Enabled, mapping.Type,
			mapping.Match, mapping.Pattern, mapping.Override, int64(123)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = sqlUpdateMapping(context.Background(), db, 123, mapping)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlUpdateMapping_NotFound(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mapping := database.Mapping{
		Added:    time.Now().Unix(),
		Label:    "Updated Mapping",
		Enabled:  false,
		Type:     "barcode",
		Match:    "glob",
		Pattern:  "updated-pattern",
		Override: "updated-override",
	}

	mock.ExpectPrepare(regexp.QuoteMeta(`
		update Mappings set
			Added = ?,
			Label = ?,
			Enabled = ?,
			Type = ?,
			Match = ?,
			Pattern = ?,
			Override = ?
		where
			DBID = ?;
	`)).
		ExpectExec().
		WithArgs(mapping.Added, mapping.Label, mapping.Enabled, mapping.Type,
			mapping.Match, mapping.Pattern, mapping.Override, int64(999)).
		WillReturnError(sqlmock.ErrCancelled)

	err = sqlUpdateMapping(context.Background(), db, 999, mapping)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute update mapping statement")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlDeleteMapping_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectPrepare(regexp.QuoteMeta("delete from Mappings where DBID = ?;")).
		ExpectExec().
		WithArgs(int64(123)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = sqlDeleteMapping(context.Background(), db, 123)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlDeleteMapping_NotFound(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectPrepare(regexp.QuoteMeta("delete from Mappings where DBID = ?;")).
		ExpectExec().
		WithArgs(int64(999)).
		WillReturnError(sqlmock.ErrCancelled)

	err = sqlDeleteMapping(context.Background(), db, 999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute mapping delete")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetAllMappings_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectedMappings := []database.Mapping{
		{
			DBID:     1,
			Added:    1672531200,
			Label:    "First Mapping",
			Enabled:  true,
			Type:     "nfc",
			Match:    "exact",
			Pattern:  "pattern-1",
			Override: "override-1",
		},
		{
			DBID:     2,
			Added:    1672531300,
			Label:    "Second Mapping",
			Enabled:  false,
			Type:     "barcode",
			Match:    "glob",
			Pattern:  "pattern-2",
			Override: "override-2",
		},
	}

	rows := sqlmock.NewRows([]string{"DBID", "Added", "Label", "Enabled", "Type", "Match", "Pattern", "Override"})
	for _, mapping := range expectedMappings {
		rows.AddRow(mapping.DBID, mapping.Added, mapping.Label, mapping.Enabled,
			mapping.Type, mapping.Match, mapping.Pattern, mapping.Override)
	}

	mock.ExpectPrepare(regexp.QuoteMeta(`
		select
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings;
	`)).
		ExpectQuery().
		WillReturnRows(rows)

	result, err := sqlGetAllMappings(context.Background(), db)
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, expectedMappings[0].DBID, result[0].DBID)
	assert.Equal(t, expectedMappings[0].Label, result[0].Label)
	assert.Equal(t, expectedMappings[1].DBID, result[1].DBID)
	assert.Equal(t, expectedMappings[1].Label, result[1].Label)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetAllMappings_Empty(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"DBID", "Added", "Label", "Enabled", "Type", "Match", "Pattern", "Override"})

	mock.ExpectPrepare(regexp.QuoteMeta(`
		select
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings;
	`)).
		ExpectQuery().
		WillReturnRows(rows)

	result, err := sqlGetAllMappings(context.Background(), db)
	require.NoError(t, err)
	assert.Empty(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetEnabledMappings_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectedMappings := []database.Mapping{
		{
			DBID:     1,
			Added:    1672531200,
			Label:    "Enabled Mapping 1",
			Enabled:  true,
			Type:     "nfc",
			Match:    "exact",
			Pattern:  "pattern-1",
			Override: "override-1",
		},
		{
			DBID:     3,
			Added:    1672531400,
			Label:    "Enabled Mapping 2",
			Enabled:  true,
			Type:     "barcode",
			Match:    "glob",
			Pattern:  "pattern-3",
			Override: "override-3",
		},
	}

	rows := sqlmock.NewRows([]string{"DBID", "Added", "Label", "Enabled", "Type", "Match", "Pattern", "Override"})
	for _, mapping := range expectedMappings {
		rows.AddRow(mapping.DBID, mapping.Added, mapping.Label, mapping.Enabled,
			mapping.Type, mapping.Match, mapping.Pattern, mapping.Override)
	}

	mock.ExpectPrepare(regexp.QuoteMeta(`
		select
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings
		where Enabled = ?
	`)).
		ExpectQuery().
		WithArgs(true).
		WillReturnRows(rows)

	result, err := sqlGetEnabledMappings(context.Background(), db)
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.True(t, result[0].Enabled)
	assert.True(t, result[1].Enabled)
	assert.Equal(t, expectedMappings[0].Label, result[0].Label)
	assert.Equal(t, expectedMappings[1].Label, result[1].Label)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlAddMapping_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mapping := database.Mapping{
		Added:    time.Now().Unix(),
		Label:    "Test Mapping",
		Enabled:  true,
		Type:     "nfc",
		Match:    "exact",
		Pattern:  "test-pattern",
		Override: "test-override",
	}

	mock.ExpectPrepare(regexp.QuoteMeta(`
		insert into Mappings(
			Added, Label, Enabled, Type, Match, Pattern, Override
		) values (?, ?, ?, ?, ?, ?, ?);
	`)).
		ExpectExec().
		WithArgs(mapping.Added, mapping.Label, mapping.Enabled, mapping.Type,
			mapping.Match, mapping.Pattern, mapping.Override).
		WillReturnError(sqlmock.ErrCancelled)

	err = sqlAddMapping(context.Background(), db, mapping)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute mapping insert")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetMapping_NotFound(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectPrepare(regexp.QuoteMeta(`
		select
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings
		where DBID = ?;
	`)).
		ExpectQuery().
		WithArgs(int64(999)).
		WillReturnError(sqlmock.ErrCancelled)

	result, err := sqlGetMapping(context.Background(), db, 999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to scan mapping row")
	assert.Equal(t, database.Mapping{}, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ZapLink Operation Tests

func TestSqlUpdateZapLinkHost_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	host := "example.com"
	zapscript := 1

	mock.ExpectPrepare(regexp.QuoteMeta(`
		INSERT INTO ZapLinkHosts (Host, ZapScript, CheckedAt)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(Host) DO UPDATE SET
			ZapScript = excluded.ZapScript,
			CheckedAt = CURRENT_TIMESTAMP;
	`)).
		ExpectExec().
		WithArgs(host, zapscript).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = sqlUpdateZapLinkHost(context.Background(), db, host, zapscript)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlUpdateZapLinkHost_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	host := "example.com"
	zapscript := 1

	mock.ExpectPrepare(regexp.QuoteMeta(`
		INSERT INTO ZapLinkHosts (Host, ZapScript, CheckedAt)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(Host) DO UPDATE SET
			ZapScript = excluded.ZapScript,
			CheckedAt = CURRENT_TIMESTAMP;
	`)).
		ExpectExec().
		WithArgs(host, zapscript).
		WillReturnError(sqlmock.ErrCancelled)

	err = sqlUpdateZapLinkHost(context.Background(), db, host, zapscript)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute update zap link host statement")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetZapLinkHost_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	host := "example.com"
	zapscript := 1

	rows := sqlmock.NewRows([]string{"ZapScript"}).
		AddRow(zapscript)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT ZapScript FROM ZapLinkHosts WHERE Host = ?")).
		WithArgs(host).
		WillReturnRows(rows)

	supported, ok, err := sqlGetZapLinkHost(context.Background(), db, host)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, supported)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetZapLinkHost_NotFound(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	host := "unknown.com"

	mock.ExpectQuery(regexp.QuoteMeta("SELECT ZapScript FROM ZapLinkHosts WHERE Host = ?")).
		WithArgs(host).
		WillReturnError(sql.ErrNoRows)

	supported, ok, err := sqlGetZapLinkHost(context.Background(), db, host)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.False(t, supported)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlUpdateZapLinkCache_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	url := "https://example.com/game"
	zapscript := "launch('game.exe')"

	mock.ExpectPrepare(regexp.QuoteMeta(`
		INSERT INTO ZapLinkCache (URL, ZapScript, UpdatedAt)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(URL) DO UPDATE SET
			ZapScript = excluded.ZapScript,
			UpdatedAt = CURRENT_TIMESTAMP;
	`)).
		ExpectExec().
		WithArgs(url, zapscript).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = sqlUpdateZapLinkCache(context.Background(), db, url, zapscript)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlUpdateZapLinkCache_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	url := "https://example.com/game"
	zapscript := "launch('game.exe')"

	mock.ExpectPrepare(regexp.QuoteMeta(`
		INSERT INTO ZapLinkCache (URL, ZapScript, UpdatedAt)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(URL) DO UPDATE SET
			ZapScript = excluded.ZapScript,
			UpdatedAt = CURRENT_TIMESTAMP;
	`)).
		ExpectExec().
		WithArgs(url, zapscript).
		WillReturnError(sqlmock.ErrCancelled)

	err = sqlUpdateZapLinkCache(context.Background(), db, url, zapscript)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute update zap link cache statement")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetZapLinkCache_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	url := "https://example.com/game"
	expectedZapscript := "launch('game.exe')"

	rows := sqlmock.NewRows([]string{"ZapScript"}).
		AddRow(expectedZapscript)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT ZapScript FROM ZapLinkCache WHERE URL = ?")).
		WithArgs(url).
		WillReturnRows(rows)

	result, err := sqlGetZapLinkCache(context.Background(), db, url)
	require.NoError(t, err)
	assert.Equal(t, expectedZapscript, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetZapLinkCache_NotFound(t *testing.T) {
	t.Parallel()
	db, mock, err := helpers.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	url := "https://unknown.com/game"

	mock.ExpectQuery(regexp.QuoteMeta("SELECT ZapScript FROM ZapLinkCache WHERE URL = ?")).
		WithArgs(url).
		WillReturnError(sql.ErrNoRows)

	result, err := sqlGetZapLinkCache(context.Background(), db, url)
	require.NoError(t, err)
	assert.Empty(t, result) // Returns empty string when not found
	assert.NoError(t, mock.ExpectationsWereMet())
}
