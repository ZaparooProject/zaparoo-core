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
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSqlAddMediaHistory_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	entry := &database.MediaHistoryEntry{
		ID:             "test-uuid",
		StartTime:      now,
		SystemID:       "nes",
		SystemName:     "Nintendo Entertainment System",
		MediaPath:      "/games/mario.nes",
		MediaName:      "Super Mario Bros.",
		LauncherID:     "retroarch",
		PlayTime:       0,
		BootUUID:       "test-boot-uuid",
		MonotonicStart: 12345,
		DurationSec:    0,
		WallDuration:   0,
		TimeSkewFlag:   false,
		ClockReliable:  true,
		ClockSource:    helpers.ClockSourceSystem,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	expectedDBID := int64(42)
	mock.ExpectPrepare(`INSERT INTO MediaHistory.*VALUES`).
		ExpectExec().
		WithArgs(
			entry.ID,
			entry.StartTime.Unix(),
			entry.SystemID,
			entry.SystemName,
			entry.MediaPath,
			entry.MediaName,
			entry.LauncherID,
			entry.PlayTime,
			entry.BootUUID,
			entry.MonotonicStart,
			entry.DurationSec,
			entry.WallDuration,
			entry.TimeSkewFlag,
			entry.ClockReliable,
			entry.ClockSource,
			entry.CreatedAt.Unix(),
			entry.UpdatedAt.Unix(),
			nil,
		).
		WillReturnResult(sqlmock.NewResult(expectedDBID, 1))

	dbid, err := sqlAddMediaHistory(context.Background(), db, entry)
	require.NoError(t, err)
	assert.Equal(t, expectedDBID, dbid)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlAddMediaHistory_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	entry := &database.MediaHistoryEntry{
		ID:             "test-uuid",
		StartTime:      now,
		SystemID:       "nes",
		SystemName:     "Nintendo Entertainment System",
		MediaPath:      "/games/mario.nes",
		MediaName:      "Super Mario Bros.",
		LauncherID:     "retroarch",
		PlayTime:       0,
		BootUUID:       "test-boot-uuid",
		MonotonicStart: 12345,
		DurationSec:    0,
		WallDuration:   0,
		TimeSkewFlag:   false,
		ClockReliable:  true,
		ClockSource:    helpers.ClockSourceSystem,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	mock.ExpectPrepare(`INSERT INTO MediaHistory.*VALUES`).
		ExpectExec().
		WithArgs(
			entry.ID,
			entry.StartTime.Unix(),
			entry.SystemID,
			entry.SystemName,
			entry.MediaPath,
			entry.MediaName,
			entry.LauncherID,
			entry.PlayTime,
			entry.BootUUID,
			entry.MonotonicStart,
			entry.DurationSec,
			entry.WallDuration,
			entry.TimeSkewFlag,
			entry.ClockReliable,
			entry.ClockSource,
			entry.CreatedAt.Unix(),
			entry.UpdatedAt.Unix(),
			nil,
		).
		WillReturnError(sqlmock.ErrCancelled)

	dbid, err := sqlAddMediaHistory(context.Background(), db, entry)
	require.Error(t, err)
	assert.Equal(t, int64(0), dbid)
	assert.Contains(t, err.Error(), "failed to execute media history insert")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlUpdateMediaHistoryTime_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	dbid := int64(42)
	playTime := 300 // 5 minutes

	mock.ExpectPrepare(`UPDATE MediaHistory SET PlayTime.*WHERE DBID`).
		ExpectExec().
		WithArgs(playTime, playTime, sqlmock.AnyArg(), dbid).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = sqlUpdateMediaHistoryTime(context.Background(), db, dbid, playTime)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlUpdateMediaHistoryTime_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	dbid := int64(42)
	playTime := 300

	mock.ExpectPrepare(`UPDATE MediaHistory SET PlayTime.*WHERE DBID`).
		ExpectExec().
		WithArgs(playTime, playTime, sqlmock.AnyArg(), dbid).
		WillReturnError(sqlmock.ErrCancelled)

	err = sqlUpdateMediaHistoryTime(context.Background(), db, dbid, playTime)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute media history time update")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCloseMediaHistory_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	dbid := int64(42)
	endTime := time.Now()
	playTime := 600 // 10 minutes

	mock.ExpectPrepare(`UPDATE MediaHistory SET EndTime.*WHERE DBID`).
		ExpectExec().
		WithArgs(endTime.Unix(), playTime, playTime, sqlmock.AnyArg(), endTime.Unix(), dbid).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = sqlCloseMediaHistory(context.Background(), db, dbid, endTime, playTime)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCloseMediaHistory_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	dbid := int64(42)
	endTime := time.Now()
	playTime := 600

	mock.ExpectPrepare(`UPDATE MediaHistory SET EndTime.*WHERE DBID`).
		ExpectExec().
		WithArgs(endTime.Unix(), playTime, playTime, sqlmock.AnyArg(), endTime.Unix(), dbid).
		WillReturnError(sqlmock.ErrCancelled)

	err = sqlCloseMediaHistory(context.Background(), db, dbid, endTime, playTime)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute media history close")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetMediaHistory_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	lastID := 0
	limit := 10
	now := time.Now()
	startTime := now.Add(-1 * time.Hour).Unix()
	endTime := now.Unix()

	rows := sqlmock.NewRows([]string{
		"DBID", "ID", "StartTime", "EndTime", "SystemID", "SystemName",
		"MediaPath", "MediaName", "LauncherID", "PlayTime",
		"BootUUID", "MonotonicStart", "DurationSec", "WallDuration", "TimeSkewFlag",
		"ClockReliable", "ClockSource", "CreatedAt", "UpdatedAt", "DeviceID",
	}).
		AddRow(
			int64(1), "uuid-1", startTime, endTime, "nes", "Nintendo Entertainment System",
			"/games/mario.nes", "Super Mario Bros.", "retroarch", 3600,
			"boot-1", int64(1000), 3600, 3600, false,
			true, "system", startTime, startTime, nil,
		).
		AddRow(
			int64(2), "uuid-2", startTime, endTime, "snes", "Super Nintendo",
			"/games/zelda.sfc", "The Legend of Zelda", "retroarch", 7200,
			"boot-1", int64(2000), 7200, 7200, false,
			true, "system", startTime, startTime, nil,
		)

	mock.ExpectPrepare(`SELECT.*FROM MediaHistory.*ORDER BY DBID DESC LIMIT`).
		ExpectQuery().
		WithArgs(2147483646, limit). // lastID=0 becomes 2147483646 in implementation
		WillReturnRows(rows)

	entries, err := sqlGetMediaHistory(context.Background(), db, lastID, limit)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
	assert.Equal(t, int64(1), entries[0].DBID)
	assert.Equal(t, "Super Mario Bros.", entries[0].MediaName)
	assert.Equal(t, 3600, entries[0].PlayTime)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetMediaHistory_EmptyResult(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	lastID := 0
	limit := 10

	rows := sqlmock.NewRows([]string{
		"DBID", "ID", "StartTime", "EndTime", "SystemID", "SystemName",
		"MediaPath", "MediaName", "LauncherID", "PlayTime",
		"BootUUID", "MonotonicStart", "DurationSec", "WallDuration", "TimeSkewFlag",
		"ClockReliable", "ClockSource", "CreatedAt", "UpdatedAt", "DeviceID",
	})

	mock.ExpectPrepare(`SELECT.*FROM MediaHistory.*ORDER BY DBID DESC LIMIT`).
		ExpectQuery().
		WithArgs(2147483646, limit). // lastID=0 becomes 2147483646 in implementation
		WillReturnRows(rows)

	entries, err := sqlGetMediaHistory(context.Background(), db, lastID, limit)
	require.NoError(t, err)
	assert.Empty(t, entries)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetMediaHistory_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	lastID := 0
	limit := 10

	mock.ExpectPrepare(`SELECT.*FROM MediaHistory.*ORDER BY DBID DESC LIMIT`).
		WillReturnError(sqlmock.ErrCancelled)

	entries, err := sqlGetMediaHistory(context.Background(), db, lastID, limit)
	require.Error(t, err)
	assert.NotNil(t, entries) // Returns empty slice, not nil
	assert.Empty(t, entries)
	assert.Contains(t, err.Error(), "failed to prepare media history query statement")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCloseHangingMediaHistory_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectPrepare(`UPDATE MediaHistory SET EndTime.*WHERE EndTime IS NULL`).
		ExpectExec().
		WillReturnResult(sqlmock.NewResult(0, 2))

	err = sqlCloseHangingMediaHistory(context.Background(), db)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCloseHangingMediaHistory_NoHangingEntries(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectPrepare(`UPDATE MediaHistory SET EndTime.*WHERE EndTime IS NULL`).
		ExpectExec().
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = sqlCloseHangingMediaHistory(context.Background(), db)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCloseHangingMediaHistory_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectPrepare(`UPDATE MediaHistory SET EndTime.*WHERE EndTime IS NULL`).
		ExpectExec().
		WillReturnError(sqlmock.ErrCancelled)

	err = sqlCloseHangingMediaHistory(context.Background(), db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to close hanging media entries")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCleanupMediaHistory_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	retentionDays := 365
	rowsDeleted := int64(10)

	mock.ExpectPrepare(`DELETE FROM MediaHistory WHERE StartTime`).
		ExpectExec().
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, rowsDeleted))

	mock.ExpectExec(`vacuum`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	rowsAffected, err := sqlCleanupMediaHistory(context.Background(), db, retentionDays)
	require.NoError(t, err)
	assert.Equal(t, rowsDeleted, rowsAffected)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCleanupMediaHistory_NoRowsToDelete(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	retentionDays := 365

	mock.ExpectPrepare(`DELETE FROM MediaHistory WHERE StartTime`).
		ExpectExec().
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	// No VACUUM expected when no rows deleted

	rowsAffected, err := sqlCleanupMediaHistory(context.Background(), db, retentionDays)
	require.NoError(t, err)
	assert.Equal(t, int64(0), rowsAffected)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCleanupMediaHistory_DeleteError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	retentionDays := 365

	mock.ExpectPrepare(`DELETE FROM MediaHistory WHERE StartTime`).
		ExpectExec().
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(sqlmock.ErrCancelled)

	rowsAffected, err := sqlCleanupMediaHistory(context.Background(), db, retentionDays)
	require.Error(t, err)
	assert.Equal(t, int64(0), rowsAffected)
	assert.Contains(t, err.Error(), "failed to execute media history cleanup")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCleanupMediaHistory_VacuumError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	retentionDays := 365
	rowsDeleted := int64(5)

	mock.ExpectPrepare(`DELETE FROM MediaHistory WHERE StartTime`).
		ExpectExec().
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, rowsDeleted))

	mock.ExpectExec(`vacuum`).
		WillReturnError(sqlmock.ErrCancelled)

	rowsAffected, err := sqlCleanupMediaHistory(context.Background(), db, retentionDays)
	require.Error(t, err)
	assert.Equal(t, rowsDeleted, rowsAffected)
	assert.Contains(t, err.Error(), "cleanup succeeded but vacuum failed")
	assert.NoError(t, mock.ExpectationsWereMet())
}
