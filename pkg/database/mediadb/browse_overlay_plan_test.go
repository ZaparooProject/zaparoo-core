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
	"path"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBrowseOverlayPlan_HigherPriorityCheckUsesParentDirRange is the regression
// guard for the browse timeouts in #1279.
//
// overlayHigherPriorityDirectoryCondition runs as a correlated subquery once per
// candidate row. Its ParentDir bounds are built by concatenation from the outer
// row, so the planner cannot recognise them as a range when choosing an index.
// Left to choose it took media_missing_idx, where IsMissing = 0 matches every
// row, making each candidate scan the whole Media table. On the device database
// that turned a 4-file directory into 64 ms and a 20,131-file directory into
// more than ten minutes, which is why media.browse hit the API timeout.
//
// Asserting the plan rather than a duration: the cost only becomes visible at a
// scale and on hardware a unit test cannot reproduce, but the wrong index shows
// up in the plan immediately.
func TestBrowseOverlayPlan_HigherPriorityCheckUsesParentDirRange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()

	parentDir := seedBrowsePlanTestDB(t, mediaDB, 200)
	seedAnalysisLimitedStats(ctx, t, mediaDB)

	// Two routes, so the higher-priority condition is actually exercised: with
	// a single route there is no higher priority to check against.
	overlay := &database.BrowseOverlay{Sources: []database.BrowseSource{
		{PathPrefix: parentDir, IncludeDirs: true},
		{PathPrefix: parentDir, IncludeDirs: true},
	}}

	plan := browseOverlayCountPlan(ctx, t, mediaDB, overlay)
	t.Logf("EXPLAIN QUERY PLAN:\n%s", plan)

	var descendant string
	for line := range strings.Lines(plan) {
		if strings.Contains(line, "descendant") {
			descendant = strings.TrimSpace(line)
		}
	}
	require.NotEmpty(t, descendant, "plan must reference the descendant scan; plan:\n%s", plan)

	assert.Contains(t, descendant, "idx_media_browse_sort",
		"the higher-priority check must resolve descendants through the ParentDir index; "+
			"got %q", descendant)
	assert.Contains(t, descendant, "ParentDir>",
		"the check must run as a ParentDir range scan, not a filter over every row; got %q", descendant)
	assert.NotContains(t, descendant, "media_missing_idx",
		"media_missing_idx matches every present row, so using it here makes each candidate "+
			"row scan the whole Media table (see #1279); got %q", descendant)
}

