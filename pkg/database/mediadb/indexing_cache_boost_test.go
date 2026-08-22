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
