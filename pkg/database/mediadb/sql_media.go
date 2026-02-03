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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

const insertMediaSQL = `INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (?, ?, ?, ?)`

func sqlFindMedia(ctx context.Context, db *sql.DB, media database.Media) (database.Media, error) {
	var row database.Media
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, MediaTitleDBID, SystemDBID, Path
		from Media
		where DBID = ?
		or (
			MediaTitleDBID = ?
			and SystemDBID = ?
			and Path = ?
		)
		or (
			SystemDBID = ?
			and Path = ?
		)
		LIMIT 1;
	`)
	if err != nil {
		return row, fmt.Errorf("failed to prepare find media statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx,
		media.DBID,
		media.MediaTitleDBID,
		media.SystemDBID,
		media.Path,
		media.SystemDBID,
		media.Path,
	).Scan(
		&row.DBID,
		&row.MediaTitleDBID,
		&row.SystemDBID,
		&row.Path,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan media row: %w", err)
	}
	return row, nil
}

func sqlInsertMediaWithPreparedStmt(ctx context.Context, stmt *sql.Stmt, row database.Media) (database.Media, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	res, err := stmt.ExecContext(ctx, dbID, row.MediaTitleDBID, row.SystemDBID, row.Path)
	if err != nil {
		return row, fmt.Errorf("failed to execute prepared insert media statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for media: %w", err)
	}

	row.DBID = lastID
	return row, nil
}

func sqlInsertMedia(ctx context.Context, db *sql.DB, row database.Media) (database.Media, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	stmt, err := db.PrepareContext(ctx, insertMediaSQL)
	if err != nil {
		return row, fmt.Errorf("failed to prepare insert media statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	res, err := stmt.ExecContext(ctx, dbID, row.MediaTitleDBID, row.SystemDBID, row.Path)
	if err != nil {
		return row, fmt.Errorf("failed to execute insert media statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for media: %w", err)
	}

	row.DBID = lastID
	return row, nil
}

func sqlGetAllMedia(ctx context.Context, db *sql.DB) ([]database.Media, error) {
	rows, err := db.QueryContext(ctx, "SELECT DBID, MediaTitleDBID, SystemDBID, Path FROM Media ORDER BY DBID")
	if err != nil {
		return nil, fmt.Errorf("failed to query media: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	media := make([]database.Media, 0)
	for rows.Next() {
		var m database.Media
		if err := rows.Scan(&m.DBID, &m.MediaTitleDBID, &m.SystemDBID, &m.Path); err != nil {
			return nil, fmt.Errorf("failed to scan media: %w", err)
		}
		media = append(media, m)
	}
	return media, rows.Err()
}

func sqlGetTotalMediaCount(ctx context.Context, db *sql.DB) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get total media count: %w", err)
	}
	return count, nil
}

// sqlGetMediaWithFullPath retrieves all media with their associated title and system information using JOIN queries.
func sqlGetMediaWithFullPath(ctx context.Context, db *sql.DB) ([]database.MediaWithFullPath, error) {
	query := `
		SELECT m.DBID, m.Path, m.MediaTitleDBID, m.SystemDBID, t.Slug, s.SystemID
		FROM Media m
		JOIN MediaTitles t ON m.MediaTitleDBID = t.DBID
		JOIN Systems s ON t.SystemDBID = s.DBID
		ORDER BY m.DBID
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query media with full path: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	media := make([]database.MediaWithFullPath, 0)
	for rows.Next() {
		var m database.MediaWithFullPath
		var systemDBID int64 // Temporary variable for the extra field
		if err := rows.Scan(&m.DBID, &m.Path, &m.MediaTitleDBID, &systemDBID, &m.TitleSlug, &m.SystemID); err != nil {
			return nil, fmt.Errorf("failed to scan media with full path: %w", err)
		}
		media = append(media, m)
	}
	return media, rows.Err()
}

