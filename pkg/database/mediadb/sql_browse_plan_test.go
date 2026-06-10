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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// arcadeScaleRows matches the Arcade system size on the MiSTer test device
// (~826 entries) where the 515ms queryDuration was originally measured.
const arcadeScaleRows = 826

// TestBrowseFilesQueryPlan_NameSortHasNoFilesort asserts that the name-sort
// browse query uses idx_media_browse_sort (no MediaTitles join, no filesort).
// Fails loudly if a regression introduces USE TEMP B-TREE FOR ORDER BY or
// accesses the MediaTitles table.
func TestBrowseFilesQueryPlan_NameSortHasNoFilesort(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()

	parentDir := seedBrowsePlanTestDB(t, mediaDB, arcadeScaleRows)
	require.NoError(t, sqlAnalyze(ctx, mediaDB.sql))

	query := `SELECT s.SystemID, m.SortName, m.Path, m.DBID, m.MediaTitleDBID, m.SortName AS SortValue
		FROM Media m
		INNER JOIN Systems s ON m.SystemDBID = s.DBID
		WHERE m.ParentDir = ? AND m.IsMissing = 0
		ORDER BY m.SortName ASC, m.DBID ASC LIMIT ?`

	rows, err := mediaDB.sql.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, parentDir, 301)
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
	assert.NotContains(t, plan, "USE TEMP B-TREE FOR ORDER BY",
		"name-sort browse must not filesort; idx_media_browse_sort should serve the ORDER BY")
	assert.NotContains(t, plan, "MediaTitles",
		"name-sort browse must not access MediaTitles; SortName is now on Media")
}

// TestExplainBrowseFilesQuery is a diagnostic helper that prints EXPLAIN QUERY
// PLAN output and warm-timing for each browse-query variant at Arcade scale
// (~826 rows in one directory). Run manually with:
//
//	go test -run TestExplainBrowseFilesQuery -v ./pkg/database/mediadb/
func TestExplainBrowseFilesQuery(t *testing.T) {
	t.Skip("diagnostic helper; run manually with -run TestExplainBrowseFilesQuery -v")

	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()

	parentDir := seedBrowsePlanTestDB(t, mediaDB, arcadeScaleRows)

	// Run ANALYZE to populate sqlite_stat1 so the planner has index cardinality
	// stats matching a production database.
	require.NoError(t, sqlAnalyze(ctx, mediaDB.sql))

	dumpScraperPragmas(ctx, t, mediaDB.sql)
	dumpSQLiteStats(ctx, t, mediaDB.sql)

	// midName is the name of the 300th row — used as the cursor value for the
	// non-first-page cases so we test the keyset seek half-way through.
	midName := fmt.Sprintf("Browse Game %05d", 300)
	midID := int64(300)

	// Each case records the SQL and args for both EXPLAIN and warm timing.
	// These match what sqlBrowseFilesFromMedia now generates (no MediaTitles join).
	type browseCase struct {
		name  string
		query string
		args  []any
	}

	cases := []browseCase{
		{
			name: "name-sort first-page (no cursor)",
			// Filter ParentDir + IsMissing via idx_media_browse_sort, sort by
			// m.SortName — no filesort, no MediaTitles join.
			query: `SELECT s.SystemID, m.SortName, m.Path, m.DBID, m.MediaTitleDBID, m.SortName AS SortValue
				FROM Media m
				INNER JOIN Systems s ON m.SystemDBID = s.DBID
				WHERE m.ParentDir = ? AND m.IsMissing = 0
				ORDER BY m.SortName ASC, m.DBID ASC LIMIT ?`,
			args: []any{parentDir, 301},
		},
		{
			name: "name-sort non-first-page (with cursor)",
			// Keyset cursor on (m.SortName, m.DBID) — same table as the filter,
			// so the index can serve the seek directly.
			query: `SELECT s.SystemID, m.SortName, m.Path, m.DBID, m.MediaTitleDBID, m.SortName AS SortValue
				FROM Media m
				INNER JOIN Systems s ON m.SystemDBID = s.DBID
				WHERE m.ParentDir = ? AND m.IsMissing = 0
				  AND (m.SortName, m.DBID) > (?, ?)
				ORDER BY m.SortName ASC, m.DBID ASC LIMIT ?`,
			args: []any{parentDir, midName, midID, 301},
		},
		{
			name: "filename-sort first-page",
			// Sort key is m.Path — no MediaTitles join needed.
			query: `SELECT s.SystemID, m.SortName, m.Path, m.DBID, m.MediaTitleDBID, m.Path AS SortValue
				FROM Media m
				INNER JOIN Systems s ON m.SystemDBID = s.DBID
				WHERE m.ParentDir = ? AND m.IsMissing = 0
				ORDER BY m.Path ASC, m.DBID ASC LIMIT ?`,
			args: []any{parentDir, 301},
		},
		{
			name: "name-sort with letter filter",
			// Letter filter now uses m.SortName — stays on the same table, no
			// separate MediaTitles lookup.
			query: `SELECT s.SystemID, m.SortName, m.Path, m.DBID, m.MediaTitleDBID, m.SortName AS SortValue
				FROM Media m
				INNER JOIN Systems s ON m.SystemDBID = s.DBID
				WHERE m.ParentDir = ? AND m.IsMissing = 0
				  AND UPPER(SUBSTR(m.SortName, 1, 1)) = ?
				ORDER BY m.SortName ASC, m.DBID ASC LIMIT ?`,
			// "B" matches "Browse Game ..." — all rows pass.
			args: []any{parentDir, "B", 301},
		},
	}

	const warmRuns = 100

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			explainQueryPlan(ctx, t, mediaDB.sql, tc.query, tc.args...)

			// Warm timing: run the query warmRuns times and drain results.
			// This measures CPU cost on a cached (warm) database; it won't
			// reproduce cold-flash I/O from the device but will reveal the
			// absolute SQL execution cost and sort structure.
			start := time.Now()
			for range warmRuns {
				func() {
					rows, err := mediaDB.sql.QueryContext(ctx, tc.query, tc.args...)
					require.NoError(t, err)
					defer func() { require.NoError(t, rows.Close()) }()
					for rows.Next() {
					}
					require.NoError(t, rows.Err())
				}()
			}
			elapsed := time.Since(start)
			t.Logf("warm timing: %d runs total=%s per-query=%s",
				warmRuns, elapsed, elapsed/time.Duration(warmRuns))
		})
	}
}

