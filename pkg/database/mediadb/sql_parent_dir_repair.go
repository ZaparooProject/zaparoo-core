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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

const temporaryParentDirRepairBatchSize = 500

type temporaryParentDirRepairStats struct {
	Scanned int64
	Updated int64
}

type parentDirRepairRow struct {
	Path      string
	ParentDir string
	DBID      int64
}

// ParentDirForMediaPath returns the immediate browse parent for an indexed media path.
func ParentDirForMediaPath(path string) string {
	if idx := strings.Index(path, "://"); idx >= 0 {
		return path[:idx+3]
	}
	if lastSlash := strings.LastIndex(path, "/"); lastSlash >= 0 {
		return path[:lastSlash+1]
	}
	return ""
}

func sqlUpdateMediaParentDir(ctx context.Context, db sqlQueryable, mediaDBID int64, parentDir string) error {
	if _, err := db.ExecContext(
		ctx,
		`UPDATE Media SET ParentDir = ? WHERE DBID = ?`,
		parentDir,
		mediaDBID,
	); err != nil {
		return fmt.Errorf("failed to update media parent dir: %w", err)
	}

	return nil
}

func sqlTemporaryParentDirRepairVersionCurrent(ctx context.Context, db sqlQueryable) (bool, error) {
	var version string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigTemporaryRepairParentDirVersion,
	).Scan(&version)
	if err == nil {
		return version == temporaryRepairParentDirVersion, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("failed to read temporary parent dir repair version: %w", err)
	}
	return false, nil
}

func sqlEmptyMediaParentDirsExist(ctx context.Context, db sqlQueryable) (bool, error) {
	var exists int
	err := db.QueryRowContext(ctx, `
		SELECT 1
		FROM Media
		WHERE ParentDir = ''
		LIMIT 1
	`).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check empty media parent dirs: %w", err)
	}
	return exists == 1, nil
}

func sqlMarkTemporaryParentDirRepairComplete(ctx context.Context, db sqlQueryable) error {
	if _, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigTemporaryRepairParentDirVersion,
		temporaryRepairParentDirVersion,
	); err != nil {
		return fmt.Errorf("failed to mark temporary parent dir repair complete: %w", err)
	}

	return nil
}

func (db *MediaDB) runTemporaryParentDirRepair(ctx context.Context, pauser *syncutil.Pauser) error {
	current, err := sqlTemporaryParentDirRepairVersionCurrent(ctx, db.sql)
	if err != nil {
		return err
	}
	if current {
		log.Debug().Msg("temporary repair job skipped: media parent directories already repaired")
		return nil
	}

	pending, err := sqlEmptyMediaParentDirsExist(ctx, db.sql)
	if err != nil {
		return err
	}
	if !pending {
		markErr := sqlMarkTemporaryParentDirRepairComplete(ctx, db.sql)
		if markErr != nil {
			return markErr
		}
		log.Debug().Msg("temporary repair job skipped: media parent directories already repaired")
		return nil
	}

	started := time.Now()
	log.Info().Msg("temporary repair job started: media parent directories")

	stats, err := db.repairMediaParentDirs(ctx, pauser)
	if err != nil {
		return err
	}

	if err := sqlMarkTemporaryParentDirRepairComplete(ctx, db.sql); err != nil {
		return err
	}

	if stats.Updated > 0 {
		if err := sqlInvalidateBrowseCache(ctx, db.sql); err != nil {
			return fmt.Errorf("failed to invalidate browse cache after temporary parent dir repair: %w", err)
		}
	}

	log.Info().
		Int64("scanned", stats.Scanned).
		Int64("updated", stats.Updated).
		Dur("elapsed", time.Since(started)).
		Msg("temporary repair job completed: media parent directories")

	return nil
}

func (db *MediaDB) repairMediaParentDirs(
	ctx context.Context,
	pauser *syncutil.Pauser,
) (temporaryParentDirRepairStats, error) {
	stats := temporaryParentDirRepairStats{}
	lastDBID := int64(0)

	for {
		if err := pauser.Wait(ctx); err != nil {
			return stats, fmt.Errorf("temporary parent dir repair paused: %w", err)
		}

		rows, err := db.loadParentDirRepairRows(ctx, lastDBID, temporaryParentDirRepairBatchSize)
		if err != nil {
			return stats, err
		}
		if len(rows) == 0 {
			return stats, nil
		}

		updates := make([]parentDirRepairRow, 0, len(rows))
		for _, row := range rows {
			stats.Scanned++
			lastDBID = row.DBID
			row.ParentDir = ParentDirForMediaPath(row.Path)
			updates = append(updates, row)
		}

		if len(updates) == 0 {
			continue
		}

		updated, err := db.updateParentDirRepairRows(ctx, updates)
		if err != nil {
			return stats, err
		}
		stats.Updated += updated

		log.Debug().
			Int64("scanned", stats.Scanned).
			Int64("updated", stats.Updated).
			Msg("temporary repair job progress: media parent directories")
	}
}

func (db *MediaDB) loadParentDirRepairRows(
	ctx context.Context, lastDBID int64, limit int,
) ([]parentDirRepairRow, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT DBID, Path, ParentDir
		FROM Media
		WHERE DBID > ? AND ParentDir = ''
		ORDER BY DBID
		LIMIT ?
	`, lastDBID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query media parent dirs for temporary repair: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close parent dir repair rows")
		}
	}()

	results := make([]parentDirRepairRow, 0, limit)
	for rows.Next() {
		var row parentDirRepairRow
		if err := rows.Scan(&row.DBID, &row.Path, &row.ParentDir); err != nil {
			return nil, fmt.Errorf("failed to scan media parent dir repair row: %w", err)
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate media parent dir repair rows: %w", err)
	}

	return results, nil
}

func (db *MediaDB) updateParentDirRepairRows(ctx context.Context, rows []parentDirRepairRow) (int64, error) {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin temporary parent dir repair transaction: %w", err)
	}

	updated := int64(0)
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				log.Warn().Err(rollbackErr).Msg("failed to roll back temporary parent dir repair transaction")
			}
		}
	}()

	stmt, err := tx.PrepareContext(ctx, `UPDATE Media SET ParentDir = ? WHERE DBID = ? AND ParentDir = ''`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare temporary parent dir repair update: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close temporary parent dir repair statement")
		}
	}()

	for _, row := range rows {
		res, err := stmt.ExecContext(ctx, row.ParentDir, row.DBID)
		if err != nil {
			return 0, fmt.Errorf("failed to update media parent dir %d: %w", row.DBID, err)
		}
		rowsAffected, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("failed to read media parent dir update count: %w", err)
		}
		updated += rowsAffected
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit temporary parent dir repair transaction: %w", err)
	}
	committed = true

	return updated, nil
}
