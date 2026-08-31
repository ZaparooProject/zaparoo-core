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
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/perfmetrics"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var ErrNullSQL = errors.New("MediaDB is not connected")

// ErrIndexingInProgress is returned by CleanMediaOrphans when media indexing is
// currently running or pending; deletions during that window could corrupt
// in-flight scanner state.
var ErrIndexingInProgress = errors.New("indexing is in progress")

// ErrOptimizationInProgress is returned by CleanMediaOrphans when background
// database optimisation is active.
var ErrOptimizationInProgress = errors.New("background optimisation is in progress")

// ErrTransactionActive is returned by CleanMediaOrphans when a batch
// transaction is currently open.
var ErrTransactionActive = errors.New("a transaction is currently active")

// ErrTransactionRequired is returned by ReconcileStagedSystem when called
// outside the scanner's open batch transaction.
var ErrTransactionRequired = errors.New("reconciling staged media requires an open transaction")

// Indexing status constants
const (
	IndexingStatusRunning   = "running"
	IndexingStatusPending   = "pending"
	IndexingStatusCompleted = "completed"
	IndexingStatusFailed    = "failed"
	IndexingStatusCancelled = "cancelled"
	IndexingStatusCorrupt   = "corrupt"
)

// defaultSlugSearchLimit is the max results returned by slug-based search methods.
const defaultSlugSearchLimit = 50

// maxSelectiveInvalidationSystems avoids huge per-commit IN clauses and debug
// logs during full-library indexing while preserving selective reindexing wins.
const maxSelectiveInvalidationSystems = 32

const (
	mediaStatsCacheWriteTimeout = 100 * time.Millisecond
	mediaCountCacheMaxEntries   = 256
)

// mediaWALCheckpointThreshold bounds WAL growth during indexing. A batch commit
// checkpoints (TRUNCATE) once the WAL has grown past this size, so a long
// multi-system index cannot accumulate an unbounded WAL — and the page-cache /
// shmem pressure that rides on it — before the post-index optimization checkpoint.
// With automatic checkpointing disabled during indexing (SetWALAutoCheckpoint(0),
// see configureIndexingPragmas), this is now the only thing bounding WAL size, so
// it must actually be reachable within a handful of systems rather than sit at a
// size a real index rarely approaches.
//
// 8 MiB is measured, not provisional. On the #1279 MiSTer test device (229,553
// media across 130 systems on SD) it fires on the larger systems and not on the
// small ones, which is the intended shape: a system reaching ~10-13 MiB of WAL
// pays a 3.2-14.1 s TRUNCATE, while the long tail of small systems never
// reaches the threshold and pays nothing. Raising it would concentrate that
// cost into rarer, longer stalls and hold more dirty WAL against page cache on
// a 1 GB device; lowering it would make small-system commits start paying the
// SD/exFAT checkpoint cost they currently avoid.
//
// A var (not const) only so tests can change it; production never mutates it.
var mediaWALCheckpointThreshold int64 = 8 * 1024 * 1024

// Connection pool sizing. Two connections (one writer, one reader) is the
// steady-state balance for low-memory devices, but while indexing runs the
// writer effectively owns a connection full-time, so the pool widens by one
// to keep a search and a browse from queueing behind each other.
const (
	baseMaxOpenConns         = 2
	indexingMaxOpenConns     = 3
	defaultConnCacheSize     = "-8192"
	defaultConnTempStore     = "FILE"
	defaultWALAutoCheckpoint = 1000
	connectionAcquireTimeout = 5 * time.Second
)

// getSqliteConnParams constructs the SQLite connection string. MediaDB uses
// synchronous=NORMAL in WAL mode: SQLite documents this pairing as durable
// against application crashes and normal power loss (only a torn/interrupted
// write to the WAL during an OS crash or hardware fault can lose the most
// recent transactions). synchronous=FULL was tried first to guard against
// that, but a confirmed corrupt MediaDB from the field turned out to be
// zeroed pages from storage ignoring fsync entirely (a torn write) - a mode
// FULL does not protect against - so it bought nothing while its per-commit
// fsync produced multi-second stalls during indexing. Recovery is now a
// rebuild, and MediaDB rows are rebuildable, so NORMAL's small window of
// last-transaction loss on power loss is an acceptable trade. UserDB keeps
// synchronous=FULL since it holds non-rebuildable user data.
func getSqliteConnParams() string {
	return "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000" +
		"&_cache_size=" + defaultConnCacheSize + "&_temp_store=" + defaultConnTempStore + "&_mmap_size=0" +
		"&_foreign_keys=ON&_txlock=immediate"
}

type mediaDBIDBounds struct {
	first int64
	last  int64
}

type mediaSearchBoundsLoad struct {
	done       chan struct{}
	err        error
	bounds     mediaDBIDBounds
	generation uint64
	found      bool
}

type systemMediaCountsSnapshot struct {
	counts     []database.SystemMediaCount
	generation uint64
}

type MediaDB struct {
	clock                   clockwork.Clock
	ctx                     context.Context
	pl                      platforms.Platform
	scrapeImageSystems      map[string]struct{}
	batchInsertTagType      *BatchInserter
	stmtInsertMedia         *sql.Stmt
	tx                      *sql.Tx
	txConn                  *sql.Conn
	stmtInsertSystem        *sql.Stmt
	sql                     database.Conn
	stmtInsertTag           *sql.Stmt
	stmtInsertTagType       *sql.Stmt
	batchInsertMediaTag     *BatchInserter
	inMemoryTagCache        atomic.Pointer[tagCache]
	mediaWriteArbiter       atomic.Pointer[database.MediaWriteArbiter]
	batchInsertTag          *BatchInserter
	mediaSearchBounds       map[int64]mediaDBIDBounds
	mediaSearchBoundsLoads  map[int64]*mediaSearchBoundsLoad
	stmtInsertMediaTag      *sql.Stmt
	batchInsertMediaTitle   *BatchInserter
	stmtInsertMediaTitle    *sql.Stmt
	batchInsertMedia        *BatchInserter
	batchInsertSystem       *BatchInserter
	batchInsertScanTag      *BatchInserter
	slugSearchCache         atomic.Pointer[SlugSearchCache]
	systemMediaCountsCache  atomic.Pointer[systemMediaCountsSnapshot]
	batchInsertScanStage    *BatchInserter
	batchInsertScanProperty *BatchInserter
	dbPath                  string
	backgroundOps           sync.WaitGroup
	backgroundOpsCount      atomic.Int64
	vacuumRetryDelay        time.Duration
	analyzeRetryDelay       time.Duration
	mediaSearchBoundsGen    uint64
	batchSize               int
	walAutoCheckpoint       atomic.Int64
	systemMediaCountsGen    atomic.Uint64
	backgroundOpsMu         syncutil.RWMutex
	mediaSearchBoundsMu     syncutil.RWMutex
	sqlMu                   syncutil.RWMutex
	scrapeImageChangesMu    syncutil.Mutex
	recreating              atomic.Bool
	browseCacheRebuilding   atomic.Bool
	needsIndexRebuild       atomic.Bool
	isOptimizing            atomic.Bool
	indexingCacheBoost      atomic.Bool
	walAutoCheckpointSet    atomic.Bool
	inTransaction           bool
	browseCacheDirty        bool
	utilityTagCacheDirty    bool
	mediaSearchBoundsDirty  bool
	scrapeImageChangesAll   bool
}

// sqlQueryable is the subset of *sql.DB and *sql.Tx needed by SQL helpers.
// Passing db.tx when a transaction is active avoids acquiring a second
// connection from the pool (which would deadlock with SetMaxOpenConns(1)).
type sqlQueryable interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// conn returns the active transaction if one exists, otherwise the raw pool.
func (db *MediaDB) conn() sqlQueryable {
	if db.tx != nil {
		return db.tx
	}
	return db.sql.Load()
}

// readConn returns the pool handle for a read that must not wait on the writer.
//
// sqlMu exists to guard db.tx and the batch inserters — the fields conn() reads
// — not the connection handle, which is an atomic pointer swapped without the
// mutex by Recreate (see the Conn invariant in pkg/database/conn.go). A read
// that goes straight to the pool therefore gains nothing from RLock, and pays
// for it: CommitTransactionWithOptions holds sqlMu exclusively for the whole
// commit, and Go's RWMutex is writer-preferring, so every such read queued
// behind it for the commit's full duration.
//
// Measured on the #1279 device mid-index: a media.scrape.status call starting
// at 13:25:44 took 25,030 ms, and the C64 batch commit that started at the same
// second took 25,123 ms — the read was blocked for exactly the commit. With the
// API timeout at 30 s that left no headroom. Browse already reads this way
// (beginBrowse), so this only brings the metadata reads into line with it.
//
// Racing a Recreate is the documented behaviour of Conn: the reader sees either
// the old handle, which fails cleanly with "database is closed", or the new one.
func (db *MediaDB) readConn() (*sql.DB, error) {
	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return nil, ErrNullSQL
	}
	return sqlDB, nil
}

// invalidationScope describes what data was changed to determine cache invalidation scope
type invalidationScope struct {
	SystemIDs               []string
	AllSystems              bool
	PreserveSlugSearchCache bool
	PreserveTagCache        bool
	UtilityTagDBIDsChanged  bool
	MediaRowsChanged        bool
}

// invalidateCaches handles all cache invalidation in one place.
func (db *MediaDB) invalidateCaches(scope invalidationScope) {
	// Rebuilding the tag list aggregates every MediaTags and MediaTitleTags
	// row (731,332 on the #1279 device) into ~25,000 tags. That is affordable
	// once after a commit, but indexing commits per batch, so media.tags then
	// pays it on every call and exceeded the 30s API timeout on device. During
	// indexing the last-good list is served instead and the end-of-run rebuild
	// republishes it — the same trade already made for the slug search cache.
	if !scope.PreserveTagCache {
		db.inMemoryTagCache.Store(nil)
	}
	db.systemMediaCountsCache.Store(nil)
	db.systemMediaCountsGen.Add(1)
	if scope.MediaRowsChanged {
		db.clearMediaSearchBounds()
	}
	clearPrefixPolicyCache()
	clearCoverAvailabilityCacheFor(db.sql.Load())
	if scope.UtilityTagDBIDsChanged {
		clearUtilityTagCache()
		clearImagePropertyTagCache()
		db.utilityTagCacheDirty = false
	}
	switch {
	case scope.PreserveSlugSearchCache:
		// An indexing run publishes each system's new entries as it commits
		// (refreshMidScanCaches), so the commit itself must leave the cache
		// alone — in both scopes. Removing the systems here instead makes a
		// search that names them unservable from memory, and a library-wide
		// search names every system, so the whole request drops onto the
		// grouped SQL LIKE path for as long as the run lasts. Measured on the
		// #1279 device mid-index: 242 ms for a query across 28 covered systems
		// against 27,205 ms for the same query across all of them.
		//
		// Nothing is served wrongly in the meantime. The cache only nominates
		// candidate title IDs and the rows come from a live query, so entries
		// whose rows have gone return nothing; only files added since the last
		// run are missing, and only until their system commits.
	case scope.AllSystems:
		db.slugSearchCache.Store(nil)
	case len(scope.SystemIDs) > 0:
		if cache := db.slugSearchCache.Load(); cache != nil {
			db.slugSearchCache.Store(cache.withoutSystems(scope.SystemIDs))
		}
	default:
		db.slugSearchCache.Store(nil)
	}

	// MediaCountCache: always nuke everything (queries are too complex to selectively invalidate)
	if err := db.InvalidateCountCache(); err != nil {
		log.Warn().Err(err).Msg("failed to invalidate media count cache")
	}

	// System-specific caches: invalidate all or by system
	if scope.AllSystems {
		// Full invalidation
		if _, err := db.conn().ExecContext(db.ctx, "DELETE FROM SystemTagsCache"); err != nil {
			log.Warn().Err(err).Msg("failed to invalidate all system tags cache")
		}
		if _, err := db.conn().ExecContext(db.ctx, "DELETE FROM SlugResolutionCache"); err != nil {
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

func (db *MediaDB) clearMediaSearchBounds() {
	db.mediaSearchBoundsMu.Lock()
	defer db.mediaSearchBoundsMu.Unlock()
	db.mediaSearchBounds = nil
	db.mediaSearchBoundsLoads = nil
	db.mediaSearchBoundsGen++
}

func (db *MediaDB) getMediaSearchBounds(ctx context.Context, systemDBID int64) (mediaDBIDBounds, bool, error) {
	db.mediaSearchBoundsMu.Lock()
	if bounds, ok := db.mediaSearchBounds[systemDBID]; ok {
		db.mediaSearchBoundsMu.Unlock()
		return bounds, bounds.first > 0, nil
	}
	if load := db.mediaSearchBoundsLoads[systemDBID]; load != nil {
		db.mediaSearchBoundsMu.Unlock()
		select {
		case <-load.done:
			return load.bounds, load.found, load.err
		case <-ctx.Done():
			return mediaDBIDBounds{}, false, fmt.Errorf("query media search bounds: %w", ctx.Err())
		}
	}
	if db.mediaSearchBoundsLoads == nil {
		db.mediaSearchBoundsLoads = make(map[int64]*mediaSearchBoundsLoad)
	}
	load := &mediaSearchBoundsLoad{
		done:       make(chan struct{}),
		generation: db.mediaSearchBoundsGen,
	}
	db.mediaSearchBoundsLoads[systemDBID] = load
	db.mediaSearchBoundsMu.Unlock()

	queryStarted := time.Now()
	var firstMediaID, lastMediaID sql.NullInt64
	queryErr := db.sql.Load().QueryRowContext(ctx, `
		SELECT MIN(DBID), MAX(DBID)
		FROM Media
		WHERE SystemDBID = ? AND IsMissing = 0`, systemDBID).Scan(&firstMediaID, &lastMediaID)
	if queryErr != nil {
		queryErr = fmt.Errorf("query media search bounds: %w", queryErr)
	} else if firstMediaID.Valid && lastMediaID.Valid {
		load.bounds = mediaDBIDBounds{first: firstMediaID.Int64, last: lastMediaID.Int64}
	}
	load.found = load.bounds.first > 0
	load.err = queryErr

	db.mediaSearchBoundsMu.Lock()
	if current := db.mediaSearchBoundsLoads[systemDBID]; current == load {
		delete(db.mediaSearchBoundsLoads, systemDBID)
	}
	if queryErr == nil && load.generation == db.mediaSearchBoundsGen {
		if db.mediaSearchBounds == nil {
			db.mediaSearchBounds = make(map[int64]mediaDBIDBounds)
		}
		db.mediaSearchBounds[systemDBID] = load.bounds
	}
	close(load.done)
	db.mediaSearchBoundsMu.Unlock()

	if queryErr == nil {
		log.Debug().
			Int64("systemDBID", systemDBID).
			Int64("firstMediaID", load.bounds.first).
			Int64("lastMediaID", load.bounds.last).
			Dur("duration", time.Since(queryStarted)).
			Msg("media search system bounds cached")
	}
	return load.bounds, load.found, load.err
}

func invalidationScopeForMediaSystemIDs(systemIDs []string) invalidationScope {
	scope := invalidationScopeForSystemIDs(systemIDs)
	scope.MediaRowsChanged = true
	return scope
}

func invalidationScopeForSystemIDs(systemIDs []string) invalidationScope {
	if len(systemIDs) == 0 || len(systemIDs) > maxSelectiveInvalidationSystems {
		return invalidationScope{AllSystems: true}
	}
	return invalidationScope{SystemIDs: systemIDs}
}

// MidScanSystemTagsCacheSurvivesCommit reports whether populating SystemTagsCache
// for one system partway through a run of systemCount systems is worth doing.
//
// It is not, once the run is large enough to invalidate all systems: an
// all-systems scope drops the whole SystemTagsCache on every commit (see
// invalidateCaches), so a population done just after one commit is deleted by
// the next one. Repeating that per system spends a query and a transaction
// commit each time for a cache that never holds more than the most recently
// indexed system, and the end-of-run PopulateSystemTagsCache rebuilds it from
// scratch regardless. Smaller runs keep a selective scope, so their populations
// survive and stay worthwhile. Mirrors invalidationScopeForSystemIDs above;
// keep the two in step.
func MidScanSystemTagsCacheSurvivesCommit(systemCount int) bool {
	return systemCount > 0 && systemCount <= maxSelectiveInvalidationSystems
}

// shouldCheckpointAfterCommit reports whether a commit must run an explicit
// checkpoint of its own. Only WALCheckpointForce does. Callers that get false
// still checkpoint via checkpointLargeWAL, which fires once the WAL passes
// mediaWALCheckpointThreshold; that size-driven path replaced the older
// indexing-status-driven force, so WALCheckpointAuto no longer needs the status
// or its lookup error to decide.
func shouldCheckpointAfterCommit(mode database.WALCheckpointMode) bool {
	return mode == database.WALCheckpointForce
}

func (db *MediaDB) DropSlugSearchCacheForSystems(systemIDs []string) {
	if cache := db.slugSearchCache.Load(); cache != nil {
		db.slugSearchCache.Store(cache.withoutSystems(systemIDs))
	}
}

func (db *MediaDB) markBrowseCacheDirty() {
	db.browseCacheDirty = true
}

func (db *MediaDB) markUtilityTagCacheDirty() {
	db.utilityTagCacheDirty = true
}

func (db *MediaDB) invalidateBrowseCacheForMediaChange() error {
	if db.inTransaction {
		db.markBrowseCacheDirty()
		return nil
	}
	if err := sqlInvalidateBrowseCache(db.ctx, db.conn()); err != nil {
		log.Debug().Err(err).Msg("failed to invalidate browse cache after media change")
		return err
	}
	return nil
}

func (db *MediaDB) flushBrowseCacheInvalidation() error {
	if !db.browseCacheDirty {
		return nil
	}
	err := sqlInvalidateBrowseCache(db.ctx, db.sql.Load())
	if err != nil {
		return err
	}
	db.clearBrowseCacheInvalidation()
	return nil
}

func (db *MediaDB) clearBrowseCacheInvalidation() {
	db.browseCacheDirty = false
}

func OpenMediaDB(ctx context.Context, pl platforms.Platform) (*MediaDB, error) {
	dbPath := filepath.Join(helpers.DataDir(pl), config.MediaDbFile)
	db := &MediaDB{
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
	log.Debug().Str("path", dbPath).Msg("checking if media database file exists")

	_, err := os.Stat(dbPath)
	if err != nil {
		exists = false
		log.Debug().Msg("media database file does not exist, creating directory")
		mkdirErr := os.MkdirAll(filepath.Dir(dbPath), 0o750)
		if mkdirErr != nil {
			return fmt.Errorf("failed to create media database directory: %w", mkdirErr)
		}
	}

	log.Debug().Msg("opening media database connection")
	sqlInstance, err := sql.Open(sqliteDriverName(), dbPath+getSqliteConnParams())
	if err != nil {
		return fmt.Errorf("failed to open media database: %w", err)
	}
	sqlInstance.SetMaxOpenConns(baseMaxOpenConns)
	// Set explicitly rather than relying on database/sql's default of 2 to
	// happen to equal baseMaxOpenConns: an idle cap below the open cap lets the
	// pool recycle connections and lose their pragmas. See SetIndexingConnBoost.
	sqlInstance.SetMaxIdleConns(baseMaxOpenConns)
	db.sql.Store(sqlInstance)
	if _, err = sqlInstance.ExecContext(db.ctx, "PRAGMA cell_size_check=ON"); err != nil {
		if database.IsCorruptionError(err) {
			db.MarkCorrupt(fmt.Sprintf("cell_size_check failed during open: %v", err))
			log.Warn().Err(err).Msg("media database cell size check failed during open")
		} else {
			// cell_size_check is a best-effort safety pragma; a non-corruption failure
			// (e.g. a transient "database is locked") must not disconnect an otherwise-usable
			// database. Keep the connection and re-attempt the pragma on the next open.
			log.Warn().Err(err).Msg("failed to enable media database cell size checks; continuing without")
		}
	}
	database.LogEffectivePragmasForDB(db.ctx, sqlInstance, "media", database.SynchronousNormal, database.UnsetPageSize)

	clearUtilityTagCache()
	clearCoverAvailabilityCache()
	clearImagePropertyTagCache()
	clearPrefixPolicyCache()
	db.systemMediaCountsCache.Store(nil)
	db.systemMediaCountsGen.Add(1)
	db.clearMediaSearchBounds()

	if !exists {
		log.Debug().Msg("media database is new, allocating schema")
		err = db.Allocate()
		if err != nil {
			return err
		}
	}

	registerCoverAvailabilityCacheOwner(sqlInstance, db)
	return nil
}

func (db *MediaDB) GetDBPath() string {
	return db.dbPath
}

// SetIndexingCacheSize temporarily increases SQLite cache_size for bulk indexing.
// Call with enable=true before indexing starts, and enable=false after it completes.
// When enabled, sets 32MB cache (vs default 8MB), intended to reduce page
// eviction during heavy insert workloads with non-sequential index keys.
//
// Also switches temp_store to MEMORY for the duration: the GROUP BY temp B-trees
// built by the post-indexing cache population are only a few MB, and the default
// temp_store=FILE writes them to slow storage (SD card) on embedded devices.
//
// What this is actually worth has never been measured, and #1279 did not settle
// it despite appearances. Round 11 was the first run where the boost reached
// every pooled connection, and its optimization phase matched round 10's to
// within 0.03% — but round 10 was not an unboosted control: two of its three
// connections carried the boost and only the third came up at DSN defaults, so
// the work may well have run boosted in both. Any future comparison has to turn
// the boost off deliberately, and has to account for cache_size and temp_store
// separately, since this one switch moves both.
//
// Both pragmas are per-connection, so every pooled connection is configured
// when the indexing state changes. BeginTransaction also re-applies the current
// settings on its dedicated connection before starting the transaction.
func (db *MediaDB) SetIndexingCacheSize(enable bool) {
	db.indexingCacheBoost.Store(enable)
	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return
	}
	if !enable {
		db.restorePooledConnPragmas(sqlDB)
		return
	}

	db.applyPooledConnPragmas(sqlDB)
}

// SetWALAutoCheckpoint applies a per-connection SQLite WAL checkpoint trigger.
// Indexing disables it (pages=0) so SQLite never attempts an automatic
// checkpoint inside tx.Commit, then restores SQLite's default after indexing;
// checkpointLargeWAL drives checkpoints explicitly and deliberately instead.
// pages=0 is a valid, distinct setting (disabled) from never having called
// this at all, which is why walAutoCheckpointSet exists rather than treating
// zero as "unset" — a MediaDB that never calls this keeps SQLite's compiled
// default (walAutoCheckpointPages below), not zero.
func (db *MediaDB) SetWALAutoCheckpoint(pages int) {
	if pages < 0 {
		return
	}
	db.walAutoCheckpoint.Store(int64(pages))
	db.walAutoCheckpointSet.Store(true)
	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return
	}

	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	conns, acquireErr := db.drainPooledConns(sqlDB)
	if acquireErr != nil {
		log.Warn().Err(acquireErr).Int("pages", pages).
			Msg("failed to acquire pooled connections while setting WAL autocheckpoint")
	}
	//nolint:gosec // pages is a validated non-negative integer, not SQL input.
	query := "PRAGMA wal_autocheckpoint = " + strconv.Itoa(pages)
	for _, conn := range conns {
		if _, err := conn.ExecContext(db.ctx, query); err != nil {
			log.Warn().Err(err).Int("pages", pages).Msg("failed to set WAL autocheckpoint")
		}
		if err := conn.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to release pooled connection after setting WAL autocheckpoint")
		}
	}
}

func (db *MediaDB) walAutoCheckpointPages() int {
	if !db.walAutoCheckpointSet.Load() {
		return defaultWALAutoCheckpoint
	}
	return int(db.walAutoCheckpoint.Load())
}

// drainPooledConns checks out every pool slot simultaneously. The caller must
// hold sqlMu so a writer transaction cannot start while the pool is drained.
//
// The target is re-read on every iteration rather than sampled once. The cap is
// not stable: SetIndexingConnBoost moves it between baseMaxOpenConns and
// indexingMaxOpenConns from another goroutine, and a drain sized against the
// wider value will block forever on a slot the pool can no longer create — with
// every existing connection already held here, so nothing can be returned to
// satisfy it either. That deadlock-against-self cost three device runs in
// #1279; it resolved only when the acquire deadline fired, and the warning it
// produced pointed at the post-failure cap rather than the one that was
// targeted.
//
// Each acquisition also gets its own deadline. A single budget shared across
// every slot means one slow acquisition silently spends the next one's time.
func (db *MediaDB) drainPooledConns(sqlDB *sql.DB) ([]*sql.Conn, error) {
	target := func() int {
		stats := sqlDB.Stats()
		count := stats.MaxOpenConnections
		if count <= 0 {
			count = max(stats.OpenConnections, 1)
		}
		if db.txConn != nil {
			count--
		}
		return count
	}

	connCount := target()
	if connCount <= 0 {
		return nil, nil
	}
	conns := make([]*sql.Conn, 0, connCount)
	for len(conns) < connCount {
		// Re-read before each acquisition: a cap that shrank mid-drain means
		// the connections already held are the whole pool and there is nothing
		// left to wait for.
		if current := target(); current < connCount {
			connCount = current
			continue
		}
		conn, err := db.acquirePooledConn(sqlDB)
		if err != nil {
			return conns, err
		}
		conns = append(conns, conn)
	}
	return conns, nil
}

// acquirePooledConn checks out one connection under its own deadline.
func (db *MediaDB) acquirePooledConn(sqlDB *sql.DB) (*sql.Conn, error) {
	acquireCtx, cancel := context.WithTimeout(db.ctx, connectionAcquireTimeout)
	defer cancel()
	conn, err := sqlDB.Conn(acquireCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire pooled connection: %w", err)
	}
	return conn, nil
}

// applyPooledConnPragmas drains the pool so both indexing pragmas reach every
// available physical connection rather than whichever connection Exec selects.
func (db *MediaDB) applyPooledConnPragmas(sqlDB *sql.DB) {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	conns, acquireErr := db.drainPooledConns(sqlDB)
	if acquireErr != nil {
		// A partial drain leaves some connections on the old pragmas, and an
		// empty one applies the boost nowhere at all. Round 8 of #1279 hit
		// exactly that: the drain timed out, this returned quietly, and the
		// whole post-index optimization ran at the 8MB default — visible only
		// as dbCacheSize on the step metrics, hours later. Log what actually
		// happened so the next run says so in the log itself. See #1279.
		//
		// maxOpenConns here is sampled AFTER the failure, so it can differ from
		// the cap the drain sized itself against — three rounds of #1279 read
		// "acquired 2 of a 2-connection pool, timed out" and looked like a
		// contradiction for exactly that reason. connsInUse likewise counts the
		// connections this drain is itself holding.
		stats := sqlDB.Stats()
		event := log.Warn().Err(acquireErr).
			Int("connsAcquired", len(conns)).
			Int("maxOpenConnsAfterFailure", stats.MaxOpenConnections).
			Int("connsInUse", stats.InUse)
		if errors.Is(acquireErr, context.DeadlineExceeded) {
			event.Msg("timed out acquiring pooled connections while enabling indexing pragmas")
		} else {
			event.Msg("failed to acquire pooled connection while enabling indexing pragmas")
		}
	}

	cacheSize, tempStore := db.connPragmaValues()
	for _, conn := range conns {
		if _, err := conn.ExecContext(db.ctx, "PRAGMA cache_size = "+cacheSize); err != nil {
			log.Warn().Err(err).Bool("enable", true).Msg("failed to set indexing cache size")
		}
		if _, err := conn.ExecContext(db.ctx, "PRAGMA temp_store = "+tempStore); err != nil {
			log.Warn().Err(err).Bool("enable", true).Msg("failed to set indexing temp_store")
		}
		if err := conn.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to release pooled connection after enabling indexing pragmas")
		}
	}
}

