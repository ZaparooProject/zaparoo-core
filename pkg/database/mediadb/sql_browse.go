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
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/rs/zerolog/log"
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

func sqlBrowseCacheReady(ctx context.Context, db sqlQueryable) (bool, error) {
	var version string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigBrowseIndexVersion,
	).Scan(&version)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("browse cache readiness query: %w", err)
	}
	if version != browseCacheSchemaVersion {
		return false, nil
	}

	var exists int
	err = db.QueryRowContext(ctx, `SELECT 1 FROM BrowseDirs LIMIT 1`).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("browse cache table readiness query: %w", err)
	}
	return true, nil
}

func sqlBrowseDirID(ctx context.Context, db sqlQueryable, dirPath string) (id int64, ok bool, err error) {
	err = db.QueryRowContext(ctx, `SELECT DBID FROM BrowseDirs WHERE Path = ?`, dirPath).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("browse cache dir lookup: %w", err)
	}
	return id, true, nil
}

func splitBrowseSystemIDs(ids string) []string {
	if ids == "" {
		return nil
	}
	return uniqueBrowseSystemIDs(strings.Split(ids, ","))
}

func sqlBrowseDirectories(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseDirectoriesOptions,
) ([]database.BrowseDirectoryResult, error) {
	ready, err := sqlBrowseCacheReady(ctx, db)
	if err != nil {
		return nil, err
	}
	if ready {
		results, parentFound, cacheErr := sqlBrowseDirectoriesFromCache(ctx, db, opts)
		if cacheErr != nil || parentFound {
			return results, cacheErr
		}

		fallback, fallbackErr := sqlBrowseDirectoriesFromMediaFallback(ctx, db, opts)
		if fallbackErr != nil {
			return nil, fallbackErr
		}
		if len(fallback) > 0 {
			log.Warn().
				Str("pathPrefix", opts.PathPrefix).
				Strs("systems", browseSystemIDsForLog(opts.Systems)).
				Int("directories", len(fallback)).
				Msg("browse cache returned no directories; using media fallback")
		}
		return fallback, nil
	}
	return sqlBrowseDirectoriesFromMediaFallback(ctx, db, opts)
}

func sqlBrowseDirectoriesFromMediaFallback(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseDirectoriesOptions,
) ([]database.BrowseDirectoryResult, error) {
	if len(opts.Systems) > 0 {
		return sqlBrowseDirectoriesForSystemsFromMedia(ctx, db, opts)
	}
	return sqlBrowseDirectoriesFromMedia(ctx, db, opts.PathPrefix)
}

func browseSystemIDsForLog(systems []systemdefs.System) []string {
	if len(systems) == 0 {
		return nil
	}
	systemIDs := make([]string, 0, len(systems))
	for i := range systems {
		systemIDs = append(systemIDs, systems[i].ID)
	}
	return systemIDs
}

