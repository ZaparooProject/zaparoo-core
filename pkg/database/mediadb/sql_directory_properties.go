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
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

type normalizedDirectoryProperty struct {
	path    string
	typeTag string
	text    string
}

func normalizeDirectoryPropertyPath(value string) (string, error) {
	if value == "" || strings.Contains(value, "://") {
		return "", fmt.Errorf("invalid directory property path %q", value)
	}
	path := filepath.ToSlash(filepath.Clean(value))
	if path == "." || path == "" {
		return "", fmt.Errorf("invalid directory property path %q", value)
	}
	if path != "/" {
		path = strings.TrimRight(path, "/")
	}
	return path, nil
}

func normalizeDirectoryProperties(rows []database.DirectoryProperty) ([]normalizedDirectoryProperty, error) {
	byKey := make(map[string]normalizedDirectoryProperty, len(rows))
	for i := range rows {
		path, err := normalizeDirectoryPropertyPath(rows[i].Path)
		if err != nil {
			return nil, err
		}
		if !isImageProperty(rows[i].TypeTag) {
			return nil, fmt.Errorf("directory property %q is not an image property", rows[i].TypeTag)
		}
		if rows[i].Text == "" {
			return nil, fmt.Errorf("directory property %q has empty text", rows[i].TypeTag)
		}
		row := normalizedDirectoryProperty{
			path:    path,
			typeTag: rows[i].TypeTag,
			text:    filepath.ToSlash(rows[i].Text),
		}
		key := row.path + "\x00" + row.typeTag
		if existing, ok := byKey[key]; ok && existing.text != row.text {
			return nil, fmt.Errorf("conflicting directory property for %q and %q", row.path, row.typeTag)
		}
		byKey[key] = row
	}

	normalized := make([]normalizedDirectoryProperty, 0, len(byKey))
	for _, row := range byKey {
		normalized = append(normalized, row)
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].path != normalized[j].path {
			return normalized[i].path < normalized[j].path
		}
		return normalized[i].typeTag < normalized[j].typeTag
	})
	return normalized, nil
}

func directoryPropertySnapshotsEqual(
	stored map[string]string, desired []normalizedDirectoryProperty,
) bool {
	if len(stored) != len(desired) {
		return false
	}
	for i := range desired {
		if stored[desired[i].path+"\x00"+desired[i].typeTag] != desired[i].text {
			return false
		}
	}
	return true
}

func loadDirectoryPropertySnapshot(
	ctx context.Context, db sqlQueryable, systemDBID int64,
) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT dp.Path, tt.Type || ':' || t.Tag, dp.Text
		FROM DirectoryProperties dp
		JOIN Tags t ON t.DBID = dp.TypeTagDBID
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE dp.SystemDBID = ?
	`, systemDBID)
	if err != nil {
		return nil, fmt.Errorf("load existing: %w", err)
	}
	defer func() { _ = rows.Close() }()

	stored := make(map[string]string)
	for rows.Next() {
		var path, typeTag, text string
		if scanErr := rows.Scan(&path, &typeTag, &text); scanErr != nil {
			return nil, fmt.Errorf("scan existing: %w", scanErr)
		}
		stored[path+"\x00"+typeTag] = text
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("existing rows: %w", rowsErr)
	}
	return stored, nil
}

// ReplaceDirectoryProperties atomically replaces the complete directory image
// property snapshot for one system. BrowseDirs is deliberately not involved:
// its DBIDs are rebuilt with the browse cache, while these path identities must
// survive that rebuild until the next media-folder scrape.
func (db *MediaDB) ReplaceDirectoryProperties(
	ctx context.Context, systemDBID int64, rows []database.DirectoryProperty,
) (changed bool, retErr error) {
	if db.sql.Load() == nil {
		return false, ErrNullSQL
	}
	if systemDBID <= 0 {
		return false, fmt.Errorf("invalid directory property system DBID %d", systemDBID)
	}
	desired, err := normalizeDirectoryProperties(rows)
	if err != nil {
		return false, err
	}

	tx, err := db.sql.Load().BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("replace directory properties: begin transaction: %w", err)
	}
	defer func() {
		if retErr != nil || !changed {
			_ = tx.Rollback()
		}
	}()

	var systemID string
	systemQuery := "SELECT SystemID FROM Systems WHERE DBID = ?"
	if err = tx.QueryRowContext(ctx, systemQuery, systemDBID).Scan(&systemID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("replace directory properties: system DBID %d not found", systemDBID)
		}
		return false, fmt.Errorf("replace directory properties: resolve system: %w", err)
	}

	stored, err := loadDirectoryPropertySnapshot(ctx, tx, systemDBID)
	if err != nil {
		return false, fmt.Errorf("replace directory properties: %w", err)
	}
	if directoryPropertySnapshotsEqual(stored, desired) {
		return false, nil
	}

	if _, err = tx.ExecContext(ctx, "DELETE FROM DirectoryProperties WHERE SystemDBID = ?", systemDBID); err != nil {
		return false, fmt.Errorf("replace directory properties: delete old snapshot: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO DirectoryProperties (SystemDBID, Path, TypeTagDBID, Text)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return false, fmt.Errorf("replace directory properties: prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for i := range desired {
		typeTagDBID, resolveErr := resolvePropertyTypeTag(ctx, tx, desired[i].typeTag)
		if resolveErr != nil {
			return false, fmt.Errorf("replace directory properties: %w", resolveErr)
		}
		if _, insertErr := stmt.ExecContext(
			ctx, systemDBID, desired[i].path, typeTagDBID, desired[i].text,
		); insertErr != nil {
			return false, fmt.Errorf("replace directory properties: insert %q: %w", desired[i].path, insertErr)
		}
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("replace directory properties: commit: %w", err)
	}
	changed = true
	db.recordScrapeImageSystem(systemID)
	return true, nil
}

func (db *MediaDB) recordScrapeImageSystem(systemID string) {
	if systemID == "" {
		return
	}
	db.scrapeImageChangesMu.Lock()
	defer db.scrapeImageChangesMu.Unlock()
	if db.scrapeImageSystems == nil {
		db.scrapeImageSystems = make(map[string]struct{})
	}
	db.scrapeImageSystems[systemID] = struct{}{}
}

// GetDirectoryProperties returns file-backed properties for one stable
// directory identity. Content type is inferred from Text by media.image.
func (db *MediaDB) GetDirectoryProperties(
	ctx context.Context, systemDBID int64, path string,
) ([]database.MediaProperty, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	normalizedPath, err := normalizeDirectoryPropertyPath(path)
	if err != nil {
		return nil, err
	}
	rows, err := db.sql.Load().QueryContext(ctx, `
		SELECT dp.TypeTagDBID, tt.Type || ':' || t.Tag, dp.Text
		FROM DirectoryProperties dp
		JOIN Tags t ON t.DBID = dp.TypeTagDBID
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE dp.SystemDBID = ? AND dp.Path = ?
		ORDER BY dp.TypeTagDBID
	`, systemDBID, normalizedPath)
	if err != nil {
		return nil, fmt.Errorf("get directory properties: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	properties := make([]database.MediaProperty, 0)
	for rows.Next() {
		var property database.MediaProperty
		if scanErr := rows.Scan(&property.TypeTagDBID, &property.TypeTag, &property.Text); scanErr != nil {
			return nil, fmt.Errorf("get directory properties: scan: %w", scanErr)
		}
		properties = append(properties, property)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("get directory properties: rows: %w", rowsErr)
	}
	return properties, nil
}
