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
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

const libraryFingerprintPrefix = "sha256:"

// libraryFingerprintKey converts an identity fingerprint to the 32 raw bytes
// the ordinal cache is keyed by.
func libraryFingerprintKey(fingerprint string) ([]byte, error) {
	digest, ok := strings.CutPrefix(fingerprint, libraryFingerprintPrefix)
	if !ok {
		return nil, fmt.Errorf("library fingerprint %q has no sha256 prefix", fingerprint)
	}
	key, err := hex.DecodeString(digest)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("library fingerprint %q is not a sha256 digest", fingerprint)
	}
	return key, nil
}

func libraryFingerprintFromKey(key []byte) string {
	return libraryFingerprintPrefix + hex.EncodeToString(key)
}

// LibraryMediaPage returns up to limit present media rows with a DBID above
// afterMediaDBID, in DBID order, for walking the whole library a page at a
// time.
func (db *MediaDB) LibraryMediaPage(
	ctx context.Context, afterMediaDBID int64, limit int,
) ([]database.LibraryMediaRow, error) {
	if limit <= 0 {
		return []database.LibraryMediaRow{}, nil
	}
	sqlDB, err := db.readConn()
	if err != nil {
		return nil, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("library media page: %w", ctxErr)
	}
	// NOT INDEXED keeps this a primary-key range walk: a planner that trusts
	// the missing-flag index would visit every present row and sort them.
	// The cancellable context is dropped for the read itself so the driver
	// steps rows without a goroutine each; callers check cancellation
	// between pages.
	rows, err := sqlDB.QueryContext(context.WithoutCancel(ctx), `
		SELECT Media.DBID, Systems.SystemID, MediaTitles.Name, MediaTitles.Slug
		FROM Media NOT INDEXED
		INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
		WHERE Media.DBID > ? AND Media.IsMissing = 0
		ORDER BY Media.DBID
		LIMIT ?`, afterMediaDBID, limit)
	if err != nil {
		return nil, fmt.Errorf("query library media page: %w", err)
	}
	defer func() { _ = rows.Close() }()

	page := make([]database.LibraryMediaRow, 0, limit)
	for rows.Next() {
		var row database.LibraryMediaRow
		if scanErr := rows.Scan(&row.MediaDBID, &row.SystemID, &row.Name, &row.Slug); scanErr != nil {
			return nil, fmt.Errorf("scan library media row: %w", scanErr)
		}
		page = append(page, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate library media page: %w", err)
	}
	return page, nil
}

// GetLibraryOrdinals returns the cached resolve answers for the fingerprints
// that have one.
func (db *MediaDB) GetLibraryOrdinals(
	ctx context.Context, fingerprints []string,
) (map[string]database.LibraryOrdinal, error) {
	found := make(map[string]database.LibraryOrdinal, len(fingerprints))
	if len(fingerprints) == 0 {
		return found, nil
	}
	sqlDB, err := db.readConn()
	if err != nil {
		return nil, err
	}
	keys := make([]any, 0, len(fingerprints))
	for _, fingerprint := range fingerprints {
		key, keyErr := libraryFingerprintKey(fingerprint)
		if keyErr != nil {
			return nil, keyErr
		}
		keys = append(keys, key)
	}
	for chunk := range slices.Chunk(keys, batchLookupIDsPerQuery) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("library ordinal lookup: %w", ctxErr)
		}
		if chunkErr := queryLibraryOrdinals(ctx, sqlDB, chunk, found); chunkErr != nil {
			return nil, chunkErr
		}
	}
	return found, nil
}

func queryLibraryOrdinals(
	ctx context.Context, sqlDB *sql.DB, keys []any, found map[string]database.LibraryOrdinal,
) error {
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := sqlDB.QueryContext(context.WithoutCancel(ctx), `
		SELECT Fingerprint, Ordinal, Code, ResolvedAt, SeenGeneration
		FROM LibraryOrdinalCache
		WHERE Fingerprint IN (`+prepareVariadic("?", ",", len(keys))+`)`, keys...)
	if err != nil {
		return fmt.Errorf("query library ordinals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			key        []byte
			ordinal    int64
			resolvedAt int64
			entry      database.LibraryOrdinal
		)
		if scanErr := rows.Scan(&key, &ordinal, &entry.Code, &resolvedAt, &entry.SeenGeneration); scanErr != nil {
			return fmt.Errorf("scan library ordinal: %w", scanErr)
		}
		if ordinal < 0 || ordinal > int64(^uint32(0)) {
			continue
		}
		entry.Fingerprint = libraryFingerprintFromKey(key)
		entry.Ordinal = uint32(ordinal)
		entry.ResolvedAt = time.Unix(resolvedAt, 0).UTC()
		found[entry.Fingerprint] = entry
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate library ordinals: %w", err)
	}
	return nil
}

