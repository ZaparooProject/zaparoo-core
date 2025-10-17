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
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
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

// getSqliteConnParams constructs the SQLite connection string
func getSqliteConnParams() string {
	// Write-optimized WAL connection: defer checkpoints, keep dirty pages in RAM, larger cache
	// synchronous=NORMAL is safe with WAL and provides good performance
	return "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000" +
		"&_cache_size=-65536&_temp_store=MEMORY&_mmap_size=67108864" +
		"&_page_size=8192&_foreign_keys=ON&_wal_autocheckpoint=0&_cache_spill=OFF"
}

type MediaDB struct {
	clock                 clockwork.Clock
	ctx                   context.Context
	pl                    platforms.Platform
	batchInsertSystem     *BatchInserter
	batchInsertMediaTitle *BatchInserter
	stmtInsertMedia       *sql.Stmt
	tx                    *sql.Tx
	stmtInsertSystem      *sql.Stmt
	sql                   *sql.DB
	stmtInsertTag         *sql.Stmt
	stmtInsertTagType     *sql.Stmt
	batchInsertMediaTag   *BatchInserter
	batchInsertTagType    *BatchInserter
	batchInsertTag        *BatchInserter
	batchInsertMedia      *BatchInserter
	stmtInsertMediaTag    *sql.Stmt
	stmtInsertMediaTitle  *sql.Stmt
	dbPath                string
	backgroundOps         sync.WaitGroup
	vacuumRetryDelay      time.Duration
	analyzeRetryDelay     time.Duration
	batchSize             int
	sqlMu                 sync.RWMutex
	isOptimizing          atomic.Bool
	inTransaction         bool
}

// invalidationScope describes what data was changed to determine cache invalidation scope
type invalidationScope struct {
	SystemIDs  []string
	AllSystems bool
}

// invalidateCaches handles all cache invalidation in one place
func (db *MediaDB) invalidateCaches(scope invalidationScope) {
	// MediaCountCache: always nuke everything (queries are too complex to selectively invalidate)
	if err := db.InvalidateCountCache(); err != nil {
		log.Warn().Err(err).Msg("failed to invalidate media count cache")
	}

	// System-specific caches: invalidate all or by system
	if scope.AllSystems {
		// Full invalidation
		if _, err := db.sql.ExecContext(db.ctx, "DELETE FROM SystemTagsCache"); err != nil {
			log.Warn().Err(err).Msg("failed to invalidate all system tags cache")
		}
		if _, err := db.sql.ExecContext(db.ctx, "DELETE FROM SlugResolutionCache"); err != nil {
			log.Warn().Err(err).Msg("failed to invalidate all slug resolution cache")
		}
	} else if len(scope.SystemIDs) > 0 {
		// Granular invalidation by system
		systemsToInvalidate := make([]systemdefs.System, 0, len(scope.SystemIDs))
		for _, id := range scope.SystemIDs {
			if s, err := systemdefs.GetSystem(id); err == nil {
				systemsToInvalidate = append(systemsToInvalidate, *s)
			}
		}

		if len(systemsToInvalidate) > 0 {
			if err := db.InvalidateSystemTagsCache(db.ctx, systemsToInvalidate); err != nil {
				log.Warn().Err(err).Msg("failed to invalidate system tags cache for specific systems")
			}
		}

		// SlugResolutionCache: use per-system invalidation for better granularity
		if err := db.InvalidateSlugCacheForSystems(db.ctx, scope.SystemIDs); err != nil {
			log.Warn().Err(err).Msg("failed to invalidate slug resolution cache for specific systems")
		}
	}
}

func OpenMediaDB(ctx context.Context, pl platforms.Platform) (*MediaDB, error) {
	dbPath := filepath.Join(helpers.DataDir(pl), config.MediaDbFile)
	db := &MediaDB{
		sql:               nil,
		pl:                pl,
		dbPath:            dbPath,
		ctx:               ctx,
		clock:             clockwork.NewRealClock(),
		analyzeRetryDelay: 10 * time.Second,
		vacuumRetryDelay:  30 * time.Second,
		batchSize:         5000, // Default batch size for batch mode transactions
	}
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
	sqlInstance, err := sql.Open("sqlite3", dbPath+getSqliteConnParams())
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

	// Run WAL checkpoint on startup to clean up any orphaned WAL from crashes
	_, err = db.sql.ExecContext(db.ctx, "PRAGMA wal_checkpoint(TRUNCATE);")
	if err != nil {
		log.Warn().Err(err).Msg("failed to run WAL checkpoint on startup")
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

	err := sqlUpdateLastGenerated(db.ctx, db.sql)

	// Only invalidate cache if NOT in a transaction (transactions invalidate once on commit)
	if err == nil && !db.inTransaction {
		db.invalidateCaches(invalidationScope{AllSystems: true})
	}

	return err
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
	db.sqlMu.RLock()
	defer db.sqlMu.RUnlock()
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlSetIndexingStatus(db.ctx, db.sql, status)
}

func (db *MediaDB) GetIndexingStatus() (string, error) {
	db.sqlMu.RLock()
	defer db.sqlMu.RUnlock()
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
	err := sqlTruncate(db.ctx, db.sql)
	if err != nil {
		return err
	}

	// Invalidate all caches after full truncation
	db.invalidateCaches(invalidationScope{AllSystems: true})
	return nil
}

func (db *MediaDB) TruncateSystems(systemIDs []string) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	err := sqlTruncateSystems(db.ctx, db.sql, systemIDs)
	if err != nil {
		return err
	}

	// Invalidate caches for the affected systems
	db.invalidateCaches(invalidationScope{SystemIDs: systemIDs})
	return nil
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
	db.clock = clockwork.NewRealClock()
	db.analyzeRetryDelay = 10 * time.Second
	db.vacuumRetryDelay = 30 * time.Second
	db.batchSize = 5000 // Default batch size for testing

	// Initialize the database schema
	if err := db.Allocate(); err != nil {
		return err
	}

	// Initialize background operations state properly for tests
	// Reset atomic state to ensure clean start
	db.isOptimizing.Store(false)

	return nil
}

