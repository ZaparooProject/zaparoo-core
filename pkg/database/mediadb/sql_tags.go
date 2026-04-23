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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

const (
	insertTagSQL     = `INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES (?, ?, ?)`
	insertTagTypeSQL = `INSERT INTO TagTypes (DBID, Type) VALUES (?, ?)`
)

func sqlFindTagType(ctx context.Context, db sqlQueryable, tagType database.TagType) (database.TagType, error) {
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

func sqlFindTag(ctx context.Context, db sqlQueryable, tagType database.Tag) (database.Tag, error) {
	var row database.Tag
	paddedTag := tags.PadTagValue(tagType.Tag)
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, TypeDBID, Tag
		from Tags
		where (DBID = ? and TypeDBID = ?)
		or (TypeDBID = ? and (Tag = ? or Tag = ?))
		LIMIT 1;
	`)
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
		tagType.TypeDBID,
		tagType.TypeDBID,
		tagType.Tag,
		paddedTag,
	).Scan(
		&row.DBID,
		&row.TypeDBID,
		&row.Tag,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan tag row: %w", err)
	}
	row.Tag = tags.UnpadTagValue(row.Tag)
	return row, nil
}

func sqlInsertTagWithPreparedStmt(ctx context.Context, stmt *sql.Stmt, row database.Tag) (database.Tag, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	paddedTag := tags.PadTagValue(row.Tag)
	res, err := stmt.ExecContext(ctx, dbID, row.TypeDBID, paddedTag)
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

	paddedTag := tags.PadTagValue(row.Tag)
	res, err := stmt.ExecContext(ctx, dbID, row.TypeDBID, paddedTag)
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

	dbTags := make([]database.Tag, 0)
	for rows.Next() {
		var tag database.Tag
		if err := rows.Scan(&tag.DBID, &tag.Tag, &tag.TypeDBID); err != nil {
			return nil, fmt.Errorf("failed to scan tag: %w", err)
		}
		tag.Tag = tags.UnpadTagValue(tag.Tag)
		dbTags = append(dbTags, tag)
	}
	return dbTags, rows.Err()
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

// sqlGetAllUsedTags queries for all tags that are currently in use with their
// aggregate usage counts across both MediaTags and MediaTitleTags.
func sqlGetAllUsedTags(ctx context.Context, db *sql.DB) ([]database.TagInfo, error) {
	// UNION ALL aggregates counts from file-level (MediaTags) and title-level
	// (MediaTitleTags) sources; the outer GROUP BY+SUM merges them per tag.
	// mediatags_tag_media_idx and mediatitletags_tag_idx make both GROUP BYs fast.
	sqlQuery := `
		SELECT tt.Type, t.Tag, SUM(cnt) AS Count
		FROM (
			SELECT TagDBID, COUNT(*) AS cnt FROM MediaTags GROUP BY TagDBID
			UNION ALL
			SELECT TagDBID, COUNT(*) AS cnt FROM MediaTitleTags GROUP BY TagDBID
		) agg
		JOIN Tags t ON agg.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		GROUP BY t.DBID, tt.Type, t.Tag
		ORDER BY tt.Type, t.Tag`

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

	result := make([]database.TagInfo, 0, 100)
	for rows.Next() {
		var tagType, tag string
		var count int64
		if scanErr := rows.Scan(&tagType, &tag, &count); scanErr != nil {
			return nil, fmt.Errorf("failed to scan all used tag result: %w", scanErr)
		}
		result = append(result, database.TagInfo{
			Type:  tagType,
			Tag:   tags.UnpadTagValue(tag),
			Count: count,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
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

	placeholders := prepareVariadic("?", ",", len(systems))

	args := make([]any, 0, len(systems)*2)
	for _, sys := range systems {
		args = append(args, sys.ID)
	}
	// Double args for the UNION's second WHERE clause
	args = append(args, args...)

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
			WHERE s.SystemID IN (` + placeholders + `)
			UNION
			SELECT DISTINCT mtt.TagDBID
			FROM MediaTitleTags mtt
			JOIN MediaTitles mtl ON mtt.MediaTitleDBID = mtl.DBID
			JOIN Systems s ON mtl.SystemDBID = s.DBID
			WHERE s.SystemID IN (` + placeholders + `)
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

	result := make([]database.TagInfo, 0, 100)
	for rows.Next() {
		var tagType, tag string
		if scanErr := rows.Scan(&tagType, &tag); scanErr != nil {
			return nil, fmt.Errorf("failed to scan optimized tag result: %w", scanErr)
		}
		result = append(result, database.TagInfo{
			Type: tagType,
			Tag:  tags.UnpadTagValue(tag),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}
