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
	"database/sql"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
)

// SetDirectoryHidden records (or clears) the hide on one folder for one
// system. It is a single row however much media the folder holds, and it
// advances the preferences revision with the write.
func (db *UserDB) SetDirectoryHidden(systemID, path string, hidden bool) error {
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	path = pathutil.CanonicalMediaPath(path)
	now := time.Now().Unix()
	return mediaUserDataTx(db.ctx, conn, now, func(tx *sql.Tx) error {
		var err error
		if hidden {
			_, err = tx.ExecContext(db.ctx, `
				insert into HiddenDirectories(SystemID, Path, CreatedAt) values (?, ?, ?)
				on conflict(SystemID, Path) do nothing;
			`, systemID, path, now)
		} else {
			_, err = tx.ExecContext(db.ctx,
				`delete from HiddenDirectories where SystemID = ? and Path = ?;`, systemID, path)
		}
		if err != nil {
			return fmt.Errorf("failed to set hidden directory: %w", err)
		}
		return nil
	})
}

// ListHiddenDirectories returns every hidden folder, ordered by system and
// path.
func (db *UserDB) ListHiddenDirectories() ([]database.HiddenDirectory, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	rows, err := conn.QueryContext(db.ctx,
		`select SystemID, Path from HiddenDirectories order by SystemID, Path;`)
	if err != nil {
		return nil, fmt.Errorf("failed to list hidden directories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	dirs := make([]database.HiddenDirectory, 0)
	for rows.Next() {
		var dir database.HiddenDirectory
		if scanErr := rows.Scan(&dir.SystemID, &dir.Path); scanErr != nil {
			return nil, fmt.Errorf("failed to scan hidden directory: %w", scanErr)
		}
		dirs = append(dirs, dir)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("failed to read hidden directories: %w", rowsErr)
	}
	return dirs, nil
}