// SetDBPathForTesting explicitly sets the DB path so test memory DBs can reload.
func (db *MediaDB) SetDBPathForTesting(dbPath string) {
	db.dbPath = dbPath
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
	if db.stmtInsertTagType != nil {
		if closeErr := db.stmtInsertTagType.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close prepared statement: stmtInsertTagType")
		}
		db.stmtInsertTagType = nil
	}
	if db.stmtInsertMediaTag != nil {
		if closeErr := db.stmtInsertMediaTag.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close prepared statement: stmtInsertMediaTag")
		}
		db.stmtInsertMediaTag = nil
	}
}

// closeAllBatchInserters closes all batch inserters and sets them to nil
func (db *MediaDB) closeAllBatchInserters() {
	if db.batchInsertSystem != nil {
		if closeErr := db.batchInsertSystem.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertSystem")
		}
		db.batchInsertSystem = nil
	}
	if db.batchInsertMediaTitle != nil {
		if closeErr := db.batchInsertMediaTitle.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertMediaTitle")
		}
		db.batchInsertMediaTitle = nil
	}
	if db.batchInsertMedia != nil {
		if closeErr := db.batchInsertMedia.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertMedia")
		}
		db.batchInsertMedia = nil
	}
	if db.batchInsertTag != nil {
		if closeErr := db.batchInsertTag.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertTag")
		}
		db.batchInsertTag = nil
	}
	if db.batchInsertTagType != nil {
		if closeErr := db.batchInsertTagType.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertTagType")
		}
		db.batchInsertTagType = nil
	}
	if db.batchInsertMediaTag != nil {
		if closeErr := db.batchInsertMediaTag.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertMediaTag")
		}
		db.batchInsertMediaTag = nil
	}
}

// RollbackTransaction rolls back the current transaction and cleans up resources
func (db *MediaDB) RollbackTransaction() error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	if db.tx == nil {
		return nil // No active transaction
	}

	// Clean up prepared statements and batch inserters first
	db.closeAllPreparedStatements()
	db.closeAllBatchInserters()

	// Rollback the transaction
	err := db.tx.Rollback()
	db.tx = nil
	db.inTransaction = false // Clear transaction flag (no cache invalidation needed on rollback)
	if err != nil {
		return fmt.Errorf("failed to rollback transaction: %w", err)
	}

	return nil
}

// rollbackAndLogError helper function to handle rollback with error logging
// Note: This is called from BeginTransaction which already holds the mutex lock,
// so we perform the rollback directly without calling RollbackTransaction
func (db *MediaDB) rollbackAndLogError() {
	if db.tx == nil {
		return
	}

	// Clean up prepared statements and batch inserters first
	db.closeAllPreparedStatements()
	db.closeAllBatchInserters()

	// Rollback the transaction
	if rbErr := db.tx.Rollback(); rbErr != nil {
		log.Error().Err(rbErr).Msg("failed to rollback transaction during prepared statement setup")
	}
	db.tx = nil
	db.inTransaction = false
}