// restorePooledConnPragmas drains the pool, resets each physical connection,
// then returns them. An existing writer is excluded because its pinned
// connection restores itself when it finishes.
func (db *MediaDB) restorePooledConnPragmas(sqlDB *sql.DB) {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	conns, acquireErr := db.drainPooledConns(sqlDB)
	if acquireErr != nil {
		if errors.Is(acquireErr, context.DeadlineExceeded) {
			log.Warn().Err(acquireErr).Msg("timed out acquiring pooled connections while restoring indexing pragmas")
		} else {
			log.Warn().Err(acquireErr).Msg("failed to acquire pooled connection while restoring indexing pragmas")
		}
	}
	for _, conn := range conns {
		if err := db.closeWriterConn(conn); err != nil {
			log.Warn().Err(err).Msg("failed to restore pooled connection after indexing")
		}
	}
}

// ensureIndexingCacheBoostApplied checks that the cache_size pragma actually
// reached every pooled connection, and retries once if it did not.
//
// applyPooledConnPragmas is best-effort by design: if it cannot check out every
// pool slot it configures the ones it got and returns. That is the right
// behaviour for indexing, which sets the boost while the pool is quiet, but
// post-index optimization starts while the app is still polling, and round 8 of
// #1279 spent its entire optimization phase at the 8MB default because of it.
func (db *MediaDB) ensureIndexingCacheBoostApplied() {
	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return
	}

	wantCacheSize, _ := db.connPragmaValues()
	if db.pooledCacheSizeMatches(sqlDB, wantCacheSize) {
		return
	}

	log.Warn().
		Str("want", wantCacheSize).
		Msg("indexing cache boost did not reach the pool, retrying")
	db.applyPooledConnPragmas(sqlDB)

	if db.pooledCacheSizeMatches(sqlDB, wantCacheSize) {
		log.Info().Str("cacheSize", wantCacheSize).Msg("indexing cache boost applied on retry")
		return
	}
	// Not fatal — optimization is correct at any cache size, just slower. Logged
	// loudly because the cost is large and otherwise invisible.
	log.Warn().
		Str("want", wantCacheSize).
		Msg("indexing cache boost still not applied; optimization will run at the default cache size")
}

// pooledCacheSizeMatches reports whether EVERY pooled connection carries the
// expected cache_size.
//
// This used to read the pragma back with a single pool query, which is not a
// verification at all: the pool hands out an arbitrary connection, so the check
// passed as soon as it happened to land on a boosted one while its siblings sat
// at the default. That is precisely the state round 9 of #1279 was in when this
// reported success and every optimization step then logged dbCacheSize -8192.
// A check that cannot fail is worse than no check, so drain the pool and look
// at all of them.
func (db *MediaDB) pooledCacheSizeMatches(sqlDB *sql.DB, want string) bool {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	conns, acquireErr := db.drainPooledConns(sqlDB)
	defer func() {
		for _, conn := range conns {
			if err := conn.Close(); err != nil {
				log.Warn().Err(err).Msg("failed to release pooled connection after cache_size check")
			}
		}
	}()
	if acquireErr != nil {
		// Could not see the whole pool, so cannot claim the whole pool matches.
		log.Warn().Err(acquireErr).
			Int("connsChecked", len(conns)).
			Msg("could not drain pool to verify cache_size")
		return false
	}
	if len(conns) == 0 {
		return false
	}

	for _, conn := range conns {
		var actual int
		if err := conn.QueryRowContext(db.ctx, "PRAGMA cache_size").Scan(&actual); err != nil {
			log.Warn().Err(err).Msg("failed to read back pooled cache_size")
			return false
		}
		if strconv.Itoa(actual) != want {
			return false
		}
	}
	return true
}

// connPragmaValues returns the cache_size and temp_store settings matching the
// current indexing state: 32MB/MEMORY while the indexing boost is active,
// 8MB/FILE steady state (mirroring the connection-string defaults).
func (db *MediaDB) connPragmaValues() (cacheSize, tempStore string) {
	if db.indexingCacheBoost.Load() {
		return "-32768", "MEMORY"
	}
	return defaultConnCacheSize, defaultConnTempStore
}

// applyConnPragmas configures a dedicated connection before it starts a
// transaction. temp_store cannot be changed from inside a transaction once the
// connection has used temporary objects, so pragma setup must precede BeginTx.
func applyConnPragmas(
	ctx context.Context, conn *sql.Conn, cacheSize, tempStore string,
) error {
	if _, err := conn.ExecContext(ctx, "PRAGMA cache_size = "+cacheSize); err != nil {
		return fmt.Errorf("failed to set writer connection cache size: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA temp_store = "+tempStore); err != nil {
		return fmt.Errorf("failed to set writer connection temp_store: %w", err)
	}
	return nil
}

// closeWriterConn restores steady-state settings before returning a writer
// connection to the pool. If restoration fails, the physical connection is
// discarded so boosted settings cannot leak into later foreground work. Setup
// failure paths call this directly because partially applied pragmas are unsafe.
func (db *MediaDB) closeWriterConn(conn *sql.Conn) error {
	if conn == nil {
		return nil
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(db.ctx), 5*time.Second)
	defer cancel()
	resetErr := applyConnPragmas(cleanupCtx, conn, defaultConnCacheSize, defaultConnTempStore)
	if resetErr != nil {
		// Returning ErrBadConn from Raw closes and discards the physical
		// connection. A later Conn.Close would only report sql.ErrConnDone.
		discardErr := conn.Raw(func(any) error { return driver.ErrBadConn })
		if errors.Is(discardErr, driver.ErrBadConn) || errors.Is(discardErr, sql.ErrConnDone) {
			discardErr = nil
		}
		return errors.Join(resetErr, discardErr)
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("failed to close writer connection: %w", err)
	}
	return nil
}

func (db *MediaDB) releaseWriterConn() error {
	conn := db.txConn
	db.txConn = nil
	if conn == nil {
		return nil
	}

	// Preserve the hot page cache between indexing batches. Resetting cache_size
	// after every commit evicts cached pages, making the next reconcile reread the
	// database from slow storage. SetIndexingCacheSize(false) drains and restores
	// the whole pool when indexing ends.
	if db.indexingCacheBoost.Load() {
		if err := conn.Close(); err != nil {
			return fmt.Errorf("failed to release boosted writer connection: %w", err)
		}
		return nil
	}
	return db.closeWriterConn(conn)
}

// analyzeApproximateMask is the PRAGMA optimize bitmask used for planner
// statistics refreshes. Each bit matters:
//
//	0x00002  run ANALYZE on tables that might benefit (the actual work)
//	0x00010  apply SQLITE_DEFAULT_OPTIMIZE_LIMIT (2000) as the analysis limit
//	0x10000  consider tables that were not queried on this connection
//
// 0x10000 is off by default and is what lets a table qualify on size change
// alone rather than on having been queried through this particular pooled
// connection. That matters here because the pool hands out an arbitrary
// connection per call.
//
// 0x10 is weaker than it looks, and this is worth stating plainly because an
// earlier version of this comment claimed otherwise. It does not stop an
// ANALYZE after 2000 rows: sqlite3-binding.c statPush makes the scan *seek
// past the current distinct value of the index's leading column* once the
// limit is hit. On a high-cardinality leading column each skip advances about
// one row, so the scan degenerates into a full index walk. Media carries
// media_path_idx(Path) and the UNIQUE(SystemDBID, Path) autoindex; MediaTitles
// carries unique slug indexes. For those, 0x10 buys close to nothing.
//
// The consequence is measured, not theoretical: round 9 of #1279 saw this call
// cost 2 ms at all but one system boundary and 54,442 ms at that one, on a
// system holding 426 files, because PRAGMA optimize is database-wide and never
// scoped to the system that just committed.
//
// If a spike like that needs attributing again, bit 0x01 turns PRAGMA optimize
// into a reporting mode that returns one row per ANALYZE it would have run
// without running any of them. Issue it by hand rather than on every call: it
// costs an extra round-trip, and because the pool hands out an arbitrary
// connection the reported plan may not describe the connection that did the
// work.
const analyzeApproximateMask = "0x10012"

// AnalyzeApproximate refreshes query-planner statistics before synchronous
// cache builds. PRAGMA optimize is used instead of a raw ANALYZE because it
// only refreshes tables likely to benefit, but note the caveat on
// analyzeApproximateMask: the 0x10 limit does not reliably bound the work.
func (db *MediaDB) AnalyzeApproximate() error {
	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return ErrNullSQL
	}
	started := time.Now()
	_, err := sqlDB.ExecContext(db.ctx, "PRAGMA optimize="+analyzeApproximateMask)
	elapsed := time.Since(started)
	if err != nil {
		return fmt.Errorf("failed to run pragma optimize: %w", err)
	}
	// Warn rather than debug when it was not a no-op: the whole point of this
	// telemetry is that a multi-second planner refresh is invisible otherwise.
	logEvent := log.Debug()
	if elapsed > time.Second {
		logEvent = log.Warn()
	}
	logEvent.
		Dur("elapsed", elapsed).
		Msg("approximate ANALYZE completed")
	return nil
}

const browseSortIndexName = "idx_media_browse_sort"

type secondaryIndex struct {
	name               string
	ddl                string
	replaceWhenEnsured bool
}

// secondaryIndexes lists all secondary indexes that can be dropped before bulk
// inserts and recreated afterward. Created synchronously at the end of indexing
// so the database is fully searchable when indexing completes.
var secondaryIndexes = []secondaryIndex{
	{name: "mediatitles_slug_idx", ddl: "CREATE INDEX IF NOT EXISTS mediatitles_slug_idx ON MediaTitles(Slug)"},
	{
		name: "mediatitles_system_slug_idx",
		ddl:  "CREATE INDEX IF NOT EXISTS mediatitles_system_slug_idx ON MediaTitles(SystemDBID, Slug)",
	},
	{
		name: "mediatitles_secondary_slug_idx",
		ddl:  "CREATE INDEX IF NOT EXISTS mediatitles_secondary_slug_idx ON MediaTitles(SecondarySlug)",
	},
	{
		name: "mediatitles_prefilter_idx",
		ddl:  "CREATE INDEX IF NOT EXISTS mediatitles_prefilter_idx ON MediaTitles(SlugLength, SlugWordCount)",
	},
	{name: "media_mediatitle_idx", ddl: "CREATE INDEX IF NOT EXISTS media_mediatitle_idx ON Media(MediaTitleDBID)"},
	{name: "media_path_idx", ddl: "CREATE INDEX IF NOT EXISTS media_path_idx ON Media(Path)"},
	// No entry for (SystemDBID, Path): Media declares UNIQUE(SystemDBID, Path),
	// so SQLite already maintains sqlite_autoindex_Media_1 over exactly those
	// columns in that order. A second identical index only doubled the b-tree
	// maintenance on every Media write. Queries that need the access path pin it
	// with INDEXED BY sqlite_autoindex_Media_1.
	{name: "media_missing_idx", ddl: "CREATE INDEX IF NOT EXISTS media_missing_idx ON Media(IsMissing)"},
	{
		name: "media_system_present_path_idx",
		ddl:  "CREATE INDEX IF NOT EXISTS media_system_present_path_idx ON Media(SystemDBID, Path) WHERE IsMissing = 0",
	},
	{
		name: "media_title_present_idx",
		ddl:  "CREATE INDEX IF NOT EXISTS media_title_present_idx ON Media(MediaTitleDBID, DBID) WHERE IsMissing = 0",
	},
	{name: "tags_tag_idx", ddl: "CREATE INDEX IF NOT EXISTS tags_tag_idx ON Tags(Tag)"},
	{name: "tags_tagtype_idx", ddl: "CREATE INDEX IF NOT EXISTS tags_tagtype_idx ON Tags(TypeDBID)"},
	{name: "tags_type_tag_idx", ddl: "CREATE INDEX IF NOT EXISTS tags_type_tag_idx ON Tags(TypeDBID, Tag)"},
	{
		name: "mediatags_tag_media_idx",
		ddl:  "CREATE INDEX IF NOT EXISTS mediatags_tag_media_idx ON MediaTags(TagDBID, MediaDBID)",
	},
	{
		name: "mediatitletags_tag_idx",
		ddl:  "CREATE INDEX IF NOT EXISTS mediatitletags_tag_idx ON MediaTitleTags(TagDBID)",
	},
	{
		name: "mediatitleproperties_title_idx",
		ddl:  "CREATE INDEX IF NOT EXISTS mediatitleproperties_title_idx ON MediaTitleProperties(MediaTitleDBID)",
	},
	{
		name: "mediatitleproperties_typetag_idx",
		ddl:  "CREATE INDEX IF NOT EXISTS mediatitleproperties_typetag_idx ON MediaTitleProperties(TypeTagDBID)",
	},
	{
		name: "mediaproperties_media_idx",
		ddl:  "CREATE INDEX IF NOT EXISTS mediaproperties_media_idx ON MediaProperties(MediaDBID)",
	},
	{
		name: "mediaproperties_typetag_idx",
		ddl:  "CREATE INDEX IF NOT EXISTS mediaproperties_typetag_idx ON MediaProperties(TypeTagDBID)",
	},
	{
		name: "idx_systemtagscache_type_tag",
		ddl:  "CREATE INDEX IF NOT EXISTS idx_systemtagscache_type_tag ON SystemTagsCache(SystemDBID, TagType, Tag)",
	},
	{
		name: "idx_slug_cache_system",
		ddl:  "CREATE INDEX IF NOT EXISTS idx_slug_cache_system ON SlugResolutionCache(SystemID)",
	},
	{
		name: "idx_slug_cache_media",
		ddl:  "CREATE INDEX IF NOT EXISTS idx_slug_cache_media ON SlugResolutionCache(MediaDBID)",
	},
	{
		name: "idx_browsedircounts_system",
		ddl:  "CREATE INDEX IF NOT EXISTS idx_browsedircounts_system ON BrowseDirCounts(SystemDBID)",
	},
	{name: "idx_media_parentdir", ddl: "CREATE INDEX IF NOT EXISTS idx_media_parentdir ON Media(ParentDir)"},
	{
		name: "idx_media_parentdir_system",
		ddl:  "CREATE INDEX IF NOT EXISTS idx_media_parentdir_system ON Media(ParentDir, SystemDBID)",
	},
	{
		name: browseSortIndexName,
		ddl: "CREATE INDEX IF NOT EXISTS " + browseSortIndexName +
			" ON Media(ParentDir, IsMissing, SortName COLLATE " + browseTitleCollationName + ", DBID)",
		replaceWhenEnsured: true,
	},
}

