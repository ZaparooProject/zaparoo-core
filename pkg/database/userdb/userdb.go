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
	"os"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

var ErrNullSQL = errors.New("UserDB is not connected")

const sqliteConnParams = "?_journal_mode=WAL&_synchronous=FULL&_busy_timeout=5000" +
	"&_cache_size=-512&_mmap_size=0"

type UserDB struct {
	pl     platforms.Platform
	ctx    context.Context
	sql    database.Conn
	dbPath string
}

func OpenUserDB(ctx context.Context, pl platforms.Platform) (*UserDB, error) {
	db := &UserDB{pl: pl, ctx: ctx}
	err := db.Open()
	return db, err
}

func (db *UserDB) Open() error {
	exists := true
	dbPath := db.GetDBPath()
	db.dbPath = dbPath
	log.Debug().Str("path", dbPath).Msg("checking if database file exists")

	_, err := os.Stat(dbPath)
	if err != nil {
		exists = false
		log.Debug().Msg("database file does not exist, creating directory")
		mkdirErr := os.MkdirAll(filepath.Dir(dbPath), 0o750)
		if mkdirErr != nil {
			return fmt.Errorf("failed to create directory for database: %w", mkdirErr)
		}
	}

	log.Debug().Msg("opening user database connection")
	sqlInstance, err := sql.Open("sqlite3", dbPath+sqliteConnParams)
	if err != nil {
		return fmt.Errorf("failed to open user database: %w", err)
	}
	db.sql.Store(sqlInstance)
	if _, err = sqlInstance.ExecContext(db.ctx, "PRAGMA cell_size_check=ON"); err != nil {
		if database.IsCorruptionError(err) {
			db.MarkCorrupt(fmt.Sprintf("cell_size_check failed during open: %v", err))
			log.Warn().Err(err).Msg("user database cell size check failed during open")
		} else {
			// cell_size_check is a best-effort safety pragma; a non-corruption failure
			// (e.g. a transient "database is locked" while another connection is active
			// during a restore) must not disconnect an otherwise-usable database. Keep the
			// connection and re-attempt the pragma on the next open.
			log.Warn().Err(err).Msg("failed to enable user database cell size checks; continuing without")
		}
	}

	if !exists {
		log.Debug().Msg("user database is new, allocating schema")
		err := db.Allocate()
		if err != nil {
			return err
		}
	}

	return nil
}

func (db *UserDB) GetDBPath() string {
	return filepath.Join(helpers.DataDir(db.pl), config.UserDbFile)
}

func (db *UserDB) UnsafeGetSQLDb() *sql.DB {
	return db.sql.Load()
}

func (db *UserDB) Truncate() error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlTruncate(db.ctx, db.sql.Load())
}

func (db *UserDB) Allocate() error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlAllocate(db.sql.Load(), db.dbPathForSidecar())
}

func (db *UserDB) MigrateUp() error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlMigrateUp(db.sql.Load(), db.dbPathForSidecar())
}

// dbPathForSidecar returns the on-disk DB path for sidecar lookup, or ""
// when no path is available (test instances that bypass Open).
func (db *UserDB) dbPathForSidecar() string {
	return db.dbPath
}

func (db *UserDB) Vacuum() error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlVacuum(db.ctx, db.sql.Load())
}

func (db *UserDB) CleanupHistory(retentionDays int) (int64, error) {
	if db.sql.Load() == nil {
		return 0, ErrNullSQL
	}
	return sqlCleanupHistory(db.ctx, db.sql.Load(), retentionDays)
}

func (db *UserDB) Close() error {
	if db.sql.Load() == nil {
		return nil
	}
	err := db.sql.Load().Close()
	if err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	return nil
}

// SetSQLForTesting allows injection of a sql.DB instance for testing purposes.
// This method should only be used in tests to set up in-memory databases.
func (db *UserDB) SetSQLForTesting(ctx context.Context, sqlDB *sql.DB, platform platforms.Platform) error {
	db.sql.Store(sqlDB)
	db.pl = platform
	db.ctx = ctx

	// Initialize the database schema
	return db.Allocate()
}

// TODO: reader source (physical reader vs web)
// TODO: metadata

func (db *UserDB) AddHistory(entry *database.HistoryEntry) error {
	return sqlAddHistory(db.ctx, db.sql.Load(), *entry)
}

func (db *UserDB) GetHistory(lastID int64) ([]database.HistoryEntry, error) {
	return sqlGetHistoryWithOffset(db.ctx, db.sql.Load(), lastID)
}

func (db *UserDB) UpdateZapLinkHost(host string, zapscript int) error {
	return sqlUpdateZapLinkHost(db.ctx, db.sql.Load(), host, zapscript)
}

func (db *UserDB) GetZapLinkHost(host string) (found, zapScript bool, err error) {
	return sqlGetZapLinkHost(db.ctx, db.sql.Load(), host)
}

func (db *UserDB) GetSupportedZapLinkHosts() ([]string, error) {
	return sqlGetSupportedZapLinkHosts(db.ctx, db.sql.Load())
}

func (db *UserDB) PruneExpiredZapLinkHosts(olderThan time.Duration) (int64, error) {
	return sqlPruneExpiredZapLinkHosts(db.ctx, db.sql.Load(), olderThan)
}

func (db *UserDB) UpdateZapLinkCache(url, zapscript string) error {
	return sqlUpdateZapLinkCache(db.ctx, db.sql.Load(), url, zapscript)
}

func (db *UserDB) GetZapLinkCache(url string) (string, error) {
	return sqlGetZapLinkCache(db.ctx, db.sql.Load(), url)
}
