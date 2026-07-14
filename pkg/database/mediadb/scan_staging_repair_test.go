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

	affected, err := sqlFlagMissingMedia(ctx, sqlDB, "C64", 1)
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
		INSERT INTO DBConfig (Name, Value) VALUES ('BrowseIndexVersion', '2');
		INSERT INTO Systems (DBID, SystemID) VALUES (1, 'SNES'), (2, 'NES'), (3, 'C64');
		INSERT INTO BrowseDirs (DBID, Path) VALUES (1, '/'), (2, '/roms'), (3, '/more-roms');
		INSERT INTO BrowseDirCounts (ParentDirDBID, ChildDirDBID, SystemDBID, FileCount)
		VALUES (1, 2, 2, 10), (1, 2, 1, 5), (1, 3, 2, 3);
	`)
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
