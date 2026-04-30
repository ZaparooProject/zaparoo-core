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
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const browseIndexVersion = "2"

type browseV2Dir struct {
	parentID  *int64
	path      string
	name      string
	id        int64
	isVirtual bool
}

type browseV2Entry struct {
	name        string
	nameChar    string
	fileName    string
	mediaDBID   int64
	systemDBID  int64
	parentDirID int64
}

type browseV2CountKey struct {
	parentDirID int64
	childDirID  int64
	systemDBID  int64
}

type browseV2Builder struct {
	dirs      map[string]*browseV2Dir
	counts    map[browseV2CountKey]int
	entries   []browseV2Entry
	nextDirID int64
}

func newBrowseV2Builder() *browseV2Builder {
	return &browseV2Builder{
		dirs:      make(map[string]*browseV2Dir),
		counts:    make(map[browseV2CountKey]int),
		nextDirID: 1,
	}
}

// sqlPopulateBrowseCache rebuilds the compact browse v2 tables from current,
// non-missing media rows. The historical method name is kept because the
// background optimization step still calls this "browse_cache".
func sqlPopulateBrowseCache(ctx context.Context, db *sql.DB) error {
	started := time.Now()
	readStarted := time.Now()
	builder := newBrowseV2Builder()
	builder.ensureDir("/")

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("browse v2: failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := scanBrowseV2Media(ctx, tx, builder); err != nil {
		return err
	}
	log.Debug().
		Dur("duration", time.Since(readStarted)).
		Int("dirs", len(builder.dirs)).
		Int("entries", len(builder.entries)).
		Int("counts", len(builder.counts)).
		Msg("browse v2 media scan complete")

	deleteStarted := time.Now()
	if err := dropBrowseV2EntryIndexes(ctx, tx); err != nil {
		return err
	}
	for _, stmt := range []string{
		"DELETE FROM BrowseDirCounts",
		"DELETE FROM BrowseEntries",
		"DELETE FROM BrowseDirs",
	} {
		if _, execErr := tx.ExecContext(ctx, stmt); execErr != nil {
			return fmt.Errorf("browse v2: failed to clear tables: %w", execErr)
		}
	}
	log.Debug().Dur("duration", time.Since(deleteStarted)).Msg("browse v2 cleared old entries")

	if err := insertBrowseV2Dirs(ctx, tx, builder.dirs); err != nil {
		return err
	}
	if err := insertBrowseV2Entries(ctx, tx, builder.entries); err != nil {
		return err
	}
	if err := createBrowseV2EntryIndexes(ctx, tx); err != nil {
		return err
	}
	if err := insertBrowseV2Counts(ctx, tx, builder.counts); err != nil {
		return err
	}

	if _, cfgErr := tx.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigBrowseIndexVersion,
		browseIndexVersion,
	); cfgErr != nil {
		return fmt.Errorf("browse v2: failed to mark index ready: %w", cfgErr)
	}

	commitStarted := time.Now()
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("browse v2: failed to commit: %w", err)
	}
	log.Debug().Dur("duration", time.Since(commitStarted)).Msg("browse v2 transaction committed")

	log.Info().
		Int("dirs", len(builder.dirs)).
		Int("entries", len(builder.entries)).
		Int("counts", len(builder.counts)).
		Dur("duration", time.Since(started)).
		Msg("browse v2 populated")
	return nil
}

