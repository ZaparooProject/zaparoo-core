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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/rs/zerolog/log"
)

const (
	insertTagSQL     = `INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES (?, ?, ?)`
	insertTagTypeSQL = `INSERT INTO TagTypes (DBID, Type) VALUES (?, ?)`
)

func sqlFindTagType(ctx context.Context, db *sql.DB, tagType database.TagType) (database.TagType, error) {
	var row database.TagType
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, Type
		from TagTypes
		where DBID = ?
		or Type = ?
		LIMIT 1;
	`)
	if err != nil {
		return row, fmt.Errorf("failed to prepare find tag type statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx,
		tagType.DBID,
		tagType.Type,
	).Scan(
		&row.DBID,
		&row.Type,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan tag type row: %w", err)
	}
	return row, nil
}

func sqlInsertTagTypeWithPreparedStmt(
	ctx context.Context, stmt *sql.Stmt, row database.TagType,
) (database.TagType, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	res, err := stmt.ExecContext(ctx, dbID, row.Type)
	if err != nil {
		return row, fmt.Errorf("failed to execute prepared insert tag type statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for tag type: %w", err)
	}

	row.DBID = lastID
	return row, nil
}

func sqlInsertTagType(ctx context.Context, db *sql.DB, row database.TagType) (database.TagType, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}
	stmt, err := db.PrepareContext(ctx, `
		insert into
		TagTypes
		(DBID, Type)
		values (?, ?)
	`)
	if err != nil {
		return row, fmt.Errorf("failed to prepare insert tag type statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	res, err := stmt.ExecContext(ctx,
		dbID,
		row.Type,
	)
	if err != nil {
		return row, fmt.Errorf("failed to execute insert tag type statement: %w", err)
	}
	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for tag type: %w", err)
	}
	row.DBID = lastID
	return row, nil
}

func sqlFindTag(ctx context.Context, db *sql.DB, tagType database.Tag) (database.Tag, error) {
	var row database.Tag
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, TypeDBID, Tag
		from Tags
		where DBID = ?
		or Tag = ?
		LIMIT 1;
	`)
	// TODO: Add TagType dependency when unknown tags supported
	if err != nil {
		return row, fmt.Errorf("failed to prepare find tag statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx,
		tagType.DBID,
		tagType.Tag,
	).Scan(
		&row.DBID,
		&row.TypeDBID,
		&row.Tag,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan tag row: %w", err)
	}
	return row, nil
}

func sqlInsertTagWithPreparedStmt(ctx context.Context, stmt *sql.Stmt, row database.Tag) (database.Tag, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	res, err := stmt.ExecContext(ctx, dbID, row.TypeDBID, row.Tag)
	if err != nil {
		return row, fmt.Errorf("failed to execute prepared insert tag statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for tag: %w", err)
	}

	row.DBID = lastID
	return row, nil
}

func sqlInsertTag(ctx context.Context, db *sql.DB, row database.Tag) (database.Tag, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	stmt, err := db.PrepareContext(ctx, insertTagSQL)
	if err != nil {
		return row, fmt.Errorf("failed to prepare insert tag statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	res, err := stmt.ExecContext(ctx, dbID, row.TypeDBID, row.Tag)
	if err != nil {
		return row, fmt.Errorf("failed to execute insert tag statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for tag: %w", err)
	}

	row.DBID = lastID
	return row, nil
}

func sqlGetAllTags(ctx context.Context, db *sql.DB) ([]database.Tag, error) {
	rows, err := db.QueryContext(ctx, "SELECT DBID, Tag, TypeDBID FROM Tags ORDER BY DBID")
	if err != nil {
		return nil, fmt.Errorf("failed to query tags: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	tags := make([]database.Tag, 0)
	for rows.Next() {
		var tag database.Tag
		if err := rows.Scan(&tag.DBID, &tag.Tag, &tag.TypeDBID); err != nil {
			return nil, fmt.Errorf("failed to scan tag: %w", err)
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func sqlGetAllTagTypes(ctx context.Context, db *sql.DB) ([]database.TagType, error) {
	rows, err := db.QueryContext(ctx, "SELECT DBID, Type FROM TagTypes ORDER BY DBID")
	if err != nil {
		return nil, fmt.Errorf("failed to query tag types: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	tagTypes := make([]database.TagType, 0)
	for rows.Next() {
		var tagType database.TagType
		if err := rows.Scan(&tagType.DBID, &tagType.Type); err != nil {
			return nil, fmt.Errorf("failed to scan tag type: %w", err)
		}
		tagTypes = append(tagTypes, tagType)
	}
	return tagTypes, rows.Err()
}

// sqlGetAllUsedTags - Ultra-fast query for all tags that are actually in use
// This avoids the expensive system filtering by directly querying MediaTags
func sqlGetAllUsedTags(ctx context.Context, db *sql.DB) ([]database.TagInfo, error) {
	sqlQuery := `
		SELECT DISTINCT TagTypes.Type, Tags.Tag
		FROM TagTypes
		JOIN Tags ON TagTypes.DBID = Tags.TypeDBID
		WHERE Tags.DBID IN (SELECT DISTINCT TagDBID FROM MediaTags)
		ORDER BY TagTypes.Type, Tags.Tag`

	stmt, err := db.PrepareContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare all used tags statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to execute all used tags query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()

	tags := make([]database.TagInfo, 0, 100)
	for rows.Next() {
		var tagType, tag string
		if scanErr := rows.Scan(&tagType, &tag); scanErr != nil {
			return nil, fmt.Errorf("failed to scan all used tag result: %w", scanErr)
		}
		tags = append(tags, database.TagInfo{
			Type: tagType,
			Tag:  tag,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tags, nil
}

// sqlGetTags - Optimized query using subquery approach
// This performs significantly better than the original 6-table join by filtering early
func sqlGetTags(
	ctx context.Context,
	db *sql.DB,
	systems []systemdefs.System,
) ([]database.TagInfo, error) {
	if len(systems) == 0 {
		return nil, errors.New("no systems provided for tag search")
	}

	args := make([]any, 0, len(systems))
	for _, sys := range systems {
		args = append(args, sys.ID)
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	sqlQuery := `
		SELECT DISTINCT TagTypes.Type, Tags.Tag
		FROM TagTypes
		JOIN Tags ON TagTypes.DBID = Tags.TypeDBID
		WHERE Tags.DBID IN (
			SELECT DISTINCT mt.TagDBID
			FROM MediaTags mt
			JOIN Media m ON mt.MediaDBID = m.DBID
			JOIN MediaTitles mtl ON m.MediaTitleDBID = mtl.DBID
			JOIN Systems s ON mtl.SystemDBID = s.DBID
			WHERE s.SystemID IN (` +
		prepareVariadic("?", ",", len(systems)) +
		`)
		)
		ORDER BY TagTypes.Type, Tags.Tag`

	stmt, err := db.PrepareContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare optimized tags statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute optimized tags query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()

	tags := make([]database.TagInfo, 0, 100)
	for rows.Next() {
		var tagType, tag string
		if scanErr := rows.Scan(&tagType, &tag); scanErr != nil {
			return nil, fmt.Errorf("failed to scan optimized tag result: %w", scanErr)
		}
		tags = append(tags, database.TagInfo{
			Type: tagType,
			Tag:  tag,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tags, nil
}
