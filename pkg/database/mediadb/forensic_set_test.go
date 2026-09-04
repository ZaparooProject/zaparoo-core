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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forensicSetSuffixes names the three files Recreate preserves as one set: the
// database, its WAL and the WAL index over it.
var forensicSetSuffixes = []string{"", "-wal", "-shm"}

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

	// Hold the three files aside exactly as they stand now, while the row is
	// still WAL-only. Closing the database checkpoints the row into the main
	// file and deletes the sidecars, so a copy taken here is the only way to
	// have the set back afterwards. Keeping a second connection open would
	// leave them on disk instead, but a file SQLite still has open cannot be
	// renamed or deleted on Windows, and renaming all three is exactly what
	// Recreate is about to do.
	held := filepath.Join(t.TempDir(), filepath.Base(path))
	for _, suffix := range forensicSetSuffixes {
		copyFileIfExists(t, path+suffix, held+suffix)
	}

	// The set only matters if the row is genuinely WAL-only, so prove the main
	// database alone cannot answer for it. Its own copy, not the held one: this
	// one is opened read-write, which would write sidecars next to it.
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

	// Put the pre-checkpoint set back over the closed database. That is the
	// on-disk state a process that died before its WAL was checkpointed leaves
	// behind, and the state the forensic set exists to capture: a main file
	// without the row, the WAL that holds it, and the index over that WAL.
	require.NoError(t, mediaDB.Close())
	for _, suffix := range forensicSetSuffixes {
		copyFileIfExists(t, held+suffix, path+suffix)
	}
	require.FileExists(t, path+"-wal", "the restored set must still carry the WAL")

	mediaDB.MarkCorrupt("test")
	require.NoError(t, mediaDB.Recreate(true))

	preserved := make(map[string]string, len(forensicSetSuffixes))
	for _, suffix := range forensicSetSuffixes {
		backup := database.CorruptBackupPath(path + suffix)
		require.FileExists(t, backup, "forensic set must include %s", filepath.Base(path+suffix))
		preserved[path+suffix] = backup
	}

	// One consistent set: put back under a single name it opens cleanly and
	// holds the committed row, which was only ever in the WAL.
	target := filepath.Join(t.TempDir(), "forensic.db")
	for _, suffix := range forensicSetSuffixes {
		copyFileIfExists(t, preserved[path+suffix], target+suffix)
	}
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
