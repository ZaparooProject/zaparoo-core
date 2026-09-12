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
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRollbackTransactionDiscardsPendingBatches(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"active", "sqlite_rolled_back", "sql_rolled_back"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			db, cleanup := setupTempMediaDB(t)
			defer cleanup()
			require.NoError(t, db.BeginTransaction(true))
			_, err := db.InsertSystem(database.System{DBID: 2, SystemID: "queued", Name: "Queued"})
			require.NoError(t, err)
			batch := db.batchInsertSystem
			switch mode {
			case "sqlite_rolled_back":
				_, err = db.tx.ExecContext(t.Context(),
					"INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'written', 'Written')")
				require.NoError(t, err)
				// ROLLBACK conflict resolution deterministically ends the SQLite transaction,
				// just as an interrupted write can, without canceling the batch context.
				_, err = db.tx.ExecContext(t.Context(),
					"INSERT OR ROLLBACK INTO Systems (DBID, SystemID, Name) VALUES (1, 'duplicate', 'Duplicate')")
				require.Error(t, err)
			case "sql_rolled_back":
				require.NoError(t, db.tx.Rollback())
			}
			rollbackErr := db.RollbackTransaction()
			var count int
			require.NoError(t, db.sql.Load().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM Systems").Scan(&count))
			assert.Zero(t, count, "abort cleanup must never persist buffered rows")
			require.NoError(t, rollbackErr)
			assert.Empty(t, batch.buffer)
			assert.Zero(t, batch.currentCount)
			assert.Empty(t, batch.stmtCache)
			assert.Nil(t, db.tx)
			assert.Nil(t, db.txConn)
			assert.False(t, db.inTransaction)
			require.NoError(t, db.BeginTransaction(true))
			_, err = db.InsertSystem(database.System{DBID: 3, SystemID: "committed", Name: "Committed"})
			require.NoError(t, err)
			require.NoError(t, db.CommitTransaction())
			require.NoError(t, db.sql.Load().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM Systems").Scan(&count))
			assert.Equal(t, 1, count, "ordinary commit must still flush pending rows")
		})
	}
}

func TestRollbackTransactionPreservesFailure(t *testing.T) {
	t.Parallel()
	sqlDB, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()
	mock.ExpectBegin()
	tx, err := sqlDB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	failure := errors.New("storage rollback failure")
	mock.ExpectRollback().WillReturnError(failure)
	db := &MediaDB{tx: tx, inTransaction: true}
	require.ErrorIs(t, db.RollbackTransaction(), failure)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRollbackAlreadyCompleteRequiresProof(t *testing.T) {
	t.Parallel()
	genericSQLite := sqlite3.Error{Code: sqlite3.ErrError, ExtendedCode: sqlite3.ErrError.Extend(0)}
	for _, tc := range []struct {
		err             error
		name            string
		ended, expected bool
	}{
		{name: "Go completed", err: fmt.Errorf("wrapped: %w", sql.ErrTxDone), expected: true},
		{name: "SQLite ended", err: genericSQLite, ended: true, expected: true},
		{name: "SQLite active or unknown", err: genericSQLite},
		{name: "I/O despite ended state", err: sqlite3.Error{Code: sqlite3.ErrIoErr}, ended: true},
		{
			name: "matching message is insufficient",
			err:  errors.New("cannot rollback - no transaction is active"), ended: true,
		},
		{name: "nil", ended: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, rollbackAlreadyComplete(tc.err, tc.ended))
		})
	}
}

func TestCommitTransactionDiscardsAfterAutoRollback(t *testing.T) {
	t.Parallel()
	for _, duringFlush := range []bool{false, true} {
		t.Run(fmt.Sprintf("during_flush_%t", duringFlush), func(t *testing.T) {
			t.Parallel()
			db, cleanup := setupTempMediaDB(t)
			defer cleanup()
			// The trigger simulates a write error that causes SQLite to end the transaction.
			_, err := db.sql.Load().ExecContext(t.Context(), `CREATE TABLE AbortBatch(value INTEGER);
    CREATE TRIGGER AbortSystems BEFORE INSERT ON Systems BEGIN
     SELECT RAISE(ROLLBACK, 'synthetic write failure'); END;`)
			require.NoError(t, err)
			require.NoError(t, db.BeginTransaction(true))
			_, err = db.InsertSystem(database.System{DBID: 1, SystemID: "failed", Name: "Failed"})
			require.NoError(t, err)
			later, err := NewBatchInserter(t.Context(), db.tx, "AbortBatch", []string{"value"}, 100)
			require.NoError(t, err)
			require.NoError(t, later.Add(99))
			require.NoError(t, db.batchInsertScanProperty.Close())
			db.batchInsertScanProperty = later
			if !duringFlush {
				_, err = db.tx.ExecContext(t.Context(),
					"INSERT INTO Systems (DBID,SystemID,Name) VALUES (2,'early','Early')")
				require.Error(t, err)
			}
			err = db.CommitTransaction()
			require.Error(t, err, "commit must not succeed after automatic rollback")
			var count int
			require.NoError(t, db.sql.Load().QueryRowContext(t.Context(),
				"SELECT COUNT(*) FROM AbortBatch").Scan(&count))
			assert.Zero(t, count, "later batches must not flush after failure")
			assert.Nil(t, db.tx)
			assert.Nil(t, db.txConn)
		})
	}
}

func TestRollbackTransactionPreservesStatementCloseFailure(t *testing.T) {
	t.Parallel()
	sqlDB, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()
	mock.ExpectBegin()
	tx, err := sqlDB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	failure := errors.New("statement close failure")
	mock.ExpectPrepare("INSERT INTO sample").WillBeClosed().WillReturnCloseError(failure)
	stmt, err := tx.PrepareContext(t.Context(), "INSERT INTO sample VALUES (?)")
	require.NoError(t, err)
	defer func() { _ = stmt.Close() }()
	mock.ExpectRollback()
	db := &MediaDB{tx: tx, batchInsertSystem: &BatchInserter{stmtCache: map[int]*sql.Stmt{1: stmt}}}
	require.ErrorIs(t, db.RollbackTransaction(), failure)
	require.NoError(t, mock.ExpectationsWereMet())
}
