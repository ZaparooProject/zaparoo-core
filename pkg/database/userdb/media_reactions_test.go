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

package userdb

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The reactions migration adds columns with defaults, so rows written by the
// previous schema keep every preference they held.
func TestMediaReactionsMigrationPreservesExistingPreferences(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	path := filepath.Join("roms", "NES", "Favorite.nes")
	require.NoError(t, db.SetMediaUserFavorite("NES", path, true))
	require.NoError(t, db.SetMediaUserHidden("NES", path, true))
	require.NoError(t, db.SetMediaUserLauncherOverride("NES", path, "RetroArch"))

	require.NoError(t, database.MigrateDownTo(db.sql.Load(), migrationFiles, "migrations", 20260914100000-1))
	require.NoError(t, sqlMigrateUp(db.sql.Load(), ""))

	row, found, err := db.GetMediaUserData("NES", path)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, row.IsFavorite)
	assert.True(t, row.IsHidden)
	assert.Equal(t, "RetroArch", row.LauncherOverride)
	assert.False(t, row.IsLiked)
	assert.False(t, row.IsDisliked)
	assert.False(t, row.IsPlayLater)
	assert.Empty(t, row.Slug)
}

func TestSetMediaUserFlagExclusivity(t *testing.T) {
	t.Parallel()
	type step struct {
		flag  database.MediaUserFlag
		value bool
	}
	tests := []struct {
		name  string
		steps []step
		want  database.MediaUserData
	}{
		{
			name:  "liked clears disliked",
			steps: []step{{database.MediaUserFlagDisliked, true}, {database.MediaUserFlagLiked, true}},
			want:  database.MediaUserData{IsLiked: true},
		},
		{
			name: "disliked clears liked and favorite",
			steps: []step{
				{database.MediaUserFlagLiked, true},
				{database.MediaUserFlagFavorite, true},
				{database.MediaUserFlagDisliked, true},
			},
			want: database.MediaUserData{IsDisliked: true},
		},
		{
			name:  "favorite clears disliked",
			steps: []step{{database.MediaUserFlagDisliked, true}, {database.MediaUserFlagFavorite, true}},
			want:  database.MediaUserData{IsFavorite: true},
		},
		{
			name: "play later and hidden disturb nothing",
			steps: []step{
				{database.MediaUserFlagFavorite, true},
				{database.MediaUserFlagLiked, true},
				{database.MediaUserFlagPlayLater, true},
				{database.MediaUserFlagHidden, true},
			},
			want: database.MediaUserData{IsFavorite: true, IsLiked: true, IsPlayLater: true, IsHidden: true},
		},
		{
			name: "clearing a flag does not touch its pair",
			steps: []step{
				{database.MediaUserFlagLiked, true},
				{database.MediaUserFlagFavorite, true},
				{database.MediaUserFlagLiked, false},
			},
			want: database.MediaUserData{IsFavorite: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db, cleanup := setupTempUserDB(t)
			t.Cleanup(cleanup)
			path := filepath.Join("roms", "NES", "Game.nes")
			for _, s := range tc.steps {
				require.NoError(t, db.SetMediaUserFlag("NES", path, s.flag, s.value))
			}
			row, found, err := db.GetMediaUserData("NES", path)
			require.NoError(t, err)
			require.True(t, found)
			for _, flag := range database.MediaUserFlags {
				assert.Equal(t, tc.want.Flag(flag), row.Flag(flag), "flag %s", flag)
			}
		})
	}
}

