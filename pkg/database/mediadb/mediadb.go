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
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

var ErrNullSQL = errors.New("MediaDB is not connected")

// Indexing status constants
const (
	IndexingStatusRunning   = "running"
	IndexingStatusPending   = "pending"
	IndexingStatusCompleted = "completed"
	IndexingStatusFailed    = "failed"
	IndexingStatusCancelled = "cancelled"
)

const sqliteConnParams = "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000" +
	"&_cache_size=-64000&_temp_store=MEMORY&_mmap_size=30000000"

type MediaDB struct {
	pl                   platforms.Platform
	ctx                  context.Context
	sql                  *sql.DB
	tx                   *sql.Tx
	stmtInsertSystem     *sql.Stmt
	stmtInsertMediaTitle *sql.Stmt
	stmtInsertMedia      *sql.Stmt
	stmtInsertTag        *sql.Stmt
	stmtInsertMediaTag   *sql.Stmt
	isOptimizing         atomic.Bool
	backgroundOps        sync.WaitGroup
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
	sqlInstance, err := sql.Open("sqlite3", dbPath+sqliteConnParams)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	db.sql = sqlInstance

	if !exists {
		err = db.Allocate()
		if err != nil {
			return err
		}
	}

	// Run PRAGMA optimize after database is opened and potentially allocated
	_, err = db.sql.ExecContext(db.ctx, "PRAGMA optimize;")
	if err != nil {
		log.Warn().Err(err).Msg("failed to run PRAGMA optimize")
	}

	// Check for incomplete optimization and resume if needed
	db.checkAndResumeOptimization()

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

func (db *MediaDB) SetOptimizationStatus(status string) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlSetOptimizationStatus(db.ctx, db.sql, status)
}

func (db *MediaDB) GetOptimizationStatus() (string, error) {
	if db.sql == nil {
		return "", ErrNullSQL
	}
	return sqlGetOptimizationStatus(db.ctx, db.sql)
}

func (db *MediaDB) SetOptimizationStep(step string) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlSetOptimizationStep(db.ctx, db.sql, step)
}

func (db *MediaDB) GetOptimizationStep() (string, error) {
	if db.sql == nil {
		return "", ErrNullSQL
	}
	return sqlGetOptimizationStep(db.ctx, db.sql)
}

func (db *MediaDB) SetIndexingStatus(status string) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlSetIndexingStatus(db.ctx, db.sql, status)
}

func (db *MediaDB) GetIndexingStatus() (string, error) {
	if db.sql == nil {
		return "", ErrNullSQL
	}
	return sqlGetIndexingStatus(db.ctx, db.sql)
}

func (db *MediaDB) SetLastIndexedSystem(systemID string) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlSetLastIndexedSystem(db.ctx, db.sql, systemID)
}

func (db *MediaDB) GetLastIndexedSystem() (string, error) {
	if db.sql == nil {
		return "", ErrNullSQL
	}
	return sqlGetLastIndexedSystem(db.ctx, db.sql)
}

func (db *MediaDB) SetIndexingSystems(systemIDs []string) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlSetIndexingSystems(db.ctx, db.sql, systemIDs)
}

func (db *MediaDB) GetIndexingSystems() ([]string, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}
	return sqlGetIndexingSystems(db.ctx, db.sql)
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

func (db *MediaDB) TruncateSystems(systemIDs []string) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlTruncateSystems(db.ctx, db.sql, systemIDs)
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

// SetSQLForTesting allows injection of a sql.DB instance for testing purposes.
// This method should only be used in tests to set up in-memory databases.
func (db *MediaDB) SetSQLForTesting(ctx context.Context, sqlDB *sql.DB, platform platforms.Platform) error {
	db.sql = sqlDB
	db.ctx = ctx
	db.pl = platform

	// Initialize the database schema
	return db.Allocate()
}

