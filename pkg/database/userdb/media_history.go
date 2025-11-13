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

package userdb

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

// AddMediaHistory adds a new media history entry and returns the DBID.
func (db *UserDB) AddMediaHistory(entry *database.MediaHistoryEntry) (int64, error) {
	if db.sql == nil {
		return 0, ErrNullSQL
	}
	return sqlAddMediaHistory(db.ctx, db.sql, entry)
}

// UpdateMediaHistoryTime updates only the PlayTime for currently playing media.
func (db *UserDB) UpdateMediaHistoryTime(dbid int64, playTime int) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlUpdateMediaHistoryTime(db.ctx, db.sql, dbid, playTime)
}

// CloseMediaHistory finalizes a media history entry with end time and final play time.
func (db *UserDB) CloseMediaHistory(dbid int64, endTime time.Time, playTime int) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlCloseMediaHistory(db.ctx, db.sql, dbid, endTime, playTime)
}

// GetMediaHistory retrieves media history entries with pagination.
func (db *UserDB) GetMediaHistory(lastID, limit int) ([]database.MediaHistoryEntry, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}
	return sqlGetMediaHistory(db.ctx, db.sql, lastID, limit)
}

// CloseHangingMediaHistory closes any media history entries left open from unclean shutdowns.
// It sets EndTime = StartTime + PlayTime for entries where EndTime is NULL.
func (db *UserDB) CloseHangingMediaHistory() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlCloseHangingMediaHistory(db.ctx, db.sql)
}

// CleanupMediaHistory removes media history older than the retention period.
func (db *UserDB) CleanupMediaHistory(retentionDays int) (int64, error) {
	if db.sql == nil {
		return 0, ErrNullSQL
	}
	return sqlCleanupMediaHistory(db.ctx, db.sql, retentionDays)
}

/*
 * Internal SQL functions
 */

func sqlAddMediaHistory(ctx context.Context, db *sql.DB, entry *database.MediaHistoryEntry) (int64, error) {
	stmt, err := db.PrepareContext(ctx, `
		INSERT INTO MediaHistory(
			StartTime, SystemID, SystemName, MediaPath, MediaName, LauncherID, PlayTime
		) VALUES (?, ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare media history insert statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	result, err := stmt.ExecContext(ctx,
		entry.StartTime.Unix(),
		entry.SystemID,
		entry.SystemName,
		entry.MediaPath,
		entry.MediaName,
		entry.LauncherID,
		entry.PlayTime,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to execute media history insert: %w", err)
	}

	dbid, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	return dbid, nil
}

func sqlUpdateMediaHistoryTime(ctx context.Context, db *sql.DB, dbid int64, playTime int) error {
	stmt, err := db.PrepareContext(ctx, `
		UPDATE MediaHistory
		SET PlayTime = ?
		WHERE DBID = ?;
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare media history time update statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	_, err = stmt.ExecContext(ctx, playTime, dbid)
	if err != nil {
		return fmt.Errorf("failed to execute media history time update: %w", err)
	}

	return nil
}

func sqlCloseMediaHistory(ctx context.Context, db *sql.DB, dbid int64, endTime time.Time, playTime int) error {
	stmt, err := db.PrepareContext(ctx, `
		UPDATE MediaHistory
		SET EndTime = ?, PlayTime = ?
		WHERE DBID = ?;
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare media history close statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	_, err = stmt.ExecContext(ctx, endTime.Unix(), playTime, dbid)
	if err != nil {
		return fmt.Errorf("failed to execute media history close: %w", err)
	}

	return nil
}

func sqlGetMediaHistory(ctx context.Context, db *sql.DB, lastID, limit int) ([]database.MediaHistoryEntry, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}

	list := make([]database.MediaHistoryEntry, 0, limit)

	// Use token-based pagination (similar to history)
	if lastID == 0 {
		lastID = 2147483646 // Max int32 value for "get latest"
	}

	q, err := db.PrepareContext(ctx, `
		SELECT
			DBID, StartTime, EndTime, SystemID, SystemName,
			MediaPath, MediaName, LauncherID, PlayTime
		FROM MediaHistory
		WHERE DBID < ?
		ORDER BY DBID DESC
		LIMIT ?;
	`)
	if err != nil {
		return list, fmt.Errorf("failed to prepare media history query statement: %w", err)
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := q.QueryContext(ctx, lastID, limit)
	if err != nil {
		return list, fmt.Errorf("failed to query media history: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	for rows.Next() {
		var entry database.MediaHistoryEntry
		var startTimeUnix int64
		var endTimeUnix sql.NullInt64

		err = rows.Scan(
			&entry.DBID,
			&startTimeUnix,
			&endTimeUnix,
			&entry.SystemID,
			&entry.SystemName,
			&entry.MediaPath,
			&entry.MediaName,
			&entry.LauncherID,
			&entry.PlayTime,
		)
		if err != nil {
			return list, fmt.Errorf("failed to scan media history row: %w", err)
		}

		entry.StartTime = time.Unix(startTimeUnix, 0)
		if endTimeUnix.Valid {
			endTime := time.Unix(endTimeUnix.Int64, 0)
			entry.EndTime = &endTime
		}

		list = append(list, entry)
	}

	if err = rows.Err(); err != nil {
		return list, fmt.Errorf("error iterating media history rows: %w", err)
	}

	return list, nil
}

func sqlCloseHangingMediaHistory(ctx context.Context, db *sql.DB) error {
	// For entries where EndTime is NULL, calculate EndTime as StartTime + PlayTime seconds
	stmt, err := db.PrepareContext(ctx, `
		UPDATE MediaHistory
		SET EndTime = StartTime + PlayTime
		WHERE EndTime IS NULL;
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare close hanging media statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	result, err := stmt.ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to close hanging media entries: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Info().Msgf("closed %d hanging media history entries", rows)
	}

	return nil
}

func sqlCleanupMediaHistory(ctx context.Context, db *sql.DB, retentionDays int) (int64, error) {
	cutoffTime := time.Now().AddDate(0, 0, -retentionDays).Unix()

	stmt, err := db.PrepareContext(ctx, `DELETE FROM MediaHistory WHERE StartTime < ?;`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare media history cleanup statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	result, err := stmt.ExecContext(ctx, cutoffTime)
	if err != nil {
		return 0, fmt.Errorf("failed to execute media history cleanup: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	// Vacuum to reclaim disk space after cleanup
	if rowsAffected > 0 {
		if err := sqlVacuum(ctx, db); err != nil {
			return rowsAffected, fmt.Errorf("cleanup succeeded but vacuum failed: %w", err)
		}
	}

	return rowsAffected, nil
}
