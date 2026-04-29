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
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSqlBrowseRootCounts_PassesArgs(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`SELECT DirPath, FileCount FROM BrowseCache WHERE DirPath IN`).
		WithArgs("/roms/SNES/", "/roms/NES/").
		WillReturnRows(
			sqlmock.NewRows([]string{"DirPath", "FileCount"}).
				AddRow("/roms/SNES/", 42).
				AddRow("/roms/NES/", 17),
		)

	counts, err := sqlBrowseRootCounts(context.Background(), db, []string{"/roms/SNES", "/roms/NES"})
	require.NoError(t, err)

	require.NotNil(t, counts["/roms/SNES"])
	assert.Equal(t, 42, *counts["/roms/SNES"])
	require.NotNil(t, counts["/roms/NES"])
	assert.Equal(t, 17, *counts["/roms/NES"])

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseRootCounts_CacheMiss(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Only one of two roots is in the cache.
	mock.ExpectQuery(`SELECT DirPath, FileCount FROM BrowseCache WHERE DirPath IN`).
		WithArgs("/roms/SNES/", "/roms/NES/").
		WillReturnRows(
			sqlmock.NewRows([]string{"DirPath", "FileCount"}).
				AddRow("/roms/SNES/", 42),
		)

	counts, err := sqlBrowseRootCounts(context.Background(), db, []string{"/roms/SNES", "/roms/NES"})
	require.NoError(t, err)

	require.NotNil(t, counts["/roms/SNES"])
	assert.Equal(t, 42, *counts["/roms/SNES"])
	assert.Nil(t, counts["/roms/NES"])

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseRootCounts_Empty(t *testing.T) {
	t.Parallel()

	counts, err := sqlBrowseRootCounts(context.Background(), nil, []string{})
	require.NoError(t, err)
	assert.Empty(t, counts)
}

func TestSqlBrowseVirtualSchemes_UsesIsVirtual(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`SELECT DirPath, FileCount FROM BrowseCache WHERE IsVirtual = 1`).
		WillReturnRows(
			sqlmock.NewRows([]string{"DirPath", "FileCount"}).
				AddRow("steam://", 150).
				AddRow("igdb://", 30),
		)

	schemes, err := sqlBrowseVirtualSchemes(context.Background(), db, database.BrowseVirtualSchemesOptions{})
	require.NoError(t, err)

	require.Len(t, schemes, 2)
	assert.Equal(t, "steam://", schemes[0].Scheme)
	assert.Equal(t, 150, schemes[0].FileCount)
	assert.Equal(t, "igdb://", schemes[1].Scheme)
	assert.Equal(t, 30, schemes[1].FileCount)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseDirectories_WithSystemsUsesSystemCache(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`(?s)SELECT b.Name, SUM\(b.FileCount\), GROUP_CONCAT\(DISTINCT s.SystemID\).*BrowseSystemCache`).
		WithArgs("/roms/shared/", "SNES", "Genesis").
		WillReturnRows(
			sqlmock.NewRows([]string{"Name", "FileCount", "SystemIDs"}).
				AddRow("RPG", 8, "SNES,Genesis"),
		)

	dirs, err := sqlBrowseDirectories(context.Background(), db, database.BrowseDirectoriesOptions{
		PathPrefix: "/roms/shared/",
		Systems: []systemdefs.System{
			{ID: "SNES"},
			{ID: "Genesis"},
		},
	})
	require.NoError(t, err)

	require.Len(t, dirs, 1)
	assert.Equal(t, "RPG", dirs[0].Name)
	assert.Equal(t, 8, dirs[0].FileCount)
	assert.Equal(t, []string{"SNES", "Genesis"}, dirs[0].SystemIDs)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseDirectories_WithSystemsReadsFromMediaWhenCacheEmpty(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`(?s)SELECT b.Name, SUM\(b.FileCount\), GROUP_CONCAT\(DISTINCT s.SystemID\).*BrowseSystemCache`).
		WithArgs("/roms/shared/", "SNES").
		WillReturnRows(sqlmock.NewRows([]string{"Name", "FileCount", "SystemIDs"}))
	mock.ExpectQuery(`(?s)WITH matched AS .*FROM Media m.*m.Path LIKE \? \|\| '%'`).
		WithArgs("/roms/shared/", "/roms/shared/", "SNES").
		WillReturnRows(
			sqlmock.NewRows([]string{"Name", "FileCount", "SystemIDs"}).
				AddRow("RPG", 2, "SNES"),
		)

	dirs, err := sqlBrowseDirectories(context.Background(), db, database.BrowseDirectoriesOptions{
		PathPrefix: "/roms/shared/",
		Systems:    []systemdefs.System{{ID: "SNES"}},
	})
	require.NoError(t, err)

	require.Len(t, dirs, 1)
	assert.Equal(t, "RPG", dirs[0].Name)
	assert.Equal(t, 2, dirs[0].FileCount)
	assert.Equal(t, []string{"SNES"}, dirs[0].SystemIDs)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseRouteCounts_ReturnsOnlyPopulatedRoutes(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(
		`(?s)SELECT b.DirPath, SUM\(b.FileCount\), GROUP_CONCAT\(DISTINCT s.SystemID\).*BrowseSystemCache`,
	).
		WithArgs("/roms/SNES/", "/roms/shared/", "SNES").
		WillReturnRows(
			sqlmock.NewRows([]string{"DirPath", "FileCount", "SystemIDs"}).
				AddRow("/roms/SNES/", 12, "SNES"),
		)
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\), GROUP_CONCAT\(DISTINCT s.SystemID\).*FROM Media m`).
		WithArgs("/roms/shared/", "SNES").
		WillReturnRows(sqlmock.NewRows([]string{"FileCount", "SystemIDs"}).AddRow(0, nil))

	counts, err := sqlBrowseRouteCounts(context.Background(), db, database.BrowseRouteCountsOptions{
		Routes:  []string{"/roms/SNES", "/roms/shared"},
		Systems: []systemdefs.System{{ID: "SNES"}},
	})
	require.NoError(t, err)

	require.Contains(t, counts, "/roms/SNES")
	assert.Equal(t, "/roms/SNES", counts["/roms/SNES"].Path)
	assert.Equal(t, 12, counts["/roms/SNES"].FileCount)
	assert.Equal(t, []string{"SNES"}, counts["/roms/SNES"].SystemIDs)
	assert.NotContains(t, counts, "/roms/shared")

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseRouteCounts_ReadsFromMediaWhenSystemCacheEmpty(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(
		`(?s)SELECT b.DirPath, SUM\(b.FileCount\), GROUP_CONCAT\(DISTINCT s.SystemID\).*BrowseSystemCache`,
	).
		WithArgs("/roms/SNES/", "SNES").
		WillReturnRows(sqlmock.NewRows([]string{"DirPath", "FileCount", "SystemIDs"}))
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\), GROUP_CONCAT\(DISTINCT s.SystemID\).*FROM Media m`).
		WithArgs("/roms/SNES/", "SNES").
		WillReturnRows(sqlmock.NewRows([]string{"FileCount", "SystemIDs"}).AddRow(3, "SNES"))

	counts, err := sqlBrowseRouteCounts(context.Background(), db, database.BrowseRouteCountsOptions{
		Routes:  []string{"/roms/SNES"},
		Systems: []systemdefs.System{{ID: "SNES"}},
	})
	require.NoError(t, err)

	require.Contains(t, counts, "/roms/SNES")
	assert.Equal(t, 3, counts["/roms/SNES"].FileCount)
	assert.Equal(t, []string{"SNES"}, counts["/roms/SNES"].SystemIDs)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseFiles_BasicQuery(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// SELECT s.SystemID, mt.Name, m.Path, m.DBID
	mock.ExpectQuery(`SELECT .+ FROM Media m`).
		WillReturnRows(
			sqlmock.NewRows([]string{"SystemID", "Name", "Path", "DBID"}).
				AddRow("snes", "Super Mario World", "/roms/SNES/smw.sfc", 1).
				AddRow("snes", "Zelda", "/roms/SNES/zelda.sfc", 2),
		)

	// fetchAndAttachTags uses Prepare + Query
	mock.ExpectPrepare(`SELECT MediaDBID.*Tag.*Type FROM`).
		ExpectQuery().
		WithArgs(int64(1), int64(2), int64(1), int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}))

	results, err := sqlBrowseFiles(context.Background(), db, &database.BrowseFilesOptions{
		PathPrefix: "/roms/SNES/",
		Limit:      10,
	})
	require.NoError(t, err)

	require.Len(t, results, 2)
	assert.Equal(t, "Super Mario World", results[0].Name)
	assert.Equal(t, "Zelda", results[1].Name)
	assert.Equal(t, int64(1), results[0].MediaID)
	assert.Equal(t, int64(2), results[1].MediaID)

	require.NoError(t, mock.ExpectationsWereMet())
}
