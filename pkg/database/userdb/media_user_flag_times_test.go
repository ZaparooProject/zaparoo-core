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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mediaUserFlagTimesVersion = 20261104120000

func flagTimes(t *testing.T, db *UserDB, flag database.MediaUserFlag) map[string]int64 {
	t.Helper()
	rows, err := db.ListMediaUserFlagTimes(flag)
	require.NoError(t, err)
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[row.SystemID+"|"+row.Path] = row.SetAt
	}
	return out
}

func TestMediaUserFlagTimesFollowTheFlag(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	require.NoError(t, db.SetMediaUserFlag("NES", "a.nes", database.MediaUserFlagFavorite, true))
	first := flagTimes(t, db, database.MediaUserFlagFavorite)
	require.Contains(t, first, "NES|a.nes")
	assert.Positive(t, first["NES|a.nes"])

	// Setting a flag again keeps the time it was first set.
	_, err := db.sql.Load().ExecContext(t.Context(),
		`update MediaUserData set FavoriteAt = 5 where Path = 'a.nes'`)
	require.NoError(t, err)
	require.NoError(t, db.SetMediaUserFlag("NES", "a.nes", database.MediaUserFlagFavorite, true))
	assert.Equal(t, int64(5), flagTimes(t, db, database.MediaUserFlagFavorite)["NES|a.nes"])

	// Another flag on the same row leaves the favorite time alone.
	require.NoError(t, db.SetMediaUserFlag("NES", "a.nes", database.MediaUserFlagPlayLater, true))
	assert.Equal(t, int64(5), flagTimes(t, db, database.MediaUserFlagFavorite)["NES|a.nes"])
	assert.Positive(t, flagTimes(t, db, database.MediaUserFlagPlayLater)["NES|a.nes"])

	// Clearing the flag clears its time, and setting it again dates it anew.
	require.NoError(t, db.SetMediaUserFlag("NES", "a.nes", database.MediaUserFlagFavorite, false))
	assert.NotContains(t, flagTimes(t, db, database.MediaUserFlagFavorite), "NES|a.nes")
	var at *int64
	require.NoError(t, db.sql.Load().QueryRowContext(t.Context(),
		`select FavoriteAt from MediaUserData where Path = 'a.nes'`).Scan(&at))
	assert.Nil(t, at)
	require.NoError(t, db.SetMediaUserFlag("NES", "a.nes", database.MediaUserFlagFavorite, true))
	assert.Greater(t, flagTimes(t, db, database.MediaUserFlagFavorite)["NES|a.nes"], int64(5))
}

func TestMediaUserFlagTimesClearWithAForbiddenPair(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	require.NoError(t, db.SetMediaUserFlag("NES", "a.nes", database.MediaUserFlagLiked, true))
	require.NoError(t, db.SetMediaUserFlag("NES", "a.nes", database.MediaUserFlagFavorite, true))
	require.NoError(t, db.SetMediaUserFlag("NES", "a.nes", database.MediaUserFlagDisliked, true))
	assert.Empty(t, flagTimes(t, db, database.MediaUserFlagLiked))
	assert.Empty(t, flagTimes(t, db, database.MediaUserFlagFavorite))
	assert.Contains(t, flagTimes(t, db, database.MediaUserFlagDisliked), "NES|a.nes")
	var liked, favorite *int64
	require.NoError(t, db.sql.Load().QueryRowContext(t.Context(),
		`select LikedAt, FavoriteAt from MediaUserData where Path = 'a.nes'`).Scan(&liked, &favorite))
	assert.Nil(t, liked)
	assert.Nil(t, favorite)
}

func TestMediaUserFlagTimesFromAWholeRowUpsert(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	require.NoError(t, db.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: "a.nes", IsFavorite: true, IsHidden: true,
	}))
	assert.Positive(t, flagTimes(t, db, database.MediaUserFlagFavorite)["NES|a.nes"])
	assert.Empty(t, flagTimes(t, db, database.MediaUserFlagLiked))
	assert.Zero(t, flagTimes(t, db, database.MediaUserFlagHidden)["NES|a.nes"], "hidden records no time")

	require.NoError(t, db.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: "a.nes", IsHidden: true,
	}))
	assert.Empty(t, flagTimes(t, db, database.MediaUserFlagFavorite))
}

// The migration adds nullable columns, so flags set before it stay set and
// report no time.
func TestMediaUserFlagTimesMigrationKeepsFlags(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	require.NoError(t, database.MigrateDownTo(db.sql.Load(), migrationFiles, "migrations", mediaUserFlagTimesVersion-1))
	_, err := db.sql.Load().ExecContext(t.Context(), `insert into MediaUserData
		(SystemID, Path, IsFavorite, IsLiked, LauncherOverride, CreatedAt, UpdatedAt)
		values ('NES', 'old.nes', 1, 1, '', 1, 1)`)
	require.NoError(t, err)
	require.NoError(t, sqlMigrateUp(db.sql.Load(), ""))

	assert.Equal(t, map[string]int64{"NES|old.nes": 0}, flagTimes(t, db, database.MediaUserFlagFavorite))
	assert.Equal(t, map[string]int64{"NES|old.nes": 0}, flagTimes(t, db, database.MediaUserFlagLiked))
	got, found, err := db.GetMediaUserData("NES", "old.nes")
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, got.IsFavorite)
	assert.True(t, got.IsLiked)
}

func TestGetMediaUserDataForPaths(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	require.NoError(t, db.SetMediaUserFlag("NES", "a.nes", database.MediaUserFlagLiked, true))
	require.NoError(t, db.SetMediaUserFlag("SNES", "a.nes", database.MediaUserFlagHidden, true))

	found, err := db.GetMediaUserDataForPaths([]database.MediaUserRef{
		{SystemID: "NES", Path: "a.nes"}, {SystemID: "NES", Path: "b.nes"},
	})
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.True(t, found[database.MediaUserRef{SystemID: "NES", Path: "a.nes"}].IsLiked)

	empty, err := db.GetMediaUserDataForPaths(nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}
