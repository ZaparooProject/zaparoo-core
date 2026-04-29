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

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func browseTestPrefix(path string) string {
	return filepath.ToSlash(path) + "/"
}

func browseTestAbsPath(parts ...string) string {
	return filepath.Join(append([]string{string(filepath.Separator)}, parts...)...)
}

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

func TestSqlBrowseVirtualSchemes_WithSystemsMergesPartialCache(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(
		`(?s)SELECT b.DirPath, SUM\(b.FileCount\), GROUP_CONCAT\(DISTINCT s.SystemID\).*BrowseSystemCache`,
	).
		WithArgs("Steam", "DOS").
		WillReturnRows(
			sqlmock.NewRows([]string{"DirPath", "FileCount", "SystemIDs"}).
				AddRow("steam://", 2, "Steam"),
		)
	mock.ExpectQuery(`(?s)SELECT substr\(m.Path, 1, instr\(m.Path, '://'\) \+ 2\).*FROM Media m`).
		WithArgs("DOS").
		WillReturnRows(
			sqlmock.NewRows([]string{"Scheme", "FileCount", "SystemIDs"}).
				AddRow("gog://", 4, "DOS").
				AddRow("steam://", 3, "DOS"),
		)

	schemes, err := sqlBrowseVirtualSchemes(context.Background(), db, database.BrowseVirtualSchemesOptions{
		Systems: []systemdefs.System{{ID: "Steam"}, {ID: "DOS"}},
	})
	require.NoError(t, err)

	require.Len(t, schemes, 2)
	assert.Equal(t, "gog://", schemes[0].Scheme)
	assert.Equal(t, 4, schemes[0].FileCount)
	assert.Equal(t, []string{"DOS"}, schemes[0].SystemIDs)
	assert.Equal(t, "steam://", schemes[1].Scheme)
	assert.Equal(t, 5, schemes[1].FileCount)
	assert.Equal(t, []string{"DOS", "Steam"}, schemes[1].SystemIDs)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseVirtualSchemes_WithSystemsReadsFromMediaWhenCacheEmpty(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(
		`(?s)SELECT b.DirPath, SUM\(b.FileCount\), GROUP_CONCAT\(DISTINCT s.SystemID\).*BrowseSystemCache`,
	).
		WithArgs("Steam").
		WillReturnRows(sqlmock.NewRows([]string{"DirPath", "FileCount", "SystemIDs"}))
	mock.ExpectQuery(`(?s)SELECT substr\(m.Path, 1, instr\(m.Path, '://'\) \+ 2\).*FROM Media m`).
		WithArgs("Steam").
		WillReturnRows(
			sqlmock.NewRows([]string{"Scheme", "FileCount", "SystemIDs"}).
				AddRow("steam://", 7, "Steam"),
		)

	schemes, err := sqlBrowseVirtualSchemes(context.Background(), db, database.BrowseVirtualSchemesOptions{
		Systems: []systemdefs.System{{ID: "Steam"}},
	})
	require.NoError(t, err)

	require.Len(t, schemes, 1)
	assert.Equal(t, "steam://", schemes[0].Scheme)
	assert.Equal(t, 7, schemes[0].FileCount)
	assert.Equal(t, []string{"Steam"}, schemes[0].SystemIDs)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseDirectories_WithSystemsUsesSystemCache(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	sharedPrefix := browseTestPrefix(browseTestAbsPath("roms", "shared"))

	mock.ExpectQuery(`(?s)SELECT b.Name, SUM\(b.FileCount\), GROUP_CONCAT\(DISTINCT s.SystemID\).*BrowseSystemCache`).
		WithArgs(sharedPrefix, "SNES", "Genesis").
		WillReturnRows(
			sqlmock.NewRows([]string{"Name", "FileCount", "SystemIDs"}).
				AddRow("RPG", 8, "SNES,Genesis"),
		)

	dirs, err := sqlBrowseDirectories(context.Background(), db, database.BrowseDirectoriesOptions{
		PathPrefix: sharedPrefix,
		Systems: []systemdefs.System{
			{ID: "SNES"},
			{ID: "Genesis"},
		},
	})
	require.NoError(t, err)

	require.Len(t, dirs, 1)
	assert.Equal(t, "RPG", dirs[0].Name)
	assert.Equal(t, 8, dirs[0].FileCount)
	assert.Equal(t, []string{"Genesis", "SNES"}, dirs[0].SystemIDs)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseDirectories_WithSystemsReadsFromMediaWhenCacheEmpty(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	sharedPrefix := browseTestPrefix(browseTestAbsPath("roms", "shared"))

	mock.ExpectQuery(`(?s)SELECT b.Name, SUM\(b.FileCount\), GROUP_CONCAT\(DISTINCT s.SystemID\).*BrowseSystemCache`).
		WithArgs(sharedPrefix, "SNES").
		WillReturnRows(sqlmock.NewRows([]string{"Name", "FileCount", "SystemIDs"}))
	mock.ExpectQuery(`(?s)WITH matched AS .*FROM Media m.*m.Path LIKE \? \|\| '%'`).
		WithArgs(sharedPrefix, sharedPrefix, "SNES").
		WillReturnRows(
			sqlmock.NewRows([]string{"Name", "FileCount", "SystemIDs"}).
				AddRow("RPG", 2, "SNES"),
		)

	dirs, err := sqlBrowseDirectories(context.Background(), db, database.BrowseDirectoriesOptions{
		PathPrefix: sharedPrefix,
		Systems:    []systemdefs.System{{ID: "SNES"}},
	})
	require.NoError(t, err)

	require.Len(t, dirs, 1)
	assert.Equal(t, "RPG", dirs[0].Name)
	assert.Equal(t, 2, dirs[0].FileCount)
	assert.Equal(t, []string{"SNES"}, dirs[0].SystemIDs)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseDirectories_WithSystemsMergesPartialCache(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	sharedPrefix := browseTestPrefix(browseTestAbsPath("roms", "shared"))

	mock.ExpectQuery(`(?s)SELECT b.Name, SUM\(b.FileCount\), GROUP_CONCAT\(DISTINCT s.SystemID\).*BrowseSystemCache`).
		WithArgs(sharedPrefix, "SNES", "Genesis").
		WillReturnRows(
			sqlmock.NewRows([]string{"Name", "FileCount", "SystemIDs"}).
				AddRow("RPG", 2, "SNES"),
		)
	mock.ExpectQuery(`(?s)WITH matched AS .*FROM Media m.*m.Path LIKE \? \|\| '%'`).
		WithArgs(sharedPrefix, sharedPrefix, "Genesis").
		WillReturnRows(
			sqlmock.NewRows([]string{"Name", "FileCount", "SystemIDs"}).
				AddRow("RPG", 3, "Genesis").
				AddRow("Shooter", 1, "Genesis"),
		)

	dirs, err := sqlBrowseDirectories(context.Background(), db, database.BrowseDirectoriesOptions{
		PathPrefix: sharedPrefix,
		Systems: []systemdefs.System{
			{ID: "SNES"},
			{ID: "Genesis"},
		},
	})
	require.NoError(t, err)

	require.Len(t, dirs, 2)
	assert.Equal(t, "RPG", dirs[0].Name)
	assert.Equal(t, 5, dirs[0].FileCount)
	assert.Equal(t, []string{"Genesis", "SNES"}, dirs[0].SystemIDs)
	assert.Equal(t, "Shooter", dirs[1].Name)
	assert.Equal(t, 1, dirs[1].FileCount)
	assert.Equal(t, []string{"Genesis"}, dirs[1].SystemIDs)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseRouteCounts_ReturnsOnlyPopulatedRoutes(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	snesRoute := filepath.ToSlash(browseTestAbsPath("roms", "SNES"))
	sharedRoute := filepath.ToSlash(browseTestAbsPath("roms", "shared"))
	snesPrefix := browseTestPrefix(browseTestAbsPath("roms", "SNES"))
	sharedPrefix := browseTestPrefix(browseTestAbsPath("roms", "shared"))

	mock.ExpectQuery(
		`(?s)SELECT b.DirPath, SUM\(b.FileCount\), GROUP_CONCAT\(DISTINCT s.SystemID\).*BrowseSystemCache`,
	).
		WithArgs(snesPrefix, sharedPrefix, "SNES").
		WillReturnRows(
			sqlmock.NewRows([]string{"DirPath", "FileCount", "SystemIDs"}).
				AddRow(snesPrefix, 12, "SNES"),
		)
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\), GROUP_CONCAT\(DISTINCT s.SystemID\).*FROM Media m`).
		WithArgs(sharedPrefix, "SNES").
		WillReturnRows(sqlmock.NewRows([]string{"FileCount", "SystemIDs"}).AddRow(0, nil))

	counts, err := sqlBrowseRouteCounts(context.Background(), db, database.BrowseRouteCountsOptions{
		Routes:  []string{snesRoute, sharedRoute},
		Systems: []systemdefs.System{{ID: "SNES"}},
	})
	require.NoError(t, err)

	require.Contains(t, counts, snesRoute)
	assert.Equal(t, snesRoute, counts[snesRoute].Path)
	assert.Equal(t, 12, counts[snesRoute].FileCount)
	assert.Equal(t, []string{"SNES"}, counts[snesRoute].SystemIDs)
	assert.NotContains(t, counts, sharedRoute)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseRouteCounts_MergesPartialCache(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	sharedRoute := filepath.ToSlash(browseTestAbsPath("roms", "shared"))
	sharedPrefix := browseTestPrefix(browseTestAbsPath("roms", "shared"))

	mock.ExpectQuery(
		`(?s)SELECT b.DirPath, SUM\(b.FileCount\), GROUP_CONCAT\(DISTINCT s.SystemID\).*BrowseSystemCache`,
	).
		WithArgs(sharedPrefix, "SNES", "Genesis").
		WillReturnRows(
			sqlmock.NewRows([]string{"DirPath", "FileCount", "SystemIDs"}).
				AddRow(sharedPrefix, 2, "SNES"),
		)
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\), GROUP_CONCAT\(DISTINCT s.SystemID\).*FROM Media m`).
		WithArgs(sharedPrefix, "Genesis").
		WillReturnRows(sqlmock.NewRows([]string{"FileCount", "SystemIDs"}).AddRow(3, "Genesis"))

	counts, err := sqlBrowseRouteCounts(context.Background(), db, database.BrowseRouteCountsOptions{
		Routes: []string{sharedRoute},
		Systems: []systemdefs.System{
			{ID: "SNES"},
			{ID: "Genesis"},
		},
	})
	require.NoError(t, err)

	require.Contains(t, counts, sharedRoute)
	assert.Equal(t, 5, counts[sharedRoute].FileCount)
	assert.Equal(t, []string{"Genesis", "SNES"}, counts[sharedRoute].SystemIDs)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseRouteCounts_ReadsFromMediaWhenSystemCacheEmpty(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	snesRoute := filepath.ToSlash(browseTestAbsPath("roms", "SNES"))
	snesPrefix := browseTestPrefix(browseTestAbsPath("roms", "SNES"))

	mock.ExpectQuery(
		`(?s)SELECT b.DirPath, SUM\(b.FileCount\), GROUP_CONCAT\(DISTINCT s.SystemID\).*BrowseSystemCache`,
	).
		WithArgs(snesPrefix, "SNES").
		WillReturnRows(sqlmock.NewRows([]string{"DirPath", "FileCount", "SystemIDs"}))
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\), GROUP_CONCAT\(DISTINCT s.SystemID\).*FROM Media m`).
		WithArgs(snesPrefix, "SNES").
		WillReturnRows(sqlmock.NewRows([]string{"FileCount", "SystemIDs"}).AddRow(3, "SNES"))

	counts, err := sqlBrowseRouteCounts(context.Background(), db, database.BrowseRouteCountsOptions{
		Routes:  []string{snesRoute},
		Systems: []systemdefs.System{{ID: "SNES"}},
	})
	require.NoError(t, err)

	require.Contains(t, counts, snesRoute)
	assert.Equal(t, 3, counts[snesRoute].FileCount)
	assert.Equal(t, []string{"SNES"}, counts[snesRoute].SystemIDs)

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
