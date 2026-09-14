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
)

const deckSyncColumns = `DeckID, Snapshot, Revision, Locked, Conflicts, RejectedCode, RejectedHash, UpdatedAt`

func scanDeckSync(scan func(dest ...any) error) (database.DeckSyncRow, error) {
	var row database.DeckSyncRow
	err := scan(&row.DeckID, &row.Snapshot, &row.Revision, &row.Locked, &row.Conflicts,
		&row.RejectedCode, &row.RejectedHash, &row.UpdatedAt)
	return row, err //nolint:wrapcheck // callers wrap with context
}

// ListDeckSync returns every deck sync row.
func (db *UserDB) ListDeckSync() ([]database.DeckSyncRow, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	rows, err := conn.QueryContext(db.ctx, `select `+deckSyncColumns+` from DeckSync`)
	if err != nil {
		return nil, fmt.Errorf("query deck sync rows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]database.DeckSyncRow, 0)
	for rows.Next() {
		row, scanErr := scanDeckSync(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("scan deck sync row: %w", scanErr)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deck sync rows: %w", err)
	}
	return out, nil
}

// GetDeckSync returns the sync row for one deck, if any.
func (db *UserDB) GetDeckSync(deckID string) (database.DeckSyncRow, bool, error) {
	conn := db.sql.Load()
	if conn == nil {
		return database.DeckSyncRow{}, false, ErrNullSQL
	}
	row, err := scanDeckSync(conn.QueryRowContext(db.ctx,
		`select `+deckSyncColumns+` from DeckSync where DeckID = ?`, deckID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return database.DeckSyncRow{}, false, nil
	}
	if err != nil {
		return database.DeckSyncRow{}, false, fmt.Errorf("get deck sync row: %w", err)
	}
	return row, true, nil
}

// UpsertDeckSync stores deck sync rows, replacing rows for the same deck.
func (db *UserDB) UpsertDeckSync(rows []database.DeckSyncRow) error {
	if len(rows) == 0 {
		return nil
	}
	return db.deckSyncTx(func(ctx context.Context, tx *sql.Tx) error {
		now := time.Now().Unix()
		for i := range rows {
			row := &rows[i]
			if _, err := tx.ExecContext(ctx, `insert or replace into DeckSync (`+deckSyncColumns+`)
				values (?, ?, ?, ?, ?, ?, ?, ?)`,
				row.DeckID, row.Snapshot, row.Revision, row.Locked, row.Conflicts,
				row.RejectedCode, row.RejectedHash, now,
			); err != nil {
				return fmt.Errorf("upsert deck sync row: %w", err)
			}
		}
		return nil
	})
}

// DeleteDeckSync removes the sync rows for the given decks.
func (db *UserDB) DeleteDeckSync(deckIDs []string) error {
	if len(deckIDs) == 0 {
		return nil
	}
	return db.deckSyncTx(func(ctx context.Context, tx *sql.Tx) error {
		for _, deckID := range deckIDs {
			if _, err := tx.ExecContext(ctx, `delete from DeckSync where DeckID = ?`, deckID); err != nil {
				return fmt.Errorf("delete deck sync row: %w", err)
			}
		}
		return nil
	})
}

// ClearDeckSync removes every deck sync row.
func (db *UserDB) ClearDeckSync() error {
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	if _, err := conn.ExecContext(db.ctx, `delete from DeckSync`); err != nil {
		return fmt.Errorf("clear deck sync rows: %w", err)
	}
	return nil
}

func (db *UserDB) deckSyncTx(fn func(ctx context.Context, tx *sql.Tx) error) error {
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	tx, err := conn.BeginTx(db.ctx, nil)
	if err != nil {
		return fmt.Errorf("begin deck sync transaction: %w", err)
	}
	if err := fn(db.ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit deck sync transaction: %w", err)
	}
	return nil
}
