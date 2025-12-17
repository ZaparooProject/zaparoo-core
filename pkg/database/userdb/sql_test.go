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
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSqlAddHistory_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	entry := database.HistoryEntry{
		ID:             "test-uuid",
		Time:           now,
		Type:           "nfc",
		TokenID:        "test-token-id",
		TokenValue:     "test-value",
		TokenData:      "test-data",
		Success:        true,
		ClockReliable:  true,
		BootUUID:       "test-boot-uuid",
		MonotonicStart: 123,
		CreatedAt:      now,
	}

	mock.ExpectPrepare(`insert into History.*values`).
		ExpectExec().
		WithArgs(
			entry.ID, entry.Time.Unix(), entry.Type, entry.TokenID,
			entry.TokenValue, entry.TokenData, entry.Success,
			entry.ClockReliable, entry.BootUUID, entry.MonotonicStart, entry.CreatedAt.Unix(), nil,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = sqlAddHistory(context.Background(), db, entry)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlAddHistory_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	entry := database.HistoryEntry{
		ID:             "test-uuid",
		Time:           now,
		Type:           "nfc",
		TokenID:        "test-token-id",
		TokenValue:     "test-value",
		TokenData:      "test-data",
		Success:        true,
		ClockReliable:  true,
		BootUUID:       "test-boot-uuid",
		MonotonicStart: 123,
		CreatedAt:      now,
	}

	mock.ExpectPrepare(`insert into History.*values`).
		ExpectExec().
		WithArgs(
			entry.ID, entry.Time.Unix(), entry.Type, entry.TokenID,
			entry.TokenValue, entry.TokenData, entry.Success,
			entry.ClockReliable, entry.BootUUID, entry.MonotonicStart, entry.CreatedAt.Unix(), nil,
		).
		WillReturnError(sqlmock.ErrCancelled)

	err = sqlAddHistory(context.Background(), db, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute history insert")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetHistoryWithOffset_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Unix(1672531200, 0)
	expectedEntries := []database.HistoryEntry{
		{
			DBID:           2,
			ID:             "uuid-2",
			Time:           now,
			Type:           "nfc",
			TokenID:        "token-2",
			TokenValue:     "value-2",
			TokenData:      "data-2",
			Success:        true,
			ClockReliable:  true,
			BootUUID:       "boot-1",
			MonotonicStart: 100,
			CreatedAt:      now,
		},
		{
			DBID:           1,
			ID:             "uuid-1",
			Time:           time.Unix(1672531100, 0),
			Type:           "barcode",
			TokenID:        "token-1",
			TokenValue:     "value-1",
			TokenData:      "data-1",
			Success:        false,
			ClockReliable:  true,
			BootUUID:       "boot-1",
			MonotonicStart: 50,
			CreatedAt:      time.Unix(1672531100, 0),
		},
	}

	rows := sqlmock.NewRows([]string{
		"DBID", "ID", "Time", "Type", "TokenID", "TokenValue", "TokenData",
		"Success", "ClockReliable", "BootUUID", "MonotonicStart", "CreatedAt", "DeviceID",
	})
	for _, entry := range expectedEntries {
		rows.AddRow(
			entry.DBID, entry.ID, entry.Time.Unix(), entry.Type, entry.TokenID,
			entry.TokenValue, entry.TokenData, entry.Success,
			entry.ClockReliable, entry.BootUUID, entry.MonotonicStart, entry.CreatedAt.Unix(), nil,
		)
	}

	mock.ExpectPrepare(`select.*from History.*where DBID.*order by.*limit`).
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
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"DBID", "Time", "Type", "TokenID", "TokenValue", "TokenData", "Success"})

	mock.ExpectPrepare(`select.*from History.*where DBID.*order by.*limit`).
		ExpectQuery().
		WithArgs(2147483646). // MaxInt32-1: sentinel value when lastID=0 to get latest records
		WillReturnRows(rows)

	result, err := sqlGetHistoryWithOffset(context.Background(), db, 0)
	require.NoError(t, err)
	assert.Empty(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetHistoryWithOffset_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectPrepare(`select.*from History.*where DBID.*order by.*limit`).
		ExpectQuery().
		WithArgs(100).
		WillReturnError(sqlmock.ErrCancelled)

	result, err := sqlGetHistoryWithOffset(context.Background(), db, 100)
	require.Error(t, err)
	assert.Empty(t, result) // Should be empty slice, not nil
	assert.Contains(t, err.Error(), "failed to query history")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Mapping Operation Tests

func TestSqlAddMapping_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
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

	mock.ExpectPrepare(`insert into Mappings.*values`).
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
	db, mock, err := testsqlmock.NewSQLMock()
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

	mock.ExpectPrepare(`select.*from Mappings.*where DBID`).
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
	db, mock, err := testsqlmock.NewSQLMock()
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

	mock.ExpectPrepare(`update Mappings set.*where DBID`).
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
	db, mock, err := testsqlmock.NewSQLMock()
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

	mock.ExpectPrepare(`update Mappings set.*where DBID`).
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
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectPrepare(`delete from Mappings where DBID`).
		ExpectExec().
		WithArgs(int64(123)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = sqlDeleteMapping(context.Background(), db, 123)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlDeleteMapping_NotFound(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectPrepare(`delete from Mappings where DBID`).
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
	db, mock, err := testsqlmock.NewSQLMock()
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

	mock.ExpectPrepare(`select.*from Mappings`).
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
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"DBID", "Added", "Label", "Enabled", "Type", "Match", "Pattern", "Override"})

	mock.ExpectPrepare(`select.*from Mappings`).
		ExpectQuery().
		WillReturnRows(rows)

	result, err := sqlGetAllMappings(context.Background(), db)
	require.NoError(t, err)
	assert.Empty(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetEnabledMappings_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
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

	mock.ExpectPrepare(`select.*from Mappings.*where Enabled`).
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
	db, mock, err := testsqlmock.NewSQLMock()
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

	mock.ExpectPrepare(`insert into Mappings.*values`).
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
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectPrepare(`select.*from Mappings.*where DBID`).
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
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	host := "example.com"
	zapscript := 1

	mock.ExpectPrepare(`INSERT INTO ZapLinkHosts.*ON CONFLICT.*UPDATE`).
		ExpectExec().
		WithArgs(host, zapscript).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = sqlUpdateZapLinkHost(context.Background(), db, host, zapscript)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlUpdateZapLinkHost_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	host := "example.com"
	zapscript := 1

	mock.ExpectPrepare(`INSERT INTO ZapLinkHosts.*ON CONFLICT.*UPDATE`).
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
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	host := "example.com"
	zapscript := 1

	rows := sqlmock.NewRows([]string{"ZapScript"}).
		AddRow(zapscript)

	mock.ExpectQuery(`SELECT.*FROM ZapLinkHosts WHERE Host`).
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
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	host := "unknown.com"

	mock.ExpectQuery(`SELECT.*FROM ZapLinkHosts WHERE Host`).
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
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	url := "https://example.com/game"
	zapscript := "launch('game.exe')"

	mock.ExpectPrepare(`INSERT INTO ZapLinkCache.*ON CONFLICT.*UPDATE`).
		ExpectExec().
		WithArgs(url, zapscript).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = sqlUpdateZapLinkCache(context.Background(), db, url, zapscript)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlUpdateZapLinkCache_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	url := "https://example.com/game"
	zapscript := "launch('game.exe')"

	mock.ExpectPrepare(`INSERT INTO ZapLinkCache.*ON CONFLICT.*UPDATE`).
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
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	url := "https://example.com/game"
	expectedZapscript := "launch('game.exe')"

	rows := sqlmock.NewRows([]string{"ZapScript"}).
		AddRow(expectedZapscript)

	mock.ExpectQuery(`SELECT.*FROM ZapLinkCache WHERE URL`).
		WithArgs(url).
		WillReturnRows(rows)

	result, err := sqlGetZapLinkCache(context.Background(), db, url)
	require.NoError(t, err)
	assert.Equal(t, expectedZapscript, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetZapLinkCache_NotFound(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	url := "https://unknown.com/game"

	mock.ExpectQuery(`SELECT.*FROM ZapLinkCache WHERE URL`).
		WithArgs(url).
		WillReturnError(sql.ErrNoRows)

	result, err := sqlGetZapLinkCache(context.Background(), db, url)
	require.NoError(t, err)
	assert.Empty(t, result) // Returns empty string when not found
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Database Management Function Tests

func TestSqlTruncate_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`delete from History.*delete from Mappings.*vacuum`).
		WillReturnResult(sqlmock.NewResult(0, 2)) // 2 tables affected

	err = sqlTruncate(context.Background(), db)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlTruncate_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`delete from History.*delete from Mappings.*vacuum`).
		WillReturnError(sqlmock.ErrCancelled)

	err = sqlTruncate(context.Background(), db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to truncate database")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlVacuum_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`vacuum`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = sqlVacuum(context.Background(), db)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlVacuum_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`vacuum`).
		WillReturnError(sqlmock.ErrCancelled)

	err = sqlVacuum(context.Background(), db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to vacuum database")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCleanupHistory_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	retentionDays := 30
	rowsDeleted := int64(5)

	// Expect DELETE query with time parameter
	mock.ExpectPrepare(`DELETE FROM History WHERE Time`).
		ExpectExec().
		WithArgs(sqlmock.AnyArg()). // Time cutoff will be calculated dynamically
		WillReturnResult(sqlmock.NewResult(0, rowsDeleted))

	// Expect VACUUM after delete
	mock.ExpectExec(`vacuum`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	rowsAffected, err := sqlCleanupHistory(context.Background(), db, retentionDays)
	require.NoError(t, err)
	assert.Equal(t, rowsDeleted, rowsAffected)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCleanupHistory_NoRowsToDelete(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	retentionDays := 30

	// Expect DELETE query but return 0 rows affected
	mock.ExpectPrepare(`DELETE FROM History WHERE Time`).
		ExpectExec().
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	// No VACUUM expected when no rows deleted

	rowsAffected, err := sqlCleanupHistory(context.Background(), db, retentionDays)
	require.NoError(t, err)
	assert.Equal(t, int64(0), rowsAffected)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCleanupHistory_DeleteError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	retentionDays := 30

	mock.ExpectPrepare(`DELETE FROM History WHERE Time`).
		ExpectExec().
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(sqlmock.ErrCancelled)

	rowsAffected, err := sqlCleanupHistory(context.Background(), db, retentionDays)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute history cleanup")
	assert.Equal(t, int64(0), rowsAffected)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCleanupHistory_VacuumError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	retentionDays := 30
	rowsDeleted := int64(3)

	// DELETE succeeds
	mock.ExpectPrepare(`DELETE FROM History WHERE Time`).
		ExpectExec().
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, rowsDeleted))

	// VACUUM fails
	mock.ExpectExec(`vacuum`).
		WillReturnError(sqlmock.ErrCancelled)

	rowsAffected, err := sqlCleanupHistory(context.Background(), db, retentionDays)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cleanup succeeded but vacuum failed")
	assert.Equal(t, rowsDeleted, rowsAffected) // Still returns rows deleted even if vacuum fails
	assert.NoError(t, mock.ExpectationsWereMet())
}

// GetSupportedZapLinkHosts Tests

func TestSqlGetSupportedZapLinkHosts_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectedHosts := []string{
		"https://example.com",
		"https://zaplink.io",
		"http://localhost:8080",
	}

	rows := sqlmock.NewRows([]string{"Host"})
	for _, host := range expectedHosts {
		rows.AddRow(host)
	}

	mock.ExpectQuery(`SELECT Host FROM ZapLinkHosts WHERE ZapScript > 0`).
		WillReturnRows(rows)

	result, err := sqlGetSupportedZapLinkHosts(context.Background(), db)
	require.NoError(t, err)
	assert.Len(t, result, 3)
	assert.Equal(t, expectedHosts, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetSupportedZapLinkHosts_Empty(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"Host"})

	mock.ExpectQuery(`SELECT Host FROM ZapLinkHosts WHERE ZapScript > 0`).
		WillReturnRows(rows)

	result, err := sqlGetSupportedZapLinkHosts(context.Background(), db)
	require.NoError(t, err)
	assert.Empty(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetSupportedZapLinkHosts_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`SELECT Host FROM ZapLinkHosts WHERE ZapScript > 0`).
		WillReturnError(sqlmock.ErrCancelled)

	result, err := sqlGetSupportedZapLinkHosts(context.Background(), db)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to query supported zap link hosts")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// PruneExpiredZapLinkHosts Tests

func TestSqlPruneExpiredZapLinkHosts_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rowsDeleted := int64(5)
	olderThan := 30 * 24 * time.Hour

	mock.ExpectExec(`DELETE FROM ZapLinkHosts WHERE ZapScript = 0 AND datetime\(CheckedAt\) < datetime\(\?\)`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, rowsDeleted))

	result, err := sqlPruneExpiredZapLinkHosts(context.Background(), db, olderThan)
	require.NoError(t, err)
	assert.Equal(t, rowsDeleted, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlPruneExpiredZapLinkHosts_NoRowsToDelete(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	olderThan := 30 * 24 * time.Hour

	mock.ExpectExec(`DELETE FROM ZapLinkHosts WHERE ZapScript = 0 AND datetime\(CheckedAt\) < datetime\(\?\)`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	result, err := sqlPruneExpiredZapLinkHosts(context.Background(), db, olderThan)
	require.NoError(t, err)
	assert.Equal(t, int64(0), result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlPruneExpiredZapLinkHosts_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	olderThan := 30 * 24 * time.Hour

	mock.ExpectExec(`DELETE FROM ZapLinkHosts WHERE ZapScript = 0 AND datetime\(CheckedAt\) < datetime\(\?\)`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(sqlmock.ErrCancelled)

	result, err := sqlPruneExpiredZapLinkHosts(context.Background(), db, olderThan)
	require.Error(t, err)
	assert.Equal(t, int64(0), result)
	assert.Contains(t, err.Error(), "failed to prune expired zap link hosts")
	assert.NoError(t, mock.ExpectationsWereMet())
}
