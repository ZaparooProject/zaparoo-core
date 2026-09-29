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
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// GetMediaCoverThumb returns the recorded cover thumbnail for a media row.
// found is false when none is recorded or the row no longer exists.
func (db *MediaDB) GetMediaCoverThumb(
	ctx context.Context, mediaDBID int64,
) (thumb database.MediaCoverThumb, found bool, err error) {
	sqlDB, err := db.readConn()
	if err != nil {
		return thumb, false, err
	}
	thumb.MediaDBID = mediaDBID
	var titleDBID int64
	var color sql.NullInt64
	err = sqlDB.QueryRowContext(ctx, `
		SELECT s.SystemID, m.Path, m.ParentDir, m.MediaTitleDBID, c.TypeTag, c.Color
		FROM MediaCoverThumbs c
		JOIN Media m ON m.DBID = c.MediaDBID
		JOIN Systems s ON s.DBID = m.SystemDBID
		WHERE c.MediaDBID = ?`, mediaDBID,
	).Scan(&thumb.SystemID, &thumb.Path, &thumb.ParentDir, &titleDBID, &thumb.TypeTag, &color)
	if errors.Is(err, sql.ErrNoRows) {
		return database.MediaCoverThumb{}, false, nil
	}
	if err != nil {
		db.NoteCorruption(err)
		return database.MediaCoverThumb{}, false, fmt.Errorf("get media cover thumb: %w", err)
	}
	if color.Valid {
		value := uint32(color.Int64 & 0xffffff) //nolint:gosec // masked to 24 bits
		thumb.Color = &value
	}

	thumb.AvailableTypeTags, err = mediaCoverPropertyTypes(ctx, sqlDB, mediaDBID, titleDBID)
	if err != nil {
		db.NoteCorruption(err)
		return database.MediaCoverThumb{}, false, err
	}
	return thumb, true, nil
}

// mediaCoverPropertyTypes lists the property TypeTags on a media row and its title.
func mediaCoverPropertyTypes(
	ctx context.Context, sqlDB *sql.DB, mediaDBID, titleDBID int64,
) (typeTags []string, err error) {
	rows, err := sqlDB.QueryContext(ctx, `
		SELECT tt.Type || ':' || t.Tag
		FROM MediaProperties mp
		JOIN Tags t ON t.DBID = mp.TypeTagDBID
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE mp.MediaDBID = ?
		UNION
		SELECT tt.Type || ':' || t.Tag
		FROM MediaTitleProperties mtp
		JOIN Tags t ON t.DBID = mtp.TypeTagDBID
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE mtp.MediaTitleDBID = ?`, mediaDBID, titleDBID)
	if err != nil {
		return nil, fmt.Errorf("get media cover thumb property types: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close media cover thumb property types: %w", closeErr)
		}
	}()
	for rows.Next() {
		var typeTag string
		if scanErr := rows.Scan(&typeTag); scanErr != nil {
			return nil, fmt.Errorf("scan media cover thumb property type: %w", scanErr)
		}
		typeTags = append(typeTags, typeTag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate media cover thumb property types: %w", err)
	}
	return typeTags, nil
}

// PutMediaCoverThumb records the image type and average colour of a media
// row's cover thumbnail. A nil color keeps any colour already recorded for
// the same type.
func (db *MediaDB) PutMediaCoverThumb(ctx context.Context, mediaDBID int64, typeTag string, color *uint32) error {
	var colorValue any
	if color != nil {
		colorValue = int64(*color & 0xffffff)
	}
	return db.libraryCacheWrite(ctx, "put media cover thumb", func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO MediaCoverThumbs (MediaDBID, TypeTag, Color)
			SELECT DBID, ?, ? FROM Media WHERE DBID = ?
			ON CONFLICT (MediaDBID) DO UPDATE SET
				Color = CASE
					WHEN excluded.Color IS NOT NULL THEN excluded.Color
					WHEN MediaCoverThumbs.TypeTag = excluded.TypeTag THEN MediaCoverThumbs.Color
					ELSE NULL
				END,
				TypeTag = excluded.TypeTag`,
			typeTag, colorValue, mediaDBID,
		); err != nil {
			return fmt.Errorf("upsert media cover thumb: %w", err)
		}
		return nil
	})
}

// GetMediaCoverColors returns recorded cover colours keyed by MediaDBID.
func (db *MediaDB) GetMediaCoverColors(ctx context.Context, mediaDBIDs []int64) (map[int64]uint32, error) {
	colors := make(map[int64]uint32)
	if len(mediaDBIDs) == 0 {
		return colors, nil
	}
	sqlDB, err := db.readConn()
	if err != nil {
		return nil, err
	}
	for chunk := range slices.Chunk(mediaDBIDs, batchLookupIDsPerQuery) {
		if err := queryMediaCoverColors(ctx, sqlDB, chunk, colors); err != nil {
			db.NoteCorruption(err)
			return nil, err
		}
	}
	return colors, nil
}

func queryMediaCoverColors(ctx context.Context, sqlDB *sql.DB, ids []int64, colors map[int64]uint32) (err error) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := sqlDB.QueryContext(ctx, `
		SELECT MediaDBID, Color FROM MediaCoverThumbs
		WHERE Color IS NOT NULL AND MediaDBID IN (`+prepareVariadic("?", ",", len(ids))+`)`, args...)
	if err != nil {
		return fmt.Errorf("get media cover colors: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close media cover colors: %w", closeErr)
		}
	}()
	for rows.Next() {
		var id, color int64
		if scanErr := rows.Scan(&id, &color); scanErr != nil {
			return fmt.Errorf("scan media cover color: %w", scanErr)
		}
		colors[id] = uint32(color & 0xffffff) //nolint:gosec // masked to 24 bits
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate media cover colors: %w", err)
	}
	return nil
}

// ClearMediaCoverThumbs drops every recorded cover thumbnail.
func (db *MediaDB) ClearMediaCoverThumbs(ctx context.Context) error {
	return db.libraryCacheWrite(ctx, "clear media cover thumbs", func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM MediaCoverThumbs"); err != nil {
			return fmt.Errorf("delete media cover thumbs: %w", err)
		}
		return nil
	})
}

// ClearMediaCoverThumbsForSystems drops recorded cover thumbnails for media in
// the given systems.
func (db *MediaDB) ClearMediaCoverThumbsForSystems(ctx context.Context, systemIDs []string) error {
	if len(systemIDs) == 0 {
		return nil
	}
	return db.libraryCacheWrite(ctx, "clear media cover thumbs for systems", func(tx *sql.Tx) error {
		for chunk := range slices.Chunk(systemIDs, batchLookupIDsPerQuery) {
			args := make([]any, len(chunk))
			for i, id := range chunk {
				args[i] = id
			}
			//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM MediaCoverThumbs WHERE MediaDBID IN (
					SELECT m.DBID FROM Media m
					JOIN Systems s ON s.DBID = m.SystemDBID
					WHERE s.SystemID IN (`+prepareVariadic("?", ",", len(chunk))+`))`, args...); err != nil {
				return fmt.Errorf("delete media cover thumbs for systems: %w", err)
			}
		}
		return nil
	})
}
