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
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// browseCacheClearStatements empties the browse cache tables, children first.
// Only safe to run with foreign key enforcement disabled — see
// acquireBrowseCacheConn.
//
//goland:noinspection SqlWithoutWhere
var browseCacheClearStatements = []string{
	"DELETE FROM BrowseDirCounts",
	"DELETE FROM BrowseDirs",
}

// sqlClearBrowseCacheTables empties both browse cache tables in a single
// foreign-key-disabled transaction. Used when an on-disk cache from an older
// schema has to be discarded before a refresh can reuse existing dir DBIDs.
func sqlClearBrowseCacheTables(ctx context.Context, db *sql.DB) error {
	conn, releaseConn, err := acquireBrowseCacheConn(ctx, db)
	if err != nil {
		return err
	}
	defer releaseConn()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("browse cache: failed to begin clear transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	started := time.Now()
	for _, stmt := range browseCacheClearStatements {
		if _, execErr := tx.ExecContext(ctx, stmt); execErr != nil {
			return fmt.Errorf("browse cache: failed to clear incompatible cache: %w", execErr)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("browse cache: failed to commit clear: %w", err)
	}
	log.Debug().Dur("duration", time.Since(started)).Msg("browse cache cleared incompatible entries")
	return nil
}

// acquireBrowseCacheConn pins a connection and disables foreign key enforcement
// on it for the duration of a browse cache rebuild, returning the connection and
// a release func that restores the pragma before the connection goes back to the
// pool.
//
// BrowseDirs is the parent of three ON DELETE CASCADE foreign keys — a
// self-reference plus two from BrowseDirCounts — and connections are opened
// _foreign_keys=ON. With enforcement on, SQLite cannot apply its truncate
// optimization to an unqualified DELETE: it removes rows one at a time, running
// the child probes and the recursive self-cascade through a statement journal.
// On the MiSTer test device that measured 261.9s to clear ~21,500 rows, 13x the
// cost of the inserts that replaced them. Deleting children first with
// enforcement off leaves no cascade work to do. See #1279, and sqlTruncate /
// sqlTruncateSystems in sql_maintenance.go which solve the same problem.
//
// PRAGMA foreign_keys is session-local and is a no-op inside a transaction, so
// the pragma has to be set on the same connection the transaction will use, and
// before that transaction begins.
func acquireBrowseCacheConn(ctx context.Context, db *sql.DB) (*sql.Conn, func(), error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("browse cache: failed to acquire connection: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("browse cache: failed to release connection")
		}
		return nil, nil, fmt.Errorf("browse cache: failed to disable foreign keys: %w", err)
	}

	release := func() {
		// context.Background: the pragma must be restored even when ctx is already
		// done, otherwise the connection returns to the pool with enforcement off
		// and silently disables it for unrelated queries.
		if _, resetErr := conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); resetErr != nil {
			// Discard the physical connection rather than let it back into the
			// pool with foreign keys disabled (same approach as closeWriterConn).
			log.Warn().Err(resetErr).
				Msg("browse cache: failed to restore foreign keys, discarding connection")
			discardErr := conn.Raw(func(any) error { return driver.ErrBadConn })
			if discardErr != nil &&
				!errors.Is(discardErr, driver.ErrBadConn) &&
				!errors.Is(discardErr, sql.ErrConnDone) {
				log.Warn().Err(discardErr).Msg("browse cache: failed to discard connection")
			}
			return
		}
		if closeErr := conn.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("browse cache: failed to release connection")
		}
	}
	return conn, release, nil
}

const browseCacheSchemaVersion = "3"

// browseCacheInvalidatedVersion is the sentinel written to
// DBConfig.BrowseIndexVersion when the cache is marked stale (e.g. media changed
// during indexing). The BrowseDirs/BrowseDirCounts rows remain in the current
// schema; only their counts may be out of date, so a stale-but-present cache can
// still be served while a refresh is scheduled, rather than falling back to a
// full media scan. The sentinel embeds the schema version it invalidates: after
// a schema bump, a cache invalidated under the previous schema no longer matches
// this value and reads as absent, so old-schema rows are never served as
// "stale-but-serveable" by newer code.
const browseCacheInvalidatedVersion = browseCacheSchemaVersion + "-stale"

