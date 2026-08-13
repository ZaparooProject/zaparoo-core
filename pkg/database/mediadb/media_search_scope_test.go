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
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestMediaDB_SearchMediaWithFilters_FallsBackWhenScopedStreamIsSparse(t *testing.T) {
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

	mock.ExpectQuery("SELECT MIN\\(DBID\\), MAX\\(DBID\\).*FROM Media").
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"min", "max"}).AddRow(int64(1), int64(20_000)))
	mock.ExpectQuery("SELECT.*MediaTitles\\.Name.*Media\\.Path.*FROM Media NOT INDEXED").
		WithArgs(int64(1), int64(10_000), int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{
			"Name", "Path", "DBID", "DisambiguationTypes", "MediaTitleDBID",
		}))

	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(nes.ID, "%rtype%", "%rtype%", 10).
		WillReturnRows(sqlmock.NewRows([]string{
			"SystemID", "Name", "Path", "DBID", "MediaTitleDBID", "DisambiguationTypes",
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

func TestMediaDB_SearchMediaWithFilters_FallsBackWhenBoundsLookupFails(t *testing.T) {
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
	mock.ExpectQuery("SELECT MIN\\(DBID\\), MAX\\(DBID\\).*FROM Media").
		WithArgs(int64(1)).
		WillReturnError(errors.New("bounds unavailable"))
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(nes.ID, "%rtype%", "%rtype%", 10).
		WillReturnRows(sqlmock.NewRows([]string{
			"SystemID", "Name", "Path", "DBID", "MediaTitleDBID", "DisambiguationTypes",
		}))

	results, err := mediaDB.SearchMediaWithFilters(context.Background(), &database.SearchFilters{
		Systems: []systemdefs.System{*nes}, Query: "R-Type", Limit: 10,
	})
	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestScopedCandidateStream_MatchesGroupedSQL(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	ctx := context.Background()

	tx, err := mediaDB.sql.Load().BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO Systems (DBID, SystemID, Name) VALUES
			(1, 'SNES', 'Super Nintendo'),
			(2, 'NES', 'Nintendo');`)
	require.NoError(t, err)

	titleStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (?, ?, ?, ?)`)
	require.NoError(t, err)
	defer func() { require.NoError(t, titleStmt.Close()) }()
	mediaStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, IsMissing) VALUES (?, ?, ?, ?, ?)`)
	require.NoError(t, err)
	defer func() { require.NoError(t, mediaStmt.Close()) }()

	const rowsPerSystem = 600
	snesCandidates := make(map[string][]int64)
	allCandidates := make(map[string][]int64)
	var firstSNES, lastSNES int64
	for i := 1; i <= rowsPerSystem; i++ {
		var slug string
		switch i % 6 {
		case 0:
			slug = fmt.Sprintf("alpha-super-%03d", i)
		case 1:
			slug = fmt.Sprintf("alpha-%03d", i)
		case 2:
			slug = fmt.Sprintf("super-%03d", i)
		case 3:
			slug = fmt.Sprintf("rare-%03d", i)
		default:
			slug = fmt.Sprintf("plain-%03d", i)
		}

		for systemDBID := int64(1); systemDBID <= 2; systemDBID++ {
			mediaID := int64((i-1)*2) + systemDBID
			titleID := systemDBID*10_000 + int64(i)
			systemID := "SNES"
			if systemDBID == 2 {
				systemID = "NES"
			}
			_, err = titleStmt.ExecContext(ctx, titleID, systemDBID, slug, fmt.Sprintf("%s Game %03d", systemID, i))
			require.NoError(t, err)
			_, err = mediaStmt.ExecContext(
				ctx, mediaID, titleID, systemDBID,
				filepath.Join("roms", strings.ToLower(systemID), fmt.Sprintf("game-%03d.rom", i)),
				i%17 == 0,
			)
			require.NoError(t, err)
			for _, query := range []string{"alpha", "super", "rare", "plain"} {
				if strings.Contains(slug, query) {
					allCandidates[query] = append(allCandidates[query], titleID)
				}
			}
			if systemDBID == 1 {
				if firstSNES == 0 {
					firstSNES = mediaID
				}
				lastSNES = mediaID
				for _, query := range []string{"alpha", "super", "rare", "plain"} {
					if strings.Contains(slug, query) {
						snesCandidates[query] = append(snesCandidates[query], titleID)
					}
				}
			}
		}
	}
	require.NoError(t, tx.Commit())

	bounds := mediaDBIDBounds{first: firstSNES, last: lastSNES}
	snes := []systemdefs.System{{ID: "SNES"}}
	assertParity := func(t *testing.T, query string, limit int, cursor *int64) []database.SearchResultWithCursor {
		t.Helper()
		expected, queryErr := sqlSearchMediaWithFiltersSorted(
			ctx, mediaDB.sql.Load(), snes, [][]string{{query}}, []string{query},
			"", nil, nil, cursor, nil, "", limit, false,
		)
		require.NoError(t, queryErr)
		actual, streamErr := sqlSearchMediaByLargeTitleDBIDSetInSystems(
			ctx, mediaDB.sql.Load(), snesCandidates[query], map[int64]string{1: "SNES"}, bounds, cursor, limit,
		)
		require.NoError(t, streamErr)
		require.Len(t, actual, len(expected))
		assert.Equal(t, searchResultIDs(expected), searchResultIDs(actual))
		for i := range actual {
			assert.Equal(t, expected[i].MediaTitleID, actual[i].MediaTitleID)
		}
		return expected
	}

	for _, tc := range []struct {
		query string
		limit int
	}{
		{query: "alpha", limit: 25},
		{query: "alpha", limit: 100},
		{query: "super", limit: 50},
		{query: "rare", limit: 100},
		{query: "plain", limit: 300},
	} {
		t.Run(fmt.Sprintf("%s-%d", tc.query, tc.limit), func(t *testing.T) {
			assertParity(t, tc.query, tc.limit, nil)
		})
	}

	firstPage := assertParity(t, "alpha", 25, nil)
	require.Len(t, firstPage, 25)
	cursor := firstPage[len(firstPage)-1].MediaID
	assertParity(t, "alpha", 25, &cursor)

	allSystems := []systemdefs.System{{ID: "SNES"}, {ID: "NES"}}
	multiExpected, err := sqlSearchMediaWithFiltersSorted(
		ctx, mediaDB.sql.Load(), allSystems, [][]string{{"alpha"}}, []string{"alpha"},
		"", nil, nil, nil, nil, "", 100, false,
	)
	require.NoError(t, err)
	multiActual, err := sqlSearchMediaByLargeTitleDBIDSetInSystems(
		ctx, mediaDB.sql.Load(), allCandidates["alpha"], map[int64]string{1: "SNES", 2: "NES"},
		mediaDBIDBounds{first: 1, last: rowsPerSystem * 2}, nil, 100,
	)
	require.NoError(t, err)
	require.Len(t, multiActual, len(multiExpected))
	assert.Equal(t, searchResultIDs(multiExpected), searchResultIDs(multiActual))
	for i := range multiActual {
		assert.Equal(t, multiExpected[i].SystemID, multiActual[i].SystemID)
		assert.Equal(t, multiExpected[i].MediaTitleID, multiActual[i].MediaTitleID)
	}
}

func searchResultIDs(results []database.SearchResultWithCursor) []int64 {
	ids := make([]int64, len(results))
	for i := range results {
		ids[i] = results[i].MediaID
	}
	return ids
}

func TestMediaSearchBounds_CachesAndClears(t *testing.T) {
	t.Parallel()

	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mediaDB := &MediaDB{}
	mediaDB.sql.Store(db)

	expectBounds := func(first, last int64) {
		mock.ExpectQuery("SELECT MIN\\(DBID\\), MAX\\(DBID\\).*FROM Media").
			WithArgs(int64(7)).
			WillReturnRows(sqlmock.NewRows([]string{"min", "max"}).AddRow(first, last))
	}

	expectBounds(100, 200)
	bounds, found, err := mediaDB.getMediaSearchBounds(context.Background(), 7)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, mediaDBIDBounds{first: 100, last: 200}, bounds)

	cached, found, err := mediaDB.getMediaSearchBounds(context.Background(), 7)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, bounds, cached)

	mediaDB.clearMediaSearchBounds()
	expectBounds(300, 400)
	refreshed, found, err := mediaDB.getMediaSearchBounds(context.Background(), 7)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, mediaDBIDBounds{first: 300, last: 400}, refreshed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMediaSearchBounds_CoalescesSameSystem(t *testing.T) {
	t.Parallel()

	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("SELECT MIN\\(DBID\\), MAX\\(DBID\\).*FROM Media").
		WithArgs(int64(7)).
		WillDelayFor(100 * time.Millisecond).
		WillReturnRows(sqlmock.NewRows([]string{"min", "max"}).AddRow(100, 200))

	mediaDB := &MediaDB{}
	mediaDB.sql.Store(db)
	type boundsResult struct {
		err    error
		bounds mediaDBIDBounds
		found  bool
	}
	results := make(chan boundsResult, 2)
	load := func() {
		bounds, found, queryErr := mediaDB.getMediaSearchBounds(context.Background(), 7)
		results <- boundsResult{err: queryErr, bounds: bounds, found: found}
	}
	go load()
	require.Eventually(t, func() bool {
		mediaDB.mediaSearchBoundsMu.RLock()
		defer mediaDB.mediaSearchBoundsMu.RUnlock()
		return mediaDB.mediaSearchBoundsLoads[7] != nil
	}, time.Second, time.Millisecond)
	go load()

	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		assert.True(t, result.found)
		assert.Equal(t, mediaDBIDBounds{first: 100, last: 200}, result.bounds)
	}
	assert.NoError(t, mock.ExpectationsWereMet(), "same-system misses should share one SQL query")
}

func TestMediaSearchBounds_CoalescesPerSystem(t *testing.T) {
	t.Parallel()

	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(4)
	mock.MatchExpectationsInOrder(false)

	for _, systemDBID := range []int64{7, 8} {
		mock.ExpectQuery("SELECT MIN\\(DBID\\), MAX\\(DBID\\).*FROM Media").
			WithArgs(systemDBID).
			WillDelayFor(100 * time.Millisecond).
			WillReturnRows(sqlmock.NewRows([]string{"min", "max"}).AddRow(systemDBID*10, systemDBID*10+9))
	}

	mediaDB := &MediaDB{}
	mediaDB.sql.Store(db)
	type boundsResult struct {
		err    error
		bounds mediaDBIDBounds
		found  bool
	}
	results := make(chan boundsResult, 2)
	for _, systemDBID := range []int64{7, 8} {
		go func() {
			bounds, found, queryErr := mediaDB.getMediaSearchBounds(context.Background(), systemDBID)
			results <- boundsResult{err: queryErr, bounds: bounds, found: found}
		}()
	}

	require.Eventually(t, func() bool {
		mediaDB.mediaSearchBoundsMu.RLock()
		defer mediaDB.mediaSearchBoundsMu.RUnlock()
		return len(mediaDB.mediaSearchBoundsLoads) == 2
	}, time.Second, time.Millisecond, "different systems should load concurrently")

	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		assert.True(t, result.found)
		assert.Positive(t, result.bounds.first)
	}
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMediaSearchBounds_InvalidatedLoadIsNotCached(t *testing.T) {
	t.Parallel()

	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("SELECT MIN\\(DBID\\), MAX\\(DBID\\).*FROM Media").
		WithArgs(int64(7)).
		WillDelayFor(100 * time.Millisecond).
		WillReturnRows(sqlmock.NewRows([]string{"min", "max"}).AddRow(100, 200))

	mediaDB := &MediaDB{}
	mediaDB.sql.Store(db)
	result := make(chan mediaDBIDBounds, 1)
	go func() {
		bounds, _, _ := mediaDB.getMediaSearchBounds(context.Background(), 7)
		result <- bounds
	}()

	require.Eventually(t, func() bool {
		mediaDB.mediaSearchBoundsMu.RLock()
		defer mediaDB.mediaSearchBoundsMu.RUnlock()
		return mediaDB.mediaSearchBoundsLoads[7] != nil
	}, time.Second, time.Millisecond)
	mediaDB.clearMediaSearchBounds()

	assert.Equal(t, mediaDBIDBounds{first: 100, last: 200}, <-result)
	mediaDB.mediaSearchBoundsMu.RLock()
	_, cached := mediaDB.mediaSearchBounds[7]
	mediaDB.mediaSearchBoundsMu.RUnlock()
	assert.False(t, cached, "an invalidated in-flight result must not be cached")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMediaSearchBounds_MetadataInvalidationPreservesCache(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "SNES", Name: "SNES"})
	require.NoError(t, err)
	require.NoError(t, mediaDB.BeginTransaction(false))
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: system.DBID,
		Slug:       "bounds-cache",
		Name:       "Bounds Cache",
	})
	require.NoError(t, err)
	media, err := mediaDB.InsertMedia(database.Media{
		SystemDBID:     system.DBID,
		MediaTitleDBID: title.DBID,
		Path:           filepath.Join("roms", "snes", "bounds-cache.sfc"),
	})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	bounds, found, err := mediaDB.getMediaSearchBounds(ctx, system.DBID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, mediaDBIDBounds{first: media.DBID, last: media.DBID}, bounds)

	mediaDB.invalidateCaches(invalidationScope{AllSystems: true})
	mediaDB.mediaSearchBoundsMu.RLock()
	cached, ok := mediaDB.mediaSearchBounds[system.DBID]
	mediaDB.mediaSearchBoundsMu.RUnlock()
	assert.True(t, ok, "metadata-only invalidation must preserve media bounds")
	assert.Equal(t, bounds, cached)

	mediaDB.invalidateCaches(invalidationScope{AllSystems: true, MediaRowsChanged: true})
	mediaDB.mediaSearchBoundsMu.RLock()
	assert.Empty(t, mediaDB.mediaSearchBounds)
	mediaDB.mediaSearchBoundsMu.RUnlock()

	// Metadata-only transactions must not evict bounds; media inserts must.
	bounds, found, err = mediaDB.getMediaSearchBounds(ctx, system.DBID)
	require.NoError(t, err)
	assert.True(t, found)
	require.NoError(t, mediaDB.BeginTransaction(false))
	require.NoError(t, mediaDB.CommitTransaction())
	mediaDB.mediaSearchBoundsMu.RLock()
	assert.Equal(t, bounds, mediaDB.mediaSearchBounds[system.DBID])
	mediaDB.mediaSearchBoundsMu.RUnlock()

	require.NoError(t, mediaDB.BeginTransaction(false))
	secondTitle, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: system.DBID,
		Slug:       "bounds-cache-second",
		Name:       "Bounds Cache Second",
	})
	require.NoError(t, err)
	_, err = mediaDB.InsertMedia(database.Media{
		SystemDBID:     system.DBID,
		MediaTitleDBID: secondTitle.DBID,
		Path:           filepath.Join("roms", "snes", "bounds-cache-second.sfc"),
	})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())
	mediaDB.mediaSearchBoundsMu.RLock()
	assert.Empty(t, mediaDB.mediaSearchBounds)
	mediaDB.mediaSearchBoundsMu.RUnlock()
}

func TestScopedCandidateStreamQueryPlanUsesRowIDOrder(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	seedBrowsePlanTestDB(t, mediaDB, 100)

	rows, err := mediaDB.sql.Load().QueryContext(
		context.Background(), "EXPLAIN QUERY PLAN "+scopedCandidateStreamQuery(1), 1, 100, 1,
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	var planLines []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &notUsed, &detail))
		planLines = append(planLines, detail)
	}
	require.NoError(t, rows.Err())

	plan := strings.Join(planLines, "\n")
	assert.Contains(t, plan, "INTEGER PRIMARY KEY")
	assert.NotContains(t, plan, "USE TEMP B-TREE FOR ORDER BY")
}

func TestSQLSearchMediaByLargeTitleDBIDSetInSystems(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	const pageSize = 10
	seedBrowsePlanTestDB(t, mediaDB, (sqliteMaxParams+pageSize)*2)

	candidateIDs := make([]int64, sqliteMaxParams+pageSize)
	for i := range candidateIDs {
		candidateIDs[i] = int64((i + 1) * 2)
	}

	bounds := mediaDBIDBounds{first: 1, last: (sqliteMaxParams + pageSize) * 2}
	results, err := sqlSearchMediaByLargeTitleDBIDSetInSystems(
		context.Background(), mediaDB.sql.Load(), candidateIDs, map[int64]string{1: "MiSTer:Arcade"},
		bounds, nil, pageSize,
	)
	require.NoError(t, err)
	require.Len(t, results, pageSize)
	for i := range results {
		assert.Equal(t, "MiSTer:Arcade", results[i].SystemID)
		assert.Equal(t, int64((i+1)*2), results[i].MediaID)
		assert.Equal(t, int64((i+1)*2), results[i].MediaTitleID)
	}

	cursor := results[len(results)-1].MediaID
	secondPage, err := sqlSearchMediaByLargeTitleDBIDSetInSystems(
		context.Background(), mediaDB.sql.Load(), candidateIDs, map[int64]string{1: "MiSTer:Arcade"},
		bounds, &cursor, pageSize,
	)
	require.NoError(t, err)
	require.Len(t, secondPage, pageSize)
	for i := range secondPage {
		assert.Equal(t, int64((i+pageSize+1)*2), secondPage[i].MediaID)
	}
}

func TestSQLSearchMediaByLargeTitleDBIDSet(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	const pageSize = 10
	seedBrowsePlanTestDB(t, mediaDB, (sqliteMaxParams+pageSize)*2)

	candidateIDs := make([]int64, sqliteMaxParams+pageSize)
	for i := range candidateIDs {
		candidateIDs[i] = int64((i + 1) * 2)
	}

	results, err := sqlSearchMediaByLargeTitleDBIDSet(
		context.Background(), mediaDB.sql.Load(), candidateIDs, "", nil, nil, nil, pageSize,
	)
	require.NoError(t, err)
	require.Len(t, results, pageSize)
	for i := range results {
		assert.Equal(t, int64((i+1)*2), results[i].MediaID)
	}

	cursor := results[len(results)-1].MediaID
	secondPage, err := sqlSearchMediaByLargeTitleDBIDSet(
		context.Background(), mediaDB.sql.Load(), candidateIDs, "", nil, nil, &cursor, pageSize,
	)
	require.NoError(t, err)
	require.Len(t, secondPage, pageSize)
	for i := range secondPage {
		assert.Equal(t, int64((i+pageSize+1)*2), secondPage[i].MediaID)
	}
}
