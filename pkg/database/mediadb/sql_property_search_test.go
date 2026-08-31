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

func TestSearchMediaByProperty_MatchesStoredValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	require.NoError(t, mediaDB.BeginTransaction(false))
	propertyType, err := mediaDB.InsertTagType(database.TagType{Type: string(tags.TagTypeProperty)})
	require.NoError(t, err)
	_, err = mediaDB.InsertTag(database.Tag{
		TypeDBID: propertyType.DBID,
		Tag:      string(tags.TagPropertyGameID),
	})
	require.NoError(t, err)

	system, err := mediaDB.InsertSystem(database.System{SystemID: "PSX", Name: "PSX"})
	require.NoError(t, err)
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: system.DBID, Slug: "ff7", Name: "Final Fantasy VII",
	})
	require.NoError(t, err)
	path := filepath.Join("roms", "PSX", "Final Fantasy VII (Disc 1).cue")
	media, err := mediaDB.InsertMedia(database.Media{
		MediaTitleDBID: title.DBID, SystemDBID: system.DBID, Path: path,
	})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	const gameID = "SLUS-00594"
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, media.DBID, []database.MediaProperty{
		{TypeTag: tags.PropertyTypeTag(tags.TagPropertyGameID), Text: gameID},
	}))

	results, err := mediaDB.SearchMediaByProperty(ctx, "PSX", string(tags.TagPropertyGameID), gameID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "PSX", results[0].SystemID)
	assert.Equal(t, path, results[0].Path)
	assert.Equal(t, media.DBID, results[0].MediaID)

	unscoped, err := mediaDB.SearchMediaByProperty(ctx, "", string(tags.TagPropertyGameID), gameID)
	require.NoError(t, err)
	require.Len(t, unscoped, 1)

	wrongSystem, err := mediaDB.SearchMediaByProperty(ctx, "PS2", string(tags.TagPropertyGameID), gameID)
	require.NoError(t, err)
	assert.Empty(t, wrongSystem)

	noMatch, err := mediaDB.SearchMediaByProperty(ctx, "PSX", string(tags.TagPropertyGameID), "SLUS-99999")
	require.NoError(t, err)
	assert.Empty(t, noMatch)
}

func TestHasMediaPropertyForPath_MatchesStoredProperty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	media := insertMediaWithGameIDProperty(t, mediaDB, "PSX", "SLUS-00594")

	has, err := mediaDB.HasMediaPropertyForPath(ctx, "PSX", media.Path, string(tags.TagPropertyGameID))
	require.NoError(t, err)
	assert.True(t, has)

	wrongPath, err := mediaDB.HasMediaPropertyForPath(ctx, "PSX", filepath.Join("roms", "PSX", "missing.cue"),
		string(tags.TagPropertyGameID))
	require.NoError(t, err)
	assert.False(t, wrongPath)

	wrongSystem, err := mediaDB.HasMediaPropertyForPath(ctx, "PS2", media.Path, string(tags.TagPropertyGameID))
	require.NoError(t, err)
	assert.False(t, wrongSystem)
}

func insertMediaWithGameIDProperty(t *testing.T, mediaDB *MediaDB, systemID, gameID string) database.Media {
	t.Helper()
	ctx := context.Background()

	require.NoError(t, mediaDB.BeginTransaction(false))
	propertyType, err := mediaDB.InsertTagType(database.TagType{Type: string(tags.TagTypeProperty)})
	require.NoError(t, err)
	_, err = mediaDB.InsertTag(database.Tag{
		TypeDBID: propertyType.DBID,
		Tag:      string(tags.TagPropertyGameID),
	})
	require.NoError(t, err)

	system, err := mediaDB.InsertSystem(database.System{SystemID: systemID, Name: systemID})
	require.NoError(t, err)
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: system.DBID, Slug: "ff7", Name: "Final Fantasy VII",
	})
	require.NoError(t, err)
	path := filepath.Join("roms", systemID, "Final Fantasy VII (Disc 1).cue")
	media, err := mediaDB.InsertMedia(database.Media{
		MediaTitleDBID: title.DBID, SystemDBID: system.DBID, Path: path,
	})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, media.DBID, []database.MediaProperty{
		{TypeTag: tags.PropertyTypeTag(tags.TagPropertyGameID), Text: gameID},
	}))
	return media
}

func TestSearchMediaByProperty_NullSQLGuard(t *testing.T) {
	t.Parallel()
	mediaDB := &MediaDB{}
	_, err := mediaDB.SearchMediaByProperty(context.Background(), "PSX", string(tags.TagPropertyGameID), "SLUS-00594")
	require.ErrorIs(t, err, ErrNullSQL)
}
