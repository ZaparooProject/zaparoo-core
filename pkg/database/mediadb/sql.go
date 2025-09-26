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
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
)

// Queries go here to keep the interface clean

//go:embed migrations/*.sql
var migrationFiles embed.FS

const (
	DBConfigLastGeneratedAt    = "LastGeneratedAt"
	DBConfigOptimizationStatus = "OptimizationStatus"
	DBConfigOptimizationStep   = "OptimizationStep"
	DBConfigIndexingStatus     = "IndexingStatus"
	DBConfigLastIndexedSystem  = "LastIndexedSystem"
	DBConfigIndexingSystems    = "IndexingSystems"
)

func sqlMigrateUp(db *sql.DB) error {
	if err := database.MigrateUp(db, migrationFiles, "migrations"); err != nil {
		return fmt.Errorf("failed to run media database migrations: %w", err)
	}
	return nil
}

func sqlAllocate(db *sql.DB) error {
	return sqlMigrateUp(db)
}

func sqlUpdateLastGenerated(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx,
		fmt.Sprintf(
			"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES ('%s', ?)",
			DBConfigLastGeneratedAt,
		),
		strconv.FormatInt(time.Now().Unix(), 10),
	)
	if err != nil {
		return fmt.Errorf("failed to set last generated timestamp: %w", err)
	}
	return nil
}

func sqlGetLastGenerated(ctx context.Context, db *sql.DB) (time.Time, error) {
	var rawTimestamp string
	err := db.QueryRowContext(ctx,
		fmt.Sprintf(
			"SELECT Value FROM DBConfig WHERE Name = '%s'",
			DBConfigLastGeneratedAt,
		),
	).Scan(&rawTimestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	} else if err != nil {
		return time.Time{}, fmt.Errorf("failed to scan timestamp: %w", err)
	}

	timestamp, err := strconv.Atoi(rawTimestamp)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse timestamp: %w", err)
	}

	return time.Unix(int64(timestamp), 0), nil
}

func sqlSetOptimizationStatus(ctx context.Context, db *sql.DB, status string) error {
	_, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigOptimizationStatus,
		status,
	)
	if err != nil {
		return fmt.Errorf("failed to set optimization status: %w", err)
	}
	return nil
}

func sqlGetOptimizationStatus(ctx context.Context, db *sql.DB) (string, error) {
	var status string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigOptimizationStatus,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("failed to get optimization status: %w", err)
	}
	return status, nil
}

func sqlSetOptimizationStep(ctx context.Context, db *sql.DB, step string) error {
	_, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigOptimizationStep,
		step,
	)
	if err != nil {
		return fmt.Errorf("failed to set optimization step: %w", err)
	}
	return nil
}

func sqlGetOptimizationStep(ctx context.Context, db *sql.DB) (string, error) {
	var step string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigOptimizationStep,
	).Scan(&step)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("failed to get optimization step: %w", err)
	}
	return step, nil
}

func sqlSetIndexingStatus(ctx context.Context, db *sql.DB, status string) error {
	_, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigIndexingStatus,
		status,
	)
	if err != nil {
		return fmt.Errorf("failed to set indexing status: %w", err)
	}
	return nil
}

func sqlGetIndexingStatus(ctx context.Context, db *sql.DB) (string, error) {
	var status string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigIndexingStatus,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("failed to get indexing status: %w", err)
	}
	return status, nil
}

func sqlSetLastIndexedSystem(ctx context.Context, db *sql.DB, systemID string) error {
	_, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigLastIndexedSystem,
		systemID,
	)
	if err != nil {
		return fmt.Errorf("failed to set last indexed system: %w", err)
	}
	return nil
}

func sqlGetLastIndexedSystem(ctx context.Context, db *sql.DB) (string, error) {
	var systemID string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigLastIndexedSystem,
	).Scan(&systemID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("failed to get last indexed system: %w", err)
	}
	return systemID, nil
}

func sqlSetIndexingSystems(ctx context.Context, db *sql.DB, systemIDs []string) error {
	systemsJSON, err := json.Marshal(systemIDs)
	if err != nil {
		return fmt.Errorf("failed to marshal systems to JSON: %w", err)
	}
	_, err = db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigIndexingSystems,
		string(systemsJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to set indexing systems: %w", err)
	}
	return nil
}

func sqlGetIndexingSystems(ctx context.Context, db *sql.DB) ([]string, error) {
	var systemsJSON string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigIndexingSystems,
	).Scan(&systemsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get indexing systems: %w", err)
	}

	var systemIDs []string
	err = json.Unmarshal([]byte(systemsJSON), &systemIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal indexing systems: %w", err)
	}
	return systemIDs, nil
}