func (db *MediaDB) BeginTransaction(batchEnabled bool) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	if db.sql == nil {
		return ErrNullSQL
	}

	// Check if a transaction is already active
	if db.inTransaction {
		return errors.New("transaction already in progress")
	}

	// Begin a proper transaction
	tx, err := db.sql.BeginTx(db.ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	db.tx = tx

	// Use batch inserters if enabled, otherwise use prepared statements
	if batchEnabled {
		// Initialize batch inserters for multi-row bulk inserts.
		// IMPORTANT: Column order must match the regular INSERT statements including DBID.
		//
		// LBYL Pattern (Look Before You Leap):
		// - Application logic prevents duplicate attempts via in-memory scanState maps (primary defense)
		// - Database UNIQUE constraints provide final protection and fail-fast behavior
		// - NO INSERT OR IGNORE on tables with PKs used as FKs (would corrupt in-memory state)
		// - ONLY MediaTags uses INSERT OR IGNORE (link table, no dependent FKs on its PK)
		//
		// Why not INSERT OR IGNORE?
		// - Application pre-generates DBIDs from in-memory counters
		// - If INSERT OR IGNORE silently fails, the invalid DBID stays in scanState maps
		// - This corrupt DBID is then used as FK in child tables → FK constraint violations
		// - Better to fail fast with UNIQUE constraint error than continue with bad state
		if db.batchInsertSystem, err = NewBatchInserterWithOptions(db.ctx, tx, "Systems",
			[]string{"DBID", "SystemID", "Name"}, db.batchSize, false); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to create batch inserter for systems: %w", err)
		}

		if db.batchInsertMediaTitle, err = NewBatchInserterWithOptions(db.ctx, tx, "MediaTitles",
			[]string{"DBID", "SystemDBID", "Slug", "Name"}, db.batchSize, false); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to create batch inserter for media titles: %w", err)
		}

		if db.batchInsertMedia, err = NewBatchInserterWithOptions(db.ctx, tx, "Media",
			[]string{"DBID", "MediaTitleDBID", "SystemDBID", "Path"}, db.batchSize, false); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to create batch inserter for media: %w", err)
		}

		if db.batchInsertTag, err = NewBatchInserterWithOptions(db.ctx, tx, "Tags",
			[]string{"DBID", "TypeDBID", "Tag"}, db.batchSize, false); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to create batch inserter for tags: %w", err)
		}

		if db.batchInsertTagType, err = NewBatchInserterWithOptions(db.ctx, tx, "TagTypes",
			[]string{"DBID", "Type"}, db.batchSize, false); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to create batch inserter for tag types: %w", err)
		}

		// MediaTags uses INSERT OR IGNORE - it's a link table with no dependent foreign keys
		if db.batchInsertMediaTag, err = NewBatchInserterWithOptions(db.ctx, tx, "MediaTags",
			[]string{"MediaDBID", "TagDBID"}, db.batchSize, true); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to create batch inserter for media tags: %w", err)
		}

		// Set up foreign key dependencies to ensure proper flush order
		// IMPORTANT: When adding a new batch inserter, you MUST declare its dependencies here.
		// Failure to do so will result in foreign key constraint violations at runtime.
		// The validation below only checks for cycles, not for missing dependencies.
		//
		// Current dependency graph:
		// - MediaTitles depends on Systems
		db.batchInsertMediaTitle.SetDependencies(db.batchInsertSystem)
		// - Tags depends on TagTypes
		db.batchInsertTag.SetDependencies(db.batchInsertTagType)
		// - Media depends on MediaTitles (and transitively on Systems)
		db.batchInsertMedia.SetDependencies(db.batchInsertMediaTitle)
		// - MediaTags depends on both Media and Tags
		db.batchInsertMediaTag.SetDependencies(db.batchInsertMedia, db.batchInsertTag)
	} else {
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

		if db.stmtInsertTagType, err = tx.PrepareContext(db.ctx, insertTagTypeSQL); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to prepare insert tag type statement: %w", err)
		}

		if db.stmtInsertMediaTag, err = tx.PrepareContext(db.ctx, insertMediaTagSQL); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to prepare insert media tag statement: %w", err)
		}
	}

	// Validate batch inserter dependencies if batch mode is enabled
	if batchEnabled {
		if err := db.validateInserterDependencies(); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("invalid batch inserter dependencies: %w", err)
		}
	}

	// Set transaction flag to prevent excessive cache invalidations during batch operations
	db.inTransaction = true

	return nil
}

// validateInserterDependencies performs cycle detection on batch inserter dependencies.
// Returns an error if a cycle is detected in the dependency graph.
func (db *MediaDB) validateInserterDependencies() error {
	// Collect all batch inserters
	inserters := []*BatchInserter{
		db.batchInsertSystem,
		db.batchInsertMediaTitle,
		db.batchInsertMedia,
		db.batchInsertTag,
		db.batchInsertTagType,
		db.batchInsertMediaTag,
	}

	// Filter out nil inserters
	var validInserters []*BatchInserter
	for _, inserter := range inserters {
		if inserter != nil {
			validInserters = append(validInserters, inserter)
		}
	}

	// Perform DFS from each inserter to detect cycles
	visited := make(map[*BatchInserter]bool)
	visiting := make(map[*BatchInserter]bool)

	var dfs func(*BatchInserter) error
	dfs = func(node *BatchInserter) error {
		if visiting[node] {
			// Back edge detected - cycle found
			return fmt.Errorf("dependency cycle detected involving table %s", node.tableName)
		}
		if visited[node] {
			// Already processed this node
			return nil
		}

		visiting[node] = true
		for _, dep := range node.dependencies {
			if err := dfs(dep); err != nil {
				return err
			}
		}
		visiting[node] = false
		visited[node] = true
		return nil
	}

	// Run DFS from each node
	for _, inserter := range validInserters {
		if err := dfs(inserter); err != nil {
			return err
		}
	}

	return nil
}

