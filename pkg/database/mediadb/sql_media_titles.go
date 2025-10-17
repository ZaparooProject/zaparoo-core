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
	"errors"
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

const insertMediaTitleSQL = `INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (?, ?, ?, ?)`

func sqlFindMediaTitle(ctx context.Context, db *sql.DB, title database.MediaTitle) (database.MediaTitle, error) {
	var row database.MediaTitle

	// Prefer exact DBID lookup when provided
	if title.DBID != 0 {
		stmt, err := db.PrepareContext(ctx, `
            select
            DBID, SystemDBID, Slug, Name
            from MediaTitles
            where DBID = ?
            limit 1;
        `)
		if err != nil {
			return row, fmt.Errorf("failed to prepare find media title by DBID statement: %w", err)
		}
		defer func() {
			if closeErr := stmt.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Msg("failed to close sql statement")
			}
		}()
		err = stmt.QueryRowContext(ctx, title.DBID).Scan(
			&row.DBID,
			&row.SystemDBID,
			&row.Slug,
			&row.Name,
		)
		if err != nil {
			return row, fmt.Errorf("failed to scan media title row by DBID: %w", err)
		}
		return row, nil
	}

	// If SystemDBID and Slug are provided, use both for accurate lookup
	if title.SystemDBID != 0 && title.Slug != "" {
		stmt, err := db.PrepareContext(ctx, `
            select
            DBID, SystemDBID, Slug, Name
            from MediaTitles
            where SystemDBID = ? and Slug = ?
            limit 1;
        `)
		if err != nil {
			return row, fmt.Errorf("failed to prepare find media title by system+slug statement: %w", err)
		}
		defer func() {
			if closeErr := stmt.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Msg("failed to close sql statement")
			}
		}()
		err = stmt.QueryRowContext(ctx, title.SystemDBID, title.Slug).Scan(
			&row.DBID,
			&row.SystemDBID,
			&row.Slug,
			&row.Name,
		)
		if err != nil {
			return row, fmt.Errorf("failed to scan media title row by system+slug: %w", err)
		}
		return row, nil
	}

	// Fallback to slug-only if that's all we have
	if title.Slug != "" {
		stmt, err := db.PrepareContext(ctx, `
            select
            DBID, SystemDBID, Slug, Name
            from MediaTitles
            where Slug = ?
            limit 1;
        `)
		if err != nil {
			return row, fmt.Errorf("failed to prepare find media title by slug statement: %w", err)
		}
		defer func() {
			if closeErr := stmt.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Msg("failed to close sql statement")
			}
		}()
		err = stmt.QueryRowContext(ctx, title.Slug).Scan(
			&row.DBID,
			&row.SystemDBID,
			&row.Slug,
			&row.Name,
		)
		if err != nil {
			return row, fmt.Errorf("failed to scan media title row by slug: %w", err)
		}
		return row, nil
	}

	return row, errors.New("insufficient parameters to find media title")
}

func sqlInsertMediaTitleWithPreparedStmt(
	ctx context.Context, stmt *sql.Stmt, row database.MediaTitle,
) (database.MediaTitle, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	res, err := stmt.ExecContext(ctx, dbID, row.SystemDBID, row.Slug, row.Name)
	if err != nil {
		return row, fmt.Errorf("failed to execute prepared insert media title statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for media title: %w", err)
	}

	row.DBID = lastID
	return row, nil
}

