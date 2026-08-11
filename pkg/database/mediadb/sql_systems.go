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
	"errors"
	"fmt"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/rs/zerolog/log"
)

const insertSystemSQL = `INSERT INTO Systems (DBID, SystemID, Name) VALUES (?, ?, ?)`

func sqlFindSystem(ctx context.Context, db sqlQueryable, system database.System) (database.System, error) {
	var row database.System
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, SystemID, Name
		from Systems
		where DBID = ?
		or SystemID = ?
		limit 1;
	`)
	if err != nil {
		return row, fmt.Errorf("failed to prepare find system statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx,
		system.DBID,
		system.SystemID,
	).Scan(
		&row.DBID,
		&row.SystemID,
		&row.Name,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan system row: %w", err)
	}
	return row, nil
}

func sqlFindSystemBySystemID(ctx context.Context, db sqlQueryable, systemID string) (database.System, error) {
	var row database.System
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, SystemID, Name
		from Systems
		where SystemID = ?
		limit 1;
	`)
	if err != nil {
		return row, fmt.Errorf("failed to prepare find system by system ID statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx, systemID).Scan(
		&row.DBID,
		&row.SystemID,
		&row.Name,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return row, sql.ErrNoRows
	}
	if err != nil {
		return row, fmt.Errorf("failed to scan system row: %w", err)
	}
	return row, nil
}

func sqlInsertSystemWithPreparedStmt(
	ctx context.Context, stmt *sql.Stmt, row database.System,
) (database.System, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	res, err := stmt.ExecContext(ctx, dbID, row.SystemID, row.Name)
	if err != nil {
		return row, fmt.Errorf("failed to execute prepared insert system statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for system: %w", err)
	}

	row.DBID = lastID
	return row, nil
}

