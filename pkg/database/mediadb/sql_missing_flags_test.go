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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertTestSystem inserts a System row and returns its DBID.
func insertTestSystem(t *testing.T, db *MediaDB, systemID, name string) int64 {
	t.Helper()
	result, err := db.sql.ExecContext(
		context.Background(),
		`INSERT INTO Systems (SystemID, Name) VALUES (?, ?)`,
		systemID, name,
	)
	require.NoError(t, err)
	id, err := result.LastInsertId()
	require.NoError(t, err)
	return id
}

// insertTestMediaTitle inserts a MediaTitle row and returns its DBID.
func insertTestMediaTitle(t *testing.T, db *MediaDB, systemDBID int64, slug, name string) int64 {
	t.Helper()
	result, err := db.sql.ExecContext(
		context.Background(),
		`INSERT INTO MediaTitles (SystemDBID, Slug, Name) VALUES (?, ?, ?)`,
		systemDBID, slug, name,
	)
	require.NoError(t, err)
	id, err := result.LastInsertId()
	require.NoError(t, err)
	return id
}

// insertTestMedia inserts a Media row with IsMissing=0 and returns its DBID.
func insertTestMedia(t *testing.T, db *MediaDB, mediaTitleDBID, systemDBID int64, path string) int64 {
	t.Helper()
	result, err := db.sql.ExecContext(
		context.Background(),
		`INSERT INTO Media (MediaTitleDBID, SystemDBID, Path) VALUES (?, ?, ?)`,
		mediaTitleDBID, systemDBID, path,
	)
	require.NoError(t, err)
	id, err := result.LastInsertId()
	require.NoError(t, err)
	return id
}

// countMissingBySystem counts rows in Media with IsMissing=1 for a given SystemDBID.
func countMissingBySystem(t *testing.T, db *MediaDB, systemDBID int64) int {
	t.Helper()
	var count int
	err := db.sql.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM Media WHERE SystemDBID = ? AND IsMissing = 1`,
		systemDBID,
	).Scan(&count)
	require.NoError(t, err)
	return count
}

// countNonMissingBySystem counts rows in Media with IsMissing=0 for a given SystemDBID.
func countNonMissingBySystem(t *testing.T, db *MediaDB, systemDBID int64) int {
	t.Helper()
	var count int
	err := db.sql.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM Media WHERE SystemDBID = ? AND IsMissing = 0`,
		systemDBID,
	).Scan(&count)
	require.NoError(t, err)
	return count
}

func TestSqlBulkSetMediaMissing_EmptyInput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	err := sqlBulkSetMediaMissing(context.Background(), db.sql, map[int]struct{}{})
	assert.NoError(t, err)
}

func TestSqlBulkSetMediaMissing_SingleChunk(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	sysDBID := insertTestSystem(t, db, "nes", "Nintendo Entertainment System")
	titleDBID := insertTestMediaTitle(t, db, sysDBID, "smb", "Super Mario Bros")

	const count = 5
	ids := make(map[int]struct{}, count)
	for i := range count {
		dbid := insertTestMedia(t, db, titleDBID, sysDBID, fmt.Sprintf("/games/nes/game%d.nes", i))
		ids[int(dbid)] = struct{}{}
	}

	require.Equal(t, 0, countMissingBySystem(t, db, sysDBID), "no rows should be missing before call")

	err := sqlBulkSetMediaMissing(context.Background(), db.sql, ids)
	require.NoError(t, err)

	assert.Equal(t, count, countMissingBySystem(t, db, sysDBID), "all rows should be marked missing")
	assert.Equal(t, 0, countNonMissingBySystem(t, db, sysDBID), "no rows should remain non-missing")
}

func TestSqlBulkSetMediaMissing_MultipleChunks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	sysDBID := insertTestSystem(t, db, "snes", "Super Nintendo")
	titleDBID := insertTestMediaTitle(t, db, sysDBID, "title", "Title")

	// Insert more than one chunk's worth of rows (chunkSize = 500).
	const count = 501
	ids := make(map[int]struct{}, count)
	for i := range count {
		dbid := insertTestMedia(t, db, titleDBID, sysDBID, fmt.Sprintf("/games/snes/rom%04d.sfc", i))
		ids[int(dbid)] = struct{}{}
	}

	require.Equal(t, 0, countMissingBySystem(t, db, sysDBID), "no rows should be missing before call")

	err := sqlBulkSetMediaMissing(context.Background(), db.sql, ids)
	require.NoError(t, err)

	assert.Equal(t, count, countMissingBySystem(t, db, sysDBID),
		"all %d rows should be marked missing across chunks", count)
	assert.Equal(t, 0, countNonMissingBySystem(t, db, sysDBID), "no rows should remain non-missing")
}

