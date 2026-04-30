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
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func expectBrowseV2Ready(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ").
		WithArgs(DBConfigBrowseIndexVersion).
		WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow(browseIndexVersion))
	mock.ExpectQuery("SELECT 1 FROM BrowseDirs LIMIT 1").
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
}

func TestSqlBrowseDirectoriesV2_ReturnsSystemCounts(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectBrowseV2Ready(mock)
	mock.ExpectQuery("SELECT DBID FROM BrowseDirs WHERE Path = ").
		WithArgs("/media/fat/games/").
		WillReturnRows(sqlmock.NewRows([]string{"DBID"}).AddRow(10))
	mock.ExpectQuery("SELECT d.Name, c.FileCount").
		WithArgs(int64(10), "SNES").
		WillReturnRows(sqlmock.NewRows([]string{"Name", "FileCount"}).
			AddRow("SNES", 42))

	snes := systemdefs.System{ID: "SNES"}
	results, err := sqlBrowseDirectories(context.Background(), db, database.BrowseDirectoriesOptions{
		PathPrefix: "/media/fat/games/",
		Systems:    []systemdefs.System{snes},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "SNES", results[0].Name)
	assert.Equal(t, 42, results[0].FileCount)
	assert.Equal(t, []string{"SNES"}, results[0].SystemIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseDirectories_FallsBackWhenV2NotReady(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ").
		WithArgs(DBConfigBrowseIndexVersion).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("WITH matched AS").
		WithArgs("/roms/", "/roms/").
		WillReturnRows(sqlmock.NewRows([]string{"Name", "FileCount"}).AddRow("SNES", 2))

	results, err := sqlBrowseDirectories(context.Background(), db, database.BrowseDirectoriesOptions{
		PathPrefix: "/roms/",
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "SNES", results[0].Name)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseVirtualSchemesV2_ReturnsEmptyWithoutMediaFallback(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectBrowseV2Ready(mock)
	mock.ExpectQuery("SELECT DBID FROM BrowseDirs WHERE Path = ").
		WithArgs("").
		WillReturnRows(sqlmock.NewRows([]string{"DBID"}).AddRow(1))
	mock.ExpectQuery("SELECT d.Path, SUM").
		WithArgs(int64(1), "SNES").
		WillReturnRows(sqlmock.NewRows([]string{"Path", "FileCount", "SystemIDs"}))

	results, err := sqlBrowseVirtualSchemes(context.Background(), db, database.BrowseVirtualSchemesOptions{
		Systems: []systemdefs.System{{ID: "SNES"}},
	})
	require.NoError(t, err)
	assert.Empty(t, results)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseRouteCountsV2_UsesChildDirCounts(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectBrowseV2Ready(mock)
	mock.ExpectQuery("SELECT DBID FROM BrowseDirs WHERE Path = ").
		WithArgs("/media/fat/games/SNES/").
		WillReturnRows(sqlmock.NewRows([]string{"DBID"}).AddRow(99))
	mock.ExpectQuery("SELECT COALESCE\\(SUM").
		WithArgs(int64(99), "SNES").
		WillReturnRows(sqlmock.NewRows([]string{"FileCount", "SystemIDs"}).AddRow(123, "SNES"))

	counts, err := sqlBrowseRouteCounts(context.Background(), db, database.BrowseRouteCountsOptions{
		Routes:  []string{"/media/fat/games/SNES"},
		Systems: []systemdefs.System{{ID: "SNES"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 123, counts["/media/fat/games/SNES"].FileCount)
	assert.Equal(t, []string{"SNES"}, counts["/media/fat/games/SNES"].SystemIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBrowseV2CursorSortValue_UsesFileNameForFilenameSort(t *testing.T) {
	t.Parallel()

	value := browseV2CursorSortValue(&database.BrowseFilesOptions{
		Sort: "filename-asc",
		Cursor: &database.BrowseCursor{
			SortValue: "/media/fat/games/SNES/Super Metroid.sfc",
			LastID:    42,
		},
	})

	assert.Equal(t, "Super Metroid.sfc", value)
}

func TestBrowseV2Builder_NormalizesRelativeFilesystemDirs(t *testing.T) {
	t.Parallel()

	builder := newBrowseV2Builder()
	builder.ensureDir("/")
	builder.addMedia(1, 2, "roms/nes/game.nes", "Game")

	assert.Contains(t, builder.dirs, "/")
	assert.Contains(t, builder.dirs, "/roms/")
	assert.Contains(t, builder.dirs, "/roms/nes/")
	assert.NotContains(t, builder.dirs, "./")
}

func TestSqlBrowseRootCountsV2_ReturnsZeroForMissingRoot(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectBrowseV2Ready(mock)
	mock.ExpectQuery("SELECT DBID FROM BrowseDirs WHERE Path = ").
		WithArgs("/roms/SNES/").
		WillReturnRows(sqlmock.NewRows([]string{"DBID"}).AddRow(7))
	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(FileCount\\)").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"FileCount"}).AddRow(10))
	mock.ExpectQuery("SELECT DBID FROM BrowseDirs WHERE Path = ").
		WithArgs("/roms/NES/").
		WillReturnError(sql.ErrNoRows)

	counts, err := sqlBrowseRootCounts(context.Background(), db, []string{"/roms/SNES", "/roms/NES"})
	require.NoError(t, err)
	require.NotNil(t, counts["/roms/SNES"])
	assert.Equal(t, 10, *counts["/roms/SNES"])
	require.NotNil(t, counts["/roms/NES"])
	assert.Equal(t, 0, *counts["/roms/NES"])
	require.NoError(t, mock.ExpectationsWereMet())
}