func sqlInsertSystem(ctx context.Context, db *sql.DB, row database.System) (database.System, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	stmt, err := db.PrepareContext(ctx, insertSystemSQL)
	if err != nil {
		return row, fmt.Errorf("failed to prepare insert system statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	res, err := stmt.ExecContext(ctx, dbID, row.SystemID, row.Name)
	if err != nil {
		return row, fmt.Errorf("failed to execute insert system statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for system: %w", err)
	}

	row.DBID = lastID
	return row, nil
}

func sqlSystemIndexed(ctx context.Context, db *sql.DB, system *systemdefs.System) bool {
	systemID := ""
	q, err := db.PrepareContext(ctx, `
		select
		SystemID
		from Systems
		where SystemID = ?;
	`)
	if err != nil {
		return false
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = q.QueryRowContext(ctx, system.ID).Scan(&systemID)
	if err != nil {
		return false
	}
	return systemID == system.ID
}

func sqlIndexedSystems(ctx context.Context, db *sql.DB) ([]string, error) {
	if state, err := sqlBrowseCacheStatus(ctx, db); err == nil && sqlBrowseCacheServeable(state) {
		list, cacheErr := sqlIndexedSystemsFromBrowseCache(ctx, db)
		if cacheErr == nil {
			return list, nil
		}
		log.Debug().Err(cacheErr).Msg("failed to read indexed systems from browse cache")
	} else if err != nil {
		log.Debug().Err(err).Msg("failed to check browse cache for indexed systems")
	}

	list := make([]string, 0)

	q, err := db.PrepareContext(ctx, `
		SELECT s.SystemID
		FROM Systems s
		WHERE EXISTS (
			SELECT 1
			FROM Media m
			WHERE m.SystemDBID = s.DBID
			  AND m.IsMissing = 0
		)
		ORDER BY s.SystemID;
	`)
	if err != nil {
		return list, fmt.Errorf("failed to prepare indexed systems query: %w", err)
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := q.QueryContext(ctx)
	if err != nil {
		return list, fmt.Errorf("failed to execute indexed systems query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	for rows.Next() {
		row := ""
		if scanErr := rows.Scan(&row); scanErr != nil {
			return list, fmt.Errorf("failed to scan indexed systems result: %w", scanErr)
		}
		list = append(list, row)
	}
	err = rows.Err()
	return list, err
}

func buildNegativeOnlySystemMediaCountsQuery(
	tags []zapscript.TagFilter,
) (query string, args []any) {
	forbidden := make([]zapscript.TagFilter, len(tags))
	for i := range tags {
		forbidden[i] = tags[i]
		forbidden[i].Operator = zapscript.TagOperatorOR
	}
	forbiddenClauses, args := BuildTagFilterSQL(forbidden)

	query = `
		WITH TotalCounts AS (
			SELECT Media.SystemDBID, COUNT(*) AS Count
			FROM Media INDEXED BY media_system_present_path_idx
			WHERE Media.IsMissing = 0
			GROUP BY Media.SystemDBID
		), ExcludedCounts AS (
			SELECT Media.SystemDBID, COUNT(*) AS Count
			FROM Media
			WHERE Media.IsMissing = 0
			AND ` + strings.Join(forbiddenClauses, " AND ") + `
			GROUP BY Media.SystemDBID
		)
		SELECT Systems.SystemID, TotalCounts.Count - COALESCE(ExcludedCounts.Count, 0)
		FROM TotalCounts
		INNER JOIN Systems ON Systems.DBID = TotalCounts.SystemDBID
		LEFT JOIN ExcludedCounts ON ExcludedCounts.SystemDBID = TotalCounts.SystemDBID
		WHERE TotalCounts.Count > COALESCE(ExcludedCounts.Count, 0)
		ORDER BY Systems.SystemID`
	return query, args
}

func querySystemMediaCounts(
	ctx context.Context,
	db sqlQueryable,
	query string,
	args ...any,
) ([]database.SystemMediaCount, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query system media counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := make([]database.SystemMediaCount, 0)
	for rows.Next() {
		var count database.SystemMediaCount
		if scanErr := rows.Scan(&count.SystemID, &count.Count); scanErr != nil {
			return nil, fmt.Errorf("failed to scan system media count: %w", scanErr)
		}
		counts = append(counts, count)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("system media count rows: %w", err)
	}
	return counts, nil
}

func sqlSystemMediaCountsFromBrowseCache(
	ctx context.Context,
	db sqlQueryable,
) ([]database.SystemMediaCount, error) {
	return querySystemMediaCounts(ctx, db, `
		SELECT Systems.SystemID, SUM(BrowseDirCounts.FileCount)
		FROM BrowseDirs
		INNER JOIN BrowseDirCounts ON BrowseDirCounts.ParentDirDBID = BrowseDirs.DBID
		INNER JOIN Systems ON Systems.DBID = BrowseDirCounts.SystemDBID
		WHERE BrowseDirs.Path = '/'
		GROUP BY BrowseDirCounts.SystemDBID, Systems.SystemID
		ORDER BY Systems.SystemID`)
}

func sqlSystemMediaCounts(
	ctx context.Context,
	db sqlQueryable,
	tags []zapscript.TagFilter,
) ([]database.SystemMediaCount, error) {
	if len(tags) == 0 {
		if state, err := sqlBrowseCacheStatus(ctx, db); err == nil && sqlBrowseCacheServeable(state) {
			counts, cacheErr := sqlSystemMediaCountsFromBrowseCache(ctx, db)
			if cacheErr == nil {
				return counts, nil
			}
			log.Debug().Err(cacheErr).Msg("failed to read system media counts from browse cache")
		} else if err != nil {
			log.Debug().Err(err).Msg("failed to check browse cache for system media counts")
		}

		return querySystemMediaCounts(ctx, db, `
			SELECT Systems.SystemID, matched.Count
			FROM (
				SELECT Media.SystemDBID, COUNT(*) AS Count
				FROM Media INDEXED BY media_system_present_path_idx
				WHERE Media.IsMissing = 0
				GROUP BY Media.SystemDBID
			) matched
			INNER JOIN Systems ON Systems.DBID = matched.SystemDBID
			ORDER BY Systems.SystemID`)
	}

	andFilters, notFilters, orFilters := database.GroupTagFiltersByOperator(tags)
	negativeOnly := len(notFilters) > 0 && len(andFilters) == 0 && len(orFilters) == 0

	var query string
	var args []any
	if negativeOnly {
		query, args = buildNegativeOnlySystemMediaCountsQuery(tags)
	} else {
		tagClauses, tagArgs := BuildTagFilterSQL(tags)
		conditions := make([]string, 1, 1+len(tagClauses))
		conditions[0] = "Media.IsMissing = 0"
		conditions = append(conditions, tagClauses...)
		query = `
			SELECT Systems.SystemID, matched.Count
			FROM (
				SELECT Media.SystemDBID, COUNT(*) AS Count
				FROM Media
				WHERE ` + strings.Join(conditions, " AND ") + `
				GROUP BY Media.SystemDBID
			) matched
			INNER JOIN Systems ON Systems.DBID = matched.SystemDBID
			ORDER BY Systems.SystemID`
		args = tagArgs
	}

	return querySystemMediaCounts(ctx, db, query, args...)
}

func sqlIndexedSystemsFromBrowseCache(ctx context.Context, db sqlQueryable) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.SystemID
		FROM Systems s
		WHERE EXISTS (
			SELECT 1 FROM BrowseDirCounts c WHERE c.SystemDBID = s.DBID
		)
		ORDER BY s.SystemID`)
	if err != nil {
		return nil, fmt.Errorf("failed to query browse cache indexed systems: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close browse cache indexed systems rows")
		}
	}()

	list := make([]string, 0)
	for rows.Next() {
		row := ""
		if scanErr := rows.Scan(&row); scanErr != nil {
			return nil, fmt.Errorf("failed to scan browse cache indexed systems result: %w", scanErr)
		}
		list = append(list, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

func sqlGetAllSystems(ctx context.Context, db *sql.DB) ([]database.System, error) {
	rows, err := db.QueryContext(ctx, "SELECT DBID, SystemID, Name FROM Systems ORDER BY DBID")
	if err != nil {
		return nil, fmt.Errorf("failed to query systems: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	systems := make([]database.System, 0)
	for rows.Next() {
		var system database.System
		if err := rows.Scan(&system.DBID, &system.SystemID, &system.Name); err != nil {
			return nil, fmt.Errorf("failed to scan system: %w", err)
		}
		systems = append(systems, system)
	}
	return systems, rows.Err()
}
