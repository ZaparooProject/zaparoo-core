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
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

var _ database.PlaylistMemoryStore = (*UserDB)(nil)

// GetPlaylistMemory returns where a playlist was left for a profile, if it
// has been played before.
func (db *UserDB) GetPlaylistMemory(profileID, playlistKey string) (database.PlaylistMemory, bool, error) {
	conn := db.sql.Load()
	if conn == nil {
		return database.PlaylistMemory{}, false, ErrNullSQL
	}
	mem := database.PlaylistMemory{ProfileID: profileID, PlaylistKey: playlistKey}
	var orderJSON string
	err := conn.QueryRowContext(db.ctx, `select ItemZapScript, ItemIndex, ItemsHash, OrderJSON, UpdatedAt
		from PlaylistMemory where ProfileID = ? and PlaylistKey = ?`, profileID, playlistKey,
	).Scan(&mem.ItemZapScript, &mem.ItemIndex, &mem.ItemsHash, &orderJSON, &mem.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return database.PlaylistMemory{}, false, nil
	}
	if err != nil {
		return database.PlaylistMemory{}, false, fmt.Errorf("get playlist memory: %w", err)
	}
	if orderJSON != "" {
		if err := json.Unmarshal([]byte(orderJSON), &mem.Order); err != nil {
			return database.PlaylistMemory{}, false, fmt.Errorf("decode playlist memory order: %w", err)
		}
	}
	return mem, true, nil
}

// UpsertPlaylistMemory records where a playlist is, then drops the profile's
// least recently used playlists beyond keep.
func (db *UserDB) UpsertPlaylistMemory(mem *database.PlaylistMemory, keep int) error {
	orderJSON := ""
	if len(mem.Order) > 0 {
		encoded, err := json.Marshal(mem.Order)
		if err != nil {
			return fmt.Errorf("encode playlist memory order: %w", err)
		}
		orderJSON = string(encoded)
	}
	return db.playlistMemoryTx(func(ctx context.Context, tx *sql.Tx) error {
		var dbid int64
		if err := tx.QueryRowContext(ctx, `insert into PlaylistMemory
			(ProfileID, PlaylistKey, ItemZapScript, ItemIndex, ItemsHash, OrderJSON, UpdatedAt)
			values (?, ?, ?, ?, ?, ?, ?)
			on conflict (ProfileID, PlaylistKey) do update set
				ItemZapScript = excluded.ItemZapScript, ItemIndex = excluded.ItemIndex,
				ItemsHash = excluded.ItemsHash, OrderJSON = excluded.OrderJSON,
				UpdatedAt = excluded.UpdatedAt
			returning DBID`,
			mem.ProfileID, mem.PlaylistKey, mem.ItemZapScript, mem.ItemIndex, mem.ItemsHash, orderJSON,
			time.Now().UnixMilli(),
		).Scan(&dbid); err != nil {
			return fmt.Errorf("upsert playlist memory: %w", err)
		}
		return pruneByProfile(ctx, tx, "PlaylistMemory", mem.ProfileID, dbid, keep)
	})
}

// DeletePlaylistMemory forgets where a playlist was left for a profile.
func (db *UserDB) DeletePlaylistMemory(profileID, playlistKey string) error {
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	if _, err := conn.ExecContext(db.ctx, `delete from PlaylistMemory where ProfileID = ? and PlaylistKey = ?`,
		profileID, playlistKey); err != nil {
		return fmt.Errorf("delete playlist memory: %w", err)
	}
	return nil
}

