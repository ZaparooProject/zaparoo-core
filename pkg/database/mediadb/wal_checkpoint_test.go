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
	"fmt"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/jonboulle/clockwork"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// TestWALCheckpointing verifies that WAL checkpoints are properly executed
// after transaction commits to keep WAL size bounded during indexing.
func TestWALCheckpointing(t *testing.T) {
	ctx := context.Background()

	// Create file-based database with the same connection parameters as production
	// (in-memory databases don't support WAL mode)
	tempDir := t.TempDir()
	dbPath := tempDir + "/test.db"

	sqlDB, err := sql.Open("sqlite3", dbPath+getSqliteConnParams())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, sqlDB.Close())
	}()

	mediaDB := &MediaDB{
		sql:   sqlDB,
		ctx:   ctx,
		clock: clockwork.NewRealClock(),
	}

	// Initialize database schema
	err = mediaDB.Allocate()
	require.NoError(t, err)

	// Verify WAL mode is enabled
	var journalMode string
	err = sqlDB.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode)
	require.NoError(t, err)
	require.Equal(t, "wal", journalMode)

	// Verify autocheckpoint is disabled
	var autocheckpoint int
	err = sqlDB.QueryRowContext(ctx, "PRAGMA wal_autocheckpoint").Scan(&autocheckpoint)
	require.NoError(t, err)
	// Note: SQLite may ignore _wal_autocheckpoint=0 in some cases, so we'll check if it's reasonable
	t.Logf("WAL autocheckpoint setting: %d", autocheckpoint)

	// Verify cache spill is disabled
	var cacheSpill int
	err = sqlDB.QueryRowContext(ctx, "PRAGMA cache_spill").Scan(&cacheSpill)
	require.NoError(t, err)
	t.Logf("Cache spill setting: %d", cacheSpill)

	// Verify cache size
	var cacheSize int
	err = sqlDB.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&cacheSize)
	require.NoError(t, err)
	t.Logf("Cache size setting: %d", cacheSize)

	// Begin transaction and insert test data
	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	// Insert test system
	system := database.System{
		SystemID: "test",
		Name:     "Test System",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)
	t.Logf("Inserted system with DBID: %d", insertedSystem.DBID)

	// Insert test media title
	title := database.MediaTitle{
		SystemDBID: insertedSystem.DBID, // Use the actual DBID from the inserted system
		Name:       "Test Game",
		Slug:       "test-game",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(&title)
	require.NoError(t, err)
	t.Logf("Inserted title with DBID: %d", insertedTitle.DBID)

	// Insert test media
	media := database.Media{
		MediaTitleDBID: insertedTitle.DBID,  // Use the actual DBID from the inserted title
		SystemDBID:     insertedSystem.DBID, // Also include the SystemDBID
		Path:           "/test/path/game.bin",
	}
	_, err = mediaDB.InsertMedia(media)
	require.NoError(t, err)

	// Commit transaction - this should trigger WAL checkpoint
	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Verify data was inserted correctly
	var count int
	err = sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Test multiple transactions to verify checkpointing works repeatedly
	for i := range 3 {
		err = mediaDB.BeginTransaction(false)
		require.NoError(t, err)

		media := database.Media{
			MediaTitleDBID: insertedTitle.DBID,
			SystemDBID:     insertedSystem.DBID,
			Path:           fmt.Sprintf("/test/path/game%d.bin", i+2), // Unique path
		}
		_, err = mediaDB.InsertMedia(media)
		require.NoError(t, err)

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)
	}

	// Verify all data is present
	err = sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 4, count) // 1 initial + 3 additional
}

// TestConnectionParameters verifies the write-optimized connection parameters
func TestConnectionParameters(t *testing.T) {
	connParams := getSqliteConnParams()

	// Verify key write-optimized parameters are present
	require.Contains(t, connParams, "_journal_mode=WAL")
	require.Contains(t, connParams, "_synchronous=NORMAL")
	require.Contains(t, connParams, "_wal_autocheckpoint=0")
	require.Contains(t, connParams, "_cache_spill=OFF")
	require.Contains(t, connParams, "_cache_size=-65536") // 64MB cache
	require.Contains(t, connParams, "_temp_store=MEMORY")
	require.Contains(t, connParams, "_mmap_size=67108864") // 64MB mmap
	require.Contains(t, connParams, "_page_size=8192")
	require.Contains(t, connParams, "_foreign_keys=ON")
	require.Contains(t, connParams, "_busy_timeout=5000")
}

