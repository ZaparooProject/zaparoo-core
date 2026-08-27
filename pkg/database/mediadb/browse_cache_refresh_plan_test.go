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

// The mid-scan browse cache refresh runs once per system during a full index.
// Round 8 of #1279 measured it at 662,224 ms run-wide — 11.04 min, 12.7% of the
// whole reindex, and 96% of that round's indexing regression.
//
// These tests do NOT explain that cost. They were written to test the theory
// that the refresh query was reading the whole Media table once per system, and
// they disproved it: the query already uses the covering partial index
// Media(SystemDBID, Path) WHERE IsMissing = 0, and ordering on the rowid only
// added a redundant sort over one system's rows — microseconds for the 7-file
// systems that still paid ~6 s on the device.
//
// What survives is worth keeping on its own terms: the sort is pure waste, and
// the covering index is easy to lose to an innocuous-looking ORDER BY. These
// pin the plan. The real cause of the 12.7% is still open.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// browseRefreshPlanSystems and browseRefreshPlanRowsPerSystem give the planner a
// table where scanning is clearly worse than seeking: many systems, each holding
// a small share of the rows, which is exactly the production shape (130 systems,
// most of them tiny, sharing one 229k-row table).
const (
	browseRefreshPlanSystems       = 20
	browseRefreshPlanRowsPerSystem = 500
)

// The single-system IN clause the mid-scan refresh always uses. The statement
// itself comes from browseCacheMediaScanQuery, the same function production
// calls, so there is no copy here that could drift from it.
const browseRefreshPlanSingleSystemInClause = "?"

// TestBrowseCacheRefreshQueryPlan_UsesCoveringIndex is the regression guard.
//
// Losing the covering index here is invisible in correctness terms and would
// only show up as wall time on a device with a large library, so assert the
// plan rather than trust it.
func TestBrowseCacheRefreshQueryPlan_UsesCoveringIndex(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()

	seedBrowseRefreshPlanDB(t, mediaDB, browseRefreshPlanSystems, browseRefreshPlanRowsPerSystem)
	require.NoError(t, sqlAnalyze(ctx, mediaDB.sql.Load()))

	plan := browseRefreshQueryPlan(ctx, t, mediaDB,
		browseCacheMediaScanQuery(browseRefreshPlanSingleSystemInClause))
	t.Logf("EXPLAIN QUERY PLAN:\n%s", plan)

	assert.Contains(t, plan, "media_system_present_path_idx",
		"the refresh must use the covering partial index Media(SystemDBID, Path) WHERE IsMissing = 0; "+
			"without it every per-system refresh reads the entire Media table")

	assert.NotContains(t, plan, "SCAN Media",
		"the refresh must not scan Media — that would be a whole-table read once per system")

	assert.NotContains(t, plan, "USE TEMP B-TREE FOR ORDER BY",
		"the covering index already yields (SystemDBID, Path) order; a sort here means "+
			"the ORDER BY no longer matches the index")
}

// TestBrowseCacheRefreshQueryPlan_OrderByRowidAddsSort records what the previous
// ORDER BY actually cost, so the next reader does not have to re-derive it.
//
// ORDER BY m.DBID reads as a harmless stable ordering. It is not free: the
// covering index carries (SystemDBID, Path) and does not yield rowid order, so
// SQLite keeps the index and sorts the result into a temp B-tree. Small — this
// is a per-system row set — but pure waste, since the caller only needs *a*
// stable order and the index already provides one.
func TestBrowseCacheRefreshQueryPlan_OrderByRowidAddsSort(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()

	seedBrowseRefreshPlanDB(t, mediaDB, browseRefreshPlanSystems, browseRefreshPlanRowsPerSystem)
	require.NoError(t, sqlAnalyze(ctx, mediaDB.sql.Load()))

	rowidOrder := "SELECT m.SystemDBID, m.Path FROM Media m " +
		"WHERE m.IsMissing = 0 AND m.SystemDBID IN (?) " +
		"ORDER BY m.DBID"

	plan := browseRefreshQueryPlan(ctx, t, mediaDB, rowidOrder)
	t.Logf("ORDER BY m.DBID (the previous form) EXPLAIN QUERY PLAN:\n%s", plan)

	assert.Contains(t, plan, "USE TEMP B-TREE FOR ORDER BY",
		"ordering on the rowid should force a sort the covering index cannot serve; "+
			"if this stops holding, the ORDER BY change is no longer buying anything "+
			"and the comment on scanBrowseCacheMediaForSystems should be revisited")

	assert.NotContains(t, plan, "SCAN Media",
		"recording the disproved theory: ordering on the rowid does NOT make SQLite "+
			"abandon the covering index for a full table scan")
}

// browseRefreshQueryPlan returns the joined EXPLAIN QUERY PLAN detail lines for
// query, bound to a single system id the way the mid-scan refresh binds it.
func browseRefreshQueryPlan(ctx context.Context, t *testing.T, mediaDB *MediaDB, query string) string {
	t.Helper()

	rows, err := mediaDB.sql.Load().QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, int64(1))
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
	return strings.Join(planLines, "\n")
}

// seedBrowseRefreshPlanDB fills Media with rowsPerSystem rows for each of
// systems systems, interleaved so that no system's rows are contiguous in rowid
// order. Interleaving matters: it is what production looks like after repeated
// partial reindexes, and it stops a rowid scan from accidentally looking cheap.
func seedBrowseRefreshPlanDB(t *testing.T, mediaDB *MediaDB, systems, rowsPerSystem int) {
	t.Helper()
	ctx := context.Background()

	tx, err := mediaDB.sql.Load().BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	systemStmt, err := tx.PrepareContext(ctx,
		"INSERT INTO Systems (DBID, SystemID, Name) VALUES (?, ?, ?)")
	require.NoError(t, err)
	defer func() { require.NoError(t, systemStmt.Close()) }()

	for s := 1; s <= systems; s++ {
		_, err = systemStmt.ExecContext(ctx, int64(s),
			fmt.Sprintf("MiSTer:PlanSystem%02d", s), fmt.Sprintf("Plan System %02d", s))
		require.NoError(t, err)
	}

	titleStmt, err := tx.PrepareContext(ctx,
		"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (?, ?, ?, ?)")
	require.NoError(t, err)
	defer func() { require.NoError(t, titleStmt.Close()) }()

	mediaStmt, err := tx.PrepareContext(ctx,
		"INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, SortName) "+
			"VALUES (?, ?, ?, ?, ?, ?)")
	require.NoError(t, err)
	defer func() { require.NoError(t, mediaStmt.Close()) }()

	var dbid int64
	for i := 1; i <= rowsPerSystem; i++ {
		for s := 1; s <= systems; s++ {
			dbid++
			parentDir := filepath.ToSlash(
				filepath.Join(string(filepath.Separator), "roms", fmt.Sprintf("system%02d", s))) + "/"
			name := fmt.Sprintf("Plan Game %02d-%05d", s, i)
			path := filepath.ToSlash(filepath.Join(parentDir, fmt.Sprintf("plan-game-%02d-%05d.bin", s, i)))

			_, err = titleStmt.ExecContext(ctx, dbid, int64(s),
				fmt.Sprintf("plan-game-%02d-%05d", s, i), name)
			require.NoError(t, err)
			_, err = mediaStmt.ExecContext(ctx, dbid, dbid, int64(s), path, parentDir, name)
			require.NoError(t, err)
		}
	}

	require.NoError(t, tx.Commit())
}
