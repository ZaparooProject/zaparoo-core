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
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// sqlBrowseDirectories returns distinct immediate subdirectory names under the
// given path prefix from BrowseCache, along with the precomputed file count.
func sqlBrowseDirectories(
	ctx context.Context,
	db sqlQueryable,
	pathPrefix string,
) ([]database.BrowseDirectoryResult, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT Name, FileCount FROM BrowseCache WHERE ParentPath = ? ORDER BY Name ASC`,
		pathPrefix,
	)
	if err != nil {
		return nil, fmt.Errorf("browse directories query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.BrowseDirectoryResult
	for rows.Next() {
		var r database.BrowseDirectoryResult
		if err := rows.Scan(&r.Name, &r.FileCount); err != nil {
			return nil, fmt.Errorf("browse directories scan: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("browse directories rows: %w", err)
	}

	return results, nil
}

// browseFilesBaseCondition returns the WHERE clause and args for filtering
// immediate children of a path prefix, with optional letter filter.
// Uses the ParentDir column for direct index lookup instead of range scan.
func browseFilesBaseCondition(
	opts *database.BrowseFilesOptions,
) (where string, args []any) {
	letterClauses, letterArgs := BuildLetterFilterSQL(opts.Letter, "mt.Name")

	conditions := make([]string, 0, 2+len(letterClauses))
	conditions = append(conditions, `m.ParentDir = ?`, `m.IsMissing = 0`)
	conditions = append(conditions, letterClauses...)

	args = make([]any, 0, 1+len(letterArgs))
	args = append(args, opts.PathPrefix)
	args = append(args, letterArgs...)

	where = strings.Join(conditions, " AND ")
	return where, args
}

// browseSortClause returns the ORDER BY clause for the given sort option.
func browseSortClause(sort string) string {
	switch sort {
	case "name-desc":
		return "mt.Name DESC, m.DBID DESC"
	case "filename-asc":
		return "m.Path ASC, m.DBID ASC"
	case "filename-desc":
		return "m.Path DESC, m.DBID DESC"
	default: // "name-asc" or empty
		return "mt.Name ASC, m.DBID ASC"
	}
}

// browseCursorCondition returns the keyset pagination WHERE clause fragment for
// the given sort order. The caller must append (sortValue, lastID) as args.
func browseCursorCondition(sort string) string {
	switch sort {
	case "name-desc":
		return ` AND (mt.Name, m.DBID) < (?, ?)`
	case "filename-asc":
		return ` AND (m.Path, m.DBID) > (?, ?)`
	case "filename-desc":
		return ` AND (m.Path, m.DBID) < (?, ?)`
	default: // "name-asc" or empty
		return ` AND (mt.Name, m.DBID) > (?, ?)`
	}
}

// sqlBrowseFiles returns indexed media files that are immediate children of the
// given path prefix (no further '/' after the prefix). Supports cursor-based
// pagination, letter filtering, and sort order.
func sqlBrowseFiles(
	ctx context.Context,
	db sqlQueryable,
	opts *database.BrowseFilesOptions,
) ([]database.SearchResultWithCursor, error) {
	where, args := browseFilesBaseCondition(opts)

	query := `
		SELECT s.SystemID, mt.Name, m.Path, m.DBID
		FROM Media m
		INNER JOIN MediaTitles mt ON m.MediaTitleDBID = mt.DBID
		INNER JOIN Systems s ON mt.SystemDBID = s.DBID
		WHERE ` + where

	if opts.Cursor != nil {
		query += browseCursorCondition(opts.Sort)
		args = append(args, opts.Cursor.SortValue, opts.Cursor.LastID)
	}

	query += ` ORDER BY ` + browseSortClause(opts.Sort) + ` LIMIT ?`
	args = append(args, opts.Limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("browse files query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.SearchResultWithCursor
	for rows.Next() {
		var r database.SearchResultWithCursor
		if err := rows.Scan(&r.SystemID, &r.Name, &r.Path, &r.MediaID); err != nil {
			return nil, fmt.Errorf("browse files scan: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("browse files rows: %w", err)
	}

	if err := fetchAndAttachTags(ctx, db, results); err != nil {
		return nil, fmt.Errorf("browse files tags: %w", err)
	}

	return results, nil
}

// sqlBrowseFileCount returns the total number of immediate child files under
// a path prefix, with optional letter filtering. Used for total count in
// pagination responses.
func sqlBrowseFileCount(
	ctx context.Context,
	db sqlQueryable,
	pathPrefix string,
	letter *string,
) (int, error) {
	where, args := browseFilesBaseCondition(&database.BrowseFilesOptions{
		PathPrefix: pathPrefix,
		Letter:     letter,
	})

	query := `
		SELECT COUNT(*)
		FROM Media m
		INNER JOIN MediaTitles mt ON m.MediaTitleDBID = mt.DBID
		WHERE ` + where

	var count int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("browse file count: %w", err)
	}

	return count, nil
}

// sqlBrowseVirtualSchemes returns distinct URI schemes present in indexed media,
// with the count of media entries for each scheme.
func sqlBrowseVirtualSchemes(
	ctx context.Context,
	db sqlQueryable,
) ([]database.BrowseVirtualScheme, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT DirPath, FileCount FROM BrowseCache
		 WHERE IsVirtual = 1 ORDER BY DirPath ASC`)
	if err != nil {
		return nil, fmt.Errorf("browse virtual schemes query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.BrowseVirtualScheme
	for rows.Next() {
		var r database.BrowseVirtualScheme
		if err := rows.Scan(&r.Scheme, &r.FileCount); err != nil {
			return nil, fmt.Errorf("browse virtual schemes scan: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("browse virtual schemes rows: %w", err)
	}

	return results, nil
}

// sqlBrowseRootCounts looks up precomputed file counts for root directories
// from BrowseCache. Returns nil *int for roots where the cache has no entry
// (cache not yet populated or no indexed content under that root).
func sqlBrowseRootCounts(
	ctx context.Context,
	db sqlQueryable,
	rootDirs []string,
) (map[string]*int, error) {
	if len(rootDirs) == 0 {
		return make(map[string]*int), nil
	}

	// Build prefix list and map prefixes back to original root strings.
	prefixes := make([]string, len(rootDirs))
	prefixToRoot := make(map[string]string, len(rootDirs))
	args := make([]any, len(rootDirs))
	for i, root := range rootDirs {
		prefix := root
		if prefix != "" && prefix[len(prefix)-1] != '/' {
			prefix += "/"
		}
		prefixes[i] = prefix
		prefixToRoot[prefix] = root
		args[i] = prefix
	}

	placeholders := make([]string, len(prefixes))
	for i := range placeholders {
		placeholders[i] = "?"
	}

	rows, err := db.QueryContext(ctx,
		`SELECT DirPath, FileCount FROM BrowseCache WHERE DirPath IN (`+
			strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("browse root counts query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Initialize all roots as cache miss (nil), then fill from results.
	counts := make(map[string]*int, len(rootDirs))
	for _, root := range rootDirs {
		counts[root] = nil
	}
	for rows.Next() {
		var dirPath string
		var count int
		if err := rows.Scan(&dirPath, &count); err != nil {
			return nil, fmt.Errorf("browse root counts scan: %w", err)
		}
		if root, ok := prefixToRoot[dirPath]; ok {
			c := count
			counts[root] = &c
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("browse root counts rows: %w", err)
	}

	return counts, nil
}
