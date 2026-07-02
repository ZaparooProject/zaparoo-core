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
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

const insertMediaSQL = `INSERT INTO Media
	(DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, SortName)
	VALUES (?, ?, ?, ?, ?, ?)`

func sqlFindMedia(ctx context.Context, db sqlQueryable, media *database.Media) (database.Media, error) {
	var row database.Media
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, SortName
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
		&row.ParentDir,
		&row.SortName,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan media row: %w", err)
	}
	return row, nil
}

func sqlInsertMediaWithPreparedStmt(ctx context.Context, stmt *sql.Stmt, row *database.Media) (database.Media, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	res, err := stmt.ExecContext(
		ctx,
		dbID,
		row.MediaTitleDBID,
		row.SystemDBID,
		row.Path,
		row.ParentDir,
		row.SortName,
	)
	if err != nil {
		return *row, fmt.Errorf("failed to execute prepared insert media statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return *row, fmt.Errorf("failed to get last insert ID for media: %w", err)
	}

	row.DBID = lastID
	return *row, nil
}

func sqlInsertMedia(ctx context.Context, db *sql.DB, row *database.Media) (database.Media, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	stmt, err := db.PrepareContext(ctx, insertMediaSQL)
	if err != nil {
		return *row, fmt.Errorf("failed to prepare insert media statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	res, err := stmt.ExecContext(
		ctx,
		dbID,
		row.MediaTitleDBID,
		row.SystemDBID,
		row.Path,
		row.ParentDir,
		row.SortName,
	)
	if err != nil {
		return *row, fmt.Errorf("failed to execute insert media statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return *row, fmt.Errorf("failed to get last insert ID for media: %w", err)
	}

	row.DBID = lastID
	return *row, nil
}

func sqlUpdateMediaTitle(
	ctx context.Context, db sqlQueryable, mediaDBID, mediaTitleDBID int64, sortName string,
) error {
	if _, err := db.ExecContext(
		ctx,
		`UPDATE Media SET MediaTitleDBID = ?, SortName = ? WHERE DBID = ?`,
		mediaTitleDBID,
		sortName,
		mediaDBID,
	); err != nil {
		return fmt.Errorf("failed to update media title: %w", err)
	}

	return nil
}

func sqlDeleteMediaTags(ctx context.Context, db sqlQueryable, mediaDBID int64) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM MediaTags WHERE MediaDBID = ?`, mediaDBID); err != nil {
		return fmt.Errorf("failed to delete media tags: %w", err)
	}

	return nil
}

func sqlGetAllMedia(ctx context.Context, db *sql.DB) ([]database.Media, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, SortName FROM Media ORDER BY DBID")
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
		if err := rows.Scan(&m.DBID, &m.MediaTitleDBID, &m.SystemDBID, &m.Path, &m.ParentDir, &m.SortName); err != nil {
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
		SELECT m.DBID, m.Path, m.ParentDir, m.MediaTitleDBID, m.SortName, m.SystemDBID, t.Slug, s.SystemID
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
		if err := rows.Scan(
			&m.DBID, &m.Path, &m.ParentDir, &m.MediaTitleDBID, &m.SortName, &systemDBID, &m.TitleSlug, &m.SystemID,
		); err != nil {
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
		SELECT m.DBID, m.Path, m.ParentDir, m.MediaTitleDBID, m.SortName, m.SystemDBID, t.Slug, s.SystemID
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
		if err := rows.Scan(
			&m.DBID, &m.Path, &m.ParentDir, &m.MediaTitleDBID, &m.SortName, &systemDBID, &m.TitleSlug, &m.SystemID,
		); err != nil {
			return nil, fmt.Errorf("failed to scan media with full path: %w", err)
		}
		media = append(media, m)
	}
	return media, rows.Err()
}

// sqlGetMediaBySystemID retrieves all media for a specific system.
// This is used for lazy loading during resume to avoid loading ALL media upfront.
// Single-table query on Media: this runs once per system over every media row,
// and no caller reads TitleSlug, so the MediaTitles join would only add a
// per-row B-tree probe. SystemID is filled from the argument. Ordering by Path
// lets SQLite stream from media_system_path_idx instead of filtering by system
// and then building a temp sort by DBID for large systems.
func sqlGetMediaBySystemID(ctx context.Context, db *sql.DB, systemID string) ([]database.MediaWithFullPath, error) {
	query := `
		SELECT m.DBID, m.Path, m.ParentDir, m.MediaTitleDBID, m.SortName, m.IsMissing
		FROM Media m INDEXED BY media_system_path_idx
		WHERE m.SystemDBID = (SELECT DBID FROM Systems WHERE SystemID = ?)
		ORDER BY m.Path
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
		if err := rows.Scan(
			&m.DBID, &m.Path, &m.ParentDir, &m.MediaTitleDBID, &m.SortName, &m.IsMissing,
		); err != nil {
			return nil, fmt.Errorf("failed to scan media for system %s: %w", systemID, err)
		}
		m.SystemID = systemID
		media = append(media, m)
	}
	return media, rows.Err()
}

// sqlGetMediaWithTagsBySystemID retrieves all media for a system and, when
// loadMediaTags is set, the scanner-managed tag DBIDs for each row in a single
// pass. Folding tag links into the media read avoids a second per-(media,tag)-link
// scan of Media: GROUP_CONCAT aggregates the links on the C side so only one extra
// string per media row crosses the cgo boundary. User-owned tags are filtered in Go
// against a pre-fetched DBID set rather than joined in SQL, matching the rationale in
// sqlGetScannerMediaTagsBySystemID. SystemID is filled from the argument.
func sqlGetMediaWithTagsBySystemID(
	ctx context.Context, db *sql.DB, systemID string, loadMediaTags bool,
) ([]database.MediaWithFullPath, error) {
	if !loadMediaTags {
		return sqlGetMediaBySystemID(ctx, db, systemID)
	}

	nonScannerTagIDs, err := sqlGetNonScannerTagDBIDs(ctx, db)
	if err != nil {
		return nil, err
	}

	query := `
		SELECT m.DBID, m.Path, m.ParentDir, m.MediaTitleDBID, m.SortName, m.IsMissing,
			(SELECT GROUP_CONCAT(mt.TagDBID) FROM MediaTags mt WHERE mt.MediaDBID = m.DBID) AS TagIDs
		FROM Media m INDEXED BY media_system_path_idx
		WHERE m.SystemDBID = (SELECT DBID FROM Systems WHERE SystemID = ?)
		ORDER BY m.Path
	`
	rows, err := db.QueryContext(ctx, query, systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query media with tags for system %s: %w", systemID, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	media := make([]database.MediaWithFullPath, 0)
	for rows.Next() {
		var (
			m      database.MediaWithFullPath
			tagIDs sql.NullString
		)
		if scanErr := rows.Scan(
			&m.DBID, &m.Path, &m.ParentDir, &m.MediaTitleDBID, &m.SortName, &m.IsMissing, &tagIDs,
		); scanErr != nil {
			return nil, fmt.Errorf("failed to scan media with tags for system %s: %w", systemID, scanErr)
		}
		m.SystemID = systemID
		parsed, parseErr := parseScannerTagIDs(tagIDs, nonScannerTagIDs)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse tag IDs for media %d in system %s: %w", m.DBID, systemID, parseErr)
		}
		m.TagIDs = parsed
		media = append(media, m)
	}
	return media, rows.Err()
}

// parseScannerTagIDs parses a GROUP_CONCAT(TagDBID) string into a slice of tag DBIDs,
// excluding non-scanner tags (user, cover/scrape property, scraper markers). Returns
// nil for a NULL/empty input or when every tag was non-scanner, so media without
// scanner tags carry no slice allocation.
func parseScannerTagIDs(tagIDs sql.NullString, nonScannerTagIDs map[int64]struct{}) ([]int, error) {
	if !tagIDs.Valid || tagIDs.String == "" {
		return nil, nil
	}
	parts := strings.Split(tagIDs.String, ",")
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		// Atoi (not ParseInt+narrow) keeps tag DBIDs as int — the type ScanState's
		// tag maps use — and errors rather than truncating an out-of-range value.
		id, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid tag DBID %q: %w", part, err)
		}
		if _, isNonScanner := nonScannerTagIDs[int64(id)]; isNonScanner {
			continue
		}
		result = append(result, id)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// sqlBulkSetMediaMissing marks media records as missing by DBID. Batches in chunks
// of 500 to stay within SQLite variable limits.
func sqlBulkSetMediaMissing(ctx context.Context, db sqlQueryable, dbids map[int]struct{}) error {
	if len(dbids) == 0 {
		return nil
	}

	ids := make([]int, 0, len(dbids))
	for id := range dbids {
		ids = append(ids, id)
	}

	const chunkSize = 500
	for i := 0; i < len(ids); i += chunkSize {
		end := i + chunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]

		placeholders := prepareVariadic("?", ",", len(chunk))
		args := make([]any, len(chunk))
		for j, id := range chunk {
			args[j] = id
		}

		//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
		stmt := fmt.Sprintf("UPDATE Media SET IsMissing = 1 WHERE IsMissing = 0 AND DBID IN (%s)", placeholders)
		if _, err := db.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("failed to bulk set media missing: %w", err)
		}
	}

	return nil
}

