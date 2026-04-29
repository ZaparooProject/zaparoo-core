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
	stdsql "database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

func browseSystemFilterClause(column string, systems []systemdefs.System) (clause string, args []any) {
	if len(systems) == 0 {
		return "", nil
	}

	placeholders := make([]string, len(systems))
	args = make([]any, len(systems))
	for i := range systems {
		placeholders[i] = "?"
		args[i] = systems[i].ID
	}

	return column + " IN (" + strings.Join(placeholders, ",") + ")", args
}

func splitBrowseSystemIDs(ids string) []string {
	if ids == "" {
		return nil
	}
	return strings.Split(ids, ",")
}

// sqlBrowseDirectories returns distinct immediate subdirectory names under the
// given path prefix from BrowseCache, along with the precomputed file count.
func sqlBrowseDirectories(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseDirectoriesOptions,
) ([]database.BrowseDirectoryResult, error) {
	if len(opts.Systems) > 0 {
		return sqlBrowseDirectoriesForSystems(ctx, db, opts)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT Name, FileCount FROM BrowseCache WHERE ParentPath = ? ORDER BY Name ASC`,
		opts.PathPrefix,
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

func sqlBrowseDirectoriesForSystems(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseDirectoriesOptions,
) ([]database.BrowseDirectoryResult, error) {
	results, err := sqlBrowseDirectoriesForSystemsFromCache(ctx, db, opts)
	if err != nil {
		return nil, err
	}
	missingSystems := browseMissingSystems(opts.Systems, browseCoveredSystemIDsFromDirectories(results))
	if len(missingSystems) == 0 {
		return results, nil
	}

	mediaResults, err := sqlBrowseDirectoriesForSystemsFromMedia(ctx, db, database.BrowseDirectoriesOptions{
		PathPrefix: opts.PathPrefix,
		Systems:    missingSystems,
	})
	if err != nil {
		return nil, err
	}

	return mergeBrowseDirectoryResults(results, mediaResults), nil
}

func browseCoveredSystemIDsFromDirectories(results []database.BrowseDirectoryResult) map[string]struct{} {
	covered := make(map[string]struct{})
	for _, result := range results {
		for _, systemID := range result.SystemIDs {
			covered[systemID] = struct{}{}
		}
	}
	return covered
}

func browseMissingSystems(systems []systemdefs.System, covered map[string]struct{}) []systemdefs.System {
	missing := make([]systemdefs.System, 0, len(systems))
	for _, system := range systems {
		if _, ok := covered[system.ID]; !ok {
			missing = append(missing, system)
		}
	}
	return missing
}

func mergeBrowseDirectoryResults(
	base []database.BrowseDirectoryResult,
	extra []database.BrowseDirectoryResult,
) []database.BrowseDirectoryResult {
	if len(extra) == 0 {
		return base
	}

	byName := make(map[string]*database.BrowseDirectoryResult, len(base)+len(extra))
	for i := range base {
		result := base[i]
		result.SystemIDs = uniqueBrowseSystemIDs(result.SystemIDs)
		byName[result.Name] = &result
	}
	for _, result := range extra {
		if existing, ok := byName[result.Name]; ok {
			existing.FileCount += result.FileCount
			existing.SystemIDs = uniqueBrowseSystemIDs(append(existing.SystemIDs, result.SystemIDs...))
			continue
		}
		result.SystemIDs = uniqueBrowseSystemIDs(result.SystemIDs)
		byName[result.Name] = &result
	}

	merged := make([]database.BrowseDirectoryResult, 0, len(byName))
	for _, result := range byName {
		merged = append(merged, *result)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Name < merged[j].Name })
	return merged
}

func sqlBrowseDirectoriesForSystemsFromCache(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseDirectoriesOptions,
) ([]database.BrowseDirectoryResult, error) {
	systemClause, systemArgs := browseSystemFilterClause("s.SystemID", opts.Systems)
	args := make([]any, 0, 1+len(systemArgs))
	args = append(args, opts.PathPrefix)
	args = append(args, systemArgs...)

	rows, err := db.QueryContext(ctx,
		`SELECT b.Name, SUM(b.FileCount), GROUP_CONCAT(DISTINCT s.SystemID)
		 FROM BrowseSystemCache b
		 INNER JOIN Systems s ON b.SystemDBID = s.DBID
		 WHERE b.ParentPath = ? AND `+systemClause+`
		 GROUP BY b.DirPath, b.Name
		 ORDER BY b.Name ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("browse directories by system query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.BrowseDirectoryResult
	for rows.Next() {
		var r database.BrowseDirectoryResult
		var systemIDs string
		if err := rows.Scan(&r.Name, &r.FileCount, &systemIDs); err != nil {
			return nil, fmt.Errorf("browse directories by system scan: %w", err)
		}
		r.SystemIDs = splitBrowseSystemIDs(systemIDs)
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("browse directories by system rows: %w", err)
	}

	return results, nil
}

func sqlBrowseDirectoriesForSystemsFromMedia(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseDirectoriesOptions,
) ([]database.BrowseDirectoryResult, error) {
	systemClause, systemArgs := browseSystemFilterClause("s.SystemID", opts.Systems)
	args := make([]any, 0, 2+len(systemArgs))
	args = append(args, opts.PathPrefix, opts.PathPrefix)
	args = append(args, systemArgs...)

	rows, err := db.QueryContext(ctx,
		`WITH matched AS (
			 SELECT s.SystemID, substr(m.Path, length(?) + 1) AS Rest
			 FROM Media m
			 INNER JOIN Systems s ON m.SystemDBID = s.DBID
			 WHERE m.IsMissing = 0 AND m.Path LIKE ? || '%' AND `+systemClause+`
		 )
		 SELECT substr(Rest, 1, instr(Rest, '/') - 1) AS Name,
			COUNT(*) AS FileCount,
			GROUP_CONCAT(DISTINCT SystemID)
		 FROM matched
		 WHERE instr(Rest, '/') > 0
		 GROUP BY Name
		 ORDER BY Name ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("browse directories by system media query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.BrowseDirectoryResult
	for rows.Next() {
		var r database.BrowseDirectoryResult
		var systemIDs string
		if err := rows.Scan(&r.Name, &r.FileCount, &systemIDs); err != nil {
			return nil, fmt.Errorf("browse directories by system media scan: %w", err)
		}
		r.SystemIDs = splitBrowseSystemIDs(systemIDs)
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("browse directories by system media rows: %w", err)
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

	conditions := make([]string, 0, 3+len(letterClauses))
	conditions = append(conditions, `m.ParentDir = ?`, `m.IsMissing = 0`)
	conditions = append(conditions, letterClauses...)

	args = make([]any, 0, 1+len(letterArgs))
	args = append(args, opts.PathPrefix)
	args = append(args, letterArgs...)
	if systemClause, systemArgs := browseSystemFilterClause("s.SystemID", opts.Systems); systemClause != "" {
		conditions = append(conditions, systemClause)
		args = append(args, systemArgs...)
	}

	where = strings.Join(conditions, " AND ")
	return where, args
}

// browseSortClause returns the ORDER BY clause for the given sort option.
func browseSortClause(sortOrder string) string {
	switch sortOrder {
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
func browseCursorCondition(sortOrder string) string {
	switch sortOrder {
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
		INNER JOIN Systems s ON m.SystemDBID = s.DBID
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
	opts database.BrowseFileCountOptions,
) (int, error) {
	where, args := browseFilesBaseCondition(&database.BrowseFilesOptions{
		PathPrefix: opts.PathPrefix,
		Letter:     opts.Letter,
		Systems:    opts.Systems,
	})

	query := `
		SELECT COUNT(*)
		FROM Media m
		INNER JOIN MediaTitles mt ON m.MediaTitleDBID = mt.DBID
		INNER JOIN Systems s ON m.SystemDBID = s.DBID
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
	opts database.BrowseVirtualSchemesOptions,
) ([]database.BrowseVirtualScheme, error) {
	if len(opts.Systems) > 0 {
		return sqlBrowseVirtualSchemesForSystems(ctx, db, opts)
	}

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

func sqlBrowseVirtualSchemesForSystems(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseVirtualSchemesOptions,
) ([]database.BrowseVirtualScheme, error) {
	results, err := sqlBrowseVirtualSchemesForSystemsFromCache(ctx, db, opts)
	if err != nil {
		return nil, err
	}
	missingSystems := browseMissingSystems(opts.Systems, browseCoveredSystemIDsFromVirtualSchemes(results))
	if len(missingSystems) == 0 {
		return results, nil
	}

	mediaResults, err := sqlBrowseVirtualSchemesForSystemsFromMedia(ctx, db, database.BrowseVirtualSchemesOptions{
		Systems: missingSystems,
	})
	if err != nil {
		return nil, err
	}

	return mergeBrowseVirtualSchemes(results, mediaResults), nil
}

func browseCoveredSystemIDsFromVirtualSchemes(results []database.BrowseVirtualScheme) map[string]struct{} {
	covered := make(map[string]struct{})
	for _, result := range results {
		for _, systemID := range result.SystemIDs {
			covered[systemID] = struct{}{}
		}
	}
	return covered
}

func mergeBrowseVirtualSchemes(
	base []database.BrowseVirtualScheme,
	extra []database.BrowseVirtualScheme,
) []database.BrowseVirtualScheme {
	if len(extra) == 0 {
		return base
	}

	byScheme := make(map[string]*database.BrowseVirtualScheme, len(base)+len(extra))
	for i := range base {
		result := base[i]
		result.SystemIDs = uniqueBrowseSystemIDs(result.SystemIDs)
		byScheme[result.Scheme] = &result
	}
	for _, result := range extra {
		if existing, ok := byScheme[result.Scheme]; ok {
			existing.FileCount += result.FileCount
			existing.SystemIDs = uniqueBrowseSystemIDs(append(existing.SystemIDs, result.SystemIDs...))
			continue
		}
		result.SystemIDs = uniqueBrowseSystemIDs(result.SystemIDs)
		byScheme[result.Scheme] = &result
	}

	merged := make([]database.BrowseVirtualScheme, 0, len(byScheme))
	for _, result := range byScheme {
		merged = append(merged, *result)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Scheme < merged[j].Scheme })
	return merged
}

func sqlBrowseVirtualSchemesForSystemsFromCache(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseVirtualSchemesOptions,
) ([]database.BrowseVirtualScheme, error) {
	systemClause, args := browseSystemFilterClause("s.SystemID", opts.Systems)
	rows, err := db.QueryContext(ctx,
		`SELECT b.DirPath, SUM(b.FileCount), GROUP_CONCAT(DISTINCT s.SystemID)
		 FROM BrowseSystemCache b
		 INNER JOIN Systems s ON b.SystemDBID = s.DBID
		 WHERE b.IsVirtual = 1 AND `+systemClause+`
		 GROUP BY b.DirPath
		 ORDER BY b.DirPath ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("browse virtual schemes by system query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.BrowseVirtualScheme
	for rows.Next() {
		var r database.BrowseVirtualScheme
		var systemIDs string
		if err := rows.Scan(&r.Scheme, &r.FileCount, &systemIDs); err != nil {
			return nil, fmt.Errorf("browse virtual schemes by system scan: %w", err)
		}
		r.SystemIDs = splitBrowseSystemIDs(systemIDs)
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("browse virtual schemes by system rows: %w", err)
	}

	return results, nil
}

func sqlBrowseVirtualSchemesForSystemsFromMedia(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseVirtualSchemesOptions,
) ([]database.BrowseVirtualScheme, error) {
	systemClause, args := browseSystemFilterClause("s.SystemID", opts.Systems)
	rows, err := db.QueryContext(ctx,
		`SELECT substr(m.Path, 1, instr(m.Path, '://') + 2) AS Scheme,
			COUNT(*) AS FileCount,
			GROUP_CONCAT(DISTINCT s.SystemID)
		 FROM Media m
		 INNER JOIN Systems s ON m.SystemDBID = s.DBID
		 WHERE m.IsMissing = 0 AND instr(m.Path, '://') > 0 AND `+systemClause+`
		 GROUP BY Scheme
		 ORDER BY Scheme ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("browse virtual schemes by system media query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.BrowseVirtualScheme
	for rows.Next() {
		var r database.BrowseVirtualScheme
		var systemIDs string
		if err := rows.Scan(&r.Scheme, &r.FileCount, &systemIDs); err != nil {
			return nil, fmt.Errorf("browse virtual schemes by system media scan: %w", err)
		}
		r.SystemIDs = splitBrowseSystemIDs(systemIDs)
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("browse virtual schemes by system media rows: %w", err)
	}

	return results, nil
}

func browseRouteCacheKey(route string) string {
	if strings.Contains(route, "://") || route == "" || strings.HasSuffix(route, "/") {
		return route
	}
	return route + "/"
}

func sqlBrowseRouteCounts(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseRouteCountsOptions,
) (map[string]database.BrowseRouteCount, error) {
	if len(opts.Routes) == 0 || len(opts.Systems) == 0 {
		return make(map[string]database.BrowseRouteCount), nil
	}

	counts, err := sqlBrowseRouteCountsFromCache(ctx, db, opts)
	if err != nil {
		return nil, err
	}

	missingByRoute := make(map[string][]systemdefs.System, len(opts.Routes))
	for _, route := range opts.Routes {
		covered := make(map[string]struct{})
		if count, ok := counts[route]; ok {
			for _, systemID := range count.SystemIDs {
				covered[systemID] = struct{}{}
			}
		}
		if missingSystems := browseMissingSystems(opts.Systems, covered); len(missingSystems) > 0 {
			missingByRoute[route] = missingSystems
		}
	}
	if len(missingByRoute) == 0 {
		return counts, nil
	}

	for route, missingSystems := range missingByRoute {
		mediaCounts, err := sqlBrowseRouteCountsFromMedia(ctx, db, database.BrowseRouteCountsOptions{
			Systems: missingSystems,
			Routes:  []string{route},
		})
		if err != nil {
			return nil, err
		}
		mediaCount, ok := mediaCounts[route]
		if !ok {
			continue
		}
		if cachedCount, ok := counts[route]; ok {
			cachedCount.FileCount += mediaCount.FileCount
			cachedCount.SystemIDs = uniqueBrowseSystemIDs(append(cachedCount.SystemIDs, mediaCount.SystemIDs...))
			counts[route] = cachedCount
			continue
		}
		mediaCount.SystemIDs = uniqueBrowseSystemIDs(mediaCount.SystemIDs)
		counts[route] = mediaCount
	}

	return counts, nil
}

func uniqueBrowseSystemIDs(systemIDs []string) []string {
	if len(systemIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(systemIDs))
	unique := make([]string, 0, len(systemIDs))
	for _, systemID := range systemIDs {
		if _, ok := seen[systemID]; ok {
			continue
		}
		seen[systemID] = struct{}{}
		unique = append(unique, systemID)
	}
	return unique
}

func sqlBrowseRouteCountsFromCache(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseRouteCountsOptions,
) (map[string]database.BrowseRouteCount, error) {
	counts := make(map[string]database.BrowseRouteCount, len(opts.Routes))

	routeKeys := make([]string, len(opts.Routes))
	keyToRoute := make(map[string]string, len(opts.Routes))
	args := make([]any, 0, len(opts.Routes)+len(opts.Systems))
	for i, route := range opts.Routes {
		key := browseRouteCacheKey(route)
		routeKeys[i] = key
		keyToRoute[key] = route
		args = append(args, key)
	}

	routePlaceholders := make([]string, len(routeKeys))
	for i := range routePlaceholders {
		routePlaceholders[i] = "?"
	}
	systemClause, systemArgs := browseSystemFilterClause("s.SystemID", opts.Systems)
	args = append(args, systemArgs...)

	rows, err := db.QueryContext(ctx,
		`SELECT b.DirPath, SUM(b.FileCount), GROUP_CONCAT(DISTINCT s.SystemID)
		 FROM BrowseSystemCache b
		 INNER JOIN Systems s ON b.SystemDBID = s.DBID
		 WHERE b.DirPath IN (`+strings.Join(routePlaceholders, ",")+
			`) AND `+systemClause+`
		 GROUP BY b.DirPath`, args...)
	if err != nil {
		return nil, fmt.Errorf("browse route counts query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var dirPath string
		var count int
		var systemIDs string
		if err := rows.Scan(&dirPath, &count, &systemIDs); err != nil {
			return nil, fmt.Errorf("browse route counts scan: %w", err)
		}
		route, ok := keyToRoute[dirPath]
		if !ok {
			continue
		}
		counts[route] = database.BrowseRouteCount{
			Path:      route,
			FileCount: count,
			SystemIDs: splitBrowseSystemIDs(systemIDs),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("browse route counts rows: %w", err)
	}

	return counts, nil
}

func sqlBrowseRouteCountsFromMedia(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseRouteCountsOptions,
) (map[string]database.BrowseRouteCount, error) {
	counts := make(map[string]database.BrowseRouteCount, len(opts.Routes))
	if len(opts.Routes) == 0 || len(opts.Systems) == 0 {
		return counts, nil
	}

	systemClause, systemArgs := browseSystemFilterClause("s.SystemID", opts.Systems)
	for _, route := range opts.Routes {
		prefix := browseRouteCacheKey(route)
		args := make([]any, 0, 1+len(systemArgs))
		args = append(args, prefix)
		args = append(args, systemArgs...)

		var count int
		var systemIDs stdsql.NullString
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*), GROUP_CONCAT(DISTINCT s.SystemID)
			 FROM Media m
			 INNER JOIN Systems s ON m.SystemDBID = s.DBID
			 WHERE m.IsMissing = 0 AND m.Path LIKE ? || '%' AND `+systemClause,
			args...,
		).Scan(&count, &systemIDs); err != nil {
			return nil, fmt.Errorf("browse route counts media scan: %w", err)
		}
		if count == 0 {
			continue
		}

		counts[route] = database.BrowseRouteCount{
			Path:      route,
			FileCount: count,
			SystemIDs: splitBrowseSystemIDs(systemIDs.String),
		}
	}

	return counts, nil
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