// DropSecondaryIndexes drops all secondary indexes to speed up bulk inserts.
// Call before a full reindex, then call CreateSecondaryIndexes after.
func (db *MediaDB) DropSecondaryIndexes() error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	for _, idx := range secondaryIndexes {
		_, err := db.sql.Load().ExecContext(db.ctx, "DROP INDEX IF EXISTS "+idx.name)
		if err != nil {
			return fmt.Errorf("failed to drop index %s: %w", idx.name, err)
		}
	}
	db.needsIndexRebuild.Store(true)
	log.Debug().Int("count", len(secondaryIndexes)).Msg("dropped secondary indexes for bulk insert")
	return nil
}

func (db *MediaDB) secondaryIndexExists(indexName string) (bool, error) {
	var exists int
	conn := db.conn()
	err := conn.QueryRowContext(
		db.ctx,
		"SELECT 1 FROM sqlite_master WHERE type = 'index' AND name = ?",
		indexName,
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check index %s existence: %w", indexName, err)
	}
	return true, nil
}

func (db *MediaDB) secondaryIndexCurrent(idx secondaryIndex) (bool, error) {
	exists, err := db.secondaryIndexExists(idx.name)
	if err != nil || !exists {
		return exists, err
	}
	if idx.name != browseSortIndexName {
		return true, nil
	}

	rows, err := db.conn().QueryContext(db.ctx,
		"SELECT name, coll FROM pragma_index_xinfo(?) WHERE key = 1", idx.name)
	if err != nil {
		return false, fmt.Errorf("failed to inspect index %s: %w", idx.name, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var column, collation sql.NullString
		if scanErr := rows.Scan(&column, &collation); scanErr != nil {
			return false, fmt.Errorf("failed to scan index %s columns: %w", idx.name, scanErr)
		}
		if column.String == "SortName" {
			return strings.EqualFold(collation.String, browseTitleCollationName), nil
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return false, fmt.Errorf("failed to read index %s columns: %w", idx.name, rowsErr)
	}
	return false, nil
}

func (db *MediaDB) missingSecondaryIndexes() ([]secondaryIndex, error) {
	missing := make([]secondaryIndex, 0)
	for _, idx := range secondaryIndexes {
		current, err := db.secondaryIndexCurrent(idx)
		if err != nil {
			return nil, err
		}
		if !current {
			missing = append(missing, idx)
		}
	}
	return missing, nil
}

func (db *MediaDB) replaceSecondaryIndex(idx secondaryIndex) error {
	tx, err := db.sql.Load().BeginTx(db.ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin replacement of index %s: %w", idx.name, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(db.ctx, "DROP INDEX IF EXISTS "+idx.name); err != nil {
		return fmt.Errorf("failed to drop old index %s: %w", idx.name, err)
	}
	if _, err = tx.ExecContext(db.ctx, idx.ddl); err != nil {
		return fmt.Errorf("failed to create replacement index %s: %w", idx.name, err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit replacement index %s: %w", idx.name, err)
	}
	return nil
}

// CreateSecondaryIndexes recreates dropped secondary indexes after bulk inserts
// and self-heals any required indexes missing from existing databases.
// Called synchronously at the end of indexing so the database is fully
// searchable when indexing completes.
func (db *MediaDB) CreateSecondaryIndexes() error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	rebuildRequested := db.needsIndexRebuild.Load()
	indexesToEnsure := secondaryIndexes
	if !rebuildRequested {
		missingIndexes, err := db.missingSecondaryIndexes()
		if err != nil {
			return err
		}
		if len(missingIndexes) == 0 {
			log.Debug().
				Bool("rebuildRequested", rebuildRequested).
				Int("count", len(secondaryIndexes)).
				Msg("all secondary indexes already present")
			return nil
		}
		indexesToEnsure = missingIndexes
	}

	for _, idx := range indexesToEnsure {
		started := time.Now()
		var err error
		if idx.replaceWhenEnsured {
			err = db.replaceSecondaryIndex(idx)
		} else {
			_, err = db.sql.Load().ExecContext(db.ctx, idx.ddl)
		}
		if err != nil {
			return fmt.Errorf("failed to create index %s: %w", idx.name, err)
		}
		log.Debug().Str("index", idx.name).Dur("elapsed", time.Since(started)).Msg("ensured secondary index exists")
	}
	db.needsIndexRebuild.Store(false)
	log.Debug().
		Bool("rebuildRequested", rebuildRequested).
		Int("count", len(indexesToEnsure)).
		Msg("ensured secondary indexes exist")
	return nil
}

func (db *MediaDB) Exists() bool {
	return db.sql.Load() != nil
}

func (db *MediaDB) UpdateLastGenerated() error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}

	err := sqlUpdateLastGenerated(db.ctx, db.conn())
	if err == nil {
		if countErr := sqlRefreshMediaCounts(db.ctx, db.conn()); countErr != nil {
			log.Warn().Err(countErr).Msg("failed to refresh cached media counts")
		}
	}

	// Only invalidate cache if NOT in a transaction (transactions invalidate once on commit)
	if err == nil && !db.inTransaction {
		systemIDs, getSystemsErr := db.GetIndexingSystems()
		indexingStatus, statusErr := db.GetIndexingStatus()
		isIndexing := statusErr == nil &&
			(indexingStatus == IndexingStatusRunning || indexingStatus == IndexingStatusPending)
		switch {
		case getSystemsErr != nil:
			log.Warn().Err(getSystemsErr).
				Msg("failed to load indexing systems for cache invalidation; clearing all caches")
			db.invalidateCaches(invalidationScope{AllSystems: true, MediaRowsChanged: true})
		default:
			scope := invalidationScopeForMediaSystemIDs(systemIDs)
			scope.PreserveSlugSearchCache = isIndexing && !scope.AllSystems
			db.invalidateCaches(scope)
		}
	}

	return err
}

func (db *MediaDB) GetLastGenerated() (time.Time, error) {
	sqlDB, err := db.readConn()
	if err != nil {
		return time.Time{}, err
	}
	return sqlGetLastGenerated(db.ctx, sqlDB)
}

func (db *MediaDB) SetOptimizationStatus(status string) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlSetOptimizationStatus(db.ctx, db.conn(), status)
}

func (db *MediaDB) GetOptimizationStatus() (string, error) {
	sqlDB, err := db.readConn()
	if err != nil {
		return "", err
	}
	return sqlGetOptimizationStatus(db.ctx, sqlDB)
}

// IsOptimizing reports whether the database is currently undergoing any
// background optimization-class operation: a full RunBackgroundOptimization
// pass, or a standalone browse-cache rebuild (see BeginBrowseCacheRebuild). It
// is the single signal callers should query to show an "optimizing" indicator —
// they should not need to know which specific operation is running.
//
// This is intentionally process-local, not the persisted OptimizationStatus: a
// browse-cache rebuild can run for minutes independently of (and possibly
// overlapping) a full optimization, and writing the persisted status from both
// would race two operations over who owns "running". Being process-local also
// means a crash simply drops the flag rather than wedging a persisted status.
func (db *MediaDB) getMediaWriteArbiter() *database.MediaWriteArbiter {
	if arbiter := db.mediaWriteArbiter.Load(); arbiter != nil {
		return arbiter
	}
	arbiter := &database.MediaWriteArbiter{}
	if db.mediaWriteArbiter.CompareAndSwap(nil, arbiter) {
		return arbiter
	}
	return db.mediaWriteArbiter.Load()
}

func (db *MediaDB) AcquireMediaWrite(operation database.MediaWriteOperation) (*database.MediaWriteLease, error) {
	lease, err := db.getMediaWriteArbiter().TryAcquire(operation)
	if err != nil {
		return nil, fmt.Errorf("acquire media database write operation: %w", err)
	}
	return lease, nil
}

func (db *MediaDB) ActiveMediaWriteOperation() database.MediaWriteOperation {
	return db.getMediaWriteArbiter().Active()
}

func (db *MediaDB) IsOptimizing() bool {
	return db.isOptimizing.Load() || db.browseCacheRebuilding.Load()
}

// BeginBrowseCacheRebuild marks a standalone browse-cache rebuild (outside a full
// RunBackgroundOptimization pass, e.g. the startup self-heal) as in progress, so
// IsOptimizing reflects it. Call EndBrowseCacheRebuild when the rebuild finishes.
func (db *MediaDB) BeginBrowseCacheRebuild() {
	db.browseCacheRebuilding.Store(true)
}

// EndBrowseCacheRebuild clears the flag set by BeginBrowseCacheRebuild.
func (db *MediaDB) EndBrowseCacheRebuild() {
	db.browseCacheRebuilding.Store(false)
}

func (db *MediaDB) SetOptimizationStep(step string) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlSetOptimizationStep(db.ctx, db.conn(), step)
}

func (db *MediaDB) GetOptimizationStep() (string, error) {
	sqlDB, err := db.readConn()
	if err != nil {
		return "", err
	}
	return sqlGetOptimizationStep(db.ctx, sqlDB)
}

// GetIndexResumeAttempts returns the number of consecutive automatic resume
// attempts that found no durable indexing progress since the previous resume.
// It resets to zero once indexing reaches a clean state or the resume checkpoint moves.
func (db *MediaDB) GetIndexResumeAttempts() (int, error) {
	sqlDB, err := db.readConn()
	if err != nil {
		return 0, err
	}
	return sqlGetIndexResumeAttempts(db.ctx, sqlDB)
}

// IncrementIndexResumeAttempts bumps the no-progress resume-attempt counter and
// returns the new value. It bounds repeated resumes only when the durable
// indexing checkpoint is not moving.
func (db *MediaDB) IncrementIndexResumeAttempts() (int, error) {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	if db.sql.Load() == nil {
		return 0, ErrNullSQL
	}
	current, err := sqlGetIndexResumeAttempts(db.ctx, db.sql.Load())
	if err != nil {
		return 0, err
	}
	next := current + 1
	if err := sqlSetIndexResumeAttempts(db.ctx, db.conn(), next); err != nil {
		return 0, err
	}
	return next, nil
}

// ResetIndexResumeAttempts clears the no-progress resume-attempt counter and
// checkpoint, giving a future interrupted index a fresh stall budget.
func (db *MediaDB) ResetIndexResumeAttempts() error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return ErrNullSQL
	}
	return sqlResetIndexResumeState(db.ctx, sqlDB)
}

// GetIndexResumeCheckpoint returns the durable indexing checkpoint observed at
// the previous auto-resume. A changed checkpoint proves the index made progress.
func (db *MediaDB) GetIndexResumeCheckpoint() (string, error) {
	sqlDB, err := db.readConn()
	if err != nil {
		return "", err
	}
	return sqlGetIndexResumeCheckpoint(db.ctx, sqlDB)
}

// SetIndexResumeCheckpoint stores the durable indexing checkpoint observed at
// auto-resume time so the next boot can detect whether progress moved.
func (db *MediaDB) SetIndexResumeCheckpoint(checkpoint string) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlSetIndexResumeCheckpoint(db.ctx, db.conn(), checkpoint)
}

func (db *MediaDB) SetIndexingStatus(status string) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlSetIndexingStatus(db.ctx, db.conn(), status)
}

func (db *MediaDB) GetIndexingStatus() (string, error) {
	sqlDB, err := db.readConn()
	if err != nil {
		return "", err
	}
	return sqlGetIndexingStatus(db.ctx, sqlDB)
}

// QuickCheck runs PRAGMA quick_check(1) and reports whether the database passes.
// quick_check is a bounded integrity scan — it skips integrity_check's expensive
// index-vs-table cross-checks and the argument stops it after the first error — so it
// is cheap enough to confirm suspected corruption before acting on it. Returns
// ok=true only when SQLite reports the single sentinel row "ok".
func (db *MediaDB) QuickCheck() (bool, error) {
	sqlDB, connErr := db.readConn()
	if connErr != nil {
		return false, connErr
	}

	rows, err := sqlDB.QueryContext(db.ctx, "PRAGMA quick_check(1)")
	if err != nil {
		return false, fmt.Errorf("quick_check query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var lines []string
	for rows.Next() {
		var line string
		if scanErr := rows.Scan(&line); scanErr != nil {
			return false, fmt.Errorf("quick_check scan failed: %w", scanErr)
		}
		lines = append(lines, line)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return false, fmt.Errorf("quick_check rows failed: %w", rowsErr)
	}

	if len(lines) == 1 && lines[0] == "ok" {
		return true, nil
	}
	log.Warn().Strs("details", lines).Msg("media database quick_check reported integrity errors")
	return false, nil
}

// MarkCorrupt writes a sidecar marker file next to the database recording that
// corruption was detected. The marker is the authoritative, DB-independent signal
// the recovery path keys on: unlike IndexingStatusCorrupt it does not require writing
// to the (possibly unwritable) database. Best-effort — failures are logged, not returned.
func (db *MediaDB) MarkCorrupt(reason string) {
	database.MarkCorrupt(db.dbPath, reason, db.clock.Now())
}

// IsMarkedCorrupt reports whether the corrupt marker sidecar exists.
func (db *MediaDB) IsMarkedCorrupt() bool {
	return database.IsMarkedCorrupt(db.dbPath)
}

// ClearCorruptMarker removes the corrupt marker sidecar. No-op when absent.
func (db *MediaDB) ClearCorruptMarker() error {
	if err := database.ClearCorruptMarker(db.dbPath); err != nil {
		return fmt.Errorf("media database: %w", err)
	}
	return nil
}

// NoteCorruption flags the database corrupt when err indicates SQLite corruption, so any
// path that first touches a malformed page (a read, a scraper write, a checkpoint) routes
// into the recovery flow instead of silently failing. It only writes the marker once — the
// expensive integrity report is logged later by the recovery orchestration, not on every
// failing query. Returns true when err was a corruption error.
func (db *MediaDB) NoteCorruption(err error) bool {
	return database.NoteCorruption(db.dbPath, err, db.clock.Now())
}

// IntegrityReport runs PRAGMA integrity_check and returns the result rows. It captures a
// corruption fingerprint in the logs — which are uploadable via the support bundle — when
// the database file itself cannot be retrieved. A healthy database returns a single "ok" row.
func (db *MediaDB) IntegrityReport() []string {
	sqlDB, err := db.readConn()
	if err != nil {
		return []string{"integrity check unavailable: database not connected"}
	}
	return database.IntegrityReport(db.ctx, sqlDB, database.DefaultIntegrityReportRows)
}

// Recreate discards the database file and reopens a fresh one. The connection is
// closed first; then the main file and its -wal/-shm sidecars are either preserved
// together as <db>{,-wal,-shm}.corrupt.bak forensic copies (keepBackup — development
// builds only) or deleted outright; and Open() allocates a fresh schema. Preservation
// happens after Close() so the three files are a stopped, consistent set: nothing is
// writing to any of them when they are renamed, and renaming a file SQLite still has
// open fails on Windows (see the comment at the call site). A sidecar surviving on disk
// next to the freshly allocated database would re-corrupt it, so every path still ends
// with RemoveSidecars regardless of whether keepBackup succeeded. Before any corrupt
// marker is cleared, the fresh database is marked pending for reindex and stale search
// caches are removed. This durable handoff lets startup resume if the process exits
// before the caller starts indexing. Callers: corruption recovery and the
// user-requested fresh-start rebuild (media.index with rebuild:true).
func (db *MediaDB) Recreate(keepBackup bool) error {
	// Serialize recreates: a user-triggered rebuild must never interleave its
	// close/delete/reopen with corruption recovery's (or another rebuild's).
	if !db.recreating.CompareAndSwap(false, true) {
		return errors.New("media database recreate already in progress")
	}
	defer db.recreating.Store(false)

	if err := db.Close(); err != nil {
		log.Warn().Err(err).Msg("error closing media database before recreate")
	}
	// Deliberately do not Store(nil) here: leaving the closed handle in place until
	// Open() swaps in the fresh one means a racing reader that loaded the handle
	// before this point gets a clean "database is closed" error instead of a nil
	// dereference. A guard check (Load() == nil) followed by a second Load() for the
	// query would otherwise race the swap-to-nil and panic.

	// Preserved after the close, not before it, because renaming a file SQLite
	// still has open fails on Windows: the main database and WAL are opened
	// without FILE_SHARE_DELETE and the -shm is memory-mapped, so every rename
	// here returned a sharing violation and the forensic copy was silently
	// skipped — then the corrupt database was removed below with no backup at
	// all. Nothing is lost by waiting: either the close checkpointed the WAL
	// into the main database, which is the file being preserved, or it could
	// not, and the WAL is still on disk to be preserved with it.
	//
	// All three files are kept as one set. The -shm holds nothing durable (it
	// is the WAL index, rebuilt from the WAL on demand), but a post-mortem of a
	// torn write wants the files exactly as the process last saw them, and once
	// the connection is closed its mapping is released so the rename succeeds
	// everywhere. The user database's recovery preserves the same three.
	if keepBackup {
		for _, path := range []string{db.dbPath, db.dbPath + "-wal", db.dbPath + "-shm"} {
			database.PreserveCorruptFile(path, "media")
		}
	}

	// Clear any transaction state so db.conn() can't hand out a stale closed tx after
	// the reopen below. Close has already rolled back and released any writer connection.
	db.tx = nil
	db.txConn = nil
	db.inTransaction = false

	// PreserveCorruptFile is best-effort: on a rename failure it logs and leaves
	// the file in place. Whether or not keepBackup ran (or partially succeeded),
	// db.dbPath must not still exist here — otherwise Open() below would reopen
	// the corrupt database instead of allocating a fresh one.
	if err := os.Remove(db.dbPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove corrupt media database: %w", err)
	}

	database.RemoveSidecars(db.dbPath)

	if err := db.Open(); err != nil {
		return fmt.Errorf("failed to reopen media database after recreate: %w", err)
	}

	// A fresh database only ever receives titles computed by the current
	// disambiguation algorithm, so stamp it now; otherwise the first
	// optimization pass after the rebuild would re-run a full backfill over
	// freshly computed values. Non-fatal: the worst case is that redundant pass.
	if err := sqlMarkDisambiguationVersionCurrent(db.ctx, db.sql.Load()); err != nil {
		log.Warn().Err(err).Msg("failed to stamp disambiguation version on recreated media database")
	}

	// Verify the replacement before clearing the marker. Open succeeding only proves
	// SQLite could read the schema; quick_check catches malformed pages or sidecars
	// before recovery starts writing a full index into another bad database.
	if ok, err := db.QuickCheck(); err != nil {
		return fmt.Errorf("failed to verify recreated media database: %w", err)
	} else if !ok {
		return errors.New("recreated media database failed quick_check")
	}

	// Persist reindex intent before clearing the corrupt marker. Path discovery and
	// indexing start asynchronously after Recreate returns, so a power loss or startup
	// failure in that window must not leave an empty database with no resume signal.
	if err := db.SetIndexingStatus(IndexingStatusPending); err != nil {
		return fmt.Errorf("failed to mark recreated media database pending reindex: %w", err)
	}

	// Persisted caches belong to the discarded database. A fresh DB starts with the
	// same generation value, so leaving generation-zero files behind can make stale
	// title candidates look valid even though their SQL rows no longer exist.
	db.inMemoryTagCache.Store(nil)
	db.slugSearchCache.Store(nil)
	cacheErr := errors.Join(
		removePersistedCacheFile(db.tagCachePath(), tagCacheKind),
		removePersistedCacheFile(db.slugSearchCachePath(), slugSearchCacheKind),
	)
	if cacheErr != nil {
		return fmt.Errorf("failed to clear caches after media database recreate: %w", cacheErr)
	}

	// Clear the marker only after the fresh database is verified and has durable
	// reindex intent, so every failure before this point remains recoverable on boot.
	if err := db.ClearCorruptMarker(); err != nil {
		return fmt.Errorf("failed to clear corrupt marker after media database recreate: %w", err)
	}
	return nil
}

func (db *MediaDB) SetScrapingStatus(status string) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlSetScrapingStatus(db.ctx, db.conn(), status)
}

func (db *MediaDB) GetScrapingStatus() (string, error) {
	sqlDB, err := db.readConn()
	if err != nil {
		return "", err
	}
	return sqlGetScrapingStatus(db.ctx, sqlDB)
}

func (db *MediaDB) SetScrapingOperation(operation database.ScrapingOperation) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlSetScrapingOperation(db.ctx, db.conn(), operation)
}

