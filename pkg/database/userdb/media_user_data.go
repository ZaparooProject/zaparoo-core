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
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

// GetMediaUserData returns the user-data row for a media path. The bool is false
// when no row exists for the (systemID, path) key, in which case the media has no
// favourite or launcher-override intent recorded.
func (db *UserDB) GetMediaUserData(systemID, path string) (database.MediaUserData, bool, error) {
	return sqlGetMediaUserData(db.ctx, db.sql.Load(), systemID, path)
}

// UpsertMediaUserData inserts or updates the user-data row for (SystemID, Path).
// CreatedAt is set on insert only; UpdatedAt is set on every write. A row with no
// favourite and no launcher override carries no user intent, so it is deleted
// rather than persisted (keeping ListMediaUserData and the backfill guard honest).
func (db *UserDB) UpsertMediaUserData(data *database.MediaUserData) error {
	conn := db.sql.Load()
	if !data.IsFavorite && data.LauncherOverride == "" {
		return sqlDeleteMediaUserData(db.ctx, conn, data.SystemID, data.Path)
	}
	return sqlUpsertMediaUserData(db.ctx, conn, data, time.Now().Unix())
}

// SetMediaUserFavorite records (or clears) the favourite intent for a media
// path without disturbing any launcher override on the same row. The write is a
// column-scoped upsert plus a conditional delete, run in one transaction so two
// concurrent edits to the same path (e.g. a favourite toggle and a launcher
// override) cannot read-modify-write over each other.
func (db *UserDB) SetMediaUserFavorite(systemID, path string, favorite bool) error {
	return sqlSetMediaUserFavorite(db.ctx, db.sql.Load(), systemID, path, favorite, time.Now().Unix())
}

// SetMediaUserLauncherOverride records (or clears, when launcherID is empty) the
// launcher-override intent for a media path without disturbing the favourite
// flag on the same row. See SetMediaUserFavorite for the concurrency guarantee.
func (db *UserDB) SetMediaUserLauncherOverride(systemID, path, launcherID string) error {
	return sqlSetMediaUserLauncherOverride(db.ctx, db.sql.Load(), systemID, path, launcherID, time.Now().Unix())
}

// DeleteMediaUserData removes the user-data row for (SystemID, Path). Deleting a
// row that does not exist is not an error.
func (db *UserDB) DeleteMediaUserData(systemID, path string) error {
	return sqlDeleteMediaUserData(db.ctx, db.sql.Load(), systemID, path)
}

// ListMediaUserData returns every user-data row, used by the reindex re-apply
// step to re-materialize the media.db projection.
func (db *UserDB) ListMediaUserData() ([]database.MediaUserData, error) {
	return sqlListMediaUserData(db.ctx, db.sql.Load())
}