func sqlInsertMediaTitle(ctx context.Context, db *sql.DB, row database.MediaTitle) (database.MediaTitle, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	stmt, err := db.PrepareContext(ctx, insertMediaTitleSQL)
	if err != nil {
		return row, fmt.Errorf("failed to prepare insert media title statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	res, err := stmt.ExecContext(ctx, dbID, row.SystemDBID, row.Slug, row.Name)
	if err != nil {
		return row, fmt.Errorf("failed to execute insert media title statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for media title: %w", err)
	}

	row.DBID = lastID
	return row, nil
}

func sqlGetAllMediaTitles(ctx context.Context, db *sql.DB) ([]database.MediaTitle, error) {
	rows, err := db.QueryContext(ctx, "SELECT DBID, Slug, Name, SystemDBID FROM MediaTitles ORDER BY DBID")
	if err != nil {
		return nil, fmt.Errorf("failed to query media titles: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	titles := make([]database.MediaTitle, 0)
	for rows.Next() {
		var title database.MediaTitle
		if err := rows.Scan(&title.DBID, &title.Slug, &title.Name, &title.SystemDBID); err != nil {
			return nil, fmt.Errorf("failed to scan media title: %w", err)
		}
		titles = append(titles, title)
	}
	return titles, rows.Err()
}

// sqlGetTitlesWithSystems retrieves all media titles with their associated system IDs using a JOIN query.
func sqlGetTitlesWithSystems(ctx context.Context, db *sql.DB) ([]database.TitleWithSystem, error) {
	query := `
		SELECT t.DBID, t.Slug, t.Name, t.SystemDBID, s.SystemID
		FROM MediaTitles t
		JOIN Systems s ON t.SystemDBID = s.DBID
		ORDER BY t.DBID
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query titles with systems: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	titles := make([]database.TitleWithSystem, 0)
	for rows.Next() {
		var title database.TitleWithSystem
		if err := rows.Scan(&title.DBID, &title.Slug, &title.Name, &title.SystemDBID, &title.SystemID); err != nil {
			return nil, fmt.Errorf("failed to scan title with system: %w", err)
		}
		titles = append(titles, title)
	}
	return titles, rows.Err()
}

// sqlGetTitlesWithSystemsExcluding retrieves all media titles with their
// associated system IDs, excluding those belonging to systems in the
// excludeSystemIDs list
func sqlGetTitlesWithSystemsExcluding(
	ctx context.Context,
	db *sql.DB,
	excludeSystemIDs []string,
) ([]database.TitleWithSystem, error) {
	if len(excludeSystemIDs) == 0 {
		return sqlGetTitlesWithSystems(ctx, db)
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(excludeSystemIDs))
	args := make([]any, len(excludeSystemIDs))
	for i, systemID := range excludeSystemIDs {
		placeholders[i] = "?"
		args[i] = systemID
	}

	//nolint:gosec // using parameterized placeholders, not user input
	query := fmt.Sprintf(`
		SELECT t.DBID, t.Slug, t.Name, t.SystemDBID, s.SystemID
		FROM MediaTitles t
		JOIN Systems s ON t.SystemDBID = s.DBID
		WHERE s.SystemID NOT IN (%s)
		ORDER BY t.DBID
	`, strings.Join(placeholders, ","))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query titles with systems excluding %v: %w", excludeSystemIDs, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	titles := make([]database.TitleWithSystem, 0)
	for rows.Next() {
		var title database.TitleWithSystem
		if err := rows.Scan(&title.DBID, &title.Slug, &title.Name, &title.SystemDBID, &title.SystemID); err != nil {
			return nil, fmt.Errorf("failed to scan title with system: %w", err)
		}
		titles = append(titles, title)
	}
	return titles, rows.Err()
}

func sqlGetAllSlugsForSystem(ctx context.Context, db *sql.DB, systemID string) ([]string, error) {
	stmt, err := db.PrepareContext(ctx, `
		SELECT DISTINCT MediaTitles.Slug
		FROM MediaTitles
		JOIN Systems ON MediaTitles.SystemDBID = Systems.DBID
		WHERE Systems.SystemID = ?
		ORDER BY MediaTitles.Slug
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare get all slugs statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to execute get all slugs query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()

	slugList := make([]string, 0, 100)
	for rows.Next() {
		var slug string
		if scanErr := rows.Scan(&slug); scanErr != nil {
			return nil, fmt.Errorf("failed to scan slug: %w", scanErr)
		}
		slugList = append(slugList, slug)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating slugs: %w", err)
	}

	return slugList, nil
}
