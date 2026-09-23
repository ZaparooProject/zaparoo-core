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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrateUpPruneDropsCachedTagLists covers a startup prune: the tag lists
// cached in memory and on disk still name what was removed, and the persisted
// snapshot carries the same index generation, so without invalidation the
// next load would serve the removed tags again.
func TestMigrateUpPruneDropsCachedTagLists(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	require.NoError(t, mediaDB.MigrateUp())
	insertSystemWithMedia(t, mediaDB, "NES", "Game", filepath.Join("roms", "nes", "game.nes"))

	conn := mediaDB.sql.Load()
	_, err := conn.ExecContext(ctx, `
		INSERT INTO TagTypes (Type, IsExclusive) VALUES ('gamefamily', 1);
		INSERT INTO Tags (TypeDBID, Tag) SELECT DBID, 'mario' FROM TagTypes WHERE Type = 'gamefamily';
		INSERT INTO MediaTitleTags (MediaTitleDBID, TagDBID)
			SELECT (SELECT DBID FROM MediaTitles LIMIT 1), DBID FROM Tags WHERE Tag = 'mario';`)
	require.NoError(t, err)
	require.NoError(t, mediaDB.PopulateSystemTagsCache(ctx))
	require.NoError(t, mediaDB.RebuildTagCache())
	require.NoError(t, mediaDB.PersistTagCache())
	path := mediaDB.tagCachePath()
	require.FileExists(t, path)
	_, err = conn.ExecContext(ctx, "UPDATE DBConfig SET Value = 'older-build' WHERE Name = ?",
		DBConfigCanonicalTagVocabHash)
	require.NoError(t, err)

	require.NoError(t, mediaDB.MigrateUp())

	_, statErr := os.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist, "the persisted tag list must not survive a prune")
	all, err := mediaDB.GetAllUsedTags(ctx)
	require.NoError(t, err)
	for _, tag := range all {
		assert.NotEqual(t, "gamefamily", tag.Type, "a pruned type must not be listed")
	}
}
