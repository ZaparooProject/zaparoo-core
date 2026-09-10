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
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flatFolderRows is the size of the folder in #1460: a system folder holding
// roughly seven thousand files directly, with no subdirectories.
const flatFolderRows = 7000

// The rest of the library the folder sits in, so ParentDir = ? is a selective
// predicate the way it is on a device rather than the whole table. Without
// them every row is in the folder under test, the planner prefers a full scan
// on its own merits, and the fixture cannot tell a real index choice from an
// artefact of its own shape.
const (
	siblingFolders    = 200
	siblingFolderRows = 250
)

// setupMigratedOnlyMediaDB opens a database in exactly the state migrations
// leave it: without CreateSecondaryIndexes, which only runs at the end of an
// indexing run. Every device that has upgraded and not reindexed since browses
// in this state.
func setupMigratedOnlyMediaDB(t *testing.T) (mediaDB *MediaDB, cleanup func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "zaparoo-browse-collation-mediadb-*")
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: tempDir})

	mediaDB, err = OpenMediaDB(context.Background(), mockPlatform)
	require.NoError(t, err)
	cleanup = func() {
		if mediaDB != nil {
			_ = mediaDB.Close()
		}
		_ = os.RemoveAll(tempDir)
	}
	return mediaDB, cleanup
}

func browseSortIndexDDL(t *testing.T, mediaDB *MediaDB) string {
	t.Helper()
	var ddl string
	err := mediaDB.sql.Load().QueryRowContext(context.Background(),
		`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`,
		browseSortIndexName).Scan(&ddl)
	require.NoError(t, err)
	return ddl
}

func explainPlan(t *testing.T, mediaDB *MediaDB, query string, args ...any) string {
	t.Helper()
	rows, err := mediaDB.sql.Load().QueryContext(context.Background(),
		"EXPLAIN QUERY PLAN "+query, args...)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	var lines []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &notUsed, &detail))
		lines = append(lines, detail)
	}
	require.NoError(t, rows.Err())
	return strings.Join(lines, "\n")
}

// seedFlatFolderLibrary builds the #1460 shape: one flat folder of several
// thousand files inside a library of ordinary folders, with planner statistics.
func seedFlatFolderLibrary(t *testing.T, mediaDB *MediaDB) (parentDir string) {
	t.Helper()
	ctx := context.Background()
	parentDir = seedBrowsePlanTestDB(t, mediaDB, flatFolderRows)

	tx, err := mediaDB.sql.Load().BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	titleStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (?, 1, ?, ?)`)
	require.NoError(t, err)
	defer func() { require.NoError(t, titleStmt.Close()) }()

	mediaStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, SortName)
		 VALUES (?, ?, 1, ?, ?, ?)`)
	require.NoError(t, err)
	defer func() { require.NoError(t, mediaStmt.Close()) }()

	id := int64(flatFolderRows + 1)
	for folder := range siblingFolders {
		siblingDir := fmt.Sprintf("/roms/other-%04d/", folder)
		for row := range siblingFolderRows {
			slug := fmt.Sprintf("other-%04d-%05d", folder, row)
			name := fmt.Sprintf("Other Game %04d %05d", folder, row)
			_, err = titleStmt.ExecContext(ctx, id, slug, name)
			require.NoError(t, err)
			_, err = mediaStmt.ExecContext(ctx, id, id, siblingDir+slug+".rom", siblingDir, name)
			require.NoError(t, err)
			id++
		}
	}
	require.NoError(t, tx.Commit())
	require.NoError(t, sqlAnalyze(ctx, mediaDB.sql.Load()))
	return parentDir
}

// cursorPagePlan returns the query plan for the statement a cursor page
// actually runs, produced by the production builder.
func cursorPagePlan(t *testing.T, mediaDB *MediaDB, parentDir string) string {
	t.Helper()
	opts := &database.BrowseFilesOptions{
		PathPrefix: parentDir,
		Limit:      7,
		Sort:       "name-asc",
		Cursor:     &database.BrowseCursor{SortValue: "Browse Game 03000", LastID: 3000},
	}
	query, args := browseFilesQuery(opts, "name-asc", browseTagPlan{})
	plan := explainPlan(t, mediaDB, query, args...)
	t.Logf("index: %s\nplan:\n%s", browseSortIndexDDL(t, mediaDB), plan)
	return plan
}

func assertCursorPageSeeks(t *testing.T, plan string) {
	t.Helper()
	assert.NotContains(t, plan, "USE TEMP B-TREE FOR ORDER BY",
		"a cursor page must be served in index order, not by sorting the folder:\n%s", plan)
	assert.Contains(t, plan, "SortName>",
		"the keyset predicate must be part of the index seek so a page costs a page:\n%s", plan)
}

// TestBrowseFilesQueryPlan_CursorPageSeeksAfterIndexing is the control: it pins
// that the statement and the assertions are right once the collated index is in
// place, so a failure of the repair test below can only be the index definition.
func TestBrowseFilesQueryPlan_CursorPageSeeksAfterIndexing(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()

	require.Contains(t, browseSortIndexDDL(t, mediaDB), browseTitleCollationName)
	assertCursorPageSeeks(t, cursorPagePlan(t, mediaDB, seedFlatFolderLibrary(t, mediaDB)))
}