func (db *MediaDB) GetScrapingOperation() (database.ScrapingOperation, bool, error) {
	sqlDB, err := db.readConn()
	if err != nil {
		return database.ScrapingOperation{}, false, err
	}
	return sqlGetScrapingOperation(db.ctx, sqlDB)
}

func (db *MediaDB) ClearScrapingOperation() error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlClearScrapingOperation(db.ctx, db.conn())
}

func (db *MediaDB) SetLastIndexedSystem(systemID string) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlSetLastIndexedSystem(db.ctx, db.conn(), systemID)
}

func (db *MediaDB) GetLastIndexedSystem() (string, error) {
	sqlDB, err := db.readConn()
	if err != nil {
		return "", err
	}
	return sqlGetLastIndexedSystem(db.ctx, sqlDB)
}

// RecomputeTitleDisambiguation recomputes the stored disambiguating tag types
// for the given MediaTitle DBIDs. Callers invoke this after any write that can
// change a title's set of media or their tags (indexing, scraping, manual tag
// edits) so reads can rely on the stored, title-global value.
func (db *MediaDB) RecomputeTitleDisambiguation(ctx context.Context, titleDBIDs []int64) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlRecomputeTitleDisambiguation(ctx, db.conn(), titleDBIDs)
}

// RecomputeSystemDisambiguation recomputes the stored disambiguating tag types
// for every MediaTitle belonging to the given system DBIDs. Called at index time
// once a system is fully written so all of its titles are refreshed together.
func (db *MediaDB) RecomputeSystemDisambiguation(ctx context.Context, systemDBIDs []int64) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlRecomputeDisambiguationForSystems(ctx, db.conn(), systemDBIDs)
}

// IndexGeneration returns the monotonic counter that's bumped on every
// successful indexing run. Returns 0 if no indexing has completed yet.
func (db *MediaDB) IndexGeneration() (int64, error) {
	sqlDB, err := db.readConn()
	if err != nil {
		return 0, err
	}
	return sqlGetIndexGeneration(db.ctx, sqlDB)
}

// BumpIndexGeneration increments the index generation counter and returns
// the new value. Not transactional with cache file writes or status flips:
// crash recovery relies on (a) a generation mismatch causing persisted
// cache files from a previous run to be rejected at load time, and (b)
// IndexingStatus remaining "in_progress" until SetIndexingStatus is called,
// so an interrupted run is resumed (and re-bumped) on the next boot.
func (db *MediaDB) BumpIndexGeneration() (int64, error) {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	if db.sql.Load() == nil {
		return 0, ErrNullSQL
	}
	return sqlBumpIndexGeneration(db.ctx, db.conn())
}

func (db *MediaDB) SetIndexingSystems(systemIDs []string) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlSetIndexingSystems(db.ctx, db.conn(), systemIDs)
}

func (db *MediaDB) GetIndexingSystems() ([]string, error) {
	sqlDB, err := db.readConn()
	if err != nil {
		return nil, err
	}
	return sqlGetIndexingSystems(db.ctx, sqlDB)
}

func (db *MediaDB) SetIndexingPlanSystems(systemIDs []string) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlSetIndexingPlanSystems(db.ctx, db.conn(), systemIDs)
}

func (db *MediaDB) GetIndexingPlanSystems() ([]string, error) {
	sqlDB, err := db.readConn()
	if err != nil {
		return nil, err
	}
	return sqlGetIndexingPlanSystems(db.ctx, sqlDB)
}

func (db *MediaDB) UnsafeGetSQLDb() *sql.DB {
	return db.sql.Load()
}

func (db *MediaDB) Truncate() error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	if err := sqlTruncate(db.ctx, db.sql.Load()); err != nil {
		return err
	}
	if err := sqlInvalidateBrowseCache(db.ctx, db.sql.Load()); err != nil {
		return err
	}

	// Invalidate all caches after full truncation
	db.invalidateCaches(invalidationScope{
		AllSystems: true, UtilityTagDBIDsChanged: true, MediaRowsChanged: true,
	})

	// Reclaim disk space freed by the truncation
	if err := sqlVacuum(db.ctx, db.sql.Load()); err != nil {
		return fmt.Errorf("failed to vacuum after truncate: %w", err)
	}

	return nil
}

func (db *MediaDB) TruncateSystems(systemIDs []string) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	err := sqlTruncateSystems(db.ctx, db.sql.Load(), systemIDs)
	if err != nil {
		return err
	}
	if err := sqlInvalidateBrowseCache(db.ctx, db.sql.Load()); err != nil {
		return err
	}

	// Invalidate caches for the affected systems
	scope := invalidationScopeForMediaSystemIDs(systemIDs)
	scope.UtilityTagDBIDsChanged = true
	db.invalidateCaches(scope)
	return nil
}

func (db *MediaDB) Allocate() error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	if err := sqlAllocate(db.sql.Load(), db.dbPath); err != nil {
		return err
	}
	db.applySchemaReadyFixups()
	return nil
}

func (db *MediaDB) MigrateUp() error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	if err := sqlMigrateUp(db.sql.Load(), db.dbPath); err != nil {
		return err
	}
	db.applySchemaReadyFixups()
	// Best-effort: stamp the disambiguation version on a database with no
	// titles before the first index writes any. The pending check performs the
	// stamp as a side effect; without this, the check first runs during
	// post-index optimization — after titles exist — and a fresh install pays
	// a full backfill over values the index just computed.
	//
	// Only needed here. The other route to a fresh schema is Allocate, and both
	// of its callers already cover this: Open's fresh-database branch is
	// followed by MigrateUp at startup, and Recreate stamps the version itself
	// straight after reopening.
	if _, err := db.disambiguationBackfillPending(db.ctx); err != nil {
		log.Warn().Err(err).Msg("failed to check disambiguation backfill state after migration")
	}
	return nil
}

// applySchemaReadyFixups runs the best-effort corrections a database needs once
// its schema is current. Both are idempotent.
//
// Called from Allocate as well as MigrateUp because a brand-new database reaches
// sqlMigrateUp through Allocate — Open takes that branch when the file does not
// exist yet, and Recreate goes the same way. Recreate is the case that made this
// matter: it reopens into a fresh database and starts a reindex immediately,
// without a MigrateUp in between, so anything hooked only there was skipped for
// every user-triggered rebuild.
func (db *MediaDB) applySchemaReadyFixups() {
	// A database without real Media statistics gets the captured seed so
	// mid-index queries have sane plans; the first system commit's approximate
	// ANALYZE replaces it. Without this a rebuild reindexes against an empty
	// sqlite_stat1, which is the plan regression #1279 started from.
	if err := sqlSeedPlannerStats(db.ctx, db.sql.Load()); err != nil {
		log.Warn().Err(err).Msg("failed to seed planner statistics")
	}
	// The browse-cache migrations stamp OptimizationStatus=pending
	// unconditionally so existing databases rebuild on upgrade, which also
	// stamps a brand-new database that has nothing to rebuild. Drop it when
	// there is no media, or the next start "resumes" an optimization over an
	// empty database.
	if err := db.clearOptimizationStampIfEmpty(db.ctx); err != nil {
		log.Warn().Err(err).Msg("failed to clear optimization stamp on empty database")
	}
}

// Vacuum keeps sqlMu, unlike the metadata reads that moved to readConn: it
// rewrites the whole database rather than reading it, and no client request
// reaches it, so there is nothing to gain by letting it overlap a commit.
func (db *MediaDB) Vacuum() error {
	db.sqlMu.RLock()
	defer db.sqlMu.RUnlock()

	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlVacuum(db.ctx, db.sql.Load())
}

// CleanMediaOrphans deletes every Media row where IsMissing=1, together with
// the associated MediaTags and MediaProperties.  MediaTitles that have no
// remaining Media rows are also removed, along with their MediaTitleTags and
// MediaTitleProperties.  Tags that are no longer referenced by any join table
// are pruned. Free pages are left for SQLite to reuse instead of running
// VACUUM here, which would take an exclusive database lock.
//
// The method returns the count of Media rows deleted.  If no missing rows
// exist, it returns (0, nil) without touching the database.
//
// CleanMediaOrphans refuses to run while media indexing is in progress
// (status running or pending), while a batch transaction is open, or while
// background optimisation is active, returning a sentinel error in each case.
func (db *MediaDB) CleanMediaOrphans(ctx context.Context) (int64, error) {
	lease, err := db.AcquireMediaWrite(database.MediaWriteOperationMaintenance)
	if err != nil {
		var conflict *database.MediaWriteConflictError
		if errors.As(err, &conflict) {
			switch conflict.Active {
			case database.MediaWriteOperationIndexing:
				return 0, fmt.Errorf("%w: %w", ErrIndexingInProgress, err)
			case database.MediaWriteOperationOptimization:
				return 0, fmt.Errorf("%w: %w", ErrOptimizationInProgress, err)
			default:
			}
		}
		return 0, err
	}
	defer lease.Release()

	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	if db.sql.Load() == nil {
		return 0, ErrNullSQL
	}

	// Guard: refuse to run while a batch transaction is open.  An open
	// transaction means batch inserters may be staging rows that reference the
	// data we would delete, which would cause FK violations on commit.
	if db.inTransaction {
		return 0, ErrTransactionActive
	}

	// Guard: refuse to run while indexing is in progress.  The scanner marks
	// rows as IsMissing=1 at the start of a scan and clears the flag as files
	// are confirmed present; deleting those rows mid-scan would corrupt the
	// scanner's in-flight state.
	status, statusErr := sqlGetIndexingStatus(db.ctx, db.sql.Load())
	if statusErr != nil && !errors.Is(statusErr, sql.ErrNoRows) {
		return 0, fmt.Errorf("failed to check indexing status: %w", statusErr)
	}
	if status == IndexingStatusRunning || status == IndexingStatusPending {
		return 0, fmt.Errorf("%w (status: %s)", ErrIndexingInProgress, status)
	}

	// Guard: refuse to run while background optimisation is active, since it
	// may be building indexes over the same tables. Uses the unified signal so
	// a standalone browse-cache rebuild also blocks cleanup — otherwise cleanup
	// could delete orphans mid-rebuild and leave the freshly rebuilt browse rows
	// stale.
	if db.IsOptimizing() {
		return 0, ErrOptimizationInProgress
	}

	deleted, err := sqlCleanMediaOrphans(ctx, db.sql.Load())
	if err != nil {
		return 0, err
	}

	if _, pruneErr := db.PruneOrphanedBlobs(ctx); pruneErr != nil {
		log.Warn().Err(pruneErr).Msg("failed to prune orphaned blobs during clean")
	}

	if deleted > 0 {
		if countErr := sqlRefreshMediaCounts(ctx, db.sql.Load()); countErr != nil {
			log.Warn().Err(countErr).Msg("failed to refresh cached media counts after orphan cleanup")
			if cacheErr := sqlInvalidateMediaCountCache(
				ctx, db.sql.Load(), DBConfigMediaTotalCount, DBConfigMediaMissingCount,
			); cacheErr != nil {
				log.Warn().Err(cacheErr).Msg("failed to invalidate cached media counts after orphan cleanup")
			}
		}
		db.invalidateCaches(invalidationScope{
			AllSystems: true, UtilityTagDBIDsChanged: true, MediaRowsChanged: true,
		})
		if err := sqlInvalidateBrowseCache(ctx, db.sql.Load()); err != nil {
			log.Warn().Err(err).Msg("failed to invalidate browse cache after orphan cleanup")
		}
	}

	return deleted, nil
}

func (db *MediaDB) Close() error {
	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return nil
	}

	// Wait for all background operations (optimization, etc.) to complete
	// before closing the database connection.
	db.WaitForBackgroundOperations()

	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()
	transactionErr := db.rollbackTransactionLocked()

	logSQLTraceSummary()
	clearUtilityTagCacheFor(sqlDB)
	clearImagePropertyTagCacheFor(sqlDB)
	unregisterCoverAvailabilityCacheOwner(sqlDB)
	clearPrefixPolicyCacheFor(sqlDB)

	closeErr := sqlDB.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("failed to close database: %w", closeErr)
	}
	return errors.Join(transactionErr, closeErr)
}

func (db *MediaDB) cacheInvalidationScopeForCommittedTransaction() invalidationScope {
	allSystemsScope := func() invalidationScope {
		return invalidationScope{
			AllSystems:             true,
			UtilityTagDBIDsChanged: db.utilityTagCacheDirty,
			MediaRowsChanged:       db.mediaSearchBoundsDirty,
		}
	}

	// CommitTransaction already holds db.sqlMu, so use the SQL helpers directly
	// instead of getters that would try to take the lock again.
	status, statusErr := sqlGetIndexingStatus(db.ctx, db.sql.Load())
	if statusErr != nil {
		log.Warn().Err(statusErr).Msg("failed to determine indexing status for cache invalidation")
		return allSystemsScope()
	}

	if status != IndexingStatusRunning && status != IndexingStatusPending {
		return allSystemsScope()
	}

	systemIDs, getSystemsErr := sqlGetIndexingSystems(db.ctx, db.sql.Load())
	if getSystemsErr != nil {
		log.Warn().Err(getSystemsErr).Msg("failed to load indexing systems for cache invalidation")
		return allSystemsScope()
	}

	scope := invalidationScopeForSystemIDs(systemIDs)
	scope.UtilityTagDBIDsChanged = db.utilityTagCacheDirty
	scope.MediaRowsChanged = db.mediaSearchBoundsDirty
	return scope
}

// SetSQLForTesting allows injection of a sql.DB instance for testing purposes.
// This method should only be used in tests to set up in-memory databases.
func (db *MediaDB) SetSQLForTesting(ctx context.Context, sqlDB *sql.DB, platform platforms.Platform) error {
	db.sql.Store(sqlDB)
	clearUtilityTagCache()
	clearCoverAvailabilityCache()
	clearImagePropertyTagCache()
	clearPrefixPolicyCache()
	db.clearMediaSearchBounds()
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

// closeAllBatchInserters closes all batch inserters and sets them to nil.
func (db *MediaDB) closeAllBatchInserters() error {
	var closeErrs []error
	if db.batchInsertSystem != nil {
		if closeErr := db.batchInsertSystem.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertSystem")
			closeErrs = append(closeErrs, fmt.Errorf("batchInsertSystem: %w", closeErr))
		}
		db.batchInsertSystem = nil
	}
	if db.batchInsertMediaTitle != nil {
		if closeErr := db.batchInsertMediaTitle.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertMediaTitle")
			closeErrs = append(closeErrs, fmt.Errorf("batchInsertMediaTitle: %w", closeErr))
		}
		db.batchInsertMediaTitle = nil
	}
	if db.batchInsertMedia != nil {
		if closeErr := db.batchInsertMedia.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertMedia")
			closeErrs = append(closeErrs, fmt.Errorf("batchInsertMedia: %w", closeErr))
		}
		db.batchInsertMedia = nil
	}
	if db.batchInsertTag != nil {
		if closeErr := db.batchInsertTag.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertTag")
			closeErrs = append(closeErrs, fmt.Errorf("batchInsertTag: %w", closeErr))
		}
		db.batchInsertTag = nil
	}
	if db.batchInsertTagType != nil {
		if closeErr := db.batchInsertTagType.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertTagType")
			closeErrs = append(closeErrs, fmt.Errorf("batchInsertTagType: %w", closeErr))
		}
		db.batchInsertTagType = nil
	}
	if db.batchInsertMediaTag != nil {
		if closeErr := db.batchInsertMediaTag.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertMediaTag")
			closeErrs = append(closeErrs, fmt.Errorf("batchInsertMediaTag: %w", closeErr))
		}
		db.batchInsertMediaTag = nil
	}
	if db.batchInsertScanStage != nil {
		if closeErr := db.batchInsertScanStage.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertScanStage")
			closeErrs = append(closeErrs, fmt.Errorf("batchInsertScanStage: %w", closeErr))
		}
		db.batchInsertScanStage = nil
	}
	if db.batchInsertScanTag != nil {
		if closeErr := db.batchInsertScanTag.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertScanTag")
			closeErrs = append(closeErrs, fmt.Errorf("batchInsertScanTag: %w", closeErr))
		}
		db.batchInsertScanTag = nil
	}
	if db.batchInsertScanProperty != nil {
		if closeErr := db.batchInsertScanProperty.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch inserter: batchInsertScanProperty")
			closeErrs = append(closeErrs, fmt.Errorf("batchInsertScanProperty: %w", closeErr))
		}
		db.batchInsertScanProperty = nil
	}

	return errors.Join(closeErrs...)
}

// FlushBatchInserters flushes all pending batch-insert buffers into the open
// transaction without committing, so freshly inserted rows become visible to
// reads on the same transaction (e.g. RecomputeSystemDisambiguation). Unlike
// closeAllBatchInserters it leaves the inserters open for continued use, which
// lets one transaction span multiple systems. No-op when no transaction or no
// batch inserters are active. Each inserter's Flush already flushes its FK
// dependencies first, so the iteration order only needs to be deterministic.
func (db *MediaDB) FlushBatchInserters() error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	if db.tx == nil {
		return nil
	}

	var flushErrs []error
	for _, bi := range []*BatchInserter{
		db.batchInsertSystem,
		db.batchInsertMediaTitle,
		db.batchInsertMedia,
		db.batchInsertTag,
		db.batchInsertTagType,
		db.batchInsertMediaTag,
		db.batchInsertScanStage,
		db.batchInsertScanTag,
		db.batchInsertScanProperty,
	} {
		if bi == nil {
			continue
		}
		if flushErr := bi.Flush(); flushErr != nil {
			flushErrs = append(flushErrs, flushErr)
		}
	}
	return errors.Join(flushErrs...)
}

// StageScannedMedia appends one scanned file and its tags to the scanner
// staging tables through the batch inserters. Requires an open batch
// transaction (BeginTransaction(true)).
func (db *MediaDB) StageScannedMedia(media *database.ScanStagedMedia) error {
	if db.batchInsertScanStage == nil || db.batchInsertScanTag == nil || db.batchInsertScanProperty == nil {
		return errors.New("staging scanned media requires an open batch transaction")
	}
	if err := db.batchInsertScanStage.Add(
		media.Path, media.ParentDir, media.Slug, media.TitleName, media.SortName,
		media.SlugLength, media.SlugWordCount, media.SecondarySlug,
	); err != nil {
		return fmt.Errorf("failed to stage scanned media %s: %w", media.Path, err)
	}
	for _, tag := range media.Tags {
		if err := db.batchInsertScanTag.Add(
			media.Path, tag.Type, tags.PadTagValue(tag.Value),
		); err != nil {
			return fmt.Errorf("failed to stage scanned media tag %s:%s: %w", tag.Type, tag.Value, err)
		}
	}
	for _, property := range media.Properties {
		if err := db.batchInsertScanProperty.Add(
			media.Path, property.Type, property.Name, property.Text,
		); err != nil {
			return fmt.Errorf("failed to stage scanned media property %s:%s: %w", property.Type, property.Name, err)
		}
	}
	return nil
}

// ReconcileStagedSystem flushes the staging inserters and folds the staged scan
// of systemID into the media tables with set-based SQL, including the touched
// -title disambiguation recompute. Must run inside the scanner's open batch
// transaction; see sqlReconcileStagedSystem for the statement sequence.
func (db *MediaDB) ReconcileStagedSystem(
	ctx context.Context, systemID string, opts database.ScanReconcileOpts,
) (database.ScanReconcileStats, error) {
	if db.tx == nil || !db.inTransaction {
		return database.ScanReconcileStats{}, ErrTransactionRequired
	}
	if err := db.FlushBatchInserters(); err != nil {
		return database.ScanReconcileStats{}, fmt.Errorf(
			"failed to flush batch inserters before reconciling %s: %w", systemID, err)
	}
	stats, err := sqlReconcileStagedSystem(ctx, db.conn(), systemID, opts)
	if err != nil {
		return stats, err
	}
	if stats.MediaUpserted > 0 || stats.MediaMissing > 0 {
		db.mediaSearchBoundsDirty = true
		countKeys := []string{DBConfigMediaMissingCount}
		if stats.MediaUpserted > 0 {
			countKeys = append(countKeys, DBConfigMediaTotalCount)
		}
		if cacheErr := sqlInvalidateMediaCountCache(ctx, db.conn(), countKeys...); cacheErr != nil {
			log.Warn().Err(cacheErr).Msg("failed to invalidate cached media counts after scan reconcile")
		}
		db.markBrowseCacheDirty()
	}
	return stats, nil
}

// ClearScanStage empties the scanner staging tables. Called at the start of a
// system's scan so rows left behind by a crashed run never leak into it.
func (db *MediaDB) ClearScanStage() error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlClearScanStage(db.ctx, db.conn())
}

