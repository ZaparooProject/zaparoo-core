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
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// reapplyMediaUserData re-materializes the media.db projection (the user's
// favourite, hidden, liked, disliked and playlater tags and launcher-override
// properties) from the UserDB source of truth after the media rows have been
// (re)built. UserDB owns this data so that a wiped or rebuilt media.db can be
// reconstructed.
//
// Each system's current user tags are read in one batch first, and only the
// missing ones are written, so an incremental reindex, where the tags already
// exist, writes nothing. Missing tags are written in one transaction when the
// database supports it; one write per file costs about 50 ms on MiSTer storage.
//
// Writes use the same media.db primitives as the live edit handlers, so the
// projection is identical. It is add-only: live edits keep media.db in sync when
// a flag or override is removed, so re-apply never needs to delete; a UserDB
// replaced as a whole is database.ReconcileMediaUserData's job. Rows whose
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

	if err := ensureLauncherOverrideTag(db); err != nil {
		return 0, err
	}
	overrideTypeTag := tags.PropertyTypeTag(tags.TagPropertyLauncherOverride)

	bySystem := make(map[string][]database.MediaUserData)
	for i := range rows {
		bySystem[rows[i].SystemID] = append(bySystem[rows[i].SystemID], rows[i])
	}

	applied := 0
	var tagUpdates []database.MediaTagUpdate
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
		mediaIDs := make([]int64, 0, len(mediaByPath))
		for i := range items {
			if media, ok := mediaByPath[items[i].Path]; ok && hasProjectedFlag(&items[i]) {
				mediaIDs = append(mediaIDs, media.DBID)
			}
		}
		existing, tErr := db.GetMediaTagsByMediaDBIDs(ctx, mediaIDs)
		if tErr != nil {
			return applied, fmt.Errorf("failed to read user tags for system %q: %w", systemID, tErr)
		}

		for i := range items {
			item := items[i]
			media, ok := mediaByPath[item.Path]
			if !ok {
				continue // path not indexed; harmless orphan
			}
			if !hasProjectedFlag(&item) && item.LauncherOverride == "" {
				continue
			}
			applied++
			if refs := missingUserFlagTagRefs(&item, existing[media.DBID]); len(refs) > 0 {
				tagUpdates = append(tagUpdates, database.MediaTagUpdate{MediaDBID: media.DBID, Add: refs})
			}
			if item.LauncherOverride != "" {
				if pErr := db.UpsertMediaProperties(ctx, media.DBID, []database.MediaProperty{{
					TypeTag: overrideTypeTag,
					Text:    item.LauncherOverride,
				}}); pErr != nil {
					return applied, fmt.Errorf("failed to re-apply launcher override for %q: %w", item.Path, pErr)
				}
			}
		}
	}

	if err := writeMediaTagUpdates(ctx, db, tagUpdates); err != nil {
		return applied, err
	}

	log.Debug().Int("rows", len(rows)).Int("applied", applied).Int("tagWrites", len(tagUpdates)).
		Msg("re-applied media user data")
	return applied, nil
}

// writeMediaTagUpdates writes the edits in one transaction when the database
// supports batches, and one file at a time otherwise.
func writeMediaTagUpdates(ctx context.Context, db database.MediaDBI, updates []database.MediaTagUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	if batcher, ok := db.(database.MediaTagBatchUpdater); ok {
		if err := batcher.UpdateMediaTagsBatch(ctx, updates); err != nil {
			return fmt.Errorf("failed to re-apply user flags for %d files: %w", len(updates), err)
		}
		return nil
	}
	for i := range updates {
		if err := db.UpdateMediaTags(ctx, updates[i].MediaDBID, nil, updates[i].Add); err != nil {
			return fmt.Errorf("failed to re-apply user flags for media %d: %w", updates[i].MediaDBID, err)
		}
	}
	return nil
}

// hasProjectedFlag reports whether the row sets any flag projected as a tag.
func hasProjectedFlag(item *database.MediaUserData) bool {
	for _, flag := range database.MediaUserFlags {
		if item.Flag(flag) {
			return true
		}
	}
	return false
}

// missingUserFlagTagRefs lists the user tags a row's flags project to that the
// file does not carry yet.
func missingUserFlagTagRefs(item *database.MediaUserData, current []database.TagInfo) []database.MediaTagRef {
	var refs []database.MediaTagRef
	for _, flag := range database.MediaUserFlags {
		if !item.Flag(flag) || slices.ContainsFunc(current, func(tag database.TagInfo) bool {
			return tag.Type == string(tags.TagTypeUser) && tag.Tag == string(flag)
		}) {
			continue
		}
		refs = append(refs, database.MediaTagRef{Type: string(tags.TagTypeUser), Tag: string(flag)})
	}
	return refs
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