func TestSetMediaUserFlagPrunesEmptyRow(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	path := filepath.Join("roms", "NES", "Game.nes")
	require.NoError(t, db.SetMediaUserFlag("NES", path, database.MediaUserFlagPlayLater, true))
	require.NoError(t, db.SetMediaUserFlag("NES", path, database.MediaUserFlagLiked, true))
	require.NoError(t, db.SetMediaUserFlag("NES", path, database.MediaUserFlagPlayLater, false))
	_, found, err := db.GetMediaUserData("NES", path)
	require.NoError(t, err)
	assert.True(t, found, "liked keeps the row alive")
	require.NoError(t, db.SetMediaUserFlag("NES", path, database.MediaUserFlagLiked, false))
	_, found, err = db.GetMediaUserData("NES", path)
	require.NoError(t, err)
	assert.False(t, found, "a row with no flag and no override is pruned")

	require.Error(t, db.SetMediaUserFlag("NES", path, database.MediaUserFlag("bogus"), true))
}

func TestUpsertMediaUserDataRefusesForbiddenPairs(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	path := filepath.Join("roms", "NES", "Game.nes")
	err := db.UpsertMediaUserData(&database.MediaUserData{SystemID: "NES", Path: path, IsLiked: true, IsDisliked: true})
	require.ErrorIs(t, err, database.ErrMediaUserFlagConflict)
	err = db.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: path, IsFavorite: true, IsDisliked: true,
	})
	require.ErrorIs(t, err, database.ErrMediaUserFlagConflict)

	require.NoError(t, db.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: path, IsDisliked: true, IsPlayLater: true, Slug: "game", MediaName: "Game",
	}))
	rows, err := db.ListMediaUserData()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.True(t, rows[0].IsDisliked)
	assert.True(t, rows[0].IsPlayLater)
	assert.Equal(t, "game", rows[0].Slug)

	// A later upsert without a slug keeps the stored one.
	require.NoError(t, db.UpsertMediaUserData(&database.MediaUserData{SystemID: "NES", Path: path, IsLiked: true}))
	row, found, err := db.GetMediaUserData("NES", path)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, row.IsLiked)
	assert.False(t, row.IsDisliked)
	assert.Equal(t, "game", row.Slug)
}

func TestSetMediaUserSnapshotStoresSlug(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	path := filepath.Join("roms", "SNES", "Super Metroid (USA).sfc")
	require.NoError(t, db.SetMediaUserFlag("SNES", path, database.MediaUserFlagLiked, true))
	require.NoError(t, db.SetMediaUserSnapshot("SNES", path, "Super Metroid", "supermetroid", []string{"region:us"}))
	row, found, err := db.GetMediaUserData("SNES", path)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "supermetroid", row.Slug)
	assert.Equal(t, "Super Metroid", row.MediaName)
}

// Full-row writes change listings too, so they must invalidate browse cursors
// like the column-scoped writers do, including when the row is pruned, and a
// refused row must leave the revision alone.
func TestFullRowMediaUserDataWritesAdvanceRevision(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	path := filepath.Join("roms", "NES", "Game.nes")
	revision := func() string {
		t.Helper()
		value, _, err := db.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
		require.NoError(t, err)
		return value
	}

	before := revision()
	require.NoError(t, db.UpsertMediaUserData(&database.MediaUserData{SystemID: "NES", Path: path, IsFavorite: true}))
	afterInsert := revision()
	assert.NotEqual(t, before, afterInsert, "an imported favorite must invalidate cursors")

	require.ErrorIs(t, db.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: path, IsLiked: true, IsDisliked: true,
	}), database.ErrMediaUserFlagConflict)
	assert.Equal(t, afterInsert, revision(), "a refused row writes nothing")

	require.NoError(t, db.UpsertMediaUserData(&database.MediaUserData{SystemID: "NES", Path: path}))
	_, found, err := db.GetMediaUserData("NES", path)
	require.NoError(t, err)
	assert.False(t, found, "a row with no intent is pruned")
	afterPrune := revision()
	assert.NotEqual(t, afterInsert, afterPrune)

	require.NoError(t, db.SetMediaUserFlag("NES", path, database.MediaUserFlagHidden, true))
	afterHide := revision()
	require.NoError(t, db.DeleteMediaUserData("NES", path))
	assert.NotEqual(t, afterHide, revision(), "deleting a hidden row must invalidate cursors")
}