// setupBrowsePlanTestDB opens a fresh temporary MediaDB for browse plan
// diagnostics. Mirrors setupBrowseBenchMediaDB but uses testing.T.
func setupBrowsePlanTestDB(t *testing.T) (mediaDB *MediaDB, cleanup func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "zaparoo-browse-plan-mediadb-*")
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

// seedBrowsePlanTestDB inserts rows rows into a single directory under a single
// system and commits in one transaction. Returns the parentDir used so callers
// can build browse opts. Mirrors seedBenchBrowseDB but uses testing.T.
func seedBrowsePlanTestDB(t *testing.T, mediaDB *MediaDB, rows int) (parentDir string) {
	t.Helper()
	ctx := context.Background()
	parentDir = filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "arcade")) + "/"
	tx, err := mediaDB.sql.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'MiSTer:Arcade', 'Arcade');
		INSERT INTO TagTypes (DBID, Type, IsExclusive) VALUES (1, 'user', 0);
		INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES (1, 1, 'favorite');
	`)
	require.NoError(t, err)

	titleStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (?, 1, ?, ?)
	`)
	require.NoError(t, err)
	defer func() { require.NoError(t, titleStmt.Close()) }()

	mediaStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, SortName) VALUES (?, ?, 1, ?, ?, ?)
	`)
	require.NoError(t, err)
	defer func() { require.NoError(t, mediaStmt.Close()) }()

	for i := 1; i <= rows; i++ {
		id := int64(i)
		slug := fmt.Sprintf("browse-game-%05d", i)
		name := fmt.Sprintf("Browse Game %05d", i)
		path := filepath.ToSlash(filepath.Join(parentDir, fmt.Sprintf("browse-game-%05d.mra", i)))
		_, err = titleStmt.ExecContext(ctx, id, slug, name)
		require.NoError(t, err)
		_, err = mediaStmt.ExecContext(ctx, id, id, path, parentDir, name)
		require.NoError(t, err)
	}

	require.NoError(t, tx.Commit())
	return parentDir
}
