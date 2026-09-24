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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MigrateUp leaves stored tags and their cached lists alone when the tag
// vocabulary has changed; the next index run's seeding removes them.
func TestMigrateUpLeavesTagsForTheNextIndex(t *testing.T) {
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
			SELECT (SELECT DBID FROM MediaTitles LIMIT 1), DBID FROM Tags WHERE Tag = 'mario';
		INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES ('`+DBConfigCanonicalTagVocabHash+`', 'older-build');`)
	require.NoError(t, err)
	require.NoError(t, mediaDB.PopulateSystemTagsCache(ctx))
	require.NoError(t, mediaDB.RebuildTagCache())
	require.NoError(t, mediaDB.PersistTagCache())
	path := mediaDB.tagCachePath()
	require.FileExists(t, path)

	require.NoError(t, mediaDB.MigrateUp())

	count := func(query string) int {
		var n int
		require.NoError(t, conn.QueryRowContext(ctx, query).Scan(&n))
		return n
	}
	assert.Equal(t, 1, count("SELECT COUNT(*) FROM TagTypes WHERE Type = 'gamefamily'"))
	assert.Equal(t, 1, count(`SELECT COUNT(*) FROM MediaTitleTags l JOIN Tags t ON t.DBID = l.TagDBID
		WHERE t.Tag = 'mario'`))
	var stamp string
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigCanonicalTagVocabHash).Scan(&stamp))
	assert.Equal(t, "older-build", stamp, "startup must not seed, so the next index still prunes")
	assert.FileExists(t, path)

	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(ctx))
	assert.Zero(t, count("SELECT COUNT(*) FROM TagTypes WHERE Type = 'gamefamily'"),
		"the index run's seeding removes what the vocabulary refuses")
}