func (db *MediaDB) CommitTransaction() error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	if db.tx == nil {
		return nil // No active transaction
	}

	// Flush all batch inserters before committing (if any were created)
	// Check if batch inserters exist rather than relying on a mode flag
	if db.batchInsertSystem != nil {
		db.closeAllBatchInserters()
	} else {
		// Clean up prepared statements
		db.closeAllPreparedStatements()
	}

	// Commit the transaction
	err := db.tx.Commit()
	if err != nil {
		// Try to rollback and combine errors if both fail
		if rbErr := db.tx.Rollback(); rbErr != nil {
			db.tx = nil
			db.inTransaction = false
			return fmt.Errorf("commit failed: %w; rollback also failed: %w", err, rbErr)
		}
		db.tx = nil
		db.inTransaction = false
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Transaction committed successfully - invalidate cache once and clear transaction flag
	db.tx = nil
	db.inTransaction = false

	// Invalidate all caches after successful transaction commit
	db.invalidateCaches(invalidationScope{AllSystems: true})

	// Run manual WAL checkpoint after commit to keep WAL size bounded during indexing
	// Use TRUNCATE to reset the WAL file after commit, keeping reads fast
	if _, chkErr := db.sql.ExecContext(db.ctx, "PRAGMA wal_checkpoint(TRUNCATE);"); chkErr != nil {
		log.Warn().Err(chkErr).Msg("failed to run WAL checkpoint after transaction commit")
	}

	return nil
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

func (db *MediaDB) insertTagTypeWithPreparedStmt(row database.TagType) (database.TagType, error) {
	return sqlInsertTagTypeWithPreparedStmt(db.ctx, db.stmtInsertTagType, row)
}

func (db *MediaDB) insertMediaTagWithPreparedStmt(row database.MediaTag) (database.MediaTag, error) {
	return sqlInsertMediaTagWithPreparedStmt(db.ctx, db.stmtInsertMediaTag, row)
}

func (*MediaDB) CreateIndexes() error {
	// Indexes are now created by migrations, this is a no-op
	return nil
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

func (db *MediaDB) SearchMediaWithFilters(
	ctx context.Context,
	filters *database.SearchFilters,
) ([]database.SearchResultWithCursor, error) {
	if db.sql == nil {
		return make([]database.SearchResultWithCursor, 0), ErrNullSQL
	}

	// Slugify query words to match stored slugs
	qWords := strings.Fields(filters.Query)
	slugifiedWords := make([]string, 0, len(qWords))
	hasNonLatinWords := false
	for _, word := range qWords {
		slug := slugs.SlugifyString(word)
		if slug != "" {
			slugifiedWords = append(slugifiedWords, slug)
		} else if word != "" {
			hasNonLatinWords = true
		}
	}

	// Try slug-based search first
	results, err := sqlSearchMediaWithFilters(
		ctx, db.sql, filters.Systems, slugifiedWords, filters.Tags,
		filters.Letter, filters.Cursor, filters.Limit, false)

	// Fallback to raw name search if we have non-Latin words that slugified to empty
	if err == nil && len(results) == 0 && hasNonLatinWords {
		return sqlSearchMediaWithFilters(
			ctx, db.sql, filters.Systems, qWords, filters.Tags, filters.Letter, filters.Cursor,
			filters.Limit, true)
	}

	return results, err
}

func (db *MediaDB) SearchMediaBySlug(
	ctx context.Context, systemID string, slug string, tags []database.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	if db.sql == nil {
		return make([]database.SearchResultWithCursor, 0), ErrNullSQL
	}
	return sqlSearchMediaBySlug(ctx, db.sql, systemID, slug, tags)
}

func (db *MediaDB) SearchMediaBySlugPrefix(
	ctx context.Context, systemID string, slugPrefix string, tags []database.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	if db.sql == nil {
		return make([]database.SearchResultWithCursor, 0), ErrNullSQL
	}
	return sqlSearchMediaBySlugPrefix(ctx, db.sql, systemID, slugPrefix, tags)
}

func (db *MediaDB) GetAllSlugsForSystem(ctx context.Context, systemID string) ([]string, error) {
	if db.sql == nil {
		return make([]string, 0), ErrNullSQL
	}
	return sqlGetAllSlugsForSystem(ctx, db.sql, systemID)
}

func (db *MediaDB) GetTags(ctx context.Context, systems []systemdefs.System) ([]database.TagInfo, error) {
	if db.sql == nil {
		return make([]database.TagInfo, 0), ErrNullSQL
	}
	return sqlGetTags(ctx, db.sql, systems)
}

// GetAllUsedTags returns all tags that are actually in use (have media associated)
// This is optimized for the "all systems" case and avoids expensive system filtering
func (db *MediaDB) GetAllUsedTags(ctx context.Context) ([]database.TagInfo, error) {
	if db.sql == nil {
		return make([]database.TagInfo, 0), ErrNullSQL
	}
	return sqlGetAllUsedTags(ctx, db.sql)
}

// PopulateSystemTagsCache rebuilds the cache table for fast tag lookups by system
// This should be called after media indexing completes
func (db *MediaDB) PopulateSystemTagsCache(ctx context.Context) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlPopulateSystemTagsCache(ctx, db.sql)
}

// PopulateSystemTagsCacheForSystems rebuilds cache for specific systems only
// Used for incremental cache updates after individual system changes
func (db *MediaDB) PopulateSystemTagsCacheForSystems(ctx context.Context, systems []systemdefs.System) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlPopulateSystemTagsCacheForSystems(ctx, db.sql, systems)
}

