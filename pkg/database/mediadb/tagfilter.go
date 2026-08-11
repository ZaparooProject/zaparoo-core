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
	"slices"
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

func candidateTagExistsSQL(condition, mediaRef string) string {
	return fmt.Sprintf(`(EXISTS (
		SELECT 1 FROM MediaTags
		JOIN Tags ON MediaTags.TagDBID = Tags.DBID
		JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
		WHERE MediaTags.MediaDBID = %s.DBID
		AND %s
	) OR EXISTS (
		SELECT 1 FROM MediaTitleTags
		JOIN Tags ON MediaTitleTags.TagDBID = Tags.DBID
		JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
		WHERE MediaTitleTags.MediaTitleDBID = %s.MediaTitleDBID
		AND %s
	))`, mediaRef, condition, mediaRef, condition)
}

// buildCandidateTagFilterSQL builds correlated tag filters for a bounded set of
// media candidates. Probing tag indexes for each candidate avoids materializing
// every media ID carrying a common tag across the full database.
func isRequiredFavoriteFilter(filter zapscript.TagFilter) bool {
	if filter.Operator == zapscript.TagOperatorNOT || filter.Operator == zapscript.TagOperatorOR {
		return false
	}
	tagType, tagValue := resolveFilter(filter.Type, filter.Value)
	return tagType == string(tags.TagTypeUser) && tagValue == string(tags.TagUserFavorite)
}

func hasRequiredFavoriteFilter(filters []zapscript.TagFilter) bool {
	return slices.ContainsFunc(filters, isRequiredFavoriteFilter)
}

func buildBrowseTagFilterSQL(
	filters []zapscript.TagFilter,
	mediaRef string,
) (clauses []string, args []any) {
	if !hasRequiredFavoriteFilter(filters) {
		return buildCandidateTagFilterSQLForRef(filters, mediaRef)
	}

	var favoriteFilter zapscript.TagFilter
	remaining := make([]zapscript.TagFilter, 0, len(filters)-1)
	for _, filter := range filters {
		if favoriteFilter.Type == "" && isRequiredFavoriteFilter(filter) {
			favoriteFilter = filter
			continue
		}
		remaining = append(remaining, filter)
	}

	favoriteClauses, favoriteArgs := buildTagFilterSQLForRef([]zapscript.TagFilter{favoriteFilter}, mediaRef)
	remainingClauses, remainingArgs := buildCandidateTagFilterSQLForRef(remaining, mediaRef)
	clauses = make([]string, 0, len(favoriteClauses)+len(remainingClauses))
	clauses = append(clauses, favoriteClauses...)
	clauses = append(clauses, remainingClauses...)
	args = make([]any, 0, len(favoriteArgs)+len(remainingArgs))
	args = append(args, favoriteArgs...)
	args = append(args, remainingArgs...)
	return clauses, args
}

func buildCandidateTagFilterSQL(filters []zapscript.TagFilter) (clauses []string, args []any) {
	return buildCandidateTagFilterSQLForRef(filters, "Media")
}

func buildCandidateTagFilterSQLForRef(
	filters []zapscript.TagFilter,
	mediaRef string,
) (clauses []string, args []any) {
	if len(filters) == 0 {
		return nil, nil
	}

	filters = expandCreditFilters(filters)
	andFilters, notFilters, orFilters := database.GroupTagFiltersByOperator(filters)
	clauses = make([]string, 0, len(filters))
	args = make([]any, 0, len(filters)*4)

	for _, f := range andFilters {
		if f.Type == string(tags.TagTypeCredit) {
			_, val := resolveFilter(f.Type, f.Value)
			condition := "Tags.Tag = ? AND TagTypes.Type IN (?, ?, ?)"
			clauses = append(clauses, candidateTagExistsSQL(condition, mediaRef))
			devType := string(tags.TagTypeDeveloper)
			pubType := string(tags.TagTypePublisher)
			credType := string(tags.TagTypeCredit)
			args = append(args, val, devType, pubType, credType, val, devType, pubType, credType)
			continue
		}

		typ, val := resolveFilter(f.Type, f.Value)
		clauses = append(clauses, candidateTagExistsSQL("TagTypes.Type = ? AND Tags.Tag = ?", mediaRef))
		args = append(args, typ, val, typ, val)
	}

	for _, f := range notFilters {
		typ, val := resolveFilter(f.Type, f.Value)
		clauses = append(clauses, "NOT "+candidateTagExistsSQL("TagTypes.Type = ? AND Tags.Tag = ?", mediaRef))
		args = append(args, typ, val, typ, val)
	}

	if len(orFilters) > 0 {
		orConditions := make([]string, 0, len(orFilters))
		orTypes := make([]string, 0, len(orFilters))
		orValues := make([]string, 0, len(orFilters))
		for _, f := range orFilters {
			typ, val := resolveFilter(f.Type, f.Value)
			orConditions = append(orConditions, "(TagTypes.Type = ? AND Tags.Tag = ?)")
			orTypes = append(orTypes, typ)
			orValues = append(orValues, val)
			args = append(args, typ, val)
		}
		for i := range orTypes {
			args = append(args, orTypes[i], orValues[i])
		}
		clauses = append(clauses, candidateTagExistsSQL("("+strings.Join(orConditions, " OR ")+")", mediaRef))
	}

	return clauses, args
}

