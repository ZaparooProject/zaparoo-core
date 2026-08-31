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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SQLite temp_store values as reported by `PRAGMA temp_store`.
const (
	tempStoreFile   = 1
	tempStoreMemory = 2
)

func openIndexingCacheTestDB(ctx context.Context, t *testing.T) *MediaDB {
	t.Helper()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: t.TempDir()})
	mediaDB, err := OpenMediaDB(ctx, mockPlatform)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mediaDB.Close() })
	return mediaDB
}

func readPragmas(
	ctx context.Context,
	t *testing.T,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
) (cacheSize, tempStore int) {
	t.Helper()
	require.NoError(t, queryer.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&cacheSize))
	require.NoError(t, queryer.QueryRowContext(ctx, "PRAGMA temp_store").Scan(&tempStore))
	return cacheSize, tempStore
}

func requirePoolWait(t *testing.T, sqlDB *sql.DB, failureMessage string) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for sqlDB.Stats().WaitCount == 0 {
		select {
		case <-deadline.C:
			t.Fatal(failureMessage)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// TestBeginTransactionAppliesIndexingCacheBoost forces pool-wide pragma setup
// onto a different connection from the writer. The writer retains a temp table,
// reproducing SQLite's rejection when temp_store is changed after BeginTx.
func TestBeginTransactionAppliesIndexingCacheBoost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB := openIndexingCacheTestDB(ctx, t)
	sqlDB := mediaDB.sql.Load()
	sqlDB.SetMaxOpenConns(2)

	require.NoError(t, mediaDB.BeginTransaction(false))
	staleWriter := mediaDB.txConn
	_, err := mediaDB.tx.ExecContext(ctx, "CREATE TEMP TABLE stale_writer_temp (id INTEGER)")
	require.NoError(t, err)
	require.NoError(t, staleWriter.Raw(func(driverConn any) error {
		sqliteConn, ok := driverConn.(*sqlite3.SQLiteConn)
		require.True(t, ok)
		return sqliteConn.RegisterFunc("stale_writer_marker", func() int { return 1 }, true)
	}))

	// Active writer stays at defaults while pool-wide setup configures other slot.
	mediaDB.SetIndexingCacheSize(true)
	cacheSize, tempStore := readPragmas(ctx, t, mediaDB.tx)
	require.Equal(t, -8192, cacheSize)
	require.Equal(t, tempStoreFile, tempStore)

	heldBoosted, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = heldBoosted.Close() })
	cacheSize, tempStore = readPragmas(ctx, t, heldBoosted)
	require.Equal(t, -32768, cacheSize)
	require.Equal(t, tempStoreMemory, tempStore)

	require.NoError(t, mediaDB.RollbackTransaction())
	require.NoError(t, mediaDB.BeginTransaction(false))
	require.NotNil(t, mediaDB.txConn)
	require.NoError(t, heldBoosted.Close())

	// Changing temp_store clears the temp schema, so use a connection-local
	// function to prove BeginTransaction selected the stale physical connection.
	var staleWriterMarker int
	require.NoError(t, mediaDB.tx.QueryRowContext(ctx,
		"SELECT stale_writer_marker()").Scan(&staleWriterMarker))
	require.Equal(t, 1, staleWriterMarker)
	cacheSize, tempStore = readPragmas(ctx, t, mediaDB.tx)
	assert.Equal(t, -32768, cacheSize)
	assert.Equal(t, tempStoreMemory, tempStore)
	require.NoError(t, mediaDB.RollbackTransaction())
	assert.Nil(t, mediaDB.tx)
	assert.Nil(t, mediaDB.txConn)

	mediaDB.SetIndexingCacheSize(false)
	pooled := make([]*sql.Conn, 0, 2)
	for range 2 {
		conn, connErr := sqlDB.Conn(ctx)
		require.NoError(t, connErr)
		pooled = append(pooled, conn)
	}
	for _, conn := range pooled {
		cacheSize, tempStore = readPragmas(ctx, t, conn)
		assert.Equal(t, -8192, cacheSize)
		assert.Equal(t, tempStoreFile, tempStore)
		require.NoError(t, conn.Close())
	}
}

