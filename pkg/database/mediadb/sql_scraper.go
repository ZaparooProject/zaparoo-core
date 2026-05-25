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
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// FindMediaBySystemAndPath returns the Media row for the given system and path,
// or nil, nil when not found.
func (db *MediaDB) FindMediaBySystemAndPath(
	ctx context.Context, systemDBID int64, path string,
) (*database.Media, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.PrepareContext(ctx, `
		SELECT DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing
		FROM Media
		WHERE SystemDBID = ? AND Path = ?
		LIMIT 1
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare FindMediaBySystemAndPath: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	var row database.Media
	err = stmt.QueryRowContext(ctx, systemDBID, path).Scan(
		&row.DBID,
		&row.MediaTitleDBID,
		&row.SystemDBID,
		&row.Path,
		&row.ParentDir,
		&row.IsMissing,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // sql.ErrNoRows means not found; nil result is the "not found" sentinel
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan FindMediaBySystemAndPath: %w", err)
	}
	return &row, nil
}

func (db *MediaDB) FindMediaBySystemAndPaths(
	ctx context.Context, systemDBID int64, paths []string,
) (map[string]database.Media, error) {
	results := make(map[string]database.Media, len(paths))
	if len(paths) == 0 {
		return results, nil
	}
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	args := make([]any, 0, len(paths)+1)
	args = append(args, systemDBID)
	for _, path := range paths {
		args = append(args, path)
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.sql.QueryContext(ctx, `
		SELECT DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing
		FROM Media
		WHERE SystemDBID = ? AND Path IN (`+prepareVariadic("?", ",", len(paths))+`)
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query FindMediaBySystemAndPaths: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	for rows.Next() {
		var row database.Media
		if err := rows.Scan(
			&row.DBID,
			&row.MediaTitleDBID,
			&row.SystemDBID,
			&row.Path,
			&row.ParentDir,
			&row.IsMissing,
		); err != nil {
			return nil, fmt.Errorf("failed to scan FindMediaBySystemAndPaths: %w", err)
		}
		results[row.Path] = row
	}
	return results, rows.Err()
}

// FindMediaBySystemAndPathFold returns the Media row for the given system and
// path using a case-insensitive path comparison, or nil, nil when not found.
// LOWER() in SQLite covers ASCII only, which is sufficient for filesystem paths.
func (db *MediaDB) FindMediaBySystemAndPathFold(
	ctx context.Context, systemDBID int64, path string,
) (*database.Media, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.PrepareContext(ctx, `
		SELECT DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing
		FROM Media
		WHERE SystemDBID = ? AND LOWER(Path) = LOWER(?)
		LIMIT 1
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare FindMediaBySystemAndPathFold: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	var row database.Media
	err = stmt.QueryRowContext(ctx, systemDBID, path).Scan(
		&row.DBID,
		&row.MediaTitleDBID,
		&row.SystemDBID,
		&row.Path,
		&row.ParentDir,
		&row.IsMissing,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // sql.ErrNoRows means not found; nil result is the "not found" sentinel
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan FindMediaBySystemAndPathFold: %w", err)
	}
	return &row, nil
}

// FindMediaBySystemAndPathSuffix returns all Media rows for the given system
// whose Path ends with "/" + filename. LIKE wildcards in the filename are
// escaped so a literal '%' or '_' in the name does not expand.
func (db *MediaDB) FindMediaBySystemAndPathSuffix(
	ctx context.Context, systemDBID int64, filename string,
) ([]database.Media, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(filename)
	pattern := "%/" + escaped
	rows, err := db.sql.QueryContext(ctx, `
		SELECT DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing
		FROM Media
		WHERE SystemDBID = ? AND Path LIKE ? ESCAPE '\'
	`, systemDBID, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to query FindMediaBySystemAndPathSuffix: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	var result []database.Media
	for rows.Next() {
		var row database.Media
		if err := rows.Scan(
			&row.DBID,
			&row.MediaTitleDBID,
			&row.SystemDBID,
			&row.Path,
			&row.ParentDir,
			&row.IsMissing,
		); err != nil {
			return nil, fmt.Errorf("failed to scan FindMediaBySystemAndPathSuffix: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// MediaHasTag returns true when the given Media record has a tag matching the
// "type:value" string tagValue. The string is split on the first colon: everything
// before the colon is matched against TagTypes.Type, everything after is matched
// against Tags.Tag (padded and unpadded forms are both checked).
func (db *MediaDB) MediaHasTag(ctx context.Context, mediaDBID int64, tagValue string) (bool, error) {
	if db.sql == nil {
		return false, ErrNullSQL
	}

	idx := strings.Index(tagValue, ":")
	if idx < 0 {
		return false, fmt.Errorf("MediaHasTag: tagValue %q is malformed — expected \"type:value\" format", tagValue)
	}

	tagType := tagValue[:idx]
	tagPart := tagValue[idx+1:]
	padded := tags.PadTagValue(tagPart)

	stmt, err := db.sql.PrepareContext(ctx, `
		SELECT 1
		FROM MediaTags mt
		JOIN Tags t ON mt.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE mt.MediaDBID = ?
		  AND tt.Type = ?
		  AND (t.Tag = ? OR t.Tag = ?)
		LIMIT 1
	`)
	if err != nil {
		return false, fmt.Errorf("failed to prepare MediaHasTag: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	var found int
	err = stmt.QueryRowContext(ctx, mediaDBID, tagType, tagPart, padded).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to scan MediaHasTag: %w", err)
	}
	return found == 1, nil
}

// GetScrapedMediaCount returns the number of distinct media rows marked as
// successfully scraped by the given scraper.
func (db *MediaDB) GetScrapedMediaCount(ctx context.Context, scraperID string) (int, error) {
	if db.sql == nil {
		return 0, ErrNullSQL
	}

	var count int
	err := db.sql.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT mt.MediaDBID)
		FROM MediaTags mt
		JOIN Tags t ON mt.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE tt.Type = ? AND t.Tag = ?
	`, string(tags.ScraperType(scraperID)), string(tags.TagScraperScraped)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count scraped media for scraper %q: %w", scraperID, err)
	}
	return count, nil
}

// GetTotalScrapedMediaCount returns the number of distinct media rows marked as
// successfully scraped by any scraper.
func (db *MediaDB) GetTotalScrapedMediaCount(ctx context.Context) (int, error) {
	if db.sql == nil {
		return 0, ErrNullSQL
	}

	var count int
	err := db.sql.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT mt.MediaDBID)
		FROM MediaTags mt
		JOIN Tags t ON mt.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE tt.Type LIKE 'scraper.%' AND t.Tag = ?
	`, string(tags.TagScraperScraped)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count scraped media: %w", err)
	}
	return count, nil
}

// GetScrapedMediaIDs returns media DBIDs in systemDBID already marked as scraped
// by scraperID.
func (db *MediaDB) GetScrapedMediaIDs(
	ctx context.Context, scraperID string, systemDBID int64,
) (map[int64]struct{}, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	rows, err := db.sql.QueryContext(ctx, `
		SELECT DISTINCT mt.MediaDBID
		FROM MediaTags mt
		JOIN Media m ON mt.MediaDBID = m.DBID
		JOIN Tags t ON mt.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE m.SystemDBID = ? AND tt.Type = ? AND t.Tag = ?
	`, systemDBID, string(tags.ScraperType(scraperID)), string(tags.TagScraperScraped))
	if err != nil {
		return nil, fmt.Errorf("failed to query scraped media IDs for scraper %q: %w", scraperID, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	mediaIDs := make(map[int64]struct{})
	for rows.Next() {
		var mediaDBID int64
		if err := rows.Scan(&mediaDBID); err != nil {
			return nil, fmt.Errorf("failed to scan scraped media ID: %w", err)
		}
		mediaIDs[mediaDBID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate scraped media IDs: %w", err)
	}
	return mediaIDs, nil
}

// UpsertMediaTags writes tags to MediaTags for a specific Media row, respecting
// TagTypes.IsExclusive: exclusive types delete existing tags of that type first;
// additive types use INSERT OR IGNORE.
func (db *MediaDB) UpsertMediaTags(ctx context.Context, mediaDBID int64, tagInfos []database.TagInfo) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return upsertTags(ctx, db.sql, tagInfos, func(tx *sql.Tx, typeDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`DELETE FROM MediaTags WHERE MediaDBID = ? AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)`,
			mediaDBID, typeDBID,
		)
		if err != nil {
			return fmt.Errorf("failed to delete media tags for type: %w", err)
		}
		return nil
	}, func(tx *sql.Tx, tagDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO MediaTags (MediaDBID, TagDBID) VALUES (?, ?)`,
			mediaDBID, tagDBID,
		)
		if err != nil {
			return fmt.Errorf("failed to insert media tag link: %w", err)
		}
		return nil
	})
}