// GetSystemTagsCached retrieves tags for specific systems using the cache table
// Falls back to the optimized subquery approach if cache is empty
func (db *MediaDB) GetSystemTagsCached(ctx context.Context, systems []systemdefs.System) ([]database.TagInfo, error) {
	if db.sql == nil {
		return make([]database.TagInfo, 0), ErrNullSQL
	}

	// Try cached approach first
	tags, err := sqlGetSystemTagsCached(ctx, db.sql, systems)
	if err != nil {
		log.Warn().Err(err).Msg("failed to get cached tags, falling back to optimized query")
		// Fallback to optimized subquery approach
		return sqlGetTags(ctx, db.sql, systems)
	}

	// If cache is empty (no results), auto-populate for requested systems (self-healing)
	if len(tags) == 0 {
		log.Debug().Int("system_count", len(systems)).Msg("cache miss, populating for requested systems")

		// Self-healing: populate cache for requested systems (best effort)
		if populateErr := db.PopulateSystemTagsCacheForSystems(ctx, systems); populateErr != nil {
			log.Warn().Err(populateErr).Msg("failed to populate cache, using direct query")
		} else {
			log.Debug().Int("system_count", len(systems)).Msg("successfully populated cache for systems")
		}

		// Always fall back to direct SQL query when cache returns no results
		// This ensures we return data even if cache population fails or systems have no tags
		return sqlGetTags(ctx, db.sql, systems)
	}

	return tags, nil
}

// InvalidateSystemTagsCache removes cache entries for specific systems
// Useful for incremental cache updates when only certain systems change
// If no systems are provided, this is a no-op and returns success.
func (db *MediaDB) InvalidateSystemTagsCache(ctx context.Context, systems []systemdefs.System) error {
	if db.sql == nil {
		return ErrNullSQL
	}

	if len(systems) == 0 {
		return nil // No-op for empty systems list
	}

	return sqlInvalidateSystemTagsCache(ctx, db.sql, systems)
}