// closeAllPreparedStatements closes all prepared statements and sets them to nil
func (db *MediaDB) closeAllPreparedStatements() {
	if db.stmtInsertSystem != nil {
		if closeErr := db.stmtInsertSystem.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close prepared statement: stmtInsertSystem")
		}
		db.stmtInsertSystem = nil
	}
	if db.stmtInsertMediaTitle != nil {
		if closeErr := db.stmtInsertMediaTitle.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close prepared statement: stmtInsertMediaTitle")
		}
		db.stmtInsertMediaTitle = nil
	}
	if db.stmtInsertMedia != nil {
		if closeErr := db.stmtInsertMedia.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close prepared statement: stmtInsertMedia")
		}
		db.stmtInsertMedia = nil
	}
	if db.stmtInsertTag != nil {
		if closeErr := db.stmtInsertTag.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close prepared statement: stmtInsertTag")
		}
		db.stmtInsertTag = nil
	}
	if db.stmtInsertMediaTag != nil {
		if closeErr := db.stmtInsertMediaTag.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close prepared statement: stmtInsertMediaTag")
		}
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

// rollbackAndLogError helper function to handle rollback with error logging
func (db *MediaDB) rollbackAndLogError() {
	if rbErr := db.RollbackTransaction(); rbErr != nil {
		log.Error().Err(rbErr).Msg("failed to rollback transaction during prepared statement setup")
	}
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
	if db.stmtInsertSystem, err = tx.PrepareContext(db.ctx, insertSystemSQL); err != nil {
		db.rollbackAndLogError()
		return fmt.Errorf("failed to prepare insert system statement: %w", err)
	}

	if db.stmtInsertMediaTitle, err = tx.PrepareContext(db.ctx, insertMediaTitleSQL); err != nil {
		db.rollbackAndLogError()
		return fmt.Errorf("failed to prepare insert media title statement: %w", err)
	}

	if db.stmtInsertMedia, err = tx.PrepareContext(db.ctx, insertMediaSQL); err != nil {
		db.rollbackAndLogError()
		return fmt.Errorf("failed to prepare insert media statement: %w", err)
	}

	if db.stmtInsertTag, err = tx.PrepareContext(db.ctx, insertTagSQL); err != nil {
		db.rollbackAndLogError()
		return fmt.Errorf("failed to prepare insert tag statement: %w", err)
	}

	if db.stmtInsertMediaTag, err = tx.PrepareContext(db.ctx, insertMediaTagSQL); err != nil {
		db.rollbackAndLogError()
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
	return sqlIndexTablesWithTransaction(db.ctx, db.tx)
}

func (db *MediaDB) insertSystemWithPreparedStmt(row database.System) (database.System, error) {
	return sqlInsertSystemWithPreparedStmt(db.ctx, db.stmtInsertSystem, row)
}

func (db *MediaDB) insertMediaTitleWithPreparedStmt(row database.MediaTitle) (database.MediaTitle, error) {
	return sqlInsertMediaTitleWithPreparedStmt(db.ctx, db.stmtInsertMediaTitle, row)
}

func (db *MediaDB) insertMediaWithPreparedStmt(row database.Media) (database.Media, error) {
	return sqlInsertMediaWithPreparedStmt(db.ctx, db.stmtInsertMedia, row)
}

func (db *MediaDB) insertTagWithPreparedStmt(row database.Tag) (database.Tag, error) {
	return sqlInsertTagWithPreparedStmt(db.ctx, db.stmtInsertTag, row)
}

func (db *MediaDB) insertMediaTagWithPreparedStmt(row database.MediaTag) (database.MediaTag, error) {
	return sqlInsertMediaTagWithPreparedStmt(db.ctx, db.stmtInsertMediaTag, row)
}

func (db *MediaDB) ReindexTables() error {
	// Use transaction if active, otherwise use direct database connection
	if db.tx != nil {
		return db.reindexTablesWithTransaction()
	}
	return sqlIndexTables(db.ctx, db.sql)
}

func (db *MediaDB) CreateIndexes() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlCreateIndexesOnly(db.ctx, db.sql)
}

