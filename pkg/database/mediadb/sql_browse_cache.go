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
	logBrowseMediaCountsBySystem(ctx, tx)

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

func sqlInvalidateBrowseCache(ctx context.Context, db sqlQueryable) error {
	_, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigBrowseIndexVersion,
		"0",
	)
	if err != nil {
		return fmt.Errorf("failed to mark browse cache stale: %w", err)
	}
	return nil
}