// Every other plan test here builds its statement with no tags, so none of them
// sees the statement an install with a hidden item actually runs: media
// visibility appends a NOT filter to every browse, and it lands in the same
// WHERE the cursor seek depends on. Without this the seek could be lost the
// moment a user hides anything and every plan test would still pass.
func TestBrowseFilesQueryPlan_CursorPageSeeksWithVisibilityFilter(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	parentDir := seedFlatFolderLibrary(t, mediaDB)

	opts := &database.BrowseFilesOptions{
		PathPrefix:    parentDir,
		Limit:         7,
		Sort:          "name-asc",
		Cursor:        &database.BrowseCursor{SortValue: "Browse Game 03000", LastID: 3000},
		Tags:          filters.ExcludeHidden(nil),
		ExcludeHidden: true,
	}
	require.NotEmpty(t, opts.Tags, "the visibility filter must actually be present")
	query, args := browseFilesQuery(opts, "name-asc",
		browsePrefixTagPlan(context.Background(), mediaDB.sql.Load(), parentDir, nil, opts.Tags))
	assertCursorPageSeeks(t, explainPlan(t, mediaDB, query, args...))
}

// TestBrowseSortIndexRepair_MakesCursorPagesSeek covers the state the existing
// plan coverage cannot reach: setupBrowsePlanTestDB calls
// CreateSecondaryIndexes, so it only ever measures the collated index.
//
// The base migration creates idx_media_browse_sort without the
// ZAPAROO_TITLE_V1 collation, and only CreateSecondaryIndexes replaces it — at
// the end of an indexing run and nowhere else. Until that happens the index
// orders SortName differently from every browse query, so the planner cannot
// use it for the ORDER BY: it falls back to idx_media_parentdir_system and a
// temp b-tree, reading and sorting the whole folder for each page. Six rows of
// a 7,000-file folder then cost 7,000 rows read and sorted, every page (#1460),
// on any device that upgraded without reindexing since.
//
// EnsureBrowseSortIndex is the repair. Both halves are asserted: that the
// migrated-only state really does lose the seek, and that the repair restores
// it without an indexing run.
func TestBrowseSortIndexRepair_MakesCursorPagesSeek(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupMigratedOnlyMediaDB(t)
	defer cleanup()
	parentDir := seedFlatFolderLibrary(t, mediaDB)

	require.NotContains(t, browseSortIndexDDL(t, mediaDB), browseTitleCollationName,
		"fixture must start in the state migrations leave behind")
	before := cursorPagePlan(t, mediaDB, parentDir)
	require.Contains(t, before, "USE TEMP B-TREE FOR ORDER BY",
		"without the collation a cursor page must be sorting the folder; if that stops "+
			"being true the repair below is no longer measuring anything:\n%s", before)

	require.NoError(t, mediaDB.EnsureBrowseSortIndex())

	assert.Contains(t, browseSortIndexDDL(t, mediaDB), browseTitleCollationName,
		"the repair must leave the collated index in place")
	assertCursorPageSeeks(t, cursorPagePlan(t, mediaDB, parentDir))

	// The repair also creates indexes that are merely absent, which is how a
	// database that never reindexes gets one added after it was built. The
	// search title sort index is the case that matters: without it a
	// name-sorted media.search sorts its whole matched set on every page.
	var searchIdx string
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(context.Background(),
		`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`,
		"mediatitles_name_sort_idx").Scan(&searchIdx),
		"the repair must create secondary indexes the database is missing, not only "+
			"replace ones whose definition moved on")
	assert.Contains(t, searchIdx, "NOCASE")
}

// TestBrowseSortIndexRepair_LeavesCurrentIndexAlone keeps the repair off the
// path of a database that has already been indexed: rebuilding the index there
// would be minutes of pointless work on every start.
func TestBrowseSortIndexRepair_LeavesCurrentIndexAlone(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	seedBrowsePlanTestDB(t, mediaDB, 10)

	before := browseSortIndexDDL(t, mediaDB)
	require.NoError(t, mediaDB.EnsureBrowseSortIndex())
	assert.Equal(t, before, browseSortIndexDDL(t, mediaDB))
}

// The slug cache rebuild starts at the same moment the browse index repair
// does, and both are launched from startup right after the database opens. If
// the repair stands back for it, it silently never runs on any install that
// warms the cache, and large-folder browsing stays slow for exactly the reason
// the repair exists. Only a media write may hold it off.
func TestBrowseSortIndexRepair_RunsWhileTheSlugCacheRecovers(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupMigratedOnlyMediaDB(t)
	defer cleanup()
	parentDir := seedFlatFolderLibrary(t, mediaDB)

	// Stand in for the recovery worker the cache starts at open. Already
	// finished, so closing the database does not wait on it.
	finished := make(chan struct{})
	close(finished)
	mediaDB.slugCacheState.mu.Lock()
	mediaDB.slugCacheState.worker = &slugCacheRecovery{cancel: func() {}, done: finished}
	mediaDB.slugCacheState.mu.Unlock()
	require.True(t, mediaDB.HasBackgroundOperations(),
		"fixture must reproduce the state that used to skip the repair")

	require.NoError(t, mediaDB.EnsureBrowseSortIndex())
	assert.Contains(t, browseSortIndexDDL(t, mediaDB), browseTitleCollationName,
		"the repair must still install the collated index while the cache rebuilds")
	assertCursorPageSeeks(t, cursorPagePlan(t, mediaDB, parentDir))

	// A media write still holds it off: that one drops the secondary indexes.
	mediaDB.TrackBackgroundOperation()
	defer mediaDB.BackgroundOperationDone()
	assert.True(t, mediaDB.hasBackgroundWrites(),
		"a tracked media write must still stop the repair racing an index drop")
}
