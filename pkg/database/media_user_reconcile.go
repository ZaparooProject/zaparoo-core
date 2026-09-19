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

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	mediatags "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

type mediaUserDataKey struct {
	systemID string
	path     string
}

// ReconcileMediaUserData makes the MediaDB projection of media user data, the
// five user flag tags and the launcher override property, match UserDB
// exactly: missing ones are added and ones UserDB no longer holds are removed.
// It is for when UserDB was replaced as a whole, as by a backup restore, where
// the projection still describes the database that was replaced. Live edits
// and the post-index re-apply only ever need to add.
//
// Only the five flag tags and the override property are touched; deck tags
// and metadata tags stay as they are. A projection already in line writes
// nothing. Rows whose system or path is not indexed are skipped.
func ReconcileMediaUserData(ctx context.Context, db *Database) error {
	mediaUserFlagsMu.Lock()
	defer mediaUserFlagsMu.Unlock()

	rows, err := db.UserDB.ListMediaUserData()
	if err != nil {
		return fmt.Errorf("failed to list media user data: %w", err)
	}
	mediaIDs, err := resolveMediaUserDataIDs(ctx, db.MediaDB, rows)
	if err != nil {
		return err
	}

	flagsChanged := 0
	for _, flag := range MediaUserFlags {
		ids := make([]int64, 0)
		for i := range rows {
			id, indexed := mediaIDs[mediaUserDataKey{systemID: rows[i].SystemID, path: rows[i].Path}]
			if indexed && rows[i].Flag(flag) {
				ids = append(ids, id)
			}
		}
		ref := MediaTagRef{Type: string(mediatags.TagTypeUser), Tag: string(flag)}
		changed, setErr := db.MediaDB.SetMediaTagMembership(ctx, ref, ids)
		if setErr != nil {
			return fmt.Errorf("failed to reconcile media user %s: %w", flag, setErr)
		}
		if changed {
			flagsChanged++
		}
	}

	set, cleared, err := reconcileLauncherOverrides(ctx, db.MediaDB, rows, mediaIDs)
	if err != nil {
		return err
	}
	log.Info().Int("rows", len(rows)).Int("flagsChanged", flagsChanged).
		Int("overridesSet", set).Int("overridesCleared", cleared).
		Msg("reconciled media user data")
	return nil
}

// resolveMediaUserDataIDs maps each row whose path is indexed to its media.
func resolveMediaUserDataIDs(
	ctx context.Context, mediaDB MediaDBI, rows []MediaUserData,
) (map[mediaUserDataKey]int64, error) {
	pathsBySystem := make(map[string][]string)
	for i := range rows {
		pathsBySystem[rows[i].SystemID] = append(pathsBySystem[rows[i].SystemID], rows[i].Path)
	}
	mediaIDs := make(map[mediaUserDataKey]int64, len(rows))
	for systemID, paths := range pathsBySystem {
		system, err := mediaDB.FindSystemBySystemID(systemID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to resolve system %q: %w", systemID, err)
		}
		mediaByPath, err := mediaDB.FindMediaBySystemAndPaths(ctx, system.DBID, paths)
		if err != nil {
			return nil, fmt.Errorf("failed to look up media for system %q: %w", systemID, err)
		}
		for path := range mediaByPath {
			mediaIDs[mediaUserDataKey{systemID: systemID, path: path}] = mediaByPath[path].DBID
		}
	}
	return mediaIDs, nil
}

// reconcileLauncherOverrides clears every projected override UserDB does not
// hold and writes every held one that is missing or different.
func reconcileLauncherOverrides(
	ctx context.Context,
	mediaDB MediaDBI,
	rows []MediaUserData,
	mediaIDs map[mediaUserDataKey]int64,
) (set, cleared int, err error) {
	projected, err := mediaDB.GetExistingMediaUserData(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read projected launcher overrides: %w", err)
	}
	wanted := make(map[mediaUserDataKey]string)
	for i := range rows {
		if rows[i].LauncherOverride != "" {
			wanted[mediaUserDataKey{systemID: rows[i].SystemID, path: rows[i].Path}] = rows[i].LauncherOverride
		}
	}

	current := make(map[mediaUserDataKey]string)
	var stale []MediaUserData
	for i := range projected {
		if projected[i].LauncherOverride == "" {
			continue
		}
		key := mediaUserDataKey{systemID: projected[i].SystemID, path: projected[i].Path}
		current[key] = projected[i].LauncherOverride
		if _, keep := wanted[key]; !keep {
			stale = append(stale, projected[i])
		}
	}

	if len(stale) > 0 {
		// A projected override belongs to an indexed file by definition, but
		// UserDB may hold no row for it, so its media is looked up here.
		staleIDs, resolveErr := resolveMediaUserDataIDs(ctx, mediaDB, stale)
		if resolveErr != nil {
			return 0, 0, resolveErr
		}
		for i := range stale {
			id, indexed := staleIDs[mediaUserDataKey{systemID: stale[i].SystemID, path: stale[i].Path}]
			if !indexed {
				continue
			}
			if clearErr := clearLauncherOverrideProperty(ctx, mediaDB, id); clearErr != nil {
				return set, cleared, clearErr
			}
			cleared++
		}
	}

	for key, launcherID := range wanted {
		id, indexed := mediaIDs[key]
		if !indexed || current[key] == launcherID {
			continue
		}
		if setErr := setLauncherOverrideProperty(ctx, mediaDB, id, launcherID); setErr != nil {
			return set, cleared, setErr
		}
		set++
	}
	return set, cleared, nil
}