func sqlGetMediaUserData(
	ctx context.Context, db *sql.DB, systemID, path string,
) (database.MediaUserData, bool, error) {
	var row database.MediaUserData
	q, err := db.PrepareContext(ctx, `
		select
		DBID, SystemID, Path, IsFavorite, LauncherOverride, CreatedAt, UpdatedAt
		from MediaUserData
		where SystemID = ? and Path = ?;
	`)
	if err != nil {
		return row, false, fmt.Errorf("failed to prepare media user data select statement: %w", err)
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = q.QueryRowContext(ctx, systemID, path).Scan(
		&row.DBID,
		&row.SystemID,
		&row.Path,
		&row.IsFavorite,
		&row.LauncherOverride,
		&row.CreatedAt,
		&row.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return database.MediaUserData{}, false, nil
	}
	if err != nil {
		return row, false, fmt.Errorf("failed to scan media user data row: %w", err)
	}
	return row, true, nil
}

func sqlUpsertMediaUserData(
	ctx context.Context, db *sql.DB, data *database.MediaUserData, now int64,
) error {
	stmt, err := db.PrepareContext(ctx, `
		insert into MediaUserData(
			SystemID, Path, IsFavorite, LauncherOverride, CreatedAt, UpdatedAt
		) values (?, ?, ?, ?, ?, ?)
		on conflict(SystemID, Path) do update set
			IsFavorite = excluded.IsFavorite,
			LauncherOverride = excluded.LauncherOverride,
			UpdatedAt = excluded.UpdatedAt;
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare media user data upsert statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	_, err = stmt.ExecContext(ctx,
		data.SystemID,
		data.Path,
		data.IsFavorite,
		data.LauncherOverride,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("failed to execute media user data upsert: %w", err)
	}
	return nil
}

func sqlSetMediaUserFavorite(
	ctx context.Context, db *sql.DB, systemID, path string, favorite bool, now int64,
) error {
	return mediaUserDataColumnWrite(ctx, db, `
		insert into MediaUserData(
			SystemID, Path, IsFavorite, LauncherOverride, CreatedAt, UpdatedAt
		) values (?, ?, ?, '', ?, ?)
		on conflict(SystemID, Path) do update set
			IsFavorite = excluded.IsFavorite,
			UpdatedAt = excluded.UpdatedAt;
	`, systemID, path, favorite, now)
}

func sqlSetMediaUserLauncherOverride(
	ctx context.Context, db *sql.DB, systemID, path, launcherID string, now int64,
) error {
	return mediaUserDataColumnWrite(ctx, db, `
		insert into MediaUserData(
			SystemID, Path, IsFavorite, LauncherOverride, CreatedAt, UpdatedAt
		) values (?, ?, 0, ?, ?, ?)
		on conflict(SystemID, Path) do update set
			LauncherOverride = excluded.LauncherOverride,
			UpdatedAt = excluded.UpdatedAt;
	`, systemID, path, launcherID, now)
}

// mediaUserDataColumnWrite applies a single-column upsert and then deletes the
// row if no user intent remains (not a favourite and no launcher override),
// both inside one transaction so the pair is atomic against concurrent writers.
// value is the column-specific bind (favourite bool or launcher ID string).
func mediaUserDataColumnWrite(
	ctx context.Context, db *sql.DB, upsert, systemID, path string, value any, now int64,
) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin media user data transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, upsert, systemID, path, value, now, now); err != nil {
		return fmt.Errorf("failed to upsert media user data column: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
		delete from MediaUserData
		where SystemID = ? and Path = ? and IsFavorite = 0 and LauncherOverride = '';
	`, systemID, path); err != nil {
		return fmt.Errorf("failed to prune empty media user data row: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit media user data transaction: %w", err)
	}
	return nil
}

func sqlDeleteMediaUserData(ctx context.Context, db *sql.DB, systemID, path string) error {
	_, err := db.ExecContext(ctx,
		`delete from MediaUserData where SystemID = ? and Path = ?;`, systemID, path)
	if err != nil {
		return fmt.Errorf("failed to execute media user data delete: %w", err)
	}
	return nil
}

func sqlListMediaUserData(ctx context.Context, db *sql.DB) ([]database.MediaUserData, error) {
	list := make([]database.MediaUserData, 0)

	q, err := db.PrepareContext(ctx, `
		select
		DBID, SystemID, Path, IsFavorite, LauncherOverride, CreatedAt, UpdatedAt
		from MediaUserData;
	`)
	if err != nil {
		return list, fmt.Errorf("failed to prepare list media user data statement: %w", err)
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := q.QueryContext(ctx)
	if err != nil {
		return list, fmt.Errorf("failed to execute list media user data query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	for rows.Next() {
		row := database.MediaUserData{}
		scanErr := rows.Scan(
			&row.DBID,
			&row.SystemID,
			&row.Path,
			&row.IsFavorite,
			&row.LauncherOverride,
			&row.CreatedAt,
			&row.UpdatedAt,
		)
		if scanErr != nil {
			return list, fmt.Errorf("failed to scan media user data row: %w", scanErr)
		}
		list = append(list, row)
	}
	if err = rows.Err(); err != nil {
		return list, fmt.Errorf("failed to iterate over media user data rows: %w", err)
	}
	return list, nil
}