func scanBrowseV2Media(ctx context.Context, tx *sql.Tx, builder *browseV2Builder) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT m.DBID, m.SystemDBID, m.Path, mt.Name
		FROM Media m
		INNER JOIN MediaTitles mt ON m.MediaTitleDBID = mt.DBID
		WHERE m.IsMissing = 0
		ORDER BY m.DBID`)
	if err != nil {
		return fmt.Errorf("browse v2: failed to query media: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var mediaDBID, systemDBID int64
		var mediaPath, title string
		if scanErr := rows.Scan(&mediaDBID, &systemDBID, &mediaPath, &title); scanErr != nil {
			return fmt.Errorf("browse v2: failed to scan media: %w", scanErr)
		}
		builder.addMedia(mediaDBID, systemDBID, mediaPath, title)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("browse v2: rows iteration error: %w", rowsErr)
	}
	return nil
}

func (b *browseV2Builder) addMedia(mediaDBID, systemDBID int64, mediaPath, title string) {
	parentPath, fileName := browseV2ParentAndFileName(mediaPath)
	parent := b.ensureDir(parentPath)
	b.entries = append(b.entries, browseV2Entry{
		mediaDBID:   mediaDBID,
		systemDBID:  systemDBID,
		parentDirID: parent.id,
		name:        title,
		nameChar:    BrowseNameFirstChar(title),
		fileName:    fileName,
	})

	for _, pair := range b.countPairsForPath(mediaPath) {
		key := browseV2CountKey{
			parentDirID: pair.parent.id,
			childDirID:  pair.child.id,
			systemDBID:  systemDBID,
		}
		b.counts[key]++
	}
}

func (b *browseV2Builder) ensureDir(dirPath string) *browseV2Dir {
	if dir, ok := b.dirs[dirPath]; ok {
		return dir
	}
	parentPath, name, isVirtual := browseV2DirParentAndName(dirPath)
	var parentID *int64
	if parentPath != "" {
		parent := b.ensureDir(parentPath)
		parentID = &parent.id
	}
	dir := &browseV2Dir{
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

type browseV2CountPair struct {
	parent *browseV2Dir
	child  *browseV2Dir
}

func (b *browseV2Builder) countPairsForPath(mediaPath string) []browseV2CountPair {
	if idx := strings.Index(mediaPath, "://"); idx >= 0 {
		return []browseV2CountPair{{parent: b.ensureDir(""), child: b.ensureDir(mediaPath[:idx+3])}}
	}

	dirs := browseV2AncestorDirs(mediaPath)
	pairs := make([]browseV2CountPair, 0, len(dirs)+1)
	root := b.ensureDir("/")
	pairs = append(pairs, browseV2CountPair{parent: root, child: root})
	for i := 0; i+1 < len(dirs); i++ {
		pairs = append(pairs, browseV2CountPair{
			parent: b.ensureDir(dirs[i]),
			child:  b.ensureDir(dirs[i+1]),
		})
	}
	return pairs
}

func browseV2AncestorDirs(mediaPath string) []string {
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

func browseV2ParentAndFileName(mediaPath string) (parentPath, fileName string) {
	if idx := strings.Index(mediaPath, "://"); idx >= 0 {
		return mediaPath[:idx+3], strings.TrimPrefix(mediaPath[idx+3:], "/")
	}
	if !strings.HasPrefix(mediaPath, "/") {
		mediaPath = "/" + mediaPath
	}
	if lastSlash := strings.LastIndex(mediaPath, "/"); lastSlash >= 0 {
		return mediaPath[:lastSlash+1], mediaPath[lastSlash+1:]
	}
	return "", mediaPath
}

func browseV2DirParentAndName(dirPath string) (parentPath, name string, isVirtual bool) {
	if dirPath == "" {
		return "", "", false
	}
	if strings.Contains(dirPath, "://") {
		return "", dirPath, true
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

func insertBrowseV2Dirs(ctx context.Context, tx *sql.Tx, dirs map[string]*browseV2Dir) error {
	started := time.Now()
	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO BrowseDirs (DBID, ParentDirDBID, Path, Name, IsVirtual) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("browse v2: failed to prepare dir insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	ordered := make([]*browseV2Dir, 0, len(dirs))
	for _, dir := range dirs {
		ordered = append(ordered, dir)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].id < ordered[j].id })

	for _, dir := range ordered {
		_, insertErr := stmt.ExecContext(ctx, dir.id, dir.parentID, dir.path, dir.name, dir.isVirtual)
		if insertErr != nil {
			return fmt.Errorf("browse v2: failed to insert dir %s: %w", dir.path, insertErr)
		}
	}
	log.Debug().Dur("duration", time.Since(started)).Int("entries", len(dirs)).Msg("browse v2 dirs inserted")
	return nil
}

func insertBrowseV2Entries(ctx context.Context, tx *sql.Tx, entries []browseV2Entry) error {
	started := time.Now()
	const chunkSize = 100
	for start := 0; start < len(entries); start += chunkSize {
		end := start + chunkSize
		if end > len(entries) {
			end = len(entries)
		}
		if err := insertBrowseV2EntryChunk(ctx, tx, entries[start:end]); err != nil {
			return err
		}
	}
	log.Debug().Dur("duration", time.Since(started)).Int("entries", len(entries)).Msg("browse v2 entries inserted")
	return nil
}

func dropBrowseV2EntryIndexes(ctx context.Context, tx *sql.Tx) error {
	started := time.Now()
	for _, stmt := range []string{
		"DROP INDEX IF EXISTS idx_browseentries_parent_system_name",
		"DROP INDEX IF EXISTS idx_browseentries_parent_system_file",
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("browse v2: failed to drop entry index: %w", err)
		}
	}
	log.Debug().Dur("duration", time.Since(started)).Msg("browse v2 entry indexes dropped")
	return nil
}

func createBrowseV2EntryIndexes(ctx context.Context, tx *sql.Tx) error {
	started := time.Now()
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_browseentries_parent_system_name
			ON BrowseEntries(ParentDirDBID, SystemDBID, Name, MediaDBID)`,
		`CREATE INDEX IF NOT EXISTS idx_browseentries_parent_system_file
			ON BrowseEntries(ParentDirDBID, SystemDBID, FileName, MediaDBID)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("browse v2: failed to create entry index: %w", err)
		}
	}
	log.Debug().Dur("duration", time.Since(started)).Msg("browse v2 entry indexes created")
	return nil
}

func insertBrowseV2EntryChunk(ctx context.Context, tx *sql.Tx, entries []browseV2Entry) error {
	if len(entries) == 0 {
		return nil
	}
	placeholders := make([]string, len(entries))
	args := make([]any, 0, len(entries)*6)
	for i, entry := range entries {
		placeholders[i] = "(?, ?, ?, ?, ?, ?)"
		args = append(args,
			entry.parentDirID,
			entry.mediaDBID,
			entry.systemDBID,
			entry.name,
			entry.nameChar,
			entry.fileName,
		)
	}
	//nolint:gosec // The dynamic SQL only expands internally generated value placeholders.
	query := `INSERT INTO BrowseEntries
		(ParentDirDBID, MediaDBID, SystemDBID, Name, NameFirstChar, FileName)
		VALUES ` + strings.Join(placeholders, ",")
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("browse v2: failed to insert entry chunk: %w", err)
	}
	return nil
}

func insertBrowseV2Counts(ctx context.Context, tx *sql.Tx, counts map[browseV2CountKey]int) error {
	started := time.Now()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO BrowseDirCounts (ParentDirDBID, ChildDirDBID, SystemDBID, FileCount)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("browse v2: failed to prepare count insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for key, count := range counts {
		if _, insertErr := stmt.ExecContext(
			ctx, key.parentDirID, key.childDirID, key.systemDBID, count,
		); insertErr != nil {
			return fmt.Errorf("browse v2: failed to insert count: %w", insertErr)
		}
	}
	log.Debug().Dur("duration", time.Since(started)).Int("entries", len(counts)).Msg("browse v2 counts inserted")
	return nil
}

func sqlInvalidateBrowseCache(ctx context.Context, db sqlQueryable, _ []int64, _ bool) error {
	_, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigBrowseIndexVersion,
		"0",
	)
	if err != nil {
		return fmt.Errorf("failed to mark browse v2 stale: %w", err)
	}
	return nil
}