// SeedCanonicalTagDefinitions ensures every canonical tag type and value exists,
// using set-based anti-joined inserts.
func (db *MediaDB) SeedCanonicalTagDefinitions(ctx context.Context) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlSeedCanonicalTags(ctx, db.conn())
}

func (db *MediaDB) clearTransactionState() {
	db.tx = nil
	db.inTransaction = false
	db.clearBrowseCacheInvalidation()
	db.utilityTagCacheDirty = false
	db.mediaSearchBoundsDirty = false
}

// rollbackTransactionLocked rolls back and releases the dedicated writer
// connection. The caller must hold sqlMu.
func (db *MediaDB) rollbackTransactionLocked() error {
	if db.tx == nil {
		return db.releaseWriterConn()
	}

	db.closeAllPreparedStatements()
	batchErr := db.closeAllBatchInserters()
	rbErr := db.tx.Rollback()
	db.clearTransactionState()
	connErr := db.releaseWriterConn()
	return errors.Join(batchErr, rbErr, connErr)
}

// RollbackTransaction rolls back the current transaction and cleans up resources.
func (db *MediaDB) RollbackTransaction() error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	if err := db.rollbackTransactionLocked(); err != nil {
		return fmt.Errorf("failed to rollback transaction: %w", err)
	}
	return nil
}

// rollbackAndLogError handles setup failures while BeginTransaction holds sqlMu.
func (db *MediaDB) rollbackAndLogError() {
	if err := db.rollbackTransactionLocked(); err != nil {
		log.Error().Err(err).Msg("failed to clean up transaction during setup")
	}
}

func (db *MediaDB) BeginTransaction(batchEnabled bool) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return ErrNullSQL
	}

	if db.inTransaction || db.tx != nil || db.txConn != nil {
		return errors.New("transaction already in progress")
	}
	db.mediaSearchBoundsDirty = false

	acquireCtx, cancel := context.WithTimeout(db.ctx, connectionAcquireTimeout)
	conn, err := sqlDB.Conn(acquireCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("failed to acquire writer connection: %w", err)
	}
	cacheSize, tempStore := db.connPragmaValues()
	if err = applyConnPragmas(db.ctx, conn, cacheSize, tempStore); err != nil {
		cleanupErr := db.closeWriterConn(conn)
		return errors.Join(err, cleanupErr)
	}
	walAutoCheckpoint := db.walAutoCheckpointPages()
	if _, err = conn.ExecContext(db.ctx,
		"PRAGMA wal_autocheckpoint = "+strconv.Itoa(walAutoCheckpoint),
	); err != nil {
		cleanupErr := db.closeWriterConn(conn)
		return errors.Join(fmt.Errorf("failed to set writer WAL autocheckpoint: %w", err), cleanupErr)
	}

	tx, err := conn.BeginTx(db.ctx, nil)
	if err != nil {
		cleanupErr := db.closeWriterConn(conn)
		return errors.Join(fmt.Errorf("failed to begin transaction: %w", err), cleanupErr)
	}
	db.tx = tx
	db.txConn = conn

	// Use batch inserters if enabled, otherwise use prepared statements
	if batchEnabled {
		if ensureErr := sqlEnsureScanStagingTables(db.ctx, tx); ensureErr != nil {
			db.rollbackAndLogError()
			return ensureErr
		}

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
			[]string{"DBID", "SystemDBID", "Slug", "Name", "SlugLength", "SlugWordCount", "SecondarySlug"},
			db.batchSize, false); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to create batch inserter for media titles: %w", err)
		}

		mediaColumns := []string{"DBID", "MediaTitleDBID", "SystemDBID", "Path", "ParentDir", "SortName"}
		if db.batchInsertMedia, err = NewBatchInserterWithOptions(
			db.ctx, tx, "Media", mediaColumns, db.batchSize, false,
		); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to create batch inserter for media: %w", err)
		}

		if db.batchInsertTag, err = NewBatchInserterWithOptions(db.ctx, tx, "Tags",
			[]string{"DBID", "TypeDBID", "Tag"}, db.batchSize, false); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to create batch inserter for tags: %w", err)
		}

		if db.batchInsertTagType, err = NewBatchInserterWithOptions(db.ctx, tx, "TagTypes",
			[]string{"DBID", "Type", "IsExclusive"}, db.batchSize, false); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to create batch inserter for tag types: %w", err)
		}

		// MediaTags uses INSERT OR IGNORE - it's a link table with no dependent foreign keys
		if db.batchInsertMediaTag, err = NewBatchInserterWithOptions(db.ctx, tx, "MediaTags",
			[]string{"MediaDBID", "TagDBID"}, db.batchSize, true); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to create batch inserter for media tags: %w", err)
		}

		// Scanner staging tables: FK-free scratch rows consumed by
		// ReconcileStagedSystem. OR IGNORE dedupes a path scanned twice in
		// one run via the primary keys.
		if db.batchInsertScanStage, err = NewBatchInserterWithOptions(db.ctx, tx, "ScanStage",
			[]string{
				"Path", "ParentDir", "Slug", "TitleName", "SortName",
				"SlugLength", "SlugWordCount", "SecondarySlug",
			}, db.batchSize, true); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to create batch inserter for scan stage: %w", err)
		}
		if db.batchInsertScanTag, err = NewBatchInserterWithOptions(db.ctx, tx, "ScanStageTags",
			[]string{"Path", "TagType", "Tag"}, db.batchSize, true); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to create batch inserter for scan stage tags: %w", err)
		}
		if db.batchInsertScanProperty, err = NewBatchInserterWithOptions(db.ctx, tx, "ScanStageProperties",
			[]string{"Path", "PropertyType", "Property", "Text"}, db.batchSize, true); err != nil {
			db.rollbackAndLogError()
			return fmt.Errorf("failed to create batch inserter for scan stage properties: %w", err)
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
	return db.CommitTransactionWithOptions(database.TransactionOptions{WALCheckpoint: database.WALCheckpointAuto})
}

// slowCommitBreakdownThreshold matches the commitElapsed threshold the indexing
// loop (mediascanner.go) already warns at, so the per-segment breakdown below
// escalates to Warn on exactly the commits that already trip that alert —
// making it actionable without cross-referencing the Debug-level breakdown.
const slowCommitBreakdownThreshold = 5 * time.Second

func (db *MediaDB) CommitTransactionWithOptions(options database.TransactionOptions) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	if db.tx == nil {
		return nil // No active transaction
	}

	flushStart := time.Now()
	// Flush all batch inserters before committing (if any were created).
	if db.batchInsertSystem != nil {
		if closeErr := db.closeAllBatchInserters(); closeErr != nil {
			cleanupErr := db.rollbackTransactionLocked()
			return errors.Join(fmt.Errorf("failed to flush batch inserts: %w", closeErr), cleanupErr)
		}
	} else {
		db.closeAllPreparedStatements()
	}
	flushElapsed := time.Since(flushStart)

	// Measured immediately around tx.Commit() so a WAL-size drop with no
	// explicit checkpoint logged afterward is direct evidence that SQLite's
	// automatic checkpointing ran inside the commit itself.
	walSizeBeforeCommit := db.mediaWALSizeForLog()
	sqliteCommitStart := time.Now()
	if err := db.tx.Commit(); err != nil {
		cleanupErr := db.rollbackTransactionLocked()
		return errors.Join(fmt.Errorf("failed to commit transaction: %w", err), cleanupErr)
	}
	sqliteCommitElapsed := time.Since(sqliteCommitStart)
	walSizeAfterCommit := db.mediaWALSizeForLog()

	// Release the pinned writer before post-commit pool queries and checkpoints.
	// A cleanup failure does not turn an already-successful commit into an
	// apparent write failure; closeWriterConn discards connections it cannot reset.
	db.tx = nil
	db.inTransaction = false
	if connErr := db.releaseWriterConn(); connErr != nil {
		log.Warn().Err(connErr).Msg("failed to reset writer connection after commit")
	}

	invalidateStart := time.Now()
	// During indexing, keep last-good slug search coverage available for
	// foreground launches/searches, but still invalidate durable/count caches so
	// random queries never trust stale MediaCountCache ranges after a commit.
	indexingStatus, statusErr := sqlGetIndexingStatus(db.ctx, db.sql.Load())
	checkpointAfterCommit := shouldCheckpointAfterCommit(options.WALCheckpoint)

	var scope invalidationScope
	indexingBatchCommit := false
	switch {
	case statusErr != nil:
		log.Warn().Err(statusErr).Msg("failed to determine indexing status for cache invalidation")
		scope = invalidationScope{AllSystems: true, MediaRowsChanged: db.mediaSearchBoundsDirty}
	case indexingStatus == IndexingStatusRunning || indexingStatus == IndexingStatusPending:
		scope = db.cacheInvalidationScopeForCommittedTransaction()
		scope.PreserveSlugSearchCache = true
		scope.PreserveTagCache = true
		indexingBatchCommit = true
	default:
		scope = db.cacheInvalidationScopeForCommittedTransaction()
	}

	db.invalidateCaches(scope)
	if indexingBatchCommit {
		log.Debug().Str("status", indexingStatus).Msg("invalidated committed caches during indexing batch commit")
	}
	db.mediaSearchBoundsDirty = false

	if err := db.flushBrowseCacheInvalidation(); err != nil {
		return err
	}
	invalidateElapsed := time.Since(invalidateStart)

	// Foreground metadata writes (favorite toggles) should not block on a full
	// checkpoint. During indexing, batch commits can grow the WAL quickly, but
	// checkpointing every batch rewrites/syncs the main DB repeatedly on slow or
	// unreliable SD/exFAT storage. So the common path only checkpoints once the WAL
	// has grown past mediaWALCheckpointThreshold, bounding its size (and RAM
	// pressure) without paying the checkpoint cost on every tiny batch.
	checkpointStart := time.Now()
	if checkpointAfterCommit {
		beforeSize := db.mediaWALSizeForLog()
		if chkErr := db.runWALCheckpointForLog("transaction_commit_forced", beforeSize); chkErr != nil {
			db.NoteCorruption(chkErr)
			log.Warn().Err(chkErr).Msg("failed to run WAL checkpoint after transaction commit")
		}
	} else {
		db.checkpointLargeWAL()
	}
	checkpointElapsed := time.Since(checkpointStart)

	totalElapsed := flushElapsed + sqliteCommitElapsed + invalidateElapsed + checkpointElapsed
	breakdownEvent := log.Debug()
	if totalElapsed > slowCommitBreakdownThreshold {
		breakdownEvent = log.Warn()
	}
	breakdownEvent = logPoolStats(breakdownEvent, db.sql.Load())
	breakdownEvent.
		Dur("flush", flushElapsed).
		Dur("sqliteCommit", sqliteCommitElapsed).
		Dur("invalidate", invalidateElapsed).
		Dur("checkpoint", checkpointElapsed).
		Dur("total", totalElapsed).
		Int64("walSizeBeforeCommit", walSizeBeforeCommit).
		Int64("walSizeAfterCommit", walSizeAfterCommit).
		Msg("media database commit breakdown")

	return nil
}

func (db *MediaDB) insertSystemWithPreparedStmt(row database.System) (database.System, error) {
	return sqlInsertSystemWithPreparedStmt(db.ctx, db.stmtInsertSystem, row)
}

func (db *MediaDB) insertMediaTitleWithPreparedStmt(row *database.MediaTitle) (database.MediaTitle, error) {
	return sqlInsertMediaTitleWithPreparedStmt(db.ctx, db.stmtInsertMediaTitle, row)
}

func (db *MediaDB) insertMediaWithPreparedStmt(row *database.Media) (database.Media, error) {
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

// Analyze keeps sqlMu for the same reason as Vacuum: it writes planner
// statistics and sits off the client request path.
func (db *MediaDB) Analyze() error {
	db.sqlMu.RLock()
	defer db.sqlMu.RUnlock()

	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlAnalyze(db.ctx, db.sql.Load())
}

// checkpointLargeWAL truncates the WAL back to empty once it has grown past
// mediaWALCheckpointThreshold. It is called from CommitTransactionWithOptions after a
// successful commit, so the caller already holds sqlMu and db.tx is nil — it execs the
// checkpoint directly rather than re-entering WALCheckpoint (which would re-lock sqlMu).
// Gating on WAL size keeps the SD/exFAT checkpoint cost off the common path of many tiny
// batches while bounding peak WAL size — and the page-cache / shmem pressure that rides on
// it — across a long multi-system index.
func (db *MediaDB) checkpointLargeWAL() {
	beforeSize := db.mediaWALSizeForLog()
	if beforeSize < mediaWALCheckpointThreshold {
		return
	}
	if chkErr := db.runWALCheckpointForLog("indexing_batch_threshold", beforeSize); chkErr != nil {
		db.NoteCorruption(chkErr)
		log.Warn().
			Err(chkErr).
			Str("path", db.dbPath+"-wal").
			Int64("walSizeBefore", beforeSize).
			Msg("failed to checkpoint large media database WAL during indexing batch commit")
		return
	}
}

// logPoolStats attaches the pool's current connection counts to event. A
// checkpoint can only reclaim WAL frames up to the oldest connection still
// holding an open read snapshot; if inUse is ever above 1 while indexing
// holds the writer, that's direct evidence something else was checked out
// at the same moment — see runWALCheckpointForLog.
func logPoolStats(event *zerolog.Event, sqlDB *sql.DB) *zerolog.Event {
	if sqlDB == nil {
		return event
	}
	stats := sqlDB.Stats()
	return event.
		Int("poolOpen", stats.OpenConnections).
		Int("poolInUse", stats.InUse).
		Int("poolIdle", stats.Idle)
}

func (db *MediaDB) mediaWALSizeForLog() int64 {
	if db.dbPath == "" {
		return 0
	}
	walPath := db.dbPath + "-wal"
	info, err := os.Stat(walPath)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		log.Debug().Err(err).Str("path", walPath).Msg("failed to stat media database WAL")
		return 0
	}
	return info.Size()
}

func (db *MediaDB) runWALCheckpointForLog(reason string, walSizeBefore int64) error {
	checkpointStart := time.Now()
	var busy, logFrames, checkpointedFrames int
	if err := db.sql.Load().QueryRowContext(db.ctx, "PRAGMA wal_checkpoint(TRUNCATE);").
		Scan(&busy, &logFrames, &checkpointedFrames); err != nil {
		return fmt.Errorf("WAL checkpoint failed: %w", err)
	}
	elapsed := time.Since(checkpointStart)
	walSizeAfter := db.mediaWALSizeForLog()
	logEvent := log.Debug()
	if busy > 0 || walSizeAfter >= mediaWALCheckpointThreshold {
		logEvent = log.Warn()
	}
	// logFrames-checkpointedFrames is how many WAL frames a reader's open snapshot
	// blocked reclaiming; the pool stats show whether a connection besides the
	// writer was checked out at that moment — together they're the #1279
	// stuck-checkpoint diagnosis this threshold being reachable now enables.
	logEvent = logPoolStats(logEvent, db.sql.Load())
	logEvent.
		Str("reason", reason).
		Str("path", db.dbPath+"-wal").
		Dur("elapsed", elapsed).
		Int64("walSizeBefore", walSizeBefore).
		Int64("walSizeAfter", walSizeAfter).
		Int64("threshold", mediaWALCheckpointThreshold).
		Int("busy", busy).
		Int("logFrames", logFrames).
		Int("checkpointedFrames", checkpointedFrames).
		Msg("media database WAL checkpoint completed")
	return nil
}

// WALCheckpoint forces a WAL checkpoint to flush pending writes to the main database file.
// It holds sqlMu so the TRUNCATE checkpoint is serialized with commits and other writes
// (matching the in-commit checkpoint in CommitTransactionWithOptions), and is a no-op
// while a transaction is open — truncating the WAL out from under an active writer on the
// other pool connection would only ever return SQLITE_BUSY.
func (db *MediaDB) WALCheckpoint() error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	if db.tx != nil {
		return nil
	}
	return db.runWALCheckpointForLog("manual", db.mediaWALSizeForLog())
}

// BrowseDirectories returns distinct immediate subdirectory names under the given path prefix.
// Browse call timing thresholds. Above the first a call is reported at info so
// it survives the default log level; above the second it is a warn.
// Vars, not consts, only so tests can lower them; production never mutates.
var (
	browseTimingLogThreshold  = 250 * time.Millisecond
	browseTimingWarnThreshold = 5 * time.Second
)

// browseCall pins one pooled connection for the duration of a browse request
// and records how long it took to get one.
//
// Two reasons it acquires explicitly rather than handing sqlBrowse* the pool.
// First attribution: a browse blocked waiting for a connection and one running
// expensive SQL are indistinguishable in the logs otherwise, and pool-wide
// wait counters cannot separate this caller's wait from any other goroutine's.
// Second cost: a single browse request issues several statements, and against
// the pool each one queues for a connection independently — a browse was
// observed making dozens of acquisitions while the pool sat saturated. One
// connection for the whole request replaces those with a single wait, and the
// statements then see a consistent snapshot as a side benefit.
type browseCall struct {
	started time.Time
	conn    *sql.Conn
	op      string
	wait    time.Duration
	routes  int
}

// beginBrowse acquires the request's connection. The caller must always call
// finish, including on error, so the connection is released.
func (db *MediaDB) beginBrowse(ctx context.Context, op string, routes int) (*browseCall, error) {
	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return nil, ErrNullSQL
	}
	started := time.Now()
	conn, err := sqlDB.Conn(ctx)
	wait := time.Since(started)
	if err != nil {
		return nil, fmt.Errorf("browse %s: failed to acquire connection after %v: %w", op, wait, err)
	}
	return &browseCall{conn: conn, op: op, routes: routes, started: started, wait: wait}, nil
}

func (c *browseCall) finish(db *MediaDB) {
	if err := c.conn.Close(); err != nil {
		log.Warn().Err(err).Str("op", c.op).Msg("failed to release browse connection")
	}
	elapsed := time.Since(c.started)
	if elapsed < browseTimingLogThreshold {
		return
	}
	sqlDB := db.sql.Load()
	event := log.Info()
	if elapsed > browseTimingWarnThreshold {
		event = log.Warn()
	}
	event = logPoolStats(event, sqlDB)
	event = event.
		Str("op", c.op).
		Dur("elapsed", elapsed).
		// connWait is this call's own queueing time, measured across its single
		// acquisition, so work is always elapsed minus it and never negative.
		Dur("connWait", c.wait).
		Dur("work", elapsed-c.wait).
		Bool("boosted", db.indexingCacheBoost.Load())
	if c.routes > 0 {
		// The overlay query joins Media once per source route and re-checks
		// higher-priority routes per candidate row, so cost rises faster than
		// route count does. Reported here so the two are comparable on one line.
		event = event.Int("routes", c.routes)
	}
	event.Msg("browse call timing")
}

func (db *MediaDB) BrowseDirectories(
	ctx context.Context, opts database.BrowseDirectoriesOptions,
) ([]database.BrowseDirectoryResult, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	call, err := db.beginBrowse(ctx, "browse directories", len(browseOverlaySources(opts.Overlay)))
	if err != nil {
		return nil, err
	}
	defer call.finish(db)
	results, err := sqlBrowseDirectories(ctx, call.conn, opts)
	db.NoteCorruption(err)
	return results, err
}

// BrowseFiles returns indexed media files that are immediate children of the given path prefix.
// A malformed-page error here (the cover-flags join reads MediaTitleProperties, where scraped
// artwork lives) flags the database corrupt so the recovery flow rebuilds it.
func (db *MediaDB) BrowseFiles(
	ctx context.Context, opts *database.BrowseFilesOptions,
) ([]database.SearchResultWithCursor, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	call, err := db.beginBrowse(ctx, "browse files", len(browseOverlaySources(opts.Overlay)))
	if err != nil {
		return nil, err
	}
	defer call.finish(db)
	results, err := sqlBrowseFiles(ctx, call.conn, opts)
	db.NoteCorruption(err)
	return results, err
}

// GetMediaCoverStatus reports image-property availability at media or title scope.
func (db *MediaDB) GetMediaCoverStatus(
	ctx context.Context, refs []database.MediaRef,
) (map[int64]bool, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	if coverIndex := cachedCoverAvailabilityIndex(db.sql.Load()); coverIndex != nil {
		statuses := make(map[int64]bool, len(refs))
		for _, ref := range refs {
			if ref.MediaDBID <= 0 {
				continue
			}
			statuses[ref.MediaDBID] = coverIndex.hasTitle(ref.MediaTitleDBID) ||
				coverIndex.hasMedia(ref.MediaDBID)
		}
		return statuses, nil
	}
	statuses, err := fetchCoverStatuses(ctx, db.sql.Load(), refs)
	db.NoteCorruption(err)
	return statuses, err
}

