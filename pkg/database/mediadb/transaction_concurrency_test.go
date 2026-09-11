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
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexingSystemsLockError(t *testing.T) {
	t.Parallel()
	for _, code := range []sqlite3.ErrNo{sqlite3.ErrBusy, sqlite3.ErrLocked} {
		cause := sqlite3.Error{Code: code, ExtendedCode: code.Extend(2)}
		original := fmt.Errorf("failed to set indexing systems: %w", cause)
		err := indexingStateLockError(
			original, "systems", database.MediaWriteOperationIndexing, true, 25*time.Millisecond,
		)
		require.ErrorIs(t, err, original)
		var got sqlite3.Error
		require.ErrorAs(t, err, &got)
		assert.Equal(t, cause.Code, got.Code)
		assert.Equal(t, cause.ExtendedCode, got.ExtendedCode)
		assert.Contains(t, err.Error(), fmt.Sprintf("sqlite_code=%d sqlite_extended_code=%d", code, code.Extend(2)))
		assert.Contains(t, err.Error(), "operation=indexing transaction=true sql_ms=25")
		assert.Less(t, len(err.Error()), 300)
	}
	for _, original := range []error{
		nil, errors.New("database is locked"), sqlite3.Error{Code: sqlite3.ErrIoErr},
	} {
		got := indexingStateLockError(original, "status", database.MediaWriteOperationNone, false, 0)
		assert.Equal(t, original, got)
	}
}

func TestMediaDB_IndexingStatusUsesActiveTransaction(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	require.NoError(t, db.SetIndexingStatus(IndexingStatusPending))
	require.NoError(t, db.BeginTransaction(false))
	t.Cleanup(func() { _ = db.RollbackTransaction() })
	_, err := db.tx.ExecContext(t.Context(),
		"UPDATE DBConfig SET Value = Value WHERE Name = ?", DBConfigIndexingStatus)
	require.NoError(t, err)
	require.NoError(t, db.SetIndexingStatus(IndexingStatusRunning))
	var value string
	require.NoError(t, db.tx.QueryRowContext(t.Context(),
		"SELECT Value FROM DBConfig WHERE Name = ?", DBConfigIndexingStatus).Scan(&value))
	assert.Equal(t, IndexingStatusRunning, value)
	got, err := db.GetIndexingStatus()
	require.NoError(t, err)
	assert.Equal(t, IndexingStatusPending, got)
	require.NoError(t, db.RollbackTransaction())
	got, err = db.GetIndexingStatus()
	require.NoError(t, err)
	assert.Equal(t, IndexingStatusPending, got)
}

func TestIndexingStatusLockErrorContext(t *testing.T) {
	t.Parallel()
	cause := sqlite3.Error{Code: sqlite3.ErrLocked, ExtendedCode: sqlite3.ErrLocked.Extend(1)}
	err := indexingStateLockError(cause, "status", database.MediaWriteOperationIndexing, false, time.Second)
	require.ErrorIs(t, err, cause)
	assert.Contains(t, err.Error(), "indexing status write")
	assert.Contains(t, err.Error(), "transaction=false sql_ms=1000")
}

func TestMediaDB_IndexingSystemsUsesActiveTransaction(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	require.NoError(t, db.SetIndexingSystems([]string{"NES"}))
	require.NoError(t, db.BeginTransaction(false))
	t.Cleanup(func() { _ = db.RollbackTransaction() })
	// Acquire SQLite's writer lock before exercising the setter. A pool-routed
	// write would contend with our transaction rather than participate in it.
	_, err := db.tx.ExecContext(t.Context(),
		"UPDATE DBConfig SET Value = Value WHERE Name = ?", DBConfigIndexingSystems)
	require.NoError(t, err)
	require.NoError(t, db.SetIndexingSystems([]string{"SNES"}))
	var value string
	require.NoError(t, db.tx.QueryRowContext(t.Context(),
		"SELECT Value FROM DBConfig WHERE Name = ?", DBConfigIndexingSystems).Scan(&value))
	assert.JSONEq(t, `["SNES"]`, value)
	// The reader pool must still see committed state, and rollback must discard
	// the setter's change. This proves transaction identity, not just success.
	got, err := db.GetIndexingSystems()
	require.NoError(t, err)
	assert.Equal(t, []string{"NES"}, got)
	require.NoError(t, db.RollbackTransaction())
	got, err = db.GetIndexingSystems()
	require.NoError(t, err)
	assert.Equal(t, []string{"NES"}, got)
}