// UpsertMediaTitleTags writes tags to MediaTitleTags for a specific MediaTitle row.
func (db *MediaDB) UpsertMediaTitleTags(ctx context.Context, mediaTitleDBID int64, tagInfos []database.TagInfo) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return upsertTags(ctx, db.sql, tagInfos, func(tx *sql.Tx, typeDBID int64) error {
		const q = `DELETE FROM MediaTitleTags` +
			` WHERE MediaTitleDBID = ? AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)`
		_, err := tx.ExecContext(ctx, q, mediaTitleDBID, typeDBID)
		if err != nil {
			return fmt.Errorf("failed to delete media title tags for type: %w", err)
		}
		return nil
	}, func(tx *sql.Tx, tagDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES (?, ?)`,
			mediaTitleDBID, tagDBID,
		)
		if err != nil {
			return fmt.Errorf("failed to insert media title tag link: %w", err)
		}
		return nil
	})
}

// upsertTags is the shared implementation for UpsertMediaTags and UpsertMediaTitleTags.
// deleteFn deletes existing tags of a type for the entity (called once per exclusive type).
// insertFn inserts the tag link for the entity.
// All operations run inside a single transaction for atomicity.
//
// Tags are grouped by type before processing. For each exclusive type the existing
// tags are deleted once (before any inserts for that type), preventing multiple tags
// of the same exclusive type from clobbering each other during the loop.
func upsertTags(
	ctx context.Context,
	db *sql.DB,
	tagInfos []database.TagInfo,
	deleteFn func(tx *sql.Tx, typeDBID int64) error,
	insertFn func(tx *sql.Tx, tagDBID int64) error,
) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("upsertTags: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := upsertTagsInTx(ctx, tx, tagInfos, deleteFn, insertFn); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("upsertTags: commit: %w", err)
	}
	committed = true
	return nil
}

func upsertTagsInTx(
	ctx context.Context,
	tx *sql.Tx,
	tagInfos []database.TagInfo,
	deleteFn func(tx *sql.Tx, typeDBID int64) error,
	insertFn func(tx *sql.Tx, tagDBID int64) error,
) error {
	// Group tags by type so that deleteFn is called at most once per exclusive type.
	type typeEntry struct {
		tags        []database.TagInfo
		dbid        int64
		isExclusive bool
	}
	typeOrder := make([]string, 0, len(tagInfos)) // preserve insertion order
	byType := make(map[string]*typeEntry, len(tagInfos))

	for _, ti := range tagInfos {
		e, exists := byType[ti.Type]
		if !exists {
			// Resolve tag type to get IsExclusive and DBID.
			// If the type is not yet registered (e.g. a runtime scraper sentinel type),
			// auto-create it as additive (IsExclusive=false).
			var typeDBID int64
			var isExclusive bool
			err := tx.QueryRowContext(ctx,
				`SELECT DBID, IsExclusive FROM TagTypes WHERE Type = ? LIMIT 1`,
				ti.Type,
			).Scan(&typeDBID, &isExclusive)
			if errors.Is(err, sql.ErrNoRows) {
				// Auto-create the tag type with IsExclusive=false.
				_, insertErr := tx.ExecContext(ctx,
					`INSERT OR IGNORE INTO TagTypes (Type, IsExclusive) VALUES (?, 0)`,
					ti.Type,
				)
				if insertErr != nil {
					return fmt.Errorf("failed to auto-create tag type %q: %w", ti.Type, insertErr)
				}
				err = tx.QueryRowContext(ctx,
					`SELECT DBID, IsExclusive FROM TagTypes WHERE Type = ? LIMIT 1`,
					ti.Type,
				).Scan(&typeDBID, &isExclusive)
			}
			if err != nil {
				return fmt.Errorf("failed to look up tag type %q: %w", ti.Type, err)
			}
			e = &typeEntry{dbid: typeDBID, isExclusive: isExclusive}
			byType[ti.Type] = e
			typeOrder = append(typeOrder, ti.Type)
		}
		e.tags = append(e.tags, ti)
	}

	// Process each type: delete once for exclusive types, then insert all tags.
	for _, typeName := range typeOrder {
		e := byType[typeName]

		if e.isExclusive {
			seen := make(map[string]struct{}, len(e.tags))
			for _, ti := range e.tags {
				seen[tags.PadTagValue(ti.Tag)] = struct{}{}
			}
			if len(seen) > 1 {
				return fmt.Errorf("exclusive tag type %q received multiple values", typeName)
			}
		}

		// For exclusive types: delete all existing tags of this type for the entity once,
		// before inserting any new tags. This prevents subsequent tags of the same type
		// from clobbering each other within this call.
		if e.isExclusive {
			if err := deleteFn(tx, e.dbid); err != nil {
				return fmt.Errorf("failed to delete exclusive tags for type %q: %w", typeName, err)
			}
		}

		for _, ti := range e.tags {
			// Resolve tag DBID; insert if missing using INSERT OR IGNORE to handle
			// concurrent writers outside this transaction (e.g. two goroutines
			// bootstrapping the same tag type simultaneously).
			tagValue := tags.PadTagValue(ti.Tag)
			var tagDBID int64
			err := tx.QueryRowContext(ctx,
				`SELECT DBID FROM Tags WHERE TypeDBID = ? AND Tag = ? LIMIT 1`,
				e.dbid, tagValue,
			).Scan(&tagDBID)
			if errors.Is(err, sql.ErrNoRows) {
				if _, insertErr := tx.ExecContext(ctx,
					`INSERT OR IGNORE INTO Tags (TypeDBID, Tag) VALUES (?, ?)`,
					e.dbid, tagValue,
				); insertErr != nil {
					return fmt.Errorf("failed to insert tag %q:%q: %w", typeName, ti.Tag, insertErr)
				}
				// Re-query after insert (handles both "we inserted" and "someone else did").
				if err = tx.QueryRowContext(ctx,
					`SELECT DBID FROM Tags WHERE TypeDBID = ? AND Tag = ? LIMIT 1`,
					e.dbid, tagValue,
				).Scan(&tagDBID); err != nil {
					return fmt.Errorf("failed to re-query tag DBID for %q:%q: %w", typeName, ti.Tag, err)
				}
			} else if err != nil {
				return fmt.Errorf("failed to look up tag DBID for %q:%q: %w", typeName, ti.Tag, err)
			}

			// Insert the tag link.
			if err := insertFn(tx, tagDBID); err != nil {
				return fmt.Errorf("failed to insert tag link for %q:%q: %w", typeName, ti.Tag, err)
			}
		}
	}

	return nil
}

// UpsertMediaTitleProperties upserts properties into MediaTitleProperties.
// Conflicts on (MediaTitleDBID, TypeTagDBID) update data columns; DBID is preserved.
// p.TypeTag must be set to the full "type:value" string; TypeTagDBID is resolved
// from the Tags table automatically.
func (db *MediaDB) UpsertMediaTitleProperties(
	ctx context.Context, mediaTitleDBID int64, props []database.MediaProperty,
) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("UpsertMediaTitleProperties: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := upsertMediaTitleProperties(ctx, tx, mediaTitleDBID, props); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("UpsertMediaTitleProperties: commit: %w", err)
	}
	committed = true
	return nil
}

func upsertMediaTitleProperties(
	ctx context.Context, q sqlQueryable, mediaTitleDBID int64, props []database.MediaProperty,
) error {
	for _, p := range props {
		typeTagDBID, err := resolvePropertyTypeTag(ctx, q, p.TypeTag)
		if err != nil {
			return fmt.Errorf("failed to resolve property type tag %q: %w", p.TypeTag, err)
		}
		_, err = q.ExecContext(ctx, `
			INSERT INTO MediaTitleProperties (MediaTitleDBID, TypeTagDBID, Text, BlobDBID)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(MediaTitleDBID, TypeTagDBID) DO UPDATE SET
				Text    = excluded.Text,
				BlobDBID = excluded.BlobDBID
		`, mediaTitleDBID, typeTagDBID, p.Text, p.BlobDBID)
		if err != nil {
			return fmt.Errorf("failed to upsert MediaTitleProperty (typeTag=%q): %w", p.TypeTag, err)
		}
	}
	return nil
}

// UpsertMediaProperties upserts properties into MediaProperties.
// Conflicts on (MediaDBID, TypeTagDBID) update data columns; DBID is preserved.
// p.TypeTag must be set to the full "type:value" string; TypeTagDBID is resolved
// from the Tags table automatically.
func (db *MediaDB) UpsertMediaProperties(ctx context.Context, mediaDBID int64, props []database.MediaProperty) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("UpsertMediaProperties: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := upsertMediaProperties(ctx, tx, mediaDBID, props); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("UpsertMediaProperties: commit: %w", err)
	}
	committed = true
	return nil
}

// ApplyScrapeResult writes all scraper metadata for a match in one transaction.
// The sentinel tag is inserted last so interrupted writes remain retryable.
func (db *MediaDB) ApplyScrapeResult(
	ctx context.Context, mediaDBID, mediaTitleDBID int64, write *database.ScrapeWrite,
) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	if write == nil {
		return errors.New("ApplyScrapeResult: write is nil")
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ApplyScrapeResult: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if len(write.MediaTags) > 0 {
		if err := upsertMediaTagsInTx(ctx, tx, mediaDBID, write.MediaTags); err != nil {
			return fmt.Errorf("upsert media tags: %w", err)
		}
	}
	if len(write.TitleTags) > 0 {
		if err := upsertMediaTitleTagsInTx(ctx, tx, mediaTitleDBID, write.TitleTags); err != nil {
			return fmt.Errorf("upsert title tags: %w", err)
		}
	}
	if len(write.TitleProps) > 0 {
		if err := upsertMediaTitleProperties(ctx, tx, mediaTitleDBID, write.TitleProps); err != nil {
			return fmt.Errorf("upsert title properties: %w", err)
		}
	}
	if len(write.MediaProps) > 0 {
		if err := upsertMediaProperties(ctx, tx, mediaDBID, write.MediaProps); err != nil {
			return fmt.Errorf("upsert media properties: %w", err)
		}
	}
	if err := upsertMediaTagsInTx(ctx, tx, mediaDBID, []database.TagInfo{write.Sentinel}); err != nil {
		return fmt.Errorf("upsert sentinel tag: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ApplyScrapeResult: commit: %w", err)
	}
	committed = true
	return nil
}

func upsertMediaTagsInTx(ctx context.Context, tx *sql.Tx, mediaDBID int64, tagInfos []database.TagInfo) error {
	return upsertTagsInTx(ctx, tx, tagInfos, func(tx *sql.Tx, typeDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`DELETE FROM MediaTags WHERE MediaDBID = ? AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)`,
			mediaDBID, typeDBID,
		)
		if err != nil {
			return fmt.Errorf("failed to delete media tags for type: %w", err)
		}
		return nil
	}, func(tx *sql.Tx, tagDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO MediaTags (MediaDBID, TagDBID) VALUES (?, ?)`,
			mediaDBID, tagDBID,
		)
		if err != nil {
			return fmt.Errorf("failed to insert media tag link: %w", err)
		}
		return nil
	})
}