// BrowseFileCount returns the total number of immediate child files under a path prefix.
func (db *MediaDB) BrowseFileCount(
	ctx context.Context,
	opts database.BrowseFileCountOptions, //nolint:gocritic // interface keeps browse option values consistent
) (int, error) {
	if db.sql.Load() == nil {
		return 0, ErrNullSQL
	}
	call, err := db.beginBrowse(ctx, "browse file count", len(browseOverlaySources(opts.Overlay)))
	if err != nil {
		return 0, err
	}
	defer call.finish(db)
	return sqlBrowseFileCount(ctx, call.conn, opts)
}

// BrowseDirCount returns the total number of immediate child directories under a path prefix.
func (db *MediaDB) BrowseDirCount(
	ctx context.Context, opts database.BrowseDirCountOptions,
) (int, error) {
	if db.sql.Load() == nil {
		return 0, ErrNullSQL
	}
	count, err := sqlBrowseDirCount(ctx, db.sql.Load(), opts)
	db.NoteCorruption(err)
	return count, err
}

// BrowseIndex returns the ordered first-character buckets for a browse scope,
// each with a count and the keyset needed to seek a media.browse page to the
// bucket's first item.
//
//nolint:gocritic // Value options preserve the established MediaDBI method contract.
func (db *MediaDB) BrowseIndex(
	ctx context.Context, opts database.BrowseIndexOptions,
) (database.BrowseIndexResult, error) {
	if db.sql.Load() == nil {
		return database.BrowseIndexResult{}, ErrNullSQL
	}
	call, err := db.beginBrowse(ctx, "browse index", len(browseOverlaySources(opts.Overlay)))
	if err != nil {
		return database.BrowseIndexResult{}, err
	}
	defer call.finish(db)
	return sqlBrowseIndex(ctx, call.conn, &opts)
}

// BrowseVirtualSchemes returns distinct URI schemes present in indexed media.
func (db *MediaDB) BrowseVirtualSchemes(
	ctx context.Context, opts database.BrowseVirtualSchemesOptions,
) ([]database.BrowseVirtualScheme, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	return sqlBrowseVirtualSchemes(ctx, db.sql.Load(), opts)
}

// BrowseRootCounts returns a map of root directory to count of indexed media
// under each root. A nil *int means the count is not yet available (cache not
// populated). A non-nil *int is the actual count (which may be 0).
func (db *MediaDB) BrowseRootCounts(
	ctx context.Context, rootDirs []string,
) (map[string]*int, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	call, err := db.beginBrowse(ctx, "browse root counts", len(rootDirs))
	if err != nil {
		return nil, err
	}
	defer call.finish(db)
	return sqlBrowseRootCounts(ctx, call.conn, rootDirs)
}

// BrowseRouteCounts returns populated route counts for system-scoped browse roots.
func (db *MediaDB) BrowseRouteCounts(
	ctx context.Context, opts database.BrowseRouteCountsOptions,
) (map[string]database.BrowseRouteCount, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	call, err := db.beginBrowse(ctx, "browse route counts", 0)
	if err != nil {
		return nil, err
	}
	defer call.finish(db)
	return sqlBrowseRouteCounts(ctx, call.conn, opts)
}

// BrowseSystemRootCandidates returns, in two batched queries, the immediate
// child subdirs of each root that hold media for the requested systems
// plus a per-root has-any-subtree-media flag. Used by the system-roots
// browse handler to avoid a per-root query fan-out.
func (db *MediaDB) BrowseSystemRootCandidates(
	ctx context.Context, opts database.BrowseSystemRootCandidatesOptions,
) (database.BrowseSystemRootCandidates, bool, error) {
	if db.sql.Load() == nil {
		return database.BrowseSystemRootCandidates{}, false, ErrNullSQL
	}
	call, err := db.beginBrowse(ctx, "browse system root candidates", len(opts.Roots))
	if err != nil {
		return database.BrowseSystemRootCandidates{}, false, err
	}
	defer call.finish(db)
	return sqlBrowseSystemRootCandidates(ctx, call.conn, opts)
}

// PopulateBrowseCache rebuilds the BrowseCache table from the current Media data.
//
// Uses a non-cancellable context: the rebuild reads every non-missing Media row
// (~1M on large libraries), and with a cancellable context mattn/go-sqlite3
// spawns a goroutine + channel per rows.Next(), which turned this scan into a
// 7.6-minute operation on device. Cancellation before the DB work is still
// honoured by the caller's pauser.Wait; see checkAndHealBrowseCache.
func (db *MediaDB) PopulateBrowseCache(ctx context.Context) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlPopulateBrowseCache(context.WithoutCancel(ctx), db.sql.Load())
}

// PopulateBrowseCacheForSystems incrementally refreshes browse cache rows for
// the given systems from committed media. Called after each system's commit
// during indexing so browse serves fresh results mid-scan without waiting for
// the end-of-run full rebuild. Systems without a DB row yet are skipped.
// Uses a non-cancellable context for the same driver reason as
// PopulateBrowseCache.
func (db *MediaDB) PopulateBrowseCacheForSystems(ctx context.Context, systemIDs []string) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	if len(systemIDs) == 0 {
		return nil
	}

	plainCtx := context.WithoutCancel(ctx)
	placeholders := make([]string, len(systemIDs))
	args := make([]any, len(systemIDs))
	for i, id := range systemIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := db.sql.Load().QueryContext(plainCtx,
		"SELECT DBID FROM Systems WHERE SystemID IN ("+strings.Join(placeholders, ",")+")", args...)
	if err != nil {
		return fmt.Errorf("browse cache: failed to resolve system DBIDs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	systemDBIDs := make([]int64, 0, len(systemIDs))
	for rows.Next() {
		var dbid int64
		if scanErr := rows.Scan(&dbid); scanErr != nil {
			return fmt.Errorf("browse cache: failed to scan system DBID: %w", scanErr)
		}
		systemDBIDs = append(systemDBIDs, dbid)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("browse cache: system DBID iteration error: %w", rowsErr)
	}

	return sqlPopulateBrowseCacheForSystems(plainCtx, db.sql.Load(), systemDBIDs)
}

// BrowseCacheNeedsRebuild reports whether the browse cache is stale or absent and
// should be rebuilt. It returns false only when the cache is fresh (matches the
// current schema version). A stale-but-present cache is served (see the browse
// dispatch in sql_browse.go) but still needs a background rebuild to correct any
// drift, so it counts as needing a rebuild here.
func (db *MediaDB) BrowseCacheNeedsRebuild(ctx context.Context) (bool, error) {
	if db.sql.Load() == nil {
		return false, ErrNullSQL
	}
	state, err := sqlBrowseCacheStatus(ctx, db.sql.Load())
	if err != nil {
		return false, err
	}
	return state != browseCacheFresh, nil
}

// SearchMediaPathExact returns indexed names matching an exact query (case-insensitive).
func (db *MediaDB) SearchMediaPathExact(
	ctx context.Context, systems []systemdefs.System, query string,
) ([]database.SearchResult, error) {
	if db.sql.Load() == nil {
		return make([]database.SearchResult, 0), ErrNullSQL
	}
	return sqlSearchMediaPathExact(ctx, db.sql.Load(), systems, query)
}

func (db *MediaDB) SearchMediaWithFilters(
	ctx context.Context,
	filters *database.SearchFilters,
) ([]database.SearchResultWithCursor, error) {
	if db.sql.Load() == nil {
		return make([]database.SearchResultWithCursor, 0), ErrNullSQL
	}

	qWords := strings.Fields(filters.Query)
	if len(qWords) == 0 || len(filters.Systems) == 0 {
		return sqlSearchMediaWithFiltersSorted(
			ctx, db.sql.Load(), filters.Systems, nil, qWords, filters.PathPrefix, filters.Tags,
			filters.Letter, filters.Cursor, filters.SortCursor, filters.Sort, filters.Limit, false)
	}

	searchSystems := filters.Systems
	if strings.Contains(filters.PathPrefix, "://") && requestedAllSystems(filters.Systems) {
		var err error
		searchSystems, err = mediaSearchSystemsForPathPrefix(ctx, db.sql.Load(), filters.PathPrefix)
		if err != nil {
			return nil, err
		}
		if len(searchSystems) == 0 {
			return []database.SearchResultWithCursor{}, nil
		}
	}

	groups := buildMediaSearchTypeGroups(searchSystems, qWords)
	systemIDs := make([]string, len(searchSystems))
	for i := range searchSystems {
		systemIDs[i] = searchSystems[i].ID
	}

	cache := db.slugSearchCache.Load()
	cacheReady := cache != nil && cache.CanServeSystems(systemIDs)
	cacheableGroups := mediaSearchTypeGroupsCacheable(groups)
	if cacheReady && cacheableGroups {
		candidateStarted := time.Now()
		candidateIDs := searchMediaTypeGroupsInCache(cache, groups)
		candidateDuration := time.Since(candidateStarted)
		log.Debug().
			Strs("systems", systemIDs).
			Int("mediaTypeGroups", len(groups)).
			Int("candidates", len(candidateIDs)).
			Dur("duration", candidateDuration).
			Msg("media search in-memory candidate timing")
		if len(candidateIDs) == 0 {
			return []database.SearchResultWithCursor{}, nil
		}

		queryParams := titleDBIDQueryParamCount(len(candidateIDs), filters)
		if queryParams <= sqliteMaxParams {
			log.Debug().
				Strs("systems", systemIDs).
				Int("mediaTypeGroups", len(groups)).
				Int("candidates", len(candidateIDs)).
				Msg("media search using in-memory slug cache")
			return sqlSearchMediaByTitleDBIDsSorted(
				ctx, db.sql.Load(), candidateIDs, filters.PathPrefix, filters.Tags,
				filters.Letter, filters.Cursor, filters.SortCursor, filters.Sort, filters.Limit)
		}

		canStreamCandidates := filters.Sort == "" && filters.SortCursor == nil && filters.PathPrefix == "" &&
			len(filters.Tags) == 0 && filters.Letter == nil && len(candidateIDs) >= filters.Limit
		if canStreamCandidates {
			var streamResults []database.SearchResultWithCursor
			var streamErr error
			strategy := ""
			switch {
			case requestedAllSystems(searchSystems):
				strategy = "global"
				streamResults, streamErr = sqlSearchMediaByLargeTitleDBIDSet(
					ctx, db.sql.Load(), candidateIDs, filters.PathPrefix, filters.Tags,
					filters.Letter, filters.Cursor, filters.Limit)
			case len(searchSystems) > 0 && len(searchSystems) <= maxScopedStreamSystems:
				resolvedDBIDs := cache.ResolveSystemDBIDs(systemIDs)
				if len(resolvedDBIDs) == len(systemIDs) {
					scopedSystems := make(map[int64]string, len(resolvedDBIDs))
					var bounds mediaDBIDBounds
					boundsReady := true
					for i, systemDBID := range resolvedDBIDs {
						systemBounds, found, boundsErr := db.getMediaSearchBounds(ctx, systemDBID)
						if boundsErr != nil {
							log.Debug().Err(boundsErr).
								Int64("systemDBID", systemDBID).
								Msg("media search bounds unavailable; using grouped SQL")
							boundsReady = false
							break
						}
						if !found {
							continue
						}
						scopedSystems[systemDBID] = systemIDs[i]
						if bounds.first == 0 || systemBounds.first < bounds.first {
							bounds.first = systemBounds.first
						}
						bounds.last = max(bounds.last, systemBounds.last)
					}
					if boundsReady && len(scopedSystems) == 0 {
						return []database.SearchResultWithCursor{}, nil
					}
					if boundsReady {
						strategy = "system-scope"
						streamResults, streamErr = sqlSearchMediaByLargeTitleDBIDSetInSystems(
							ctx, db.sql.Load(), candidateIDs, scopedSystems, bounds,
							filters.Cursor, filters.Limit)
					}
				}
			}
			if strategy != "" {
				log.Debug().
					Strs("systems", systemIDs).
					Str("strategy", strategy).
					Int("mediaTypeGroups", len(groups)).
					Int("candidates", len(candidateIDs)).
					Msg("media search streaming large in-memory candidate set")
				if streamErr == nil {
					return streamResults, nil
				}
				if !errors.Is(streamErr, errSearchCandidateSetTooSparse) {
					return nil, streamErr
				}
				log.Debug().
					Str("strategy", strategy).
					Msg("media search candidate stream too sparse; falling back to grouped SQL")
			}
		}

		log.Debug().
			Int("candidates", len(candidateIDs)).
			Int("queryParams", queryParams).
			Int("maxQueryParams", sqliteMaxParams).
			Str("sort", filters.Sort).
			Msg("media search cache candidates exceed SQLite parameter budget")
	}

	// A cache that covered the whole library and is only missing the systems
	// currently being re-indexed does not need a wholesale fallback: serve the
	// covered systems from memory and scope the SQL to the few in flight. On
	// the #1279 device the alternative was eight grouped LIKE queries across
	// 293 systems for every search, every one of which hit the API timeout.
	if cacheableGroups && !cacheReady {
		if cached, viaSQL, ok := cache.PartitionServableSystems(systemIDs); ok && len(cached) > 0 {
			log.Debug().
				Int("cachedSystems", len(cached)).
				Int("sqlSystems", len(viaSQL)).
				Msg("media search splitting between cache and scoped SQL")
			return db.searchSplitAcrossCacheAndSQL(ctx, cached, viaSQL, qWords, filters)
		}
	}

	// Search each media type separately so one type's normalization does not
	// broaden matches in unrelated systems.
	log.Debug().
		Strs("systems", systemIDs).
		Bool("cachePresent", cache != nil).
		Bool("cacheReady", cacheReady).
		Bool("cacheableGroups", cacheableGroups).
		Int("mediaTypeGroups", len(groups)).
		Msg("media search falling back to grouped SQL LIKE path")
	return db.searchMediaTypeGroupsWithSQL(ctx, groups, qWords, filters)
}

// slugCacheSearch is a shared helper for slug-based search methods that use the
// in-memory cache. It handles slugification, system resolution, and SQL fallback.
// Returns (results, true, nil) on cache hit, or (nil, false, nil) when the caller
// should fall back to the SQL-only path.
func (db *MediaDB) slugCacheSearch(
	ctx context.Context,
	systemID string,
	input string,
	tagFilters []zapscript.TagFilter,
	matchFn func(*SlugSearchCache, []int64, []byte) []int64,
) ([]database.SearchResultWithCursor, bool, error) {
	cache := db.slugSearchCache.Load()
	if cache == nil || !cache.CanServeSystems([]string{systemID}) {
		log.Debug().
			Str("system", systemID).
			Bool("cachePresent", cache != nil).
			Msg("slug search falling back to SQL path")
		return nil, false, nil
	}
	mediaType := slugs.MediaTypeGame
	if system, err := systemdefs.GetSystem(systemID); err == nil && system != nil {
		mediaType = system.GetMediaType()
	}
	slugified := []byte(slugs.Slugify(mediaType, input))
	if len(slugified) == 0 {
		return []database.SearchResultWithCursor{}, true, nil
	}
	systemDBIDs := cache.ResolveSystemDBIDs([]string{systemID})
	if len(systemDBIDs) == 0 {
		return []database.SearchResultWithCursor{}, true, nil
	}
	candidates := matchFn(cache, systemDBIDs, slugified)
	if len(candidates) == 0 {
		return []database.SearchResultWithCursor{}, true, nil
	}
	// Slug cache narrows title candidates only. Apply tag filters in the bounded
	// media query so cache hits preserve SQL-fallback semantics before LIMIT.
	results, err := sqlSearchMediaByTitleDBIDs(
		ctx, db.sql.Load(), candidates, tagFilters, nil, nil, defaultSlugSearchLimit)
	log.Debug().
		Str("system", systemID).
		Int("candidates", len(candidates)).
		Msg("slug search using in-memory slug cache")
	return results, true, err
}

func (db *MediaDB) SearchMediaBySlug(
	ctx context.Context, systemID string, slug string, tagFilters []zapscript.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	if db.sql.Load() == nil {
		return make([]database.SearchResultWithCursor, 0), ErrNullSQL
	}
	if results, ok, err := db.slugCacheSearch(ctx, systemID, slug, tagFilters,
		(*SlugSearchCache).ExactSlugMatch); ok || err != nil {
		return results, err
	}
	return sqlSearchMediaBySlug(ctx, db.sql.Load(), systemID, slug, tagFilters)
}

func (db *MediaDB) SearchMediaBySecondarySlug(
	ctx context.Context, systemID string, secondarySlug string, tagFilters []zapscript.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	if db.sql.Load() == nil {
		return make([]database.SearchResultWithCursor, 0), ErrNullSQL
	}
	if results, ok, err := db.slugCacheSearch(ctx, systemID, secondarySlug, tagFilters,
		(*SlugSearchCache).ExactSecondarySlugMatch); ok || err != nil {
		return results, err
	}
	return sqlSearchMediaBySecondarySlug(ctx, db.sql.Load(), systemID, secondarySlug, tagFilters)
}

func (db *MediaDB) SearchMediaBySlugPrefix(
	ctx context.Context, systemID string, slugPrefix string, tagFilters []zapscript.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	if db.sql.Load() == nil {
		return make([]database.SearchResultWithCursor, 0), ErrNullSQL
	}
	if results, ok, err := db.slugCacheSearch(ctx, systemID, slugPrefix, tagFilters,
		(*SlugSearchCache).PrefixSlugMatch); ok || err != nil {
		return results, err
	}
	return sqlSearchMediaBySlugPrefix(ctx, db.sql.Load(), systemID, slugPrefix, tagFilters)
}

// SearchMediaBySlugIn searches for media items matching any of the provided slugs using an IN clause.
// This is optimized for searching multiple slug candidates in a single query.
func (db *MediaDB) SearchMediaBySlugIn(
	ctx context.Context, systemID string, slugList []string, tagFilters []zapscript.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	if db.sql.Load() == nil {
		return make([]database.SearchResultWithCursor, 0), ErrNullSQL
	}

	cache := db.slugSearchCache.Load()
	if cache != nil && cache.CanServeSystems([]string{systemID}) {
		mediaType := slugs.MediaTypeGame
		if system, err := systemdefs.GetSystem(systemID); err == nil && system != nil {
			mediaType = system.GetMediaType()
		}
		slugBytes := make([][]byte, 0, len(slugList))
		for _, s := range slugList {
			slugified := slugs.Slugify(mediaType, s)
			if slugified != "" {
				slugBytes = append(slugBytes, []byte(slugified))
			}
		}
		if len(slugBytes) == 0 {
			return []database.SearchResultWithCursor{}, nil
		}
		systemDBIDs := cache.ResolveSystemDBIDs([]string{systemID})
		if len(systemDBIDs) == 0 {
			return []database.SearchResultWithCursor{}, nil
		}
		candidates := cache.ExactSlugMatchAny(systemDBIDs, slugBytes)
		if len(candidates) == 0 {
			return []database.SearchResultWithCursor{}, nil
		}
		return sqlSearchMediaByTitleDBIDs(ctx, db.sql.Load(), candidates, tagFilters, nil, nil, defaultSlugSearchLimit)
	}

	return sqlSearchMediaBySlugIn(ctx, db.sql.Load(), systemID, slugList, tagFilters)
}

// GetTitlesWithPreFilter retrieves media titles filtered by slug length and word count ranges.
// This dramatically reduces the candidate set for fuzzy matching by using indexed pre-filter columns.
// Uses the composite index idx_media_prefilter (SlugLength, SlugWordCount) for efficient range queries.
func (db *MediaDB) GetTitlesWithPreFilter(
	ctx context.Context, systemID string, minLength, maxLength, minWordCount, maxWordCount int,
) ([]database.MediaTitle, error) {
	if db.sql.Load() == nil {
		return make([]database.MediaTitle, 0), ErrNullSQL
	}

	// Look up system DBID from committed state. This path is used by foreground
	// title resolution and must not read the active indexing transaction.
	system, err := sqlFindSystemBySystemID(ctx, db.sql.Load(), systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to find system '%s': %w", systemID, err)
	}

	return sqlGetCandidatesWithPreFilter(ctx, db.sql.Load(), system.DBID, PreFilterQuery{
		MinLength:    minLength,
		MaxLength:    maxLength,
		MinWordCount: minWordCount,
		MaxWordCount: maxWordCount,
	})
}

func (db *MediaDB) GetTags(ctx context.Context, systems []systemdefs.System) ([]database.TagInfo, error) {
	if db.sql.Load() == nil {
		return make([]database.TagInfo, 0), ErrNullSQL
	}
	return sqlGetTags(ctx, db.sql.Load(), systems)
}

// GetAllUsedTags returns all tags that are actually in use (have media associated)
// This is optimized for the "all systems" case and avoids expensive system filtering
func (db *MediaDB) GetAllUsedTags(ctx context.Context) ([]database.TagInfo, error) {
	if db.sql.Load() == nil {
		return make([]database.TagInfo, 0), ErrNullSQL
	}
	if cache := db.inMemoryTagCache.Load(); cache != nil {
		return slices.Clone(cache.allTags), nil
	}
	return sqlGetAllUsedTags(ctx, db.sql.Load())
}

// PopulateSystemTagsCache rebuilds the cache table for fast tag lookups by system
// This should be called after media indexing completes
func (db *MediaDB) PopulateSystemTagsCache(ctx context.Context) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	// Unlike every other MediaDB write path, this used to BeginTx unconditionally
	// against the pool. With _txlock=immediate in the DSN, that grabs SQLite's
	// single WAL writer lock at BEGIN itself — while indexing holds it for a
	// whole system's commit, a concurrent caller (this can self-heal from a
	// plain read via GetSystemTagsCached) would block for the full busy_timeout
	// pinning a reader mark at a stale WAL position the whole time. Fail fast
	// instead, matching applyMediaTagMutations.
	if db.inTransaction {
		return ErrTransactionActive
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return sqlPopulateSystemTagsCache(ctx, db.sql.Load())
}

// PopulateSystemTagsCacheForSystems rebuilds cache for specific systems only
// Used for incremental cache updates after individual system changes
func (db *MediaDB) PopulateSystemTagsCacheForSystems(ctx context.Context, systems []systemdefs.System) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	// See PopulateSystemTagsCache above for why this guards against a
	// concurrent indexing transaction rather than blocking on it.
	if db.inTransaction {
		return ErrTransactionActive
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return sqlPopulateSystemTagsCacheForSystems(ctx, db.sql.Load(), systems)
}

// GetSystemTagsCached retrieves tags for specific systems using the cache table
// Falls back to the optimized subquery approach if cache is empty
func (db *MediaDB) GetSystemTagsCached(ctx context.Context, systems []systemdefs.System) ([]database.TagInfo, error) {
	if db.sql.Load() == nil {
		return make([]database.TagInfo, 0), ErrNullSQL
	}
	if len(systems) == 0 {
		return nil, errors.New("no systems provided for cached tag search")
	}

	// Try in-memory cache first (instant)
	if cache := db.inMemoryTagCache.Load(); cache != nil {
		return cache.tagsForSystems(systems), nil
	}

	// Fallback: try SQL cached approach
	cachedTags, err := sqlGetSystemTagsCached(ctx, db.sql.Load(), systems)
	if err != nil {
		log.Warn().Err(err).Msg("failed to get cached tags, falling back to optimized query")
		// Fallback to optimized subquery approach
		return sqlGetTags(ctx, db.sql.Load(), systems)
	}

	// If cache is empty (no results), auto-populate for requested systems (self-healing)
	if len(cachedTags) == 0 {
		log.Debug().Int("system_count", len(systems)).Msg("cache miss, populating for requested systems")

		// Self-healing: populate cache for requested systems
		if populateErr := db.PopulateSystemTagsCacheForSystems(ctx, systems); populateErr != nil {
			log.Warn().Err(populateErr).Msg("failed to populate cache, using direct query")
			return sqlGetTags(ctx, db.sql.Load(), systems)
		}

		// Populate succeeded — re-read from cache table instead of running
		// another expensive 6-table join query via sqlGetTags
		cachedTags, err = sqlGetSystemTagsCached(ctx, db.sql.Load(), systems)
		if err != nil || len(cachedTags) == 0 {
			return sqlGetTags(ctx, db.sql.Load(), systems)
		}

		// Rebuild in-memory cache so subsequent requests are instant
		go func() {
			if cacheErr := db.RebuildTagCache(); cacheErr != nil {
				log.Warn().Err(cacheErr).Msg("failed to rebuild tag cache after self-healing")
			}
		}()
	}

	return cachedTags, nil
}

// InvalidateSystemTagsCache removes cache entries for specific systems
// Useful for incremental cache updates when only certain systems change
// If no systems are provided, this is a no-op and returns success.
func (db *MediaDB) InvalidateSystemTagsCache(ctx context.Context, systems []systemdefs.System) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}

	if len(systems) == 0 {
		return nil // No-op for empty systems list
	}

	return sqlInvalidateSystemTagsCache(ctx, db.sql.Load(), systems)
}

