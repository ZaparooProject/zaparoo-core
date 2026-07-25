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

package userdb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Play-history sync support: MediaHistory rows are uploaded to the Zaparoo
// API keyed by their session UUID, cursored on (UpdatedAt, DBID) so healed
// timestamps, play-time updates, close-outs, and tombstones re-sync
// naturally. SyncedAt records acknowledgement of an exact local version;
// the server-side timestamp watermark is the cross-install resume cursor.

// One synced-at argument plus two arguments per exact-version reference must
// stay within SQLite's common 999-variable limit.
const mediaHistorySyncMarkChunkSize = 499

// BackfillMediaHistoryUUIDs assigns stable session UUIDs to legacy
// MediaHistory rows written before UUIDs existed. Existing timestamps remain
// unchanged.
func (db *UserDB) BackfillMediaHistoryUUIDs() (int64, error) {
	return sqlBackfillMediaHistoryUUIDs(db.ctx, db.sql.Load())
}

// ResetMediaHistorySyncAfter clears local acknowledgements newer than the
// server watermark so restored or reset server state is uploaded again. A nil
// watermark means the server has no sessions and resets every local row.
func (db *UserDB) ResetMediaHistorySyncAfter(watermark *time.Time) error {
	return sqlResetMediaHistorySyncAfter(db.ctx, db.sql.Load(), watermark)
}

// GetMediaHistorySyncBatch returns unsynced rows after the local
// (after, afterDBID) cursor in (UpdatedAt, DBID) order. Mutations clear
// SyncedAt, so acknowledged rows do not repeat and unreliable-clock rows
// remain eligible regardless of the server's timestamp watermark. Rows
// without a session UUID are excluded; startup backfills legacy rows first.
func (db *UserDB) GetMediaHistorySyncBatch(
	after time.Time, afterDBID int64, limit int,
) ([]database.MediaHistoryEntry, error) {
	return sqlGetMediaHistorySyncBatch(db.ctx, db.sql.Load(), after, afterDBID, limit)
}

// MarkMediaHistorySynced stamps SyncedAt only when each row still matches the
// uploaded version. Concurrent mutations advance UpdatedAt and remain unsynced.
func (db *UserDB) MarkMediaHistorySynced(refs []database.MediaHistorySyncRef, syncedAt time.Time) error {
	return sqlMarkMediaHistorySynced(db.ctx, db.sql.Load(), refs, syncedAt)
}

func sqlBackfillMediaHistoryUUIDs(ctx context.Context, db *sql.DB) (backfilled int64, err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin media history UUID backfill transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	dbids, err := func() (ids []int64, resultErr error) {
		rows, queryErr := tx.QueryContext(ctx, `
			SELECT DBID FROM MediaHistory WHERE ID IS NULL OR ID = '';
		`)
		if queryErr != nil {
			return nil, fmt.Errorf("failed to query media history rows missing UUIDs: %w", queryErr)
		}
		defer func() {
			if closeErr := rows.Close(); closeErr != nil && resultErr == nil {
				resultErr = fmt.Errorf("failed to close media history UUID backfill rows: %w", closeErr)
			}
		}()

		for rows.Next() {
			var dbid int64
			if scanErr := rows.Scan(&dbid); scanErr != nil {
				return nil, fmt.Errorf("failed to scan media history DBID: %w", scanErr)
			}
			ids = append(ids, dbid)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return nil, fmt.Errorf("error iterating media history rows missing UUIDs: %w", rowsErr)
		}
		return ids, nil
	}()
	if err != nil {
		return 0, err
	}

	if len(dbids) > 0 {
		backfilled, err = func() (count int64, resultErr error) {
			stmt, prepareErr := tx.PrepareContext(ctx, `
				UPDATE MediaHistory SET ID = ? WHERE DBID = ? AND (ID IS NULL OR ID = '');
			`)
			if prepareErr != nil {
				return 0, fmt.Errorf("failed to prepare media history UUID backfill statement: %w", prepareErr)
			}
			defer func() {
				if closeErr := stmt.Close(); closeErr != nil && resultErr == nil {
					resultErr = fmt.Errorf("failed to close media history UUID backfill statement: %w", closeErr)
				}
			}()

			for _, dbid := range dbids {
				result, execErr := stmt.ExecContext(ctx, uuid.New().String(), dbid)
				if execErr != nil {
					return 0, fmt.Errorf("failed to backfill media history UUID: %w", execErr)
				}
				affected, rowsErr := result.RowsAffected()
				if rowsErr != nil {
					return 0, fmt.Errorf("failed to count backfilled media history UUIDs: %w", rowsErr)
				}
				count += affected
			}
			return count, nil
		}()
		if err != nil {
			return 0, err
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return 0, fmt.Errorf("failed to commit media history UUID backfill: %w", commitErr)
	}
	return backfilled, nil
}

func sqlResetMediaHistorySyncAfter(ctx context.Context, db *sql.DB, watermark *time.Time) error {
	query := `UPDATE MediaHistory SET SyncedAt = NULL;`
	var args []any
	if watermark != nil {
		query = `UPDATE MediaHistory SET SyncedAt = NULL WHERE UpdatedAt > ?;`
		args = append(args, watermark.Unix())
	}
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("failed to reset media history sync state: %w", err)
	}
	return nil
}