// PutLibraryOrdinals stores resolve answers, replacing earlier answers for
// the same fingerprints.
func (db *MediaDB) PutLibraryOrdinals(ctx context.Context, ordinals []database.LibraryOrdinal) error {
	if len(ordinals) == 0 {
		return nil
	}
	return db.libraryCacheWrite(ctx, "store library ordinals", func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO LibraryOrdinalCache (Fingerprint, Ordinal, Code, ResolvedAt, SeenGeneration)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(Fingerprint) DO UPDATE SET
				Ordinal = excluded.Ordinal,
				Code = excluded.Code,
				ResolvedAt = excluded.ResolvedAt,
				SeenGeneration = max(SeenGeneration, excluded.SeenGeneration)`)
		if err != nil {
			return fmt.Errorf("prepare library ordinal insert: %w", err)
		}
		defer func() { _ = stmt.Close() }()
		for i := range ordinals {
			key, keyErr := libraryFingerprintKey(ordinals[i].Fingerprint)
			if keyErr != nil {
				return keyErr
			}
			if _, execErr := stmt.ExecContext(ctx,
				key, int64(ordinals[i].Ordinal), ordinals[i].Code,
				ordinals[i].ResolvedAt.Unix(), ordinals[i].SeenGeneration,
			); execErr != nil {
				return fmt.Errorf("insert library ordinal: %w", execErr)
			}
		}
		return nil
	})
}

// MarkLibraryOrdinalsSeen records that an inventory walk of generation met
// these cached fingerprints.
func (db *MediaDB) MarkLibraryOrdinalsSeen(ctx context.Context, fingerprints []string, generation int64) error {
	if len(fingerprints) == 0 {
		return nil
	}
	keys := make([]any, 0, len(fingerprints))
	for _, fingerprint := range fingerprints {
		key, err := libraryFingerprintKey(fingerprint)
		if err != nil {
			return err
		}
		keys = append(keys, key)
	}
	return db.libraryCacheWrite(ctx, "mark library ordinals seen", func(tx *sql.Tx) error {
		for chunk := range slices.Chunk(keys, batchLookupIDsPerQuery) {
			args := make([]any, 0, len(chunk)+1)
			args = append(args, generation)
			args = append(args, chunk...)
			//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
			if _, err := tx.ExecContext(ctx, `
				UPDATE LibraryOrdinalCache SET SeenGeneration = ?
				WHERE Fingerprint IN (`+prepareVariadic("?", ",", len(chunk))+`)`, args...); err != nil {
				return fmt.Errorf("update library ordinal generation: %w", err)
			}
		}
		return nil
	})
}

// PruneLibraryOrdinals drops cached answers no walk has met since before
// generation, and reports how many it dropped.
func (db *MediaDB) PruneLibraryOrdinals(ctx context.Context, generation int64) (int64, error) {
	var removed int64
	err := db.libraryCacheWrite(ctx, "prune library ordinals", func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "DELETE FROM LibraryOrdinalCache WHERE SeenGeneration < ?", generation)
		if err != nil {
			return fmt.Errorf("delete library ordinals: %w", err)
		}
		removed, err = result.RowsAffected()
		if err != nil {
			return fmt.Errorf("count pruned library ordinals: %w", err)
		}
		return nil
	})
	return removed, err
}

// ClearLibraryOrdinalCache drops every cached resolve answer.
func (db *MediaDB) ClearLibraryOrdinalCache(ctx context.Context) error {
	return db.libraryCacheWrite(ctx, "clear library ordinals", func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM LibraryOrdinalCache"); err != nil {
			return fmt.Errorf("delete library ordinals: %w", err)
		}
		return nil
	})
}

// GetLibraryInventoryState returns the last committed inventory record, or
// the zero value when none was recorded.
func (db *MediaDB) GetLibraryInventoryState(ctx context.Context) (database.LibraryInventoryState, error) {
	var state database.LibraryInventoryState
	sqlDB, err := db.readConn()
	if err != nil {
		return state, err
	}
	var raw string
	err = sqlDB.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?", DBConfigLibraryInventoryState,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("get library inventory state: %w", err)
	}
	if unmarshalErr := json.Unmarshal([]byte(raw), &state); unmarshalErr != nil {
		// A record this Core cannot read only costs one rebuild.
		log.Debug().Err(unmarshalErr).Msg("ignoring unreadable library inventory state")
		state = database.LibraryInventoryState{}
	}
	return state, nil
}

// SetLibraryInventoryState replaces the committed inventory record.
func (db *MediaDB) SetLibraryInventoryState(ctx context.Context, state *database.LibraryInventoryState) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode library inventory state: %w", err)
	}
	return db.libraryCacheWrite(ctx, "set library inventory state", func(tx *sql.Tx) error {
		if _, execErr := tx.ExecContext(ctx,
			"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
			DBConfigLibraryInventoryState, string(encoded),
		); execErr != nil {
			return fmt.Errorf("store library inventory state: %w", execErr)
		}
		return nil
	})
}

// libraryCacheWrite runs one short write transaction outside any indexing
// batch. It refuses to run while a batch transaction is open, so a Library
// sync pass never waits on the scanner for the write lock.
func (db *MediaDB) libraryCacheWrite(ctx context.Context, what string, write func(*sql.Tx) error) error {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return ErrNullSQL
	}
	if db.inTransaction {
		return ErrTransactionActive
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin transaction: %w", what, err)
	}
	if err := write(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: commit: %w", what, err)
	}
	return nil
}
