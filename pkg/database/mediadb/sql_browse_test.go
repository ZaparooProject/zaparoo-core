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

	schemes, err := sqlBrowseVirtualSchemes(context.Background(), db)
	require.NoError(t, err)

	require.Len(t, schemes, 2)
	assert.Equal(t, "steam://", schemes[0].Scheme)
	assert.Equal(t, 150, schemes[0].FileCount)
	assert.Equal(t, "igdb://", schemes[1].Scheme)
	assert.Equal(t, 30, schemes[1].FileCount)

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
