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
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// RewriteImagePropertyPrefix rewrites every media and title image property
// whose text starts with oldPrefix to start with newPrefix instead, turning
// sep in the rest of the text into "/". It moves stored image paths to an
// images:// path when their files move into the image folder.
//
// apply runs inside the transaction once the rows are rewritten, and an error
// from it rolls the rewrite back, so a file move that fails leaves the
// database as it was. When apply succeeded but the commit fails, the error is
// returned and the caller undoes whatever apply did.
func (db *MediaDB) RewriteImagePropertyPrefix(
	ctx context.Context, oldPrefix, newPrefix, sep string, apply func() error,
) (int64, error) {
	if oldPrefix == "" {
		return 0, errors.New("rewrite image properties: empty prefix")
	}
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return 0, ErrNullSQL
	}
	if db.inTransaction {
		return 0, ErrTransactionActive
	}

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin image property rewrite: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// SQLite's substr counts characters in text, not bytes.
	prefixChars := utf8.RuneCountInString(oldPrefix)
	var total int64
	for _, table := range []string{"MediaProperties", "MediaTitleProperties"} {
		//nolint:gosec // table names come from the fixed list above
		result, execErr := tx.ExecContext(ctx, `
			UPDATE `+table+`
			SET Text = ? || replace(substr(Text, ?), ?, '/')
			WHERE substr(Text, 1, ?) = ?
				AND TypeTagDBID IN (
					SELECT t.DBID FROM Tags t
					JOIN TagTypes tt ON tt.DBID = t.TypeDBID
					WHERE tt.Type = ? AND substr(t.Tag, 1, ?) = ?
				)`,
			newPrefix, prefixChars+1, sep, prefixChars, oldPrefix,
			string(tags.TagTypeProperty), len(imagePropertyValuePrefix), imagePropertyValuePrefix,
		)
		if execErr != nil {
			db.NoteCorruption(execErr)
			return 0, fmt.Errorf("rewrite %s image paths: %w", table, execErr)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return 0, fmt.Errorf("count rewritten %s image paths: %w", table, rowsErr)
		}
		total += rows
	}

	if apply != nil {
		if err := apply(); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit image property rewrite: %w", err)
	}
	committed = true
	return total, nil
}
