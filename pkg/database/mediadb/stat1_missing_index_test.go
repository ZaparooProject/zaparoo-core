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
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func missingIndexStat(t *testing.T, mediaDB *MediaDB) (fields []string) {
	t.Helper()
	var stat string
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(context.Background(),
		`SELECT stat FROM sqlite_stat1 WHERE tbl = 'Media' AND idx = 'media_missing_idx'`).Scan(&stat))
	return strings.Fields(stat)
}

// discardMediaPlannerStats removes the Media statistics the seed helper's full
// ANALYZE wrote, so the next approximate refresh has to sample the table the
// way it does on a device at the first system commit of an index run. With
// fresh full statistics present PRAGMA optimize skips the table and the test
// measures nothing.
func discardMediaPlannerStats(t *testing.T, mediaDB *MediaDB) {
	t.Helper()
	ctx := context.Background()
	_, err := mediaDB.sql.Load().ExecContext(ctx, "DELETE FROM sqlite_stat1 WHERE tbl = 'Media'")
	require.NoError(t, err)
	_, err = mediaDB.sql.Load().ExecContext(ctx, "ANALYZE sqlite_schema")
	require.NoError(t, err)
}

// TestAnalyzeApproximate_MissingIndexStatIsTruthful pins the statistics
// AnalyzeApproximate leaves for media_missing_idx on a library larger than the
// sampling limit.
//
// The first half documents the trap: the raw sampled pass stores a per-value
// row count of about 2,001 for a column that has one value, which is what the
// MiSTer test device carried after a full index run. If that half ever fails,
// the sampling has changed and sqlTruthfulMissingIndexStat may no longer be
// needed. The second half is the guarantee: after AnalyzeApproximate the row
// says every row shares the value.
func TestAnalyzeApproximate_MissingIndexStatIsTruthful(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	seedFlatFolderLibrary(t, mediaDB)
	discardMediaPlannerStats(t, mediaDB)

	_, err := mediaDB.sql.Load().ExecContext(ctx, "PRAGMA optimize="+analyzeApproximateMask)
	require.NoError(t, err)
	sampled := missingIndexStat(t, mediaDB)
	t.Logf("sampled media_missing_idx stat: %v", sampled)
	require.Len(t, sampled, 2)
	require.NotEqual(t, sampled[0], sampled[1],
		"the sampled pass no longer misreports the single-valued index; "+
			"sqlTruthfulMissingIndexStat may be obsolete")

	require.NoError(t, mediaDB.AnalyzeApproximate())
	fixed := missingIndexStat(t, mediaDB)
	t.Logf("corrected media_missing_idx stat: %v", fixed)
	require.Len(t, fixed, 2)
	assert.Equal(t, fixed[0], fixed[1],
		"after AnalyzeApproximate the index must report that every row shares the value")
	assert.Equal(t, sampled[0], fixed[0], "the row count must be kept as ANALYZE wrote it")
}

// TestOpen_CorrectsSampledMissingIndexStat covers the upgrade case: a library
// indexed by a build without the correction carries the sampled row until its
// next index run. Opening the database has to fix it so the first search after
// the upgrade does not pay the whole-library plan.
func TestOpen_CorrectsSampledMissingIndexStat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	seedFlatFolderLibrary(t, mediaDB)
	total := missingIndexStat(t, mediaDB)[0]

	_, err := mediaDB.sql.Load().ExecContext(ctx,
		`UPDATE sqlite_stat1 SET stat = ? WHERE tbl = 'Media' AND idx = 'media_missing_idx'`,
		total+" 2001")
	require.NoError(t, err)
	require.NoError(t, mediaDB.Close())

	require.NoError(t, mediaDB.Open())
	assert.Equal(t, []string{total, total}, missingIndexStat(t, mediaDB),
		"opening the database must rewrite the sampled row")
}

// TestSearchNameSortSurvivesApproximateStats runs the statement media.search
// issues for a broad, name-sorted query over every system, with the planner
// statistics an index run actually leaves behind, and checks the page is read
// from the title name index.
//
// TestSearchNameSortRidesTitleNameIndex covers the same plan under a full
// ANALYZE. This one exists because the device never runs a full ANALYZE: it
// runs AnalyzeApproximate, and with the statistics that pass stored before
// sqlTruthfulMissingIndexStat the planner chose media_missing_idx and sorted
// the whole library for every page.
func TestSearchNameSortSurvivesApproximateStats(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	seedFlatFolderLibrary(t, mediaDB)
	discardMediaPlannerStats(t, mediaDB)
	require.NoError(t, mediaDB.AnalyzeApproximate())

	groups := buildMediaSearchTypeGroups(systemdefs.AllSystems(), []string{"a"})
	var widest mediaSearchTypeGroup
	for _, group := range groups {
		if len(group.systems) > len(widest.systems) {
			widest = group
		}
	}
	require.Greater(t, len(widest.systems), 100, "the widest media-type group is the case that failed")

	const sortOrder = "name-asc"
	first, err := searchFilteredQuery(widest.systems, widest.variantGroups, []string{"a"}, "",
		nil, nil, nil, nil, sortOrder, 25, widest.includeName)
	require.NoError(t, err)
	firstPlan := explainPlan(t, mediaDB, first.query, first.args...)
	t.Logf("first page plan:\n%s", firstPlan)
	assertNameSortReadsIndexOrder(t, firstPlan)
	assert.NotContains(t, firstPlan, "media_missing_idx",
		"IsMissing = 0 matches every present row and must not drive the page:\n%s", firstPlan)

	cursor := &database.SearchCursor{Sort: sortOrder, SortValue: "Browse Game 03000", LastID: 3000}
	deep, err := searchFilteredQuery(widest.systems, widest.variantGroups, []string{"a"}, "",
		nil, nil, nil, cursor, sortOrder, 25, widest.includeName)
	require.NoError(t, err)
	deepPlan := explainPlan(t, mediaDB, deep.query, deep.args...)
	t.Logf("cursor page plan:\n%s", deepPlan)
	assertNameSortReadsIndexOrder(t, deepPlan)
	assert.NotContains(t, deepPlan, "media_missing_idx", deepPlan)
}
