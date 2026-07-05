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
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const browseCacheSchemaVersion = "2"

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
	dirs      map[string]*browseCacheDir
	counts    map[browseCacheCountKey]int
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

	tx, err := db.BeginTx(ctx, nil)
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
	for _, stmt := range []string{
		"DELETE FROM BrowseDirCounts",
		"DELETE FROM BrowseDirs",
	} {
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
		return []browseCacheCountPair{{parent: b.ensureDir("/"), child: b.ensureDir(mediaPath[:idx+3])}}
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

// loadBrowseCacheDirs seeds the builder with the existing BrowseDirs rows so
// dirs shared across systems keep their DBIDs (other systems' count rows
// reference them). Returns the first DBID available for newly created dirs.
func loadBrowseCacheDirs(ctx context.Context, tx *sql.Tx, builder *browseCacheBuilder) (int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT DBID, ParentDirDBID, Path, Name, IsVirtual FROM BrowseDirs")
	if err != nil {
		return 0, fmt.Errorf("browse cache: failed to load existing dirs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var dir browseCacheDir
		if scanErr := rows.Scan(&dir.id, &dir.parentID, &dir.path, &dir.name, &dir.isVirtual); scanErr != nil {
			return 0, fmt.Errorf("browse cache: failed to scan existing dir: %w", scanErr)
		}
		builder.dirs[dir.path] = &dir
		if dir.id >= builder.nextDirID {
			builder.nextDirID = dir.id + 1
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return 0, fmt.Errorf("browse cache: existing dirs iteration error: %w", rowsErr)
	}
	return builder.nextDirID, nil
}

// scanBrowseCacheMediaForSystems feeds the target systems' non-missing media
// rows into the builder.
func scanBrowseCacheMediaForSystems(
	ctx context.Context, tx *sql.Tx, builder *browseCacheBuilder, inClause string, args []any,
) error {
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	rows, err := tx.QueryContext(ctx,
		"SELECT m.SystemDBID, m.Path FROM Media m WHERE m.IsMissing = 0 AND m.SystemDBID IN ("+
			inClause+") ORDER BY m.DBID", args...)
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

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("browse cache: failed to begin system refresh transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	firstNewID, err := loadBrowseCacheDirs(ctx, tx, builder)
	if err != nil {
		return err
	}
	builder.ensureDir("/")

	args := make([]any, len(systemDBIDs))
	for i, id := range systemDBIDs {
		args[i] = id
	}
	inClause := prepareVariadic("?", ",", len(systemDBIDs))

	if err := scanBrowseCacheMediaForSystems(ctx, tx, builder, inClause, args); err != nil {
		return err
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	if _, execErr := tx.ExecContext(ctx,
		"DELETE FROM BrowseDirCounts WHERE SystemDBID IN ("+inClause+")", args...); execErr != nil {
		return fmt.Errorf("browse cache: failed to clear system counts: %w", execErr)
	}

	newDirs := make(map[string]*browseCacheDir)
	for dirPath, dir := range builder.dirs {
		if dir.id >= firstNewID {
			newDirs[dirPath] = dir
		}
	}
	if err := insertBrowseCacheDirs(ctx, tx, newDirs); err != nil {
		return err
	}
	if err := insertBrowseCacheCounts(ctx, tx, builder.counts); err != nil {
		return err
	}

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

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("browse cache: failed to commit system refresh: %w", err)
	}

	log.Info().
		Int("systems", len(systemDBIDs)).
		Int("newDirs", len(newDirs)).
		Int("counts", len(builder.counts)).
		Dur("duration", time.Since(started)).
		Msg("browse cache refreshed for systems")
	return nil
}

func sqlInvalidateBrowseCache(ctx context.Context, db sqlQueryable) error {
	_, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigBrowseIndexVersion,
		browseCacheInvalidatedVersion,
	)
	if err != nil {
		return fmt.Errorf("failed to mark browse cache stale: %w", err)
	}
	return nil
}