func (db *MediaDB) SearchMediaPathGlob(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	// TODO: glob pattern matching unclear on some patterns
	// query == path like with possible *
	var nullResults []database.SearchResult
	if db.sql == nil {
		return nullResults, ErrNullSQL
	}
	// Search terms are slugified to match the database's Slug field.
	// This provides fuzzy matching: spaces/punctuation are ignored,
	// making searches more forgiving (e.g., "mega man" finds "Megaman")
	var parts []string
	for _, part := range strings.Split(query, "*") {
		if part != "" {
			// Slugify search parts to match how titles are stored
			parts = append(parts, slugs.SlugifyString(part))
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
func (db *MediaDB) SystemIndexed(system *systemdefs.System) bool {
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

	return sqlRandomGame(db.ctx, db.sql, &system)
}

// RandomGameWithQuery returns a random game matching the specified MediaQuery.
func (db *MediaDB) RandomGameWithQuery(query *database.MediaQuery) (database.SearchResult, error) {
	var result database.SearchResult
	if db.sql == nil {
		return result, ErrNullSQL
	}

	// Check cache first
	if stats, found := db.GetCachedStats(db.ctx, query); found {
		if stats.Count == 0 {
			return result, sql.ErrNoRows
		}
		// Use cached stats to generate random selection
		return db.randomGameWithStats(query, stats)
	}

	// Cache miss - use the full SQL implementation and cache the stats
	result, stats, err := sqlRandomGameWithQueryAndStats(db.ctx, db.sql, query)
	if err != nil {
		return result, err
	}

	// Cache the stats for future use (best effort - don't fail if caching fails)
	if cacheErr := db.SetCachedStats(db.ctx, query, stats); cacheErr != nil {
		log.Warn().Err(cacheErr).Msg("failed to cache media query stats")
	}

	return result, nil
}

// GetTotalMediaCount returns the total number of media entries in the database.
func (db *MediaDB) GetTotalMediaCount() (int, error) {
	if db.sql == nil {
		return 0, ErrNullSQL
	}
	return sqlGetTotalMediaCount(db.ctx, db.sql)
}

// MediaStats represents cached statistics for a media query
type MediaStats struct {
	Count   int
	MinDBID int64
	MaxDBID int64
}

// GetCachedStats returns cached statistics for the given media query, if available.
// Returns the stats and true if found, or empty stats and false if not cached.
func (db *MediaDB) GetCachedStats(ctx context.Context, query *database.MediaQuery) (MediaStats, bool) {
	if db.sql == nil {
		return MediaStats{}, false
	}

	queryHash, err := db.generateQueryHash(query)
	if err != nil {
		log.Warn().Err(err).Msg("failed to generate query hash for cache lookup")
		return MediaStats{}, false
	}

	var stats MediaStats
	err = db.sql.QueryRowContext(ctx,
		"SELECT Count, MinDBID, MaxDBID FROM MediaCountCache WHERE QueryHash = ?",
		queryHash).Scan(&stats.Count, &stats.MinDBID, &stats.MaxDBID)
	if errors.Is(err, sql.ErrNoRows) {
		return MediaStats{}, false
	}
	if err != nil {
		log.Warn().Err(err).Str("queryHash", queryHash).Msg("failed to get cached stats")
		return MediaStats{}, false
	}

	return stats, true
}

// randomGameWithStats generates a random game selection using cached statistics.
func (db *MediaDB) randomGameWithStats(query *database.MediaQuery, stats MediaStats) (database.SearchResult, error) {
	var row database.SearchResult

	// Generate random DBID within the range
	randomOffset, err := helpers.RandomInt(int(stats.MaxDBID - stats.MinDBID + 1))
	if err != nil {
		return row, fmt.Errorf("failed to generate random DBID offset: %w", err)
	}
	targetDBID := stats.MinDBID + int64(randomOffset)

	// Use shared helper to build WHERE clause and arguments
	whereClause, args := buildMediaQueryWhereClause(query)

	// Get the first media item with DBID >= targetDBID
	//nolint:gosec // whereClause is built from safe conditions, no user input
	selectQuery := fmt.Sprintf(`
		SELECT Systems.SystemID, Media.Path
		FROM Media
		INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
		%s AND Media.DBID >= ?
		ORDER BY Media.DBID ASC
		LIMIT 1
	`, whereClause)

	args = append(args, targetDBID)
	err = db.sql.QueryRowContext(db.ctx, selectQuery, args...).Scan(
		&row.SystemID,
		&row.Path,
	)
	if errors.Is(err, sql.ErrNoRows) {
		// If no row found >= targetDBID (gap in DBID sequence), try wrapping to beginning
		selectQuery = fmt.Sprintf(`
			SELECT Systems.SystemID, Media.Path
			FROM Media
			INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
			INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
			%s AND Media.DBID < ?
			ORDER BY Media.DBID DESC
			LIMIT 1
		`, whereClause)
		args[len(args)-1] = targetDBID // Replace the last argument
		err = db.sql.QueryRowContext(db.ctx, selectQuery, args...).Scan(
			&row.SystemID,
			&row.Path,
		)
	}
	if err != nil {
		return row, fmt.Errorf("failed to scan random game row using cached stats: %w", err)
	}
	row.Name = helpers.FilenameFromPath(row.Path)
	return row, nil
}

// SetCachedStats stores statistics for the given media query in the cache.
func (db *MediaDB) SetCachedStats(ctx context.Context, query *database.MediaQuery, stats MediaStats) error {
	if db.sql == nil {
		return ErrNullSQL
	}

	queryHash, err := db.generateQueryHash(query)
	if err != nil {
		return fmt.Errorf("failed to generate query hash: %w", err)
	}

	queryParams, err := json.Marshal(query)
	if err != nil {
		return fmt.Errorf("failed to marshal query params: %w", err)
	}

	_, err = db.sql.ExecContext(ctx, `
		INSERT OR REPLACE INTO MediaCountCache (QueryHash, QueryParams, Count, MinDBID, MaxDBID, LastUpdated)
		VALUES (?, ?, ?, ?, ?, ?)
	`, queryHash, string(queryParams), stats.Count, stats.MinDBID, stats.MaxDBID, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("failed to cache stats: %w", err)
	}

	return nil
}

// InvalidateCountCache clears all cached media counts.
// This should be called after any operation that changes the media database content.
func (db *MediaDB) InvalidateCountCache() error {
	if db.sql == nil {
		return ErrNullSQL
	}

	_, err := db.sql.ExecContext(db.ctx, "DELETE FROM MediaCountCache")
	if err != nil {
		return fmt.Errorf("failed to invalidate count cache: %w", err)
	}
	return nil
}

// generateQueryHash creates a consistent hash for a MediaQuery for cache key purposes.
func (*MediaDB) generateQueryHash(query *database.MediaQuery) (string, error) {
	// Normalize the query to ensure consistent hashing
	normalized := database.MediaQuery{
		Systems:    make([]string, len(query.Systems)),
		PathGlob:   strings.ToLower(strings.TrimSpace(query.PathGlob)),
		PathPrefix: strings.ToLower(strings.TrimSpace(query.PathPrefix)),
		Tags:       make([]database.TagFilter, len(query.Tags)),
	}

	// Sort systems for consistent ordering
	copy(normalized.Systems, query.Systems)
	sort.Strings(normalized.Systems)

	// Copy and sort tags for consistent ordering
	copy(normalized.Tags, query.Tags)
	sort.Slice(normalized.Tags, func(i, j int) bool {
		if normalized.Tags[i].Type != normalized.Tags[j].Type {
			return normalized.Tags[i].Type < normalized.Tags[j].Type
		}
		return normalized.Tags[i].Value < normalized.Tags[j].Value
	})

	// Marshal to JSON with consistent ordering
	queryBytes, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("failed to marshal normalized query: %w", err)
	}

	// Generate SHA256 hash
	hash := sha256.Sum256(queryBytes)
	return fmt.Sprintf("%x", hash), nil
}

func (db *MediaDB) FindSystem(row database.System) (database.System, error) {
	return sqlFindSystem(db.ctx, db.sql, row)
}

func (db *MediaDB) FindSystemBySystemID(systemID string) (database.System, error) {
	return sqlFindSystemBySystemID(db.ctx, db.sql, systemID)
}

func (db *MediaDB) InsertSystem(row database.System) (database.System, error) {
	var result database.System
	var err error

	// Use batch inserter if available
	if db.batchInsertSystem != nil {
		err = db.batchInsertSystem.Add(row.DBID, row.SystemID, row.Name)
		if err != nil {
			return row, fmt.Errorf("failed to add system to batch: %w", err)
		}
		// Return row as-is (DBID is already set by caller)
		return row, nil
	}

	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertSystem != nil {
		result, err = db.insertSystemWithPreparedStmt(row)
	} else {
		result, err = sqlInsertSystem(db.ctx, db.sql, row)
	}

	// Only invalidate cache if NOT in a transaction (transactions invalidate once on commit)
	if err == nil && !db.inTransaction {
		db.invalidateCaches(invalidationScope{SystemIDs: []string{result.SystemID}})
	}

	return result, err
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
	var result database.MediaTitle
	var err error

	// Use batch inserter if available
	if db.batchInsertMediaTitle != nil {
		err = db.batchInsertMediaTitle.Add(row.DBID, row.SystemDBID, row.Slug, row.Name)
		if err != nil {
			return row, fmt.Errorf("failed to add media title to batch: %w", err)
		}
		// Return row as-is (DBID is already set by caller)
		return row, nil
	}

	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertMediaTitle != nil {
		result, err = db.insertMediaTitleWithPreparedStmt(row)
	} else {
		result, err = sqlInsertMediaTitle(db.ctx, db.sql, row)
	}

	// Only invalidate cache if NOT in a transaction (transactions invalidate once on commit)
	if err == nil && !db.inTransaction {
		db.invalidateCaches(invalidationScope{AllSystems: true})
	}

	return result, err
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
	var result database.Media
	var err error

	// Use batch inserter if available
	if db.batchInsertMedia != nil {
		err = db.batchInsertMedia.Add(row.DBID, row.MediaTitleDBID, row.SystemDBID, row.Path)
		if err != nil {
			return row, fmt.Errorf("failed to add media to batch: %w", err)
		}
		// Return row as-is (DBID is already set by caller)
		return row, nil
	}

	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertMedia != nil {
		result, err = db.insertMediaWithPreparedStmt(row)
	} else {
		result, err = sqlInsertMedia(db.ctx, db.sql, row)
	}

	// Only invalidate cache if NOT in a transaction (transactions invalidate once on commit)
	if err == nil && !db.inTransaction {
		db.invalidateCaches(invalidationScope{AllSystems: true})
	}

	return result, err
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

// InsertTagType inserts a new TagType into the database.
func (db *MediaDB) InsertTagType(row database.TagType) (database.TagType, error) {
	var result database.TagType
	var err error

	// Use batch inserter if available
	if db.batchInsertTagType != nil {
		err = db.batchInsertTagType.Add(row.DBID, row.Type)
		if err != nil {
			return row, fmt.Errorf("failed to add tag type to batch: %w", err)
		}
		// Return row as-is (DBID is already set by caller)
		return row, nil
	}

	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertTagType != nil {
		result, err = db.insertTagTypeWithPreparedStmt(row)
	} else {
		result, err = sqlInsertTagType(db.ctx, db.sql, row)
	}

	// Only invalidate cache if NOT in a transaction (transactions invalidate once on commit)
	if err == nil && !db.inTransaction {
		db.invalidateCaches(invalidationScope{AllSystems: true})
	}

	return result, err
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
	var result database.Tag
	var err error

	// Use batch inserter if available
	if db.batchInsertTag != nil {
		err = db.batchInsertTag.Add(row.DBID, row.TypeDBID, row.Tag)
		if err != nil {
			return row, fmt.Errorf("failed to add tag to batch: %w", err)
		}
		// Return row as-is (DBID is already set by caller)
		return row, nil
	}

	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertTag != nil {
		result, err = db.insertTagWithPreparedStmt(row)
	} else {
		result, err = sqlInsertTag(db.ctx, db.sql, row)
	}

	// Only invalidate cache if NOT in a transaction (transactions invalidate once on commit)
	if err == nil && !db.inTransaction {
		db.invalidateCaches(invalidationScope{AllSystems: true})
	}

	return result, err
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
	var result database.MediaTag
	var err error

	// Use batch inserter if available
	if db.batchInsertMediaTag != nil {
		err = db.batchInsertMediaTag.Add(row.MediaDBID, row.TagDBID)
		if err != nil {
			return row, fmt.Errorf("failed to add media tag to batch: %w", err)
		}
		// Note: DBID not available in batch mode, caller must handle differently
		return row, nil
	}

	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertMediaTag != nil {
		result, err = db.insertMediaTagWithPreparedStmt(row)
	} else {
		result, err = sqlInsertMediaTag(db.ctx, db.sql, row)
	}

	// Only invalidate cache if NOT in a transaction (transactions invalidate once on commit)
	if err == nil && !db.inTransaction {
		db.invalidateCaches(invalidationScope{AllSystems: true})
	}

	return result, err
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

func (db *MediaDB) GetAllTags() ([]database.Tag, error) {
	return sqlGetAllTags(db.ctx, db.sql)
}

func (db *MediaDB) GetAllTagTypes() ([]database.TagType, error) {
	return sqlGetAllTagTypes(db.ctx, db.sql)
}

// GetTitlesWithSystems retrieves all media titles with their associated system IDs using a JOIN query.
// This is more efficient than fetching titles and systems separately and matching them in application code.
func (db *MediaDB) GetTitlesWithSystems() ([]database.TitleWithSystem, error) {
	return sqlGetTitlesWithSystems(db.ctx, db.sql)
}

// GetMediaWithFullPath retrieves all media with their associated title and system information using JOIN queries.
// This eliminates the need for nested loops to match media with titles and systems.
func (db *MediaDB) GetMediaWithFullPath() ([]database.MediaWithFullPath, error) {
	return sqlGetMediaWithFullPath(db.ctx, db.sql)
}

// GetSystemsExcluding retrieves all systems except those in the excludeSystemIDs list.
// This is optimized for selective indexing to avoid loading data for systems being reindexed.
func (db *MediaDB) GetSystemsExcluding(excludeSystemIDs []string) ([]database.System, error) {
	return sqlGetSystemsExcluding(db.ctx, db.sql, excludeSystemIDs)
}

// GetTitlesWithSystemsExcluding retrieves all media titles with their associated system IDs,
// excluding those belonging to systems in the excludeSystemIDs list.
// This is optimized for selective indexing to avoid loading data for systems being reindexed.
func (db *MediaDB) GetTitlesWithSystemsExcluding(excludeSystemIDs []string) ([]database.TitleWithSystem, error) {
	return sqlGetTitlesWithSystemsExcluding(db.ctx, db.sql, excludeSystemIDs)
}

// GetMediaWithFullPathExcluding retrieves all media with their associated title and system information,
// excluding those belonging to systems in the excludeSystemIDs list.
// This is optimized for selective indexing to avoid loading data for systems being reindexed.
func (db *MediaDB) GetMediaWithFullPathExcluding(excludeSystemIDs []string) ([]database.MediaWithFullPath, error) {
	return sqlGetMediaWithFullPathExcluding(db.ctx, db.sql, excludeSystemIDs)
}

// RunBackgroundOptimization performs database optimization operations in the background.
// This includes creating indexes, running ANALYZE, and vacuuming the database.
// It can be safely interrupted and resumed later.
func (db *MediaDB) RunBackgroundOptimization(statusCallback func(optimizing bool)) {
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
		// Notify that optimization has failed
		if statusCallback != nil {
			statusCallback(false)
		}
		return
	}

	log.Info().Msg("starting background database optimization")

	// Set status to running
	if err := db.SetOptimizationStatus(IndexingStatusRunning); err != nil {
		log.Error().Err(err).Msg("failed to set optimization status to running")
		// Notify that optimization has failed to start
		if statusCallback != nil {
			statusCallback(false)
		}
		return
	}

	// Notify that optimization has started
	if statusCallback != nil {
		statusCallback(true)
	}

	// Define optimization steps
	type optimizationStep struct {
		fn         func() error
		name       string
		maxRetries int
		retryDelay time.Duration
	}

	steps := []optimizationStep{
		{name: "analyze", fn: db.Analyze, maxRetries: 2, retryDelay: db.analyzeRetryDelay},
		{name: "vacuum", fn: db.Vacuum, maxRetries: 3, retryDelay: db.vacuumRetryDelay},
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
				db.clock.Sleep(delay)
			}
		}

		// Final check after all retries
		if stepErr != nil {
			log.Error().Err(stepErr).Msgf("optimization step %s failed after %d attempts", step.name, step.maxRetries+1)
			if setErr := db.SetOptimizationStatus(IndexingStatusFailed); setErr != nil {
				log.Error().Err(setErr).Msg("failed to set optimization status to failed")
			}
			// Clear optimization step on failure
			if setErr := db.SetOptimizationStep(""); setErr != nil {
				log.Error().Err(setErr).Msg("failed to clear optimization step on failure")
			}

			// Notify that optimization has failed
			if statusCallback != nil {
				statusCallback(false)
			}
			// Reset optimization flag
			db.isOptimizing.Store(false)
			return
		}

		log.Info().Msgf("optimization step %s completed", step.name)
	}

	// Mark as completed
	if err := db.SetOptimizationStatus(IndexingStatusCompleted); err != nil {
		log.Error().Err(err).Msg("failed to set optimization status to completed")
		return
	}
	// Clear optimization step on completion
	if err := db.SetOptimizationStep(""); err != nil {
		log.Error().Err(err).Msg("failed to clear optimization step on completion")
	}

	// Notify that optimization has completed
	if statusCallback != nil {
		statusCallback(false)
	}

	// Reset optimization flag
	db.isOptimizing.Store(false)

	log.Info().Msg("background database optimization completed")
}

// WaitForBackgroundOperations waits for all background operations to complete.
// This should be called before closing the database to ensure clean shutdown.
func (db *MediaDB) WaitForBackgroundOperations() {
	db.backgroundOps.Wait()
}

// GetLaunchCommandForMedia generates a title-based launch command for the given media.
func (db *MediaDB) GetLaunchCommandForMedia(ctx context.Context, systemID, path string) (string, error) {
	db.sqlMu.RLock()
	defer db.sqlMu.RUnlock()

	if db.sql == nil {
		return "", ErrNullSQL
	}

	return sqlGetLaunchCommandForMedia(ctx, db.sql, systemID, path)
}