// BuildTagFilterSQL constructs SQL WHERE clauses and arguments for tag filtering
// using a hybrid strategy optimized for SQLite performance:
//   - AND filters: INTERSECT pattern
//   - NOT filters: forward NOT IN anti-set
//   - OR filters: EXISTS with OR conditions
//
// Returns a slice of WHERE clause strings and corresponding arguments.
// Clauses should be joined with " AND " and appended to the main query's WHERE conditions.
func BuildTagFilterSQL(filters []zapscript.TagFilter) (clauses []string, args []any) {
	return buildTagFilterSQLForRef(filters, "Media")
}

func buildTagFilterSQLForRef(filters []zapscript.TagFilter, mediaRef string) (clauses []string, args []any) {
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

		intersectClause := fmt.Sprintf("%s.DBID IN (%s)", mediaRef, strings.Join(intersectSelects, " INTERSECT "))
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
		clauses = append(clauses, fmt.Sprintf("%s.DBID IN (%s)", mediaRef, andCreditSelect))
		devType := string(tags.TagTypeDeveloper)
		pubType := string(tags.TagTypePublisher)
		credType := string(tags.TagTypeCredit)
		args = append(args, val, devType, pubType, credType, val, devType, pubType, credType)
	}

	// Build forward anti-set clauses for NOT filters. Resolving matching media IDs
	// once lets SQLite use the reverse tag indexes instead of repeating correlated
	// tag joins for every candidate media row.
	for _, f := range notFilters {
		typ, val := resolveFilter(f.Type, f.Value)
		clause := mediaRef + `.DBID NOT IN (
			SELECT MediaDBID FROM MediaTags
			JOIN Tags ON MediaTags.TagDBID = Tags.DBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE TagTypes.Type = ? AND Tags.Tag = ?
			UNION ALL
			SELECT m.DBID AS MediaDBID FROM Media m
			JOIN MediaTitleTags mtt ON m.MediaTitleDBID = mtt.MediaTitleDBID
			JOIN Tags ON mtt.TagDBID = Tags.DBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE TagTypes.Type = ? AND Tags.Tag = ?
		)`
		clauses = append(clauses, clause)
		args = append(args, typ, val, typ, val)
	}

	// Resolve all OR matches through reverse tag indexes once. Correlating OR
	// probes against the outer Media scan makes even sparse unions O(all media)
	// on MiSTer and can exceed the API timeout.
	if len(orFilters) > 0 {
		orConditions := make([]string, 0, len(orFilters))
		orTypes := make([]string, 0, len(orFilters))
		orValues := make([]string, 0, len(orFilters))
		for _, f := range orFilters {
			typ, val := resolveFilter(f.Type, f.Value)
			orTypes = append(orTypes, typ)
			orValues = append(orValues, val)
			orConditions = append(orConditions, "(TagTypes.Type = ? AND Tags.Tag = ?)")
			args = append(args, typ, val)
		}
		orJoined := strings.Join(orConditions, " OR ")
		for i := range orTypes {
			args = append(args, orTypes[i], orValues[i])
		}

		orClause := fmt.Sprintf(`%s.DBID IN (
			SELECT MediaDBID FROM MediaTags
			JOIN Tags ON MediaTags.TagDBID = Tags.DBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE %s
			UNION
			SELECT m.DBID AS MediaDBID FROM Media m
			JOIN MediaTitleTags mtt ON m.MediaTitleDBID = mtt.MediaTitleDBID
			JOIN Tags ON mtt.TagDBID = Tags.DBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE %s
		)`, mediaRef, orJoined, orJoined)
		clauses = append(clauses, orClause)
	}

	return clauses, args
}
