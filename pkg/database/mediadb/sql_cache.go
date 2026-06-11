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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

const systemTagsCacheUpsertClause = `
		WHERE true
		ON CONFLICT(SystemDBID, TagDBID) DO UPDATE SET
			Count = SystemTagsCache.Count + excluded.Count`

// buildPopulateMediaTagsSQL and buildPopulateTitleTagsSQL populate the cache in
// two smaller upserts instead of one UNION ALL + final GROUP BY. The prior shape
// built an extra temp B-tree over already-aggregated rows; splitting preserves
// results while reducing sort/merge work on slow storage.
func buildPopulateMediaTagsSQL(systemFilter string) string {
	mediaWhere := "WHERE m.IsMissing = 0"
	if systemFilter != "" {
		mediaWhere = "WHERE m.SystemDBID IN " + systemFilter + " AND m.IsMissing = 0"
	}
	return fmt.Sprintf(`
		INSERT INTO SystemTagsCache (SystemDBID, TagDBID, TagType, Tag, Count)
		SELECT agg.SystemDBID, agg.TagDBID, tt.Type, t.Tag, agg.Cnt AS Count
		FROM (
			SELECT
				m.SystemDBID AS SystemDBID,
				mt.TagDBID AS TagDBID,
				COUNT(*) AS Cnt
			FROM Media m INDEXED BY media_system_path_idx
			CROSS JOIN MediaTags mt
			%s
			  AND mt.MediaDBID = m.DBID
			GROUP BY m.SystemDBID, mt.TagDBID
		) agg
		JOIN Tags t ON agg.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID`+systemTagsCacheUpsertClause, mediaWhere)
}

func buildPopulateTitleTagsSQL(systemFilter string) string {
	titleWhere := "WHERE EXISTS(SELECT 1 FROM Media m WHERE m.MediaTitleDBID = mtl.DBID AND m.IsMissing = 0)"
	if systemFilter != "" {
		titleWhere = "WHERE mtl.SystemDBID IN " + systemFilter +
			" AND EXISTS(SELECT 1 FROM Media m WHERE m.MediaTitleDBID = mtl.DBID AND m.IsMissing = 0)"
	}
	return fmt.Sprintf(`
		INSERT INTO SystemTagsCache (SystemDBID, TagDBID, TagType, Tag, Count)
		SELECT agg.SystemDBID, agg.TagDBID, tt.Type, t.Tag, agg.Cnt AS Count
		FROM (
			SELECT
				mtl.SystemDBID AS SystemDBID,
				mtt.TagDBID AS TagDBID,
				COUNT(*) AS Cnt
			FROM MediaTitleTags mtt
			JOIN MediaTitles mtl ON mtt.MediaTitleDBID = mtl.DBID
			%s
			GROUP BY mtl.SystemDBID, mtt.TagDBID
		) agg
		JOIN Tags t ON agg.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID`+systemTagsCacheUpsertClause, titleWhere)
}

// sqlPopulateSystemTagsCache - Populates the SystemTagsCache table for fast tag lookups
// This should be called after media indexing to ensure cache is up to date
func sqlPopulateSystemTagsCache(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin system tags cache transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, execErr := tx.ExecContext(ctx, "DELETE FROM SystemTagsCache"); execErr != nil {
		return fmt.Errorf("failed to clear system tags cache: %w", execErr)
	}
	if _, execErr := tx.ExecContext(ctx, buildPopulateMediaTagsSQL("")); execErr != nil {
		return fmt.Errorf("failed to populate media tags cache rows: %w", execErr)
	}
	if _, execErr := tx.ExecContext(ctx, buildPopulateTitleTagsSQL("")); execErr != nil {
		return fmt.Errorf("failed to populate title tags cache rows: %w", execErr)
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("failed to commit system tags cache transaction: %w", commitErr)
	}
	committed = true
	return nil
}

