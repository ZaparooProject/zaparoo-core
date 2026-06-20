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

const insertMediaTitleSQL = `
	INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name, SlugLength, SlugWordCount, SecondarySlug)
	VALUES (?, ?, ?, ?, ?, ?, ?)
`

func sqlFindMediaTitle(ctx context.Context, db sqlQueryable, title *database.MediaTitle) (database.MediaTitle, error) {
	var row database.MediaTitle

	// Prefer exact DBID lookup when provided
	if title.DBID != 0 {
		stmt, err := db.PrepareContext(ctx, `
            select
            DBID, SystemDBID, Slug, Name, SlugLength, SlugWordCount, SecondarySlug
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
			&row.SlugLength,
			&row.SlugWordCount,
			&row.SecondarySlug,
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
            DBID, SystemDBID, Slug, Name, SlugLength, SlugWordCount, SecondarySlug
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
			&row.SlugLength,
			&row.SlugWordCount,
			&row.SecondarySlug,
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
            DBID, SystemDBID, Slug, Name, SlugLength, SlugWordCount, SecondarySlug
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
			&row.SlugLength,
			&row.SlugWordCount,
			&row.SecondarySlug,
		)
		if err != nil {
			return row, fmt.Errorf("failed to scan media title row by slug: %w", err)
		}
		return row, nil
	}

	return row, errors.New("insufficient parameters to find media title")
}

func sqlInsertMediaTitleWithPreparedStmt(
	ctx context.Context, stmt *sql.Stmt, row *database.MediaTitle,
) (database.MediaTitle, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	res, err := stmt.ExecContext(
		ctx, dbID, row.SystemDBID, row.Slug, row.Name, row.SlugLength,
		row.SlugWordCount, row.SecondarySlug,
	)
	if err != nil {
		return *row, fmt.Errorf("failed to execute prepared insert media title statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return *row, fmt.Errorf("failed to get last insert ID for media title: %w", err)
	}

	row.DBID = lastID
	return *row, nil
}

func sqlInsertMediaTitle(ctx context.Context, db *sql.DB, row *database.MediaTitle) (database.MediaTitle, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	stmt, err := db.PrepareContext(ctx, insertMediaTitleSQL)
	if err != nil {
		return *row, fmt.Errorf("failed to prepare insert media title statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	res, err := stmt.ExecContext(
		ctx, dbID, row.SystemDBID, row.Slug, row.Name, row.SlugLength, row.SlugWordCount,
		row.SecondarySlug,
	)
	if err != nil {
		return *row, fmt.Errorf("failed to execute insert media title statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return *row, fmt.Errorf("failed to get last insert ID for media title: %w", err)
	}

	row.DBID = lastID
	return *row, nil
}

func sqlUpdateMediaTitleName(ctx context.Context, db sqlQueryable, titleDBID int64, name string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE MediaTitles SET Name = ? WHERE DBID = ?`,
		name, titleDBID,
	)
	if err != nil {
		return fmt.Errorf("failed to update media title name: %w", err)
	}
	return nil
}

func sqlGetAllMediaTitles(ctx context.Context, db *sql.DB) ([]database.MediaTitle, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT DBID, Slug, Name, SystemDBID, SlugLength, SlugWordCount, SecondarySlug
		 FROM MediaTitles ORDER BY DBID`)
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
		if err := rows.Scan(
			&title.DBID, &title.Slug, &title.Name,
			&title.SystemDBID, &title.SlugLength, &title.SlugWordCount, &title.SecondarySlug,
		); err != nil {
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

// sqlGetTitlesBySystemID retrieves all media titles for a specific system.
// This is used for lazy loading during resume to avoid loading ALL titles upfront.
// SystemID is filled from the argument rather than joined from Systems: every row's
// SystemID equals the filter argument, so the join only added a per-row probe and a
// redundant string crossing (the top reindex allocator). SystemDBID is still selected
// because other callers read it.
func sqlGetTitlesBySystemID(ctx context.Context, db *sql.DB, systemID string) ([]database.TitleWithSystem, error) {
	query := `
		SELECT t.DBID, t.Slug, t.Name, t.SystemDBID
		FROM MediaTitles t
		WHERE t.SystemDBID = (SELECT DBID FROM Systems WHERE SystemID = ?)
	`
	rows, err := db.QueryContext(ctx, query, systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query titles for system %s: %w", systemID, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	titles := make([]database.TitleWithSystem, 0)
	for rows.Next() {
		var title database.TitleWithSystem
		if err := rows.Scan(&title.DBID, &title.Slug, &title.Name, &title.SystemDBID); err != nil {
			return nil, fmt.Errorf("failed to scan title for system %s: %w", systemID, err)
		}
		title.SystemID = systemID
		titles = append(titles, title)
	}
	return titles, rows.Err()
}

// sqlRecomputeTitleDisambiguation recomputes MediaTitles.DisambiguationTypes for
// the given titles. A tag type is disambiguating for a title when the title's
// present (non-missing) media hold more than one distinct per-media value-set for
// that type, restricted to database.ZapScriptTagTypes. Comparing per-media sets
// (not values pooled across media) means a multi-valued type that is identical on
// every sibling — e.g. every disc tagged (USA, Europe) — does not falsely
// disambiguate. The result is stored as a sorted, comma-separated list of type
// names (empty when nothing disambiguates).
//
// This is the single source of truth for sibling disambiguation: read paths
// filter each result's tags by the stored types instead of re-deriving across a
// page of results, which made disambiguation depend on pagination and sort order.
func sqlRecomputeTitleDisambiguation(ctx context.Context, db sqlQueryable, titleDBIDs []int64) error {
	return sqlRecomputeDisambiguation(ctx, db, "DBID", titleDBIDs)
}

// sqlRecomputeDisambiguationForSystems recomputes DisambiguationTypes for every
// MediaTitle belonging to the given systems. Used at index time so titles whose
// media set changed (including titles that lost variants) are all refreshed.
func sqlRecomputeDisambiguationForSystems(ctx context.Context, db sqlQueryable, systemDBIDs []int64) error {
	return sqlRecomputeDisambiguation(ctx, db, "SystemDBID", systemDBIDs)
}

// sqlRecomputeDisambiguation runs the disambiguation UPDATE filtered by either
// MediaTitles.DBID (title-scoped) or MediaTitles.SystemDBID (system-scoped).
// filterCol is a trusted constant, never user input.
func sqlRecomputeDisambiguation(ctx context.Context, db sqlQueryable, filterCol string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	typeHolders := prepareVariadic("?", ",", len(database.ZapScriptTagTypes))
	typeArgs := make([]any, len(database.ZapScriptTagTypes))
	for i, t := range database.ZapScriptTagTypes {
		typeArgs[i] = t
	}

	// Chunk IDs so total bound parameters stay under SQLite's limit.
	chunkSize := sqliteMaxParams - len(database.ZapScriptTagTypes)
	for start := 0; start < len(ids); start += chunkSize {
		end := start + chunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]

		args := make([]any, 0, len(typeArgs)+len(chunk))
		args = append(args, typeArgs...)
		for _, id := range chunk {
			args = append(args, id)
		}

		//nolint:gosec // filterCol is a trusted constant; placeholders are parameterized
		query := fmt.Sprintf(`
			UPDATE MediaTitles
			SET DisambiguationTypes = COALESCE((
				SELECT group_concat(Type, ',')
				FROM (
					SELECT Type
					FROM (
						SELECT Type, MediaDBID, group_concat(Tag) AS ValueSet
						FROM (
							SELECT DISTINCT tt.Type AS Type, m.DBID AS MediaDBID, t.Tag AS Tag
							FROM Media m
							JOIN MediaTags mt ON mt.MediaDBID = m.DBID
							JOIN Tags t ON t.DBID = mt.TagDBID
							JOIN TagTypes tt ON tt.DBID = t.TypeDBID
							WHERE m.MediaTitleDBID = MediaTitles.DBID
							  AND m.IsMissing = 0
							  AND tt.Type IN (%s)
							ORDER BY tt.Type, m.DBID, t.Tag
						)
						GROUP BY Type, MediaDBID
					)
					GROUP BY Type
					HAVING COUNT(DISTINCT ValueSet) > 1
					ORDER BY Type
				)
			), '')
			WHERE MediaTitles.%s IN (%s)
		`, typeHolders, filterCol, prepareVariadic("?", ",", len(chunk)))

		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("failed to recompute disambiguation: %w", err)
		}
	}
	return nil
}

// PreFilterQuery represents pre-filter parameters for efficient fuzzy matching candidate reduction.
type PreFilterQuery struct {
	MinLength    int
	MaxLength    int
	MinWordCount int
	MaxWordCount int
}

// sqlGetCandidatesWithPreFilter retrieves media titles filtered by slug length and word count ranges.
// This dramatically reduces the candidate set before applying expensive fuzzy matching algorithms.
//
// Uses the composite index idx_media_prefilter (SlugLength, SlugWordCount) for efficient range queries.
func sqlGetCandidatesWithPreFilter(
	ctx context.Context,
	db *sql.DB,
	systemDBID int64,
	query PreFilterQuery,
) ([]database.MediaTitle, error) {
	sqlQuery := `
		SELECT DBID, SystemDBID, Slug, Name, SlugLength, SlugWordCount, SecondarySlug
		FROM MediaTitles
		WHERE SystemDBID = ?
		  AND SlugLength BETWEEN ? AND ?
		  AND SlugWordCount BETWEEN ? AND ?
	`

	stmt, err := db.PrepareContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare get candidates with pre-filter statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx,
		systemDBID,
		query.MinLength, query.MaxLength,
		query.MinWordCount, query.MaxWordCount,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to execute get candidates with pre-filter query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	titles := make([]database.MediaTitle, 0)
	for rows.Next() {
		var title database.MediaTitle
		if err := rows.Scan(
			&title.DBID,
			&title.SystemDBID,
			&title.Slug,
			&title.Name,
			&title.SlugLength,
			&title.SlugWordCount,
			&title.SecondarySlug,
		); err != nil {
			return nil, fmt.Errorf("failed to scan media title with pre-filter: %w", err)
		}
		titles = append(titles, title)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating pre-filtered titles: %w", err)
	}

	return titles, nil
}