// sqlGetMediaWithFullPathExcluding retrieves all media with their
// associated title and system information, excluding those belonging to
// systems in the excludeSystemIDs list
func sqlGetMediaWithFullPathExcluding(
	ctx context.Context,
	db *sql.DB,
	excludeSystemIDs []string,
) ([]database.MediaWithFullPath, error) {
	if len(excludeSystemIDs) == 0 {
		return sqlGetMediaWithFullPath(ctx, db)
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
		SELECT m.DBID, m.Path, m.MediaTitleDBID, m.SystemDBID, t.Slug, s.SystemID
		FROM Media m
		JOIN MediaTitles t ON m.MediaTitleDBID = t.DBID
		JOIN Systems s ON t.SystemDBID = s.DBID
		WHERE s.SystemID NOT IN (%s)
		ORDER BY m.DBID
	`, strings.Join(placeholders, ","))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query media with full path excluding %v: %w", excludeSystemIDs, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	media := make([]database.MediaWithFullPath, 0)
	for rows.Next() {
		var m database.MediaWithFullPath
		var systemDBID int64 // Temporary variable for the extra field
		if err := rows.Scan(&m.DBID, &m.Path, &m.MediaTitleDBID, &systemDBID, &m.TitleSlug, &m.SystemID); err != nil {
			return nil, fmt.Errorf("failed to scan media with full path: %w", err)
		}
		media = append(media, m)
	}
	return media, rows.Err()
}

// sqlGetMediaBySystemID retrieves all media for a specific system with their associated title and system information.
// This is used for lazy loading during resume to avoid loading ALL media upfront.
func sqlGetMediaBySystemID(ctx context.Context, db *sql.DB, systemID string) ([]database.MediaWithFullPath, error) {
	query := `
		SELECT m.DBID, m.Path, m.MediaTitleDBID, m.SystemDBID, t.Slug, s.SystemID
		FROM Media m
		JOIN MediaTitles t ON m.MediaTitleDBID = t.DBID
		JOIN Systems s ON t.SystemDBID = s.DBID
		WHERE s.SystemID = ?
		ORDER BY m.DBID
	`
	rows, err := db.QueryContext(ctx, query, systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query media for system %s: %w", systemID, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	media := make([]database.MediaWithFullPath, 0)
	for rows.Next() {
		var m database.MediaWithFullPath
		var systemDBID int64 // Temporary variable for the extra field
		if err := rows.Scan(&m.DBID, &m.Path, &m.MediaTitleDBID, &systemDBID, &m.TitleSlug, &m.SystemID); err != nil {
			return nil, fmt.Errorf("failed to scan media for system %s: %w", systemID, err)
		}
		media = append(media, m)
	}
	return media, rows.Err()
}

// sqlGetLaunchCommandForMedia generates a title-based launch command for media at the given path.
// Returns a command in the format: @systemID/titleName or @systemID/titleName (year:XXXX)
func sqlGetLaunchCommandForMedia(
	ctx context.Context,
	db *sql.DB,
	systemID string,
	path string,
) (string, error) {
	query := `
		SELECT
			mt.Name,
			(
				SELECT t.Tag
				FROM MediaTags mtags
				INNER JOIN Tags t ON mtags.TagDBID = t.DBID
				INNER JOIN TagTypes tt ON t.TypeDBID = tt.DBID
				WHERE mtags.MediaDBID = m.DBID
				  AND tt.Type = 'year'
				  AND t.Tag GLOB '[0-9][0-9][0-9][0-9]'
				LIMIT 1
			) as Year
		FROM Media m
		INNER JOIN MediaTitles mt ON m.MediaTitleDBID = mt.DBID
		INNER JOIN Systems s ON mt.SystemDBID = s.DBID
		WHERE s.SystemID = ? AND m.Path = ?
		LIMIT 1
	`

	stmt, err := db.PrepareContext(ctx, query)
	if err != nil {
		return "", fmt.Errorf("failed to prepare get launch command statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	var titleName string
	var year sql.NullString

	err = stmt.QueryRowContext(ctx, systemID, path).Scan(&titleName, &year)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No media title found, return empty string
			return "", nil
		}
		return "", fmt.Errorf("failed to query launch command: %w", err)
	}

	// Build the launch command
	launchCmd := fmt.Sprintf("@%s/%s", systemID, titleName)
	if year.Valid && year.String != "" {
		launchCmd = fmt.Sprintf("%s (year:%s)", launchCmd, year.String)
	}

	return launchCmd, nil
}
