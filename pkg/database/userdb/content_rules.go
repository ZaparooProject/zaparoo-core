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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// GetContentRule returns the content rule stored for profileID, the empty
// string being the shared profile. Mode is empty when the profile has no
// mode of its own.
func (db *UserDB) GetContentRule(profileID string) (database.ContentRule, error) {
	if db.sql.Load() == nil {
		return database.ContentRule{}, ErrNullSQL
	}
	return sqlGetContentRule(db.ctx, db.sql.Load(), profileID)
}

// ReplaceContentRule stores rule as the whole content rule of profileID,
// dropping whatever was there. An empty Mode clears the profile's own mode.
func (db *UserDB) ReplaceContentRule(profileID string, rule *database.ContentRule) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlReplaceContentRule(db.ctx, db.sql.Load(), profileID, rule)
}

// sqlGetContentRule reads the mode and the entries in one transaction, so
// a rule replaced while it is being read comes back whole: never the old
// mode with the new entries, which could admit what both rules refuse.
func sqlGetContentRule(ctx context.Context, db *sql.DB, profileID string) (database.ContentRule, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return database.ContentRule{}, fmt.Errorf("failed to begin content rule read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rule := database.ContentRule{Entries: []database.ContentRuleEntry{}}
	err = tx.QueryRowContext(ctx,
		`SELECT Mode FROM ProfileContentModes WHERE ProfileID = ?;`, profileID).Scan(&rule.Mode)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return database.ContentRule{}, fmt.Errorf("failed to query content mode: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT Effect, Kind, SystemID, Path, Value
		FROM ProfileContentRules WHERE ProfileID = ? ORDER BY DBID;
	`, profileID)
	if err != nil {
		return database.ContentRule{}, fmt.Errorf("failed to query content rules: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var entry database.ContentRuleEntry
		if err := rows.Scan(&entry.Effect, &entry.Kind, &entry.SystemID, &entry.Path, &entry.Value); err != nil {
			return database.ContentRule{}, fmt.Errorf("failed to scan content rule: %w", err)
		}
		rule.Entries = append(rule.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		return database.ContentRule{}, fmt.Errorf("failed to read content rules: %w", err)
	}
	return rule, nil
}

func sqlReplaceContentRule(
	ctx context.Context, db *sql.DB, profileID string, rule *database.ContentRule,
) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin content rule transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := sqlDeleteContentRule(ctx, tx, profileID); err != nil {
		return err
	}
	if rule.Mode != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ProfileContentModes (ProfileID, Mode) VALUES (?, ?);`,
			profileID, rule.Mode); err != nil {
			return fmt.Errorf("failed to store content mode: %w", err)
		}
	}
	for i := range rule.Entries {
		entry := &rule.Entries[i]
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ProfileContentRules (ProfileID, Effect, Kind, SystemID, Path, Value)
			VALUES (?, ?, ?, ?, ?, ?);
		`, profileID, entry.Effect, entry.Kind, entry.SystemID, entry.Path, entry.Value); err != nil {
			return fmt.Errorf("failed to store content rule: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit content rule transaction: %w", err)
	}
	return nil
}

// sqlDeleteContentRule removes a profile's content rule inside tx, so a
// profile delete takes its rule with it.
func sqlDeleteContentRule(ctx context.Context, tx *sql.Tx, profileID string) error {
	for _, stmt := range []string{
		`DELETE FROM ProfileContentModes WHERE ProfileID = ?;`,
		`DELETE FROM ProfileContentRules WHERE ProfileID = ?;`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, profileID); err != nil {
			return fmt.Errorf("failed to clear content rule: %w", err)
		}
	}
	return nil
}
