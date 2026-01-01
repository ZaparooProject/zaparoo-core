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
	"github.com/rs/zerolog/log"
)

// sqlPopulateSystemTagsCache - Populates the SystemTagsCache table for fast tag lookups
// This should be called after media indexing to ensure cache is up to date
func sqlPopulateSystemTagsCache(ctx context.Context, db *sql.DB) error {
	// Clear existing cache
	clearStmt, err := db.PrepareContext(ctx, "DELETE FROM SystemTagsCache")
	if err != nil {
		return fmt.Errorf("failed to prepare clear cache statement: %w", err)
	}
	defer func() {
		if closeErr := clearStmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close clear cache statement")
		}
	}()

	if _, execErr := clearStmt.ExecContext(ctx); execErr != nil {
		return fmt.Errorf("failed to clear system tags cache: %w", execErr)
	}

	// Populate cache with all system-tag combinations
	populateSQL := `
		INSERT INTO SystemTagsCache (SystemDBID, TagDBID, TagType, Tag)
		SELECT DISTINCT
			s.DBID as SystemDBID,
			t.DBID as TagDBID,
			tt.Type as TagType,
			t.Tag as Tag
		FROM Systems s
		JOIN MediaTitles mtl ON s.DBID = mtl.SystemDBID
		JOIN Media m ON mtl.DBID = m.MediaTitleDBID
		JOIN MediaTags mt ON m.DBID = mt.MediaDBID
		JOIN Tags t ON mt.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		ORDER BY s.DBID, tt.Type, t.Tag`

	populateStmt, err := db.PrepareContext(ctx, populateSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare populate cache statement: %w", err)
	}
	defer func() {
		if closeErr := populateStmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close populate cache statement")
		}
	}()

	if _, err := populateStmt.ExecContext(ctx); err != nil {
		return fmt.Errorf("failed to populate system tags cache: %w", err)
	}

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

	// Step 2: Delete cache entries for these systems
	placeholders := prepareVariadic("?", ",", len(systemDBIDs))
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	deleteSQL := fmt.Sprintf("DELETE FROM SystemTagsCache WHERE SystemDBID IN (%s)", placeholders)
	deleteStmt, err := db.PrepareContext(ctx, deleteSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare selective cache clear statement: %w", err)
	}
	defer func() {
		if closeErr := deleteStmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close selective cache clear statement")
		}
	}()

	args := make([]any, len(systemDBIDs))
	for i, id := range systemDBIDs {
		args[i] = id
	}

	if _, execErr := deleteStmt.ExecContext(ctx, args...); execErr != nil {
		return fmt.Errorf("failed to clear cache for specific systems: %w", execErr)
	}

	// Step 3: Populate cache for these systems only
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	populateSQL := fmt.Sprintf(`
		INSERT INTO SystemTagsCache (SystemDBID, TagDBID, TagType, Tag)
		SELECT DISTINCT
			s.DBID as SystemDBID,
			t.DBID as TagDBID,
			tt.Type as TagType,
			t.Tag as Tag
		FROM Systems s
		JOIN MediaTitles mtl ON s.DBID = mtl.SystemDBID
		JOIN Media m ON mtl.DBID = m.MediaTitleDBID
		JOIN MediaTags mt ON m.DBID = mt.MediaDBID
		JOIN Tags t ON mt.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE s.DBID IN (%s)
		ORDER BY s.DBID, tt.Type, t.Tag`, placeholders)

	populateStmt, err := db.PrepareContext(ctx, populateSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare selective cache populate statement: %w", err)
	}
	defer func() {
		if closeErr := populateStmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close selective cache populate statement")
		}
	}()

	if _, execErr := populateStmt.ExecContext(ctx, args...); execErr != nil {
		return fmt.Errorf("failed to populate cache for specific systems: %w", execErr)
	}

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
		SELECT DISTINCT TagType, Tag
		FROM SystemTagsCache
		WHERE SystemDBID IN (` +
		prepareVariadic("?", ",", len(args)) +
		`)
		ORDER BY TagType, Tag`

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

	tags := make([]database.TagInfo, 0, 100)
	for rows.Next() {
		var tagType, tag string
		if scanErr := rows.Scan(&tagType, &tag); scanErr != nil {
			return nil, fmt.Errorf("failed to scan cached tag result: %w", scanErr)
		}
		tags = append(tags, database.TagInfo{
			Type: tagType,
			Tag:  tag,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tags, nil
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
