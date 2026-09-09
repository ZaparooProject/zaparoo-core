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
	"database/sql"
	"errors"

	"github.com/mattn/go-sqlite3"
)

// rollbackSQLTransaction completes Go's transaction even when SQLite has already
// rolled it back after an interrupted write. Unknown state remains reportable.
func rollbackSQLTransaction(tx *sql.Tx, conn *sql.Conn) error {
	sqliteEnded := sqliteTransactionEnded(conn)
	err := tx.Rollback()
	if rollbackAlreadyComplete(err, sqliteEnded) {
		return nil
	}
	return err //nolint:wrapcheck // RollbackTransaction adds the operation context.
}

// sqliteTransactionEnded requires positive driver state, not an error message.
func sqliteTransactionEnded(conn *sql.Conn) bool {
	sqliteEnded := false
	if conn != nil {
		if err := conn.Raw(func(driverConn any) error {
			if sqliteConn, ok := driverConn.(*sqlite3.SQLiteConn); ok {
				sqliteEnded = sqliteConn.AutoCommit()
			}
			return nil
		}); err != nil {
			sqliteEnded = false
		}
	}
	return sqliteEnded
}

func rollbackAlreadyComplete(err error, sqliteEnded bool) bool {
	if errors.Is(err, sql.ErrTxDone) {
		return true
	}
	var sqliteErr sqlite3.Error
	return sqliteEnded && errors.As(err, &sqliteErr) &&
		sqliteErr.Code == sqlite3.ErrError && sqliteErr.ExtendedCode == sqlite3.ErrError.Extend(0)
}
