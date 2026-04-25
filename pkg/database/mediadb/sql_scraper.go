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
func (db *MediaDB) FindMediaBySystemAndPath(ctx context.Context, systemDBID int64, path string) (*database.Media, error) {
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
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan FindMediaBySystemAndPath: %w", err)
	}
	return &row, nil
}

// FindMediaBySystemAndPathFold returns the Media row for the given system and
// path using a case-insensitive path comparison, or nil, nil when not found.
// LOWER() in SQLite covers ASCII only, which is sufficient for filesystem paths.
func (db *MediaDB) FindMediaBySystemAndPathFold(ctx context.Context, systemDBID int64, path string) (*database.Media, error) {
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
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan FindMediaBySystemAndPathFold: %w", err)
	}
	return &row, nil
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
		// No colon: treat the whole string as a raw tag value match.
		padded := tags.PadTagValue(tagValue)
		stmt, err := db.sql.PrepareContext(ctx, `
			SELECT 1
			FROM MediaTags mt
			JOIN Tags t ON mt.TagDBID = t.DBID
			WHERE mt.MediaDBID = ?
			  AND (t.Tag = ? OR t.Tag = ?)
			LIMIT 1
		`)
		if err != nil {
			return false, fmt.Errorf("failed to prepare MediaHasTag (no-colon): %w", err)
		}
		defer func() { _ = stmt.Close() }()
		var found int
		err = stmt.QueryRowContext(ctx, mediaDBID, tagValue, padded).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return found == 1, err
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
		return err
	}, func(tx *sql.Tx, tagDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO MediaTags (MediaDBID, TagDBID) VALUES (?, ?)`,
			mediaDBID, tagDBID,
		)
		return err
	})
}

// UpsertMediaTitleTags writes tags to MediaTitleTags for a specific MediaTitle row.
func (db *MediaDB) UpsertMediaTitleTags(ctx context.Context, mediaTitleDBID int64, tagInfos []database.TagInfo) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return upsertTags(ctx, db.sql, tagInfos, func(tx *sql.Tx, typeDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`DELETE FROM MediaTitleTags WHERE MediaTitleDBID = ? AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)`,
			mediaTitleDBID, typeDBID,
		)
		return err
	}, func(tx *sql.Tx, tagDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES (?, ?)`,
			mediaTitleDBID, tagDBID,
		)
		return err
	})
}

