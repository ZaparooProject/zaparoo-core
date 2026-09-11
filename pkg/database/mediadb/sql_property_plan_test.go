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

// TestPropertyLookupsDriveFromPropertyTable pins the join order of the bulk
// property lookups. Scrapers fill the property tables after indexing has run
// ANALYZE, so the planner usually has no statistics for them; without a fixed
// order it drove a many-ID lookup from TagTypes outward and probed the
// property index once per tag per requested ID, which took minutes on a
// MiSTer for a few thousand IDs.
func TestPropertyLookupsDriveFromPropertyTable(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	const ids = 2000
	args := make([]any, ids)
	for i := range args {
		args[i] = int64(i + 1)
	}
	inList := prepareVariadic("?", ",", ids)

	tests := []struct {
		name      string
		query     string
		wantFirst string
	}{
		{
			name:      "media properties",
			query:     mediaPropertyQuery("WHERE mp.MediaDBID IN ("+inList+")", propertyGroupInclude),
			wantFirst: "SEARCH mp USING INDEX mediaproperties_media_idx (MediaDBID=?)",
		},
		{
			name:      "media property metadata",
			query:     mediaPropertyMetadataQuery("WHERE mp.MediaDBID IN ("+inList+")", propertyGroupInclude),
			wantFirst: "SEARCH mp USING INDEX mediaproperties_media_idx (MediaDBID=?)",
		},
		{
			name:      "title properties",
			query:     mediaTitlePropertyQuery("WHERE mtp.MediaTitleDBID IN ("+inList+")", propertyGroupInclude),
			wantFirst: "SEARCH mtp USING INDEX mediatitleproperties_title_idx (MediaTitleDBID=?)",
		},
		{
			name: "title property metadata",
			query: mediaTitlePropertyMetadataQuery(
				"WHERE mtp.MediaTitleDBID IN ("+inList+")", propertyGroupInclude,
			),
			wantFirst: "SEARCH mtp USING INDEX mediatitleproperties_title_idx (MediaTitleDBID=?)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := mediaDB.sql.Load().QueryContext(ctx, "EXPLAIN QUERY PLAN "+tt.query, args...)
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
			require.NotEmpty(t, plan)
			assert.Equal(t, tt.wantFirst, plan[0], "plan: %v", plan)
			for _, step := range plan {
				assert.NotContains(t, step, "SCAN tt", "plan drives from TagTypes: %v", plan)
			}
		})
	}
}