type browseCacheDir struct {
	parentID  *int64
	path      string
	name      string
	id        int64
	isVirtual bool
}

type browseCacheCountKey struct {
	parentDirID int64
	childDirID  int64
	systemDBID  int64
}

type browseCacheBuilder struct {
	dirs   map[string]*browseCacheDir
	counts map[browseCacheCountKey]int
	// lookupExisting resolves a dir path to its already-persisted row. It is nil
	// for a full rebuild, which clears the table and owns every DBID itself.
	// Incremental refreshes set it so a dir shared with an already-indexed system
	// keeps its DBID, which that system's count rows reference.
	lookupExisting func(path string) (*browseCacheDir, bool, error)
	// lookupErr holds the first lookup failure. ensureDir cannot return an error
	// without threading one through addMedia and countPairsForPath, so the caller
	// must check this before persisting anything: after a failure the builder has
	// minted a fresh DBID for a path that may already exist, and inserting that
	// would violate the Path unique constraint or split a shared dir in two.
	lookupErr error
	nextDirID int64
	mediaRows int
}

func newBrowseCacheBuilder() *browseCacheBuilder {
	return &browseCacheBuilder{
		dirs:      make(map[string]*browseCacheDir),
		counts:    make(map[browseCacheCountKey]int),
		nextDirID: 1,
	}
}

// sqlPopulateBrowseCache rebuilds the compact browse cache tables from current,
// non-missing media rows. The historical method name is kept because the
// background optimization step still calls this "browse_cache".
func sqlPopulateBrowseCache(ctx context.Context, db *sql.DB) error {
	started := time.Now()
	readStarted := time.Now()
	builder := newBrowseCacheBuilder()
	builder.ensureDir("/")

	// Pinned connection with foreign keys off: the clear below is an unqualified
	// DELETE over a table with cascading children, which is pathologically slow
	// with enforcement on. See acquireBrowseCacheConn.
	conn, releaseConn, err := acquireBrowseCacheConn(ctx, db)
	if err != nil {
		return err
	}
	defer releaseConn()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("browse cache: failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := scanBrowseCacheMedia(ctx, tx, builder); err != nil {
		return err
	}
	log.Debug().
		Dur("duration", time.Since(readStarted)).
		Int("dirs", len(builder.dirs)).
		Int("media", builder.mediaRows).
		Int("counts", len(builder.counts)).
		Msg("browse cache media scan complete")

	deleteStarted := time.Now()
	for _, stmt := range browseCacheClearStatements {
		if _, execErr := tx.ExecContext(ctx, stmt); execErr != nil {
			return fmt.Errorf("browse cache: failed to clear tables: %w", execErr)
		}
	}
	log.Debug().Dur("duration", time.Since(deleteStarted)).Msg("browse cache cleared old entries")

	if err := insertBrowseCacheDirs(ctx, tx, builder.dirs); err != nil {
		return err
	}
	if err := insertBrowseCacheCounts(ctx, tx, builder.counts); err != nil {
		return err
	}

	if _, cfgErr := tx.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigBrowseIndexVersion,
		browseCacheSchemaVersion,
	); cfgErr != nil {
		return fmt.Errorf("browse cache: failed to mark index ready: %w", cfgErr)
	}
	if _, cfgErr := tx.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigBrowseIndexComplete,
		"1",
	); cfgErr != nil {
		return fmt.Errorf("browse cache: failed to mark index complete: %w", cfgErr)
	}

	commitStarted := time.Now()
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("browse cache: failed to commit: %w", err)
	}
	log.Debug().Dur("duration", time.Since(commitStarted)).Msg("browse cache transaction committed")

	log.Info().
		Int("dirs", len(builder.dirs)).
		Int("media", builder.mediaRows).
		Int("counts", len(builder.counts)).
		Dur("duration", time.Since(started)).
		Msg("browse cache populated")
	return nil
}

