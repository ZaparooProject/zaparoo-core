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

//go:build sqlite_trace || trace

package mediadb

import (
	"database/sql"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/sqltrace"
	sqlite3 "github.com/mattn/go-sqlite3"
)

const sqliteTraceRuntimeDriver = "sqlite3_zaparoo_trace"

var runtimeSQLTrace = sqltrace.NewCollector("mediadb")

func init() {
	sql.Register(sqliteTraceRuntimeDriver, &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			if err := registerMediaSQLiteCollations(conn); err != nil {
				return err
			}
			if !config.IsDevelopmentVersion() {
				return nil
			}
			return runtimeSQLTrace.Attach(conn)
		},
	})
}

func sqliteDriverName() string {
	if config.IsDevelopmentVersion() {
		return sqliteTraceRuntimeDriver
	}
	return sqliteMediaDriver
}

func logSQLTraceSummary() {
	runtimeSQLTrace.LogTop(sqltrace.TopStatements)
}
