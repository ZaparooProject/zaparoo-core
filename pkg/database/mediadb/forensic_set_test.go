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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMediaDB_Recreate_KeepBackup_PreservesConsistentForensicSet pins what a
// corruption post-mortem needs from Recreate: the database, its WAL and its
// SHM kept together, untouched, and reassemblable into a database that opens
// with every committed row — including rows that only ever reached the WAL.
func TestMediaDB_Recreate_KeepBackup_PreservesConsistentForensicSet(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	insertSystemWithMedia(t, mediaDB, "NES", "Some Game", filepath.Join("roms", "nes", "game.nes"))
	path := mediaDB.GetDBPath()
	require.FileExists(t, path+"-wal", "the row must still be in the WAL for the set to matter")

	// The set only matters if the row is genuinely WAL-only, so prove the main
	// database alone cannot answer for it. This has to happen before Recreate:
	// Recreate closes the database first, and that close can checkpoint the row
	// into the main file.
	mainOnly := filepath.Join(t.TempDir(), "main-only.db")
	copyFileIfExists(t, path, mainOnly)
	var mainOnlyMedia int
	mainOnlyErr := openSnapshot(t, mainOnly).
		QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&mainOnlyMedia)
	if mainOnlyErr == nil {
		assert.Zero(t, mainOnlyMedia, "the row must not have been checkpointed into the main database")
	} else {
		// Nothing has been checkpointed at all, so even the schema is missing.
		require.ErrorContains(t, mainOnlyErr, "no such table")
	}

	// A second, read-only connection keeps the WAL and SHM on disk through
	// Close(): SQLite checkpoints and deletes them only when the last
	// connection leaves, and a read-only connection can do neither. That is
	// the on-disk state a crashed or corrupt process leaves behind, which is
	// what the forensic set exists to capture.
	// The media driver, not the bare one: this file carries an index collated
	// with ZAPAROO_TITLE_V1, and a connection without that collation cannot
	// even run integrity_check against it. The count below happens not to need
	// it, which is not a property worth depending on.
	holder, err := sql.Open(sqliteMediaDriver, "file:"+path+"?mode=ro")
	require.NoError(t, err)
	holderConn, err := holder.Conn(ctx)
	require.NoError(t, err)
	var seen int
	require.NoError(t, holderConn.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&seen))
	require.Equal(t, 1, seen)
	holderReleased := false
	releaseHolder := func() {
		if holderReleased {
			return
		}
		holderReleased = true
		require.NoError(t, holderConn.Close())
		require.NoError(t, holder.Close())
	}
	t.Cleanup(releaseHolder)

	mediaDB.MarkCorrupt("test")
	require.NoError(t, mediaDB.Recreate(true))

	preserved := make(map[string]string, 3)
	for _, file := range []string{path, path + "-wal", path + "-shm"} {
		backup := database.CorruptBackupPath(file)
		require.FileExists(t, backup, "forensic set must include %s", filepath.Base(file))
		preserved[file] = backup
	}
	// The renamed files are no longer in use by anything: release the holder
	// before reading them back so the check below sees closed, quiescent files.
	releaseHolder()

	// One consistent set: put back under a single name it opens cleanly and
	// holds the committed row, which was only ever in the WAL.
	target := filepath.Join(t.TempDir(), "forensic.db")
	copyFileIfExists(t, preserved[path], target)
	copyFileIfExists(t, preserved[path+"-wal"], target+"-wal")
	copyFileIfExists(t, preserved[path+"-shm"], target+"-shm")
	forensic := openSnapshot(t, target)
	requireIntegrityOK(t, forensic)
	assert.Equal(t, 1, countRowsWhere(t, forensic, "Media", ""))

	// The rebuilt database is fresh, and the marker is cleared.
	assert.False(t, mediaDB.IsMarkedCorrupt())
	assert.Zero(t, countRowsWhere(t, mediaDB.sql.Load(), "Media", ""))
	ok, err := mediaDB.QuickCheck()
	require.NoError(t, err)
	assert.True(t, ok)
}
