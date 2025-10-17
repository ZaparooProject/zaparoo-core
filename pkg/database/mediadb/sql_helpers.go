// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"database/sql"
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
)

// return ?, ?,... based on count
func prepareVariadic(p, s string, c int) string {
	if c < 1 {
		return ""
	}
	q := make([]string, c)
	for i := range q {
		q[i] = p
	}
	return strings.Join(q, s)
}

// buildMediaQueryWhereClause creates WHERE clause and arguments for a MediaQuery.
// Centralizes the logic to avoid duplication between different query functions.
func buildMediaQueryWhereClause(query *database.MediaQuery) (whereClause string, args []any) {
	var whereConditions []string

	// System filtering
	if len(query.Systems) > 0 {
		placeholders := make([]string, len(query.Systems))
		for i, system := range query.Systems {
			placeholders[i] = "?"
			args = append(args, system)
		}
		whereConditions = append(whereConditions,
			fmt.Sprintf("Systems.SystemID IN (%s)", strings.Join(placeholders, ",")))
	}

	// Path prefix filtering (for absolute paths)
	if query.PathPrefix != "" {
		whereConditions = append(whereConditions, "Media.Path LIKE ?")
		args = append(args, query.PathPrefix+"%")
	}

	// PathGlob - match against slugified titles for fuzzy search
	if query.PathGlob != "" {
		// Search terms are slugified to match the database's Slug field.
		// This provides fuzzy matching: spaces/punctuation are ignored,
		// making searches more forgiving (e.g., "mega man" finds "Megaman")
		var parts []string
		for _, part := range strings.Split(query.PathGlob, "*") {
			if part != "" {
				// Slugify search parts to match how titles are stored
				parts = append(parts, slugs.SlugifyString(part))
			}
		}
		for _, part := range parts {
			whereConditions = append(whereConditions, "MediaTitles.Slug LIKE ?")
			args = append(args, "%"+part+"%")
		}
	}

	tagClauses, tagArgs := BuildTagFilterSQL(query.Tags)
	whereConditions = append(whereConditions, tagClauses...)
	args = append(args, tagArgs...)

	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	return whereClause, args
}

// sqlGetMaxID returns the maximum ID from the specified table and column
// This function uses hardcoded table/column names that are validated by callers
func sqlGetMaxID(ctx context.Context, db *sql.DB, tableName, columnName string) (int64, error) {
	var query string
	switch tableName {
	case "Systems":
		query = "SELECT COALESCE(MAX(DBID), 0) FROM Systems"
	case "MediaTitles":
		query = "SELECT COALESCE(MAX(DBID), 0) FROM MediaTitles"
	case "Media":
		query = "SELECT COALESCE(MAX(DBID), 0) FROM Media"
	case "TagTypes":
		query = "SELECT COALESCE(MAX(DBID), 0) FROM TagTypes"
	case "Tags":
		query = "SELECT COALESCE(MAX(DBID), 0) FROM Tags"
	case "MediaTags":
		query = "SELECT COALESCE(MAX(DBID), 0) FROM MediaTags"
	default:
		return 0, fmt.Errorf("invalid table name: %s", tableName)
	}

	var maxID int64
	err := db.QueryRowContext(ctx, query).Scan(&maxID)
	if err != nil {
		return 0, fmt.Errorf("failed to get max ID from %s.%s: %w", tableName, columnName, err)
	}
	return maxID, nil
}
