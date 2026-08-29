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
	"fmt"

	sqlite3 "github.com/mattn/go-sqlite3"
)

const sqliteMediaDriver = "sqlite3_zaparoo_media"

func init() {
	sql.Register(sqliteMediaDriver, &sqlite3.SQLiteDriver{
		ConnectHook: registerMediaSQLiteCollations,
	})
}

func registerMediaSQLiteCollations(conn *sqlite3.SQLiteConn) error {
	if err := conn.RegisterCollation(browseTitleCollationName, compareBrowseTitles); err != nil {
		return fmt.Errorf("register media title collation: %w", err)
	}
	if err := conn.RegisterCollation(browseDirectoryCollationName, compareBrowseDirectoryNames); err != nil {
		return fmt.Errorf("register media directory collation: %w", err)
	}
	return nil
}
