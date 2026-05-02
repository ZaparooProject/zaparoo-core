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

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func browseTestPath(parts ...string) string {
	return filepath.ToSlash(filepath.Join(append([]string{string(filepath.Separator)}, parts...)...))
}

func browseTestDir(parts ...string) string {
	return browseTestPath(parts...) + "/"
}

func expectBrowseCacheReady(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ").
		WithArgs(DBConfigBrowseIndexVersion).
		WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow(browseCacheSchemaVersion))
	mock.ExpectQuery("SELECT 1 FROM BrowseDirs LIMIT 1").
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
}

func TestSqlBrowseDirectoriesFromCache_ReturnsSystemCounts(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectBrowseCacheReady(mock)
	gamesDir := browseTestDir("media", "fat", "games")
	mock.ExpectQuery("SELECT DBID FROM BrowseDirs WHERE Path = ").
		WithArgs(gamesDir).
		WillReturnRows(sqlmock.NewRows([]string{"DBID"}).AddRow(10))
	mock.ExpectQuery("SELECT d.Name, c.FileCount").
		WithArgs(int64(10), "SNES").
		WillReturnRows(sqlmock.NewRows([]string{"Name", "FileCount"}).
			AddRow("SNES", 42))

	snes := systemdefs.System{ID: "SNES"}
	results, err := sqlBrowseDirectories(context.Background(), db, database.BrowseDirectoriesOptions{
		PathPrefix: gamesDir,
		Systems:    []systemdefs.System{snes},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "SNES", results[0].Name)
	assert.Equal(t, 42, results[0].FileCount)
	assert.Equal(t, []string{"SNES"}, results[0].SystemIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseDirectories_FallsBackWhenCacheNotReady(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ").
		WithArgs(DBConfigBrowseIndexVersion).
		WillReturnError(sql.ErrNoRows)
	romsDir := browseTestDir("roms")
	mock.ExpectQuery("WITH matched AS").
		WithArgs(romsDir, romsDir).
		WillReturnRows(sqlmock.NewRows([]string{"Name", "FileCount"}).AddRow("SNES", 2))

	results, err := sqlBrowseDirectories(context.Background(), db, database.BrowseDirectoriesOptions{
		PathPrefix: romsDir,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "SNES", results[0].Name)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseVirtualSchemesFromCache_ReturnsEmptyWithoutMediaFallback(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectBrowseCacheReady(mock)
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

func TestSqlBrowseVirtualSchemesFromCache_ReturnsEmptyWhenRootMissing(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectBrowseCacheReady(mock)
	mock.ExpectQuery("SELECT DBID FROM BrowseDirs WHERE Path = ").
		WithArgs("").
		WillReturnError(sql.ErrNoRows)

	results, err := sqlBrowseVirtualSchemes(context.Background(), db, database.BrowseVirtualSchemesOptions{
		Systems: []systemdefs.System{{ID: "SNES"}},
	})
	require.NoError(t, err)
	assert.Empty(t, results)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseRouteCountsFromCache_UsesChildDirCounts(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectBrowseCacheReady(mock)
	snesDir := browseTestDir("media", "fat", "games", "SNES")
	snesRoute := browseTestPath("media", "fat", "games", "SNES")
	mock.ExpectQuery("SELECT DBID FROM BrowseDirs WHERE Path = ").
		WithArgs(snesDir).
		WillReturnRows(sqlmock.NewRows([]string{"DBID"}).AddRow(99))
	mock.ExpectQuery("SELECT COALESCE\\(SUM").
		WithArgs(int64(99), "SNES").
		WillReturnRows(sqlmock.NewRows([]string{"FileCount", "SystemIDs"}).AddRow(123, "SNES"))

	counts, err := sqlBrowseRouteCounts(context.Background(), db, database.BrowseRouteCountsOptions{
		Routes:  []string{snesRoute},
		Systems: []systemdefs.System{{ID: "SNES"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 123, counts[snesRoute].FileCount)
	assert.Equal(t, []string{"SNES"}, counts[snesRoute].SystemIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBrowseCacheBuilder_NormalizesRelativeFilesystemDirs(t *testing.T) {
	t.Parallel()

	builder := newBrowseCacheBuilder()
	builder.ensureDir("/")
	builder.addMedia(2, filepath.Join("roms", "nes", "game.nes"))

	assert.Contains(t, builder.dirs, "/")
	assert.Contains(t, builder.dirs, "/roms/")
	assert.Contains(t, builder.dirs, "/roms/nes/")
	assert.NotContains(t, builder.dirs, "./")
}

func TestBrowseCacheBuilder_NormalizesFilesystemPathSeparators(t *testing.T) {
	t.Parallel()

	builder := newBrowseCacheBuilder()
	builder.ensureDir("/")
	builder.addMedia(2, `roms\\snes//RPG/../Action/game.sfc`)

	assert.Contains(t, builder.dirs, "/roms/")
	assert.Contains(t, builder.dirs, "/roms/snes/")
	assert.Contains(t, builder.dirs, "/roms/snes/Action/")
	assert.NotContains(t, builder.dirs, `/roms\\snes/`)
}

func TestBrowseCacheBuilder_NormalizesURIPathPortion(t *testing.T) {
	t.Parallel()

	builder := newBrowseCacheBuilder()
	builder.ensureDir("/")
	builder.addMedia(2, `steam://440\\Team Fortress 2/../Team Fortress 2`)

	assert.Contains(t, builder.dirs, "steam://")
	assert.NotContains(t, builder.dirs, `steam://440\\Team Fortress 2/../`)
}

func TestSqlBrowseRootCountsFromCache_ReturnsZeroForMissingRoot(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectBrowseCacheReady(mock)
	snesRoot := browseTestPath("roms", "SNES")
	nesRoot := browseTestPath("roms", "NES")
	mock.ExpectQuery("SELECT DBID FROM BrowseDirs WHERE Path = ").
		WithArgs(browseTestDir("roms", "SNES")).
		WillReturnRows(sqlmock.NewRows([]string{"DBID"}).AddRow(7))
	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(FileCount\\)").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"FileCount"}).AddRow(10))
	mock.ExpectQuery("SELECT DBID FROM BrowseDirs WHERE Path = ").
		WithArgs(browseTestDir("roms", "NES")).
		WillReturnError(sql.ErrNoRows)

	counts, err := sqlBrowseRootCounts(context.Background(), db, []string{snesRoot, nesRoot})
	require.NoError(t, err)
	require.NotNil(t, counts[snesRoot])
	assert.Equal(t, 10, *counts[snesRoot])
	require.NotNil(t, counts[nesRoot])
	assert.Equal(t, 0, *counts[nesRoot])
	require.NoError(t, mock.ExpectationsWereMet())
}
