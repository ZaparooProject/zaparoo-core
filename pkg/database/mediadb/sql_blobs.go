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
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

// UpsertMediaBlob computes the SHA-256 hash of data, inserts a new MediaBlobs
// row when no matching hash exists (INSERT OR IGNORE), then returns the DBID of
// the canonical row. Identical data always resolves to the same DBID.
func (db *MediaDB) UpsertMediaBlob(ctx context.Context, contentType string, data []byte) (int64, error) {
	if db.sql == nil {
		return 0, ErrNullSQL
	}
	h := sha256.New()
	h.Write([]byte(contentType))
	h.Write(data)
	hash := hex.EncodeToString(h.Sum(nil))

	_, err := db.sql.ExecContext(ctx, `
		INSERT OR IGNORE INTO MediaBlobs (Hash, ContentType, Data)
		VALUES (?, ?, ?)
	`, hash, contentType, data)
	if err != nil {
		return 0, fmt.Errorf("UpsertMediaBlob insert: %w", err)
	}

	var dbid int64
	err = db.sql.QueryRowContext(ctx,
		`SELECT DBID FROM MediaBlobs WHERE Hash = ?`, hash,
	).Scan(&dbid)
	if err != nil {
		return 0, fmt.Errorf("UpsertMediaBlob lookup: %w", err)
	}
	return dbid, nil
}

// GetMediaBlob returns the MediaBlob row for the given DBID,
// or nil, nil when not found.
func (db *MediaDB) GetMediaBlob(ctx context.Context, blobDBID int64) (*database.MediaBlob, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}
	var b database.MediaBlob
	err := db.sql.QueryRowContext(ctx,
		`SELECT DBID, Hash, ContentType, Data FROM MediaBlobs WHERE DBID = ?`,
		blobDBID,
	).Scan(&b.DBID, &b.Hash, &b.ContentType, &b.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // not-found represented as nil, nil per package convention
	}
	if err != nil {
		return nil, fmt.Errorf("GetMediaBlob: %w", err)
	}
	return &b, nil
}

// PruneOrphanedBlobs deletes MediaBlobs rows not referenced by either
// MediaTitleProperties or MediaProperties. Returns the count of rows deleted.
func (db *MediaDB) PruneOrphanedBlobs(ctx context.Context) (int64, error) {
	if db.sql == nil {
		return 0, ErrNullSQL
	}
	res, err := db.sql.ExecContext(ctx, `
		DELETE FROM MediaBlobs
		WHERE DBID NOT IN (
			SELECT BlobDBID FROM MediaTitleProperties WHERE BlobDBID IS NOT NULL
			UNION ALL
			SELECT BlobDBID FROM MediaProperties WHERE BlobDBID IS NOT NULL
		)
	`)
	if err != nil {
		return 0, fmt.Errorf("PruneOrphanedBlobs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("PruneOrphanedBlobs rows affected: %w", err)
	}
	log.Debug().Int64("deleted", n).Msg("mediadb: pruned orphaned blobs")
	return n, nil
}
