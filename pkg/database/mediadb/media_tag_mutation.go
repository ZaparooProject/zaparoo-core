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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

type resolvedMediaTag struct {
	tagDBID     int64
	tagTypeDBID int64
	isExclusive bool
}

const refreshMediaTagCacheSQL = `
	WITH tag_count AS (
		SELECT
			(SELECT COUNT(*)
			 FROM MediaTags mt
			 JOIN Media m ON m.DBID = mt.MediaDBID
			 WHERE mt.TagDBID = ? AND m.SystemDBID = ? AND m.IsMissing = 0)
			+
			(SELECT COUNT(*)
			 FROM MediaTitleTags mtt
			 JOIN MediaTitles mtl ON mtl.DBID = mtt.MediaTitleDBID
			 WHERE mtt.TagDBID = ? AND mtl.SystemDBID = ?
			   AND EXISTS (
				   SELECT 1 FROM Media m
				   WHERE m.MediaTitleDBID = mtl.DBID AND m.IsMissing = 0
			   )) AS Count
	)
	INSERT INTO SystemTagsCache (SystemDBID, TagDBID, TagType, Tag, Count)
	SELECT ?, t.DBID, tt.Type, t.Tag, tag_count.Count
	FROM Tags t
	JOIN TagTypes tt ON tt.DBID = t.TypeDBID
	CROSS JOIN tag_count
	WHERE t.DBID = ? AND tag_count.Count > 0`

// UpdateMediaTags mutates file-level tags and refreshes only affected cache
// aggregates. Removals run before additions, so an addition wins when the same
// tag appears in both lists.
func (db *MediaDB) UpdateMediaTags(
	ctx context.Context,
	mediaDBID int64,
	remove []database.MediaTagRef,
	add []database.MediaTagRef,
) error {
	if len(remove) == 0 && len(add) == 0 {
		return nil
	}

	systemID, cacheChanged, err := db.applyMediaTagMutations(ctx, mediaDBID, remove, add)
	if err != nil {
		return err
	}
	if !cacheChanged {
		return nil
	}

	// SQL cache rows were updated transactionally. Drop process and persisted
	// snapshots so no pre-mutation aggregate survives this generation.
	db.inMemoryTagCache.Store(nil)
	clearUtilityTagCache()
	if persistErr := db.PersistTagCache(); persistErr != nil {
		log.Warn().Err(persistErr).Str("system", systemID).
			Msg("failed to remove stale persisted tag cache after media tag update")
	}
	return nil
}