func sqlAnalyze(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "ANALYZE;")
	if err != nil {
		return fmt.Errorf("failed to analyze database: %w", err)
	}
	return nil
}

//goland:noinspection SqlWithoutWhere
func sqlTruncate(ctx context.Context, db *sql.DB) error {
	sqlStmt := `
	delete from Systems;
	delete from MediaTitles;
	delete from Media;
	delete from TagTypes;
	delete from Tags;
	delete from MediaTags;
	delete from MediaTitleTags;
	delete from SupportingMedia;
	vacuum;
	`
	_, err := db.ExecContext(ctx, sqlStmt)
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
	cleanupStmt := `
		DELETE FROM Tags WHERE DBID NOT IN (
			SELECT TagDBID FROM MediaTags WHERE TagDBID IS NOT NULL
			UNION
			SELECT TagDBID FROM MediaTitleTags WHERE TagDBID IS NOT NULL
			UNION
			SELECT TypeTagDBID FROM SupportingMedia WHERE TypeTagDBID IS NOT NULL
		);
		DELETE FROM TagTypes WHERE DBID NOT IN (
			SELECT TypeDBID FROM Tags WHERE TypeDBID IS NOT NULL
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

func sqlFindSystem(ctx context.Context, db *sql.DB, system database.System) (database.System, error) {
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

func sqlFindSystemBySystemID(ctx context.Context, db *sql.DB, systemID string) (database.System, error) {
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
	if err != nil {
		return row, fmt.Errorf("failed to scan system row: %w", err)
	}
	return row, nil
}

const (
	insertSystemSQL     = `INSERT INTO Systems (DBID, SystemID, Name) VALUES (?, ?, ?)`
	insertMediaTitleSQL = `INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (?, ?, ?, ?)`
	insertMediaSQL      = `INSERT INTO Media (DBID, MediaTitleDBID, Path) VALUES (?, ?, ?)`
	insertTagSQL        = `INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES (?, ?, ?)`
	insertMediaTagSQL   = `INSERT OR IGNORE INTO MediaTags (MediaDBID, TagDBID) VALUES (?, ?)`
)

// Fast prepared statement execution functions for batch operations during scanning
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

func sqlInsertMediaWithPreparedStmt(ctx context.Context, stmt *sql.Stmt, row database.Media) (database.Media, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	res, err := stmt.ExecContext(ctx, dbID, row.MediaTitleDBID, row.Path)
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

func sqlInsertTagWithPreparedStmt(ctx context.Context, stmt *sql.Stmt, row database.Tag) (database.Tag, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	res, err := stmt.ExecContext(ctx, dbID, row.TypeDBID, row.Tag)
	if err != nil {
		return row, fmt.Errorf("failed to execute prepared insert tag statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for tag: %w", err)
	}

	row.DBID = lastID
	return row, nil
}

func sqlInsertMediaTagWithPreparedStmt(
	ctx context.Context, stmt *sql.Stmt, row database.MediaTag,
) (database.MediaTag, error) {
	res, err := stmt.ExecContext(ctx, row.MediaDBID, row.TagDBID)
	if err != nil {
		return row, fmt.Errorf("failed to execute prepared insert media tag statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for media tag: %w", err)
	}

	if lastID == 0 {
		// INSERT OR IGNORE occurred - row already existed, no new ID generated
		log.Debug().Int64("MediaDBID", row.MediaDBID).Int64("TagDBID", row.TagDBID).
			Msg("MediaTag already exists, INSERT OR IGNORE executed")
		// Note: row.DBID remains as originally provided (usually 0)
	} else {
		row.DBID = lastID
	}

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

func sqlFindMediaTitle(ctx context.Context, db *sql.DB, title database.MediaTitle) (database.MediaTitle, error) {
	var row database.MediaTitle
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, SystemDBID, Slug, Name
		from MediaTitles
		where DBID = ?
		or Slug = ?
		LIMIT 1;
	`)
	if err != nil {
		return row, fmt.Errorf("failed to prepare find media title statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx,
		title.DBID,
		title.Slug,
	).Scan(
		&row.DBID,
		&row.SystemDBID,
		&row.Slug,
		&row.Name,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan media title row: %w", err)
	}
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

func sqlFindMedia(ctx context.Context, db *sql.DB, media database.Media) (database.Media, error) {
	var row database.Media
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, MediaTitleDBID, Path
		from Media
		where DBID = ?
		or (
			MediaTitleDBID = ?
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
		media.Path,
	).Scan(
		&row.DBID,
		&row.MediaTitleDBID,
		&row.Path,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan media row: %w", err)
	}
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

	res, err := stmt.ExecContext(ctx, dbID, row.MediaTitleDBID, row.Path)
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

func sqlFindTagType(ctx context.Context, db *sql.DB, tagType database.TagType) (database.TagType, error) {
	var row database.TagType
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, Type
		from TagTypes
		where DBID = ?
		or Type = ?
		LIMIT 1;
	`)
	if err != nil {
		return row, fmt.Errorf("failed to prepare find tag type statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx,
		tagType.DBID,
		tagType.Type,
	).Scan(
		&row.DBID,
		&row.Type,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan tag type row: %w", err)
	}
	return row, nil
}

func sqlInsertTagType(ctx context.Context, db *sql.DB, row database.TagType) (database.TagType, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}
	stmt, err := db.PrepareContext(ctx, `
		insert into
		TagTypes
		(DBID, Type)
		values (?, ?)
	`)
	if err != nil {
		return row, fmt.Errorf("failed to prepare insert tag type statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	res, err := stmt.ExecContext(ctx,
		dbID,
		row.Type,
	)
	if err != nil {
		return row, fmt.Errorf("failed to execute insert tag type statement: %w", err)
	}
	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for tag type: %w", err)
	}
	row.DBID = lastID
	return row, nil
}

func sqlFindTag(ctx context.Context, db *sql.DB, tagType database.Tag) (database.Tag, error) {
	var row database.Tag
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, TypeDBID, Tag
		from Tags
		where DBID = ?
		or Tag = ?
		LIMIT 1;
	`)
	// TODO: Add TagType dependency when unknown tags supported
	if err != nil {
		return row, fmt.Errorf("failed to prepare find tag statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx,
		tagType.DBID,
		tagType.Tag,
	).Scan(
		&row.DBID,
		&row.TypeDBID,
		&row.Tag,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan tag row: %w", err)
	}
	return row, nil
}

func sqlInsertTag(ctx context.Context, db *sql.DB, row database.Tag) (database.Tag, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	stmt, err := db.PrepareContext(ctx, insertTagSQL)
	if err != nil {
		return row, fmt.Errorf("failed to prepare insert tag statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	res, err := stmt.ExecContext(ctx, dbID, row.TypeDBID, row.Tag)
	if err != nil {
		return row, fmt.Errorf("failed to execute insert tag statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for tag: %w", err)
	}

	row.DBID = lastID
	return row, nil
}

func sqlFindMediaTag(ctx context.Context, db *sql.DB, mediaTag database.MediaTag) (database.MediaTag, error) {
	var row database.MediaTag
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, MediaDBID, TagDBID
		from MediaTags
		where DBID = ?
		or (
			MediaDBID = ?
			and TagDBID = ?
		)
		LIMIT 1;
	`)
	if err != nil {
		return row, fmt.Errorf("failed to prepare find media tag statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx,
		mediaTag.DBID,
		mediaTag.MediaDBID,
		mediaTag.TagDBID,
	).Scan(
		&row.DBID,
		&row.MediaDBID,
		&row.TagDBID,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan media tag row: %w", err)
	}
	return row, nil
}

func sqlInsertMediaTag(ctx context.Context, db *sql.DB, row database.MediaTag) (database.MediaTag, error) {
	stmt, err := db.PrepareContext(ctx, insertMediaTagSQL)
	if err != nil {
		return row, fmt.Errorf("failed to prepare insert media tag statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	res, err := stmt.ExecContext(ctx, row.MediaDBID, row.TagDBID)
	if err != nil {
		return row, fmt.Errorf("failed to execute insert media tag statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for media tag: %w", err)
	}

	if lastID == 0 {
		// INSERT OR IGNORE occurred - row already existed, no new ID generated
		log.Debug().Int64("MediaDBID", row.MediaDBID).Int64("TagDBID", row.TagDBID).
			Msg("MediaTag already exists, INSERT OR IGNORE executed")
		// Note: row.DBID remains as originally provided (usually 0)
	} else {
		row.DBID = lastID
	}

	return row, nil
}

// Not in use
/*
func sqlCleanInactiveMedia(db *sql.DB) error {
	_, err := db.Exec(`
		delete from MediaTitles
		where DBID in (
			select MediaTitleDBID
			from Media
			where IsActive = 0
			group by MediaTitleDBID
		);

		delete from MediaTags
		where MediaDBID in (
			select DBID
			from Media
			where IsActive = 0
		);

		delete from Media
		where IsActive = 0;
	`)
	return err
}
*/

// return ?, ?,... based on count
func prepareVariadic(p, s string, c int) string {
	if c < 1 {
		return ""
	}
	q := make([]string, c)
	for i := range q {
		q[i] = p
	}
	return strings.Join(q, s)
}

func sqlSearchMediaPathExact(
	ctx context.Context,
	db *sql.DB,
	systems []systemdefs.System,
	path string,
) ([]database.SearchResult, error) {
	// query == path
	if len(systems) == 0 {
		return nil, errors.New("no systems provided for media search")
	}
	slug := helpers.SlugifyPath(path)

	results := make([]database.SearchResult, 0, 1)
	args := make([]any, 0)
	for _, sys := range systems {
		args = append(args, sys.ID)
	}
	args = append(args, slug, path)

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	stmt, err := db.PrepareContext(ctx, `
		select 
			Systems.SystemID,
			Media.Path
		from Systems
		inner join MediaTitles
			on Systems.DBID = MediaTitles.SystemDBID
		inner join Media
			on MediaTitles.DBID = Media.MediaTitleDBID
		where Systems.SystemID IN (`+
		prepareVariadic("?", ",", len(systems))+
		`)
		and MediaTitles.Slug = ?
		and Media.Path = ?
		LIMIT 1
	`)
	if err != nil {
		return results, fmt.Errorf("failed to prepare media path exact search statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx,
		args...,
	)
	if err != nil {
		return results, fmt.Errorf("failed to execute media path exact search query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	for rows.Next() {
		result := database.SearchResult{}
		if scanErr := rows.Scan(
			&result.SystemID,
			&result.Path,
		); scanErr != nil {
			return results, fmt.Errorf("failed to scan search result: %w", scanErr)
		}
		result.Name = helpers.FilenameFromPath(result.Path)
		results = append(results, result)
	}
	err = rows.Err()
	if err != nil {
		return results, err
	}
	return results, nil
}

func sqlSearchMediaPathParts(
	ctx context.Context,
	db *sql.DB,
	systems []systemdefs.System,
	parts []string,
) ([]database.SearchResult, error) {
	results := make([]database.SearchResult, 0, 250)

	if len(systems) == 0 {
		return nil, errors.New("no systems provided for media search")
	}

	// search for anything in systems on blank query
	if len(parts) == 0 {
		parts = []string{""}
	}

	args := make([]any, 0)
	for _, sys := range systems {
		args = append(args, sys.ID)
	}
	for _, p := range parts {
		args = append(args, "%"+p+"%")
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	stmt, err := db.PrepareContext(ctx, `
		select
			Systems.SystemID,
			Media.Path
		from Systems
		inner join MediaTitles
			on Systems.DBID = MediaTitles.SystemDBID
		inner join Media
			on MediaTitles.DBID = Media.MediaTitleDBID
		where Systems.SystemID IN (`+
		prepareVariadic("?", ",", len(systems))+
		`)
		and `+
		prepareVariadic(" MediaTitles.Slug like ? ", " and ", len(parts))+
		` LIMIT 250
	`)
	if err != nil {
		return results, fmt.Errorf("failed to prepare media path parts search statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx,
		args...,
	)
	if err != nil {
		return results, fmt.Errorf("failed to execute media path parts search query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	for rows.Next() {
		result := database.SearchResult{}
		if scanErr := rows.Scan(
			&result.SystemID,
			&result.Path,
		); scanErr != nil {
			return results, fmt.Errorf("failed to scan search result: %w", scanErr)
		}
		result.Name = helpers.FilenameFromPath(result.Path)
		results = append(results, result)
	}
	err = rows.Err()
	if err != nil {
		return results, err
	}
	return results, nil
}

func sqlSearchMediaPathPartsWithCursor(
	ctx context.Context,
	db *sql.DB,
	systems []systemdefs.System,
	parts []string,
	cursor *int64,
	limit int,
) ([]database.SearchResultWithCursor, error) {
	results := make([]database.SearchResultWithCursor, 0, limit)
	if len(systems) == 0 {
		return nil, errors.New("no systems provided for media search")
	}

	// Search for anything in systems on blank query
	if len(parts) == 0 {
		parts = []string{""}
	}

	args := make([]any, 0)
	for _, sys := range systems {
		args = append(args, sys.ID)
	}
	for _, p := range parts {
		args = append(args, "%"+p+"%")
	}

	// Add cursor condition if provided
	cursorCondition := ""
	if cursor != nil {
		cursorCondition = " AND Media.DBID > ? "
		args = append(args, *cursor)
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	sqlQuery := `
		select
			Systems.SystemID,
			Media.Path,
			Media.DBID,
			json_group_array(
				json_object(
					'tag', Tags.Tag,
					'type', TagTypes.Type
				)
			) FILTER (WHERE Tags.Tag IS NOT NULL) as tags
		from Systems
		inner join MediaTitles
			on Systems.DBID = MediaTitles.SystemDBID
		inner join Media
			on MediaTitles.DBID = Media.MediaTitleDBID
		left join MediaTags
			on Media.DBID = MediaTags.MediaDBID
		left join Tags
			on MediaTags.TagDBID = Tags.DBID
		left join TagTypes
			on Tags.TypeDBID = TagTypes.DBID
		where Systems.SystemID IN (` +
		prepareVariadic("?", ",", len(systems)) +
		`)
		and ` +
		prepareVariadic(" MediaTitles.Slug like ? ", " and ", len(parts)) +
		cursorCondition +
		` GROUP BY Media.DBID, Systems.SystemID, Media.Path
		ORDER BY Media.DBID ASC
		LIMIT ?`

	args = append(args, limit)

	stmt, err := db.PrepareContext(ctx, sqlQuery)
	if err != nil {
		return results, fmt.Errorf("failed to prepare cursor-based media search statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return results, fmt.Errorf("failed to execute cursor-based media search query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()

	for rows.Next() {
		result := database.SearchResultWithCursor{}
		var tagsJSON sql.NullString
		if scanErr := rows.Scan(
			&result.SystemID,
			&result.Path,
			&result.MediaID,
			&tagsJSON,
		); scanErr != nil {
			return results, fmt.Errorf("failed to scan cursor-based search result: %w", scanErr)
		}
		result.Name = helpers.FilenameFromPath(result.Path)

		// Parse tags from JSON
		if tagsJSON.Valid && tagsJSON.String != "" && tagsJSON.String != "null" {
			if unmarshalErr := json.Unmarshal([]byte(tagsJSON.String), &result.Tags); unmarshalErr != nil {
				log.Error().Err(unmarshalErr).Str("tags_json", tagsJSON.String).
					Int64("media_id", result.MediaID).
					Msg("failed to unmarshal tags JSON from database")
				result.Tags = []database.TagInfo{}
			}
		} else {
			result.Tags = []database.TagInfo{}
		}

		results = append(results, result)
	}
	err = rows.Err()
	if err != nil {
		return results, err
	}
	return results, nil
}

func sqlSearchMediaWithFilters(
	ctx context.Context,
	db *sql.DB,
	systems []systemdefs.System,
	parts []string,
	tags []string,
	cursor *int64,
	limit int,
) ([]database.SearchResultWithCursor, error) {
	results := make([]database.SearchResultWithCursor, 0, limit)
	if len(systems) == 0 {
		return nil, errors.New("no systems provided for media search")
	}

	// Search for anything in systems on blank query
	if len(parts) == 0 {
		parts = []string{""}
	}

	args := make([]any, 0)
	for _, sys := range systems {
		args = append(args, sys.ID)
	}
	for _, p := range parts {
		args = append(args, "%"+p+"%")
	}

	// Add cursor condition if provided
	cursorCondition := ""
	if cursor != nil {
		cursorCondition = " AND Media.DBID > ? "
		args = append(args, *cursor)
	}

	// Add tag filtering condition
	tagFilterCondition := ""
	if len(tags) > 0 {
		tagFilterCondition = `
			AND Media.DBID IN (
				SELECT MediaDBID FROM MediaTags
				JOIN Tags ON MediaTags.TagDBID = Tags.DBID
				WHERE Tags.Tag IN (` + prepareVariadic("?", ",", len(tags)) + `)
			)`
		for _, tag := range tags {
			args = append(args, tag)
		}
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	sqlQuery := `
		select
			Systems.SystemID,
			Media.Path,
			Media.DBID,
			json_group_array(
				json_object(
					'tag', Tags.Tag,
					'type', TagTypes.Type
				)
			) FILTER (WHERE Tags.Tag IS NOT NULL) as tags
		from Systems
		inner join MediaTitles
			on Systems.DBID = MediaTitles.SystemDBID
		inner join Media
			on MediaTitles.DBID = Media.MediaTitleDBID
		left join MediaTags
			on Media.DBID = MediaTags.MediaDBID
		left join Tags
			on MediaTags.TagDBID = Tags.DBID
		left join TagTypes
			on Tags.TypeDBID = TagTypes.DBID
		where Systems.SystemID IN (` +
		prepareVariadic("?", ",", len(systems)) +
		`)
		and ` +
		prepareVariadic(" MediaTitles.Slug like ? ", " and ", len(parts)) +
		cursorCondition +
		tagFilterCondition +
		` GROUP BY Media.DBID, Systems.SystemID, Media.Path
		ORDER BY Media.DBID ASC
		LIMIT ?`

	args = append(args, limit)

	stmt, err := db.PrepareContext(ctx, sqlQuery)
	if err != nil {
		return results, fmt.Errorf("failed to prepare filtered media search statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return results, fmt.Errorf("failed to execute filtered media search query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()

	for rows.Next() {
		result := database.SearchResultWithCursor{}
		var tagsJSON sql.NullString
		if scanErr := rows.Scan(
			&result.SystemID,
			&result.Path,
			&result.MediaID,
			&tagsJSON,
		); scanErr != nil {
			return results, fmt.Errorf("failed to scan filtered search result: %w", scanErr)
		}
		result.Name = helpers.FilenameFromPath(result.Path)

		// Parse tags from JSON
		if tagsJSON.Valid && tagsJSON.String != "" && tagsJSON.String != "null" {
			if unmarshalErr := json.Unmarshal([]byte(tagsJSON.String), &result.Tags); unmarshalErr != nil {
				log.Error().Err(unmarshalErr).Str("tags_json", tagsJSON.String).
					Int64("media_id", result.MediaID).
					Msg("failed to unmarshal tags JSON from database")
				result.Tags = []database.TagInfo{}
			}
		} else {
			result.Tags = []database.TagInfo{}
		}

		results = append(results, result)
	}
	err = rows.Err()
	if err != nil {
		return results, err
	}
	return results, nil
}

func sqlGetTagFacets(
	ctx context.Context,
	db *sql.DB,
	systems []systemdefs.System,
	parts []string,
	tags []string,
) ([]database.TagTypeFacet, error) {
	if len(systems) == 0 {
		return nil, errors.New("no systems provided for facet search")
	}

	// Search for anything in systems on blank query
	if len(parts) == 0 {
		parts = []string{""}
	}

	args := make([]any, 0)
	for _, sys := range systems {
		args = append(args, sys.ID)
	}
	for _, p := range parts {
		args = append(args, "%"+p+"%")
	}

	// Add tag filtering condition
	tagFilterCondition := ""
	if len(tags) > 0 {
		tagFilterCondition = `
			AND EXISTS (
				SELECT 1 FROM MediaTags mt2
				JOIN Tags t2 ON mt2.TagDBID = t2.DBID
				WHERE mt2.MediaDBID = Media.DBID
				AND t2.Tag IN (` + prepareVariadic("?", ",", len(tags)) + `)
			)`
		for _, tag := range tags {
			args = append(args, tag)
		}
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	sqlQuery := `
		SELECT
			TagTypes.Type,
			Tags.Tag,
			COUNT(DISTINCT Media.DBID) as count
		FROM TagTypes
		JOIN Tags ON TagTypes.DBID = Tags.TypeDBID
		JOIN MediaTags ON Tags.DBID = MediaTags.TagDBID
		JOIN Media ON MediaTags.MediaDBID = Media.DBID
		JOIN MediaTitles ON Media.MediaTitleDBID = MediaTitles.DBID
		JOIN Systems ON MediaTitles.SystemDBID = Systems.DBID
		WHERE Systems.SystemID IN (` +
		prepareVariadic("?", ",", len(systems)) +
		`)
		AND ` +
		prepareVariadic(" MediaTitles.Slug like ? ", " AND ", len(parts)) +
		tagFilterCondition +
		` GROUP BY TagTypes.Type, Tags.Tag
		ORDER BY TagTypes.Type, count DESC, Tags.Tag`

	stmt, err := db.PrepareContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare tag facets statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute tag facets query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()

	// Group facets by type
	facetMap := make(map[string][]database.TagFacet)
	for rows.Next() {
		var tagType, tag string
		var count int
		if scanErr := rows.Scan(&tagType, &tag, &count); scanErr != nil {
			return nil, fmt.Errorf("failed to scan tag facet result: %w", scanErr)
		}

		if facetMap[tagType] == nil {
			facetMap[tagType] = make([]database.TagFacet, 0)
		}
		facetMap[tagType] = append(facetMap[tagType], database.TagFacet{
			Tag:   tag,
			Count: count,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Convert map to slice
	result := make([]database.TagTypeFacet, 0, len(facetMap))
	for tagType, values := range facetMap {
		result = append(result, database.TagTypeFacet{
			Type:   tagType,
			Values: values,
		})
	}

	return result, nil
}

func sqlSystemIndexed(ctx context.Context, db *sql.DB, system systemdefs.System) bool {
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
	list := make([]string, 0)

	q, err := db.PrepareContext(ctx, `
		select SystemID from Systems;
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

func sqlRandomGame(ctx context.Context, db *sql.DB, system systemdefs.System) (database.SearchResult, error) {
	var row database.SearchResult

	// Step 1: Get count, min DBID, and max DBID for this system
	statsQuery := `
		SELECT COUNT(*), COALESCE(MIN(Media.DBID), 0), COALESCE(MAX(Media.DBID), 0)
		FROM Media
		INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
		WHERE Systems.SystemID = ?
	`
	var count int
	var minDBID, maxDBID int64
	err := db.QueryRowContext(ctx, statsQuery, system.ID).Scan(&count, &minDBID, &maxDBID)
	if err != nil {
		return row, fmt.Errorf("failed to get media stats for system: %w", err)
	}

	if count == 0 {
		return row, sql.ErrNoRows
	}

	// Step 2: Generate random DBID within the range
	// This approach is O(log n) instead of O(n) for OFFSET
	randomOffset, err := helpers.RandomInt(int(maxDBID - minDBID + 1))
	if err != nil {
		return row, fmt.Errorf("failed to generate random DBID offset: %w", err)
	}
	targetDBID := minDBID + int64(randomOffset)

	// Step 3: Get the first media item with DBID >= targetDBID
	selectQuery := `
		SELECT Systems.SystemID, Media.Path
		FROM Media
		INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
		WHERE Systems.SystemID = ? AND Media.DBID >= ?
		ORDER BY Media.DBID ASC
		LIMIT 1
	`
	err = db.QueryRowContext(ctx, selectQuery, system.ID, targetDBID).Scan(
		&row.SystemID,
		&row.Path,
	)
	if errors.Is(err, sql.ErrNoRows) {
		// If no row found >= targetDBID (gap in DBID sequence), try wrapping to beginning
		selectQuery = `
			SELECT Systems.SystemID, Media.Path
			FROM Media
			INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
			INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
			WHERE Systems.SystemID = ? AND Media.DBID < ?
			ORDER BY Media.DBID DESC
			LIMIT 1
		`
		err = db.QueryRowContext(ctx, selectQuery, system.ID, targetDBID).Scan(
			&row.SystemID,
			&row.Path,
		)
	}
	if err != nil {
		return row, fmt.Errorf("failed to scan random game row using DBID approach: %w", err)
	}
	row.Name = helpers.FilenameFromPath(row.Path)
	return row, nil
}

// buildMediaQueryWhereClause creates WHERE clause and arguments for a MediaQuery.
// Centralizes the logic to avoid duplication between different query functions.
func buildMediaQueryWhereClause(query database.MediaQuery) (whereClause string, args []any) {
	var whereConditions []string

	// System filtering
	if len(query.Systems) > 0 {
		placeholders := make([]string, len(query.Systems))
		for i, system := range query.Systems {
			placeholders[i] = "?"
			args = append(args, system)
		}
		whereConditions = append(whereConditions,
			fmt.Sprintf("Systems.SystemID IN (%s)", strings.Join(placeholders, ",")))
	}

	// Path prefix filtering (for absolute paths)
	if query.PathPrefix != "" {
		whereConditions = append(whereConditions, "Media.Path LIKE ?")
		args = append(args, query.PathPrefix+"%")
	}

	// PathGlob - match against slugified titles for fuzzy search
	if query.PathGlob != "" {
		// Search terms are slugified to match the database's Slug field.
		// This provides fuzzy matching: spaces/punctuation are ignored,
		// making searches more forgiving (e.g., "mega man" finds "Megaman")
		var parts []string
		for _, part := range strings.Split(query.PathGlob, "*") {
			if part != "" {
				// Slugify search parts to match how titles are stored
				parts = append(parts, helpers.SlugifyString(part))
			}
		}
		for _, part := range parts {
			whereConditions = append(whereConditions, "MediaTitles.Slug LIKE ?")
			args = append(args, "%"+part+"%")
		}
	}

	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	return whereClause, args
}

// sqlRandomGameWithQueryAndStats returns a random game matching the query along with the computed statistics.
func sqlRandomGameWithQueryAndStats(
	ctx context.Context, db *sql.DB, query database.MediaQuery,
) (database.SearchResult, MediaStats, error) {
	var row database.SearchResult
	var stats MediaStats

	// Use shared helper to build WHERE clause and arguments
	whereClause, args := buildMediaQueryWhereClause(query)

	// Step 1: Get count, min DBID, and max DBID for this query
	//nolint:gosec // whereClause is built from safe conditions, no user input
	statsQuery := fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(MIN(Media.DBID), 0), COALESCE(MAX(Media.DBID), 0)
		FROM Media
		INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
		%s
	`, whereClause)

	err := db.QueryRowContext(ctx, statsQuery, args...).Scan(&stats.Count, &stats.MinDBID, &stats.MaxDBID)
	if err != nil {
		return row, stats, fmt.Errorf("failed to get media stats for query: %w", err)
	}

	if stats.Count == 0 {
		return row, stats, sql.ErrNoRows
	}

	// Step 2: Generate random DBID within the range
	randomOffset, err := helpers.RandomInt(int(stats.MaxDBID - stats.MinDBID + 1))
	if err != nil {
		return row, stats, fmt.Errorf("failed to generate random DBID offset: %w", err)
	}
	targetDBID := stats.MinDBID + int64(randomOffset)

	// Step 3: Get the first media item with DBID >= targetDBID
	//nolint:gosec // whereClause is built from safe conditions, no user input
	selectQuery := fmt.Sprintf(`
		SELECT Systems.SystemID, Media.Path
		FROM Media
		INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
		%s AND Media.DBID >= ?
		ORDER BY Media.DBID ASC
		LIMIT 1
	`, whereClause)

	args = append(args, targetDBID)
	err = db.QueryRowContext(ctx, selectQuery, args...).Scan(
		&row.SystemID,
		&row.Path,
	)
	if errors.Is(err, sql.ErrNoRows) {
		// If no row found >= targetDBID (gap in DBID sequence), try wrapping to beginning
		selectQuery = fmt.Sprintf(`
			SELECT Systems.SystemID, Media.Path
			FROM Media
			INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
			INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
			%s AND Media.DBID < ?
			ORDER BY Media.DBID DESC
			LIMIT 1
		`, whereClause)
		args[len(args)-1] = targetDBID
		err = db.QueryRowContext(ctx, selectQuery, args...).Scan(
			&row.SystemID,
			&row.Path,
		)
	}
	if err != nil {
		return row, stats, fmt.Errorf("failed to scan random game row with query: %w", err)
	}
	row.Name = helpers.FilenameFromPath(row.Path)
	return row, stats, nil
}

// sqlGetMaxID returns the maximum ID from the specified table and column
// This function uses hardcoded table/column names that are validated by callers
func sqlGetMaxID(ctx context.Context, db *sql.DB, tableName, columnName string) (int64, error) {
	var query string
	switch tableName {
	case "Systems":
		query = "SELECT COALESCE(MAX(DBID), 0) FROM Systems"
	case "MediaTitles":
		query = "SELECT COALESCE(MAX(DBID), 0) FROM MediaTitles"
	case "Media":
		query = "SELECT COALESCE(MAX(DBID), 0) FROM Media"
	case "TagTypes":
		query = "SELECT COALESCE(MAX(DBID), 0) FROM TagTypes"
	case "Tags":
		query = "SELECT COALESCE(MAX(DBID), 0) FROM Tags"
	case "MediaTags":
		query = "SELECT COALESCE(MAX(DBID), 0) FROM MediaTags"
	default:
		return 0, fmt.Errorf("invalid table name: %s", tableName)
	}

	var maxID int64
	err := db.QueryRowContext(ctx, query).Scan(&maxID)
	if err != nil {
		return 0, fmt.Errorf("failed to get max ID from %s.%s: %w", tableName, columnName, err)
	}
	return maxID, nil
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

func sqlGetAllMedia(ctx context.Context, db *sql.DB) ([]database.Media, error) {
	rows, err := db.QueryContext(ctx, "SELECT DBID, Path, MediaTitleDBID FROM Media ORDER BY DBID")
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
		if err := rows.Scan(&m.DBID, &m.Path, &m.MediaTitleDBID); err != nil {
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

// sqlGetMediaWithFullPath retrieves all media with their associated title and system information using JOIN queries.
func sqlGetMediaWithFullPath(ctx context.Context, db *sql.DB) ([]database.MediaWithFullPath, error) {
	query := `
		SELECT m.DBID, m.Path, m.MediaTitleDBID, t.Slug, s.SystemID
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
		if err := rows.Scan(&m.DBID, &m.Path, &m.MediaTitleDBID, &m.TitleSlug, &m.SystemID); err != nil {
			return nil, fmt.Errorf("failed to scan media with full path: %w", err)
		}
		media = append(media, m)
	}
	return media, rows.Err()
}