func TestIndexingCacheBoostAppliesToEveryPooledConnection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB := openIndexingCacheTestDB(ctx, t)
	sqlDB := mediaDB.sql.Load()
	sqlDB.SetMaxOpenConns(2)

	mediaDB.SetIndexingCacheSize(true)
	pooled := make([]*sql.Conn, 0, 2)
	for range 2 {
		conn, err := sqlDB.Conn(ctx)
		require.NoError(t, err)
		pooled = append(pooled, conn)
	}
	for _, conn := range pooled {
		cacheSize, tempStore := readPragmas(ctx, t, conn)
		assert.Equal(t, -32768, cacheSize)
		assert.Equal(t, tempStoreMemory, tempStore)
		require.NoError(t, conn.Close())
	}
}

func TestWALAutoCheckpointAppliesToPoolAndWriter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB := openIndexingCacheTestDB(ctx, t)
	sqlDB := mediaDB.sql.Load()
	sqlDB.SetMaxOpenConns(2)

	assertPooledValues := func(want int) {
		t.Helper()
		conns := make([]*sql.Conn, 0, 2)
		for range 2 {
			conn, err := sqlDB.Conn(ctx)
			require.NoError(t, err)
			conns = append(conns, conn)
		}
		for _, conn := range conns {
			var got int
			require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA wal_autocheckpoint").Scan(&got))
			assert.Equal(t, want, got)
			require.NoError(t, conn.Close())
		}
	}

	mediaDB.SetWALAutoCheckpoint(128)
	assertPooledValues(128)

	require.NoError(t, mediaDB.BeginTransaction(false))
	var writerValue int
	require.NoError(t, mediaDB.tx.QueryRowContext(ctx, "PRAGMA wal_autocheckpoint").Scan(&writerValue))
	assert.Equal(t, 128, writerValue)
	require.NoError(t, mediaDB.RollbackTransaction())

	mediaDB.SetWALAutoCheckpoint(defaultWALAutoCheckpoint)
	assertPooledValues(defaultWALAutoCheckpoint)
}

// TestWALAutoCheckpointZeroDisablesRatherThanFallingBackToDefault covers the
// bug fixed alongside #1279's indexing-checkpoint changes: pages=0 (disable
// automatic checkpointing entirely, what indexing now requests) must persist
// as 0, not be treated as "never configured" and silently fall back to
// SQLite's default of 1000 — which is exactly what walAutoCheckpointPages did
// before walAutoCheckpointSet was added, since it used pages<=0 to mean unset.
func TestWALAutoCheckpointZeroDisablesRatherThanFallingBackToDefault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB := openIndexingCacheTestDB(ctx, t)
	sqlDB := mediaDB.sql.Load()
	sqlDB.SetMaxOpenConns(2)

	// Before any explicit call, a fresh MediaDB reports SQLite's own default.
	assert.Equal(t, defaultWALAutoCheckpoint, mediaDB.walAutoCheckpointPages())

	mediaDB.SetWALAutoCheckpoint(0)
	assert.Equal(t, 0, mediaDB.walAutoCheckpointPages(), "explicit 0 must not fall back to the default")

	conn, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	var pooledValue int
	require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA wal_autocheckpoint").Scan(&pooledValue))
	assert.Equal(t, 0, pooledValue)
	require.NoError(t, conn.Close())

	require.NoError(t, mediaDB.BeginTransaction(false))
	var writerValue int
	require.NoError(t, mediaDB.tx.QueryRowContext(ctx, "PRAGMA wal_autocheckpoint").Scan(&writerValue))
	assert.Equal(t, 0, writerValue, "a writer connection opened after disabling must also see 0, not the default")
	require.NoError(t, mediaDB.RollbackTransaction())
}

func TestIndexingPragmaRestoreWithUnlimitedPool(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB := openIndexingCacheTestDB(ctx, t)
	sqlDB := mediaDB.sql.Load()
	sqlDB.SetMaxOpenConns(1)
	mediaDB.SetIndexingCacheSize(true)
	sqlDB.SetMaxOpenConns(0)

	mediaDB.SetIndexingCacheSize(false)
	conn, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	cacheSize, tempStore := readPragmas(ctx, t, conn)
	assert.Equal(t, -8192, cacheSize)
	assert.Equal(t, tempStoreFile, tempStore)
	require.NoError(t, conn.Close())
}

