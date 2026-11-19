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

package userdb

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
)

const mediaUserFieldColumns = ` SystemID, Path, Field, Value, Slug, CreatedAt, UpdatedAt `

// ListMediaUserFields returns every user-edited field, oldest write first, so
// a reader that keeps the last row it sees for a title keeps the newest.
func (db *UserDB) ListMediaUserFields() ([]database.MediaUserField, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	rows, err := conn.QueryContext(db.ctx, `select`+mediaUserFieldColumns+`
		from MediaUserFields order by UpdatedAt, DBID;`)
	if err != nil {
		return nil, fmt.Errorf("failed to query media user fields: %w", err)
	}
	return scanMediaUserFields(rows)
}

// GetMediaUserFieldsForTitle returns the user-edited fields of one title,
// oldest write first: those recorded against the given paths, and title-level
// ones recorded against a file that carried the same slug.
func (db *UserDB) GetMediaUserFieldsForTitle(
	systemID, slug string, paths []string,
) ([]database.MediaUserField, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	canonical := canonicalMediaPaths(paths)
	where, args := mediaUserFieldTitleClause(systemID, slug, canonical)
	//nolint:gosec // only placeholders are concatenated
	rows, err := conn.QueryContext(db.ctx, `select`+mediaUserFieldColumns+`
		from MediaUserFields where `+where+` order by UpdatedAt, DBID;`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query media user fields for a title: %w", err)
	}
	fields, err := scanMediaUserFields(rows)
	if err != nil {
		return nil, err
	}
	// A row matched by slug alone only counts for a title-level field.
	kept := fields[:0]
	for i := range fields {
		if slices.Contains(canonical, fields[i].Path) || database.IsMediaUserFieldTitleLevel(fields[i].Field) {
			kept = append(kept, fields[i])
		}
	}
	return kept, nil
}

// SetMediaUserField records one user-edited field against field.Path. A
// title-level field is first removed from the other paths whose rows the
// title owns (titlePaths), in the same transaction, so a title has one owner
// row per field. Slug is stored, not matched: a file that carried the slug
// may belong to another title now, and only the caller can tell.
func (db *UserDB) SetMediaUserField(field *database.MediaUserField, titlePaths []string) error {
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	path := pathutil.CanonicalMediaPath(field.Path)
	now := time.Now().Unix()
	return mediaUserDataTx(db.ctx, conn, now, func(tx *sql.Tx) error {
		if database.IsMediaUserFieldTitleLevel(field.Field) {
			where, args := mediaUserFieldTitleClause(field.SystemID, "", canonicalMediaPaths(titlePaths))
			args = append(args, field.Field, path)
			//nolint:gosec // only placeholders are concatenated
			if _, err := tx.ExecContext(db.ctx,
				`delete from MediaUserFields where `+where+` and Field = ? and Path <> ?;`, args...,
			); err != nil {
				return fmt.Errorf("failed to clear a media user field on sibling files: %w", err)
			}
		}
		if _, err := tx.ExecContext(db.ctx, `
			insert into MediaUserFields(SystemID, Path, Field, Value, Slug, CreatedAt, UpdatedAt)
			values (?, ?, ?, ?, ?, ?, ?)
			on conflict(SystemID, Path, Field) do update set
				Value = excluded.Value,
				Slug = excluded.Slug,
				UpdatedAt = excluded.UpdatedAt;
		`, field.SystemID, path, field.Field, field.Value, field.Slug, now, now); err != nil {
			return fmt.Errorf("failed to upsert media user field: %w", err)
		}
		return nil
	})
}

// DeleteMediaUserField removes one field from the given paths of a system. A
// title-level field is also removed from any file that carried slug. Deleting
// a field that is not recorded is not an error.
func (db *UserDB) DeleteMediaUserField(systemID, slug string, paths []string, field string) error {
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	if !database.IsMediaUserFieldTitleLevel(field) {
		slug = ""
	}
	where, args := mediaUserFieldTitleClause(systemID, slug, canonicalMediaPaths(paths))
	args = append(args, field)
	return mediaUserDataTx(db.ctx, conn, time.Now().Unix(), func(tx *sql.Tx) error {
		//nolint:gosec // only placeholders are concatenated
		if _, err := tx.ExecContext(db.ctx,
			`delete from MediaUserFields where `+where+` and Field = ?;`, args...,
		); err != nil {
			return fmt.Errorf("failed to delete media user field: %w", err)
		}
		return nil
	})
}

// mediaUserFieldTitleClause selects a system's rows recorded against any of
// paths or, when slug is not empty, against a file that carried slug.
func mediaUserFieldTitleClause(systemID, slug string, paths []string) (where string, args []any) {
	args = make([]any, 0, len(paths)+2)
	args = append(args, systemID)
	matches := make([]string, 0, 2)
	if len(paths) > 0 {
		matches = append(matches, `Path in (`+sqlPlaceholders(len(paths))+`)`)
		for _, path := range paths {
			args = append(args, path)
		}
	}
	if slug != "" {
		matches = append(matches, `Slug = ?`)
		args = append(args, slug)
	}
	if len(matches) == 0 {
		return `SystemID = ? and 0`, args
	}
	return `SystemID = ? and (` + strings.Join(matches, ` or `) + `)`, args
}

func canonicalMediaPaths(paths []string) []string {
	canonical := make([]string, 0, len(paths))
	for _, path := range paths {
		canonical = append(canonical, pathutil.CanonicalMediaPath(path))
	}
	return canonical
}

func sqlPlaceholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func scanMediaUserFields(rows *sql.Rows) ([]database.MediaUserField, error) {
	defer func() { _ = rows.Close() }()
	var out []database.MediaUserField
	for rows.Next() {
		var row database.MediaUserField
		if err := rows.Scan(
			&row.SystemID, &row.Path, &row.Field, &row.Value, &row.Slug, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan media user field row: %w", err)
		}
		row.Path = pathutil.CanonicalMediaPath(row.Path)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate media user field rows: %w", err)
	}
	return out, nil
}
