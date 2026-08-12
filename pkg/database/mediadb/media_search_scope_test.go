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
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediaDB_SearchMediaWithFilters_ScopesCachedVariantsByMediaType(t *testing.T) {
	t.Parallel()

	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	nes, err := systemdefs.GetSystem(systemdefs.SystemNES)
	require.NoError(t, err)
	movie, err := systemdefs.GetSystem(systemdefs.SystemMovie)
	require.NoError(t, err)

	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{slug: "rtype", titleDBID: 10, systemDBID: 1},
		{slug: "mariokart", titleDBID: 11, systemDBID: 1},
		{slug: "r", titleDBID: 20, systemDBID: 2},
	}, map[int64]string{1: nes.ID, 2: movie.ID})
	cache.complete = true

	mediaDB := &MediaDB{}
	mediaDB.sql.Store(db)
	mediaDB.slugSearchCache.Store(cache)

	nesPath := filepath.Join("games", "nes", "rtype.nes")
	moviePath := filepath.Join("movies", "rtype.mkv")
	mock.ExpectQuery("SELECT .+ FROM MediaTitles").
		WithArgs(int64(10), int64(20), 10).
		WillReturnRows(sqlmock.NewRows([]string{
			"SystemID", "Name", "Path", "DBID", "MediaTitleDBID", "DisambiguationTypes",
		}).
			AddRow(nes.ID, "R-Type", nesPath, int64(100), int64(10), "").
			AddRow(movie.ID, "R-Type", moviePath, int64(200), int64(20), ""))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(100, 200, 10, 20).
		WillReturnRows(sqlmock.NewRows([]string{"hasTags"}).AddRow(false))

	results, err := mediaDB.SearchMediaWithFilters(context.Background(), &database.SearchFilters{
		Systems: []systemdefs.System{*nes, *movie},
		Query:   "R-Type",
		Limit:   10,
	})

	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, nesPath, results[0].Path)
	assert.Equal(t, moviePath, results[1].Path)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSearchMediaTypeGroupsInCache_SkipsMissingSystemGroup(t *testing.T) {
	t.Parallel()

	nes, err := systemdefs.GetSystem(systemdefs.SystemNES)
	require.NoError(t, err)
	movie, err := systemdefs.GetSystem(systemdefs.SystemMovie)
	require.NoError(t, err)

	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{slug: "rtype", titleDBID: 10, systemDBID: 1},
		{slug: "mariokart", titleDBID: 11, systemDBID: 1},
	}, map[int64]string{1: nes.ID})
	cache.complete = true

	groups := buildMediaSearchTypeGroups([]systemdefs.System{*nes, *movie}, []string{"R-Type"})
	candidates := searchMediaTypeGroupsInCache(cache, groups)

	assert.Equal(t, []int64{10}, candidates)
}

func TestMediaDB_SearchMediaWithFilters_ScopesSQLVariantsByMediaType(t *testing.T) {
	t.Parallel()

	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	nes, err := systemdefs.GetSystem(systemdefs.SystemNES)
	require.NoError(t, err)
	movie, err := systemdefs.GetSystem(systemdefs.SystemMovie)
	require.NoError(t, err)

	mediaDB := &MediaDB{}
	mediaDB.sql.Store(db)

	nesPath := filepath.Join("games", "nes", "rtype.nes")
	moviePath := filepath.Join("movies", "rtype.mkv")
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(nes.ID, "%rtype%", "%rtype%", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"SystemID", "Name", "Path", "DBID", "MediaTitleDBID", "DisambiguationTypes",
		}).AddRow(nes.ID, "R-Type", nesPath, int64(300), int64(30), ""))
	expectSearchTagsQuery(mock, 300, 30)
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(movie.ID, "%r%", "%r%", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"SystemID", "Name", "Path", "DBID", "MediaTitleDBID", "DisambiguationTypes",
		}).AddRow(movie.ID, "R-Type", moviePath, int64(200), int64(20), ""))
	expectSearchTagsQuery(mock, 200, 20)

	results, err := mediaDB.SearchMediaWithFilters(context.Background(), &database.SearchFilters{
		Systems: []systemdefs.System{*nes, *movie},
		Query:   "R-Type",
		Limit:   1,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, moviePath, results[0].Path)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMediaDB_SearchMediaWithFilters_FallsBackBeforeSQLiteVariableLimit(t *testing.T) {
	t.Parallel()

	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	nes, err := systemdefs.GetSystem(systemdefs.SystemNES)
	require.NoError(t, err)

	entries := make([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}, sqliteMaxParams)
	for i := range entries {
		entries[i] = struct {
			slug       string
			secSlug    string
			titleDBID  int64
			systemDBID int64
		}{slug: "rtype", titleDBID: int64(i + 1), systemDBID: 1}
	}
	cache := buildTestCache(entries, map[int64]string{1: nes.ID})
	cache.complete = true

	mediaDB := &MediaDB{}
	mediaDB.sql.Store(db)
	mediaDB.slugSearchCache.Store(cache)

	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(nes.ID, "%rtype%", "%rtype%", 10).
		WillReturnRows(sqlmock.NewRows([]string{
			"SystemID", "Name", "Path", "DBID", "DisambiguationTypes",
		}))

	results, err := mediaDB.SearchMediaWithFilters(context.Background(), &database.SearchFilters{
		Systems: []systemdefs.System{*nes},
		Query:   "R-Type",
		Limit:   10,
	})

	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}