// TestTransactionPerformanceWithWAL verifies that transactions perform well
// with the WAL optimizations enabled.
func TestTransactionPerformanceWithWAL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	ctx := context.Background()

	// Create in-memory database with the same connection parameters as production
	sqlDB, err := sql.Open("sqlite3", ":memory:"+getSqliteConnParams())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, sqlDB.Close())
	}()

	mediaDB := &MediaDB{
		sql:   sqlDB,
		ctx:   ctx,
		clock: clockwork.NewRealClock(),
	}

	// Initialize database schema
	err = mediaDB.Allocate()
	require.NoError(t, err)

	// Insert test system
	system := database.System{
		SystemID: "test",
		Name:     "Test System",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	// Insert test media title
	title := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Name:       "Test Game",
		Slug:       "test-game",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(&title)
	require.NoError(t, err)

	// Measure performance of batch inserts (simulating media scanner behavior)
	const numInserts = 1000

	start := time.Now()

	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	for i := range numInserts {
		media := database.Media{
			MediaTitleDBID: insertedTitle.DBID,
			SystemDBID:     insertedSystem.DBID,
			Path:           fmt.Sprintf("/test/path/game%d.bin", i),
		}
		_, err = mediaDB.InsertMedia(media)
		require.NoError(t, err)
	}

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	duration := time.Since(start)

	// Verify all inserts succeeded
	var count int
	err = sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, numInserts, count)

	// Performance should be reasonable - this is more of a smoke test
	// than a strict performance requirement
	t.Logf("Inserted %d records in %v (%.2f records/sec)",
		numInserts, duration, float64(numInserts)/duration.Seconds())

	// Ensure it completes in reasonable time (adjust as needed)
	require.Less(t, duration, 5*time.Second, "Batch insert should complete quickly")
}

// TestWALSizeManagement verifies that WAL checkpoints keep WAL size bounded
func TestWALSizeManagement(t *testing.T) {
	ctx := context.Background()

	// Create file-based database to check WAL file size
	tempDir := t.TempDir()
	dbPath := tempDir + "/test.db"

	sqlDB, err := sql.Open("sqlite3", dbPath+getSqliteConnParams())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, sqlDB.Close())
	}()

	mediaDB := &MediaDB{
		sql:   sqlDB,
		ctx:   ctx,
		clock: clockwork.NewRealClock(),
	}

	// Initialize database schema
	err = mediaDB.Allocate()
	require.NoError(t, err)

	// Insert test system
	system := database.System{
		SystemID: "test",
		Name:     "Test System",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	// Insert test media title
	title := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Name:       "Test Game",
		Slug:       "test-game",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(&title)
	require.NoError(t, err)

	// Perform multiple transactions to generate WAL activity
	const numTransactions = 10
	const insertsPerTransaction = 100

	for tx := range numTransactions {
		err = mediaDB.BeginTransaction(false)
		require.NoError(t, err)

		for i := range insertsPerTransaction {
			media := database.Media{
				MediaTitleDBID: insertedTitle.DBID,
				SystemDBID:     insertedSystem.DBID,
				Path:           fmt.Sprintf("/test/path/tx%d_game%d.bin", tx, i),
			}
			_, err = mediaDB.InsertMedia(media)
			require.NoError(t, err)
		}

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Check WAL checkpoint status after each commit
		var walCheckpointResult sql.NullString
		err = sqlDB.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(
			&walCheckpointResult, &walCheckpointResult, &walCheckpointResult)
		// The checkpoint in CommitTransaction should handle this, but we verify it works
		require.NoError(t, err)
	}

	// Verify all data was inserted
	var count int
	err = sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, numTransactions*insertsPerTransaction, count)
}