func sqlBrowseDirectoriesFromCache(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseDirectoriesOptions,
) ([]database.BrowseDirectoryResult, bool, error) {
	parentID, ok, err := sqlBrowseDirID(ctx, db, opts.PathPrefix)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	if len(opts.Systems) == 1 {
		results, cacheErr := sqlBrowseDirectoriesFromCacheForSingleSystem(ctx, db, parentID, opts.Systems[0].ID)
		return results, true, cacheErr
	}

	args := []any{parentID}
	systemClause, systemArgs := browseSystemFilterClause("s.SystemID", opts.Systems)
	query := `SELECT d.Name, SUM(c.FileCount), GROUP_CONCAT(DISTINCT s.SystemID)
		FROM BrowseDirCounts c
		INNER JOIN BrowseDirs d ON c.ChildDirDBID = d.DBID
		INNER JOIN Systems s ON c.SystemDBID = s.DBID
		WHERE c.ParentDirDBID = ? AND c.ChildDirDBID != c.ParentDirDBID AND d.IsVirtual = 0`
	if systemClause != "" {
		query += ` AND ` + systemClause
		args = append(args, systemArgs...)
	}
	query += ` GROUP BY d.DBID, d.Name ORDER BY d.Name ASC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, true, fmt.Errorf("browse cache directories query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.BrowseDirectoryResult
	for rows.Next() {
		var r database.BrowseDirectoryResult
		var systemIDs string
		if scanErr := rows.Scan(&r.Name, &r.FileCount, &systemIDs); scanErr != nil {
			return nil, true, fmt.Errorf("browse cache directories scan: %w", scanErr)
		}
		r.SystemIDs = splitBrowseSystemIDs(systemIDs)
		results = append(results, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, true, fmt.Errorf("browse cache directories rows: %w", rowsErr)
	}
	return results, true, nil
}

func sqlBrowseDirectoriesFromCacheForSingleSystem(
	ctx context.Context,
	db sqlQueryable,
	parentID int64,
	systemID string,
) ([]database.BrowseDirectoryResult, error) {
	rows, err := db.QueryContext(ctx, `SELECT d.Name, c.FileCount
		FROM BrowseDirCounts c
		INNER JOIN BrowseDirs d ON c.ChildDirDBID = d.DBID
		INNER JOIN Systems s ON c.SystemDBID = s.DBID
		WHERE c.ParentDirDBID = ?
			AND c.ChildDirDBID != c.ParentDirDBID
			AND d.IsVirtual = 0
			AND s.SystemID = ?
		ORDER BY d.Name ASC`, parentID, systemID)
	if err != nil {
		return nil, fmt.Errorf("browse cache single-system directories query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.BrowseDirectoryResult
	for rows.Next() {
		var r database.BrowseDirectoryResult
		if scanErr := rows.Scan(&r.Name, &r.FileCount); scanErr != nil {
			return nil, fmt.Errorf("browse cache single-system directories scan: %w", scanErr)
		}
		r.SystemIDs = []string{systemID}
		results = append(results, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("browse cache single-system directories rows: %w", rowsErr)
	}
	return results, nil
}

func sqlBrowseDirectoriesFromMedia(
	ctx context.Context,
	db sqlQueryable,
	pathPrefix string,
) ([]database.BrowseDirectoryResult, error) {
	pathCondition, pathArgs := browsePathPrefixCondition("Path", pathPrefix)
	args := append([]any{pathPrefix}, pathArgs...)
	rows, err := db.QueryContext(ctx,
		`WITH matched AS (
			 SELECT substr(Path, length(?) + 1) AS Rest
			 FROM Media
			 WHERE IsMissing = 0 AND `+pathCondition+`
		 )
		 SELECT substr(Rest, 1, instr(Rest, '/') - 1) AS Name,
			COUNT(*) AS FileCount
		 FROM matched
		 WHERE instr(Rest, '/') > 0
		 GROUP BY Name
		 ORDER BY Name ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("browse directories media query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.BrowseDirectoryResult
	for rows.Next() {
		var r database.BrowseDirectoryResult
		if scanErr := rows.Scan(&r.Name, &r.FileCount); scanErr != nil {
			return nil, fmt.Errorf("browse directories media scan: %w", scanErr)
		}
		results = append(results, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("browse directories media rows: %w", rowsErr)
	}
	return results, nil
}

func sqlBrowseDirectoriesForSystemsFromMedia(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseDirectoriesOptions,
) ([]database.BrowseDirectoryResult, error) {
	systemClause, systemArgs := browseSystemFilterClause("s.SystemID", opts.Systems)
	pathCondition, pathArgs := browsePathPrefixCondition("m.Path", opts.PathPrefix)
	args := make([]any, 0, 1+len(pathArgs)+len(systemArgs))
	args = append(args, opts.PathPrefix)
	args = append(args, pathArgs...)
	args = append(args, systemArgs...)
	rows, err := db.QueryContext(ctx,
		`WITH matched AS (
			 SELECT substr(m.Path, length(?) + 1) AS Rest, s.SystemID
			 FROM Media m
			 INNER JOIN Systems s ON m.SystemDBID = s.DBID
			 WHERE m.IsMissing = 0 AND `+pathCondition+` AND `+systemClause+`
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
		if scanErr := rows.Scan(&r.Name, &r.FileCount, &systemIDs); scanErr != nil {
			return nil, fmt.Errorf("browse directories by system media scan: %w", scanErr)
		}
		r.SystemIDs = splitBrowseSystemIDs(systemIDs)
		results = append(results, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("browse directories by system media rows: %w", rowsErr)
	}
	return results, nil
}

func browsePathPrefixCondition(column string, pathPrefix string) (string, []any) {
	if upper := stringPrefixUpperBound(pathPrefix); upper != "" {
		return column + ` >= ? AND ` + column + ` < ?`, []any{pathPrefix, upper}
	}
	return column + ` LIKE ? || '%'`, []any{pathPrefix}
}

func browseFilesBaseCondition(opts *database.BrowseFilesOptions) (where string, args []any) {
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

	return strings.Join(conditions, " AND "), args
}

func browseSortClause(sortOrder string) string {
	switch sortOrder {
	case "name-desc":
		return "mt.Name DESC, m.DBID DESC"
	case "filename-asc":
		return "m.Path ASC, m.DBID ASC"
	case "filename-desc":
		return "m.Path DESC, m.DBID DESC"
	default:
		return "mt.Name ASC, m.DBID ASC"
	}
}

func browseCursorCondition(sortOrder string) string {
	switch sortOrder {
	case "name-desc":
		return ` AND (mt.Name, m.DBID) < (?, ?)`
	case "filename-asc":
		return ` AND (m.Path, m.DBID) > (?, ?)`
	case "filename-desc":
		return ` AND (m.Path, m.DBID) < (?, ?)`
	default:
		return ` AND (mt.Name, m.DBID) > (?, ?)`
	}
}

func sqlBrowseFiles(
	ctx context.Context,
	db sqlQueryable,
	opts *database.BrowseFilesOptions,
) ([]database.SearchResultWithCursor, error) {
	return sqlBrowseFilesFromMedia(ctx, db, opts)
}

func sqlBrowseFilesFromMedia(
	ctx context.Context,
	db sqlQueryable,
	opts *database.BrowseFilesOptions,
) ([]database.SearchResultWithCursor, error) {
	where, args := browseFilesBaseCondition(opts)
	query := `SELECT s.SystemID, mt.Name, m.Path, m.DBID, mt.DBID
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

	queryStarted := time.Now()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("browse files query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.SearchResultWithCursor
	for rows.Next() {
		var r database.SearchResultWithCursor
		if scanErr := rows.Scan(&r.SystemID, &r.Name, &r.Path, &r.MediaID, &r.MediaTitleID); scanErr != nil {
			return nil, fmt.Errorf("browse files scan: %w", scanErr)
		}
		results = append(results, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("browse files rows: %w", rowsErr)
	}
	queryElapsed := time.Since(queryStarted)

	tagsStarted := time.Now()
	if err := fetchAndAttachTags(ctx, db, results); err != nil {
		return nil, fmt.Errorf("browse files tags: %w", err)
	}
	tagsElapsed := time.Since(tagsStarted)

	log.Debug().
		Str("pathPrefix", opts.PathPrefix).
		Strs("systems", browseSystemIDsForLog(opts.Systems)).
		Int("rows", len(results)).
		Dur("queryDuration", queryElapsed).
		Dur("tagsDuration", tagsElapsed).
		Msg("browse files step timing")
	return results, nil
}

func sqlBrowseFileCount(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseFileCountOptions,
) (int, error) {
	return sqlBrowseFileCountFromMedia(ctx, db, opts)
}

func sqlBrowseFileCountFromMedia(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseFileCountOptions,
) (int, error) {
	where, args := browseFilesBaseCondition(&database.BrowseFilesOptions{
		PathPrefix: opts.PathPrefix,
		Letter:     opts.Letter,
		Systems:    opts.Systems,
	})
	query := `SELECT COUNT(*)
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

func sqlBrowseVirtualSchemes(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseVirtualSchemesOptions,
) ([]database.BrowseVirtualScheme, error) {
	ready, err := sqlBrowseCacheReady(ctx, db)
	if err != nil {
		return nil, err
	}
	if ready {
		return sqlBrowseVirtualSchemesFromCache(ctx, db, opts)
	}
	if len(opts.Systems) > 0 {
		return sqlBrowseVirtualSchemesForSystemsFromMedia(ctx, db, opts)
	}
	return sqlBrowseVirtualSchemesFromMedia(ctx, db)
}

func sqlBrowseVirtualSchemesFromCache(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseVirtualSchemesOptions,
) ([]database.BrowseVirtualScheme, error) {
	rootID, ok, err := sqlBrowseDirID(ctx, db, "")
	if err != nil || !ok {
		return nil, err
	}
	args := []any{rootID}
	systemClause, systemArgs := browseSystemFilterClause("s.SystemID", opts.Systems)
	query := `SELECT d.Path, SUM(c.FileCount), GROUP_CONCAT(DISTINCT s.SystemID)
		FROM BrowseDirCounts c
		INNER JOIN BrowseDirs d ON c.ChildDirDBID = d.DBID
		INNER JOIN Systems s ON c.SystemDBID = s.DBID
		WHERE c.ParentDirDBID = ? AND d.IsVirtual = 1`
	if systemClause != "" {
		query += ` AND ` + systemClause
		args = append(args, systemArgs...)
	}
	query += ` GROUP BY d.DBID, d.Path ORDER BY d.Path ASC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("browse cache virtual schemes query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.BrowseVirtualScheme
	for rows.Next() {
		var r database.BrowseVirtualScheme
		var systemIDs string
		if scanErr := rows.Scan(&r.Scheme, &r.FileCount, &systemIDs); scanErr != nil {
			return nil, fmt.Errorf("browse cache virtual schemes scan: %w", scanErr)
		}
		r.SystemIDs = splitBrowseSystemIDs(systemIDs)
		results = append(results, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("browse cache virtual schemes rows: %w", rowsErr)
	}
	return results, nil
}

func sqlBrowseVirtualSchemesFromMedia(ctx context.Context, db sqlQueryable) ([]database.BrowseVirtualScheme, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT substr(Path, 1, instr(Path, '://') + 2) AS Scheme,
			COUNT(*) AS FileCount
		 FROM Media
		 WHERE IsMissing = 0 AND instr(Path, '://') > 0
		 GROUP BY Scheme
		 ORDER BY Scheme ASC`)
	if err != nil {
		return nil, fmt.Errorf("browse virtual schemes media query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.BrowseVirtualScheme
	for rows.Next() {
		var r database.BrowseVirtualScheme
		if scanErr := rows.Scan(&r.Scheme, &r.FileCount); scanErr != nil {
			return nil, fmt.Errorf("browse virtual schemes media scan: %w", scanErr)
		}
		results = append(results, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("browse virtual schemes media rows: %w", rowsErr)
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
		if scanErr := rows.Scan(&r.Scheme, &r.FileCount, &systemIDs); scanErr != nil {
			return nil, fmt.Errorf("browse virtual schemes by system media scan: %w", scanErr)
		}
		r.SystemIDs = splitBrowseSystemIDs(systemIDs)
		results = append(results, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("browse virtual schemes by system media rows: %w", rowsErr)
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
	ready, err := sqlBrowseCacheReady(ctx, db)
	if err != nil {
		return nil, err
	}
	if ready {
		return sqlBrowseRouteCountsFromCache(ctx, db, opts)
	}
	return sqlBrowseRouteCountsFromMedia(ctx, db, opts)
}

func sqlBrowseRouteCountsFromCache(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseRouteCountsOptions,
) (map[string]database.BrowseRouteCount, error) {
	counts := make(map[string]database.BrowseRouteCount, len(opts.Routes))
	systemClause, systemArgs := browseSystemFilterClause("s.SystemID", opts.Systems)
	for _, route := range opts.Routes {
		dirID, ok, err := sqlBrowseDirID(ctx, db, browseRouteCacheKey(route))
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		args := append([]any{dirID}, systemArgs...)
		var count int
		var systemIDs sql.NullString
		err = db.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(c.FileCount), 0), GROUP_CONCAT(DISTINCT s.SystemID)
			 FROM BrowseDirCounts c
			 INNER JOIN Systems s ON c.SystemDBID = s.DBID
			 WHERE c.ChildDirDBID = ? AND `+systemClause,
			args...,
		).Scan(&count, &systemIDs)
		if err != nil {
			return nil, fmt.Errorf("browse cache route counts query: %w", err)
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
		args := append([]any{prefix}, systemArgs...)
		var count int
		var systemIDs sql.NullString
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

// sqlBrowseSystemRootCandidates resolves a list of filesystem roots
// against the BrowseDirCounts cache in two queries, regardless of how many
// roots the platform has.
//
// HasMedia[root] is true when the root has any media in its subtree (direct
// files or any descendant subdir) for the requested systems; Children[root]
// holds the immediate subdir names that themselves contain media. Roots
// absent from the cache (not indexed at all) are absent from both maps;
// callers should treat them as "no media" rather than "cache miss". The
// cacheReady return reflects only whether the BrowseDirs/BrowseDirCounts
// tables are populated.
func sqlBrowseSystemRootCandidates(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseSystemRootCandidatesOptions,
) (database.BrowseSystemRootCandidates, bool, error) {
	result := database.BrowseSystemRootCandidates{
		Children: make(map[string][]string),
		HasMedia: make(map[string]bool),
	}
	if len(opts.Roots) == 0 || len(opts.Systems) == 0 {
		return result, true, nil
	}
	ready, err := sqlBrowseCacheReady(ctx, db)
	if err != nil {
		return result, false, err
	}
	if !ready {
		return result, false, nil
	}

	// Map cache-key path → original input root: BrowseDirs stores paths in
	// trailing-slash form, so we look up by that key but return results keyed
	// by the caller's input form. Callers should pre-normalize their roots
	// (e.g. via filepath.Clean): when two distinct input strings collide on
	// browseRouteCacheKey (such as "/media/fat" and "/media/fat/"), only the
	// first form is retained in the result map and the second is dropped.
	rootByKey := make(map[string]string, len(opts.Roots))
	rootKeyPlaceholders := make([]string, 0, len(opts.Roots))
	rootKeyArgs := make([]any, 0, len(opts.Roots))
	for _, root := range opts.Roots {
		key := browseRouteCacheKey(root)
		if _, dup := rootByKey[key]; dup {
			continue
		}
		rootByKey[key] = root
		rootKeyPlaceholders = append(rootKeyPlaceholders, "?")
		rootKeyArgs = append(rootKeyArgs, key)
	}
	rootIN := strings.Join(rootKeyPlaceholders, ",")

	systemClause, systemArgs := browseSystemFilterClause("s.SystemID", opts.Systems)
	systemWhere := ""
	if systemClause != "" {
		systemWhere = ` AND ` + systemClause
	}

	if err := loadBrowseSystemRootHasMedia(
		ctx, db, rootIN, rootKeyArgs, systemWhere, systemArgs, rootByKey, &result,
	); err != nil {
		return result, true, err
	}
	if err := loadBrowseSystemRootChildren(
		ctx, db, rootIN, rootKeyArgs, systemWhere, systemArgs, rootByKey, &result,
	); err != nil {
		return result, true, err
	}
	for root := range result.Children {
		sort.Strings(result.Children[root])
	}
	return result, true, nil
}

// loadBrowseSystemRootHasMedia populates HasMedia[root] for any roots whose
// subtree contains media for the requested systems. For any indexed media
// file, every ancestor dir (including the root itself when it's an ancestor)
// is recorded as a CHILD in some (ancestor_parent, ancestor) pair, so a row
// with cd.Path = root means the root's subtree has media. The "/" root
// self-loop also matches.
func loadBrowseSystemRootHasMedia(
	ctx context.Context,
	db sqlQueryable,
	rootIN string,
	rootKeyArgs []any,
	systemWhere string,
	systemArgs []any,
	rootByKey map[string]string,
	result *database.BrowseSystemRootCandidates,
) error {
	args := make([]any, 0, len(rootKeyArgs)+len(systemArgs))
	args = append(args, rootKeyArgs...)
	args = append(args, systemArgs...)
	query := `SELECT DISTINCT cd.Path
		FROM BrowseDirs cd
		INNER JOIN BrowseDirCounts c ON c.ChildDirDBID = cd.DBID
		INNER JOIN Systems s ON c.SystemDBID = s.DBID
		WHERE cd.Path IN (` + rootIN + `)` + systemWhere
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("browse cache root has-media query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key string
		if scanErr := rows.Scan(&key); scanErr != nil {
			return fmt.Errorf("browse cache root has-media scan: %w", scanErr)
		}
		if root, ok := rootByKey[key]; ok {
			result.HasMedia[root] = true
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("browse cache root has-media rows: %w", rowsErr)
	}
	return nil
}

// loadBrowseSystemRootChildren populates Children[root] with immediate
// non-virtual subdir names that themselves contain media for the requested
// systems. Excludes the "/" self-loop.
func loadBrowseSystemRootChildren(
	ctx context.Context,
	db sqlQueryable,
	rootIN string,
	rootKeyArgs []any,
	systemWhere string,
	systemArgs []any,
	rootByKey map[string]string,
	result *database.BrowseSystemRootCandidates,
) error {
	args := make([]any, 0, len(rootKeyArgs)+len(systemArgs))
	args = append(args, rootKeyArgs...)
	args = append(args, systemArgs...)
	query := `SELECT DISTINCT pd.Path, cd.Name
		FROM BrowseDirs pd
		INNER JOIN BrowseDirCounts c ON c.ParentDirDBID = pd.DBID
		INNER JOIN BrowseDirs cd ON c.ChildDirDBID = cd.DBID
		INNER JOIN Systems s ON c.SystemDBID = s.DBID
		WHERE pd.Path IN (` + rootIN + `)
			AND cd.IsVirtual = 0
			AND c.ChildDirDBID != c.ParentDirDBID` + systemWhere
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("browse cache root children query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var parentKey, name string
		if scanErr := rows.Scan(&parentKey, &name); scanErr != nil {
			return fmt.Errorf("browse cache root children scan: %w", scanErr)
		}
		root, ok := rootByKey[parentKey]
		if !ok || name == "" {
			continue
		}
		result.HasMedia[root] = true
		result.Children[root] = append(result.Children[root], name)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("browse cache root children rows: %w", rowsErr)
	}
	return nil
}

func sqlBrowseRootCounts(ctx context.Context, db sqlQueryable, rootDirs []string) (map[string]*int, error) {
	counts := make(map[string]*int, len(rootDirs))
	for _, root := range rootDirs {
		counts[root] = nil
	}
	if len(rootDirs) == 0 {
		return counts, nil
	}
	ready, err := sqlBrowseCacheReady(ctx, db)
	if err != nil {
		return nil, err
	}
	if !ready {
		return counts, nil
	}
	for _, root := range rootDirs {
		count := 0
		counts[root] = &count
		dirID, ok, lookupErr := sqlBrowseDirID(ctx, db, browseRouteCacheKey(root))
		if lookupErr != nil {
			return nil, lookupErr
		}
		if !ok {
			continue
		}
		var dbCount int
		if scanErr := db.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(FileCount), 0) FROM BrowseDirCounts WHERE ChildDirDBID = ?`,
			dirID,
		).Scan(&dbCount); scanErr != nil {
			return nil, fmt.Errorf("browse cache root counts query: %w", scanErr)
		}
		c := dbCount
		counts[root] = &c
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
	sort.Strings(unique)
	return unique
}