func (db *MediaDB) applyMediaTagMutations(
	ctx context.Context,
	mediaDBID int64,
	remove []database.MediaTagRef,
	add []database.MediaTagRef,
) (systemID string, cacheChanged bool, err error) {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return "", false, ErrNullSQL
	}
	if db.inTransaction {
		return "", false, ErrTransactionActive
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", false, ctxErr
	}

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return "", false, fmt.Errorf("begin media tag transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var systemDBID int64
	if err = tx.QueryRowContext(ctx, `
		SELECT m.SystemDBID, s.SystemID
		FROM Media m
		JOIN Systems s ON s.DBID = m.SystemDBID
		WHERE m.DBID = ?`, mediaDBID).Scan(&systemDBID, &systemID); err != nil {
		return "", false, fmt.Errorf("resolve media tag system: %w", err)
	}

	affectedTagDBIDs := make(map[int64]struct{}, len(remove)+len(add))
	if removeErr := removeMediaTags(ctx, tx, mediaDBID, remove, affectedTagDBIDs); removeErr != nil {
		return "", false, removeErr
	}
	if addErr := addMediaTags(ctx, tx, mediaDBID, add, affectedTagDBIDs); addErr != nil {
		return "", false, addErr
	}
	if len(affectedTagDBIDs) == 0 {
		return systemID, false, nil
	}

	cacheReady, cacheErr := systemTagCacheReady(ctx, tx, systemDBID)
	if cacheErr != nil {
		return "", false, cacheErr
	}
	if cacheReady {
		for tagDBID := range affectedTagDBIDs {
			if refreshErr := refreshMediaTagCache(ctx, tx, systemDBID, tagDBID); refreshErr != nil {
				return "", false, refreshErr
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM MediaCountCache"); err != nil {
		return "", false, fmt.Errorf("invalidate media count cache after tag update: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		"DELETE FROM SlugResolutionCache WHERE SystemID = ?", systemID,
	); err != nil {
		return "", false, fmt.Errorf("invalidate slug cache after tag update: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return "", false, fmt.Errorf("commit media tag transaction: %w", err)
	}
	committed = true
	return systemID, true, nil
}

func removeMediaTags(
	ctx context.Context,
	tx *sql.Tx,
	mediaDBID int64,
	refs []database.MediaTagRef,
	affectedTagDBIDs map[int64]struct{},
) error {
	for _, ref := range refs {
		resolved, found, err := findOrCreateMediaTag(ctx, tx, ref, false)
		if err != nil {
			return fmt.Errorf("resolve media tag %s:%s for removal: %w", ref.Type, ref.Tag, err)
		}
		if !found {
			continue
		}
		if _, err = tx.ExecContext(ctx,
			"DELETE FROM MediaTags WHERE MediaDBID = ? AND TagDBID = ?", mediaDBID, resolved.tagDBID,
		); err != nil {
			return fmt.Errorf("remove media tag %s:%s: %w", ref.Type, ref.Tag, err)
		}
		affectedTagDBIDs[resolved.tagDBID] = struct{}{}
	}
	return nil
}

func addMediaTags(
	ctx context.Context,
	tx *sql.Tx,
	mediaDBID int64,
	refs []database.MediaTagRef,
	affectedTagDBIDs map[int64]struct{},
) error {
	for _, ref := range refs {
		resolved, _, err := findOrCreateMediaTag(ctx, tx, ref, true)
		if err != nil {
			return fmt.Errorf("resolve media tag %s:%s for addition: %w", ref.Type, ref.Tag, err)
		}
		if resolved.isExclusive {
			if err = removeExclusiveMediaTags(
				ctx, tx, mediaDBID, resolved.tagTypeDBID, affectedTagDBIDs,
			); err != nil {
				return fmt.Errorf("replace media tag type %s: %w", ref.Type, err)
			}
		}
		if _, err = tx.ExecContext(ctx, insertMediaTagSQL, mediaDBID, resolved.tagDBID); err != nil {
			return fmt.Errorf("add media tag %s:%s: %w", ref.Type, ref.Tag, err)
		}
		affectedTagDBIDs[resolved.tagDBID] = struct{}{}
	}
	return nil
}

func removeExclusiveMediaTags(
	ctx context.Context,
	tx *sql.Tx,
	mediaDBID int64,
	tagTypeDBID int64,
	affectedTagDBIDs map[int64]struct{},
) error {
	rows, err := tx.QueryContext(ctx, `
		DELETE FROM MediaTags
		WHERE MediaDBID = ?
		  AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)
		RETURNING TagDBID`, mediaDBID, tagTypeDBID)
	if err != nil {
		return fmt.Errorf("delete existing exclusive media tags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var tagDBID int64
		if err = rows.Scan(&tagDBID); err != nil {
			return fmt.Errorf("scan replaced exclusive media tag: %w", err)
		}
		affectedTagDBIDs[tagDBID] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate replaced exclusive media tags: %w", err)
	}
	return nil
}

func findOrCreateMediaTag(
	ctx context.Context,
	tx *sql.Tx,
	ref database.MediaTagRef,
	create bool,
) (resolvedMediaTag, bool, error) {
	if ref.Type == "" || ref.Tag == "" {
		return resolvedMediaTag{}, false, errors.New("media tag type and value are required")
	}

	var resolved resolvedMediaTag
	err := tx.QueryRowContext(ctx,
		"SELECT DBID, IsExclusive FROM TagTypes WHERE Type = ?", ref.Type,
	).Scan(&resolved.tagTypeDBID, &resolved.isExclusive)
	if errors.Is(err, sql.ErrNoRows) {
		if !create {
			return resolvedMediaTag{}, false, nil
		}
		resolved.isExclusive = tags.IsExclusiveType(tags.TagType(ref.Type))
		result, insertErr := tx.ExecContext(ctx,
			"INSERT INTO TagTypes (Type, IsExclusive) VALUES (?, ?)",
			ref.Type, resolved.isExclusive,
		)
		if insertErr != nil {
			return resolvedMediaTag{}, false, fmt.Errorf("insert tag type %q: %w", ref.Type, insertErr)
		}
		resolved.tagTypeDBID, err = result.LastInsertId()
	}
	if err != nil {
		return resolvedMediaTag{}, false, fmt.Errorf("find tag type %q: %w", ref.Type, err)
	}

	paddedTag := tags.PadTagValue(ref.Tag)
	err = tx.QueryRowContext(ctx, `
		SELECT DBID
		FROM Tags
		WHERE TypeDBID = ? AND (Tag = ? OR Tag = ?)
		ORDER BY DBID
		LIMIT 1`, resolved.tagTypeDBID, ref.Tag, paddedTag).Scan(&resolved.tagDBID)
	if errors.Is(err, sql.ErrNoRows) {
		if !create {
			return resolvedMediaTag{}, false, nil
		}
		result, insertErr := tx.ExecContext(ctx,
			"INSERT INTO Tags (TypeDBID, Tag, DisplayName) VALUES (?, ?, '')",
			resolved.tagTypeDBID, paddedTag,
		)
		if insertErr != nil {
			return resolvedMediaTag{}, false, fmt.Errorf("insert tag %q: %w", ref.Tag, insertErr)
		}
		resolved.tagDBID, err = result.LastInsertId()
	}
	if err != nil {
		return resolvedMediaTag{}, false, fmt.Errorf("find tag %q: %w", ref.Tag, err)
	}
	return resolved, true, nil
}

func systemTagCacheReady(ctx context.Context, tx *sql.Tx, systemDBID int64) (bool, error) {
	var ready bool
	if err := tx.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM SystemTagsCache WHERE SystemDBID = ?)", systemDBID,
	).Scan(&ready); err != nil {
		return false, fmt.Errorf("check system tag cache before media tag update: %w", err)
	}
	return ready, nil
}

func refreshMediaTagCache(ctx context.Context, tx *sql.Tx, systemDBID, tagDBID int64) error {
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM SystemTagsCache WHERE SystemDBID = ? AND TagDBID = ?", systemDBID, tagDBID,
	); err != nil {
		return fmt.Errorf("clear media tag cache entry: %w", err)
	}
	if _, err := tx.ExecContext(ctx, refreshMediaTagCacheSQL,
		tagDBID, systemDBID,
		tagDBID, systemDBID,
		systemDBID, tagDBID,
	); err != nil {
		return fmt.Errorf("refresh media tag cache entry: %w", err)
	}
	return nil
}
