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
	"errors"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
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

func TestLogBrowseMediaCountsBySystem_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	mock.ExpectQuery("SELECT s.SystemID").WillReturnRows(sqlmock.NewRows(
		[]string{"SystemID", "TotalMedia", "CurrentMedia", "MissingMedia"},
	).AddRow("NES", 10, 8, 2))
	mock.ExpectRollback()

	logBrowseMediaCountsBySystem(context.Background(), tx)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLogBrowseMediaCountsBySystem_QueryError(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	mock.ExpectQuery("SELECT s.SystemID").WillReturnError(errors.New("query failed"))
	mock.ExpectRollback()

	logBrowseMediaCountsBySystem(context.Background(), tx)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLogBrowseMediaCountsBySystem_ScanError(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	mock.ExpectQuery("SELECT s.SystemID").WillReturnRows(sqlmock.NewRows(
		[]string{"SystemID", "TotalMedia", "CurrentMedia", "MissingMedia"},
	).AddRow("NES", "not-an-int", 8, 2))
	mock.ExpectRollback()

	logBrowseMediaCountsBySystem(context.Background(), tx)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLogBrowseMediaCountsBySystem_RowsError(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	mock.ExpectQuery("SELECT s.SystemID").WillReturnRows(sqlmock.NewRows(
		[]string{"SystemID", "TotalMedia", "CurrentMedia", "MissingMedia"},
	).AddRow("NES", 10, 8, 2).RowError(0, errors.New("rows failed")))
	mock.ExpectRollback()

	logBrowseMediaCountsBySystem(context.Background(), tx)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
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
		WithArgs(romsDir, romsDir, stringPrefixUpperBound(romsDir)).
		WillReturnRows(sqlmock.NewRows([]string{"Name", "FileCount"}).AddRow("SNES", 2))

	results, err := sqlBrowseDirectories(context.Background(), db, database.BrowseDirectoriesOptions{
		PathPrefix: romsDir,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "SNES", results[0].Name)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseDirectories_ReturnsEmptyWhenReadyCacheParentHasNoChildren(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectBrowseCacheReady(mock)
	psxDir := browseTestDir("media", "fat", "games", "PSX")
	mock.ExpectQuery("SELECT DBID FROM BrowseDirs WHERE Path = ").
		WithArgs(psxDir).
		WillReturnRows(sqlmock.NewRows([]string{"DBID"}).AddRow(10))
	mock.ExpectQuery("SELECT d.Name, c.FileCount").
		WithArgs(int64(10), "PSX").
		WillReturnRows(sqlmock.NewRows([]string{"Name", "FileCount"}))

	results, err := sqlBrowseDirectories(context.Background(), db, database.BrowseDirectoriesOptions{
		PathPrefix: psxDir,
		Systems:    []systemdefs.System{{ID: "PSX"}},
	})
	require.NoError(t, err)
	assert.Empty(t, results)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlBrowseDirectories_FallsBackWhenReadyCacheParentMissing(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectBrowseCacheReady(mock)
	psxDir := browseTestDir("media", "fat", "games", "PSX")
	mock.ExpectQuery("SELECT DBID FROM BrowseDirs WHERE Path = ").
		WithArgs(psxDir).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("WITH matched AS").
		WithArgs(psxDir, psxDir, stringPrefixUpperBound(psxDir), "PSX").
		WillReturnRows(sqlmock.NewRows([]string{"Name", "FileCount", "SystemIDs"}).AddRow("USA", 273, "PSX"))

	results, err := sqlBrowseDirectories(context.Background(), db, database.BrowseDirectoriesOptions{
		PathPrefix: psxDir,
		Systems:    []systemdefs.System{{ID: "PSX"}},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "USA", results[0].Name)
	assert.Equal(t, 273, results[0].FileCount)
	assert.Equal(t, []string{"PSX"}, results[0].SystemIDs)
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

func TestBrowseCacheBuilder_AttachesVirtualSchemeToRoot(t *testing.T) {
	t.Parallel()

	builder := newBrowseCacheBuilder()
	builder.ensureDir("/")
	builder.addMedia(2, "smb://server/share/game.rom")

	root := builder.dirs["/"]
	scheme := builder.dirs["smb://"]
	require.NotNil(t, root)
	require.NotNil(t, scheme)
	assert.Equal(t, root.id, *scheme.parentID)
	assert.Equal(t, 1, builder.counts[browseCacheCountKey{
		parentDirID: root.id,
		childDirID:  scheme.id,
		systemDBID:  2,
	}])
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

func TestFetchAndAttachUtilityTags_EmptyResults(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{}
	err = fetchAndAttachUtilityTags(context.Background(), db, results)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachUtilityTags_TagTypeAbsent(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{MediaID: 1, Name: "Game"},
	}

	// One sqlFindTagType prepare+query per entry in UtilityTags; empty rows =
	// ErrNoRows = skip. Loop over all entries so the test stays valid if the list grows.
	for _, ct := range tags.UtilityTags {
		mock.ExpectPrepare(`select.*DBID.*Type.*IsExclusive.*from TagTypes`).
			ExpectQuery().
			WithArgs(int64(0), string(ct.Type)).
			WillReturnRows(sqlmock.NewRows([]string{"DBID", "Type", "IsExclusive"}))
	}

	err = fetchAndAttachUtilityTags(context.Background(), db, results)
	require.NoError(t, err)
	assert.Empty(t, results[0].Tags)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachUtilityTags_NoFavorites(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{MediaID: 10, Name: "Game A"},
		{MediaID: 11, Name: "Game B"},
	}

	mock.ExpectPrepare(`select.*DBID.*Type.*IsExclusive.*from TagTypes`).
		ExpectQuery().
		WithArgs(int64(0), "user").
		WillReturnRows(sqlmock.NewRows([]string{"DBID", "Type", "IsExclusive"}).AddRow(int64(5), "user", false))

	tagRows := sqlmock.NewRows([]string{"DBID", "TypeDBID", "Tag", "DisplayName"}).
		AddRow(int64(42), int64(5), "favorite", "")
	mock.ExpectPrepare(`select.*DBID.*TypeDBID.*Tag.*DisplayName.*from Tags`).
		ExpectQuery().
		WillReturnRows(tagRows)

	// MediaTags query returns no rows — neither entry has any utility tag.
	mock.ExpectQuery(`SELECT mt\.MediaDBID, mt\.TagDBID FROM MediaTags`).
		WithArgs(int64(10), int64(11), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"MediaDBID", "TagDBID"}))

	err = fetchAndAttachUtilityTags(context.Background(), db, results)
	require.NoError(t, err)
	assert.Empty(t, results[0].Tags)
	assert.Empty(t, results[1].Tags)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachUtilityTags_WithFavorites(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{MediaID: 20, Name: "Favorited Game"},
		{MediaID: 21, Name: "Regular Game"},
	}

	mock.ExpectPrepare(`select.*DBID.*Type.*IsExclusive.*from TagTypes`).
		ExpectQuery().
		WithArgs(int64(0), "user").
		WillReturnRows(sqlmock.NewRows([]string{"DBID", "Type", "IsExclusive"}).AddRow(int64(5), "user", false))

	tagRows := sqlmock.NewRows([]string{"DBID", "TypeDBID", "Tag", "DisplayName"}).
		AddRow(int64(42), int64(5), "favorite", "")
	mock.ExpectPrepare(`select.*DBID.*TypeDBID.*Tag.*DisplayName.*from Tags`).
		ExpectQuery().
		WillReturnRows(tagRows)

	// Only media ID 20 has the favorite utility tag.
	mock.ExpectQuery(`SELECT mt\.MediaDBID, mt\.TagDBID FROM MediaTags`).
		WithArgs(int64(20), int64(21), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"MediaDBID", "TagDBID"}).AddRow(int64(20), int64(42)))

	err = fetchAndAttachUtilityTags(context.Background(), db, results)
	require.NoError(t, err)
	require.Len(t, results[0].Tags, 1, "favorited entry should have one tag")
	assert.Equal(t, "favorite", results[0].Tags[0].Tag)
	assert.Equal(t, "user", results[0].Tags[0].Type)
	assert.Empty(t, results[1].Tags, "non-favorited entry should have no tags")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachUtilityTags_TagTypeRealDBError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{MediaID: 1, Name: "Game"},
	}

	// Simulate a real DB error (not ErrNoRows) on the tag type lookup.
	mock.ExpectPrepare(`select.*DBID.*Type.*IsExclusive.*from TagTypes`).
		ExpectQuery().
		WithArgs(int64(0), string(tags.UtilityTags[0].Type)).
		WillReturnError(sql.ErrConnDone)

	err = fetchAndAttachUtilityTags(context.Background(), db, results)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browse utility tags")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachUtilityTags_TagRealDBError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{MediaID: 1, Name: "Game"},
	}

	// Tag type found; then a real DB error on the tag value lookup.
	mock.ExpectPrepare(`select.*DBID.*Type.*IsExclusive.*from TagTypes`).
		ExpectQuery().
		WithArgs(int64(0), string(tags.UtilityTags[0].Type)).
		WillReturnRows(sqlmock.NewRows([]string{"DBID", "Type", "IsExclusive"}).AddRow(int64(5), "user", false))
	mock.ExpectPrepare(`select.*DBID.*TypeDBID.*Tag.*DisplayName.*from Tags`).
		ExpectQuery().
		WillReturnError(sql.ErrConnDone)

	err = fetchAndAttachUtilityTags(context.Background(), db, results)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browse utility tags")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachCoverFlags_EmptyResults(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{}
	err = fetchAndAttachCoverFlags(context.Background(), db, results)
	assert.NoError(t, err)
	// No DB operations should occur for empty input.
	assert.NoError(t, mock.ExpectationsWereMet())
}

func expectImagePropertyTagLookup(mock sqlmock.Sqlmock, ids ...int64) {
	rows := sqlmock.NewRows([]string{"DBID"})
	for _, id := range ids {
		rows.AddRow(id)
	}
	mock.ExpectQuery(`SELECT t\.DBID\s+FROM Tags t\s+JOIN TagTypes tt ON tt\.DBID = t\.TypeDBID\s+WHERE tt\.Type = \? AND t\.Tag LIKE \?\s+ORDER BY t\.DBID`).
		WithArgs(string(tags.TagTypeProperty), imagePropertyValuePrefix+"%").
		WillReturnRows(rows)
}

func TestFetchAndAttachCoverFlags_NoCoverEntries(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{MediaID: 1, Name: "NoCoverGame"},
		{MediaID: 2, Name: "AnotherNoCoverGame"},
	}

	// Query returns no rows — neither media ID has any image property.
	expectImagePropertyTagLookup(mock, 901)
	mock.ExpectQuery(`SELECT 'media' AS Scope, mp\.MediaDBID AS ID`).
		WithArgs(int64(1), int64(2), int64(901)).
		WillReturnRows(sqlmock.NewRows([]string{"Scope", "ID"}))

	err = fetchAndAttachCoverFlags(context.Background(), db, results)
	require.NoError(t, err)
	assert.False(t, results[0].HasCover, "entry with no image property should have HasCover=false")
	assert.False(t, results[1].HasCover, "entry with no image property should have HasCover=false")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachCoverFlags_MediaLevelCover(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{MediaID: 10, Name: "GameWithCover"},
		{MediaID: 20, Name: "GameWithoutCover"},
	}

	// Query returns mediaID 10 as having a cover (media-level property).
	expectImagePropertyTagLookup(mock, 901)
	mock.ExpectQuery(`SELECT 'media' AS Scope, mp\.MediaDBID AS ID`).
		WithArgs(int64(10), int64(20), int64(901)).
		WillReturnRows(sqlmock.NewRows([]string{"Scope", "ID"}).AddRow("media", int64(10)))

	err = fetchAndAttachCoverFlags(context.Background(), db, results)
	require.NoError(t, err)
	assert.True(t, results[0].HasCover, "entry with image property should have HasCover=true")
	assert.False(t, results[1].HasCover, "entry without image property should have HasCover=false")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachCoverFlags_TitleLevelCover(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Both media share a title; the title-level cover property means both
	// should be marked as having a cover.
	results := []database.SearchResultWithCursor{
		{MediaID: 30, MediaTitleID: 100, Name: "GameA"},
		{MediaID: 31, MediaTitleID: 100, Name: "GameA (Rev B)"},
	}

	// Query returns the shared title ID from the title-level UNION ALL leg.
	expectImagePropertyTagLookup(mock, 901)
	mock.ExpectQuery(`SELECT 'media' AS Scope, mp\.MediaDBID AS ID`).
		WithArgs(int64(30), int64(31), int64(901), int64(100), int64(901)).
		WillReturnRows(sqlmock.NewRows([]string{"Scope", "ID"}).AddRow("title", int64(100)))

	err = fetchAndAttachCoverFlags(context.Background(), db, results)
	require.NoError(t, err)
	assert.True(t, results[0].HasCover)
	assert.True(t, results[1].HasCover)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchAndAttachCoverFlags_QueryError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	results := []database.SearchResultWithCursor{
		{MediaID: 5, Name: "SomeGame"},
	}

	expectImagePropertyTagLookup(mock, 901)
	mock.ExpectQuery(`SELECT 'media' AS Scope, mp\.MediaDBID AS ID`).
		WithArgs(int64(5), int64(901)).
		WillReturnError(errors.New("db unavailable"))

	err = fetchAndAttachCoverFlags(context.Background(), db, results)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browse cover flags query")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Integration tests use a real SQLite DB to verify the query joins correctly
// against the canonical tag schema. These catch schema/prefix mismatches that
// pure sqlmock tests cannot — sqlmock never executes real SQL.

// seedImagePropertyTags inserts the minimal TagTypes/Tags rows needed to call
// UpsertMediaProperties/UpsertMediaTitleProperties in a bare setupTempMediaDB.
// A full index run would seed all canonical tags; these tests need only the
// property type and image-boxart tag.
func seedImagePropertyTags(t *testing.T, mediaDB *MediaDB) {
	t.Helper()
	ctx := context.Background()
	_, err := mediaDB.sql.ExecContext(ctx, `
		INSERT OR IGNORE INTO TagTypes (DBID, Type, IsExclusive) VALUES (900, 'property', 0);
		INSERT OR IGNORE INTO Tags (DBID, TypeDBID, Tag) VALUES (901, 900, 'image-boxart');
	`)
	require.NoError(t, err)
}

func TestFetchAndAttachCoverFlags_Integration_MediaLevelProperty(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	seedImagePropertyTags(t, mediaDB)

	ctx := context.Background()

	sys, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	// Insert two media rows: one will get an image property, one will not.
	require.NoError(t, mediaDB.BeginTransaction(false))
	titleA, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: sys.DBID,
		Slug:       slugs.Slugify(nesSystem.GetMediaType(), "Game With Cover"),
		Name:       "Game With Cover",
	})
	require.NoError(t, err)
	mediaA, err := mediaDB.InsertMedia(database.Media{
		SystemDBID:     sys.DBID,
		MediaTitleDBID: titleA.DBID,
		Path:           filepath.Join("roms", "nes", "with_cover.nes"),
		ParentDir:      filepath.ToSlash(filepath.Join("roms", "nes")) + "/",
	})
	require.NoError(t, err)

	titleB, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: sys.DBID,
		Slug:       slugs.Slugify(nesSystem.GetMediaType(), "Game Without Cover"),
		Name:       "Game Without Cover",
	})
	require.NoError(t, err)
	mediaB, err := mediaDB.InsertMedia(database.Media{
		SystemDBID:     sys.DBID,
		MediaTitleDBID: titleB.DBID,
		Path:           filepath.Join("roms", "nes", "no_cover.nes"),
		ParentDir:      filepath.ToSlash(filepath.Join("roms", "nes")) + "/",
	})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	// Write a media-level image property only for mediaA.
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, mediaA.DBID, []database.MediaProperty{
		{TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart), Text: filepath.Join("art", "with_cover.png")},
	}))

	results := []database.SearchResultWithCursor{
		{MediaID: mediaA.DBID, MediaTitleID: titleA.DBID, Name: "Game With Cover"},
		{MediaID: mediaB.DBID, MediaTitleID: titleB.DBID, Name: "Game Without Cover"},
	}

	require.NoError(t, fetchAndAttachCoverFlags(ctx, mediaDB.sql, results))
	assert.True(t, results[0].HasCover, "media with image property should have HasCover=true")
	assert.False(t, results[1].HasCover, "media without image property should have HasCover=false")
}

