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
	delete from SupportingMedia;
	delete from MediaTitleTags;
	delete from MediaTags;
	delete from Media;
	delete from MediaTitles;
	delete from Tags;
	delete from TagTypes;
	delete from Systems;
	delete from SlugResolutionCache;
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

	// Create placeholders for IN clause
	placeholders := prepareVariadic("?", ",", len(systemIDs))

	// Convert systemIDs to interface slice for query parameters
	args := make([]any, len(systemIDs))
	for i, id := range systemIDs {
		args[i] = id
	}

	// With proper foreign keys, just delete Systems
	// CASCADE handles: MediaTitles → Media → MediaTags
	//                  MediaTitles → SupportingMedia
	//                  MediaTitles → MediaTitleTags
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	deleteStmt := fmt.Sprintf("DELETE FROM Systems WHERE SystemID IN (%s)", placeholders)
	_, err := db.ExecContext(ctx, deleteStmt, args...)
	if err != nil {
		return fmt.Errorf("failed to delete systems: %w", err)
	}

	// Clean up orphaned tags (RESTRICT prevents cascade, so we handle these separately)
	// Only deletes truly orphaned tags that aren't referenced anywhere
	// IMPORTANT: Do NOT delete TagTypes during selective indexing - they are global infrastructure
	// shared across all systems. Deleting them would break other systems' media that reference
	// TagTypes not used by the system being reindexed (e.g., "Extension" TagType).
	cleanupStmt := `
		DELETE FROM Tags WHERE DBID NOT IN (
			SELECT TagDBID FROM MediaTags WHERE TagDBID IS NOT NULL
			UNION
			SELECT TagDBID FROM MediaTitleTags WHERE TagDBID IS NOT NULL
			UNION
			SELECT TypeTagDBID FROM SupportingMedia WHERE TypeTagDBID IS NOT NULL
		);
	`
	_, err = db.ExecContext(ctx, cleanupStmt)
	if err != nil {
		return fmt.Errorf("failed to clean up orphaned tags: %w", err)
	}

	// Invalidate media count cache since system data was modified
	_, err = db.ExecContext(ctx, "DELETE FROM MediaCountCache")
	if err != nil {
		// Log warning but don't fail the operation - cache invalidation is not critical
		log.Warn().Err(err).Msg("failed to invalidate media count cache during system truncation")
	}

	// Invalidate system tags cache since system data was modified
	_, err = db.ExecContext(ctx, "DELETE FROM SystemTagsCache")
	if err != nil {
		// Log warning but don't fail the operation - cache invalidation is not critical
		log.Warn().Err(err).Msg("failed to invalidate system tags cache during system truncation")
	}

	// Invalidate slug resolution cache for the affected systems
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	slugCacheDeleteStmt := fmt.Sprintf("DELETE FROM SlugResolutionCache WHERE SystemID IN (%s)", placeholders)
	_, err = db.ExecContext(ctx, slugCacheDeleteStmt, args...)
	if err != nil {
		// Log warning but don't fail the operation - cache invalidation is not critical
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