// browseOverlayCountPlan returns the joined EXPLAIN QUERY PLAN lines for the
// overlay file-count statement, built exactly as production builds it.
func browseOverlayCountPlan(
	ctx context.Context, t *testing.T, mediaDB *MediaDB, overlay *database.BrowseOverlay,
) string {
	t.Helper()

	query, args := browseOverlayFileCountQuery(database.BrowseFileCountOptions{Overlay: overlay})
	rows, err := mediaDB.sql.Load().QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
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

// seedAnalysisLimitedStats installs the sqlite_stat1 rows a real device carries,
// copied from the #1279 test MiSTer.
//
// The shape that matters is media_missing_idx: "215287 2001" claims about 2,001
// rows per IsMissing value when every one of the 215,287 rows shares the same
// value. 2001 is SQLITE_DEFAULT_OPTIMIZE_LIMIT + 1 — PRAGMA optimize runs with
// an analysis limit (analyzeApproximateMask sets bit 0x10), stops after 2,000
// index entries and extrapolates, and for a single-valued index that estimate is
// two orders of magnitude too selective. That wrong stat, not the data, is what
// makes the planner choose media_missing_idx here.
//
// Analysing the fixture instead would produce honest stats for 200 rows, the
// planner would pick correctly on its own, and this test would pass with or
// without the fix.
func seedAnalysisLimitedStats(ctx context.Context, t *testing.T, mediaDB *MediaDB) {
	t.Helper()
	sqlDB := mediaDB.sql.Load()
	_, err := sqlDB.ExecContext(ctx, "ANALYZE")
	require.NoError(t, err)
	for _, row := range []struct{ idx, stat string }{
		{"idx_media_browse_sort", "215287 14 14 2 1"},
		{"idx_media_parentdir", "215287 14"},
		{"idx_media_parentdir_system", "215287 14 10"},
		{"media_missing_idx", "215287 2001"},
		{"media_path_idx", "215287 2"},
		{"media_system_present_path_idx", "215287 501 1"},
		{"sqlite_autoindex_Media_1", "215287 501 1"},
	} {
		_, err = sqlDB.ExecContext(ctx,
			"INSERT OR REPLACE INTO sqlite_stat1(tbl, idx, stat) VALUES ('Media', ?, ?)", row.idx, row.stat)
		require.NoError(t, err, "seeding stat for %s", row.idx)
	}
	// The planner caches sqlite_stat1 per connection; force a reload.
	_, err = sqlDB.ExecContext(ctx, "ANALYZE sqlite_master")
	require.NoError(t, err)
}

// TestBrowseOverlayDirectoriesPlan_UsesCacheIndexes pins the plan of the
// cache-backed merged directory listing.
//
// The cost this change removes is a Media prefix range scan per route
// (m.Path >= parent_dir ...), which reads every row beneath the route. The
// replacement must reach BrowseDirCounts by ParentDirDBID and touch Media only
// for the direct-file shadow check, which is a ParentDir equality lookup.
// Asserting the plan rather than a duration: at fixture scale either shape
// finishes instantly, but the wrong one shows up in the plan immediately.
func TestBrowseOverlayDirectoriesPlan_UsesCacheIndexes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()

	parentDir := seedBrowsePlanTestDB(t, mediaDB, 200)
	require.NoError(t, sqlPopulateBrowseCache(ctx, mediaDB.sql.Load()))
	seedAnalysisLimitedStats(ctx, t, mediaDB)

	root := path.Dir(strings.TrimSuffix(parentDir, "/")) + "/"
	sources := []database.BrowseSource{
		{PathPrefix: root, IncludeDirs: true},
		{PathPrefix: parentDir, IncludeDirs: true},
	}
	opts := database.BrowseDirectoriesOptions{
		Overlay: &database.BrowseOverlay{Sources: sources},
	}
	parentIDs, usable, err := browseOverlayCacheParents(ctx, mediaDB.sql.Load(), sources, nil)
	require.NoError(t, err)
	require.True(t, usable, "the fixture must leave the cache serving this listing")

	mediaQuery, mediaArgs := browseOverlayDirectoriesMediaQuery(opts)
	mediaPlan := browseQueryPlan(ctx, t, mediaDB, mediaQuery, mediaArgs)
	t.Logf("fallback EXPLAIN QUERY PLAN:\n%s", mediaPlan)
	// Pinning the fallback's shape first is what gives the assertion below its
	// meaning: "Path>" is the range scan over every row beneath a route, and
	// asserting its absence only says something because it is present here.
	require.Contains(t, mediaPlan, "Path>",
		"the fallback is expected to range-scan Media by path; plan:\n%s", mediaPlan)

	query, args := browseOverlayDirectoriesCacheQuery(opts, sources, parentIDs)
	plan := browseQueryPlan(ctx, t, mediaDB, query, args)
	t.Logf("cache EXPLAIN QUERY PLAN:\n%s", plan)

	assert.Contains(t, plan, "BrowseDirCounts",
		"the candidate directories must come from the browse cache; plan:\n%s", plan)
	assert.NotContains(t, plan, "Path>",
		"a Path range scan means the listing is still reading every media row "+
			"beneath each route, which is the cost this path exists to avoid; plan:\n%s", plan)
	assert.Contains(t, plan, "ParentDir=?",
		"Media may still be read for the direct-file shadow check, but only as an "+
			"equality lookup on ParentDir; plan:\n%s", plan)
}

// browseQueryPlan returns the joined EXPLAIN QUERY PLAN lines for a statement.
func browseQueryPlan(
	ctx context.Context, t *testing.T, mediaDB *MediaDB, query string, args []any,
) string {
	t.Helper()

	rows, err := mediaDB.sql.Load().QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
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