func TestFetchAndAttachCoverFlags_Integration_TitleLevelProperty(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	seedImagePropertyTags(t, mediaDB)

	ctx := context.Background()

	sys, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	// Two media sharing one title; cover is on the title, not the media.
	require.NoError(t, mediaDB.BeginTransaction(false))
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: sys.DBID,
		Slug:       slugs.Slugify(nesSystem.GetMediaType(), "Shared Title"),
		Name:       "Shared Title",
	})
	require.NoError(t, err)
	mediaA, err := mediaDB.InsertMedia(database.Media{
		SystemDBID:     sys.DBID,
		MediaTitleDBID: title.DBID,
		Path:           filepath.Join("roms", "nes", "rev_a.nes"),
		ParentDir:      filepath.ToSlash(filepath.Join("roms", "nes")) + "/",
	})
	require.NoError(t, err)
	mediaB, err := mediaDB.InsertMedia(database.Media{
		SystemDBID:     sys.DBID,
		MediaTitleDBID: title.DBID,
		Path:           filepath.Join("roms", "nes", "rev_b.nes"),
		ParentDir:      filepath.ToSlash(filepath.Join("roms", "nes")) + "/",
	})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	// Write the image property at the title level only.
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, title.DBID, []database.MediaProperty{
		{TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart), Text: filepath.Join("art", "shared.png")},
	}))

	results := []database.SearchResultWithCursor{
		{MediaID: mediaA.DBID, MediaTitleID: title.DBID, Name: "Rev A"},
		{MediaID: mediaB.DBID, MediaTitleID: title.DBID, Name: "Rev B"},
	}

	require.NoError(t, fetchAndAttachCoverFlags(ctx, mediaDB.sql, results))
	assert.True(t, results[0].HasCover, "media whose title has an image property should have HasCover=true")
	assert.True(t, results[1].HasCover, "media whose title has an image property should have HasCover=true")
}