func sqlGetMediaHistorySyncBatch(
	ctx context.Context, db *sql.DB, after time.Time, afterDBID int64, limit int,
) ([]database.MediaHistoryEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	stmt, err := db.PrepareContext(ctx, `
		SELECT
			DBID, ID, StartTime, EndTime, SystemID, SystemName,
			MediaPath, MediaName, LauncherID, PlayTime,
			BootUUID, MonotonicStart, DurationSec, WallDuration, TimeSkewFlag,
			ClockReliable, ClockSource, CreatedAt, UpdatedAt, DeviceID, ProfileID,
			COALESCE(IsDeleted, 0), SyncedAt, Tags
		FROM MediaHistory
		WHERE SyncedAt IS NULL
		  AND (UpdatedAt > ? OR (UpdatedAt = ? AND DBID > ?))
		  AND ID IS NOT NULL AND ID != ''
		ORDER BY UpdatedAt ASC, DBID ASC
		LIMIT ?;
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare media history sync batch statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	afterUnix := after.Unix()
	rows, err := stmt.QueryContext(ctx, afterUnix, afterUnix, afterDBID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query media history sync batch: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	list := make([]database.MediaHistoryEntry, 0, limit)
	for rows.Next() {
		var entry database.MediaHistoryEntry
		var startTimeUnix int64
		var endTimeUnix sql.NullInt64
		var createdAtUnix, updatedAtUnix int64
		var id, clockSource sql.NullString
		var deviceID, rowProfileID sql.NullString
		var isDeleted int64
		var syncedAtUnix sql.NullInt64
		var rawTags string

		err = rows.Scan(
			&entry.DBID,
			&id,
			&startTimeUnix,
			&endTimeUnix,
			&entry.SystemID,
			&entry.SystemName,
			&entry.MediaPath,
			&entry.MediaName,
			&entry.LauncherID,
			&entry.PlayTime,
			&entry.BootUUID,
			&entry.MonotonicStart,
			&entry.DurationSec,
			&entry.WallDuration,
			&entry.TimeSkewFlag,
			&entry.ClockReliable,
			&clockSource,
			&createdAtUnix,
			&updatedAtUnix,
			&deviceID,
			&rowProfileID,
			&isDeleted,
			&syncedAtUnix,
			&rawTags,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan media history sync row: %w", err)
		}

		if id.Valid {
			entry.ID = id.String
		}
		if clockSource.Valid {
			entry.ClockSource = clockSource.String
		}
		if deviceID.Valid {
			deviceStr := deviceID.String
			entry.DeviceID = &deviceStr
		}
		if rowProfileID.Valid {
			profileStr := rowProfileID.String
			entry.ProfileID = &profileStr
		}
		entry.StartTime = time.Unix(startTimeUnix, 0)
		if endTimeUnix.Valid {
			endTime := time.Unix(endTimeUnix.Int64, 0)
			entry.EndTime = &endTime
		}
		entry.CreatedAt = time.Unix(createdAtUnix, 0)
		entry.UpdatedAt = time.Unix(updatedAtUnix, 0)
		entry.IsDeleted = isDeleted != 0
		if syncedAtUnix.Valid {
			syncedAt := time.Unix(syncedAtUnix.Int64, 0)
			entry.SyncedAt = &syncedAt
		}
		entry.Tags = database.DecodeTagStrings(rawTags)

		list = append(list, entry)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating media history sync rows: %w", err)
	}
	return list, nil
}

func sqlMarkMediaHistorySynced(
	ctx context.Context, db *sql.DB, refs []database.MediaHistorySyncRef, syncedAt time.Time,
) error {
	for start := 0; start < len(refs); start += mediaHistorySyncMarkChunkSize {
		end := min(start+mediaHistorySyncMarkChunkSize, len(refs))
		chunk := refs[start:end]
		placeholders := make([]string, len(chunk))
		args := make([]any, 0, 1+(len(chunk)*2))
		args = append(args, syncedAt.Unix())
		for i := range chunk {
			placeholders[i] = "(?, ?)"
			args = append(args, chunk[i].DBID, chunk[i].UpdatedAt.Unix())
		}
		//nolint:gosec // placeholders are hardcoded "?" markers, not user input
		query := fmt.Sprintf(
			`UPDATE MediaHistory SET SyncedAt = ? WHERE (DBID, UpdatedAt) IN (%s);`,
			strings.Join(placeholders, ", "),
		)
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("failed to mark media history synced: %w", err)
		}
	}
	return nil
}
