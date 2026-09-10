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

	zapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSearchNameSortRidesTitleNameIndex pins the two halves that have to agree
// for a name-sorted search page to cost a page.
//
// searchCursorPredicate supplies a bound the planner can turn into a range, and
// mediatitles_name_sort_idx supplies the ordering to range over. Either alone
// is not enough: without the index the sort is a temp b-tree over the whole
// matched set no matter how the predicate is written, and without the bound the
// index cannot be entered at the cursor. The index collation must stay NOCASE
// to match searchSortExpr, the same lockstep idx_media_browse_sort needs.
func TestSearchNameSortRidesTitleNameIndex(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	seedFlatFolderLibrary(t, mediaDB)

	var ddl string
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`,
		"mediatitles_name_sort_idx").Scan(&ddl))
	assert.Contains(t, ddl, "NOCASE",
		"the index must order the way searchSortExpr does or it cannot serve the sort")

	// The statement media.search runs for one media-type group: the broad LIKE
	// that cannot be indexed, joined through Media, ordered by title name. The
	// LIKE is a given; what matters is that the ordering comes from the index
	// rather than a sort of everything the LIKE matched.
	systems := []systemdefs.System{{ID: "MiSTer:Arcade"}}
	variants := [][]string{{"a"}}
	const sortOrder = "name-asc"

	// First page: no cursor, so the floor condition is the only thing that can
	// give the planner a range on the ordered column. This is the case that
	// failed outright on device before the floor existed.
	floor, _ := searchSortFloorCondition(sortOrder)
	require.NotEmpty(t, floor, "a name sort must emit a first-page floor")
	first, err := searchFilteredQuery(systems, variants, []string{"a"}, "", nil, nil,
		nil, nil, sortOrder, 25, false)
	require.NoError(t, err)
	firstPlan := explainPlan(t, mediaDB, first.query, first.args...)
	t.Logf("name-sort first page plan:\n%s", firstPlan)
	assertNameSortReadsIndexOrder(t, firstPlan)

	cursor := &database.SearchCursor{Sort: sortOrder, SortValue: "Browse Game 03000", LastID: 3000}
	deep, err := searchFilteredQuery(systems, variants, []string{"a"}, "", nil, nil,
		nil, cursor, sortOrder, 25, false)
	require.NoError(t, err)
	plan := explainPlan(t, mediaDB, deep.query, deep.args...)
	t.Logf("name-sort cursor page plan:\n%s", plan)
	assertNameSortReadsIndexOrder(t, plan)
}

// assertNameSortReadsIndexOrder checks a name-sorted search page takes its
// ordering from mediatitles_name_sort_idx rather than sorting whatever the
// query's LIKE matched.
//
// SQLite reports a whole-set sort as "USE TEMP B-TREE FOR ORDER BY" and a
// tie-break-only sort as "... FOR LAST TERM OF ORDER BY". The second is expected
// and cheap — it orders by DBID within one equal name. The first means the index
// is not serving the ordering at all, and on a large library that is the
// difference between a page and a deadline.
func assertNameSortReadsIndexOrder(t *testing.T, plan string) {
	t.Helper()
	assert.Contains(t, plan, "mediatitles_name_sort_idx (Name>",
		"the page must enter the index as a range, or it reads the matched set from "+
			"its start:\n%s", plan)
	assert.NotContains(t, plan, "USE TEMP B-TREE FOR ORDER BY",
		"a name-sorted search page must read the ordering from the index, not sort the "+
			"whole matched set:\n%s", plan)
}

// The system filter looks redundant when every system is requested, and it is
// dropped for a tag-only browse because the tag subquery constrains Media to a
// handful of rowids instead. A NOT tag does the opposite: it excludes rows and
// selects nothing, so dropping the filter for it hands an ordinary search the
// 705ms-to-1182ms regression the comment on skipSystemFilter records. Media
// visibility appends exactly such a tag to every search once anything is
// hidden, so this is the difference between a fast search and a slow one for
// every user who hides a single item.
func TestSearchSkipsSystemFilterOnlyForSelectingTags(t *testing.T) {
	t.Parallel()

	all := systemdefs.AllSystems()
	const sortOrder = "name-asc"

	build := func(t *testing.T, tags []zapscript.TagFilter) searchFilteredStatement {
		t.Helper()
		stmt, err := searchFilteredQuery(all, nil, nil, "", tags, nil,
			nil, nil, sortOrder, 25, false)
		require.NoError(t, err)
		return stmt
	}

	positive := build(t, []zapscript.TagFilter{
		{Type: "user", Value: "favorite", Operator: zapscript.TagOperatorAND},
	})
	assert.True(t, positive.skipSystemFilter,
		"a selecting tag constrains Media directly, so the system filter is redundant")

	negated := build(t, []zapscript.TagFilter{
		{Type: "user", Value: "hidden", Operator: zapscript.TagOperatorNOT},
	})
	assert.False(t, negated.skipSystemFilter,
		"a NOT tag selects nothing, so the system filter still has to constrain the join")

	mixed := build(t, []zapscript.TagFilter{
		{Type: "user", Value: "hidden", Operator: zapscript.TagOperatorNOT},
		{Type: "user", Value: "favorite", Operator: zapscript.TagOperatorAND},
	})
	assert.True(t, mixed.skipSystemFilter,
		"a visibility exclusion must not disable the favorites fast path")

	none := build(t, nil)
	assert.False(t, none.skipSystemFilter,
		"an untagged search keeps the filter, which is the behaviour this protects")
}