// sqlPopulateSystemTagsCacheForSystems - Populates cache for specific systems only
// Unlike the full version, this selectively updates only the requested systems
func sqlPopulateSystemTagsCacheForSystems(
	ctx context.Context, db *sql.DB, systems []systemdefs.System,
) error {
	if len(systems) == 0 {
		return nil // No-op for empty systems list
	}

	// Step 1: Get DBIDs for requested systems
	systemDBIDs := make([]int64, 0, len(systems))
	getDBIDStmt, err := db.PrepareContext(ctx, "SELECT DBID FROM Systems WHERE SystemID = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare system DBID lookup: %w", err)
	}
	defer func() {
		if closeErr := getDBIDStmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close system DBID lookup statement")
		}
	}()

	for _, system := range systems {
		var dbid int64
		if scanErr := getDBIDStmt.QueryRowContext(ctx, system.ID).Scan(&dbid); scanErr != nil {
			if errors.Is(scanErr, sql.ErrNoRows) {
				log.Debug().Str("system_id", system.ID).Msg("system not found in database, skipping cache population")
				continue
			}
			return fmt.Errorf("failed to get DBID for system %s: %w", system.ID, scanErr)
		}
		systemDBIDs = append(systemDBIDs, dbid)
	}

	if len(systemDBIDs) == 0 {
		return nil // No systems found in database
	}

	// Step 2: Delete cache entries for these systems and repopulate them in
	// one transaction. This keeps WAL FULL durability while avoiding separate
	// fsyncs for clear/media/title phases.
	placeholders := prepareVariadic("?", ",", len(systemDBIDs))
	args := make([]any, len(systemDBIDs))
	for i, id := range systemDBIDs {
		args[i] = id
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin selective system tags cache transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	deleteSQL := fmt.Sprintf("DELETE FROM SystemTagsCache WHERE SystemDBID IN (%s)", placeholders)
	if _, execErr := tx.ExecContext(ctx, deleteSQL, args...); execErr != nil {
		return fmt.Errorf("failed to clear cache for specific systems: %w", execErr)
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	placeholderList := fmt.Sprintf("(%s)", placeholders)
	if _, execErr := tx.ExecContext(ctx, buildPopulateMediaTagsSQL(placeholderList), args...); execErr != nil {
		return fmt.Errorf("failed to populate media cache rows for specific systems: %w", execErr)
	}
	if _, execErr := tx.ExecContext(ctx, buildPopulateTitleTagsSQL(placeholderList), args...); execErr != nil {
		return fmt.Errorf("failed to populate title cache rows for specific systems: %w", execErr)
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("failed to commit selective system tags cache transaction: %w", commitErr)
	}
	committed = true

	log.Debug().Int("system_count", len(systems)).Msg("populated system tags cache for specific systems")
	return nil
}

// sqlGetSystemTagsCached - Fast retrieval of tags for a specific system using cache
func sqlGetSystemTagsCached(
	ctx context.Context,
	db *sql.DB,
	systems []systemdefs.System,
) ([]database.TagInfo, error) {
	if len(systems) == 0 {
		return nil, errors.New("no systems provided for cached tag search")
	}

	// Prepare statement once for all system lookups
	systemLookupStmt, err := db.PrepareContext(ctx, "SELECT DBID FROM Systems WHERE SystemID = ?")
	if err != nil {
		return nil, fmt.Errorf("failed to prepare system lookup: %w", err)
	}
	defer func() {
		if closeErr := systemLookupStmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close system lookup statement")
		}
	}()

	args := make([]any, 0, len(systems))
	for _, sys := range systems {
		// We need to get the DBID for each system
		var systemDBID int
		err = systemLookupStmt.QueryRowContext(ctx, sys.ID).Scan(&systemDBID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // Skip systems that don't exist
			}
			return nil, fmt.Errorf("failed to lookup system %s: %w", sys.ID, err)
		}
		args = append(args, systemDBID)
	}

	if len(args) == 0 {
		return make([]database.TagInfo, 0), nil
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	sqlQuery := `
		SELECT stc.TagType, stc.Tag, t.DisplayName, SUM(stc.Count) AS Count
		FROM SystemTagsCache stc
		JOIN Tags t ON stc.TagDBID = t.DBID
		WHERE stc.SystemDBID IN (` +
		prepareVariadic("?", ",", len(args)) +
		`)
		GROUP BY stc.TagType, stc.Tag, t.DisplayName
		ORDER BY stc.TagType, stc.Tag`

	stmt, err := db.PrepareContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare cached tags statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute cached tags query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()

	result := make([]database.TagInfo, 0, 100)
	for rows.Next() {
		var tagType, tag, label string
		var count int64
		if scanErr := rows.Scan(&tagType, &tag, &label, &count); scanErr != nil {
			return nil, fmt.Errorf("failed to scan cached tag result: %w", scanErr)
		}
		result = append(result, database.TagInfo{
			Type:  tagType,
			Tag:   tags.UnpadTagValue(tag),
			Label: label,
			Count: count,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// sqlInvalidateSystemTagsCache - Invalidates cache for specific systems
func sqlInvalidateSystemTagsCache(ctx context.Context, db *sql.DB, systems []systemdefs.System) error {
	if len(systems) == 0 {
		return nil
	}

	// Prepare statement once for all system lookups
	systemLookupStmt, err := db.PrepareContext(ctx, "SELECT DBID FROM Systems WHERE SystemID = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare system lookup: %w", err)
	}
	defer func() {
		if closeErr := systemLookupStmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close system lookup statement")
		}
	}()

	args := make([]any, 0, len(systems))
	for _, sys := range systems {
		// Get system DBID
		var systemDBID int
		err = systemLookupStmt.QueryRowContext(ctx, sys.ID).Scan(&systemDBID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // Skip systems that don't exist
			}
			return fmt.Errorf("failed to lookup system %s: %w", sys.ID, err)
		}
		args = append(args, systemDBID)
	}

	if len(args) == 0 {
		return nil
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	sqlQuery := `DELETE FROM SystemTagsCache WHERE SystemDBID IN (` +
		prepareVariadic("?", ",", len(args)) + `)`

	stmt, err := db.PrepareContext(ctx, sqlQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare cache invalidation statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	if _, err := stmt.ExecContext(ctx, args...); err != nil {
		return fmt.Errorf("failed to invalidate system tags cache: %w", err)
	}

	return nil
}