func TestMediaDB_IndexingLeaseExcludesCompetingWriters(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	lease, err := db.AcquireMediaWrite(database.MediaWriteOperationIndexing)
	require.NoError(t, err)
	defer lease.Release()
	require.NoError(t, db.SetIndexingSystems([]string{"NES"}))

	for _, operation := range []database.MediaWriteOperation{
		database.MediaWriteOperationOptimization, database.MediaWriteOperationScraping,
	} {
		competing, acquireErr := db.AcquireMediaWrite(operation)
		if competing != nil {
			competing.Release()
		}
		require.ErrorIs(t, acquireErr, database.ErrMediaWriteConflict)
		require.Nil(t, competing)
	}
	var started atomic.Bool
	db.RunBackgroundOptimization(func(bool) { started.Store(true) }, syncutil.NewPauser())
	assert.False(t, started.Load(), "optimization must stop at ownership check")
	assert.Equal(t, database.MediaWriteOperationIndexing, db.ActiveMediaWriteOperation())
	got, err := db.GetIndexingSystems()
	require.NoError(t, err)
	assert.Equal(t, []string{"NES"}, got)

	lease.Release()
	for _, operation := range []database.MediaWriteOperation{
		database.MediaWriteOperationOptimization, database.MediaWriteOperationScraping,
	} {
		available, acquireErr := db.AcquireMediaWrite(operation)
		require.NoError(t, acquireErr)
		available.Release()
	}
}

// TestMediaDB_ConcurrentTransactionAttempts_OnlyOneSucceeds tests that only one
// transaction can be active at a time when multiple goroutines attempt to begin
// transactions concurrently.
func TestMediaDB_ConcurrentTransactionAttempts_OnlyOneSucceeds(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	const numGoroutines = 10
	var successCount atomic.Int32
	var alreadyInProgressCount atomic.Int32
	var wg sync.WaitGroup

	// Launch multiple goroutines that all try to begin a transaction
	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()

			err := mediaDB.BeginTransaction(false)
			switch {
			case err == nil:
				successCount.Add(1)
				// Hold the transaction briefly to ensure contention
				time.Sleep(50 * time.Millisecond)
				// Clean up the successful transaction
				_ = mediaDB.RollbackTransaction()
			case err.Error() == "transaction already in progress":
				alreadyInProgressCount.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}

	wg.Wait()

	// Verify that only one goroutine succeeded
	assert.Equal(t, int32(1), successCount.Load(),
		"exactly one transaction should succeed")

	// Verify that all other goroutines got the "already in progress" error
	assert.Equal(t, int32(numGoroutines-1), alreadyInProgressCount.Load(),
		"all other attempts should get 'transaction already in progress' error")
}

// TestMediaDB_TransactionAlreadyInProgress_ErrorMessage tests that attempting
// to begin a transaction while one is active returns the correct error.
func TestMediaDB_TransactionAlreadyInProgress_ErrorMessage(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Begin first transaction
	err := mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	// Attempt to begin second transaction
	err = mediaDB.BeginTransaction(false)
	require.Error(t, err)
	assert.Equal(t, "transaction already in progress", err.Error())

	// Clean up
	err = mediaDB.CommitTransaction()
	require.NoError(t, err)
}

// TestMediaDB_TransactionLifecycle_CanRestartAfterCommit tests that after
// committing a transaction, a new transaction can be started successfully.
func TestMediaDB_TransactionLifecycle_CanRestartAfterCommit(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// First transaction cycle
	err := mediaDB.BeginTransaction(false)
	require.NoError(t, err, "first BeginTransaction should succeed")

	system := database.System{
		DBID:     1,
		SystemID: "test-system-1",
		Name:     "Test System 1",
	}
	_, err = mediaDB.InsertSystem(system)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err, "first CommitTransaction should succeed")

	// Second transaction cycle should work
	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err, "second BeginTransaction should succeed after commit")

	system2 := database.System{
		DBID:     2,
		SystemID: "test-system-2",
		Name:     "Test System 2",
	}
	_, err = mediaDB.InsertSystem(system2)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err, "second CommitTransaction should succeed")

	// Verify both systems were committed
	foundSystem1, err := mediaDB.FindSystemBySystemID("test-system-1")
	require.NoError(t, err)
	assert.Equal(t, "test-system-1", foundSystem1.SystemID)

	foundSystem2, err := mediaDB.FindSystemBySystemID("test-system-2")
	require.NoError(t, err)
	assert.Equal(t, "test-system-2", foundSystem2.SystemID)
}