func TestWriterConnectionRetainsBoostUntilIndexingEnds(t *testing.T) {
	t.Parallel()
	for _, finish := range []string{"commit", "rollback"} {
		t.Run(finish, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			mediaDB := openIndexingCacheTestDB(ctx, t)
			sqlDB := mediaDB.sql.Load()
			sqlDB.SetMaxOpenConns(1)
			mediaDB.SetIndexingCacheSize(true)

			require.NoError(t, mediaDB.BeginTransaction(false))
			cacheSize, tempStore := readPragmas(ctx, t, mediaDB.tx)
			require.Equal(t, -32768, cacheSize)
			require.Equal(t, tempStoreMemory, tempStore)
			if finish == "commit" {
				require.NoError(t, mediaDB.CommitTransaction())
			} else {
				require.NoError(t, mediaDB.RollbackTransaction())
			}

			conn, err := sqlDB.Conn(ctx)
			require.NoError(t, err)
			cacheSize, tempStore = readPragmas(ctx, t, conn)
			assert.Equal(t, -32768, cacheSize)
			assert.Equal(t, tempStoreMemory, tempStore)
			require.NoError(t, conn.Close())

			mediaDB.SetIndexingCacheSize(false)
			conn, err = sqlDB.Conn(ctx)
			require.NoError(t, err)
			cacheSize, tempStore = readPragmas(ctx, t, conn)
			assert.Equal(t, -8192, cacheSize)
			assert.Equal(t, tempStoreFile, tempStore)
			require.NoError(t, conn.Close())
		})
	}
}

func TestBeginTransactionReportsPragmaFailureAndDiscardsConnection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB := openIndexingCacheTestDB(ctx, t)
	sqlDB := mediaDB.sql.Load()
	sqlDB.SetMaxOpenConns(1)

	conn, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	require.NoError(t, conn.Raw(func(driverConn any) error {
		sqliteConn, ok := driverConn.(*sqlite3.SQLiteConn)
		require.True(t, ok)
		sqliteConn.RegisterAuthorizer(func(op int, pragmaName, _, _ string) int {
			if op == sqlite3.SQLITE_PRAGMA && strings.EqualFold(pragmaName, "temp_store") {
				return sqlite3.SQLITE_DENY
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	}))
	require.NoError(t, conn.Close())

	err = mediaDB.BeginTransaction(false)
	require.ErrorContains(t, err, "failed to set writer connection temp_store")
	require.NotErrorIs(t, err, sql.ErrConnDone)
	assert.Nil(t, mediaDB.tx)
	assert.Nil(t, mediaDB.txConn)
	assert.Zero(t, sqlDB.Stats().InUse)

	// Failed restoration discards the denied connection, so a fresh connection works.
	require.NoError(t, mediaDB.BeginTransaction(false))
	require.NoError(t, mediaDB.RollbackTransaction())
}

func TestBeginTransactionReleasesConnectionOnBeginFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB := openIndexingCacheTestDB(ctx, t)
	sqlDB := mediaDB.sql.Load()
	sqlDB.SetMaxOpenConns(1)

	conn, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	require.NoError(t, conn.Raw(func(driverConn any) error {
		sqliteConn, ok := driverConn.(*sqlite3.SQLiteConn)
		require.True(t, ok)
		sqliteConn.RegisterAuthorizer(func(op int, _, _, _ string) int {
			if op == sqlite3.SQLITE_TRANSACTION {
				return sqlite3.SQLITE_DENY
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	}))
	require.NoError(t, conn.Close())

	err = mediaDB.BeginTransaction(false)
	require.ErrorContains(t, err, "failed to begin transaction")
	assert.Nil(t, mediaDB.tx)
	assert.Nil(t, mediaDB.txConn)
	assert.Zero(t, sqlDB.Stats().InUse)
}

func TestBeginTransactionReleasesConnectionOnSetupFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB := openIndexingCacheTestDB(ctx, t)
	sqlDB := mediaDB.sql.Load()
	sqlDB.SetMaxOpenConns(1)

	_, err := sqlDB.ExecContext(ctx, "DROP TABLE MediaTags")
	require.NoError(t, err)
	err = mediaDB.BeginTransaction(false)
	require.ErrorContains(t, err, "failed to prepare insert media tag statement")
	assert.Nil(t, mediaDB.tx)
	assert.Nil(t, mediaDB.txConn)
	assert.False(t, mediaDB.inTransaction)
	assert.Zero(t, sqlDB.Stats().InUse)
}

func TestBeginTransactionPoolWaitHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	mediaDB := openIndexingCacheTestDB(ctx, t)
	sqlDB := mediaDB.sql.Load()
	sqlDB.SetMaxOpenConns(1)

	held, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { result <- mediaDB.BeginTransaction(false) }()

	requirePoolWait(t, sqlDB, "BeginTransaction did not wait for exhausted pool")
	cancel()
	err = <-result
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "failed to acquire writer connection")
	assert.Nil(t, mediaDB.tx)
	assert.Nil(t, mediaDB.txConn)
	require.NoError(t, held.Close())
}

func TestIndexingPragmaRestorePoolWaitHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	mediaDB := openIndexingCacheTestDB(ctx, t)
	sqlDB := mediaDB.sql.Load()
	sqlDB.SetMaxOpenConns(1)
	mediaDB.SetIndexingCacheSize(true)

	held, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		mediaDB.SetIndexingCacheSize(false)
		close(done)
	}()

	requirePoolWait(t, sqlDB, "pragma restoration did not wait for exhausted pool")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pragma restoration did not stop after context cancellation")
	}
	require.NoError(t, held.Close())
	assert.Zero(t, sqlDB.Stats().InUse)
}

