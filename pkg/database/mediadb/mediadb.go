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

package mediadb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

var ErrNullSQL = errors.New("MediaDB is not connected")

type MediaDB struct {
	sql *sql.DB
	pl  platforms.Platform
	ctx context.Context
	// Prepared statements for batch operations during scanning
	tx                   *sql.Tx
	stmtInsertSystem     *sql.Stmt
	stmtInsertMediaTitle *sql.Stmt
	stmtInsertMedia      *sql.Stmt
	stmtInsertTag        *sql.Stmt
	stmtInsertMediaTag   *sql.Stmt
}

func OpenMediaDB(ctx context.Context, pl platforms.Platform) (*MediaDB, error) {
	db := &MediaDB{sql: nil, pl: pl, ctx: ctx}
	err := db.Open()
	return db, err
}

func (db *MediaDB) Open() error {
	exists := true
	dbPath := db.GetDBPath()
	_, err := os.Stat(dbPath)
	if err != nil {
		exists = false
		mkdirErr := os.MkdirAll(filepath.Dir(dbPath), 0o750)
		if mkdirErr != nil {
			return fmt.Errorf("failed to create database directory: %w", mkdirErr)
		}
	}
	sqlInstance, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	db.sql = sqlInstance
	if !exists {
		return db.Allocate()
	}
	return nil
}

func (db *MediaDB) GetDBPath() string {
	return filepath.Join(helpers.DataDir(db.pl), config.MediaDbFile)
}

func (db *MediaDB) Exists() bool {
	return db.sql != nil
}

func (db *MediaDB) UpdateLastGenerated() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlUpdateLastGenerated(db.ctx, db.sql)
}

func (db *MediaDB) GetLastGenerated() (time.Time, error) {
	if db.sql == nil {
		return time.Time{}, ErrNullSQL
	}
	return sqlGetLastGenerated(db.ctx, db.sql)
}

func (db *MediaDB) UnsafeGetSQLDb() *sql.DB {
	return db.sql
}

func (db *MediaDB) Truncate() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlTruncate(db.ctx, db.sql)
}

func (db *MediaDB) Allocate() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlAllocate(db.sql)
}

func (db *MediaDB) MigrateUp() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlMigrateUp(db.sql)
}

func (db *MediaDB) Vacuum() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlVacuum(db.ctx, db.sql)
}

func (db *MediaDB) Close() error {
	if db.sql == nil {
		return nil
	}
	err := db.sql.Close()
	if err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	return nil
}

// closeAllPreparedStatements closes all prepared statements and sets them to nil
func (db *MediaDB) closeAllPreparedStatements() {
	if db.stmtInsertSystem != nil {
		_ = db.stmtInsertSystem.Close()
		db.stmtInsertSystem = nil
	}
	if db.stmtInsertMediaTitle != nil {
		_ = db.stmtInsertMediaTitle.Close()
		db.stmtInsertMediaTitle = nil
	}
	if db.stmtInsertMedia != nil {
		_ = db.stmtInsertMedia.Close()
		db.stmtInsertMedia = nil
	}
	if db.stmtInsertTag != nil {
		_ = db.stmtInsertTag.Close()
		db.stmtInsertTag = nil
	}
	if db.stmtInsertMediaTag != nil {
		_ = db.stmtInsertMediaTag.Close()
		db.stmtInsertMediaTag = nil
	}
}

// RollbackTransaction rolls back the current transaction and cleans up resources
func (db *MediaDB) RollbackTransaction() error {
	if db.tx == nil {
		return nil // No active transaction
	}

	// Clean up prepared statements first
	db.closeAllPreparedStatements()

	// Rollback the transaction
	err := db.tx.Rollback()
	db.tx = nil
	if err != nil {
		return fmt.Errorf("failed to rollback transaction: %w", err)
	}

	return nil
}

