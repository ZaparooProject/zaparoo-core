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

package mediascanner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// reapplyMediaUserData re-materializes the media.db projection (favourite tags
// and launcher-override properties) from the UserDB source of truth after the
// media rows have been (re)built. UserDB owns this data so that a wiped or
// rebuilt media.db can be reconstructed; on an incremental reindex the rows
// already exist and the writes are idempotent no-ops.
//
// Writes use the same media.db primitives as the live edit handlers, so the
// projection is identical. It is add-only: live edits keep media.db in sync when
// a favourite/override is removed, so re-apply never needs to delete. Rows whose
// system or path is not currently indexed are harmless orphans and are skipped.
func reapplyMediaUserData(
	ctx context.Context, db database.MediaDBI, userDB database.UserDBI,
) (int, error) {
	rows, err := userDB.ListMediaUserData()
	if err != nil {
		return 0, fmt.Errorf("failed to list media user data: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}

	favTagDBID, err := ensureFavoriteTag(db)
	if err != nil {
		return 0, err
	}
	if err := ensureLauncherOverrideTag(db); err != nil {
		return 0, err
	}
	overrideTypeTag := tags.PropertyTypeTag(tags.TagPropertyLauncherOverride)

	bySystem := make(map[string][]database.MediaUserData)
	for _, row := range rows {
		bySystem[row.SystemID] = append(bySystem[row.SystemID], row)
	}

	applied := 0
	for systemID, items := range bySystem {
		system, sysErr := db.FindSystemBySystemID(systemID)
		if errors.Is(sysErr, sql.ErrNoRows) {
			continue // system not indexed; harmless orphans
		}
		if sysErr != nil {
			return applied, fmt.Errorf("failed to resolve system %q: %w", systemID, sysErr)
		}

		paths := make([]string, 0, len(items))
		for i := range items {
			paths = append(paths, items[i].Path)
		}
		mediaByPath, mErr := db.FindMediaBySystemAndPaths(ctx, system.DBID, paths)
		if mErr != nil {
			return applied, fmt.Errorf("failed to look up media for system %q: %w", systemID, mErr)
		}

		for i := range items {
			item := items[i]
			media, ok := mediaByPath[item.Path]
			if !ok {
				continue // path not indexed; harmless orphan
			}
			wrote := false
			if item.IsFavorite {
				if _, fErr := db.FindOrInsertMediaTag(database.MediaTag{
					MediaDBID: media.DBID,
					TagDBID:   favTagDBID,
				}); fErr != nil {
					return applied, fmt.Errorf("failed to re-apply favourite for %q: %w", item.Path, fErr)
				}
				wrote = true
			}
			if item.LauncherOverride != "" {
				if pErr := db.UpsertMediaProperties(ctx, media.DBID, []database.MediaProperty{{
					TypeTag: overrideTypeTag,
					Text:    item.LauncherOverride,
				}}); pErr != nil {
					return applied, fmt.Errorf("failed to re-apply launcher override for %q: %w", item.Path, pErr)
				}
				wrote = true
			}
			if wrote {
				applied++
			}
		}
	}

	log.Debug().Int("rows", len(rows)).Int("applied", applied).Msg("re-applied media user data")
	return applied, nil
}

// ensureFavoriteTag finds or inserts the canonical user:favorite tag and returns
// its DBID so per-media favourite tags can be written.
func ensureFavoriteTag(db database.MediaDBI) (int64, error) {
	tagType, err := db.FindOrInsertTagType(database.TagType{
		Type:        string(tags.TagTypeUser),
		IsExclusive: tags.IsExclusiveType(tags.TagTypeUser),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to find or insert user tag type: %w", err)
	}
	tagRow, err := db.FindOrInsertTag(database.Tag{
		TypeDBID: tagType.DBID,
		Tag:      string(tags.TagUserFavorite),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to find or insert favourite tag: %w", err)
	}
	return tagRow.DBID, nil
}

// ensureLauncherOverrideTag finds or inserts the canonical
// property:launcher-override tag so UpsertMediaProperties can resolve it.
func ensureLauncherOverrideTag(db database.MediaDBI) error {
	tagType, err := db.FindOrInsertTagType(database.TagType{
		Type:        string(tags.TagTypeProperty),
		IsExclusive: tags.IsExclusiveType(tags.TagTypeProperty),
	})
	if err != nil {
		return fmt.Errorf("failed to find or insert property tag type: %w", err)
	}
	if _, err := db.FindOrInsertTag(database.Tag{
		TypeDBID: tagType.DBID,
		Tag:      string(tags.TagPropertyLauncherOverride),
	}); err != nil {
		return fmt.Errorf("failed to find or insert launcher override tag: %w", err)
	}
	return nil
}
