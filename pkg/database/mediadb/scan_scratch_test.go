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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openScanScratchTestDB(t *testing.T) *MediaDB {
	t.Helper()
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test-platform")
	db := &MediaDB{
		pl: pl, ctx: context.Background(), batchSize: 100,
		dbPath: filepath.Join(t.TempDir(), "media.db"),
	}
	require.NoError(t, db.Open())
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func stagedPaths(t *testing.T, db *MediaDB) int {
	t.Helper()
	var count int
	require.NoError(t, db.conn().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM ScanStage").Scan(&count))
	return count
}

// Staged rows have to survive the commits that split a large system into
// batches, and they must never land in the main database file.
func TestScanScratch_StagedRowsStayOutOfMainAcrossTransactions(t *testing.T) {
	t.Parallel()
	db := openScanScratchTestDB(t)
	ctx := context.Background()

	require.NoError(t, db.BeginTransaction(true))
	require.NoError(t, db.StageScannedMedia(&database.ScanStagedMedia{
		Path: "/games/nes/a.nes", ParentDir: "/games/nes/", Slug: "a", TitleName: "A", SortName: "A",
		Tags: []database.ScanStagedTag{{Type: "region", Value: "us"}},
	}))
	require.NoError(t, db.CommitTransaction())

	var inMain int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'ScanStage%' OR name = 'ScanTouchedTitles'").Scan(&inMain))
	assert.Zero(t, inMain, "staging tables must not exist in the main database")
	_, err := os.Stat(db.scanScratchPath())
	require.NoError(t, err, "the scratch database file must exist while the database is open")

	// A later batch of the same system may run on a different pooled
	// connection, and must see what the earlier one staged.
	require.NoError(t, db.BeginTransaction(true))
	assert.Equal(t, 1, stagedPaths(t, db))
	require.NoError(t, db.StageScannedMedia(&database.ScanStagedMedia{
		Path: "/games/nes/b.nes", ParentDir: "/games/nes/", Slug: "b", TitleName: "B", SortName: "B",
	}))
	require.NoError(t, db.FlushBatchInserters())
	assert.Equal(t, 2, stagedPaths(t, db))

	// A rollback undoes staged rows too, even though the scratch database
	// keeps no journal on disk.
	require.NoError(t, db.RollbackTransaction())
	require.NoError(t, db.BeginTransaction(true))
	assert.Equal(t, 1, stagedPaths(t, db))
	require.NoError(t, db.RollbackTransaction())

	// Clearing outside a transaction still reaches the scratch database.
	require.NoError(t, db.ClearScanStage())
	require.NoError(t, db.BeginTransaction(true))
	assert.Zero(t, stagedPaths(t, db))
	require.NoError(t, db.RollbackTransaction())
}

func TestScanScratch_StaleFileIsRemovedOnOpenAndClose(t *testing.T) {
	t.Parallel()
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test-platform")
	path := filepath.Join(t.TempDir(), "media.db")
	db := &MediaDB{pl: pl, ctx: context.Background(), batchSize: 100, dbPath: path}
	require.NoError(t, db.Open())
	require.NoError(t, db.BeginTransaction(true))
	require.NoError(t, db.CommitTransaction())
	require.FileExists(t, db.scanScratchPath())
	require.NoError(t, db.Close())
	require.NoFileExists(t, db.scanScratchPath())

	// A crash leaves the file behind with whatever it held.
	require.NoError(t, os.WriteFile(db.scanScratchPath(), []byte("left by a crash"), 0o600))
	reopened := &MediaDB{pl: pl, ctx: context.Background(), batchSize: 100, dbPath: path}
	require.NoError(t, reopened.Open())
	t.Cleanup(func() { _ = reopened.Close() })
	require.NoError(t, reopened.BeginTransaction(true))
	assert.Zero(t, stagedPaths(t, reopened))
	require.NoError(t, reopened.RollbackTransaction())
}

func TestScanScratch_DatabaseWithoutAFileStagesInMain(t *testing.T) {
	t.Parallel()
	db := &MediaDB{}
	assert.Empty(t, db.scanScratchPath())
	db.dbPath = ":memory:"
	assert.Empty(t, db.scanScratchPath())
}

// The scratch file grows to hold a whole system's staged rows. Once indexing
// ends it has to go, rather than sit at full size, empty, until Core restarts.
func TestScanScratch_RemovedWhenIndexingEnds(t *testing.T) {
	t.Parallel()
	db := openScanScratchTestDB(t)

	db.SetIndexingCacheSize(true)
	require.NoError(t, db.BeginTransaction(true))
	for i := range 20000 {
		name := fmt.Sprintf("Game %05d With A Reasonably Long Title For Padding", i)
		require.NoError(t, db.StageScannedMedia(&database.ScanStagedMedia{
			Path:      fmt.Sprintf("/media/fat/games/NES/Collection/%s (USA).nes", name),
			ParentDir: "/media/fat/games/NES/Collection/",
			Slug:      fmt.Sprintf("game%05d", i), TitleName: name, SortName: name,
			Tags: []database.ScanStagedTag{{Type: "region", Value: "us"}},
		}))
	}
	require.NoError(t, db.CommitTransaction())
	require.NoError(t, db.ClearScanStage())

	full, err := os.Stat(db.scanScratchPath())
	require.NoError(t, err)
	require.Greater(t, full.Size(), int64(1<<20), "staging must have grown the scratch file")

	db.SetIndexingCacheSize(false)
	require.NoFileExists(t, db.scanScratchPath())

	// No connection may still hold the deleted file: one that did would keep
	// staging into it while the others used a new one.
	sqlDB := db.sql.Load()
	db.sqlMu.Lock()
	conns, err := db.drainPooledConns(sqlDB)
	db.sqlMu.Unlock()
	require.NoError(t, err)
	require.NotEmpty(t, conns)
	for _, conn := range conns {
		var attached int
		require.NoError(t, conn.QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM pragma_database_list WHERE name = ?", scanScratchSchema).Scan(&attached))
		assert.Zero(t, attached, "a pooled connection still has the deleted scratch file attached")
	}
	for _, conn := range conns {
		require.NoError(t, conn.Close())
	}

	// Connections that had it attached start the next run on a new file, and
	// all of them see the same one: no connection may still hold the deleted
	// file, or rows staged by one batch would be missing from the next.
	require.NoError(t, db.ClearScanStage())
	require.NoError(t, db.BeginTransaction(true))
	require.NoError(t, db.StageScannedMedia(&database.ScanStagedMedia{
		Path: "/games/nes/a.nes", ParentDir: "/games/nes/", Slug: "a", TitleName: "A", SortName: "A",
	}))
	require.NoError(t, db.CommitTransaction())
	require.FileExists(t, db.scanScratchPath())
	for range 4 {
		require.NoError(t, db.BeginTransaction(true))
		assert.Equal(t, 1, stagedPaths(t, db))
		require.NoError(t, db.CommitTransaction())
	}
}
