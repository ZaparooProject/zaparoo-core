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
	"strings"

	"github.com/rs/zerolog/log"
)

// sqlPopulateBrowseCache rebuilds the BrowseCache table from the current Media
// data. Reads all paths, extracts every directory level, then bulk-inserts
// aggregated counts.
func sqlPopulateBrowseCache(ctx context.Context, db *sql.DB) error {
	// Read all paths from Media
	rows, err := db.QueryContext(ctx, "SELECT Path FROM Media")
	if err != nil {
		return fmt.Errorf("browse cache: failed to query paths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Count files per directory prefix. Each path contributes to all its
	// ancestor directories. Virtual scheme paths (containing "://") only
	// contribute to their scheme prefix.
	dirCounts := make(map[string]int)
	for rows.Next() {
		var p string
		if scanErr := rows.Scan(&p); scanErr != nil {
			return fmt.Errorf("browse cache: failed to scan path: %w", scanErr)
		}

		if strings.Contains(p, "://") {
			idx := strings.Index(p, "://")
			scheme := p[:idx+3]
			dirCounts[scheme]++
			continue
		}

		// Filesystem path: extract every ancestor directory.
		dir := path.Dir(p)
		for dir != "" && dir != "." && dir != "/" {
			dirCounts[dir+"/"]++
			dir = path.Dir(dir)
		}
		if dir == "/" {
			dirCounts["/"]++
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("browse cache: rows iteration error: %w", rowsErr)
	}

	// Bulk insert BrowseCache entries inside a transaction so the old cache
	// remains visible to concurrent readers until the new data is committed.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("browse cache: failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, delErr := tx.ExecContext(ctx, "DELETE FROM BrowseCache"); delErr != nil {
		return fmt.Errorf("failed to clear browse cache: %w", delErr)
	}

	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO BrowseCache (DirPath, ParentPath, Name, FileCount, IsVirtual) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("browse cache: failed to prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for dirPath, count := range dirCounts {
		parentPath, name := browseCacheParentAndName(dirPath)
		isVirtual := strings.Contains(dirPath, "://")
		if _, insertErr := stmt.ExecContext(ctx, dirPath, parentPath, name, count, isVirtual); insertErr != nil {
			return fmt.Errorf("browse cache: failed to insert %s: %w", dirPath, insertErr)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("browse cache: failed to commit: %w", err)
	}

	log.Info().Int("entries", len(dirCounts)).Msg("browse cache populated")
	return nil
}

// browseCacheParentAndName extracts the parent path and display name from a
// directory path or virtual scheme.
//
//	"/media/fat/games/SNES/" → ("/media/fat/games/", "SNES")
//	"/media/fat/"            → ("/media/", "fat")
//	"steam://"               → ("", "steam://")
func browseCacheParentAndName(dirPath string) (parentPath, name string) {
	// Virtual schemes have no parent
	if strings.Contains(dirPath, "://") {
		return "", dirPath
	}

	// Strip trailing slash, split into parent + name
	trimmed := strings.TrimSuffix(dirPath, "/")
	parent := path.Dir(trimmed)
	name = path.Base(trimmed)

	if parent == "/" || parent == "." {
		return "", name
	}
	return parent + "/", name
}
