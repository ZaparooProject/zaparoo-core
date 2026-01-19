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
	"fmt"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// BuildTagFilterSQL constructs SQL WHERE clauses and arguments for tag filtering
// using a hybrid strategy optimized for SQLite performance:
//   - AND filters: INTERSECT pattern
//   - NOT filters: NOT EXISTS pattern
//   - OR filters: EXISTS with OR conditions
//
// Returns a slice of WHERE clause strings and corresponding arguments.
// Clauses should be joined with " AND " and appended to the main query's WHERE conditions.
func BuildTagFilterSQL(filters []zapscript.TagFilter) (clauses []string, args []any) {
	if len(filters) == 0 {
		return nil, nil
	}

	// Group filters by operator using shared logic
	andFilters, notFilters, orFilters := database.GroupTagFiltersByOperator(filters)

	clauses = make([]string, 0, len(filters))
	args = make([]any, 0, len(filters)*2)

	// Build INTERSECT clause for AND filters (optimal performance on SQLite)
	// Each INTERSECT reduces the result set, making this extremely fast
	if len(andFilters) > 0 {
		selectTpl := `SELECT MediaDBID FROM MediaTags
			JOIN Tags ON MediaTags.TagDBID = Tags.DBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE TagTypes.Type = ? AND Tags.Tag = ?`

		var intersectSelects []string
		for _, f := range andFilters {
			intersectSelects = append(intersectSelects, selectTpl)
			args = append(args, f.Type, f.Value)
		}

		intersectClause := fmt.Sprintf("Media.DBID IN (%s)", strings.Join(intersectSelects, " INTERSECT "))
		clauses = append(clauses, intersectClause)
	}

	// Build NOT EXISTS clauses for NOT filters
	// Each NOT filter excludes media that has the specified tag
	for _, f := range notFilters {
		clause := `NOT EXISTS (
			SELECT 1 FROM MediaTags
			JOIN Tags ON MediaTags.TagDBID = Tags.DBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE MediaTags.MediaDBID = Media.DBID
			AND TagTypes.Type = ? AND Tags.Tag = ?
		)`
		clauses = append(clauses, clause)
		args = append(args, f.Type, f.Value)
	}

	// Build a single EXISTS clause with OR for all OR filters
	// Media must have at least ONE of the OR tags
	if len(orFilters) > 0 {
		var orConditions []string
		for _, f := range orFilters {
			orConditions = append(orConditions, "(TagTypes.Type = ? AND Tags.Tag = ?)")
			args = append(args, f.Type, f.Value)
		}

		orClause := fmt.Sprintf(`EXISTS (
			SELECT 1 FROM MediaTags
			JOIN Tags ON MediaTags.TagDBID = Tags.DBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE MediaTags.MediaDBID = Media.DBID
			AND (%s)
		)`, strings.Join(orConditions, " OR "))
		clauses = append(clauses, orClause)
	}

	return clauses, args
}
