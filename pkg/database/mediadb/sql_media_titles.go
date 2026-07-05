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
// the given titles. A tag type disambiguates a title when the title's present
// (non-missing) sibling media disagree on it: either they hold more than one
// distinct per-media value-set, or some media carry the type and others lack it
// entirely. Only tag types in database.ZapScriptTagTypes (the eligibility allowlist)
// are considered. Comparing per-media sets (not values pooled across media) means a
// multi-valued type that is identical on every sibling — e.g. every disc tagged
// (USA, Europe) — does not falsely disambiguate. The result is stored as a sorted,
// comma-separated list of type names (empty when nothing disambiguates).
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
//
// Each chunk executes as a single atomic UPDATE: a LEFT JOIN over the in-scope titles
// COALESCEs a computed type list (or ” when a title no longer qualifies) so the reset and
// set happen together. This matters because GetZapScriptTagsBySystemAndPath reads
// DisambiguationTypes without holding db.sqlMu, and MediaDB caps the pool at one connection;
// a two-statement reset-then-set would release the connection between them and let that
// reader observe the transient blank state, writing a ZapScript tag with no disambiguation
// suffix. One statement never releases the connection mid-update, so no reader sees a
// partial result.
func sqlRecomputeDisambiguation(ctx context.Context, db sqlQueryable, filterCol string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	// Allowlist of tag types eligible for disambiguation, rendered as an IN clause.
	typeArgs := make([]any, len(database.ZapScriptTagTypes))
	for i, t := range database.ZapScriptTagTypes {
		typeArgs[i] = t
	}
	typeClause := " AND tt.Type IN (" + prepareVariadic("?", ",", len(database.ZapScriptTagTypes)) + ")"

	// Chunk IDs so bound parameters stay under SQLite's limit; leave room for the
	// type params the set statement appends.
	chunkSize := sqliteMaxParams - len(database.ZapScriptTagTypes)
	for start := 0; start < len(ids); start += chunkSize {
		end := start + chunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		holders := prepareVariadic("?", ",", len(chunk))
		chunkArgs := make([]any, len(chunk))
		for i, id := range chunk {
			chunkArgs[i] = id
		}

		// Compute disambiguating types for the in-scope multi-media titles in one
		// set-based pass (a single global sort + aggregate, no per-title correlated
		// subquery), then LEFT JOIN the full in-scope set back so titles that no longer
		// qualify COALESCE to '' — the reset and the set land in one atomic UPDATE. A
		// type disambiguates when its sibling media disagree: either two media carry
		// different per-media value-sets (COUNT(DISTINCT vs) > 1), or some media carry
		// the type and others lack it (mtc < the title's total non-missing media count)
		// — the latter tells "Jackal (W)" apart from "Jackal (W) [bl]". Types are stored
		// comma-joined in alphabetical order; read paths reorder them by display rank.
		// The IS NOT guard skips rows already holding the computed value.
		//nolint:gosec // filterCol is a trusted constant; values are parameterized.
		setQuery := fmt.Sprintf(`
			WITH scope AS (
				SELECT DBID AS tid FROM MediaTitles WHERE %s IN (%s)
			),
			tot AS (
				SELECT m.MediaTitleDBID AS tid, COUNT(*) AS tm
				FROM Media m
				JOIN scope ON scope.tid = m.MediaTitleDBID
				WHERE m.IsMissing = 0
				GROUP BY m.MediaTitleDBID
				HAVING COUNT(*) > 1
			),
			mvs AS (
				SELECT tid, typ, mid, group_concat(tag ORDER BY tag) AS vs
				FROM (
					SELECT DISTINCT m.MediaTitleDBID AS tid, tt.Type AS typ, m.DBID AS mid, t.Tag AS tag
					FROM tot
					JOIN Media m ON m.MediaTitleDBID = tot.tid
					JOIN MediaTags x ON x.MediaDBID = m.DBID
					JOIN Tags t ON t.DBID = x.TagDBID
					JOIN TagTypes tt ON tt.DBID = t.TypeDBID
					WHERE m.IsMissing = 0%s
				)
				GROUP BY tid, typ, mid
			),
			agg AS (
				SELECT tid, typ, COUNT(DISTINCT vs) AS dv, COUNT(*) AS mtc
				FROM mvs GROUP BY tid, typ
			),
			qual AS (
				SELECT agg.tid AS tid, agg.typ AS typ
				FROM agg JOIN tot ON tot.tid = agg.tid
				WHERE agg.dv > 1 OR agg.mtc < tot.tm
			),
			grp AS (
				SELECT tid, group_concat(typ, ',' ORDER BY typ) AS types
				FROM qual
				GROUP BY tid
			),
			result AS (
				SELECT scope.tid AS tid, COALESCE(grp.types, '') AS types
				FROM scope LEFT JOIN grp ON grp.tid = scope.tid
			)
			UPDATE MediaTitles SET DisambiguationTypes = result.types
			FROM result
			WHERE MediaTitles.DBID = result.tid
			  AND MediaTitles.DisambiguationTypes IS NOT result.types
		`, filterCol, holders, typeClause)

		setArgs := make([]any, 0, len(chunkArgs)+len(typeArgs))
		setArgs = append(setArgs, chunkArgs...)
		setArgs = append(setArgs, typeArgs...)
		if _, err := db.ExecContext(ctx, setQuery, setArgs...); err != nil {
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
