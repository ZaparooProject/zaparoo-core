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

	"github.com/rs/zerolog/log"
)

func sqlAnalyze(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "ANALYZE;")
	if err != nil {
		return fmt.Errorf("failed to analyze database: %w", err)
	}
	return nil
}

//goland:noinspection SqlWithoutWhere
func sqlTruncate(ctx context.Context, db *sql.DB) error {
	// Disable foreign keys to avoid CASCADE overhead during mass deletion
	_, err := db.ExecContext(ctx, "PRAGMA foreign_keys = OFF;")
	if err != nil {
		return fmt.Errorf("failed to disable foreign keys: %w", err)
	}
	defer func() {
		// Re-enable foreign keys after truncation
		_, _ = db.ExecContext(ctx, "PRAGMA foreign_keys = ON;")
	}()

	// Delete in reverse dependency order (children first, parents last)
	// to avoid any cascading overhead and minimize index updates
	sqlStmt := `
	delete from MediaProperties;
	delete from MediaTitleProperties;
	delete from MediaTitleTags;
	delete from MediaTags;
	delete from Media;
	delete from MediaTitles;
	delete from Tags;
	delete from TagTypes;
	delete from Systems;
	delete from SlugResolutionCache;
	delete from BrowseDirCounts;
	delete from BrowseEntries;
	delete from BrowseDirs;
	`
	_, err = db.ExecContext(ctx, sqlStmt)
	if err != nil {
		return fmt.Errorf("failed to truncate database: %w", err)
	}
	return nil
}

