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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediaDB_QuickCheck_HealthyReturnsOK(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ok, err := mediaDB.QuickCheck()
	require.NoError(t, err)
	assert.True(t, ok, "freshly allocated database should pass quick_check")
}

func TestMediaDB_QuickCheck_CorruptFileFails(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Overwrite the on-disk file with non-database bytes, then reopen.
	path := mediaDB.GetDBPath()
	require.NoError(t, mediaDB.Close())
	mediaDB.sql.Store(nil)
	require.NoError(t, os.WriteFile(path, []byte("this is not a sqlite database file at all"), 0o600))
	require.NoError(t, mediaDB.Open())

	ok, _ := mediaDB.QuickCheck()
	assert.False(t, ok, "corrupt file must not pass quick_check")
}

func TestMediaDB_WALCheckpoint_NoOpDuringTransaction(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	require.NoError(t, mediaDB.BeginTransaction(false))
	defer func() { _ = mediaDB.RollbackTransaction() }()

	// Must not truncate the WAL out from under an open writer; returns nil and
	// leaves the transaction intact.
	require.NoError(t, mediaDB.WALCheckpoint())
	assert.NotNil(t, mediaDB.tx, "transaction should still be open after a no-op checkpoint")
}

func TestMediaDB_CorruptMarkerLifecycle(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	assert.False(t, mediaDB.IsMarkedCorrupt(), "no marker should exist initially")

	mediaDB.MarkCorrupt("test reason")
	assert.True(t, mediaDB.IsMarkedCorrupt())
	_, statErr := os.Stat(mediaDB.GetDBPath() + database.CorruptMarkerSuffix)
	require.NoError(t, statErr, "marker sidecar file should exist on disk")

	require.NoError(t, mediaDB.ClearCorruptMarker())
	assert.False(t, mediaDB.IsMarkedCorrupt())

	// Clearing an absent marker is a no-op.
	require.NoError(t, mediaDB.ClearCorruptMarker())
}

func TestMediaDB_IntegrityReport_HealthyReturnsOK(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	report := mediaDB.IntegrityReport()
	assert.Equal(t, []string{"ok"}, report)
}

func TestMediaDB_RecreateAfterCorruption_KeepBackup(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	insertSystemWithMedia(t, mediaDB, "NES", "Some Game", filepath.Join("roms", "nes", "game.nes"))
	mediaDB.MarkCorrupt("test")
	path := mediaDB.GetDBPath()

	require.NoError(t, mediaDB.RecreateAfterCorruption(true))

	// Forensic backup kept, marker cleared. (The reopened WAL database creates fresh
	// -wal/-shm sidecars; the point is the stale corrupt ones don't survive into it,
	// which the empty+queryable check below confirms.)
	_, backupErr := os.Stat(path + database.CorruptMarkerSuffix + ".bak")
	require.NoError(t, backupErr, "backup copy should be kept when keepBackup=true")
	assert.False(t, mediaDB.IsMarkedCorrupt(), "marker should be cleared after recreate")

	// Fresh schema: queryable and empty.
	var count int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM Media").Scan(&count))
	assert.Equal(t, 0, count, "recreated database should be empty")
}

// zeroHighPages reproduces the real-world corruption: a contiguous run of high-numbered
// (table-data) pages overwritten with 0x00, leaving the schema pages intact so the DB still
// opens but integrity checks fail — mirroring the supplied corrupt media.db.
func zeroHighPages(t *testing.T, db *MediaDB) {
	t.Helper()
	var pageSize, pageCount int
	require.NoError(t, db.sql.Load().QueryRowContext(context.Background(), "PRAGMA page_size").Scan(&pageSize))
	require.NoError(t, db.sql.Load().QueryRowContext(context.Background(), "PRAGMA page_count").Scan(&pageCount))
	require.Greater(t, pageCount, 10, "need a DB with enough data pages to corrupt")

	// Flush the WAL into the main file and remove sidecars so the zeroing is not undone by
	// WAL recovery on reopen.
	require.NoError(t, db.WALCheckpoint())
	path := db.GetDBPath()
	require.NoError(t, db.Close())
	db.sql.Store(nil)
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")

	f, err := os.OpenFile(path, os.O_WRONLY, 0o600) //nolint:gosec // test-controlled temp DB path
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	zero := make([]byte, pageSize)
	// Zero the top third of pages (table/index data, never the page-1 header/schema root).
	for p := (pageCount * 2 / 3); p <= pageCount; p++ {
		_, werr := f.WriteAt(zero, int64((p-1)*pageSize))
		require.NoError(t, werr)
	}
}

func TestMediaDB_ZeroedPages_DetectedAndRecovered(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Grow the DB so the top third holds real table data.
	for i := range 400 {
		name := fmt.Sprintf("Game %04d", i)
		insertSystemWithMedia(t, mediaDB, "NES", name, filepath.Join("roms", "nes", name+".nes"))
	}

	zeroHighPages(t, mediaDB)
	require.NoError(t, mediaDB.Open())

	ok, _ := mediaDB.QuickCheck()
	assert.False(t, ok, "zeroed data pages must fail quick_check")
	report := mediaDB.IntegrityReport()
	assert.NotEqual(t, []string{"ok"}, report, "integrity report should show the damage")

	// Recovery rebuilds to a clean, passing database.
	require.NoError(t, mediaDB.RecreateAfterCorruption(false))
	ok, err := mediaDB.QuickCheck()
	require.NoError(t, err)
	assert.True(t, ok, "database should pass quick_check after recovery")
}

func TestMediaDB_BrowseFiles_RoutesCorruptionToMarker(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// Corrupt the whole file so any query fails deterministically, isolating the test to
	// the question we care about: does a corruption error on the read path flag the DB?
	path := mediaDB.GetDBPath()
	require.NoError(t, mediaDB.Close())
	mediaDB.sql.Store(nil)
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")
	require.NoError(t, os.WriteFile(path, []byte("not a sqlite database — corrupted on purpose"), 0o600))
	require.NoError(t, mediaDB.Open())
	assert.True(t, mediaDB.IsMarkedCorrupt(), "cell_size_check should mark a malformed DB during open")

	_, err := mediaDB.BrowseFiles(context.Background(), &database.BrowseFilesOptions{})
	require.Error(t, err)
	assert.True(t, mediaDB.IsMarkedCorrupt(), "a malformed browse read must keep the DB marked corrupt")
}

func TestMediaDB_RecreateAfterCorruption_DeleteWhenNoBackup(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	insertSystemWithMedia(t, mediaDB, "NES", "Some Game", filepath.Join("roms", "nes", "game.nes"))
	mediaDB.MarkCorrupt("test")
	path := mediaDB.GetDBPath()

	require.NoError(t, mediaDB.RecreateAfterCorruption(false))

	_, backupErr := os.Stat(path + database.CorruptMarkerSuffix + ".bak")
	assert.True(t, os.IsNotExist(backupErr), "no backup should be kept when keepBackup=false")
	assert.False(t, mediaDB.IsMarkedCorrupt())

	var count int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM Media").Scan(&count))
	assert.Equal(t, 0, count)
}
