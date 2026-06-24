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
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

type mediaUserDataKey struct {
	systemID string
	path     string
}

// GetExistingMediaUserData reads user-authored data already materialized in
// media.db — favourites (user:favorite MediaTags) and launcher overrides
// (launcher-override MediaProperties) — and returns it as MediaUserData rows
// keyed by (SystemID, Path). It exists so a one-time startup backfill can seed
// UserDB from versions that wrote this data only to media.db. A row carries
// whichever of IsFavorite / LauncherOverride applies; both may be set.
func (db *MediaDB) GetExistingMediaUserData(ctx context.Context) ([]database.MediaUserData, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}

	merged := make(map[mediaUserDataKey]*database.MediaUserData)
	if err := scanExistingFavorites(ctx, conn, merged); err != nil {
		return nil, err
	}
	if err := scanExistingLauncherOverrides(ctx, conn, merged); err != nil {
		return nil, err
	}

	result := make([]database.MediaUserData, 0, len(merged))
	for _, entry := range merged {
		result = append(result, *entry)
	}
	return result, nil
}

func mergedEntry(merged map[mediaUserDataKey]*database.MediaUserData, k mediaUserDataKey) *database.MediaUserData {
	entry := merged[k]
	if entry == nil {
		entry = &database.MediaUserData{SystemID: k.systemID, Path: k.path}
		merged[k] = entry
	}
	return entry
}

func scanExistingFavorites(
	ctx context.Context, conn *sql.DB, merged map[mediaUserDataKey]*database.MediaUserData,
) error {
	// Tags are stored in their padded form (see sql_tags.go), so match that.
	favTag := tags.PadTagValue(string(tags.TagUserFavorite))
	rows, err := conn.QueryContext(ctx, `
		SELECT s.SystemID, m.Path
		FROM MediaTags mt
		JOIN Tags t ON mt.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		JOIN Media m ON mt.MediaDBID = m.DBID
		JOIN Systems s ON m.SystemDBID = s.DBID
		WHERE tt.Type = ? AND t.Tag = ?
	`, string(tags.TagTypeUser), favTag)
	if err != nil {
		return fmt.Errorf("failed to query existing favourites: %w", err)
	}
	defer closeRows(rows)

	for rows.Next() {
		var k mediaUserDataKey
		if scanErr := rows.Scan(&k.systemID, &k.path); scanErr != nil {
			return fmt.Errorf("failed to scan existing favourite: %w", scanErr)
		}
		mergedEntry(merged, k).IsFavorite = true
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("failed to iterate existing favourites: %w", rowsErr)
	}
	return nil
}

func scanExistingLauncherOverrides(
	ctx context.Context, conn *sql.DB, merged map[mediaUserDataKey]*database.MediaUserData,
) error {
	// Tags are stored in their padded form (see sql_tags.go), so match that.
	overrideTag := tags.PadTagValue(string(tags.TagPropertyLauncherOverride))
	rows, err := conn.QueryContext(ctx, `
		SELECT s.SystemID, m.Path, mp.Text
		FROM MediaProperties mp
		JOIN Tags t ON mp.TypeTagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		JOIN Media m ON mp.MediaDBID = m.DBID
		JOIN Systems s ON m.SystemDBID = s.DBID
		WHERE tt.Type = ? AND t.Tag = ?
	`, string(tags.TagTypeProperty), overrideTag)
	if err != nil {
		return fmt.Errorf("failed to query existing launcher overrides: %w", err)
	}
	defer closeRows(rows)

	for rows.Next() {
		var (
			k    mediaUserDataKey
			text string
		)
		if scanErr := rows.Scan(&k.systemID, &k.path, &text); scanErr != nil {
			return fmt.Errorf("failed to scan existing launcher override: %w", scanErr)
		}
		if text == "" {
			continue
		}
		mergedEntry(merged, k).LauncherOverride = text
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("failed to iterate existing launcher overrides: %w", rowsErr)
	}
	return nil
}

func closeRows(rows *sql.Rows) {
	if closeErr := rows.Close(); closeErr != nil {
		log.Warn().Err(closeErr).Msg("failed to close sql rows")
	}
}
