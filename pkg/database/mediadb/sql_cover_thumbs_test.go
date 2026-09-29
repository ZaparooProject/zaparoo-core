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
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mediaCoverThumbsVersion is 20260930120000_media_cover_thumbs.
const mediaCoverThumbsVersion = 20260930120000

func TestMediaCoverThumbsMigrationUpgrade(t *testing.T) {
	db, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()
	sqlDB := db.sql.Load()

	goose.SetBaseFS(migrationFiles)
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.DownTo(sqlDB, "migrations", mediaCoverThumbsVersion-1))
	_, err := sqlDB.ExecContext(ctx, "SELECT 1 FROM MediaCoverThumbs LIMIT 1")
	require.Error(t, err, "the table must not exist before the migration")

	require.NoError(t, goose.Up(sqlDB, "migrations"))
	var media int
	require.NoError(t, sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&media))
	assert.Equal(t, 1, media, "existing media rows survive the upgrade")
	require.NoError(t, db.PutMediaCoverThumb(ctx, 1, "property:image-boxart", nil))
}

func TestMediaCoverThumbsRoundTrip(t *testing.T) {
	t.Parallel()
	db, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()
	sqlDB := db.sql.Load()
	_, err := sqlDB.ExecContext(ctx, `
		INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES (3, 3, 'image-screenshot');
		INSERT INTO MediaProperties (MediaDBID, TypeTagDBID, Text) VALUES (1, 3, '/a.png');
		INSERT INTO MediaTitleProperties (MediaTitleDBID, TypeTagDBID, Text)
			VALUES (1, 2, '/b.png');
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (2, 'SNES', 'Super Nintendo');
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 2, 'zelda', 'Zelda');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 2, 'roms/zelda.sfc');
	`)
	require.NoError(t, err)

	_, found, err := db.GetMediaCoverThumb(ctx, 1)
	require.NoError(t, err)
	assert.False(t, found, "no record yet")

	color := uint32(0x123456)
	require.NoError(t, db.PutMediaCoverThumb(ctx, 1, "property:image-boxart", &color))
	thumb, found, err := db.GetMediaCoverThumb(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "NES", thumb.SystemID)
	assert.Equal(t, "roms/mario.nes", thumb.Path)
	assert.Equal(t, "property:image-boxart", thumb.TypeTag)
	require.NotNil(t, thumb.Color)
	assert.Equal(t, color, *thumb.Color)
	assert.ElementsMatch(t, []string{"property:image-boxart", "property:image-screenshot"}, thumb.AvailableTypeTags)

	// Same type without a colour keeps the known colour; a new type drops it.
	require.NoError(t, db.PutMediaCoverThumb(ctx, 1, "property:image-boxart", nil))
	colors, err := db.GetMediaCoverColors(ctx, []int64{1, 2, 99})
	require.NoError(t, err)
	assert.Equal(t, map[int64]uint32{1: color}, colors)
	require.NoError(t, db.PutMediaCoverThumb(ctx, 1, "property:image-screenshot", nil))
	colors, err = db.GetMediaCoverColors(ctx, []int64{1})
	require.NoError(t, err)
	assert.Empty(t, colors)

	// A missing media row is ignored rather than violating the foreign key.
	require.NoError(t, db.PutMediaCoverThumb(ctx, 99, "property:image-boxart", &color))
	_, found, err = db.GetMediaCoverThumb(ctx, 99)
	require.NoError(t, err)
	assert.False(t, found)

	other := uint32(0xabcdef)
	require.NoError(t, db.PutMediaCoverThumb(ctx, 1, "property:image-boxart", &color))
	require.NoError(t, db.PutMediaCoverThumb(ctx, 2, "property:image-boxart", &other))
	require.NoError(t, db.ClearMediaCoverThumbsForSystems(ctx, []string{"SNES"}))
	colors, err = db.GetMediaCoverColors(ctx, []int64{1, 2})
	require.NoError(t, err)
	assert.Equal(t, map[int64]uint32{1: color}, colors)

	// Deleting media cascades to its record.
	_, err = sqlDB.ExecContext(ctx, "DELETE FROM Media WHERE DBID = 1")
	require.NoError(t, err)
	var remaining int
	require.NoError(t, sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM MediaCoverThumbs").Scan(&remaining))
	assert.Zero(t, remaining)

	require.NoError(t, db.PutMediaCoverThumb(ctx, 2, "property:image-boxart", &other))
	require.NoError(t, db.ClearMediaCoverThumbs(ctx))
	require.NoError(t, sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM MediaCoverThumbs").Scan(&remaining))
	assert.Zero(t, remaining)
}