// TestMediaDB_TransactionLifecycle_CanRestartAfterRollback tests that after
// rolling back a transaction, a new transaction can be started successfully.
func TestMediaDB_TransactionLifecycle_CanRestartAfterRollback(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// First transaction - will be rolled back
	err := mediaDB.BeginTransaction(false)
	require.NoError(t, err, "first BeginTransaction should succeed")

	system := database.System{
		DBID:     1,
		SystemID: "test-system-rollback",
		Name:     "Test System Rollback",
	}
	_, err = mediaDB.InsertSystem(system)
	require.NoError(t, err)

	err = mediaDB.RollbackTransaction()
	require.NoError(t, err, "RollbackTransaction should succeed")

	// Verify data was not committed
	_, err = mediaDB.FindSystemBySystemID("test-system-rollback")
	require.Error(t, err)

	// Second transaction should work after rollback
	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err, "second BeginTransaction should succeed after rollback")

	system2 := database.System{
		DBID:     2,
		SystemID: "test-system-committed",
		Name:     "Test System Committed",
	}
	_, err = mediaDB.InsertSystem(system2)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err, "second CommitTransaction should succeed")

	// Verify only the second system exists
	_, err = mediaDB.FindSystemBySystemID("test-system-rollback")
	require.Error(t, err)

	foundSystem2, err := mediaDB.FindSystemBySystemID("test-system-committed")
	require.NoError(t, err)
	assert.Equal(t, "test-system-committed", foundSystem2.SystemID)
}

// TestMediaDB_ConcurrentTransactionStressTest simulates realistic concurrent
// access patterns where multiple operations attempt transactions while reads
// are ongoing.
func TestMediaDB_ConcurrentTransactionStressTest(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	const (
		numWriters = 5
		numReaders = 10
		duration   = 2 * time.Second
	)

	var (
		successfulTxns  atomic.Int32
		failedTxns      atomic.Int32
		successfulReads atomic.Int32
		wg              sync.WaitGroup
		done            = make(chan struct{})
	)

	// Start timer
	go func() {
		time.Sleep(duration)
		close(done)
	}()

	// Launch writers that compete for transactions
	for i := range numWriters {
		wg.Add(1)
		writerID := i
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					err := mediaDB.BeginTransaction(false)
					if err == nil {
						// Successfully got transaction
						system := database.System{
							DBID:     int64(100 + writerID),
							SystemID: "concurrent-test",
							Name:     "Concurrent Test System",
						}
						_, _ = mediaDB.InsertSystem(system)

						// Hold transaction briefly
						time.Sleep(10 * time.Millisecond)

						_ = mediaDB.RollbackTransaction()
						successfulTxns.Add(1)
					} else {
						failedTxns.Add(1)
					}
					// Small delay between attempts
					time.Sleep(5 * time.Millisecond)
				}
			}
		}()
	}

	// Launch readers that read concurrently
	for range numReaders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					// Reads should always work
					_, err := mediaDB.GetLastGenerated()
					if err == nil {
						successfulReads.Add(1)
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
		}()
	}

	wg.Wait()

	// Verify reasonable behavior
	t.Logf("Successful transactions: %d", successfulTxns.Load())
	t.Logf("Failed transaction attempts: %d", failedTxns.Load())
	t.Logf("Successful reads: %d", successfulReads.Load())

	assert.Positive(t, successfulTxns.Load(),
		"at least some transactions should succeed")
	assert.Positive(t, failedTxns.Load(),
		"concurrent attempts should be rejected")
	assert.Positive(t, successfulReads.Load(),
		"reads should succeed during contention")
}

// TestMediaDB_NoTransaction_RollbackSafe tests that calling RollbackTransaction
// when no transaction is active is safe and returns no error.
func TestMediaDB_NoTransaction_RollbackSafe(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Rollback with no active transaction should be safe
	err := mediaDB.RollbackTransaction()
	assert.NoError(t, err, "RollbackTransaction with no active transaction should not error")
}

// TestMediaDB_NoTransaction_CommitSafe tests that calling CommitTransaction
// when no transaction is active is safe and returns no error.
func TestMediaDB_NoTransaction_CommitSafe(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Commit with no active transaction should be safe
	err := mediaDB.CommitTransaction()
	assert.NoError(t, err, "CommitTransaction with no active transaction should not error")
}