func TestSqlBulkSetMediaMissing_SkipsAlreadyMissing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	sysDBID := insertTestSystem(t, db, "genesis", "Sega Genesis")
	titleDBID := insertTestMediaTitle(t, db, sysDBID, "sonic", "Sonic")

	// Insert 3 rows with IsMissing=0.
	ids := make(map[int]struct{})
	for i := range 3 {
		dbid := insertTestMedia(t, db, titleDBID, sysDBID, fmt.Sprintf("/games/genesis/rom%d.bin", i))
		ids[int(dbid)] = struct{}{}
	}

	// Manually mark one row missing before the bulk call.
	firstID := func() int {
		for id := range ids {
			return id
		}
		return 0
	}()
	_, err := db.sql.ExecContext(
		context.Background(),
		`UPDATE Media SET IsMissing = 1 WHERE DBID = ?`,
		firstID,
	)
	require.NoError(t, err)

	err = sqlBulkSetMediaMissing(context.Background(), db.sql, ids)
	require.NoError(t, err)

	// All 3 rows should now be missing (including the one pre-set).
	assert.Equal(t, 3, countMissingBySystem(t, db, sysDBID))
}

func TestSqlResetMissingFlags_EmptyInput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	err := sqlResetMissingFlags(context.Background(), db.sql, []int{})
	assert.NoError(t, err)
}

func TestSqlResetMissingFlags_OnlyTargetedSystem(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	sys1DBID := insertTestSystem(t, db, "nes", "Nintendo Entertainment System")
	sys2DBID := insertTestSystem(t, db, "snes", "Super Nintendo")

	title1DBID := insertTestMediaTitle(t, db, sys1DBID, "mario", "Mario")
	title2DBID := insertTestMediaTitle(t, db, sys2DBID, "zelda", "Zelda")

	// Insert Media rows for both systems.
	const perSystem = 4
	for i := range perSystem {
		insertTestMedia(t, db, title1DBID, sys1DBID, fmt.Sprintf("/nes/game%d.nes", i))
		insertTestMedia(t, db, title2DBID, sys2DBID, fmt.Sprintf("/snes/game%d.sfc", i))
	}

	// Mark all rows missing.
	_, err := db.sql.ExecContext(context.Background(), `UPDATE Media SET IsMissing = 1`)
	require.NoError(t, err)

	require.Equal(t, perSystem, countMissingBySystem(t, db, sys1DBID))
	require.Equal(t, perSystem, countMissingBySystem(t, db, sys2DBID))

	// Reset only system 1.
	err = sqlResetMissingFlags(context.Background(), db.sql, []int{int(sys1DBID)})
	require.NoError(t, err)

	assert.Equal(t, 0, countMissingBySystem(t, db, sys1DBID), "system 1 rows should be reset")
	assert.Equal(t, perSystem, countMissingBySystem(t, db, sys2DBID), "system 2 rows should remain missing")
}

func TestSqlResetMissingFlags_MultipleSystemsPartialReset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	sys1DBID := insertTestSystem(t, db, "gb", "Game Boy")
	sys2DBID := insertTestSystem(t, db, "gbc", "Game Boy Color")
	sys3DBID := insertTestSystem(t, db, "gba", "Game Boy Advance")

	title1DBID := insertTestMediaTitle(t, db, sys1DBID, "tetris", "Tetris")
	title2DBID := insertTestMediaTitle(t, db, sys2DBID, "links", "Links Awakening DX")
	title3DBID := insertTestMediaTitle(t, db, sys3DBID, "advance", "Advance Wars")

	for i := range 3 {
		insertTestMedia(t, db, title1DBID, sys1DBID, fmt.Sprintf("/gb/game%d.gb", i))
		insertTestMedia(t, db, title2DBID, sys2DBID, fmt.Sprintf("/gbc/game%d.gbc", i))
		insertTestMedia(t, db, title3DBID, sys3DBID, fmt.Sprintf("/gba/game%d.gba", i))
	}

	// Mark all rows missing.
	_, err := db.sql.ExecContext(context.Background(), `UPDATE Media SET IsMissing = 1`)
	require.NoError(t, err)

	// Reset sys1 and sys3; leave sys2 missing.
	err = sqlResetMissingFlags(context.Background(), db.sql, []int{int(sys1DBID), int(sys3DBID)})
	require.NoError(t, err)

	assert.Equal(t, 0, countMissingBySystem(t, db, sys1DBID), "sys1 should be reset")
	assert.Equal(t, 3, countMissingBySystem(t, db, sys2DBID), "sys2 should remain missing")
	assert.Equal(t, 0, countMissingBySystem(t, db, sys3DBID), "sys3 should be reset")
}
