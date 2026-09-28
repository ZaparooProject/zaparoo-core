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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1584: the systems list and browse load the hidden media on every
// request. Once a user:hidden tag existed, the planner walked every system's
// present media and probed MediaTags for each, 8.5 s on a MiSTer with a cold
// cache and nothing hidden, past the TUI's request timeout. The lookup must
// start from the hidden tag's MediaTags rows.
func TestHiddenMediaRowsDriveFromHiddenTag(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()
	sqlDB := mediaDB.sql.Load()

	// sqlite_stat1 rows from the #1584 test MiSTer (236k media, 108 systems).
	// PRAGMA optimize's analysis limit left Systems at "15 1" and put 28k rows
	// behind each tag, which is what made walking Systems then Media look
	// cheaper than the tag lookup. Honest stats for an empty fixture would plan
	// correctly with or without the fix.
	_, err := sqlDB.ExecContext(ctx, "ANALYZE")
	require.NoError(t, err)
	for _, row := range []struct{ tbl, idx, stat string }{
		{"Media", "media_system_present_path_idx", "236881 2001 1"},
		{"Media", "sqlite_autoindex_Media_1", "236881 2001 1"},
		{"Media", "media_missing_idx", "236881 236881"},
		{"MediaTags", "MediaTags", "339075 4 1"},
		{"MediaTags", "mediatags_tag_media_idx", "339075 28257 1"},
		{"Systems", "sqlite_autoindex_Systems_1", "15 1"},
		{"TagTypes", "sqlite_autoindex_TagTypes_1", "50 1"},
		{"Tags", "tags_tag_idx", "23103 1"},
		{"Tags", "tags_tagtype_idx", "23103 334"},
		{"Tags", "tags_type_tag_idx", "23103 334 1"},
	} {
		_, err = sqlDB.ExecContext(ctx,
			"INSERT OR REPLACE INTO sqlite_stat1(tbl, idx, stat) VALUES (?, ?, ?)", row.tbl, row.idx, row.stat)
		require.NoError(t, err, "seeding stat for %s", row.idx)
	}
	// The planner caches sqlite_stat1 per connection; force a reload.
	_, err = sqlDB.ExecContext(ctx, "ANALYZE sqlite_master")
	require.NoError(t, err)

	rows, err := sqlDB.QueryContext(ctx, "EXPLAIN QUERY PLAN "+hiddenMediaRowsSQL)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &notUsed, &detail))
		plan = append(plan, detail)
	}
	require.NoError(t, rows.Err())

	require.Len(t, plan, 5, "plan: %v", plan)
	assert.Contains(t, plan[2], "SEARCH mt USING COVERING INDEX mediatags_tag_media_idx (TagDBID=?)", "plan: %v", plan)
	// Each tagged row is then one rowid seek, by primary key or through
	// media_missing_idx's (IsMissing, rowid) key.
	assert.Contains(t, plan[3], "SEARCH m ", "plan: %v", plan)
	assert.Contains(t, plan[3], "rowid=?", "plan: %v", plan)
	for _, step := range plan {
		assert.NotContains(t, step, "SCAN", "plan: %v", plan)
	}
}
