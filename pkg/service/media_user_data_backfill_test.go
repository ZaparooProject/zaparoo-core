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
	"errors"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBackfillMediaHistoryUUIDs(t *testing.T) {
	t.Parallel()
	userDB := testhelpers.NewMockUserDBI()
	userDB.On("BackfillMediaHistoryUUIDs").Return(int64(3), nil).Once()

	backfilled, err := backfillMediaHistoryUUIDs(userDB)
	require.NoError(t, err)
	assert.Equal(t, int64(3), backfilled)

	userDB.AssertExpectations(t)
}

// seedMediaUserData inserts one favorited and one launcher-overridden media into
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

	require.NoError(t, backfillMediaUserData(ctx, db, nil))

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
	// media.db has favorites/overrides that aren't in UserDB.
	manualPath := filepath.Join("roms", "SNES", "Manual.sfc")
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "SNES", Path: manualPath, IsFavorite: true,
	}))
	favPath, _ := seedMediaUserData(ctx, t, mediaDB)

	db := &database.Database{MediaDB: mediaDB, UserDB: userDB}
	require.NoError(t, backfillMediaUserData(ctx, db, nil))

	_, found, err := userDB.GetMediaUserData("NES", favPath)
	require.NoError(t, err)
	assert.False(t, found, "guard must skip backfill when UserDB already has data")

	all, err := userDB.ListMediaUserData()
	require.NoError(t, err)
	assert.Len(t, all, 1, "only the pre-existing row remains")
}

// Rows rescued from a media database that has already been discarded are the only
// copy left, so the backfill has to take them in place of a read it can no longer
// do.
func TestBackfillMediaUserDataUsesRescuedRows(t *testing.T) {
	ctx := context.Background()

	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	rescuedPath := filepath.Join("roms", "NES", "Rescued.nes")
	rescued := []database.MediaUserData{{
		SystemID: "NES", Path: rescuedPath, IsFavorite: true, LauncherOverride: "RetroArch",
	}}

	// An empty media database stands in for the rebuilt one: without the rescued
	// rows there would be nothing to import.
	db := &database.Database{MediaDB: mediaDB, UserDB: userDB}
	require.NoError(t, backfillMediaUserData(ctx, db, rescued))

	row, found, err := userDB.GetMediaUserData("NES", rescuedPath)
	require.NoError(t, err)
	require.True(t, found, "rescued rows must be imported when UserDB is empty")
	assert.True(t, row.IsFavorite)
	assert.Equal(t, "RetroArch", row.LauncherOverride)
}

// A media database written by a newer build may not answer these queries at all. The
// caller cannot tell that from "there was nothing to rescue" — one means the file held
// no favorites, the other means it may have held some that are about to be deleted
// unread — so it is reported as an error.
func TestRescueMediaUserDataOnUnreadableDatabase(t *testing.T) {
	ctx := context.Background()

	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	require.NoError(t, mediaDB.Close())

	rows, err := rescueMediaUserData(ctx, mediaDB)
	require.Error(t, err)
	assert.Empty(t, rows)
}

// The read failure is reported, but nothing was lost: media.db is still there to retry
// from on the next boot, and startup is not stopped either way.
func TestBackfillMediaUserDataReportsUnreadableMediaDB(t *testing.T) {
	ctx := context.Background()

	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	require.NoError(t, mediaDB.Close())

	require.Error(t, backfillMediaUserData(ctx, &database.Database{MediaDB: mediaDB, UserDB: userDB}, nil))

	all, err := userDB.ListMediaUserData()
	require.NoError(t, err)
	assert.Empty(t, all)
}

// A caller that is about to destroy the rows' only copy needs to know they did not all
// land, so a failing upsert is reported rather than only logged — including when some
// rows did make it, which is the case a count alone would hide.
func TestBackfillMediaUserDataReportsFailedRescuedRows(t *testing.T) {
	t.Parallel()
	firstPath := filepath.Join("roms", "NES", "One.nes")
	secondPath := filepath.Join("roms", "NES", "Two.nes")

	// NewMockUserDBI already answers ListMediaUserData with no rows, which is the
	// state the backfill runs in.
	userDB := testhelpers.NewMockUserDBI()
	userDB.On("UpsertMediaUserData", mock.MatchedBy(func(row *database.MediaUserData) bool {
		return row.Path == firstPath
	})).Return(nil).Once()
	userDB.On("UpsertMediaUserData", mock.MatchedBy(func(row *database.MediaUserData) bool {
		return row.Path == secondPath
	})).Return(errors.New("no space left on device")).Once()

	err := backfillMediaUserData(context.Background(),
		&database.Database{UserDB: userDB},
		[]database.MediaUserData{
			{SystemID: "NES", Path: firstPath, IsFavorite: true},
			{SystemID: "NES", Path: secondPath, IsFavorite: true},
		})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "imported 1 of 2")

	userDB.AssertExpectations(t)
}

// Rescued rows with nowhere to go is a failure too: reporting success would tell the
// caller the rows are safe when they are about to be deleted.
func TestBackfillMediaUserDataWithoutUserDBRejectsRescuedRows(t *testing.T) {
	t.Parallel()
	err := backfillMediaUserData(context.Background(), &database.Database{},
		[]database.MediaUserData{{SystemID: "NES", Path: filepath.Join("roms", "NES", "One.nes")}})
	require.Error(t, err)
}

// Nothing to read and nothing rescued means there is no work to do.
func TestBackfillMediaUserDataWithoutMediaDB(t *testing.T) {
	ctx := context.Background()

	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	require.NoError(t, backfillMediaUserData(ctx, &database.Database{UserDB: userDB}, nil))

	all, err := userDB.ListMediaUserData()
	require.NoError(t, err)
	assert.Empty(t, all)
}

// The rescued rows are as stale as any other media.db copy, so the guard that makes
// UserDB authoritative applies to them too.
func TestBackfillMediaUserDataIgnoresRescuedRowsWhenPopulated(t *testing.T) {
	ctx := context.Background()

	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	manualPath := filepath.Join("roms", "SNES", "Manual.sfc")
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "SNES", Path: manualPath, IsFavorite: true,
	}))

	rescuedPath := filepath.Join("roms", "NES", "Rescued.nes")
	db := &database.Database{MediaDB: mediaDB, UserDB: userDB}
	require.NoError(t, backfillMediaUserData(ctx, db, []database.MediaUserData{{
		SystemID: "NES", Path: rescuedPath, IsFavorite: true,
	}}))

	_, found, err := userDB.GetMediaUserData("NES", rescuedPath)
	require.NoError(t, err)
	assert.False(t, found, "guard must skip rescued rows when UserDB already has data")
}