// Sibling disambiguation is exercised end-to-end in disambiguation_test.go: it
// now reads stored per-title types (RecomputeSystemDisambiguation) instead of
// grouping a page in memory, so it is correct across page boundaries.

// TestBrowseFiles_SortNameFallback_Integration verifies that a media row with
// SortName=” (pre-migration) gets its display name derived from the file path
// rather than emitting an empty Name field.
func TestBrowseFiles_SortNameFallback_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()
	sys, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	require.NoError(t, mediaDB.BeginTransaction(false))
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: sys.DBID,
		Slug:       slugs.Slugify(nesSystem.GetMediaType(), "Expected Name"),
		Name:       "Expected Name",
	})
	require.NoError(t, err)

	parentDir := browseTestDir("roms", "nes")
	media, err := mediaDB.InsertMedia(database.Media{
		SystemDBID:     sys.DBID,
		MediaTitleDBID: title.DBID,
		Path:           browseTestPath("roms", "nes", "mygame.nes"),
		ParentDir:      parentDir,
		SortName:       "", // intentionally empty — simulates a pre-migration row
	})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	// Blank out SortName directly so the scan loop hits the fallback path.
	_, err = mediaDB.sql.ExecContext(ctx, `UPDATE Media SET SortName = '' WHERE DBID = ?`, media.DBID)
	require.NoError(t, err)

	results, err := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
		PathPrefix: parentDir,
		Limit:      10,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "mygame", results[0].Name,
		"SortName='' should fall back to filename-without-extension")
}