// sqlResetMissingFlags clears IsMissing for all media belonging to the given system DBIDs.
func sqlResetMissingFlags(ctx context.Context, db sqlQueryable, systemDBIDs []int) error {
	if len(systemDBIDs) == 0 {
		return nil
	}

	placeholders := prepareVariadic("?", ",", len(systemDBIDs))
	args := make([]any, len(systemDBIDs))
	for i, id := range systemDBIDs {
		args[i] = id
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	stmt := fmt.Sprintf("UPDATE Media SET IsMissing = 0 WHERE IsMissing = 1 AND SystemDBID IN (%s)", placeholders)
	if _, err := db.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("failed to reset missing flags: %w", err)
	}

	return nil
}

// sqlGetLaunchCommandForMedia generates a title-based launch command for media at the given path.
// Returns a command in the format: @systemID/titleName (year:XXXX) (players:N)
// Only includes tags that are disambiguating (siblings under the same title have
// different values for that tag type).
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
				  AND (
					SELECT COUNT(DISTINCT all_tags.Tag) FROM (
						SELECT st.Tag FROM Media sib
						INNER JOIN MediaTags smt ON sib.DBID = smt.MediaDBID
						INNER JOIN Tags st ON smt.TagDBID = st.DBID
						INNER JOIN TagTypes stt ON st.TypeDBID = stt.DBID
						WHERE sib.MediaTitleDBID = m.MediaTitleDBID AND stt.Type = 'year'
						UNION
						SELECT st.Tag
						FROM MediaTitleTags mtt
						INNER JOIN Tags st ON mtt.TagDBID = st.DBID
						INNER JOIN TagTypes stt ON st.TypeDBID = stt.DBID
						WHERE mtt.MediaTitleDBID = m.MediaTitleDBID AND stt.Type = 'year'
					) all_tags
				  ) > 1
				LIMIT 1
			) as Year,
			(
				SELECT t.Tag
				FROM MediaTags mtags
				INNER JOIN Tags t ON mtags.TagDBID = t.DBID
				INNER JOIN TagTypes tt ON t.TypeDBID = tt.DBID
				WHERE mtags.MediaDBID = m.DBID
				  AND tt.Type = 'players'
				  AND (
					SELECT COUNT(DISTINCT all_tags.Tag) FROM (
						SELECT st.Tag FROM Media sib
						INNER JOIN MediaTags smt ON sib.DBID = smt.MediaDBID
						INNER JOIN Tags st ON smt.TagDBID = st.DBID
						INNER JOIN TagTypes stt ON st.TypeDBID = stt.DBID
						WHERE sib.MediaTitleDBID = m.MediaTitleDBID AND stt.Type = 'players'
						UNION
						SELECT st.Tag
						FROM MediaTitleTags mtt
						INNER JOIN Tags st ON mtt.TagDBID = st.DBID
						INNER JOIN TagTypes stt ON st.TypeDBID = stt.DBID
						WHERE mtt.MediaTitleDBID = m.MediaTitleDBID AND stt.Type = 'players'
					) all_tags
				  ) > 1
				LIMIT 1
			) as Players
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
	var year, players sql.NullString

	err = stmt.QueryRowContext(ctx, systemID, path).Scan(&titleName, &year, &players)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No media title found, return empty string
			return "", nil
		}
		return "", fmt.Errorf("failed to query launch command: %w", err)
	}

	var tags []database.TagInfo
	if year.Valid && year.String != "" {
		tags = append(tags, database.TagInfo{Type: "year", Tag: year.String})
	}
	if players.Valid && players.String != "" {
		tags = append(tags, database.TagInfo{Type: "players", Tag: players.String})
	}

	return database.BuildTitleZapScript(systemID, titleName, tags), nil
}
