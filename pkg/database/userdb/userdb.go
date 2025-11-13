// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	_ "github.com/mattn/go-sqlite3"
)

var ErrNullSQL = errors.New("UserDB is not connected")

const sqliteConnParams = "?_journal_mode=WAL&_synchronous=FULL&_busy_timeout=5000"

type UserDB struct {
	sql *sql.DB
	pl  platforms.Platform
	ctx context.Context
}

func OpenUserDB(ctx context.Context, pl platforms.Platform) (*UserDB, error) {
	db := &UserDB{sql: nil, pl: pl, ctx: ctx}
	err := db.Open()
	return db, err
}

func (db *UserDB) Open() error {
	exists := true
	dbPath := db.GetDBPath()
	_, err := os.Stat(dbPath)
	if err != nil {
		exists = false
		mkdirErr := os.MkdirAll(filepath.Dir(dbPath), 0o750)
		if mkdirErr != nil {
			return fmt.Errorf("failed to create directory for database: %w", mkdirErr)
		}
	}
	sqlInstance, err := sql.Open("sqlite3", dbPath+sqliteConnParams)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	db.sql = sqlInstance
	if !exists {
		return db.Allocate()
	}
	return nil
}

func (db *UserDB) GetDBPath() string {
	return filepath.Join(helpers.DataDir(db.pl), config.UserDbFile)
}

func (db *UserDB) UnsafeGetSQLDb() *sql.DB {
	return db.sql
}

func (db *UserDB) Truncate() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlTruncate(db.ctx, db.sql)
}

func (db *UserDB) Allocate() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlAllocate(db.sql)
}

func (db *UserDB) MigrateUp() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlMigrateUp(db.sql)
}

func (db *UserDB) Vacuum() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlVacuum(db.ctx, db.sql)
}

func (db *UserDB) CleanupHistory(retentionDays int) (int64, error) {
	if db.sql == nil {
		return 0, ErrNullSQL
	}
	return sqlCleanupHistory(db.ctx, db.sql, retentionDays)
}

func (db *UserDB) Close() error {
	if db.sql == nil {
		return nil
	}
	err := db.sql.Close()
	if err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	return nil
}

// SetSQLForTesting allows injection of a sql.DB instance for testing purposes.
// This method should only be used in tests to set up in-memory databases.
func (db *UserDB) SetSQLForTesting(ctx context.Context, sqlDB *sql.DB, platform platforms.Platform) error {
	db.sql = sqlDB
	db.pl = platform
	db.ctx = ctx

	// Initialize the database schema
	return db.Allocate()
}

// TODO: reader source (physical reader vs web)
// TODO: metadata

func (db *UserDB) AddHistory(entry *database.HistoryEntry) error {
	return sqlAddHistory(db.ctx, db.sql, *entry)
}

func (db *UserDB) GetHistory(lastID int) ([]database.HistoryEntry, error) {
	return sqlGetHistoryWithOffset(db.ctx, db.sql, lastID)
}

func (db *UserDB) UpdateZapLinkHost(host string, zapscript int) error {
	return sqlUpdateZapLinkHost(db.ctx, db.sql, host, zapscript)
}

func (db *UserDB) GetZapLinkHost(host string) (found, zapScript bool, err error) {
	return sqlGetZapLinkHost(db.ctx, db.sql, host)
}

func (db *UserDB) UpdateZapLinkCache(url, zapscript string) error {
	return sqlUpdateZapLinkCache(db.ctx, db.sql, url, zapscript)
}

func (db *UserDB) GetZapLinkCache(url string) (string, error) {
	return sqlGetZapLinkCache(db.ctx, db.sql, url)
}