func logBrowseMediaCountsBySystem(ctx context.Context, tx *sql.Tx) {
	rows, err := tx.QueryContext(ctx, `
		SELECT s.SystemID,
			COUNT(*) AS TotalMedia,
			SUM(CASE WHEN m.IsMissing = 0 THEN 1 ELSE 0 END) AS CurrentMedia,
			SUM(CASE WHEN m.IsMissing != 0 THEN 1 ELSE 0 END) AS MissingMedia
		FROM Media m
		INNER JOIN Systems s ON m.SystemDBID = s.DBID
		GROUP BY s.SystemID
		ORDER BY s.SystemID`)
	if err != nil {
		log.Debug().Err(err).Msg("browse media system counts diagnostic failed")
		return
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var systemID string
		var totalMedia, currentMedia, missingMedia int
		if scanErr := rows.Scan(&systemID, &totalMedia, &currentMedia, &missingMedia); scanErr != nil {
			log.Debug().Err(scanErr).Msg("browse media system counts diagnostic scan failed")
			return
		}
		log.Debug().
			Str("system", systemID).
			Int("totalMedia", totalMedia).
			Int("currentMedia", currentMedia).
			Int("missingMedia", missingMedia).
			Msg("browse media system count")
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		log.Debug().Err(rowsErr).Msg("browse media system counts diagnostic rows failed")
	}
}