// upsertTags is the shared implementation for UpsertMediaTags and UpsertMediaTitleTags.
// deleteFn deletes existing tags of a type for the entity (called for exclusive types).
// insertFn inserts the tag link for the entity.
// All operations run inside a single transaction for atomicity.
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

	for _, ti := range tagInfos {
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

		// Resolve tag DBID; insert if missing using INSERT OR IGNORE to handle
		// concurrent writers outside this transaction (e.g. two goroutines
		// bootstrapping the same tag type simultaneously).
		tagValue := tags.PadTagValue(ti.Tag)
		var tagDBID int64
		err = tx.QueryRowContext(ctx,
			`SELECT DBID FROM Tags WHERE TypeDBID = ? AND Tag = ? LIMIT 1`,
			typeDBID, tagValue,
		).Scan(&tagDBID)
		if errors.Is(err, sql.ErrNoRows) {
			if _, insertErr := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO Tags (TypeDBID, Tag) VALUES (?, ?)`,
				typeDBID, tagValue,
			); insertErr != nil {
				return fmt.Errorf("failed to insert tag %q:%q: %w", ti.Type, ti.Tag, insertErr)
			}
			// Re-query after insert (handles both "we inserted" and "someone else did").
			if err = tx.QueryRowContext(ctx,
				`SELECT DBID FROM Tags WHERE TypeDBID = ? AND Tag = ? LIMIT 1`,
				typeDBID, tagValue,
			).Scan(&tagDBID); err != nil {
				return fmt.Errorf("failed to re-query tag DBID for %q:%q: %w", ti.Type, ti.Tag, err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to look up tag DBID for %q:%q: %w", ti.Type, ti.Tag, err)
		}

		// For exclusive types: delete all existing tags of this type for the entity.
		if isExclusive {
			if err := deleteFn(tx, typeDBID); err != nil {
				return fmt.Errorf("failed to delete exclusive tags for type %q: %w", ti.Type, err)
			}
		}

		// Insert the tag link.
		if err := insertFn(tx, tagDBID); err != nil {
			return fmt.Errorf("failed to insert tag link for %q:%q: %w", ti.Type, ti.Tag, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("upsertTags: commit: %w", err)
	}
	committed = true
	return nil
}

// UpsertMediaTitleProperties upserts properties into MediaTitleProperties.
// Conflicts on (MediaTitleDBID, TypeTagDBID) update data columns; DBID is preserved.
// p.TypeTag must be set to the full "type:value" string; TypeTagDBID is resolved
// from the Tags table automatically.
func (db *MediaDB) UpsertMediaTitleProperties(ctx context.Context, mediaTitleDBID int64, props []database.MediaProperty) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	for _, p := range props {
		typeTagDBID, err := resolvePropertyTypeTag(ctx, db.sql, p.TypeTag)
		if err != nil {
			return fmt.Errorf("failed to resolve property type tag %q: %w", p.TypeTag, err)
		}
		_, err = db.sql.ExecContext(ctx, `
			INSERT INTO MediaTitleProperties (MediaTitleDBID, TypeTagDBID, Text, ContentType, Binary)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(MediaTitleDBID, TypeTagDBID) DO UPDATE SET
				Text        = excluded.Text,
				ContentType = excluded.ContentType,
				Binary      = excluded.Binary
		`, mediaTitleDBID, typeTagDBID, p.Text, p.ContentType, p.Binary)
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
	for _, p := range props {
		typeTagDBID, err := resolvePropertyTypeTag(ctx, db.sql, p.TypeTag)
		if err != nil {
			return fmt.Errorf("failed to resolve property type tag %q: %w", p.TypeTag, err)
		}
		_, err = db.sql.ExecContext(ctx, `
			INSERT INTO MediaProperties (MediaDBID, TypeTagDBID, Text, ContentType, Binary)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(MediaDBID, TypeTagDBID) DO UPDATE SET
				Text        = excluded.Text,
				ContentType = excluded.ContentType,
				Binary      = excluded.Binary
		`, mediaDBID, typeTagDBID, p.Text, p.ContentType, p.Binary)
		if err != nil {
			return fmt.Errorf("failed to upsert MediaProperty (typeTag=%q): %w", p.TypeTag, err)
		}
	}
	return nil
}

// resolvePropertyTypeTag looks up the DBID of the Tags row for the given full
// tag string (e.g. "property:description"). The tag must already exist in the DB
// (seeded by SeedCanonicalTags). Returns an error if not found.
func resolvePropertyTypeTag(ctx context.Context, db *sql.DB, typeTag string) (int64, error) {
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
func (db *MediaDB) FindMediaTitlesWithoutSentinel(ctx context.Context, systemDBID int64, sentinelTag string) ([]database.MediaTitle, error) {
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
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan FindMediaTitleByDBID: %w", err)
	}
	return &t, nil
}

// GetMediaTitleProperties returns all MediaTitleProperties rows for the given
// title. TypeTag is populated as "type:value" from the joined Tags/TagTypes rows.
func (db *MediaDB) GetMediaTitleProperties(ctx context.Context, mediaTitleDBID int64) ([]database.MediaProperty, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.PrepareContext(ctx, `
		SELECT tt.Type || ':' || t.Tag, mtp.TypeTagDBID, mtp.Text, mtp.ContentType, mtp.Binary
		FROM MediaTitleProperties mtp
		JOIN Tags t    ON mtp.TypeTagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE mtp.MediaTitleDBID = ?
	`)
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

// GetMediaProperties returns all MediaProperties rows for the given Media record.
// TypeTag is populated as "type:value" from the joined Tags/TagTypes rows.
func (db *MediaDB) GetMediaProperties(ctx context.Context, mediaDBID int64) ([]database.MediaProperty, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.PrepareContext(ctx, `
		SELECT tt.Type || ':' || t.Tag, mp.TypeTagDBID, mp.Text, mp.ContentType, mp.Binary
		FROM MediaProperties mp
		JOIN Tags t    ON mp.TypeTagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE mp.MediaDBID = ?
	`)
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

func scanProperties(rows *sql.Rows) ([]database.MediaProperty, error) {
	var props []database.MediaProperty
	for rows.Next() {
		var p database.MediaProperty
		if err := rows.Scan(&p.TypeTag, &p.TypeTagDBID, &p.Text, &p.ContentType, &p.Binary); err != nil {
			return nil, fmt.Errorf("failed to scan MediaProperty: %w", err)
		}
		props = append(props, p)
	}
	if props == nil {
		props = []database.MediaProperty{}
	}
	return props, rows.Err()
}