func TestCloseRollsBackAndReleasesWriterConnection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB := openIndexingCacheTestDB(ctx, t)
	require.NoError(t, mediaDB.BeginTransaction(false))
	require.NotNil(t, mediaDB.txConn)

	require.NoError(t, mediaDB.Close())
	assert.Nil(t, mediaDB.tx)
	assert.Nil(t, mediaDB.txConn)
	assert.False(t, mediaDB.inTransaction)
}

// TestOptimizationBoostAppliesWhileAnotherCallerHoldsAConnection reproduces the
// round 8 failure of #1279.
//
// Post-index optimization begins seconds after indexing ends, while the app is
// still polling. At baseMaxOpenConns (2) that leaves one free slot, and
// drainPooledConns wants every slot, so the drain timed out and the entire
// optimization ran at the 8MB default — visible only as dbCacheSize on the step
// metrics long afterwards.
//
// ensureIndexingCacheBoostApplied is what makes this survivable: it reads the
// pragma back through the pool — the same way the optimization steps get their
// connection — and retries when the drain did not reach it.
func TestOptimizationBoostAppliesWhileAnotherCallerHoldsAConnection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB := openIndexingCacheTestDB(ctx, t)
	sqlDB := mediaDB.sql.Load()

	// Stand in for the app's polling: hold a connection across the boost, then
	// release it the way a short query would, so the retry has something to work
	// with. Holding it forever would test a situation that does not occur.
	held, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	releaseOnce := sync.OnceFunc(func() { _ = held.Close() })
	defer releaseOnce()
	go func() {
		time.Sleep(connectionAcquireTimeout / 2)
		releaseOnce()
	}()

	// The order RunBackgroundOptimizationWithLease uses: cap first, so the drain
	// covers the slot the boost adds rather than leaving it to be opened later
	// from the DSN at the default cache size.
	mediaDB.SetIndexingConnBoost(true)
	mediaDB.SetIndexingCacheSize(true)
	mediaDB.ensureIndexingCacheBoostApplied()
	t.Cleanup(func() {
		mediaDB.SetIndexingConnBoost(false)
		mediaDB.SetIndexingCacheSize(false)
	})

	// Read the pragma back the way the optimization steps do — through the pool,
	// on whichever connection it hands out.
	cacheSize, tempStore := readPragmas(ctx, t, sqlDB)
	assert.Equal(t, -32768, cacheSize,
		"optimization must run at the boosted cache size even when another caller "+
			"held a pooled connection; round 8 silently ran the whole phase at -8192")
	assert.Equal(t, tempStoreMemory, tempStore,
		"temp_store must reach the pooled connection alongside cache_size")
}

