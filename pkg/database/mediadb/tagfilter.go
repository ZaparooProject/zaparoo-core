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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// resolveFilter applies deprecated-alias canonicalization then numeric padding to a
// raw type/value pair from a TagFilter. Returns the storage-ready type and value.
// Alias resolution is intentionally applied here (query/filter layer only); the
// indexing parser (tag_mappings.go) already emits canonical forms directly.
// strings.Index (first colon) is correct because tag values can be hierarchical
// (e.g. "keyboard:mahjong", "barcode:barcodeboy") — the type is always the
// first segment; everything after the first colon is the value.
func resolveFilter(filterType, filterValue string) (tagType, tagValue string) {
	fullTag := tags.CanonicalizeTagAlias(filterType + ":" + filterValue)
	idx := strings.Index(fullTag, ":")
	if idx < 0 {
		return fullTag, ""
	}
	return fullTag[:idx], tags.PadTagValue(fullTag[idx+1:])
}

// expandCreditFilters replaces NOT and OR "credit" filters with three filters covering
// developer, publisher, and credit tag types, implementing union-match semantics:
//   - AND credit → passed through unchanged; BuildTagFilterSQL generates a per-filter EXISTS clause
//   - NOT credit → three NOT filters (absent from all credit types)
//   - OR credit → three OR filters (union with other OR conditions)
func expandCreditFilters(filters []zapscript.TagFilter) []zapscript.TagFilter {
	expanded := make([]zapscript.TagFilter, 0, len(filters))
	for _, f := range filters {
		if f.Type != string(tags.TagTypeCredit) || f.Operator == zapscript.TagOperatorAND {
			expanded = append(expanded, f)
			continue
		}
		creditTypes := []string{
			string(tags.TagTypeDeveloper),
			string(tags.TagTypePublisher),
			string(tags.TagTypeCredit),
		}
		op := zapscript.TagOperatorOR
		if f.Operator == zapscript.TagOperatorNOT {
			op = zapscript.TagOperatorNOT
		}
		for _, t := range creditTypes {
			expanded = append(expanded, zapscript.TagFilter{Type: t, Value: f.Value, Operator: op})
		}
	}
	return expanded
}

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

	filters = expandCreditFilters(filters)

	// Group filters by operator using shared logic
	andFilters, notFilters, orFilters := database.GroupTagFiltersByOperator(filters)

	clauses = make([]string, 0, len(filters))
	args = make([]any, 0, len(filters)*4)

	// Separate AND credit filters from regular AND filters.
	// AND credit:X must match any of developer/publisher/credit for value X, so each one
	// gets its own EXISTS clause (appended directly to clauses, joined with AND by the caller).
	// Merging them into the INTERSECT path would require type-parameterized sub-selects; a
	// separate EXISTS clause per filter is simpler and correct.
	var andCreditFilters, regularAndFilters []zapscript.TagFilter
	for _, f := range andFilters {
		if f.Type == string(tags.TagTypeCredit) {
			andCreditFilters = append(andCreditFilters, f)
		} else {
			regularAndFilters = append(regularAndFilters, f)
		}
	}

	// Build INTERSECT clause for regular AND filters (optimal performance on SQLite)
	// Each INTERSECT reduces the result set, making this extremely fast
	// Each select unions MediaTags (file-level) and MediaTitleTags (title-level)
	if len(regularAndFilters) > 0 {
		selectTpl := `SELECT MediaDBID FROM (
			SELECT MediaDBID FROM MediaTags
			JOIN Tags ON MediaTags.TagDBID = Tags.DBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE TagTypes.Type = ? AND Tags.Tag = ?
			UNION
			SELECT m.DBID AS MediaDBID FROM Media m
			JOIN MediaTitleTags mtt ON m.MediaTitleDBID = mtt.MediaTitleDBID
			JOIN Tags ON mtt.TagDBID = Tags.DBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE TagTypes.Type = ? AND Tags.Tag = ?
		)`

		var intersectSelects []string
		for _, f := range regularAndFilters {
			typ, val := resolveFilter(f.Type, f.Value)
			intersectSelects = append(intersectSelects, selectTpl)
			args = append(args, typ, val, typ, val)
		}

		intersectClause := fmt.Sprintf("Media.DBID IN (%s)", strings.Join(intersectSelects, " INTERSECT "))
		clauses = append(clauses, intersectClause)
	}

	// Build per-filter forward-lookup IN clause for AND credit filters.
	// Each clause independently requires the game to be credited to that company in any role.
	// Multiple AND credit clauses are joined with AND by the caller, preserving intersection semantics.
	// Uses forward lookup (Media.DBID IN SELECT) rather than correlated EXISTS to avoid O(N) table scan.
	//nolint:gosec // False positive: "cred" in variable name is not a credential
	const andCreditSelect = `SELECT MediaDBID FROM (
		SELECT MediaDBID FROM MediaTags
		JOIN Tags ON MediaTags.TagDBID = Tags.DBID
		JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
		WHERE Tags.Tag = ? AND TagTypes.Type IN (?, ?, ?)
		UNION
		SELECT m.DBID AS MediaDBID FROM Media m
		JOIN MediaTitleTags mtt ON m.MediaTitleDBID = mtt.MediaTitleDBID
		JOIN Tags ON mtt.TagDBID = Tags.DBID
		JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
		WHERE Tags.Tag = ? AND TagTypes.Type IN (?, ?, ?)
	)`
	for _, f := range andCreditFilters {
		_, val := resolveFilter(f.Type, f.Value)
		clauses = append(clauses, fmt.Sprintf("Media.DBID IN (%s)", andCreditSelect))
		devType := string(tags.TagTypeDeveloper)
		pubType := string(tags.TagTypePublisher)
		credType := string(tags.TagTypeCredit)
		args = append(args, val, devType, pubType, credType, val, devType, pubType, credType)
	}

	// Build NOT EXISTS clauses for NOT filters
	// Each NOT filter excludes media that has the specified tag at either level
	for _, f := range notFilters {
		typ, val := resolveFilter(f.Type, f.Value)
		clause := `NOT EXISTS (
			SELECT 1 FROM MediaTags
			JOIN Tags ON MediaTags.TagDBID = Tags.DBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE MediaTags.MediaDBID = Media.DBID
			AND TagTypes.Type = ? AND Tags.Tag = ?
		) AND NOT EXISTS (
			SELECT 1 FROM MediaTitleTags
			JOIN Tags ON MediaTitleTags.TagDBID = Tags.DBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE MediaTitleTags.MediaTitleDBID = Media.MediaTitleDBID
			AND TagTypes.Type = ? AND Tags.Tag = ?
		)`
		clauses = append(clauses, clause)
		args = append(args, typ, val, typ, val)
	}

	// Build a single EXISTS clause with OR for all OR filters
	// Media must have at least ONE of the OR tags from either level
	if len(orFilters) > 0 {
		var orConditions []string
		var orTyps, orVals []string
		for _, f := range orFilters {
			typ, val := resolveFilter(f.Type, f.Value)
			orTyps = append(orTyps, typ)
			orVals = append(orVals, val)
			orConditions = append(orConditions, "(TagTypes.Type = ? AND Tags.Tag = ?)")
			args = append(args, typ, val)
		}
		orJoined := strings.Join(orConditions, " OR ")

		// Duplicate args for the second EXISTS (MediaTitleTags)
		for i := range orTyps {
			args = append(args, orTyps[i], orVals[i])
		}

		orClause := fmt.Sprintf(`(EXISTS (
			SELECT 1 FROM MediaTags
			JOIN Tags ON MediaTags.TagDBID = Tags.DBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE MediaTags.MediaDBID = Media.DBID
			AND (%s)
		) OR EXISTS (
			SELECT 1 FROM MediaTitleTags
			JOIN Tags ON MediaTitleTags.TagDBID = Tags.DBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE MediaTitleTags.MediaTitleDBID = Media.MediaTitleDBID
			AND (%s)
		))`, orJoined, orJoined)
		clauses = append(clauses, orClause)
	}

	return clauses, args
}
