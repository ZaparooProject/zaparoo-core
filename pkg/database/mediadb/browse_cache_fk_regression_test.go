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

// Regression tests for the browse cache clear (#1279).
//
// The rebuild used to run two unqualified DELETEs with foreign key enforcement
// on. BrowseDirs is the parent of three ON DELETE CASCADE foreign keys — a
// self-reference plus two from BrowseDirCounts — so SQLite could not use its
// truncate optimization: it deleted row by row, running child probes and the
// recursive self-cascade through a statement journal. On the MiSTer test device
// that measured 261.9s to clear ~21,500 rows, 13x the cost of the inserts that
// replaced them.
//
// The fix pins a connection and disables foreign keys around the rebuild
// transaction. That makes it critical to prove the pragma is genuinely restored
// afterwards: a pooled connection leaking foreign_keys=OFF would silently
// disable enforcement for unrelated queries for the rest of the process.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertForeignKeysEnforced proves enforcement is actually active, rather than
// only that the pragma reads back as ON: it attempts a real violation and
// requires SQLite to reject it.
func assertForeignKeysEnforced(t *testing.T, ctx context.Context, mediaDB *MediaDB) { //nolint:revive // t before ctx is standard test helper convention
	t.Helper()

	_, err := mediaDB.sql.Load().ExecContext(ctx,
		"INSERT INTO BrowseDirCounts (ParentDirDBID, ChildDirDBID, SystemDBID, FileCount) "+
			"VALUES (?, ?, ?, ?)",
		999999, 999999, 999999, 1)
	require.Error(t, err,
		"foreign key enforcement must be active after a browse cache rebuild; "+
			"a connection leaked back into the pool with foreign_keys=OFF")
	assert.Contains(t, err.Error(), "FOREIGN KEY",
		"expected an FK constraint violation, got a different error")
}

// TestPopulateBrowseCache_RestoresForeignKeys_Integration is the guard that the
// FK-disabling fix does not leak. It checks every connection the pool can hand
// out, not just one, because only the pinned connection had the pragma changed.
func TestPopulateBrowseCache_RestoresForeignKeys_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()
	insertSystemWithMedia(t, mediaDB, "SNES", "Super RPG",
		filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "snes", "super-rpg.sfc")))

	// Enforcement is on before the rebuild.
	assertForeignKeysEnforced(t, ctx, mediaDB)

	require.NoError(t, sqlPopulateBrowseCache(ctx, mediaDB.sql.Load()))

	// ...and still on after it, on any connection the pool returns.
	for range 8 {
		assertForeignKeysEnforced(t, ctx, mediaDB)
	}

	var fkEnabled int
	require.NoError(t,
		mediaDB.sql.Load().QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fkEnabled))
	assert.Equal(t, 1, fkEnabled, "PRAGMA foreign_keys should read back as enabled")
}

// TestPopulateBrowseCache_RebuildDropsStaleRows_Integration checks the clear
// still does its job with foreign keys off — rows for media that no longer
// exists must not survive a rebuild.
func TestPopulateBrowseCache_RebuildDropsStaleRows_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()
	snes := insertSystemWithMedia(t, mediaDB, "SNES", "Super RPG",
		filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "snes", "super-rpg.sfc")))
	insertSystemWithMedia(t, mediaDB, "NES", "Mario",
		filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "nes", "mario.nes")))

	require.NoError(t, sqlPopulateBrowseCache(ctx, mediaDB.sql.Load()))
	before := countTableRows(t, mediaDB, "BrowseDirs", "1=1")
	require.Positive(t, before, "first rebuild should populate dirs")
	require.Positive(t, countTableRows(t, mediaDB, "BrowseDirCounts",
		"SystemDBID = ?", snes.DBID))

	// Remove every SNES media row, then rebuild.
	_, err := mediaDB.sql.Load().ExecContext(ctx, "DELETE FROM Media WHERE SystemDBID = ?", snes.DBID)
	require.NoError(t, err)
	require.NoError(t, sqlPopulateBrowseCache(ctx, mediaDB.sql.Load()))

	assert.Zero(t, countTableRows(t, mediaDB, "BrowseDirCounts", "SystemDBID = ?", snes.DBID),
		"counts for a system with no media must not survive the rebuild")

	// The clear runs with foreign keys off, so orphans would go unnoticed at
	// write time. Verify the rebuilt tables are self-consistent anyway.
	assert.Zero(t, countTableRows(t, mediaDB, "BrowseDirCounts",
		"ParentDirDBID NOT IN (SELECT DBID FROM BrowseDirs)"),
		"BrowseDirCounts.ParentDirDBID must reference a live BrowseDirs row")
	assert.Zero(t, countTableRows(t, mediaDB, "BrowseDirCounts",
		"ChildDirDBID NOT IN (SELECT DBID FROM BrowseDirs)"),
		"BrowseDirCounts.ChildDirDBID must reference a live BrowseDirs row")
	assert.Zero(t, countTableRows(t, mediaDB, "BrowseDirs",
		"ParentDirDBID IS NOT NULL AND ParentDirDBID NOT IN (SELECT DBID FROM BrowseDirs)"),
		"BrowseDirs.ParentDirDBID must reference a live BrowseDirs row")

	assertForeignKeysEnforced(t, ctx, mediaDB)
}
