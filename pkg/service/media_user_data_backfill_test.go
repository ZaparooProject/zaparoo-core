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

package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedMediaUserData inserts one favourited and one launcher-overridden media into
// media.db, mimicking a database written by a version that stored this data only
// there. Returns the two paths.
func seedMediaUserData(ctx context.Context, t *testing.T, mediaDB *mediadb.MediaDB) (favPath, overridePath string) {
	t.Helper()
	favPath = filepath.Join("roms", "NES", "Fav.nes")
	overridePath = filepath.Join("roms", "NES", "Override.nes")

	require.NoError(t, mediaDB.BeginTransaction(false))
	system, err := mediaDB.InsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{SystemDBID: system.DBID, Slug: "t", Name: "T"})
	require.NoError(t, err)
	favMedia, err := mediaDB.InsertMedia(database.Media{
		MediaTitleDBID: title.DBID, SystemDBID: system.DBID, Path: favPath,
	})
	require.NoError(t, err)
	overrideMedia, err := mediaDB.InsertMedia(database.Media{
		MediaTitleDBID: title.DBID, SystemDBID: system.DBID, Path: overridePath,
	})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	userType, err := mediaDB.FindOrInsertTagType(database.TagType{
		Type: string(tags.TagTypeUser), IsExclusive: tags.IsExclusiveType(tags.TagTypeUser),
	})
	require.NoError(t, err)
	favTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: userType.DBID, Tag: string(tags.TagUserFavorite)})
	require.NoError(t, err)
	_, err = mediaDB.FindOrInsertMediaTag(database.MediaTag{MediaDBID: favMedia.DBID, TagDBID: favTag.DBID})
	require.NoError(t, err)

	propType, err := mediaDB.FindOrInsertTagType(database.TagType{
		Type: string(tags.TagTypeProperty), IsExclusive: tags.IsExclusiveType(tags.TagTypeProperty),
	})
	require.NoError(t, err)
	_, err = mediaDB.FindOrInsertTag(database.Tag{
		TypeDBID: propType.DBID, Tag: string(tags.TagPropertyLauncherOverride),
	})
	require.NoError(t, err)
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, overrideMedia.DBID, []database.MediaProperty{{
		TypeTag: tags.PropertyTypeTag(tags.TagPropertyLauncherOverride),
		Text:    "RetroArch",
	}}))

	return favPath, overridePath
}

func TestBackfillMediaUserData(t *testing.T) {
	ctx := context.Background()

	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	favPath, overridePath := seedMediaUserData(ctx, t, mediaDB)
	db := &database.Database{MediaDB: mediaDB, UserDB: userDB}

	backfillMediaUserData(ctx, db)

	fav, found, err := userDB.GetMediaUserData("NES", favPath)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, fav.IsFavorite)

	override, found, err := userDB.GetMediaUserData("NES", overridePath)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "RetroArch", override.LauncherOverride)
}

func TestBackfillMediaUserDataGuardSkipsWhenPopulated(t *testing.T) {
	ctx := context.Background()

	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	// UserDB already has a row, so the one-time backfill must not run — even though
	// media.db has favourites/overrides that aren't in UserDB.
	manualPath := filepath.Join("roms", "SNES", "Manual.sfc")
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "SNES", Path: manualPath, IsFavorite: true,
	}))
	favPath, _ := seedMediaUserData(ctx, t, mediaDB)

	db := &database.Database{MediaDB: mediaDB, UserDB: userDB}
	backfillMediaUserData(ctx, db)

	_, found, err := userDB.GetMediaUserData("NES", favPath)
	require.NoError(t, err)
	assert.False(t, found, "guard must skip backfill when UserDB already has data")

	all, err := userDB.ListMediaUserData()
	require.NoError(t, err)
	assert.Len(t, all, 1, "only the pre-existing row remains")
}