func sqlTruncateSystems(ctx context.Context, db *sql.DB, systemIDs []string) error {
	if len(systemIDs) == 0 {
		return nil
	}

	// String placeholders for SystemID lookups (e.g. SlugResolutionCache keyed by string).
	strPlaceholders := prepareVariadic("?", ",", len(systemIDs))
	strArgs := make([]any, len(systemIDs))
	for i, id := range systemIDs {
		strArgs[i] = id
	}

	// Pin to a single connection: PRAGMA foreign_keys is session-local, so the PRAGMA
	// and the subsequent DELETEs must execute on the same connection.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection for system truncation: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Str("systems", fmt.Sprintf("%v", systemIDs)).
				Msg("failed to release connection after system truncation")
		}
	}()

	// Resolve SystemID strings → SystemDBID integers so subsequent statements
	// use primary-key lookups instead of string scans.
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	rows, err := conn.QueryContext(ctx,
		fmt.Sprintf("SELECT DBID FROM Systems WHERE SystemID IN (%s)", strPlaceholders), strArgs...)
	if err != nil {
		return fmt.Errorf("failed to resolve system DBIDs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var systemDBIDs []any
	for rows.Next() {
		var dbid int64
		if scanErr := rows.Scan(&dbid); scanErr != nil {
			return fmt.Errorf("failed to scan system DBID: %w", scanErr)
		}
		systemDBIDs = append(systemDBIDs, dbid)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate system DBIDs: %w", err)
	}

	if len(systemDBIDs) == 0 {
		return nil // None of the given systemIDs exist; nothing to do.
	}

	dbidPlaceholders := prepareVariadic("?", ",", len(systemDBIDs))

	// Step 1: collect Tag DBIDs referenced by the target systems BEFORE any deletes.
	// Only these tags can become orphans — bounding the later cleanup to this set
	// avoids a full-table scan of MediaTags/MediaTitleTags/MediaTitleProperties/MediaProperties.
	if _, err = conn.ExecContext(ctx,
		"CREATE TEMP TABLE IF NOT EXISTS _tts_candidate_tags (DBID INTEGER PRIMARY KEY)"); err != nil {
		return fmt.Errorf("failed to create candidate tags temp table: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "DROP TABLE IF EXISTS _tts_candidate_tags")
	}()

	// Each UNION branch needs its own copy of the system DBID args (4 branches).
	candidateArgs := make([]any, 0, len(systemDBIDs)*4)
	for range 4 {
		candidateArgs = append(candidateArgs, systemDBIDs...)
	}
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	if _, err = conn.ExecContext(ctx, fmt.Sprintf(`
		INSERT OR IGNORE INTO _tts_candidate_tags (DBID)
		    SELECT TagDBID FROM MediaTags
		        WHERE MediaDBID IN (SELECT DBID FROM Media WHERE SystemDBID IN (%[1]s))
		UNION
		    SELECT TagDBID FROM MediaTitleTags
		        WHERE MediaTitleDBID IN (SELECT DBID FROM MediaTitles WHERE SystemDBID IN (%[1]s))
		UNION
		    SELECT TypeTagDBID FROM MediaTitleProperties
		        WHERE MediaTitleDBID IN (SELECT DBID FROM MediaTitles WHERE SystemDBID IN (%[1]s))
		UNION
		    SELECT TypeTagDBID FROM MediaProperties
		        WHERE MediaDBID IN (SELECT DBID FROM Media WHERE SystemDBID IN (%[1]s))`,
		dbidPlaceholders), candidateArgs...); err != nil {
		return fmt.Errorf("failed to collect candidate tags: %w", err)
	}

	// Disable FK enforcement to delete children in explicit order without CASCADE overhead.
	// On MiSTer SD card, cascading 50K–200K child rows is orders of magnitude slower than
	// scoped explicit DELETEs.
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("failed to disable foreign keys: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON")
	}()

	// Delete children in reverse dependency order, scoped to target SystemDBIDs.
	// MediaTags references Media(DBID) — must route through Media.SystemDBID.
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	if _, err = conn.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM MediaTags WHERE MediaDBID IN (SELECT DBID FROM Media WHERE SystemDBID IN (%s))",
		dbidPlaceholders), systemDBIDs...); err != nil {
		return fmt.Errorf("failed to delete MediaTags: %w", err)
	}
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	if _, err = conn.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM MediaTitleTags WHERE MediaTitleDBID IN (SELECT DBID FROM MediaTitles WHERE SystemDBID IN (%s))",
		dbidPlaceholders), systemDBIDs...); err != nil {
		return fmt.Errorf("failed to delete MediaTitleTags: %w", err)
	}
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	if _, err = conn.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM MediaTitleProperties"+
			" WHERE MediaTitleDBID IN (SELECT DBID FROM MediaTitles WHERE SystemDBID IN (%s))",
		dbidPlaceholders), systemDBIDs...); err != nil {
		return fmt.Errorf("failed to delete MediaTitleProperties: %w", err)
	}
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	if _, err = conn.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM MediaProperties WHERE MediaDBID IN (SELECT DBID FROM Media WHERE SystemDBID IN (%s))",
		dbidPlaceholders), systemDBIDs...); err != nil {
		return fmt.Errorf("failed to delete MediaProperties: %w", err)
	}
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	if _, err = conn.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM Media WHERE SystemDBID IN (%s)", dbidPlaceholders), systemDBIDs...); err != nil {
		return fmt.Errorf("failed to delete Media: %w", err)
	}
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	if _, err = conn.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM MediaTitles WHERE SystemDBID IN (%s)", dbidPlaceholders), systemDBIDs...); err != nil {
		return fmt.Errorf("failed to delete MediaTitles: %w", err)
	}
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	if _, err = conn.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM Systems WHERE DBID IN (%s)", dbidPlaceholders), systemDBIDs...); err != nil {
		return fmt.Errorf("failed to delete Systems: %w", err)
	}

	// Orphan tag cleanup: delete Tags that were only referenced by the truncated systems.
	// NOT EXISTS uses the mediatags_tag_media_idx / MediaTitleTags PK / MediaTitleProperties/MediaProperties indexes
	// for O(log n) lookups rather than a full-table scan.
	// IMPORTANT: TagTypes are deliberately NOT deleted — they are global infrastructure shared
	// across all systems; deleting them would break remaining systems' tag references.
	if _, err = conn.ExecContext(ctx, `
		DELETE FROM Tags
		    WHERE DBID IN (SELECT DBID FROM _tts_candidate_tags)
		      AND NOT EXISTS (SELECT 1 FROM MediaTags            WHERE TagDBID     = Tags.DBID)
		      AND NOT EXISTS (SELECT 1 FROM MediaTitleTags       WHERE TagDBID     = Tags.DBID)
		      AND NOT EXISTS (SELECT 1 FROM MediaTitleProperties WHERE TypeTagDBID = Tags.DBID)
		      AND NOT EXISTS (SELECT 1 FROM MediaProperties      WHERE TypeTagDBID = Tags.DBID)`); err != nil {
		return fmt.Errorf("failed to clean up orphaned tags: %w", err)
	}

	// Cache invalidation — not FK-dependent, so these run after PRAGMA FK ON restores.
	if _, err = conn.ExecContext(ctx, "DELETE FROM MediaCountCache"); err != nil {
		log.Warn().Err(err).Msg("failed to invalidate media count cache during system truncation")
	}
	if _, err = conn.ExecContext(ctx, "DELETE FROM SystemTagsCache"); err != nil {
		log.Warn().Err(err).Msg("failed to invalidate system tags cache during system truncation")
	}
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	slugStmt := fmt.Sprintf("DELETE FROM SlugResolutionCache WHERE SystemID IN (%s)", strPlaceholders)
	if _, err = conn.ExecContext(ctx, slugStmt, strArgs...); err != nil {
		log.Warn().Err(err).Msg("failed to invalidate slug resolution cache during system truncation")
	}

	return nil
}