func (db *MediaDB) BeginTransaction() error {
	if db.sql == nil {
		return ErrNullSQL
	}

	// Begin a proper transaction
	tx, err := db.sql.BeginTx(db.ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	db.tx = tx

	// Prepare statements for batch operations - clean up on any error
	if db.stmtInsertSystem, err = tx.PrepareContext(db.ctx, `
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (?, ?, ?)
	`); err != nil {
		if rbErr := db.RollbackTransaction(); rbErr != nil {
			log.Error().Err(rbErr).Msg("failed to rollback transaction during prepared statement setup")
		}
		return fmt.Errorf("failed to prepare insert system statement: %w", err)
	}

	if db.stmtInsertMediaTitle, err = tx.PrepareContext(db.ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (?, ?, ?, ?)
	`); err != nil {
		if rbErr := db.RollbackTransaction(); rbErr != nil {
			log.Error().Err(rbErr).Msg("failed to rollback transaction during prepared statement setup")
		}
		return fmt.Errorf("failed to prepare insert media title statement: %w", err)
	}

	if db.stmtInsertMedia, err = tx.PrepareContext(db.ctx, `
		INSERT INTO Media (DBID, Path, MediaTitleDBID) VALUES (?, ?, ?)
	`); err != nil {
		if rbErr := db.RollbackTransaction(); rbErr != nil {
			log.Error().Err(rbErr).Msg("failed to rollback transaction during prepared statement setup")
		}
		return fmt.Errorf("failed to prepare insert media statement: %w", err)
	}

	if db.stmtInsertTag, err = tx.PrepareContext(db.ctx, `
		INSERT INTO Tags (DBID, Tag, TypeDBID) VALUES (?, ?, ?)
	`); err != nil {
		if rbErr := db.RollbackTransaction(); rbErr != nil {
			log.Error().Err(rbErr).Msg("failed to rollback transaction during prepared statement setup")
		}
		return fmt.Errorf("failed to prepare insert tag statement: %w", err)
	}

	if db.stmtInsertMediaTag, err = tx.PrepareContext(db.ctx, `
		INSERT INTO MediaTags (DBID, MediaDBID, TagDBID) VALUES (?, ?, ?)
	`); err != nil {
		if rbErr := db.RollbackTransaction(); rbErr != nil {
			log.Error().Err(rbErr).Msg("failed to rollback transaction during prepared statement setup")
		}
		return fmt.Errorf("failed to prepare insert media tag statement: %w", err)
	}

	return nil
}

func (db *MediaDB) CommitTransaction() error {
	if db.tx == nil {
		return nil // No active transaction
	}

	// Always clean up prepared statements first
	db.closeAllPreparedStatements()

	// Commit the transaction
	err := db.tx.Commit()
	if err != nil {
		// Try to rollback and combine errors if both fail
		if rbErr := db.tx.Rollback(); rbErr != nil {
			db.tx = nil
			return fmt.Errorf("commit failed: %w; rollback also failed: %w", err, rbErr)
		}
		db.tx = nil
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	db.tx = nil
	return nil
}

func (db *MediaDB) reindexTablesWithTransaction() error {
	return sqlIndexTables(db.ctx, db.tx)
}

// PreparedStatementProvider interface implementation
func (db *MediaDB) GetInsertSystemStmt() *sql.Stmt {
	return db.stmtInsertSystem
}

func (db *MediaDB) GetInsertMediaTitleStmt() *sql.Stmt {
	return db.stmtInsertMediaTitle
}

func (db *MediaDB) GetInsertMediaStmt() *sql.Stmt {
	return db.stmtInsertMedia
}

func (db *MediaDB) GetInsertTagStmt() *sql.Stmt {
	return db.stmtInsertTag
}

func (db *MediaDB) GetInsertMediaTagStmt() *sql.Stmt {
	return db.stmtInsertMediaTag
}

// DBExecutor interface implementation for MediaDB
func (db *MediaDB) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	if db.tx != nil {
		stmt, err := db.tx.PrepareContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare transaction statement: %w", err)
		}
		return stmt, nil
	}
	stmt, err := db.sql.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare database statement: %w", err)
	}
	return stmt, nil
}

func (db *MediaDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if db.tx != nil {
		result, err := db.tx.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to execute transaction statement: %w", err)
		}
		return result, nil
	}
	result, err := db.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute database statement: %w", err)
	}
	return result, nil
}

func (db *MediaDB) ReindexTables() error {
	// Use transaction if active, otherwise use direct database connection
	if db.tx != nil {
		return db.reindexTablesWithTransaction()
	}
	return sqlIndexTables(db.ctx, db.sql)
}

// SearchMediaPathExact returns indexed names matching an exact query (case-insensitive).
func (db *MediaDB) SearchMediaPathExact(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	if db.sql == nil {
		return make([]database.SearchResult, 0), ErrNullSQL
	}
	return sqlSearchMediaPathExact(db.ctx, db.sql, systems, query)
}

// SearchMediaPathWords returns indexed names that include every word in a query (case-insensitive).
func (db *MediaDB) SearchMediaPathWords(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	if db.sql == nil {
		return make([]database.SearchResult, 0), ErrNullSQL
	}
	qWords := strings.Fields(strings.ToLower(query))
	return sqlSearchMediaPathParts(db.ctx, db.sql, systems, qWords)
}

func (db *MediaDB) SearchMediaPathGlob(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	// TODO: glob pattern matching unclear on some patterns
	// query == path like with possible *
	var nullResults []database.SearchResult
	if db.sql == nil {
		return nullResults, ErrNullSQL
	}
	var parts []string
	for _, part := range strings.Split(query, "*") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		// return random instead
		rnd, err := db.RandomGame(systems)
		if err != nil {
			return nullResults, err
		}
		return []database.SearchResult{rnd}, nil
	}

	// TODO: since we approximated a glob, we should actually check
	//       result paths against base glob to confirm
	return sqlSearchMediaPathParts(db.ctx, db.sql, systems, parts)
}

// SystemIndexed returns true if a specific system is indexed in the media database.
func (db *MediaDB) SystemIndexed(system systemdefs.System) bool {
	if db.sql == nil {
		return false
	}
	return sqlSystemIndexed(db.ctx, db.sql, system)
}

// IndexedSystems returns all systems indexed in the media database.
func (db *MediaDB) IndexedSystems() ([]string, error) {
	// TODO: what is a JBONE??
	// JBONE: return string map of Systems.Key, Systems.Indexed
	var systems []string
	if db.sql == nil {
		return systems, ErrNullSQL
	}
	return sqlIndexedSystems(db.ctx, db.sql)
}

// RandomGame returns a random game from specified systems.
func (db *MediaDB) RandomGame(systems []systemdefs.System) (database.SearchResult, error) {
	var result database.SearchResult
	if db.sql == nil {
		return result, ErrNullSQL
	}

	system, err := helpers.RandomElem(systems)
	if err != nil {
		return result, fmt.Errorf("failed to select random system: %w", err)
	}

	return sqlRandomGame(db.ctx, db.sql, system)
}

func (db *MediaDB) FindSystem(row database.System) (database.System, error) {
	return sqlFindSystem(db.ctx, db.sql, row)
}

func (db *MediaDB) InsertSystem(row database.System) (database.System, error) {
	return sqlInsertSystem(db.ctx, db, row)
}

func (db *MediaDB) FindOrInsertSystem(row database.System) (database.System, error) {
	system, err := db.FindSystem(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertSystem(row)
	}
	return system, err
}

func (db *MediaDB) FindMediaTitle(row database.MediaTitle) (database.MediaTitle, error) {
	return sqlFindMediaTitle(db.ctx, db.sql, row)
}

func (db *MediaDB) InsertMediaTitle(row database.MediaTitle) (database.MediaTitle, error) {
	return sqlInsertMediaTitle(db.ctx, db, row)
}

func (db *MediaDB) FindOrInsertMediaTitle(row database.MediaTitle) (database.MediaTitle, error) {
	system, err := db.FindMediaTitle(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertMediaTitle(row)
	}
	return system, err
}

func (db *MediaDB) FindMedia(row database.Media) (database.Media, error) {
	return sqlFindMedia(db.ctx, db.sql, row)
}

func (db *MediaDB) InsertMedia(row database.Media) (database.Media, error) {
	return sqlInsertMedia(db.ctx, db, row)
}

func (db *MediaDB) FindOrInsertMedia(row database.Media) (database.Media, error) {
	system, err := db.FindMedia(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertMedia(row)
	}
	return system, err
}

func (db *MediaDB) FindTagType(row database.TagType) (database.TagType, error) {
	return sqlFindTagType(db.ctx, db.sql, row)
}

func (db *MediaDB) InsertTagType(row database.TagType) (database.TagType, error) {
	return sqlInsertTagType(db.ctx, db.sql, row)
}

func (db *MediaDB) FindOrInsertTagType(row database.TagType) (database.TagType, error) {
	system, err := db.FindTagType(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertTagType(row)
	}
	return system, err
}

func (db *MediaDB) FindTag(row database.Tag) (database.Tag, error) {
	return sqlFindTag(db.ctx, db.sql, row)
}

func (db *MediaDB) InsertTag(row database.Tag) (database.Tag, error) {
	return sqlInsertTag(db.ctx, db, row)
}

func (db *MediaDB) FindOrInsertTag(row database.Tag) (database.Tag, error) {
	system, err := db.FindTag(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertTag(row)
	}
	return system, err
}

func (db *MediaDB) FindMediaTag(row database.MediaTag) (database.MediaTag, error) {
	return sqlFindMediaTag(db.ctx, db.sql, row)
}

func (db *MediaDB) InsertMediaTag(row database.MediaTag) (database.MediaTag, error) {
	return sqlInsertMediaTag(db.ctx, db, row)
}

func (db *MediaDB) FindOrInsertMediaTag(row database.MediaTag) (database.MediaTag, error) {
	system, err := db.FindMediaTag(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertMediaTag(row)
	}
	return system, err
}