func (db *MediaDB) Analyze() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlAnalyze(db.ctx, db.sql)
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

func (db *MediaDB) SearchMediaPathWordsWithCursor(
	ctx context.Context, systems []systemdefs.System, query string, cursor *int64, limit int,
) ([]database.SearchResultWithCursor, error) {
	if db.sql == nil {
		return make([]database.SearchResultWithCursor, 0), ErrNullSQL
	}
	qWords := strings.Fields(strings.ToLower(query))
	return sqlSearchMediaPathPartsWithCursor(ctx, db.sql, systems, qWords, cursor, limit)
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

// GetTotalMediaCount returns the total number of media entries in the database.
func (db *MediaDB) GetTotalMediaCount() (int, error) {
	if db.sql == nil {
		return 0, ErrNullSQL
	}
	return sqlGetTotalMediaCount(db.ctx, db.sql)
}

func (db *MediaDB) FindSystem(row database.System) (database.System, error) {
	return sqlFindSystem(db.ctx, db.sql, row)
}

func (db *MediaDB) InsertSystem(row database.System) (database.System, error) {
	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertSystem != nil {
		return db.insertSystemWithPreparedStmt(row)
	}
	return sqlInsertSystem(db.ctx, db.sql, row)
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
	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertMediaTitle != nil {
		return db.insertMediaTitleWithPreparedStmt(row)
	}
	return sqlInsertMediaTitle(db.ctx, db.sql, row)
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
	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertMedia != nil {
		return db.insertMediaWithPreparedStmt(row)
	}
	return sqlInsertMedia(db.ctx, db.sql, row)
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
	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertTag != nil {
		return db.insertTagWithPreparedStmt(row)
	}
	return sqlInsertTag(db.ctx, db.sql, row)
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
	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertMediaTag != nil {
		return db.insertMediaTagWithPreparedStmt(row)
	}
	return sqlInsertMediaTag(db.ctx, db.sql, row)
}

func (db *MediaDB) FindOrInsertMediaTag(row database.MediaTag) (database.MediaTag, error) {
	system, err := db.FindMediaTag(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertMediaTag(row)
	}
	return system, err
}

// GetMax*ID methods for resume functionality
func (db *MediaDB) GetMaxSystemID() (int64, error) {
	return sqlGetMaxID(db.ctx, db.sql, "Systems", "DBID")
}

func (db *MediaDB) GetMaxTitleID() (int64, error) {
	return sqlGetMaxID(db.ctx, db.sql, "MediaTitles", "DBID")
}

func (db *MediaDB) GetMaxMediaID() (int64, error) {
	return sqlGetMaxID(db.ctx, db.sql, "Media", "DBID")
}

func (db *MediaDB) GetMaxTagTypeID() (int64, error) {
	return sqlGetMaxID(db.ctx, db.sql, "TagTypes", "DBID")
}

func (db *MediaDB) GetMaxTagID() (int64, error) {
	return sqlGetMaxID(db.ctx, db.sql, "Tags", "DBID")
}

func (db *MediaDB) GetMaxMediaTagID() (int64, error) {
	return sqlGetMaxID(db.ctx, db.sql, "MediaTags", "DBID")
}

func (db *MediaDB) GetAllSystems() ([]database.System, error) {
	return sqlGetAllSystems(db.ctx, db.sql)
}

func (db *MediaDB) GetAllMediaTitles() ([]database.MediaTitle, error) {
	return sqlGetAllMediaTitles(db.ctx, db.sql)
}

func (db *MediaDB) GetAllMedia() ([]database.Media, error) {
	return sqlGetAllMedia(db.ctx, db.sql)
}

// RunBackgroundOptimization performs database optimization operations in the background.
// This includes creating indexes, running ANALYZE, and vacuuming the database.
// It can be safely interrupted and resumed later.
func (db *MediaDB) RunBackgroundOptimization() {
	if !db.isOptimizing.CompareAndSwap(false, true) {
		log.Info().Msg("background optimization is already running, skipping")
		return
	}
	db.backgroundOps.Add(1)
	defer func() {
		db.isOptimizing.Store(false)
		db.backgroundOps.Done()
	}()

	if db.sql == nil {
		log.Error().Msg("cannot run background optimization: database not connected")
		return
	}

	log.Info().Msg("starting background database optimization")

	// Set status to running
	if err := db.SetOptimizationStatus("running"); err != nil {
		log.Error().Err(err).Msg("failed to set optimization status to running")
		return
	}

	// Define optimization steps
	type optimizationStep struct {
		fn         func() error
		name       string
		maxRetries int
		retryDelay time.Duration
	}

	steps := []optimizationStep{
		{name: "indexes", fn: db.CreateIndexes, maxRetries: 2, retryDelay: 10 * time.Second},
		{name: "analyze", fn: db.Analyze, maxRetries: 2, retryDelay: 10 * time.Second},
		{name: "vacuum", fn: db.Vacuum, maxRetries: 3, retryDelay: 30 * time.Second},
	}

	// Execute each step with retry logic
	for _, step := range steps {
		log.Info().Msgf("running optimization step: %s", step.name)

		if err := db.SetOptimizationStep(step.name); err != nil {
			log.Error().Err(err).Msgf("failed to set optimization step to %s", step.name)
		}

		// Execute step with retry and exponential backoff
		var stepErr error
		for attempt := 0; attempt <= step.maxRetries; attempt++ {
			stepErr = step.fn()
			if stepErr == nil {
				break // Success
			}

			if attempt < step.maxRetries {
				delay := step.retryDelay * time.Duration(1<<attempt) // Exponential backoff
				log.Warn().Err(stepErr).Msgf("optimization step %s failed (attempt %d/%d), retrying in %v",
					step.name, attempt+1, step.maxRetries+1, delay)
				time.Sleep(delay)
			}
		}

		// Final check after all retries
		if stepErr != nil {
			log.Error().Err(stepErr).Msgf("optimization step %s failed after %d attempts", step.name, step.maxRetries+1)
			if setErr := db.SetOptimizationStatus("failed"); setErr != nil {
				log.Error().Err(setErr).Msg("failed to set optimization status to failed")
			}
			// Clear optimization step on failure
			if setErr := db.SetOptimizationStep(""); setErr != nil {
				log.Error().Err(setErr).Msg("failed to clear optimization step on failure")
			}
			return
		}

		log.Info().Msgf("optimization step %s completed", step.name)
	}

	// Mark as completed
	if err := db.SetOptimizationStatus("completed"); err != nil {
		log.Error().Err(err).Msg("failed to set optimization status to completed")
		return
	}
	// Clear optimization step on completion
	if err := db.SetOptimizationStep(""); err != nil {
		log.Error().Err(err).Msg("failed to clear optimization step on completion")
	}

	log.Info().Msg("background database optimization completed successfully")
}

// checkAndResumeOptimization checks if there's an incomplete optimization and resumes it.
func (db *MediaDB) checkAndResumeOptimization() {
	status, err := db.GetOptimizationStatus()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get optimization status during startup check")
		return
	}

	switch status {
	case "pending", "running":
		log.Info().Msgf("resuming incomplete optimization (status: %s)", status)
		go db.RunBackgroundOptimization()
	case "failed":
		log.Info().Msg("retrying failed optimization")
		go db.RunBackgroundOptimization()
	case "completed":
		// Nothing to do
	case "":
		// No optimization status set, this is normal for older databases
	default:
		log.Warn().Msgf("unknown optimization status: %s", status)
	}
}

// WaitForBackgroundOperations waits for all background operations to complete.
// This should be called before closing the database to ensure clean shutdown.
func (db *MediaDB) WaitForBackgroundOperations() {
	db.backgroundOps.Wait()
}