func sqlVacuum(ctx context.Context, db *sql.DB) error {
	sqlStmt := `
	vacuum;
	`
	_, err := db.ExecContext(ctx, sqlStmt)
	if err != nil {
		return fmt.Errorf("failed to vacuum database: %w", err)
	}
	return nil
}

// sqlCleanMediaOrphans removes Media rows where IsMissing=1 along with their
// associated child rows.  MediaTitle rows that become fully orphaned (no
// surviving Media row) are also removed together with their tag and property
// child rows.  Tags that are no longer referenced anywhere are pruned.
// Returns the number of Media rows deleted.
func sqlCleanMediaOrphans(ctx context.Context, db *sql.DB) (int64, error) {
	// Quick check: skip all temp-table setup when nothing needs cleaning.
	var missingCount int64
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM Media WHERE IsMissing = 1",
	).Scan(&missingCount); err != nil {
		return 0, fmt.Errorf("failed to count missing media: %w", err)
	}
	if missingCount == 0 {
		return 0, nil
	}

	// Pin to a single connection: PRAGMA foreign_keys is session-local, so
	// the PRAGMA and the subsequent DELETEs must run on the same connection.
	conn, err := db.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to acquire connection for orphan cleanup: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to release connection after orphan cleanup")
		}
	}()

	// Temp table: MediaTitle DBIDs that become fully orphaned once the missing
	// Media rows are deleted (every Media row for that title is missing).
	if _, err = conn.ExecContext(ctx,
		"CREATE TEMP TABLE IF NOT EXISTS _cmo_orphan_titles (DBID INTEGER PRIMARY KEY)",
	); err != nil {
		return 0, fmt.Errorf("failed to create orphan titles temp table: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "DROP TABLE IF EXISTS _cmo_orphan_titles")
	}()

	// Temp table: Tag DBIDs that may become orphans after this cleanup.
	if _, err = conn.ExecContext(ctx,
		"CREATE TEMP TABLE IF NOT EXISTS _cmo_candidate_tags (DBID INTEGER PRIMARY KEY)",
	); err != nil {
		return 0, fmt.Errorf("failed to create candidate tags temp table: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "DROP TABLE IF EXISTS _cmo_candidate_tags")
	}()

	// A MediaTitle is orphaned when every Media row it owns has IsMissing=1
	// (there is no surviving IsMissing=0 sibling).
	if _, err = conn.ExecContext(ctx, `
		INSERT OR IGNORE INTO _cmo_orphan_titles (DBID)
		SELECT DISTINCT m.MediaTitleDBID
		FROM   Media m
		WHERE  m.IsMissing = 1
		  AND  NOT EXISTS (
		           SELECT 1 FROM Media m2
		           WHERE  m2.MediaTitleDBID = m.MediaTitleDBID
		             AND  m2.IsMissing = 0
		       )`,
	); err != nil {
		return 0, fmt.Errorf("failed to identify orphaned MediaTitles: %w", err)
	}

	// Collect every Tag DBID that could become an orphan.  Bounding the later
	// cleanup to this candidate set avoids full-table scans.
	if _, err = conn.ExecContext(ctx, `
		INSERT OR IGNORE INTO _cmo_candidate_tags (DBID)
		    SELECT TagDBID     FROM MediaTags
		        WHERE MediaDBID       IN (SELECT DBID FROM Media WHERE IsMissing = 1)
		UNION
		    SELECT TagDBID     FROM MediaTitleTags
		        WHERE MediaTitleDBID  IN (SELECT DBID FROM _cmo_orphan_titles)
		UNION
		    SELECT TypeTagDBID FROM MediaTitleProperties
		        WHERE MediaTitleDBID  IN (SELECT DBID FROM _cmo_orphan_titles)
		UNION
		    SELECT TypeTagDBID FROM MediaProperties
		        WHERE MediaDBID       IN (SELECT DBID FROM Media WHERE IsMissing = 1)`,
	); err != nil {
		return 0, fmt.Errorf("failed to collect candidate tags: %w", err)
	}

	// Disable FK enforcement so children can be deleted explicitly in the
	// optimal order without CASCADE overhead (matters on low-power devices).
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return 0, fmt.Errorf("failed to disable foreign keys: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON")
	}()

	// Delete file-level child rows for missing Media.
	if _, err = conn.ExecContext(ctx,
		"DELETE FROM MediaTags WHERE MediaDBID IN (SELECT DBID FROM Media WHERE IsMissing = 1)",
	); err != nil {
		return 0, fmt.Errorf("failed to delete MediaTags for missing media: %w", err)
	}
	if _, err = conn.ExecContext(ctx,
		"DELETE FROM MediaProperties WHERE MediaDBID IN (SELECT DBID FROM Media WHERE IsMissing = 1)",
	); err != nil {
		return 0, fmt.Errorf("failed to delete MediaProperties for missing media: %w", err)
	}

	// Delete the missing Media rows themselves.
	res, err := conn.ExecContext(ctx, "DELETE FROM Media WHERE IsMissing = 1")
	if err != nil {
		return 0, fmt.Errorf("failed to delete missing media: %w", err)
	}
	deleted, _ := res.RowsAffected()

	// Delete title-level child rows for orphaned MediaTitles.
	if _, err = conn.ExecContext(ctx,
		"DELETE FROM MediaTitleTags WHERE MediaTitleDBID IN (SELECT DBID FROM _cmo_orphan_titles)",
	); err != nil {
		return 0, fmt.Errorf("failed to delete MediaTitleTags for orphaned titles: %w", err)
	}
	if _, err = conn.ExecContext(ctx,
		"DELETE FROM MediaTitleProperties WHERE MediaTitleDBID IN (SELECT DBID FROM _cmo_orphan_titles)",
	); err != nil {
		return 0, fmt.Errorf("failed to delete MediaTitleProperties for orphaned titles: %w", err)
	}

	// Delete the orphaned MediaTitle rows.
	if _, err = conn.ExecContext(ctx,
		"DELETE FROM MediaTitles WHERE DBID IN (SELECT DBID FROM _cmo_orphan_titles)",
	); err != nil {
		return 0, fmt.Errorf("failed to delete orphaned MediaTitles: %w", err)
	}

	// Remove Tags that are now referenced by nothing.  NOT EXISTS lookups use
	// existing indexes for O(log n) checks rather than full-table scans.
	// TagTypes are deliberately preserved — they are global infrastructure
	// shared across all systems.
	if _, err = conn.ExecContext(ctx, `
		DELETE FROM Tags
		WHERE DBID IN (SELECT DBID FROM _cmo_candidate_tags)
		  AND NOT EXISTS (SELECT 1 FROM MediaTags            WHERE TagDBID     = Tags.DBID)
		  AND NOT EXISTS (SELECT 1 FROM MediaTitleTags       WHERE TagDBID     = Tags.DBID)
		  AND NOT EXISTS (SELECT 1 FROM MediaTitleProperties WHERE TypeTagDBID = Tags.DBID)
		  AND NOT EXISTS (SELECT 1 FROM MediaProperties      WHERE TypeTagDBID = Tags.DBID)`,
	); err != nil {
		return 0, fmt.Errorf("failed to clean up orphaned tags: %w", err)
	}

	return deleted, nil
}
