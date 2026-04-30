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

func sqlBrowseV2Ready(ctx context.Context, db sqlQueryable) (bool, error) {
	var version string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigBrowseIndexVersion,
	).Scan(&version)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("browse v2 readiness query: %w", err)
	}
	if version != browseIndexVersion {
		return false, nil
	}

	var exists int
	err = db.QueryRowContext(ctx, `SELECT 1 FROM BrowseDirs LIMIT 1`).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("browse v2 table readiness query: %w", err)
	}
	return true, nil
}

func sqlBrowseDirID(ctx context.Context, db sqlQueryable, dirPath string) (id int64, ok bool, err error) {
	err = db.QueryRowContext(ctx, `SELECT DBID FROM BrowseDirs WHERE Path = ?`, dirPath).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("browse v2 dir lookup: %w", err)
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
	ready, err := sqlBrowseV2Ready(ctx, db)
	if err != nil {
		return nil, err
	}
	if ready {
		return sqlBrowseDirectoriesV2(ctx, db, opts)
	}
	if len(opts.Systems) > 0 {
		return sqlBrowseDirectoriesForSystemsFromMedia(ctx, db, opts)
	}
	return sqlBrowseDirectoriesFromMedia(ctx, db, opts.PathPrefix)
}

func sqlBrowseDirectoriesV2(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseDirectoriesOptions,
) ([]database.BrowseDirectoryResult, error) {
	parentID, ok, err := sqlBrowseDirID(ctx, db, opts.PathPrefix)
	if err != nil || !ok {
		return nil, err
	}
	if len(opts.Systems) == 1 {
		return sqlBrowseDirectoriesV2ForSingleSystem(ctx, db, parentID, opts.Systems[0].ID)
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
		return nil, fmt.Errorf("browse v2 directories query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.BrowseDirectoryResult
	for rows.Next() {
		var r database.BrowseDirectoryResult
		var systemIDs string
		if scanErr := rows.Scan(&r.Name, &r.FileCount, &systemIDs); scanErr != nil {
			return nil, fmt.Errorf("browse v2 directories scan: %w", scanErr)
		}
		r.SystemIDs = splitBrowseSystemIDs(systemIDs)
		results = append(results, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("browse v2 directories rows: %w", rowsErr)
	}
	return results, nil
}

func sqlBrowseDirectoriesV2ForSingleSystem(
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
		return nil, fmt.Errorf("browse v2 single-system directories query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.BrowseDirectoryResult
	for rows.Next() {
		var r database.BrowseDirectoryResult
		if scanErr := rows.Scan(&r.Name, &r.FileCount); scanErr != nil {
			return nil, fmt.Errorf("browse v2 single-system directories scan: %w", scanErr)
		}
		r.SystemIDs = []string{systemID}
		results = append(results, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("browse v2 single-system directories rows: %w", rowsErr)
	}
	return results, nil
}

func sqlBrowseDirectoriesFromMedia(
	ctx context.Context,
	db sqlQueryable,
	pathPrefix string,
) ([]database.BrowseDirectoryResult, error) {
	rows, err := db.QueryContext(ctx,
		`WITH matched AS (
			 SELECT substr(Path, length(?) + 1) AS Rest
			 FROM Media
			 WHERE IsMissing = 0 AND Path LIKE ? || '%'
		 )
		 SELECT substr(Rest, 1, instr(Rest, '/') - 1) AS Name,
			COUNT(*) AS FileCount
		 FROM matched
		 WHERE instr(Rest, '/') > 0
		 GROUP BY Name
		 ORDER BY Name ASC`,
		pathPrefix, pathPrefix,
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
	args := make([]any, 0, 2+len(systemArgs))
	args = append(args, opts.PathPrefix, opts.PathPrefix)
	args = append(args, systemArgs...)
	rows, err := db.QueryContext(ctx,
		`WITH matched AS (
			 SELECT substr(m.Path, length(?) + 1) AS Rest, s.SystemID
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

func browseV2FilesBaseCondition(opts *database.BrowseFilesOptions, parentID int64) (where string, args []any) {
	conditions := []string{`b.ParentDirDBID = ?`}
	args = []any{parentID}
	if letterClause, letterArgs := buildBrowseNameCharFilter("b.NameFirstChar", opts.Letter); letterClause != "" {
		conditions = append(conditions, letterClause)
		args = append(args, letterArgs...)
	}
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

func browseV2SortClause(sortOrder string) string {
	switch sortOrder {
	case "name-desc":
		return "b.Name DESC, b.MediaDBID DESC"
	case "filename-asc":
		return "b.FileName ASC, b.MediaDBID ASC"
	case "filename-desc":
		return "b.FileName DESC, b.MediaDBID DESC"
	default:
		return "b.Name ASC, b.MediaDBID ASC"
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

func browseV2CursorCondition(sortOrder string) string {
	switch sortOrder {
	case "name-desc":
		return ` AND (b.Name, b.MediaDBID) < (?, ?)`
	case "filename-asc":
		return ` AND (b.FileName, b.MediaDBID) > (?, ?)`
	case "filename-desc":
		return ` AND (b.FileName, b.MediaDBID) < (?, ?)`
	default:
		return ` AND (b.Name, b.MediaDBID) > (?, ?)`
	}
}

func browseV2CursorSortValue(opts *database.BrowseFilesOptions) string {
	if opts.Cursor == nil {
		return ""
	}
	if opts.Sort != "filename-asc" && opts.Sort != "filename-desc" {
		return opts.Cursor.SortValue
	}
	_, fileName := browseV2ParentAndFileName(opts.Cursor.SortValue)
	return fileName
}

func sqlBrowseFiles(
	ctx context.Context,
	db sqlQueryable,
	opts *database.BrowseFilesOptions,
) ([]database.SearchResultWithCursor, error) {
	ready, err := sqlBrowseV2Ready(ctx, db)
	if err != nil {
		return nil, err
	}
	if ready {
		return sqlBrowseFilesV2(ctx, db, opts)
	}
	return sqlBrowseFilesFromMedia(ctx, db, opts)
}

func sqlBrowseFilesV2(
	ctx context.Context,
	db sqlQueryable,
	opts *database.BrowseFilesOptions,
) ([]database.SearchResultWithCursor, error) {
	parentID, ok, err := sqlBrowseDirID(ctx, db, opts.PathPrefix)
	if err != nil || !ok {
		return nil, err
	}
	where, args := browseV2FilesBaseCondition(opts, parentID)
	query := `SELECT s.SystemID, b.Name, m.Path, b.MediaDBID
		FROM BrowseEntries b
		INNER JOIN Media m ON b.MediaDBID = m.DBID
		INNER JOIN Systems s ON b.SystemDBID = s.DBID
		WHERE ` + where
	if opts.Cursor != nil {
		query += browseV2CursorCondition(opts.Sort)
		args = append(args, browseV2CursorSortValue(opts), opts.Cursor.LastID)
	}
	query += ` ORDER BY ` + browseV2SortClause(opts.Sort) + ` LIMIT ?`
	args = append(args, opts.Limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("browse v2 files query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.SearchResultWithCursor
	for rows.Next() {
		var r database.SearchResultWithCursor
		if scanErr := rows.Scan(&r.SystemID, &r.Name, &r.Path, &r.MediaID); scanErr != nil {
			return nil, fmt.Errorf("browse v2 files scan: %w", scanErr)
		}
		results = append(results, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("browse v2 files rows: %w", rowsErr)
	}
	if err := fetchAndAttachTags(ctx, db, results); err != nil {
		return nil, fmt.Errorf("browse v2 files tags: %w", err)
	}
	return results, nil
}

func sqlBrowseFilesFromMedia(
	ctx context.Context,
	db sqlQueryable,
	opts *database.BrowseFilesOptions,
) ([]database.SearchResultWithCursor, error) {
	where, args := browseFilesBaseCondition(opts)
	query := `SELECT s.SystemID, mt.Name, m.Path, m.DBID
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
		if scanErr := rows.Scan(&r.SystemID, &r.Name, &r.Path, &r.MediaID); scanErr != nil {
			return nil, fmt.Errorf("browse files scan: %w", scanErr)
		}
		results = append(results, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("browse files rows: %w", rowsErr)
	}
	if err := fetchAndAttachTags(ctx, db, results); err != nil {
		return nil, fmt.Errorf("browse files tags: %w", err)
	}
	return results, nil
}

func sqlBrowseFileCount(
	ctx context.Context,
	db sqlQueryable,
	opts database.BrowseFileCountOptions,
) (int, error) {
	ready, err := sqlBrowseV2Ready(ctx, db)
	if err != nil {
		return 0, err
	}
	if ready {
		return sqlBrowseFileCountV2(ctx, db, opts)
	}
	return sqlBrowseFileCountFromMedia(ctx, db, opts)
}

func sqlBrowseFileCountV2(ctx context.Context, db sqlQueryable, opts database.BrowseFileCountOptions) (int, error) {
	parentID, ok, err := sqlBrowseDirID(ctx, db, opts.PathPrefix)
	if err != nil || !ok {
		return 0, err
	}
	where, args := browseV2FilesBaseCondition(&database.BrowseFilesOptions{
		PathPrefix: opts.PathPrefix,
		Letter:     opts.Letter,
		Systems:    opts.Systems,
	}, parentID)
	query := `SELECT COUNT(*) FROM BrowseEntries b INNER JOIN Systems s ON b.SystemDBID = s.DBID WHERE ` + where
	var count int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("browse v2 file count: %w", err)
	}
	return count, nil
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
	ready, err := sqlBrowseV2Ready(ctx, db)
	if err != nil {
		return nil, err
	}
	if ready {
		return sqlBrowseVirtualSchemesV2(ctx, db, opts)
	}
	if len(opts.Systems) > 0 {
		return sqlBrowseVirtualSchemesForSystemsFromMedia(ctx, db, opts)
	}
	return sqlBrowseVirtualSchemesFromMedia(ctx, db)
}

func sqlBrowseVirtualSchemesV2(
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
		return nil, fmt.Errorf("browse v2 virtual schemes query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.BrowseVirtualScheme
	for rows.Next() {
		var r database.BrowseVirtualScheme
		var systemIDs string
		if scanErr := rows.Scan(&r.Scheme, &r.FileCount, &systemIDs); scanErr != nil {
			return nil, fmt.Errorf("browse v2 virtual schemes scan: %w", scanErr)
		}
		r.SystemIDs = splitBrowseSystemIDs(systemIDs)
		results = append(results, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("browse v2 virtual schemes rows: %w", rowsErr)
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
	ready, err := sqlBrowseV2Ready(ctx, db)
	if err != nil {
		return nil, err
	}
	if ready {
		return sqlBrowseRouteCountsV2(ctx, db, opts)
	}
	return sqlBrowseRouteCountsFromMedia(ctx, db, opts)
}

func sqlBrowseRouteCountsV2(
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
			return nil, fmt.Errorf("browse v2 route counts query: %w", err)
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

func sqlBrowseRootCounts(ctx context.Context, db sqlQueryable, rootDirs []string) (map[string]*int, error) {
	counts := make(map[string]*int, len(rootDirs))
	for _, root := range rootDirs {
		counts[root] = nil
	}
	if len(rootDirs) == 0 {
		return counts, nil
	}
	ready, err := sqlBrowseV2Ready(ctx, db)
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
			return nil, fmt.Errorf("browse v2 root counts query: %w", scanErr)
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