func scanBrowseCacheMedia(ctx context.Context, tx *sql.Tx, builder *browseCacheBuilder) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT m.SystemDBID, m.Path
		FROM Media m
		WHERE m.IsMissing = 0
		ORDER BY m.DBID`)
	if err != nil {
		return fmt.Errorf("browse cache: failed to query media: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var systemDBID int64
		var mediaPath string
		if scanErr := rows.Scan(&systemDBID, &mediaPath); scanErr != nil {
			return fmt.Errorf("browse cache: failed to scan media: %w", scanErr)
		}
		builder.addMedia(systemDBID, mediaPath)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("browse cache: rows iteration error: %w", rowsErr)
	}
	return nil
}

func (b *browseCacheBuilder) addMedia(systemDBID int64, mediaPath string) {
	b.mediaRows++
	mediaPath = browseCacheNormalizePath(mediaPath)

	for _, pair := range b.countPairsForPath(mediaPath) {
		key := browseCacheCountKey{
			parentDirID: pair.parent.id,
			childDirID:  pair.child.id,
			systemDBID:  systemDBID,
		}
		b.counts[key]++
	}
}

func (b *browseCacheBuilder) ensureDir(dirPath string) *browseCacheDir {
	if dir, ok := b.dirs[dirPath]; ok {
		return dir
	}
	// A miss is resolved against the table once and then cached in b.dirs either
	// way, so each distinct path costs at most one probe per refresh. Ancestors
	// are not walked here: countPairsForPath already calls ensureDir on every
	// ancestor explicitly, and the recursion below exists only to fill parentID
	// when minting a new dir.
	if b.lookupExisting != nil && b.lookupErr == nil {
		dir, found, err := b.lookupExisting(dirPath)
		switch {
		case err != nil:
			b.lookupErr = err
		case found:
			b.dirs[dirPath] = dir
			return dir
		}
	}
	parentPath, name, isVirtual := browseCacheDirParentAndName(dirPath)
	var parentID *int64
	if parentPath != "" {
		parent := b.ensureDir(parentPath)
		parentID = &parent.id
	}
	dir := &browseCacheDir{
		id:        b.nextDirID,
		path:      dirPath,
		name:      name,
		parentID:  parentID,
		isVirtual: isVirtual,
	}
	b.nextDirID++
	b.dirs[dirPath] = dir
	return dir
}

type browseCacheCountPair struct {
	parent *browseCacheDir
	child  *browseCacheDir
}

func (b *browseCacheBuilder) countPairsForPath(mediaPath string) []browseCacheCountPair {
	mediaPath = browseCacheNormalizePath(mediaPath)
	if idx := strings.Index(mediaPath, "://"); idx >= 0 {
		root := b.ensureDir("/")
		scheme := b.ensureDir(mediaPath[:idx+3])
		return []browseCacheCountPair{
			{parent: root, child: scheme},
			{parent: scheme, child: scheme},
		}
	}

	dirs := browseCacheAncestorDirs(mediaPath)
	pairs := make([]browseCacheCountPair, 0, len(dirs)+1)
	root := b.ensureDir("/")
	pairs = append(pairs, browseCacheCountPair{parent: root, child: root})
	for i := 0; i+1 < len(dirs); i++ {
		pairs = append(pairs, browseCacheCountPair{
			parent: b.ensureDir(dirs[i]),
			child:  b.ensureDir(dirs[i+1]),
		})
	}
	// A self-pair on the media's immediate parent stores direct-child file
	// count. Parent→child pairs intentionally remain subtree counts for route
	// discovery; keeping both shapes lets media.browse answer totalFiles without
	// scanning a large Media partition on every cold first page.
	if len(dirs) > 1 {
		leaf := b.ensureDir(dirs[len(dirs)-1])
		pairs = append(pairs, browseCacheCountPair{parent: leaf, child: leaf})
	}
	return pairs
}

func browseCacheAncestorDirs(mediaPath string) []string {
	mediaPath = browseCacheNormalizePath(mediaPath)
	dirs := []string{"/"}
	if !strings.HasPrefix(mediaPath, "/") {
		mediaPath = "/" + mediaPath
	}
	dir := path.Dir(mediaPath)
	if dir == "." || dir == "/" || dir == "" {
		return dirs
	}

	parts := strings.Split(strings.Trim(dir, "/"), "/")
	current := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		current += "/" + part
		dirs = append(dirs, current+"/")
	}
	return dirs
}

func browseCacheNormalizePath(mediaPath string) string {
	if mediaPath == "" {
		return "/"
	}
	if idx := strings.Index(mediaPath, "://"); idx >= 0 {
		prefix := mediaPath[:idx+3]
		pathPart := browseCacheCleanPathPart(mediaPath[idx+3:])
		if pathPart == "/" {
			return prefix
		}
		return prefix + strings.TrimPrefix(pathPart, "/")
	}

	return browseCacheCleanPathPart(mediaPath)
}

func browseCacheCleanPathPart(pathPart string) string {
	pathPart = strings.ReplaceAll(pathPart, "\\", string(filepath.Separator))
	cleaned := filepath.ToSlash(filepath.Clean(pathPart))
	if cleaned == "." {
		return "/"
	}
	return cleaned
}

func browseCacheDirParentAndName(dirPath string) (parentPath, name string, isVirtual bool) {
	if dirPath == "" {
		return "", "", false
	}
	if strings.Contains(dirPath, "://") {
		return "/", dirPath, true
	}
	if dirPath == "/" {
		return "", "/", false
	}
	trimmed := strings.TrimSuffix(dirPath, "/")
	parent := path.Dir(trimmed)
	if parent == "." {
		return "", path.Base(trimmed), false
	}
	if parent == "/" {
		return "/", path.Base(trimmed), false
	}
	return parent + "/", path.Base(trimmed), false
}

func insertBrowseCacheDirs(ctx context.Context, tx *sql.Tx, dirs map[string]*browseCacheDir) error {
	started := time.Now()
	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO BrowseDirs (DBID, ParentDirDBID, Path, Name, IsVirtual) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("browse cache: failed to prepare dir insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	ordered := make([]*browseCacheDir, 0, len(dirs))
	for _, dir := range dirs {
		ordered = append(ordered, dir)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].id < ordered[j].id })

	for _, dir := range ordered {
		_, insertErr := stmt.ExecContext(ctx, dir.id, dir.parentID, dir.path, dir.name, dir.isVirtual)
		if insertErr != nil {
			return fmt.Errorf("browse cache: failed to insert dir %s: %w", dir.path, insertErr)
		}
	}
	log.Debug().Dur("duration", time.Since(started)).Int("entries", len(dirs)).Msg("browse cache dirs inserted")
	return nil
}

func insertBrowseCacheCounts(ctx context.Context, tx *sql.Tx, counts map[browseCacheCountKey]int) error {
	started := time.Now()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO BrowseDirCounts (ParentDirDBID, ChildDirDBID, SystemDBID, FileCount)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("browse cache: failed to prepare count insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for key, count := range counts {
		if _, insertErr := stmt.ExecContext(
			ctx, key.parentDirID, key.childDirID, key.systemDBID, count,
		); insertErr != nil {
			return fmt.Errorf("browse cache: failed to insert count: %w", insertErr)
		}
	}
	log.Debug().Dur("duration", time.Since(started)).Int("entries", len(counts)).Msg("browse cache counts inserted")
	return nil
}

// attachBrowseCacheDirLookup points the builder at the existing BrowseDirs rows
// so dirs shared across systems keep their DBIDs (other systems' count rows
// reference them). Returns the first DBID available for newly created dirs, and
// a closer for the prepared statement.
//
// This resolves one path at a time instead of loading the whole table. The table
// accumulates every dir from every system already indexed, while a refresh only
// ever touches the ancestor closure of one system's media paths, so a full load
// costs O(all dirs) per system and grew to 1.2s on the last system of a 131-
// system run while inserting fewer rows than the first. Probing is O(dirs this
// system touches) against the Path unique index, and each distinct path is
// probed at most once because ensureDir caches hits and misses alike.
func attachBrowseCacheDirLookup(
	ctx context.Context, tx *sql.Tx, builder *browseCacheBuilder,
) (firstNewID int64, closeFn func(), err error) {
	// MAX over an INTEGER PRIMARY KEY is a single index seek, not a scan. Taken
	// inside the refresh transaction, so the !compatible truncation above is
	// already visible and restarts numbering at 1.
	if scanErr := tx.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(DBID), 0) + 1 FROM BrowseDirs").Scan(&firstNewID); scanErr != nil {
		return 0, nil, fmt.Errorf("browse cache: failed to read next dir id: %w", scanErr)
	}
	builder.nextDirID = firstNewID

	stmt, err := tx.PrepareContext(ctx,
		"SELECT DBID, ParentDirDBID, Path, Name, IsVirtual FROM BrowseDirs WHERE Path = ?")
	if err != nil {
		return 0, nil, fmt.Errorf("browse cache: failed to prepare dir lookup: %w", err)
	}

	builder.lookupExisting = func(path string) (*browseCacheDir, bool, error) {
		var dir browseCacheDir
		scanErr := stmt.QueryRowContext(ctx, path).
			Scan(&dir.id, &dir.parentID, &dir.path, &dir.name, &dir.isVirtual)
		switch {
		case errors.Is(scanErr, sql.ErrNoRows):
			return nil, false, nil
		case scanErr != nil:
			return nil, false, fmt.Errorf("browse cache: failed to look up dir %s: %w", path, scanErr)
		}
		return &dir, true, nil
	}
	return firstNewID, func() { _ = stmt.Close() }, nil
}

// scanBrowseCacheMediaForSystems feeds the target systems' non-missing media
// rows into the builder.
func scanBrowseCacheMediaForSystems(
	ctx context.Context, tx *sql.Tx, builder *browseCacheBuilder, inClause string, args []any,
) error {
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	// Ordered by (SystemDBID, Path), which is exactly what
	// media_system_present_path_idx yields, so the covering index serves the scan
	// and the ordering together. Ordering on m.DBID instead adds a temp B-tree
	// sort: DBID is the rowid, and the index does not carry it in sorted order.
	// Row order only decides which DBID a newly minted dir receives, so any stable
	// order will do — and Path order is steadier than rowid order, which depends
	// on insertion history. See browse_cache_refresh_plan_test.go.
	rows, err := tx.QueryContext(ctx,
		"SELECT m.SystemDBID, m.Path FROM Media m WHERE m.IsMissing = 0 AND m.SystemDBID IN ("+
			inClause+") ORDER BY m.SystemDBID, m.Path", args...)
	if err != nil {
		return fmt.Errorf("browse cache: failed to query system media: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var systemDBID int64
		var mediaPath string
		if scanErr := rows.Scan(&systemDBID, &mediaPath); scanErr != nil {
			return fmt.Errorf("browse cache: failed to scan system media: %w", scanErr)
		}
		builder.addMedia(systemDBID, mediaPath)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("browse cache: system media iteration error: %w", rowsErr)
	}
	return nil
}

// sqlPopulateBrowseCacheForSystems incrementally refreshes the browse cache
// for specific systems from committed media rows: existing dir rows are
// reused, missing dirs are added, and only the target systems' count rows
// are replaced. The cache version is left at the stale sentinel — serveable
// immediately, with the end-of-optimization full rebuild still pending to
// remove orphaned dirs and correct any drift. This is what makes browse
// usable per-system while a long index is still running.
func sqlPopulateBrowseCacheForSystems(ctx context.Context, db *sql.DB, systemDBIDs []int64) error {
	if len(systemDBIDs) == 0 {
		return nil
	}
	started := time.Now()
	builder := newBrowseCacheBuilder()

	// Per-statement timers. Round 8 of #1279 measured this function at 662,224 ms
	// run-wide — 11.04 min, 12.7% of a full reindex — and could not say which
	// statement spent it. The shape ruled out the obvious answers: the cost was
	// flat at ~6 s whether a system held 7 files or 1,441, systems holding zero
	// files paid nothing, and the inserts were 24-53 ms of an 8-19 s call. A flat
	// per-call cost points at setup or teardown rather than at the work, so every
	// step is timed separately and reported below.
	var timing browseCacheRefreshTiming

	// An on-disk cache from an older schema has to be cleared wholesale. That is
	// the same cascading full-table delete as the full rebuild, so it runs in its
	// own foreign-key-disabled transaction ahead of the refresh below. The common
	// path — a compatible cache — keeps enforcement on: its delete is scoped to
	// BrowseDirCounts, a child-only table where no cascade fires, and this
	// function runs once per system on every index.
	stepStart := time.Now()
	compatible, err := sqlBrowseCacheVersionCompatible(ctx, db)
	if err != nil {
		return err
	}
	timing.versionCheck = time.Since(stepStart)
	if !compatible {
		stepStart = time.Now()
		if clearErr := sqlClearBrowseCacheTables(ctx, db); clearErr != nil {
			return clearErr
		}
		timing.clearIncompatible = time.Since(stepStart)
	}

	// Timed separately from the statements that follow: BeginTx can block waiting
	// for a pooled connection, and connection wait is not query cost.
	stepStart = time.Now()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("browse cache: failed to begin system refresh transaction: %w", err)
	}
	timing.beginTx = time.Since(stepStart)
	defer func() { _ = tx.Rollback() }()

	stepStart = time.Now()
	firstNewID, closeLookup, err := attachBrowseCacheDirLookup(ctx, tx, builder)
	if err != nil {
		return err
	}
	timing.attachLookup = time.Since(stepStart)
	defer closeLookup()
	builder.ensureDir("/")

	args := make([]any, len(systemDBIDs))
	for i, id := range systemDBIDs {
		args[i] = id
	}
	inClause := prepareVariadic("?", ",", len(systemDBIDs))

	stepStart = time.Now()
	if err := scanBrowseCacheMediaForSystems(ctx, tx, builder, inClause, args); err != nil {
		return err
	}
	timing.mediaScan = time.Since(stepStart)
	// Must come before any write: ensureDir swallows lookup failures and mints a
	// new DBID for the path it could not resolve, so persisting past one would
	// duplicate a shared dir. The transaction rolls back untouched.
	if builder.lookupErr != nil {
		return builder.lookupErr
	}

	stepStart = time.Now()
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	if _, execErr := tx.ExecContext(ctx,
		"DELETE FROM BrowseDirCounts WHERE SystemDBID IN ("+inClause+")", args...); execErr != nil {
		return fmt.Errorf("browse cache: failed to clear system counts: %w", execErr)
	}
	timing.deleteCounts = time.Since(stepStart)

	newDirs := make(map[string]*browseCacheDir)
	for dirPath, dir := range builder.dirs {
		if dir.id >= firstNewID {
			newDirs[dirPath] = dir
		}
	}
	stepStart = time.Now()
	if err := insertBrowseCacheDirs(ctx, tx, newDirs); err != nil {
		return err
	}
	if err := insertBrowseCacheCounts(ctx, tx, builder.counts); err != nil {
		return err
	}
	timing.inserts = time.Since(stepStart)

	// Mark the cache serveable but pending a full rebuild. Never downgrade
	// visibility: the sentinel is only meaningful alongside present rows,
	// which this refresh guarantees.
	if _, cfgErr := tx.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigBrowseIndexVersion,
		browseCacheInvalidatedVersion,
	); cfgErr != nil {
		return fmt.Errorf("browse cache: failed to mark system refresh: %w", cfgErr)
	}
	// Only a full rebuild proves unfiltered coverage across every system.
	if _, cfgErr := tx.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigBrowseIndexComplete,
		"0",
	); cfgErr != nil {
		return fmt.Errorf("browse cache: failed to mark partial index coverage: %w", cfgErr)
	}

	stepStart = time.Now()
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("browse cache: failed to commit system refresh: %w", err)
	}
	timing.commit = time.Since(stepStart)

	total := time.Since(started)
	timing.unattributed = total - timing.sum()
	timing.apply(log.Info().
		Int("systems", len(systemDBIDs)).
		Int("newDirs", len(newDirs)).
		Int("counts", len(builder.counts)).
		Dur("duration", total)).
		Msg("browse cache refreshed for systems")
	return nil
}

// browseCacheRefreshTiming breaks sqlPopulateBrowseCacheForSystems into its
// individual statements so a slow refresh names the statement responsible.
//
// Every field is reported on every refresh, including the near-zero ones: the
// point is to see which one is not near-zero, and a field omitted when small is
// a field that cannot be ruled out later.
type browseCacheRefreshTiming struct {
	versionCheck      time.Duration
	clearIncompatible time.Duration
	beginTx           time.Duration
	attachLookup      time.Duration
	mediaScan         time.Duration
	deleteCounts      time.Duration
	inserts           time.Duration
	commit            time.Duration
	unattributed      time.Duration
}

func (t *browseCacheRefreshTiming) sum() time.Duration {
	return t.versionCheck + t.clearIncompatible + t.beginTx + t.attachLookup +
		t.mediaScan + t.deleteCounts + t.inserts + t.commit
}

func (t *browseCacheRefreshTiming) apply(e *zerolog.Event) *zerolog.Event {
	return e.
		Dur("versionCheck", t.versionCheck).
		Dur("clearIncompatible", t.clearIncompatible).
		Dur("beginTx", t.beginTx).
		Dur("attachLookup", t.attachLookup).
		Dur("mediaScan", t.mediaScan).
		Dur("deleteCounts", t.deleteCounts).
		Dur("inserts", t.inserts).
		Dur("commit", t.commit).
		Dur("unattributed", t.unattributed)
}

func sqlBrowseCacheVersionCompatible(ctx context.Context, db sqlQueryable) (bool, error) {
	var version string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigBrowseIndexVersion,
	).Scan(&version)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("browse cache version query: %w", err)
	}
	return version == browseCacheSchemaVersion || version == browseCacheInvalidatedVersion, nil
}

func sqlInvalidateBrowseCache(ctx context.Context, db sqlQueryable) error {
	_, err := db.ExecContext(ctx, `
		UPDATE DBConfig SET Value = ?
		WHERE Name = ? AND Value IN (?, ?)`,
		browseCacheInvalidatedVersion,
		DBConfigBrowseIndexVersion,
		browseCacheSchemaVersion,
		browseCacheInvalidatedVersion,
	)
	if err != nil {
		return fmt.Errorf("failed to mark browse cache stale: %w", err)
	}
	return nil
}