func (db *MediaDB) SearchMediaPathGlob(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	// TODO: glob pattern matching unclear on some patterns
	// query == path like with possible *
	var nullResults []database.SearchResult
	if db.sql.Load() == nil {
		return nullResults, ErrNullSQL
	}

	// Collect unique MediaTypes from the systems being searched
	uniqueMediaTypes := make(map[slugs.MediaType]struct{})
	for _, system := range systems {
		uniqueMediaTypes[system.GetMediaType()] = struct{}{}
	}

	// Generate slug variants for each glob part based on MediaTypes present
	var variantGroups [][]string
	for _, part := range strings.Split(query, "*") {
		if part == "" {
			continue
		}

		seenVariants := make(map[string]struct{})
		variants := make([]string, 0, len(uniqueMediaTypes))

		// Generate a slug variant for each MediaType present
		for mediaType := range uniqueMediaTypes {
			slugVariant := slugs.Slugify(mediaType, part)
			if slugVariant != "" {
				if _, exists := seenVariants[slugVariant]; !exists {
					variants = append(variants, slugVariant)
					seenVariants[slugVariant] = struct{}{}
				}
			}
		}

		if len(variants) > 0 {
			variantGroups = append(variantGroups, variants)
		}
	}

	if len(variantGroups) == 0 {
		// return random instead
		rnd, err := db.RandomGame(db.ctx, systems)
		if err != nil {
			return nullResults, err
		}
		return []database.SearchResult{rnd}, nil
	}

	// TODO: since we approximated a glob, we should actually check
	//       result paths against base glob to confirm
	return sqlSearchMediaPathParts(db.ctx, db.sql.Load(), systems, variantGroups)
}

// SystemIndexed returns true if a specific system is indexed in the media database.
func (db *MediaDB) SystemIndexed(system *systemdefs.System) bool {
	if db.sql.Load() == nil {
		return false
	}
	return sqlSystemIndexed(db.ctx, db.sql.Load(), system)
}

// IndexedSystems returns all systems indexed in the media database.
func (db *MediaDB) IndexedSystems() ([]string, error) {
	var systems []string
	if db.sql.Load() == nil {
		return systems, ErrNullSQL
	}
	return sqlIndexedSystems(db.ctx, db.sql.Load())
}

func (db *MediaDB) SystemMediaCounts(
	ctx context.Context,
	tagFilters []zapscript.TagFilter,
) ([]database.SystemMediaCount, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	if len(tagFilters) > 0 {
		return sqlSystemMediaCounts(ctx, db.sql.Load(), tagFilters)
	}
	if cached := db.systemMediaCountsCache.Load(); cached != nil &&
		cached.generation == db.systemMediaCountsGen.Load() {
		return slices.Clone(cached.counts), nil
	}

	generation := db.systemMediaCountsGen.Load()
	counts, err := sqlSystemMediaCounts(ctx, db.sql.Load(), nil)
	if err != nil {
		return nil, err
	}
	if generation == db.systemMediaCountsGen.Load() {
		db.systemMediaCountsCache.Store(&systemMediaCountsSnapshot{
			counts:     slices.Clone(counts),
			generation: generation,
		})
	}
	return counts, nil
}

// RandomGame returns a uniformly selected media row from the specified systems.
func (db *MediaDB) RandomGame(ctx context.Context, systems []systemdefs.System) (database.SearchResult, error) {
	if db.sql.Load() == nil {
		return database.SearchResult{}, ErrNullSQL
	}
	if len(systems) == 0 {
		return database.SearchResult{}, sql.ErrNoRows
	}
	systemIDs := make([]string, len(systems))
	for i := range systems {
		systemIDs[i] = systems[i].ID
	}
	return db.RandomGameWithQuery(ctx, &database.MediaQuery{Systems: systemIDs})
}

// RandomGameWithQuery returns a random game matching the specified MediaQuery.
func (db *MediaDB) RandomGameWithQuery(ctx context.Context, query *database.MediaQuery) (database.SearchResult, error) {
	var result database.SearchResult
	if db.sql.Load() == nil {
		return result, ErrNullSQL
	}

	// Per-system counts preserve uniform media-row weighting while narrowing
	// broad system scopes before random row selection touches the Media table.
	if query.PathPrefix == "" && query.PathGlob == "" && len(query.Systems) > 1 {
		started := time.Now()
		counts, err := db.SystemMediaCounts(ctx, query.Tags)
		if err != nil {
			return result, fmt.Errorf("failed to get system media counts for random selection: %w", err)
		}
		selectedSystem, err := selectWeightedSystem(counts, query.Systems)
		if err != nil {
			return result, err
		}
		log.Debug().
			Int("systems", len(query.Systems)).
			Int("tagFilters", len(query.Tags)).
			Str("selectedSystem", selectedSystem).
			Dur("duration", time.Since(started)).
			Msg("narrowed weighted random media scope")

		narrowed := *query
		narrowed.Systems = []string{selectedSystem}
		return db.RandomGameWithQuery(ctx, &narrowed)
	}

	// Use one full matching scope so systems are weighted by their matching
	// media rows and tag filters cannot choose an empty system first.
	// Check MediaCountCache before repeating the count query.
	if stats, found := db.GetCachedStats(ctx, query); found {
		if stats.Count == 0 {
			return result, sql.ErrNoRows
		}
		result, err := db.randomGameWithStats(ctx, query, stats)
		if err == nil || !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}

		// A matching-row miss means cached count/range statistics are stale.
		// Refresh once and retry through the uncached selector.
		return db.randomGameWithFreshStats(ctx, query)
	}

	return db.randomGameWithFreshStats(ctx, query)
}

func selectWeightedSystem(
	counts []database.SystemMediaCount,
	systems []string,
) (string, error) {
	return selectWeightedSystemUsing(counts, systems, helpers.RandomInt)
}

func selectWeightedSystemUsing(
	counts []database.SystemMediaCount,
	systems []string,
	randomInt func(int) (int, error),
) (string, error) {
	requested := make(map[string]struct{}, len(systems))
	for _, systemID := range systems {
		requested[systemID] = struct{}{}
	}

	eligible := make([]database.SystemMediaCount, 0, len(systems))
	total := 0
	for _, count := range counts {
		if _, ok := requested[count.SystemID]; !ok || count.Count <= 0 {
			continue
		}
		eligible = append(eligible, count)
		total += count.Count
	}
	if total == 0 {
		return "", sql.ErrNoRows
	}

	offset, err := randomInt(total)
	if err != nil {
		return "", fmt.Errorf("failed to select weighted random system: %w", err)
	}
	for _, count := range eligible {
		if offset < count.Count {
			return count.SystemID, nil
		}
		offset -= count.Count
	}
	return "", sql.ErrNoRows
}

func (db *MediaDB) randomGameWithFreshStats(
	ctx context.Context,
	query *database.MediaQuery,
) (database.SearchResult, error) {
	result, stats, err := sqlRandomGameWithQueryAndStats(ctx, db.sql.Load(), query)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			db.cacheMediaStats(ctx, query, stats)
		}
		return result, err
	}

	// Cache the stats for future use (best effort - don't fail if caching fails).
	db.cacheMediaStats(ctx, query, stats)
	return result, nil
}

func (db *MediaDB) cacheMediaStats(ctx context.Context, query *database.MediaQuery, stats MediaStats) {
	cacheCtx, cancel := context.WithTimeout(ctx, mediaStatsCacheWriteTimeout)
	defer cancel()
	if cacheErr := db.SetCachedStats(cacheCtx, query, stats); cacheErr != nil {
		log.Warn().Err(cacheErr).Msg("failed to cache media query stats")
	}
}

// GetTotalMediaCount returns the total number of media entries in the database.
func (db *MediaDB) GetTotalMediaCount() (int, error) {
	if db.sql.Load() == nil {
		return 0, ErrNullSQL
	}
	return sqlGetTotalMediaCount(db.ctx, db.sql.Load())
}

// HasAnyMedia reports whether at least one media row exists. Cheap (EXISTS
// with LIMIT semantics) so it is safe to call while an index is running;
// used to report truthful database existence to clients mid-index.
func (db *MediaDB) HasAnyMedia() (bool, error) {
	if db.sql.Load() == nil {
		return false, ErrNullSQL
	}
	var exists bool
	err := db.sql.Load().QueryRowContext(db.ctx, "SELECT EXISTS(SELECT 1 FROM Media)").Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check for media presence: %w", err)
	}
	return exists, nil
}

// GetMissingMediaCount returns the number of media entries flagged missing —
// what a media.clean.orphans call would delete.
func (db *MediaDB) GetMissingMediaCount() (int, error) {
	if db.sql.Load() == nil {
		return 0, ErrNullSQL
	}
	return sqlGetMissingMediaCount(db.ctx, db.sql.Load())
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
	if db.sql.Load() == nil {
		return MediaStats{}, false
	}

	queryHash, err := db.generateQueryHash(query)
	if err != nil {
		log.Warn().Err(err).Msg("failed to generate query hash for cache lookup")
		return MediaStats{}, false
	}

	var stats MediaStats
	err = db.sql.Load().QueryRowContext(ctx,
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

// randomGameWithStats generates a uniform random media selection using cached statistics.
func (db *MediaDB) randomGameWithStats(
	ctx context.Context, query *database.MediaQuery, stats MediaStats,
) (database.SearchResult, error) {
	return sqlSelectRandomGameWithStats(ctx, db.sql.Load(), query, stats)
}

// SetCachedStats stores statistics for the given media query in the cache.
func (db *MediaDB) SetCachedStats(ctx context.Context, query *database.MediaQuery, stats MediaStats) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	if db.inTransaction {
		return nil
	}

	queryHash, err := db.generateQueryHash(query)
	if err != nil {
		return fmt.Errorf("failed to generate query hash: %w", err)
	}

	queryParams, err := json.Marshal(query)
	if err != nil {
		return fmt.Errorf("failed to marshal query params: %w", err)
	}

	_, err = db.sql.Load().ExecContext(ctx, `
		INSERT OR REPLACE INTO MediaCountCache (QueryHash, QueryParams, Count, MinDBID, MaxDBID, LastUpdated)
		VALUES (?, ?, ?, ?, ?, ?)
	`, queryHash, string(queryParams), stats.Count, stats.MinDBID, stats.MaxDBID, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("failed to cache stats: %w", err)
	}

	_, err = db.sql.Load().ExecContext(ctx, `
		DELETE FROM MediaCountCache
		WHERE QueryHash IN (
			SELECT QueryHash
			FROM MediaCountCache
			WHERE QueryHash <> ?
			ORDER BY LastUpdated ASC, QueryHash ASC
			LIMIT (
				SELECT CASE
					WHEN COUNT(*) > ? THEN COUNT(*) - ?
					ELSE 0
				END
				FROM MediaCountCache
			)
		)`, queryHash, mediaCountCacheMaxEntries, mediaCountCacheMaxEntries)
	if err != nil {
		return fmt.Errorf("failed to prune media count cache: %w", err)
	}

	return nil
}

// InvalidateCountCache clears all cached media counts.
// This should be called after any operation that changes the media database content.
func (db *MediaDB) InvalidateCountCache() error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}

	_, err := db.conn().ExecContext(db.ctx, "DELETE FROM MediaCountCache")
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
		PathPrefix: strings.TrimSpace(query.PathPrefix),
		Tags:       make([]zapscript.TagFilter, len(query.Tags)),
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
		if normalized.Tags[i].Value != normalized.Tags[j].Value {
			return normalized.Tags[i].Value < normalized.Tags[j].Value
		}
		return normalized.Tags[i].Operator < normalized.Tags[j].Operator
	})

	// Marshal to JSON with consistent ordering
	queryBytes, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("failed to marshal normalized query: %w", err)
	}

	// Generate SHA256 hash
	hash := sha256.Sum256(queryBytes)
	return hex.EncodeToString(hash[:]), nil
}

// Find* lookups below always use db.sql.Load() rather than db.conn(): callers
// are external readers (API handlers, zapscript launch, scrapers) that can run
// concurrently with an active indexing transaction, and borrowing the
// indexer's own in-flight *sql.Tx from another goroutine risks "transaction
// has already been committed or rolled back" if it commits mid-read.
func (db *MediaDB) FindSystem(row database.System) (database.System, error) {
	return sqlFindSystem(db.ctx, db.sql.Load(), row)
}

func (db *MediaDB) FindSystemBySystemID(systemID string) (database.System, error) {
	return sqlFindSystemBySystemID(db.ctx, db.sql.Load(), systemID)
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
		result, err = sqlInsertSystem(db.ctx, db.sql.Load(), row)
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

func (db *MediaDB) FindMediaTitle(row *database.MediaTitle) (database.MediaTitle, error) {
	return sqlFindMediaTitle(db.ctx, db.sql.Load(), row)
}

func (db *MediaDB) InsertMediaTitle(row *database.MediaTitle) (database.MediaTitle, error) {
	var result database.MediaTitle
	var err error

	// Use batch inserter if available
	if db.batchInsertMediaTitle != nil {
		err = db.batchInsertMediaTitle.Add(
			row.DBID, row.SystemDBID, row.Slug, row.Name, row.SlugLength,
			row.SlugWordCount, row.SecondarySlug,
		)
		if err != nil {
			return *row, fmt.Errorf("failed to add media title to batch: %w", err)
		}
		// Return row as-is (DBID is already set by caller)
		return *row, nil
	}

	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertMediaTitle != nil {
		result, err = db.insertMediaTitleWithPreparedStmt(row)
	} else {
		result, err = sqlInsertMediaTitle(db.ctx, db.sql.Load(), row)
	}

	// Only invalidate cache if NOT in a transaction (transactions invalidate once on commit)
	if err == nil && !db.inTransaction {
		db.invalidateCaches(invalidationScope{AllSystems: true})
	}

	return result, err
}

func (db *MediaDB) FindOrInsertMediaTitle(row *database.MediaTitle) (database.MediaTitle, error) {
	system, err := db.FindMediaTitle(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertMediaTitle(row)
	}
	return system, err
}

// FindMedia implements MediaDBI. Param is by value because the interface is.
func (db *MediaDB) FindMedia(row database.Media) (database.Media, error) { //nolint:gocritic
	return sqlFindMedia(db.ctx, db.sql.Load(), &row)
}

// InsertMedia implements MediaDBI. Param is by value because the interface is.
func (db *MediaDB) InsertMedia(row database.Media) (database.Media, error) { //nolint:gocritic
	var result database.Media
	var err error

	// Use batch inserter if available
	if db.batchInsertMedia != nil {
		err = db.batchInsertMedia.Add(
			row.DBID,
			row.MediaTitleDBID,
			row.SystemDBID,
			row.Path,
			row.ParentDir,
			row.SortName,
		)
		if err != nil {
			return row, fmt.Errorf("failed to add media to batch: %w", err)
		}
		db.markBrowseCacheDirty()
		db.mediaSearchBoundsDirty = true
		// Return row as-is (DBID is already set by caller)
		return row, nil
	}

	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertMedia != nil {
		result, err = db.insertMediaWithPreparedStmt(&row)
	} else {
		result, err = sqlInsertMedia(db.ctx, db.sql.Load(), &row)
	}

	// Only invalidate cache if NOT in a transaction (transactions invalidate once on commit)
	if err == nil && !db.inTransaction {
		db.invalidateCaches(invalidationScope{AllSystems: true, MediaRowsChanged: true})
		if invalidateErr := db.invalidateBrowseCacheForMediaChange(); invalidateErr != nil {
			return result, invalidateErr
		}
	} else if err == nil {
		db.markBrowseCacheDirty()
		db.mediaSearchBoundsDirty = true
	}

	return result, err
}

func (db *MediaDB) TemporaryRepairJobsPending(ctx context.Context) (bool, error) {
	if db.sql.Load() == nil {
		return false, ErrNullSQL
	}
	pending, err := db.parentDirRepairPending(ctx)
	if err != nil || pending {
		return pending, err
	}
	return db.disambiguationBackfillPending(ctx)
}

func (db *MediaDB) parentDirRepairPending(ctx context.Context) (bool, error) {
	current, err := sqlTemporaryParentDirRepairVersionCurrent(ctx, db.sql.Load())
	if err != nil {
		return false, err
	}
	if current {
		return false, nil
	}

	pending, err := sqlEmptyMediaParentDirsExist(ctx, db.sql.Load())
	if err != nil || pending {
		return pending, err
	}

	if err := sqlMarkTemporaryParentDirRepairComplete(ctx, db.sql.Load()); err != nil {
		return false, err
	}
	return false, nil
}

func (db *MediaDB) DeleteMediaTag(mediaDBID, tagDBID int64) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}

	err := sqlDeleteMediaTag(db.ctx, db.conn(), mediaDBID, tagDBID)
	if err == nil {
		if db.inTransaction {
			db.markBrowseCacheDirty()
			db.markUtilityTagCacheDirty()
		} else {
			db.invalidateCaches(invalidationScope{AllSystems: true})
		}
	}

	return err
}

