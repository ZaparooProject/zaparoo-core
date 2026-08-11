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

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemMediaCounts_TagScopes(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	nesPath := filepath.Join("roms", "nes", "chrono-trigger.nes")
	nesSystem := insertSystemWithMedia(t, mediaDB, "NES", "Chrono Trigger", nesPath)
	nesMedia, err := mediaDB.FindMedia(database.Media{SystemDBID: nesSystem.DBID, Path: nesPath})
	require.NoError(t, err)

	variantPath := filepath.Join("roms", "nes", "chrono-trigger-rev-a.nes")
	require.NoError(t, mediaDB.BeginTransaction(false))
	_, err = mediaDB.InsertMedia(database.Media{
		SystemDBID:     nesSystem.DBID,
		MediaTitleDBID: nesMedia.MediaTitleDBID,
		Path:           variantPath,
		ParentDir:      filepath.ToSlash(filepath.Dir(variantPath)) + "/",
		SortName:       "Chrono Trigger",
	})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	snesPath := filepath.Join("roms", "snes", "earthbound.sfc")
	snesSystem := insertSystemWithMedia(t, mediaDB, "SNES", "EarthBound", snesPath)
	snesMedia, err := mediaDB.FindMedia(database.Media{SystemDBID: snesSystem.DBID, Path: snesPath})
	require.NoError(t, err)

	require.NoError(t, mediaDB.UpdateMediaTags(ctx, nesMedia.DBID, nil, []database.MediaTagRef{{
		Type: "user",
		Tag:  "favorite",
	}}))
	require.NoError(t, mediaDB.UpdateMediaTags(ctx, snesMedia.DBID, nil, []database.MediaTagRef{{
		Type: "user",
		Tag:  "favorite",
	}}))
	require.NoError(t, mediaDB.UpsertMediaTitleTags(ctx, nesMedia.MediaTitleDBID, []database.TagInfo{{
		Type: "genre",
		Tag:  "rpg",
	}}))

	allCounts, err := mediaDB.SystemMediaCounts(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, []database.SystemMediaCount{
		{SystemID: "NES", Count: 2},
		{SystemID: "SNES", Count: 1},
	}, allCounts)

	favoriteCounts, err := mediaDB.SystemMediaCounts(ctx, []zapscript.TagFilter{{
		Type:  "user",
		Value: "favorite",
	}})
	require.NoError(t, err)
	assert.Equal(t, []database.SystemMediaCount{
		{SystemID: "NES", Count: 1},
		{SystemID: "SNES", Count: 1},
	}, favoriteCounts)

	titleCounts, err := mediaDB.SystemMediaCounts(ctx, []zapscript.TagFilter{{
		Type:  "genre",
		Value: "rpg",
	}})
	require.NoError(t, err)
	assert.Equal(t, []database.SystemMediaCount{{SystemID: "NES", Count: 2}}, titleCounts)

	combinedCounts, err := mediaDB.SystemMediaCounts(ctx, []zapscript.TagFilter{
		{Type: "genre", Value: "rpg"},
		{Type: "user", Value: "favorite"},
	})
	require.NoError(t, err)
	assert.Equal(t, []database.SystemMediaCount{{SystemID: "NES", Count: 1}}, combinedCounts)

	orCounts, err := mediaDB.SystemMediaCounts(ctx, []zapscript.TagFilter{
		{Type: "genre", Value: "rpg", Operator: zapscript.TagOperatorOR},
		{Type: "user", Value: "favorite", Operator: zapscript.TagOperatorOR},
	})
	require.NoError(t, err)
	assert.Equal(t, []database.SystemMediaCount{
		{SystemID: "NES", Count: 2},
		{SystemID: "SNES", Count: 1},
	}, orCounts)

	notFavoriteCounts, err := mediaDB.SystemMediaCounts(ctx, []zapscript.TagFilter{{
		Type: "user", Value: "favorite", Operator: zapscript.TagOperatorNOT,
	}})
	require.NoError(t, err)
	assert.Equal(t, []database.SystemMediaCount{{SystemID: "NES", Count: 1}}, notFavoriteCounts)

	notRPGCounts, err := mediaDB.SystemMediaCounts(ctx, []zapscript.TagFilter{{
		Type: "genre", Value: "rpg", Operator: zapscript.TagOperatorNOT,
	}})
	require.NoError(t, err)
	assert.Equal(t, []database.SystemMediaCount{{SystemID: "SNES", Count: 1}}, notRPGCounts)

	multipleNotCounts, err := mediaDB.SystemMediaCounts(ctx, []zapscript.TagFilter{
		{Type: "user", Value: "favorite", Operator: zapscript.TagOperatorNOT},
		{Type: "genre", Value: "rpg", Operator: zapscript.TagOperatorNOT},
	})
	require.NoError(t, err)
	assert.Empty(t, multipleNotCounts)
}

func TestSystemMediaCounts_UsesBrowseCache(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaPath := filepath.Join("roms", "nes", "game.nes")
	system := insertSystemWithMedia(t, mediaDB, "NES", "Game", mediaPath)
	db := mediaDB.sql.Load()

	_, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO BrowseDirs (Path, Name, IsVirtual) VALUES ('/', '/', 0)`)
	require.NoError(t, err)
	var rootDirDBID int64
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT DBID FROM BrowseDirs WHERE Path = '/'",
	).Scan(&rootDirDBID))
	_, err = db.ExecContext(ctx, `
		INSERT OR REPLACE INTO BrowseDirCounts
			(ParentDirDBID, ChildDirDBID, SystemDBID, FileCount)
		VALUES (?, ?, ?, ?)`, rootDirDBID, rootDirDBID, system.DBID, 42)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)`,
		DBConfigBrowseIndexVersion, browseCacheSchemaVersion)
	require.NoError(t, err)

	counts, err := mediaDB.SystemMediaCounts(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, []database.SystemMediaCount{{SystemID: "NES", Count: 42}}, counts)
}

func TestSystemMediaCounts_ExcludesMissingMedia(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaPath := filepath.Join("roms", "nes", "missing.nes")
	system := insertSystemWithMedia(t, mediaDB, "NES", "Missing", mediaPath)
	media, err := mediaDB.FindMedia(database.Media{SystemDBID: system.DBID, Path: mediaPath})
	require.NoError(t, err)
	require.NoError(t, mediaDB.UpdateMediaTags(ctx, media.DBID, nil, []database.MediaTagRef{{
		Type: "user",
		Tag:  "favorite",
	}}))

	_, err = mediaDB.sql.Load().ExecContext(ctx, "UPDATE Media SET IsMissing = 1 WHERE DBID = ?", media.DBID)
	require.NoError(t, err)

	counts, err := mediaDB.SystemMediaCounts(ctx, []zapscript.TagFilter{{
		Type:  "user",
		Value: "favorite",
	}})
	require.NoError(t, err)
	assert.Empty(t, counts)
}
