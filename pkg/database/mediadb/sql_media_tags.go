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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

const insertMediaTagSQL = `INSERT OR IGNORE INTO MediaTags (MediaDBID, TagDBID) VALUES (?, ?)`

func sqlFindMediaTag(ctx context.Context, db sqlQueryable, mediaTag database.MediaTag) (database.MediaTag, error) {
	var row database.MediaTag
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, MediaDBID, TagDBID
		from MediaTags
		where DBID = ?
		or (
			MediaDBID = ?
			and TagDBID = ?
		)
		LIMIT 1;
	`)
	if err != nil {
		return row, fmt.Errorf("failed to prepare find media tag statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx,
		mediaTag.DBID,
		mediaTag.MediaDBID,
		mediaTag.TagDBID,
	).Scan(
		&row.DBID,
		&row.MediaDBID,
		&row.TagDBID,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan media tag row: %w", err)
	}
	return row, nil
}

func sqlInsertMediaTagWithPreparedStmt(
	ctx context.Context, stmt *sql.Stmt, row database.MediaTag,
) (database.MediaTag, error) {
	res, err := stmt.ExecContext(ctx, row.MediaDBID, row.TagDBID)
	if err != nil {
		return row, fmt.Errorf("failed to execute prepared insert media tag statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for media tag: %w", err)
	}

	if lastID == 0 {
		// INSERT OR IGNORE occurred - row already existed, no new ID generated
		log.Debug().Int64("MediaDBID", row.MediaDBID).Int64("TagDBID", row.TagDBID).
			Msg("MediaTag already exists, INSERT OR IGNORE executed")
		// Note: row.DBID remains as originally provided (usually 0)
	} else {
		row.DBID = lastID
	}

	return row, nil
}

func sqlInsertMediaTag(ctx context.Context, db *sql.DB, row database.MediaTag) (database.MediaTag, error) {
	stmt, err := db.PrepareContext(ctx, insertMediaTagSQL)
	if err != nil {
		return row, fmt.Errorf("failed to prepare insert media tag statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	res, err := stmt.ExecContext(ctx, row.MediaDBID, row.TagDBID)
	if err != nil {
		return row, fmt.Errorf("failed to execute insert media tag statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for media tag: %w", err)
	}

	if lastID == 0 {
		// INSERT OR IGNORE occurred - row already existed, no new ID generated
		log.Debug().Int64("MediaDBID", row.MediaDBID).Int64("TagDBID", row.TagDBID).
			Msg("MediaTag already exists, INSERT OR IGNORE executed")
		// Note: row.DBID remains as originally provided (usually 0)
	} else {
		row.DBID = lastID
	}

	return row, nil
}

func sqlDeleteMediaTag(ctx context.Context, db sqlQueryable, mediaDBID, tagDBID int64) error {
	if _, err := db.ExecContext(
		ctx,
		`DELETE FROM MediaTags WHERE MediaDBID = ? AND TagDBID = ?`,
		mediaDBID,
		tagDBID,
	); err != nil {
		return fmt.Errorf("failed to delete media tag: %w", err)
	}

	return nil
}

func sqlGetMediaTagsBySystemID(ctx context.Context, db *sql.DB, systemID string) ([]database.MediaTagLink, error) {
	query := `
		SELECT mt.MediaDBID, mt.TagDBID
		FROM MediaTags mt
		JOIN Media m ON mt.MediaDBID = m.DBID
		JOIN Systems s ON m.SystemDBID = s.DBID
		WHERE s.SystemID = ?
		ORDER BY mt.MediaDBID, mt.TagDBID
	`
	rows, err := db.QueryContext(ctx, query, systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query media tags for system %s: %w", systemID, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	links := make([]database.MediaTagLink, 0)
	for rows.Next() {
		var link database.MediaTagLink
		if err := rows.Scan(&link.MediaDBID, &link.TagDBID); err != nil {
			return nil, fmt.Errorf("failed to scan media tags for system %s: %w", systemID, err)
		}
		links = append(links, link)
	}

	return links, rows.Err()
}
