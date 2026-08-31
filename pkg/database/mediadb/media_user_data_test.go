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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetExistingMediaUserData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	favPath := filepath.Join("roms", "NES", "Fav.nes")
	overridePath := filepath.Join("roms", "NES", "Override.nes")
	plainPath := filepath.Join("roms", "NES", "Plain.nes")

	require.NoError(t, mediaDB.BeginTransaction(false))
	system, err := mediaDB.InsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{SystemDBID: system.DBID, Slug: "t", Name: "T"})
	require.NoError(t, err)

	insertMedia := func(path string) database.Media {
		m, insErr := mediaDB.InsertMedia(database.Media{
			MediaTitleDBID: title.DBID, SystemDBID: system.DBID, Path: path,
		})
		require.NoError(t, insErr)
		return m
	}
	favMedia := insertMedia(favPath)
	overrideMedia := insertMedia(overridePath)
	insertMedia(plainPath)
	require.NoError(t, mediaDB.CommitTransaction())

	// Favourite on favMedia.
	userType, err := mediaDB.FindOrInsertTagType(database.TagType{
		Type: string(tags.TagTypeUser), IsExclusive: tags.IsExclusiveType(tags.TagTypeUser),
	})
	require.NoError(t, err)
	favTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: userType.DBID, Tag: string(tags.TagUserFavorite)})
	require.NoError(t, err)
	_, err = mediaDB.FindOrInsertMediaTag(database.MediaTag{MediaDBID: favMedia.DBID, TagDBID: favTag.DBID})
	require.NoError(t, err)

	// Launcher override on overrideMedia.
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

	result, err := mediaDB.GetExistingMediaUserData(ctx)
	require.NoError(t, err)

	got := make(map[string]database.MediaUserData, len(result))
	for _, r := range result {
		got[r.Path] = r
	}
	require.Len(t, got, 2, "only favourited/overridden media are returned")

	assert.True(t, got[favPath].IsFavorite)
	assert.Empty(t, got[favPath].LauncherOverride)
	assert.Equal(t, "NES", got[favPath].SystemID)

	assert.False(t, got[overridePath].IsFavorite)
	assert.Equal(t, "RetroArch", got[overridePath].LauncherOverride)

	_, plainPresent := got[plainPath]
	assert.False(t, plainPresent, "media with no user data is not returned")
}

func TestGetExistingMediaUserDataEmpty(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	result, err := mediaDB.GetExistingMediaUserData(context.Background())
	require.NoError(t, err)
	assert.Empty(t, result)
}
