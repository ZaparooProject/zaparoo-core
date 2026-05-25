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
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

// mediaBlobHash computes a SHA-256 hash from length-prefixed content type and
// data. Framing the fields prevents ambiguous boundaries between them.
func mediaBlobHash(contentType string, data []byte) string {
	h := sha256.New()
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(contentType)))
	_, _ = h.Write(size[:])
	_, _ = h.Write([]byte(contentType))
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// UpsertMediaBlob computes a SHA-256 hash from framed content type and data,
// inserts a new MediaBlobs row when no matching hash exists (INSERT OR IGNORE),
// then returns the DBID of the canonical row. Identical content always resolves
// to the same DBID.
func (db *MediaDB) UpsertMediaBlob(ctx context.Context, contentType string, data []byte) (int64, error) {
	if db.sql == nil {
		return 0, ErrNullSQL
	}
	// TODO: If blob hashing shows up in scraper benchmarks, evaluate faster
	// hashes with collision handling instead of relying on UNIQUE(Hash) alone.
	hash := mediaBlobHash(contentType, data)

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

// GetMediaBlobDataCapped returns blob data only when it is not larger than maxBytes.
func (db *MediaDB) GetMediaBlobDataCapped(
	ctx context.Context, blobDBID int64, maxBytes int64,
) (data []byte, contentType string, err error) {
	if db.sql == nil {
		return nil, "", ErrNullSQL
	}
	err = db.sql.QueryRowContext(ctx, `
		SELECT Data, ContentType
		FROM MediaBlobs
		WHERE DBID = ? AND length(Data) <= ?
	`, blobDBID, maxBytes).Scan(&data, &contentType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("GetMediaBlobDataCapped: %w", err)
	}
	return data, contentType, nil
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