func upsertMediaTitleTagsInTx(
	ctx context.Context, tx *sql.Tx, mediaTitleDBID int64, tagInfos []database.TagInfo,
) error {
	return upsertTagsInTx(ctx, tx, tagInfos, func(tx *sql.Tx, typeDBID int64) error {
		const q = `DELETE FROM MediaTitleTags` +
			` WHERE MediaTitleDBID = ? AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)`
		_, err := tx.ExecContext(ctx, q, mediaTitleDBID, typeDBID)
		if err != nil {
			return fmt.Errorf("failed to delete media title tags for type: %w", err)
		}
		return nil
	}, func(tx *sql.Tx, tagDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES (?, ?)`,
			mediaTitleDBID, tagDBID,
		)
		if err != nil {
			return fmt.Errorf("failed to insert media title tag link: %w", err)
		}
		return nil
	})
}

func upsertMediaProperties(ctx context.Context, q sqlQueryable, mediaDBID int64, props []database.MediaProperty) error {
	for _, p := range props {
		typeTagDBID, err := resolvePropertyTypeTag(ctx, q, p.TypeTag)
		if err != nil {
			return fmt.Errorf("failed to resolve property type tag %q: %w", p.TypeTag, err)
		}
		_, err = q.ExecContext(ctx, `
			INSERT INTO MediaProperties (MediaDBID, TypeTagDBID, Text, BlobDBID)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(MediaDBID, TypeTagDBID) DO UPDATE SET
				Text    = excluded.Text,
				BlobDBID = excluded.BlobDBID
		`, mediaDBID, typeTagDBID, p.Text, p.BlobDBID)
		if err != nil {
			return fmt.Errorf("failed to upsert MediaProperty (typeTag=%q): %w", p.TypeTag, err)
		}
	}
	return nil
}

// DeleteMediaTitleProperty removes the property row for (mediaTitleDBID, typeTagDBID)
// from MediaTitleProperties. It is a no-op when no matching row exists.
func (db *MediaDB) DeleteMediaTitleProperty(ctx context.Context, mediaTitleDBID, typeTagDBID int64) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	_, err := db.sql.ExecContext(ctx,
		`DELETE FROM MediaTitleProperties WHERE MediaTitleDBID = ? AND TypeTagDBID = ?`,
		mediaTitleDBID, typeTagDBID,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to delete MediaTitleProperty (mediaTitleDBID=%d, typeTagDBID=%d): %w",
			mediaTitleDBID, typeTagDBID, err)
	}
	return nil
}

// DeleteMediaProperty removes the property row for (mediaDBID, typeTagDBID)
// from MediaProperties. It is a no-op when no matching row exists.
func (db *MediaDB) DeleteMediaProperty(ctx context.Context, mediaDBID, typeTagDBID int64) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	_, err := db.sql.ExecContext(ctx,
		`DELETE FROM MediaProperties WHERE MediaDBID = ? AND TypeTagDBID = ?`,
		mediaDBID, typeTagDBID,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to delete MediaProperty (mediaDBID=%d, typeTagDBID=%d): %w",
			mediaDBID, typeTagDBID, err)
	}
	return nil
}

// resolvePropertyTypeTag looks up the DBID of the Tags row for the given full
// tag string (e.g. "property:description"). The tag must already exist in the DB
// (seeded by SeedCanonicalTags). Returns an error if not found.
func resolvePropertyTypeTag(ctx context.Context, db sqlQueryable, typeTag string) (int64, error) {
	// typeTag format: "type:value" — split on first colon.
	idx := strings.Index(typeTag, ":")
	if idx < 0 {
		return 0, fmt.Errorf("property type tag %q is not in type:value format", typeTag)
	}
	tagType := typeTag[:idx]
	tagValue := tags.PadTagValue(typeTag[idx+1:])
	unpadded := typeTag[idx+1:]

	var tagDBID int64
	err := db.QueryRowContext(ctx, `
		SELECT t.DBID
		FROM Tags t
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE tt.Type = ? AND (t.Tag = ? OR t.Tag = ?)
		LIMIT 1
	`, tagType, tagValue, unpadded).Scan(&tagDBID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("property type tag %q not found in Tags table (run SeedCanonicalTags first)", typeTag)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to resolve property type tag %q: %w", typeTag, err)
	}
	return tagDBID, nil
}

// FindMediaTitlesWithoutSentinel returns MediaTitle rows for the given system
// that have no Media row tagged with sentinelTag. sentinelTag must be in
// "type:value" format (e.g. "scraper.gamelist.xml:scraped").
func (db *MediaDB) FindMediaTitlesWithoutSentinel(
	ctx context.Context, systemDBID int64, sentinelTag string,
) ([]database.MediaTitle, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	// Split on first colon to get the stored TagTypes.Type and the raw tag value.
	idx := strings.Index(sentinelTag, ":")
	if idx < 0 {
		return nil, fmt.Errorf("sentinelTag %q is not in type:value format", sentinelTag)
	}
	tagType := sentinelTag[:idx]
	tagPart := sentinelTag[idx+1:]
	padded := tags.PadTagValue(tagPart)

	stmt, err := db.sql.PrepareContext(ctx, `
		SELECT mt.DBID, mt.SystemDBID, mt.Slug, mt.Name
		FROM MediaTitles mt
		WHERE mt.SystemDBID = ?
		  AND NOT EXISTS (
			SELECT 1
			FROM Media m
			JOIN MediaTags mtag ON m.DBID = mtag.MediaDBID
			JOIN Tags t         ON mtag.TagDBID = t.DBID
			JOIN TagTypes tt    ON t.TypeDBID = tt.DBID
			WHERE m.MediaTitleDBID = mt.DBID
			  AND tt.Type = ?
			  AND (t.Tag = ? OR t.Tag = ?)
		  )
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare FindMediaTitlesWithoutSentinel: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, systemDBID, tagType, tagPart, padded)
	if err != nil {
		return nil, fmt.Errorf("failed to query FindMediaTitlesWithoutSentinel: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	var titles []database.MediaTitle
	for rows.Next() {
		var t database.MediaTitle
		if err := rows.Scan(&t.DBID, &t.SystemDBID, &t.Slug, &t.Name); err != nil {
			return nil, fmt.Errorf("failed to scan MediaTitle: %w", err)
		}
		titles = append(titles, t)
	}
	return titles, rows.Err()
}

// FindMediaTitleByDBID returns the MediaTitle with the given DBID, or nil, nil
// when not found.
func (db *MediaDB) FindMediaTitleByDBID(ctx context.Context, dbid int64) (*database.MediaTitle, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.PrepareContext(ctx, `
		SELECT DBID, SystemDBID, Slug, Name
		FROM MediaTitles
		WHERE DBID = ?
		LIMIT 1
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare FindMediaTitleByDBID: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	var t database.MediaTitle
	err = stmt.QueryRowContext(ctx, dbid).Scan(&t.DBID, &t.SystemDBID, &t.Slug, &t.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // sql.ErrNoRows means not found; nil result is the "not found" sentinel
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan FindMediaTitleByDBID: %w", err)
	}
	return &t, nil
}

// FindMediaTitleBySystemAndSlug returns the MediaTitle matching systemDBID and
// slug, or nil, nil when not found.
func (db *MediaDB) FindMediaTitleBySystemAndSlug(
	ctx context.Context, systemDBID int64, slug string,
) (*database.MediaTitle, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.PrepareContext(ctx, `
		SELECT DBID, SystemDBID, Slug, Name
		FROM MediaTitles
		WHERE SystemDBID = ? AND Slug = ?
		LIMIT 1
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare FindMediaTitleBySystemAndSlug: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	var t database.MediaTitle
	err = stmt.QueryRowContext(ctx, systemDBID, slug).Scan(&t.DBID, &t.SystemDBID, &t.Slug, &t.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // sql.ErrNoRows means not found; nil result is the "not found" sentinel
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan FindMediaTitleBySystemAndSlug: %w", err)
	}
	return &t, nil
}

// GetMediaTitleProperties returns all MediaTitleProperties rows for the given
// title. TypeTag is populated as "type:value" from the joined Tags/TagTypes rows.
func (db *MediaDB) GetMediaTitleProperties(
	ctx context.Context, mediaTitleDBID int64,
) ([]database.MediaProperty, error) {
	return db.loadMediaTitleProperties(ctx, mediaTitleDBID)
}

func (db *MediaDB) loadMediaTitleProperties(
	ctx context.Context, mediaTitleDBID int64,
) ([]database.MediaProperty, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.PrepareContext(ctx, mediaTitlePropertyQuery(
		"WHERE mtp.MediaTitleDBID = ?", propertyGroupOmit))
	if err != nil {
		return nil, fmt.Errorf("failed to prepare GetMediaTitleProperties: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, mediaTitleDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaTitleProperties: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanProperties(rows)
}

// GetMediaTitlePropertiesByMediaTitleDBIDs returns properties grouped by
// MediaTitle DBID. Binary blobs are capped at MaxMediaPropertyBinaryBytes.
func (db *MediaDB) GetMediaTitlePropertiesByMediaTitleDBIDs(
	ctx context.Context, mediaTitleDBIDs []int64,
) (map[int64][]database.MediaProperty, error) {
	return db.loadMediaTitlePropertiesByMediaTitleDBIDs(ctx, mediaTitleDBIDs)
}

func (db *MediaDB) loadMediaTitlePropertiesByMediaTitleDBIDs(
	ctx context.Context, mediaTitleDBIDs []int64,
) (map[int64][]database.MediaProperty, error) {
	results := make(map[int64][]database.MediaProperty, len(mediaTitleDBIDs))
	if len(mediaTitleDBIDs) == 0 {
		return results, nil
	}
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	args := int64Args(mediaTitleDBIDs)
	where := `WHERE mtp.MediaTitleDBID IN (` + prepareVariadic("?", ",", len(mediaTitleDBIDs)) + `)`
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.sql.QueryContext(ctx, mediaTitlePropertyQuery(where, propertyGroupInclude), args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaTitlePropertiesByMediaTitleDBIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanGroupedProperties(rows)
}

// GetMediaProperties returns all MediaProperties rows for the given Media record.
// TypeTag is populated as "type:value" from the joined Tags/TagTypes rows.
func (db *MediaDB) GetMediaProperties(ctx context.Context, mediaDBID int64) ([]database.MediaProperty, error) {
	return db.loadMediaProperties(ctx, mediaDBID)
}

func (db *MediaDB) loadMediaProperties(
	ctx context.Context, mediaDBID int64,
) ([]database.MediaProperty, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.PrepareContext(ctx, mediaPropertyQuery("WHERE mp.MediaDBID = ?", propertyGroupOmit))
	if err != nil {
		return nil, fmt.Errorf("failed to prepare GetMediaProperties: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, mediaDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaProperties: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanProperties(rows)
}

// GetMediaPropertiesByMediaDBIDs returns properties grouped by Media DBID.
// Binary blobs are capped at MaxMediaPropertyBinaryBytes.
func (db *MediaDB) GetMediaPropertiesByMediaDBIDs(
	ctx context.Context, mediaDBIDs []int64,
) (map[int64][]database.MediaProperty, error) {
	return db.loadMediaPropertiesByMediaDBIDs(ctx, mediaDBIDs)
}

func (db *MediaDB) loadMediaPropertiesByMediaDBIDs(
	ctx context.Context, mediaDBIDs []int64,
) (map[int64][]database.MediaProperty, error) {
	results := make(map[int64][]database.MediaProperty, len(mediaDBIDs))
	if len(mediaDBIDs) == 0 {
		return results, nil
	}
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	args := int64Args(mediaDBIDs)
	where := `WHERE mp.MediaDBID IN (` + prepareVariadic("?", ",", len(mediaDBIDs)) + `)`
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.sql.QueryContext(ctx, mediaPropertyQuery(where, propertyGroupInclude), args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaPropertiesByMediaDBIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanGroupedProperties(rows)
}

// GetMediaWithTitleAndSystem fetches a Media record together with its parent
// MediaTitle and System via a single JOIN query. Returns nil, nil when the
// mediaDBID does not exist. IsMissing is NOT filtered.
func (db *MediaDB) GetMediaWithTitleAndSystem(ctx context.Context, mediaDBID int64) (*database.MediaFullRow, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	stmt, err := db.sql.PrepareContext(ctx, `
		SELECT
			m.DBID, m.Path, m.ParentDir, m.IsMissing, m.MediaTitleDBID, m.SystemDBID,
			mt.DBID, mt.Slug, mt.SecondarySlug, mt.Name, mt.SlugLength, mt.SlugWordCount, mt.SystemDBID,
			s.DBID, s.SystemID, s.Name
		FROM Media m
		INNER JOIN MediaTitles mt ON m.MediaTitleDBID = mt.DBID
		INNER JOIN Systems s ON mt.SystemDBID = s.DBID
		WHERE m.DBID = ?
		LIMIT 1
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare GetMediaWithTitleAndSystem: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	var row database.MediaFullRow
	err = stmt.QueryRowContext(ctx, mediaDBID).Scan(
		&row.DBID, &row.Path, &row.ParentDir, &row.IsMissing,
		&row.MediaTitleDBID, &row.SystemDBID,
		&row.Title.DBID, &row.Title.Slug, &row.Title.SecondarySlug, &row.Title.Name,
		&row.Title.SlugLength, &row.Title.SlugWordCount, &row.Title.SystemDBID,
		&row.System.DBID, &row.System.SystemID, &row.System.Name,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // sql.ErrNoRows means not found; nil result is the "not found" sentinel
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan GetMediaWithTitleAndSystem: %w", err)
	}
	return &row, nil
}

func (db *MediaDB) GetMediaWithTitleAndSystemByIDs(
	ctx context.Context, mediaDBIDs []int64,
) (map[int64]database.MediaFullRow, error) {
	results := make(map[int64]database.MediaFullRow, len(mediaDBIDs))
	if len(mediaDBIDs) == 0 {
		return results, nil
	}
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	args := int64Args(mediaDBIDs)
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.sql.QueryContext(ctx, `
		SELECT
			m.DBID, m.Path, m.ParentDir, m.IsMissing, m.MediaTitleDBID, m.SystemDBID,
			mt.DBID, mt.Slug, mt.SecondarySlug, mt.Name, mt.SlugLength, mt.SlugWordCount, mt.SystemDBID,
			s.DBID, s.SystemID, s.Name
		FROM Media m
		INNER JOIN MediaTitles mt ON m.MediaTitleDBID = mt.DBID
		INNER JOIN Systems s ON mt.SystemDBID = s.DBID
		WHERE m.DBID IN (`+prepareVariadic("?", ",", len(mediaDBIDs))+`)
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaWithTitleAndSystemByIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	for rows.Next() {
		var row database.MediaFullRow
		if err := rows.Scan(
			&row.DBID, &row.Path, &row.ParentDir, &row.IsMissing,
			&row.MediaTitleDBID, &row.SystemDBID,
			&row.Title.DBID, &row.Title.Slug, &row.Title.SecondarySlug, &row.Title.Name,
			&row.Title.SlugLength, &row.Title.SlugWordCount, &row.Title.SystemDBID,
			&row.System.DBID, &row.System.SystemID, &row.System.Name,
		); err != nil {
			return nil, fmt.Errorf("failed to scan GetMediaWithTitleAndSystemByIDs: %w", err)
		}
		results[row.DBID] = row
	}
	return results, rows.Err()
}

// GetMediaTagsByMediaDBID returns the file-level tags (MediaTags) for a single
// Media row, ordered by type then tag value.
func (db *MediaDB) GetMediaTagsByMediaDBID(ctx context.Context, mediaDBID int64) ([]database.TagInfo, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	stmt, err := db.sql.PrepareContext(ctx, `
		SELECT Tags.Tag, TagTypes.Type
		FROM MediaTags
		JOIN Tags ON MediaTags.TagDBID = Tags.DBID
		JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
		WHERE MediaTags.MediaDBID = ?
		ORDER BY TagTypes.Type, Tags.Tag
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare GetMediaTagsByMediaDBID: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, mediaDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaTagsByMediaDBID: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanTagInfos(rows)
}

func (db *MediaDB) GetMediaTagsByMediaDBIDs(
	ctx context.Context, mediaDBIDs []int64,
) (map[int64][]database.TagInfo, error) {
	results := make(map[int64][]database.TagInfo, len(mediaDBIDs))
	if len(mediaDBIDs) == 0 {
		return results, nil
	}
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	args := int64Args(mediaDBIDs)
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.sql.QueryContext(ctx, `
		SELECT MediaTags.MediaDBID, Tags.Tag, TagTypes.Type
		FROM MediaTags
		JOIN Tags ON MediaTags.TagDBID = Tags.DBID
		JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
		WHERE MediaTags.MediaDBID IN (`+prepareVariadic("?", ",", len(mediaDBIDs))+`)
		ORDER BY MediaTags.MediaDBID, TagTypes.Type, Tags.Tag
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaTagsByMediaDBIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanGroupedTagInfos(rows)
}

// GetMediaTitleTagsByMediaTitleDBID returns the title-level tags (MediaTitleTags)
// for a single MediaTitle row, ordered by type then tag value.
func (db *MediaDB) GetMediaTitleTagsByMediaTitleDBID(
	ctx context.Context, mediaTitleDBID int64,
) ([]database.TagInfo, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	stmt, err := db.sql.PrepareContext(ctx, `
		SELECT Tags.Tag, TagTypes.Type
		FROM MediaTitleTags
		JOIN Tags ON MediaTitleTags.TagDBID = Tags.DBID
		JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
		WHERE MediaTitleTags.MediaTitleDBID = ?
		ORDER BY TagTypes.Type, Tags.Tag
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare GetMediaTitleTagsByMediaTitleDBID: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, mediaTitleDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaTitleTagsByMediaTitleDBID: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanTagInfos(rows)
}

func (db *MediaDB) GetMediaTitleTagsByMediaTitleDBIDs(
	ctx context.Context, mediaTitleDBIDs []int64,
) (map[int64][]database.TagInfo, error) {
	results := make(map[int64][]database.TagInfo, len(mediaTitleDBIDs))
	if len(mediaTitleDBIDs) == 0 {
		return results, nil
	}
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	args := int64Args(mediaTitleDBIDs)
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.sql.QueryContext(ctx, `
		SELECT MediaTitleTags.MediaTitleDBID, Tags.Tag, TagTypes.Type
		FROM MediaTitleTags
		JOIN Tags ON MediaTitleTags.TagDBID = Tags.DBID
		JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
		WHERE MediaTitleTags.MediaTitleDBID IN (`+prepareVariadic("?", ",", len(mediaTitleDBIDs))+`)
		ORDER BY MediaTitleTags.MediaTitleDBID, TagTypes.Type, Tags.Tag
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaTitleTagsByMediaTitleDBIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanGroupedTagInfos(rows)
}

type propertyGroupMode int

const (
	propertyGroupOmit propertyGroupMode = iota
	propertyGroupInclude
)

func propertySelectColumns(entityIDColumn string, groupMode propertyGroupMode) string {
	parts := []string{}
	if groupMode == propertyGroupInclude {
		parts = append(parts, entityIDColumn)
	}
	dataColumn := fmt.Sprintf(
		"CASE WHEN mb.Data IS NOT NULL AND length(mb.Data) <= %d THEN mb.Data ELSE NULL END",
		database.MaxMediaPropertyBinaryBytes,
	)
	parts = append(parts,
		"tt.Type || ':' || t.Tag",
		"TypeTagDBID",
		"Text",
		"BlobDBID",
		"ContentType",
		"CASE WHEN mb.Data IS NOT NULL THEN length(mb.Data) ELSE NULL END",
		dataColumn,
	)
	return strings.Join(parts, ", ")
}

func mediaTitlePropertyQuery(where string, groupMode propertyGroupMode) string {
	return `
		SELECT ` + propertySelectColumns("mtp.MediaTitleDBID", groupMode) + `
		FROM MediaTitleProperties mtp
		JOIN Tags t      ON mtp.TypeTagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		LEFT JOIN MediaBlobs mb ON mtp.BlobDBID = mb.DBID
		` + where
}

func mediaPropertyQuery(where string, groupMode propertyGroupMode) string {
	return `
		SELECT ` + propertySelectColumns("mp.MediaDBID", groupMode) + `
		FROM MediaProperties mp
		JOIN Tags t      ON mp.TypeTagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		LEFT JOIN MediaBlobs mb ON mp.BlobDBID = mb.DBID
		` + where
}

func scanTagInfos(rows *sql.Rows) ([]database.TagInfo, error) {
	result := make([]database.TagInfo, 0)
	for rows.Next() {
		var t database.TagInfo
		if err := rows.Scan(&t.Tag, &t.Type); err != nil {
			return nil, fmt.Errorf("failed to scan TagInfo: %w", err)
		}
		t.Tag = tags.UnpadTagValue(t.Tag)
		result = append(result, t)
	}
	return result, rows.Err()
}

func scanGroupedTagInfos(rows *sql.Rows) (map[int64][]database.TagInfo, error) {
	result := make(map[int64][]database.TagInfo)
	for rows.Next() {
		var dbid int64
		var t database.TagInfo
		if err := rows.Scan(&dbid, &t.Tag, &t.Type); err != nil {
			return nil, fmt.Errorf("failed to scan grouped TagInfo: %w", err)
		}
		t.Tag = tags.UnpadTagValue(t.Tag)
		result[dbid] = append(result[dbid], t)
	}
	return result, rows.Err()
}

func scanProperties(rows *sql.Rows) ([]database.MediaProperty, error) {
	var props []database.MediaProperty
	for rows.Next() {
		var p database.MediaProperty
		var blobDBID sql.NullInt64
		var contentType sql.NullString
		var blobSize sql.NullInt64
		var binary []byte
		if err := rows.Scan(
			&p.TypeTag, &p.TypeTagDBID, &p.Text,
			&blobDBID, &contentType, &blobSize, &binary,
		); err != nil {
			return nil, fmt.Errorf("failed to scan MediaProperty: %w", err)
		}
		setPropertyBlobFields(&p, blobDBID, contentType, blobSize, binary)
		props = append(props, p)
	}
	if props == nil {
		props = []database.MediaProperty{}
	}
	return props, rows.Err()
}

func scanGroupedProperties(rows *sql.Rows) (map[int64][]database.MediaProperty, error) {
	result := make(map[int64][]database.MediaProperty)
	for rows.Next() {
		var dbid int64
		prop, err := scanPropertyWithDBID(rows, &dbid)
		if err != nil {
			return nil, err
		}
		result[dbid] = append(result[dbid], prop)
	}
	return result, rows.Err()
}

func scanPropertyWithDBID(rows *sql.Rows, dbid *int64) (database.MediaProperty, error) {
	var p database.MediaProperty
	var blobDBID sql.NullInt64
	var contentType sql.NullString
	var blobSize sql.NullInt64
	var binary []byte
	if err := rows.Scan(
		dbid, &p.TypeTag, &p.TypeTagDBID, &p.Text,
		&blobDBID, &contentType, &blobSize, &binary,
	); err != nil {
		return database.MediaProperty{}, fmt.Errorf("failed to scan grouped MediaProperty: %w", err)
	}
	setPropertyBlobFields(&p, blobDBID, contentType, blobSize, binary)
	return p, nil
}

func setPropertyBlobFields(
	p *database.MediaProperty,
	blobDBID sql.NullInt64,
	contentType sql.NullString,
	blobSize sql.NullInt64,
	binary []byte,
) {
	if blobDBID.Valid {
		p.BlobDBID = &blobDBID.Int64
	}
	p.ContentType = contentType.String
	if blobSize.Valid {
		p.BlobSize = blobSize.Int64
	}
	p.Binary = binary
}

func int64Args(values []int64) []any {
	args := make([]any, len(values))
	for i, value := range values {
		args[i] = value
	}
	return args
}
