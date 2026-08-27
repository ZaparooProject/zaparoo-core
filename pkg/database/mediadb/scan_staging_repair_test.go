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
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/require"
)

func TestFlagMissingMedia_ChunksLargeMissingSet(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open(sqliteDriverName(), filepath.Join(t.TempDir(), "media.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	_, err = sqlDB.ExecContext(ctx, `
		CREATE TABLE Media (
			DBID INTEGER PRIMARY KEY,
			MediaTitleDBID INTEGER NOT NULL,
			SystemDBID INTEGER NOT NULL,
			Path TEXT NOT NULL,
			IsMissing INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE ScanStage (Path TEXT PRIMARY KEY) WITHOUT ROWID;
		CREATE INDEX media_system_present_path_idx ON Media(SystemDBID, Path) WHERE IsMissing = 0;
	`)
	require.NoError(t, err)

	romsRoot := filepath.Join(t.TempDir(), "roms")
	c64Root := filepath.Join(romsRoot, "c64")
	keepPath := filepath.Join(c64Root, "keep.d64")
	otherPath := filepath.Join(romsRoot, "other", "other.d64")

	for i := range scanFlagMissingBatchSize + 1 {
		_, err = sqlDB.ExecContext(ctx,
			"INSERT INTO Media (MediaTitleDBID, SystemDBID, Path, IsMissing) VALUES (1, 1, ?, 0)",
			filepath.Join(c64Root, "old", fmt.Sprintf("%05d.d64", i)))
		require.NoError(t, err)
	}
	_, err = sqlDB.ExecContext(ctx,
		"INSERT INTO Media (MediaTitleDBID, SystemDBID, Path, IsMissing) VALUES (1, 1, ?, 0)", keepPath)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx,
		"INSERT INTO Media (MediaTitleDBID, SystemDBID, Path, IsMissing) VALUES (1, 2, ?, 0)", otherPath)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, "INSERT INTO ScanStage (Path) VALUES (?)", keepPath)
	require.NoError(t, err)

	affected, _, err := sqlFlagMissingMedia(ctx, sqlDB, "C64", 1, nil)
	require.NoError(t, err)
	require.EqualValues(t, scanFlagMissingBatchSize+1, affected)

	var missing, present int
	err = sqlDB.QueryRowContext(ctx,
		"SELECT COALESCE(SUM(IsMissing), 0), COUNT(*) - COALESCE(SUM(IsMissing), 0) FROM Media WHERE SystemDBID = 1").
		Scan(&missing, &present)
	require.NoError(t, err)
	require.Equal(t, scanFlagMissingBatchSize+1, missing)
	require.Equal(t, 1, present)

	var otherMissing int
	err = sqlDB.QueryRowContext(ctx, "SELECT IsMissing FROM Media WHERE SystemDBID = 2").Scan(&otherMissing)
	require.NoError(t, err)
	require.Zero(t, otherMissing)
}

func TestMediaCountsUseCachedDBConfig(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open(sqliteDriverName(), filepath.Join(t.TempDir(), "media.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	_, err = sqlDB.ExecContext(ctx, `
		CREATE TABLE DBConfig (Name TEXT PRIMARY KEY, Value TEXT NOT NULL);
		CREATE TABLE Media (DBID INTEGER PRIMARY KEY, IsMissing INTEGER NOT NULL DEFAULT 0);
		INSERT INTO DBConfig (Name, Value) VALUES ('MediaTotalCount', '123'), ('MediaMissingCount', '45');
	`)
	require.NoError(t, err)

	total, err := sqlGetTotalMediaCount(ctx, sqlDB)
	require.NoError(t, err)
	require.Equal(t, 123, total)

	missing, err := sqlGetMissingMediaCount(ctx, sqlDB)
	require.NoError(t, err)
	require.Equal(t, 45, missing)
}

func TestIndexedSystemsUsesBrowseCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open(sqliteDriverName(), filepath.Join(t.TempDir(), "media.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	_, err = sqlDB.ExecContext(ctx, `
		CREATE TABLE DBConfig (Name TEXT PRIMARY KEY, Value TEXT NOT NULL);
		CREATE TABLE Systems (DBID INTEGER PRIMARY KEY, SystemID TEXT NOT NULL UNIQUE);
		CREATE TABLE BrowseDirs (DBID INTEGER PRIMARY KEY, Path TEXT NOT NULL UNIQUE);
		CREATE TABLE BrowseDirCounts (
			ParentDirDBID INTEGER NOT NULL,
			ChildDirDBID INTEGER NOT NULL,
			SystemDBID INTEGER NOT NULL,
			FileCount INTEGER NOT NULL,
			PRIMARY KEY (ParentDirDBID, ChildDirDBID, SystemDBID)
		);
		INSERT INTO DBConfig (Name, Value) VALUES ('BrowseIndexVersion', ?);
		INSERT INTO Systems (DBID, SystemID) VALUES (1, 'SNES'), (2, 'NES'), (3, 'C64');
		INSERT INTO BrowseDirs (DBID, Path) VALUES (1, '/'), (2, '/roms'), (3, '/more-roms');
		INSERT INTO BrowseDirCounts (ParentDirDBID, ChildDirDBID, SystemDBID, FileCount)
		VALUES (1, 2, 2, 10), (1, 2, 1, 5), (1, 3, 2, 3);
	`, browseCacheSchemaVersion)
	require.NoError(t, err)

	systems, err := sqlIndexedSystems(ctx, sqlDB)
	require.NoError(t, err)
	require.Equal(t, []string{"NES", "SNES"}, systems)
}

func TestClearScanStage_RecreatesMissingScratchTables(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "media.db")
	sqlDB, err := sql.Open(sqliteDriverName(), dbPath+"?_foreign_keys=ON")
	require.NoError(t, err)

	mediaDB := &MediaDB{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	require.NoError(t, mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform))
	mediaDB.SetDBPathForTesting(dbPath)
	t.Cleanup(func() { require.NoError(t, mediaDB.Close()) })

	_, err = mediaDB.sql.Load().ExecContext(ctx, `
		DROP TABLE IF EXISTS ScanStageProperties;
		DROP TABLE ScanStageTags;
		DROP INDEX IF EXISTS scanstage_slug_idx;
		DROP TABLE ScanStage;
		DROP TABLE ScanTouchedTitles;
	`)
	require.NoError(t, err)

	require.NoError(t, mediaDB.ClearScanStage())

	for _, table := range []string{"ScanStage", "ScanStageTags", "ScanStageProperties", "ScanTouchedTitles"} {
		var name string
		err = mediaDB.sql.Load().QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
		require.NoError(t, err)
		require.Equal(t, table, name)
	}
	for _, index := range []string{
		"scanstage_slug_idx",
		"scanstagetags_type_tag_path_idx",
		"scanstageproperties_property_idx",
	} {
		var name string
		err = mediaDB.sql.Load().QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?", index).Scan(&name)
		require.NoError(t, err)
		require.Equal(t, index, name)
	}
}

// newUpsertStagedMediaTestDB builds a minimal schema covering only the
// columns sqlUpsertStagedMedia touches, mirroring the pattern used by
// TestFlagMissingMedia_ChunksLargeMissingSet above.
func newUpsertStagedMediaTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open(sqliteDriverName(), filepath.Join(t.TempDir(), "media.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	_, err = sqlDB.ExecContext(context.Background(), `
		CREATE TABLE MediaTitles (
			DBID INTEGER PRIMARY KEY,
			SystemDBID INTEGER NOT NULL,
			Slug TEXT NOT NULL
		);
		CREATE TABLE Media (
			DBID INTEGER PRIMARY KEY,
			MediaTitleDBID INTEGER NOT NULL,
			SystemDBID INTEGER NOT NULL,
			Path TEXT NOT NULL,
			ParentDir TEXT NOT NULL,
			SortName TEXT NOT NULL,
			IsMissing INTEGER NOT NULL DEFAULT 0,
			UNIQUE (SystemDBID, Path)
		);
		CREATE TABLE ScanStage (
			Path TEXT PRIMARY KEY,
			ParentDir TEXT NOT NULL,
			Slug TEXT NOT NULL,
			SortName TEXT NOT NULL
		) WITHOUT ROWID;
	`)
	require.NoError(t, err)
	return sqlDB
}

// stageSyntheticMedia inserts n zero-padded-path titles/staged rows, one
// title per row, so each row's identity in Media is unambiguous by Path.
// Paths are zero-padded so BINARY ordering matches insertion order, which
// the chunk-boundary tests below depend on.
func stageSyntheticMedia(t *testing.T, sqlDB *sql.DB, systemDBID int64, n int) {
	t.Helper()
	ctx := context.Background()
	for i := range n {
		slug := fmt.Sprintf("game-%06d", i)
		path := fmt.Sprintf("/roms/c64/%06d.d64", i)
		_, err := sqlDB.ExecContext(ctx,
			"INSERT INTO MediaTitles (SystemDBID, Slug) VALUES (?, ?)", systemDBID, slug)
		require.NoError(t, err)
		_, err = sqlDB.ExecContext(ctx,
			"INSERT INTO ScanStage (Path, ParentDir, Slug, SortName) VALUES (?, ?, ?, ?)",
			path, "/roms/c64", slug, slug)
		require.NoError(t, err)
	}
}

// readMediaRowsOrderedByPath reads every tracked Media column except DBID,
// ordered by Path — DBID assignment order is not part of the chunking
// contract (see sqlUpsertStagedMedia's doc comment).
func readMediaRowsOrderedByPath(t *testing.T, sqlDB *sql.DB, systemDBID int64) []string {
	t.Helper()
	rows, err := sqlDB.QueryContext(context.Background(), `
		SELECT Path || '|' || MediaTitleDBID || '|' || ParentDir || '|' || SortName || '|' || IsMissing
		FROM Media WHERE SystemDBID = ? ORDER BY Path`, systemDBID)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	result := make([]string, 0)
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		result = append(result, line)
	}
	require.NoError(t, rows.Err())
	return result
}

// TestUpsertStagedMedia_ChunksAcrossBatchBoundary proves the cursor covers
// the full staged key space across more than two chunk boundaries with no
// gap and no double-visit.
func TestUpsertStagedMedia_ChunksAcrossBatchBoundary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sqlDB := newUpsertStagedMediaTestDB(t)

	const systemDBID = 1
	const n = scanUpsertMediaBatchSize*2 + 1
	stageSyntheticMedia(t, sqlDB, systemDBID, n)

	affected, _, err := sqlUpsertStagedMedia(ctx, sqlDB, "C64", systemDBID, nil)
	require.NoError(t, err)
	require.EqualValues(t, n, affected)

	var count int
	require.NoError(t, sqlDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM Media WHERE SystemDBID = ?", systemDBID).Scan(&count))
	require.Equal(t, n, count)

	var mismatched int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM Media m
		JOIN ScanStage s ON s.Path = m.Path
		JOIN MediaTitles t ON t.DBID = m.MediaTitleDBID
		WHERE m.SystemDBID = ?
		  AND (t.Slug <> s.Slug OR m.ParentDir <> s.ParentDir OR m.SortName <> s.SortName OR m.IsMissing <> 0)`,
		systemDBID).Scan(&mismatched))
	require.Zero(t, mismatched)
}

// TestUpsertStagedMedia_NoOpChunkTerminates is the direct regression guard
// for the "RowsAffected can't drive the loop" trap: ON CONFLICT ... WHERE
// <changed> reports 0 for a rerun even though every chunk still has rows to
// examine, so the cursor — not the affected count — must terminate the loop.
// The timeout turns a cursor regression into a fast test failure instead of
// a hung suite. This also doubles as an idempotency check.
func TestUpsertStagedMedia_NoOpChunkTerminates(t *testing.T) {
	t.Parallel()
	sqlDB := newUpsertStagedMediaTestDB(t)

	const systemDBID = 1
	const n = scanUpsertMediaBatchSize + 5
	stageSyntheticMedia(t, sqlDB, systemDBID, n)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	first, _, err := sqlUpsertStagedMedia(ctx, sqlDB, "C64", systemDBID, nil)
	require.NoError(t, err)
	require.EqualValues(t, n, first)

	second, _, err := sqlUpsertStagedMedia(ctx, sqlDB, "C64", systemDBID, nil)
	require.NoError(t, err)
	require.Zero(t, second)
}

// TestUpsertStagedMedia_MatchesUnchunkedResult proves chunking doesn't change
// the final result: the same staged set run through the chunked helper and
// through the original single-statement SQL must produce identical Media
// rows (DBID assignment order aside — see readMediaRowsOrderedByPath).
func TestUpsertStagedMedia_MatchesUnchunkedResult(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const systemDBID = 1
	const n = scanUpsertMediaBatchSize + 137 // spans a boundary, not a multiple of it

	chunkedDB := newUpsertStagedMediaTestDB(t)
	stageSyntheticMedia(t, chunkedDB, systemDBID, n)
	_, _, err := sqlUpsertStagedMedia(ctx, chunkedDB, "C64", systemDBID, nil)
	require.NoError(t, err)

	unchunkedDB := newUpsertStagedMediaTestDB(t)
	stageSyntheticMedia(t, unchunkedDB, systemDBID, n)
	_, err = unchunkedDB.ExecContext(ctx, `
		INSERT INTO Media (MediaTitleDBID, SystemDBID, Path, ParentDir, SortName, IsMissing)
		SELECT t.DBID, ?, s.Path, s.ParentDir, s.SortName, 0
		FROM ScanStage s
		JOIN MediaTitles t ON t.SystemDBID = ? AND t.Slug = s.Slug
		WHERE true
		ON CONFLICT (SystemDBID, Path) DO UPDATE SET
			MediaTitleDBID = excluded.MediaTitleDBID,
			ParentDir      = excluded.ParentDir,
			SortName       = excluded.SortName,
			IsMissing      = 0
		WHERE MediaTitleDBID <> excluded.MediaTitleDBID
		   OR ParentDir <> excluded.ParentDir
		   OR SortName <> excluded.SortName
		   OR IsMissing <> 0`, systemDBID, systemDBID)
	require.NoError(t, err)

	require.Equal(t,
		readMediaRowsOrderedByPath(t, unchunkedDB, systemDBID),
		readMediaRowsOrderedByPath(t, chunkedDB, systemDBID))
}

// TestUpsertStagedMedia_UpdatesChangedRowsAcrossChunks proves the
// ON CONFLICT ... WHERE <changed> guard still discriminates per chunk:
// only rows whose tracked columns actually differ are reported/corrected,
// across a pre-populated set that straddles a chunk boundary.
func TestUpsertStagedMedia_UpdatesChangedRowsAcrossChunks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sqlDB := newUpsertStagedMediaTestDB(t)

	const systemDBID = 1
	const n = scanUpsertMediaBatchSize + 50
	stageSyntheticMedia(t, sqlDB, systemDBID, n)

	// Rows [changedFrom, changedTo) straddle the chunk boundary at
	// scanUpsertMediaBatchSize and get deliberately wrong values that must be
	// corrected. Every other row is pre-populated already matching what the
	// stage would produce, and must NOT be reported as changed.
	const changedFrom = scanUpsertMediaBatchSize - 5
	const changedTo = scanUpsertMediaBatchSize + 5
	for i := range n {
		slug := fmt.Sprintf("game-%06d", i)
		path := fmt.Sprintf("/roms/c64/%06d.d64", i)
		var titleDBID int64
		require.NoError(t, sqlDB.QueryRowContext(ctx,
			"SELECT DBID FROM MediaTitles WHERE SystemDBID = ? AND Slug = ?", systemDBID, slug).
			Scan(&titleDBID))

		if i >= changedFrom && i < changedTo {
			_, err := sqlDB.ExecContext(ctx, `
				INSERT INTO Media (MediaTitleDBID, SystemDBID, Path, ParentDir, SortName, IsMissing)
				VALUES (?, ?, ?, 'wrong', 'wrong', 1)`, titleDBID, systemDBID, path)
			require.NoError(t, err)
		} else {
			_, err := sqlDB.ExecContext(ctx, `
				INSERT INTO Media (MediaTitleDBID, SystemDBID, Path, ParentDir, SortName, IsMissing)
				VALUES (?, ?, ?, '/roms/c64', ?, 0)`, titleDBID, systemDBID, path, slug)
			require.NoError(t, err)
		}
	}

	affected, _, err := sqlUpsertStagedMedia(ctx, sqlDB, "C64", systemDBID, nil)
	require.NoError(t, err)
	require.EqualValues(t, changedTo-changedFrom, affected)

	var stillWrong int
	require.NoError(t, sqlDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM Media WHERE SystemDBID = ? "+
			"AND (ParentDir = 'wrong' OR SortName = 'wrong' OR IsMissing <> 0)",
		systemDBID).Scan(&stillWrong))
	require.Zero(t, stillWrong)
}