// ListOpenPlaylists returns the playlist recorded as open in each slot.
func (db *UserDB) ListOpenPlaylists() ([]database.OpenPlaylist, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	rows, err := conn.QueryContext(db.ctx, `select Slot, ProfileID, Snapshot, UpdatedAt from OpenPlaylists`)
	if err != nil {
		return nil, fmt.Errorf("query open playlists: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]database.OpenPlaylist, 0)
	for rows.Next() {
		var row database.OpenPlaylist
		if err := rows.Scan(&row.Slot, &row.ProfileID, &row.Snapshot, &row.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan open playlist: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open playlists: %w", err)
	}
	return out, nil
}

// UpsertOpenPlaylist records the playlist open in a slot.
func (db *UserDB) UpsertOpenPlaylist(row *database.OpenPlaylist) error {
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	if _, err := conn.ExecContext(db.ctx, `insert or replace into OpenPlaylists
		(Slot, ProfileID, Snapshot, UpdatedAt) values (?, ?, ?, ?)`,
		row.Slot, row.ProfileID, row.Snapshot, time.Now().UnixMilli(),
	); err != nil {
		return fmt.Errorf("upsert open playlist: %w", err)
	}
	return nil
}

// DeleteOpenPlaylist records that no playlist is open in a slot.
func (db *UserDB) DeleteOpenPlaylist(slot string) error {
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	if _, err := conn.ExecContext(db.ctx, `delete from OpenPlaylists where Slot = ?`, slot); err != nil {
		return fmt.Errorf("delete open playlist: %w", err)
	}
	return nil
}

// GetMediaPosition returns how far into a piece of media a profile got.
func (db *UserDB) GetMediaPosition(profileID, systemID, path string) (database.MediaPosition, bool, error) {
	conn := db.sql.Load()
	if conn == nil {
		return database.MediaPosition{}, false, ErrNullSQL
	}
	pos := database.MediaPosition{ProfileID: profileID, SystemID: systemID, Path: path}
	err := conn.QueryRowContext(db.ctx, `select PositionMs, DurationMs, UpdatedAt from MediaPositions
		where ProfileID = ? and SystemID = ? and Path = ?`, profileID, systemID, path,
	).Scan(&pos.PositionMs, &pos.DurationMs, &pos.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return database.MediaPosition{}, false, nil
	}
	if err != nil {
		return database.MediaPosition{}, false, fmt.Errorf("get media position: %w", err)
	}
	return pos, true, nil
}

// UpsertMediaPosition records how far into a piece of media a profile got,
// then drops the profile's least recently used positions beyond keep.
func (db *UserDB) UpsertMediaPosition(pos *database.MediaPosition, keep int) error {
	return db.playlistMemoryTx(func(ctx context.Context, tx *sql.Tx) error {
		var dbid int64
		if err := tx.QueryRowContext(ctx, `insert into MediaPositions
			(ProfileID, SystemID, Path, PositionMs, DurationMs, UpdatedAt)
			values (?, ?, ?, ?, ?, ?)
			on conflict (ProfileID, SystemID, Path) do update set
				PositionMs = excluded.PositionMs, DurationMs = excluded.DurationMs,
				UpdatedAt = excluded.UpdatedAt
			returning DBID`,
			pos.ProfileID, pos.SystemID, pos.Path, pos.PositionMs, pos.DurationMs, time.Now().UnixMilli(),
		).Scan(&dbid); err != nil {
			return fmt.Errorf("upsert media position: %w", err)
		}
		return pruneByProfile(ctx, tx, "MediaPositions", pos.ProfileID, dbid, keep)
	})
}

// DeleteMediaPosition forgets how far into a piece of media a profile got.
func (db *UserDB) DeleteMediaPosition(profileID, systemID, path string) error {
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	if _, err := conn.ExecContext(db.ctx, `delete from MediaPositions
		where ProfileID = ? and SystemID = ? and Path = ?`, profileID, systemID, path); err != nil {
		return fmt.Errorf("delete media position: %w", err)
	}
	return nil
}

// pruneByProfile keeps the row just written, current, and the profile's
// keep-1 most recently updated others in table. The row just written is kept
// by its DBID because rows written in the same millisecond tie on UpdatedAt.
// table is always one of this file's tables, never caller input.
func pruneByProfile(ctx context.Context, tx *sql.Tx, table, profileID string, current int64, keep int) error {
	if keep <= 0 {
		return nil
	}
	//nolint:gosec // table is a fixed name chosen by this package
	if _, err := tx.ExecContext(ctx, `delete from `+table+` where ProfileID = ? and DBID <> ? and DBID not in (
		select DBID from `+table+` where ProfileID = ? and DBID <> ? order by UpdatedAt desc, DBID desc limit ?)`,
		profileID, current, profileID, current, keep-1,
	); err != nil {
		return fmt.Errorf("prune %s: %w", table, err)
	}
	return nil
}

func (db *UserDB) playlistMemoryTx(fn func(ctx context.Context, tx *sql.Tx) error) error {
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	tx, err := conn.BeginTx(db.ctx, nil)
	if err != nil {
		return fmt.Errorf("begin playlist memory transaction: %w", err)
	}
	if err := fn(db.ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit playlist memory transaction: %w", err)
	}
	return nil
}