// FindOrInsertMedia implements MediaDBI. Param is by value because the interface is.
func (db *MediaDB) FindOrInsertMedia(row database.Media) (database.Media, error) { //nolint:gocritic
	system, err := db.FindMedia(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertMedia(row)
	}
	return system, err
}

func (db *MediaDB) FindTagType(row database.TagType) (database.TagType, error) {
	return sqlFindTagType(db.ctx, db.sql.Load(), row)
}

// InsertTagType inserts a new TagType into the database.
func (db *MediaDB) InsertTagType(row database.TagType) (database.TagType, error) {
	var result database.TagType
	var err error

	// Use batch inserter if available
	if db.batchInsertTagType != nil {
		isExclusive := 0
		if row.IsExclusive {
			isExclusive = 1
		}
		err = db.batchInsertTagType.Add(row.DBID, row.Type, isExclusive)
		if err != nil {
			return row, fmt.Errorf("failed to add tag type to batch: %w", err)
		}
		db.markUtilityTagCacheDirty()
		// Return row as-is (DBID is already set by caller)
		return row, nil
	}

	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertTagType != nil {
		result, err = db.insertTagTypeWithPreparedStmt(row)
	} else {
		result, err = sqlInsertTagType(db.ctx, db.sql.Load(), row)
	}

	// Only invalidate cache if NOT in a transaction (transactions invalidate once on commit)
	if err == nil && !db.inTransaction {
		db.invalidateCaches(invalidationScope{AllSystems: true, UtilityTagDBIDsChanged: true})
	} else if err == nil {
		db.markUtilityTagCacheDirty()
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
	return sqlFindTag(db.ctx, db.sql.Load(), row)
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
		db.markUtilityTagCacheDirty()
		// Return row as-is (DBID is already set by caller)
		return row, nil
	}

	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertTag != nil {
		result, err = db.insertTagWithPreparedStmt(row)
	} else {
		result, err = sqlInsertTag(db.ctx, db.sql.Load(), row)
	}

	// Only invalidate cache if NOT in a transaction (transactions invalidate once on commit)
	if err == nil && !db.inTransaction {
		db.invalidateCaches(invalidationScope{AllSystems: true, UtilityTagDBIDsChanged: true})
	} else if err == nil {
		db.markUtilityTagCacheDirty()
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
	return sqlFindMediaTag(db.ctx, db.sql.Load(), row)
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
		result, err = sqlInsertMediaTag(db.ctx, db.sql.Load(), row)
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

func (db *MediaDB) GetAllSystems() ([]database.System, error) {
	return sqlGetAllSystems(db.ctx, db.sql.Load())
}

// GetTitlesBySystemID retrieves all media titles for a specific system with their associated system information.
// This is used for lazy loading during resume to avoid loading ALL titles upfront.
//
// Uses a non-cancellable context: with a cancellable context, mattn/go-sqlite3
// spawns a goroutine + channel per rows.Next() call, which is significant
// overhead on the scanner's bulk per-system reads. The scanner checks for
// cancellation between queries instead.
func (db *MediaDB) GetTitlesBySystemID(systemID string) ([]database.TitleWithSystem, error) {
	return sqlGetTitlesBySystemID(context.WithoutCancel(db.ctx), db.sql.Load(), systemID)
}

// GetMediaBySystemID retrieves all media for a specific system.
// This is used for lazy loading during resume to avoid loading ALL media upfront.
// TitleSlug is NOT populated: no caller uses it, and fetching it would add a
// MediaTitles probe per media row to the scanner's hottest state-load query.
// Non-cancellable context: see GetTitlesBySystemID.
func (db *MediaDB) GetMediaBySystemID(systemID string) ([]database.MediaWithFullPath, error) {
	return sqlGetMediaBySystemID(context.WithoutCancel(db.ctx), db.sql.Load(), systemID)
}

// RunBackgroundOptimization performs database optimization operations in the background.
// This includes creating indexes, running ANALYZE, and vacuuming the database.
// It can be safely interrupted and resumed later.
func (db *MediaDB) RunBackgroundOptimization(
	statusCallback func(optimizing bool), pauser *syncutil.Pauser,
) {
	lease, err := db.AcquireMediaWrite(database.MediaWriteOperationOptimization)
	if err != nil {
		log.Info().Err(err).Msg("background optimization deferred")
		return
	}
	if err := db.RunBackgroundOptimizationWithLease(statusCallback, pauser, lease); err != nil {
		log.Error().Err(err).Msg("background optimization failed")
	}
}

// RunBackgroundOptimizationWithLease runs optimization using ownership handed
// off by another operation, such as successful indexing. It always releases lease.
func notifyOptimizationStatus(statusCallback func(optimizing bool), optimizing bool) {
	if statusCallback == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("panic recovered in optimization status callback")
		}
	}()
	statusCallback(optimizing)
}

func (db *MediaDB) RunBackgroundOptimizationWithLease(
	statusCallback func(optimizing bool), pauser *syncutil.Pauser, lease *database.MediaWriteLease,
) (runErr error) {
	if !lease.ValidFor(database.MediaWriteOperationOptimization) {
		lease.Release()
		return database.ErrMediaWriteLease
	}

	db.isOptimizing.Store(true)
	db.backgroundOps.Add(1)

	// Optimization is bulk work on an otherwise idle device, but it ran with the
	// default 8MB cache, 2 connections and temp_store=FILE. The indexing boost is
	// scoped to NewNamesIndex's stack frame (configureIndexingPragmas is installed
	// there with defer), while post-index optimization is deliberately detached
	// into its own goroutine that only starts after that function returns — so the
	// boost was always already released by the time these steps ran. On the MiSTer
	// test device that meant pragma_optimize read 359MB through an 8MB cache and
	// the browse cache rebuild spilled its temp B-trees to the SD card. The
	// startup-triggered optimization paths never had a boost at all.
	//
	// The media write lease makes optimization and indexing mutually exclusive, so
	// these cannot interleave, but restore the previous state rather than assume.
	//
	// Registered before the recovery defer below so that, on a panic, the restore
	// runs *after* the handler has written its status: restoring pragmas can
	// discard a pooled connection, which the handler still needs.
	// In round 8 of #1279 this boost silently did nothing. SetIndexingCacheSize
	// drains every pooled connection to put the pragma on each one; another
	// caller held one, the 5s drain timed out, and the whole optimization ran at
	// the 8MB default regardless. Nothing said so — the only evidence was
	// dbCacheSize on the step metrics, read hours after the fact.
	//
	// So the boost is verified rather than assumed, and retried once when it did
	// not take. Contention here is transient (the app polls for a few
	// milliseconds at a time), so a second attempt usually lands.
	//
	// Raise the connection cap BEFORE applying the pragmas. Doing it the other
	// way round sizes the drain against the narrow cap, so the extra connection
	// the next line permits is later opened straight from the DSN — at the 8MB
	// default, with nothing left to configure it. Rounds 8, 9 and 10 of #1279
	// all reported dbCacheSize -8192 for exactly that reason, even though the
	// connections the drain did reach were configured correctly.
	//
	// An earlier attempt at this ordering was reverted because the drain timed
	// out; that timeout was the caller-side race in startIndexing (the indexing
	// pool boost was still pending, so the drain sized itself to a cap that was
	// about to shrink underneath it) and is fixed at the source. drainPooledConns
	// now also re-reads the target rather than sampling it once.
	//
	// Round 11 confirmed all six steps then run at -32768 with no drain timeout.
	// It did not, however, show the boost is worth anything: see
	// SetIndexingCacheSize for why round 10 is not a valid control to compare
	// against. Treat the value of this boost as unmeasured.
	if !db.indexingCacheBoost.Load() {
		defer func() {
			db.SetIndexingConnBoost(false)
			db.SetIndexingCacheSize(false)
		}()
		db.SetIndexingConnBoost(true)
		db.SetIndexingCacheSize(true)
		db.ensureIndexingCacheBoostApplied()
	}

	defer func() {
		// Recover from any panics to prevent crashing the entire service.
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("panic recovered in background optimization")
			// Try to mark optimization as failed so it can be retried.
			if db.sql.Load() != nil {
				_ = db.SetOptimizationStatus(IndexingStatusFailed)
			}
			runErr = fmt.Errorf("background optimization panic: %v", r)
			notifyOptimizationStatus(statusCallback, false)
		}
		db.isOptimizing.Store(false)
		db.backgroundOps.Done()
		lease.Release()
	}()

	if db.sql.Load() == nil {
		log.Error().Msg("cannot run background optimization: database not connected")
		if statusCallback != nil {
			statusCallback(false)
		}
		return ErrNullSQL
	}

	log.Info().Msg("starting background database optimization")

	// Set status to running
	if err := db.SetOptimizationStatus(IndexingStatusRunning); err != nil {
		log.Error().Err(err).Msg("failed to set optimization status to running")
		if statusCallback != nil {
			statusCallback(false)
		}
		return fmt.Errorf("failed to set optimization status to running: %w", err)
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

	steps := make([]optimizationStep, 0, 5)

	rd := db.analyzeRetryDelay

	// NOTE: Indexes, tags cache, and slug search cache are built synchronously
	// at the end of NewNamesIndex so the database is fully searchable when
	// indexing completes. Background optimization improves query performance
	// after the database is already open for searches.
	//
	// Step order matters: the temporary parent-dir repair runs first because it
	// can change paths the browse cache is built from (and it invalidates the
	// cache when it does), so browse_cache follows immediately to rebuild from the
	// repaired data. browse_cache is placed ahead of pragma_optimize and
	// page_prefetch so the user-visible browse fix lands before the expensive
	// planner/buffer housekeeping and survives interruption of those later steps.
	// disambiguation_backfill is a one-time stamp-gated repair (a no-op once
	// current) whose per-system recompute leans heavily on the query planner, so
	// it runs after pragma_optimize has refreshed statistics. WAL checkpoint
	// follows as non-critical housekeeping.
	db.needsIndexRebuild.Store(false)

	steps = append(steps,
		optimizationStep{
			name: "temporary_repair_parent_dirs", fn: func() error {
				return db.runTemporaryParentDirRepair(db.ctx, pauser)
			},
			maxRetries: 0, retryDelay: rd,
		},
		optimizationStep{
			name: "browse_cache", fn: func() error {
				return db.PopulateBrowseCache(db.ctx)
			},
			maxRetries: 0, retryDelay: rd,
		},
		optimizationStep{
			name: "pragma_optimize", fn: db.AnalyzeApproximate,
			maxRetries: 2, retryDelay: rd,
		},
		optimizationStep{
			name: "disambiguation_backfill", fn: func() error {
				return db.runDisambiguationBackfill(db.ctx, pauser)
			},
			maxRetries: 0, retryDelay: rd,
		},
		optimizationStep{
			name: "page_prefetch", fn: func() error {
				if err := db.prefetchSearchPages(db.ctx); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return err
					}
					log.Warn().Err(err).Msg("page_prefetch failed, continuing anyway")
					return nil
				}
				return nil
			},
			maxRetries: 0, retryDelay: rd,
		},
		optimizationStep{
			name: "wal_checkpoint", fn: db.WALCheckpoint,
			maxRetries: 1, retryDelay: rd,
		},
		// NOTE: VACUUM is intentionally omitted. It takes an exclusive lock
		// for the entire duration, blocking all reads (including card scans).
		// PRAGMA optimize is sufficient for planner maintenance here. SQLite
		// reuses free pages on the next INSERT, so disk reclamation is not needed.
	)

	// Resume from the persisted step rather than always restarting at step 1.
	// SetOptimizationStep records the step that was running when the process was
	// interrupted; on resume we skip the steps that already completed and re-run
	// that step (it may not have finished). An empty or unrecognised value starts
	// from the beginning. This stops every restart from redoing the expensive
	// pragma_optimize/page_prefetch work already done on a previous boot.
	startStep := 0
	if persisted, stepErr := db.GetOptimizationStep(); stepErr != nil {
		log.Warn().Err(stepErr).Msg("failed to read persisted optimization step; starting from the first step")
	} else if persisted != "" {
		for i := range steps {
			if steps[i].name == persisted {
				startStep = i
				break
			}
		}
		if startStep > 0 {
			log.Info().Str("step", persisted).Msgf("resuming background optimization from step %d/%d",
				startStep+1, len(steps))
		}
	}

	// Per-step resource metrics so post-index/startup housekeeping (browse cache
	// rebuild, pragma optimize, page prefetch) shows real wall time and write cost
	// in logs, not just start/complete markers.
	stepRecorder := perfmetrics.NewRecorderForDB(db)

	// Execute each step with retry logic
	for _, step := range steps[startStep:] {
		// Wait if paused (e.g. game is running)
		if err := pauser.Wait(db.ctx); err != nil {
			log.Info().Msg("background optimization cancelled while paused")
			if setErr := db.SetOptimizationStatus(IndexingStatusFailed); setErr != nil {
				if errors.Is(setErr, context.Canceled) {
					log.Debug().Err(setErr).Msg("set optimization status to failed skipped (cancelled)")
				} else {
					log.Error().Err(setErr).Msg("failed to set optimization status to failed")
				}
			}
			if statusCallback != nil {
				statusCallback(false)
			}
			return fmt.Errorf("wait to run background optimization: %w", err)
		}

		log.Info().Msgf("running optimization step: %s", step.name)

		if err := db.SetOptimizationStep(step.name); err != nil {
			// A cancelled context here just means the service is shutting down
			// mid-optimization; that's expected, so keep it out of Sentry.
			if errors.Is(err, context.Canceled) {
				log.Debug().Err(err).Msgf("set optimization step to %s skipped (cancelled)", step.name)
			} else {
				log.Error().Err(err).Msgf("failed to set optimization step to %s", step.name)
			}
		}

		// Execute step with retry and exponential backoff
		stepMetricsStart := stepRecorder.Capture(db.ctx, true)
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
			// Database corruption can't be repaired by optimization. Route it to the
			// same corrupt-database state the indexer uses so the app surfaces the
			// repair/rebuild flow instead of repeatedly failing maintenance. The sidecar
			// marker is the durable signal recovery keys on, since the in-DB status write
			// may itself fail on a malformed database.
			if database.IsCorruptionError(stepErr) {
				log.Error().Strs("integrity", db.IntegrityReport()).
					Msg("media database integrity check after optimization failure")
				db.MarkCorrupt(fmt.Sprintf("optimization step %s: %v", step.name, stepErr))
				if setErr := db.SetIndexingStatus(IndexingStatusCorrupt); setErr != nil {
					log.Error().Err(setErr).Msg("failed to mark media database as corrupt after optimization failure")
				}
			}
			// Clear the step before writing the failed status: a crash between the
			// two writes must not leave a Failed status with a stale step, or the
			// next boot's failure-resume would start mid-list and skip the steps
			// before it. The reverse gap (step cleared, status still running) just
			// re-runs everything from the first step.
			if setErr := db.SetOptimizationStep(""); setErr != nil {
				log.Error().Err(setErr).Msg("failed to clear optimization step on failure")
			}
			if setErr := db.SetOptimizationStatus(IndexingStatusFailed); setErr != nil {
				log.Error().Err(setErr).Msg("failed to set optimization status to failed")
			}

			// Notify that optimization has failed
			if statusCallback != nil {
				statusCallback(false)
			}
			return stepErr
		}

		stepMetricsEnd := stepRecorder.Capture(db.ctx, true)
		perfmetrics.AddDelta(log.Info().Str("step", step.name), &stepMetricsStart, &stepMetricsEnd).
			Msg("optimization step metrics")

		log.Info().Msgf("optimization step %s completed", step.name)
	}

	// Mark as completed
	if err := db.SetOptimizationStatus(IndexingStatusCompleted); err != nil {
		log.Error().Err(err).Msg("failed to set optimization status to completed")
		return fmt.Errorf("failed to set optimization status to completed: %w", err)
	}
	// Clear optimization step on completion
	if err := db.SetOptimizationStep(""); err != nil {
		log.Error().Err(err).Msg("failed to clear optimization step on completion")
	}

	// Notify that optimization has completed
	if statusCallback != nil {
		statusCallback(false)
	}

	log.Info().Msg("background database optimization completed")
	return nil
}

// WaitForBackgroundOperations waits for all background operations to complete.
// This should be called before closing the database to ensure clean shutdown.
func (db *MediaDB) WaitForBackgroundOperations() {
	db.backgroundOps.Wait()
}

// SetIndexingConnBoost widens the connection pool while an index runs (the
// writer holds a connection near-continuously, which would otherwise leave a
// single connection for all foreground reads) and restores the steady-state
// size afterwards. Safe to call around a Recreate: it always acts on the
// currently-loaded pool.
//
// The idle cap is kept equal to the open cap on purpose, and this is a
// correctness requirement rather than a pooling tweak: database/sql defaults
// MaxIdleConns to 2, so a boosted pool of 3 would close its third connection
// whenever it went idle and silently reopen a fresh one under load. Connection
// pragmas do not survive that. SetWALAutoCheckpoint only reaches connections it
// can drain when it is called, so a reopened connection comes back at SQLite's
// compiled wal_autocheckpoint default instead of the 0 indexing requires, and a
// write landing on it can trigger an automatic checkpoint outside
// checkpointLargeWAL's explicit, measured path. Observed once on device during
// #1279: a pooled connection reported SQLite's compiled default of 1000 pages
// where indexing had set 0, with the pool size unchanged across the window, so
// a reopened connection was the only explanation. Matching the caps keeps the
// boosted connection alive for the whole run so its pragmas stay applied.
func (db *MediaDB) SetIndexingConnBoost(active bool) {
	sqlInstance := db.sql.Load()
	if sqlInstance == nil {
		return
	}
	// Order matters when shrinking: database/sql silently clamps MaxIdleConns
	// down to MaxOpenConns, so lower the idle cap first to avoid leaving it
	// above the new open cap.
	if active {
		sqlInstance.SetMaxOpenConns(indexingMaxOpenConns)
		sqlInstance.SetMaxIdleConns(indexingMaxOpenConns)
	} else {
		sqlInstance.SetMaxIdleConns(baseMaxOpenConns)
		sqlInstance.SetMaxOpenConns(baseMaxOpenConns)
	}
}

// BeginRecovery prevents new background operations from registering, then waits
// for operations already registered to drain. EndRecovery must follow it.
func (db *MediaDB) BeginRecovery() {
	db.backgroundOpsMu.Lock()
	db.backgroundOps.Wait()
}

// EndRecovery allows background operations to register after recovery completes.
func (db *MediaDB) EndRecovery() {
	db.backgroundOpsMu.Unlock()
}

// TrackBackgroundOperation increments the background operations counter.
// Call BackgroundOperationDone when the operation completes.
// This allows external code (like the indexing goroutine) to be tracked.
func (db *MediaDB) TrackBackgroundOperation() {
	db.backgroundOpsMu.RLock()
	defer db.backgroundOpsMu.RUnlock()
	db.backgroundOps.Add(1)
	db.backgroundOpsCount.Add(1)
}

// HasBackgroundOperations reports whether this process currently owns media database
// background work. Persisted running statuses cannot answer this after a crash.
func (db *MediaDB) HasBackgroundOperations() bool {
	return db.backgroundOpsCount.Load() > 0
}

// BackgroundOperationDone decrements the background operations counter.
// This should be called when an operation started with TrackBackgroundOperation completes.
func (db *MediaDB) BackgroundOperationDone() {
	db.backgroundOpsCount.Add(-1)
	db.backgroundOps.Done()
}

// GetLaunchCommandForMedia generates a title-based launch command for the given media.
func (db *MediaDB) GetLaunchCommandForMedia(ctx context.Context, systemID, path string) (string, error) {
	sqlDB, err := db.readConn()
	if err != nil {
		return "", err
	}

	return sqlGetLaunchCommandForMedia(ctx, sqlDB, systemID, path)
}

// CheckForDuplicateMediaTitles returns any MediaTitle records that have duplicate (SystemDBID, Slug) combinations.
// Used in tests to validate data integrity after selective updates.
func (db *MediaDB) CheckForDuplicateMediaTitles() ([]string, error) {
	sqlDB, connErr := db.readConn()
	if connErr != nil {
		return nil, connErr
	}

	query := `
		SELECT SystemDBID, Slug, COUNT(*) as cnt
		FROM MediaTitles
		GROUP BY SystemDBID, Slug
		HAVING cnt > 1
	`

	rows, err := sqlDB.QueryContext(db.ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query duplicate media titles: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("failed to close rows in CheckForDuplicateMediaTitles")
		}
	}()

	duplicates := make([]string, 0)
	for rows.Next() {
		var systemDBID int64
		var slug string
		var count int
		if err := rows.Scan(&systemDBID, &slug, &count); err != nil {
			return nil, fmt.Errorf("failed to scan duplicate media title row: %w", err)
		}
		duplicates = append(duplicates, fmt.Sprintf("SystemDBID=%d, Slug=%s (count=%d)", systemDBID, slug, count))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating duplicate media titles: %w", err)
	}

	return duplicates, nil
}