// TestOptimizationBoostVerificationDetectsMissingPragma covers the check itself:
// it must notice an unboosted pool rather than trusting that the drain worked.
func TestOptimizationBoostVerificationDetectsMissingPragma(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB := openIndexingCacheTestDB(ctx, t)
	sqlDB := mediaDB.sql.Load()

	// Boost state is on, but no pragma has been pushed to any connection.
	mediaDB.indexingCacheBoost.Store(true)
	t.Cleanup(func() { mediaDB.SetIndexingCacheSize(false) })

	wantCacheSize, _ := mediaDB.connPragmaValues()
	require.Equal(t, "-32768", wantCacheSize)
	assert.False(t, mediaDB.pooledCacheSizeMatches(sqlDB, wantCacheSize),
		"verification must report a pool that never received the pragma; "+
			"silently returning true here is what made the round 8 failure invisible")

	// And the repair path must fix exactly that.
	mediaDB.ensureIndexingCacheBoostApplied()
	cacheSize, _ := readPragmas(ctx, t, sqlDB)
	assert.Equal(t, -32768, cacheSize, "the retry must apply the pragma it found missing")
}

// TestConnectionOpenedAfterBoostCarriesBoostedPragmas covers the second, silent
// half of the same bug: the pragmas reaching every connection that exists is not
// enough if the boost then permits another one to be opened.
//
// Applying the cache size before raising the connection cap sized the drain
// against the narrow cap, so the extra slot was later filled straight from the
// DSN at -8192 with nothing left to configure it. That connection then served
// optimization steps and their dbCacheSize metric, which is what the device logs
// reported for three rounds while the drain itself had succeeded.
func TestConnectionOpenedAfterBoostCarriesBoostedPragmas(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB := openIndexingCacheTestDB(ctx, t)
	sqlDB := mediaDB.sql.Load()

	// The order RunBackgroundOptimizationWithLease uses.
	mediaDB.SetIndexingConnBoost(true)
	mediaDB.SetIndexingCacheSize(true)
	t.Cleanup(func() {
		mediaDB.SetIndexingConnBoost(false)
		mediaDB.SetIndexingCacheSize(false)
	})

	// Hold every slot at once so each one has to be a distinct physical
	// connection, including any the boost newly permitted.
	held := make([]*sql.Conn, 0, indexingMaxOpenConns)
	for range indexingMaxOpenConns {
		conn, err := sqlDB.Conn(ctx)
		require.NoError(t, err)
		held = append(held, conn)
	}
	t.Cleanup(func() {
		for _, conn := range held {
			_ = conn.Close()
		}
	})

	for i, conn := range held {
		var cacheSize int
		require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&cacheSize))
		assert.Equal(t, -32768, cacheSize,
			"pooled connection %d must carry the boosted cache size; a connection opened "+
				"after the boost is born from the DSN at -8192 unless the cap was raised "+
				"before the pragmas were applied (see #1279)", i)
	}
}

// TestPooledCacheSizeMatchesRejectsPartiallyBoostedPool pins the verification
// contract: a partially-boosted pool must not verify as applied.
//
// The previous implementation was a single pool query, which returns an
// arbitrary connection. On the mixed pool below it answered correctly or
// incorrectly depending on which connection the pool happened to hand out — a
// coin flip, not a check. That nondeterminism is the bug: round 9 logged the
// boost as applied and every optimization step then reported -8192. Draining
// the pool makes the answer deterministic, which is what this pins.
func TestPooledCacheSizeMatchesRejectsPartiallyBoostedPool(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB := openIndexingCacheTestDB(ctx, t)
	sqlDB := mediaDB.sql.Load()

	// Boost the flag but deliberately configure only ONE connection, leaving the
	// rest of the pool at the DSN default.
	mediaDB.indexingCacheBoost.Store(true)
	t.Cleanup(func() { mediaDB.indexingCacheBoost.Store(false) })

	conn, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, "PRAGMA cache_size = -32768")
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	// Force a second physical connection to exist so the pool is genuinely mixed.
	first, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	second, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	require.NoError(t, first.Close())
	require.NoError(t, second.Close())

	// Repeat: a single-sample check would only be caught on the runs where it
	// happened to pick the unboosted connection.
	for range 8 {
		assert.False(t, mediaDB.pooledCacheSizeMatches(sqlDB, "-32768"),
			"a pool where only some connections are boosted must NOT verify as applied; "+
				"a check that answers by luck is worse than no check")
	}
}
