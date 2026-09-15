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
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

const libraryStateSyncColumns = `IdentityKey, MediaType, SystemID, CoreSlug, VariantTags, Title,
	Favorite, Intent, Reaction, PreferredTags, SentPreferredTags, Revision, Deleted, Unmatched, RejectedCode,
	RejectedHash, UpdatedAt`

// ListLibraryStateSync returns every personal state base row.
func (db *UserDB) ListLibraryStateSync() ([]database.LibraryStateSyncRow, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	rows, err := conn.QueryContext(db.ctx, `select `+libraryStateSyncColumns+` from LibraryStateSync`)
	if err != nil {
		return nil, fmt.Errorf("query library state sync rows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]database.LibraryStateSyncRow, 0)
	for rows.Next() {
		var (
			row       database.LibraryStateSyncRow
			variants  string
			preferred string
			sent      string
		)
		if scanErr := rows.Scan(
			&row.IdentityKey, &row.MediaType, &row.SystemID, &row.CoreSlug, &variants, &row.Title,
			&row.Favorite, &row.Intent, &row.Reaction, &preferred, &sent, &row.Revision, &row.Deleted,
			&row.Unmatched, &row.RejectedCode, &row.RejectedHash, &row.UpdatedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("scan library state sync row: %w", scanErr)
		}
		row.VariantTags = decodeTagList(variants)
		row.PreferredTags = decodeTagList(preferred)
		row.SentPreferredTags = decodeTagList(sent)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate library state sync rows: %w", err)
	}
	return out, nil
}

// UpsertLibraryStateSync stores personal state base rows, replacing rows
// with the same identity.
func (db *UserDB) UpsertLibraryStateSync(rows []database.LibraryStateSyncRow) error {
	if len(rows) == 0 {
		return nil
	}
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	tx, err := conn.BeginTx(db.ctx, nil)
	if err != nil {
		return fmt.Errorf("begin library state sync transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(db.ctx, `insert or replace into LibraryStateSync (`+libraryStateSyncColumns+`)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare library state sync upsert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	now := time.Now().Unix()
	for i := range rows {
		row := &rows[i]
		if _, execErr := stmt.ExecContext(db.ctx,
			row.IdentityKey, row.MediaType, row.SystemID, row.CoreSlug, encodeTagList(row.VariantTags),
			row.Title, row.Favorite, row.Intent, row.Reaction, encodeTagList(row.PreferredTags),
			encodeTagList(row.SentPreferredTags),
			row.Revision, row.Deleted, row.Unmatched, row.RejectedCode, row.RejectedHash, now,
		); execErr != nil {
			return fmt.Errorf("upsert library state sync row: %w", execErr)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit library state sync rows: %w", err)
	}
	return nil
}

// DeleteLibraryStateSync removes the base rows for the given identities.
func (db *UserDB) DeleteLibraryStateSync(identityKeys []string) error {
	if len(identityKeys) == 0 {
		return nil
	}
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	tx, err := conn.BeginTx(db.ctx, nil)
	if err != nil {
		return fmt.Errorf("begin library state sync delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for chunk := range slices.Chunk(identityKeys, 200) {
		args := make([]any, len(chunk))
		for i := range chunk {
			args[i] = chunk[i]
		}
		marks := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		//nolint:gosec // Safe: only placeholders are interpolated.
		if _, execErr := tx.ExecContext(db.ctx,
			`delete from LibraryStateSync where IdentityKey in (`+marks+`)`, args...,
		); execErr != nil {
			return fmt.Errorf("delete library state sync rows: %w", execErr)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit library state sync delete: %w", err)
	}
	return nil
}

// ClearLibraryStateSync removes every personal state base row.
func (db *UserDB) ClearLibraryStateSync() error {
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	if _, err := conn.ExecContext(db.ctx, `delete from LibraryStateSync`); err != nil {
		return fmt.Errorf("clear library state sync rows: %w", err)
	}
	return nil
}

func encodeTagList(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(tags)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func decodeTagList(raw string) []string {
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil || tags == nil {
		return []string{}
	}
	return tags
}
