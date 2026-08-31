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

import "strings"

const browseNameSymbolBucket = "#"

// browseBucketExpr returns the SQL expression that derives a first-character
// bucket from the given column. It is the single source of truth for the bucket
// vocabulary on the SQL side: BuildLetterFilterSQL and the media.browse.index
// facet both go through it so the filter, the facet, and the seek can never
// disagree about which bucket a title belongs to. When the sort key changes
// (e.g. a future normalized SortKey column), only this expression and
// BrowseNameFirstChar need to move together.
func browseBucketExpr(column string) string {
	return "UPPER(SUBSTR(" + column + ", 1, 1))"
}

// browseBucketKeyExpr returns the SQL expression that folds the first character
// of column into the canonical browse bucket key ("A".."Z", "0-9", "#"). It is
// the SQL twin of BrowseNameFirstChar: the media.browse.index facet groups by
// this expression, so the two must stay in lockstep or the facet and the letter
// filter would disagree about bucket membership. UPPER leaves digits unchanged,
// so the numeric test works on the upper-cased character.
func browseBucketKeyExpr(column string) string {
	c := browseBucketExpr(column)
	return "CASE" +
		" WHEN " + c + " BETWEEN 'A' AND 'Z' THEN " + c +
		" WHEN " + c + " BETWEEN '0' AND '9' THEN '0-9'" +
		" ELSE '" + browseNameSymbolBucket + "' END"
}

// BrowseNameFirstChar returns the indexed first-character bucket used by
// media.browse letter filters.
func BrowseNameFirstChar(name string) string {
	if name == "" {
		return browseNameSymbolBucket
	}

	first := strings.ToUpper(name[:1])
	if first >= "A" && first <= "Z" {
		return first
	}
	if first >= "0" && first <= "9" {
		return "0-9"
	}
	return browseNameSymbolBucket
}

// BuildLetterFilterSQL constructs SQL WHERE clauses for filtering by the first
// character of a column. Supports single letters (A-Z), "0-9" for numeric
// starts, and "#" for symbols (non-alphanumeric).
//
// The column parameter is the SQL expression to filter on, e.g.,
// "MediaTitles.Name" or "mt.Name".
//
// Returns clauses and args in the same format as BuildTagFilterSQL.
func BuildLetterFilterSQL(letter *string, column string) (clauses []string, args []any) {
	if letter == nil || *letter == "" {
		return nil, nil
	}

	letterValue := strings.ToUpper(*letter)
	upper := browseBucketExpr(column)

	switch {
	case letterValue == "0-9":
		clauses = append(clauses, upper+" BETWEEN '0' AND '9'")
	case letterValue == "#":
		clauses = append(clauses,
			upper+" NOT BETWEEN 'A' AND 'Z'",
			upper+" NOT BETWEEN '0' AND '9'",
		)
	case len(letterValue) == 1 && letterValue >= "A" && letterValue <= "Z":
		clauses = append(clauses, upper+" = ?")
		args = append(args, letterValue)
	}

	return clauses, args
}
