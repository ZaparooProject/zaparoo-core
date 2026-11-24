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
	"context"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func batchTestPaths() (disc1, disc2 string) {
	dir := filepath.Join("roms", "PSX", "Game")
	return filepath.Join(dir, "Game (Disc 1).chd"), filepath.Join(dir, "Game (Disc 2).chd")
}

func TestSetMediaUserFlagsWritesEveryPathOnce(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	disc1, disc2 := batchTestPaths()
	revision := func() string {
		t.Helper()
		value, _, err := db.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
		require.NoError(t, err)
		return value
	}

	require.NoError(t, db.SetMediaUserFlag("PSX", disc1, database.MediaUserFlagDisliked, true))
	before := revision()
	require.NoError(t, db.SetMediaUserFlags([]database.MediaUserFlagWrite{
		{SystemID: "PSX", Path: disc1, Flag: database.MediaUserFlagDisliked, Value: false},
		{SystemID: "PSX", Path: disc2, Flag: database.MediaUserFlagFavorite, Value: true},
		{SystemID: "PSX", Path: disc2, Flag: database.MediaUserFlagPlayLater, Value: true},
	}))
	after := revision()
	assert.NotEqual(t, before, after)

	_, found, err := db.GetMediaUserData("PSX", disc1)
	require.NoError(t, err)
	assert.False(t, found, "a row left with no intent is pruned")
	row, found, err := db.GetMediaUserData("PSX", disc2)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, row.IsFavorite)
	assert.True(t, row.IsPlayLater)

	// The revision moves by exactly one for the whole batch.
	require.NoError(t, db.SetMediaUserFlag("PSX", disc1, database.MediaUserFlagLiked, true))
	single := revision()
	require.NoError(t, db.SetMediaUserFlags([]database.MediaUserFlagWrite{
		{SystemID: "PSX", Path: disc1, Flag: database.MediaUserFlagLiked, Value: false},
		{SystemID: "PSX", Path: disc2, Flag: database.MediaUserFlagPlayLater, Value: false},
	}))
	assert.Equal(t, nextRevision(t, single), revision())

	// An empty batch writes nothing.
	unchanged := revision()
	require.NoError(t, db.SetMediaUserFlags(nil))
	assert.Equal(t, unchanged, revision())
}

func nextRevision(t *testing.T, value string) string {
	t.Helper()
	n, err := strconv.Atoi(value)
	require.NoError(t, err, "revision %q is a counter", value)
	return strconv.Itoa(n + 1)
}

func TestSetMediaUserFlagsFollowsTheSingleWriteRules(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	disc1, _ := batchTestPaths()

	// Disliking clears like and favorite on the same row, as one write does.
	require.NoError(t, db.SetMediaUserFlags([]database.MediaUserFlagWrite{
		{SystemID: "PSX", Path: disc1, Flag: database.MediaUserFlagFavorite, Value: true},
		{SystemID: "PSX", Path: disc1, Flag: database.MediaUserFlagLiked, Value: true},
	}))
	require.NoError(t, db.SetMediaUserFlags([]database.MediaUserFlagWrite{
		{SystemID: "PSX", Path: disc1, Flag: database.MediaUserFlagDisliked, Value: true},
	}))
	row, _, err := db.GetMediaUserData("PSX", disc1)
	require.NoError(t, err)
	assert.True(t, row.IsDisliked)
	assert.False(t, row.IsFavorite)
	assert.False(t, row.IsLiked)

	// Setting a flag already set keeps when it was first set; setting it
	// again after a clear starts over.
	require.NoError(t, db.SetMediaUserFlag("PSX", disc1, database.MediaUserFlagPlayLater, true))
	first := flagTimes(t, db, database.MediaUserFlagPlayLater)["PSX|"+disc1]
	require.NotZero(t, first)
	_, err = db.sql.Load().ExecContext(context.Background(),
		`update MediaUserData set PlayLaterAt = 1 where SystemID = 'PSX' and Path = ?`, disc1)
	require.NoError(t, err)
	require.NoError(t, db.SetMediaUserFlags([]database.MediaUserFlagWrite{
		{SystemID: "PSX", Path: disc1, Flag: database.MediaUserFlagPlayLater, Value: true},
	}))
	assert.Equal(t, int64(1), flagTimes(t, db, database.MediaUserFlagPlayLater)["PSX|"+disc1])
	require.NoError(t, db.SetMediaUserFlags([]database.MediaUserFlagWrite{
		{SystemID: "PSX", Path: disc1, Flag: database.MediaUserFlagPlayLater, Value: false},
	}))
	require.NoError(t, db.SetMediaUserFlags([]database.MediaUserFlagWrite{
		{SystemID: "PSX", Path: disc1, Flag: database.MediaUserFlagPlayLater, Value: true},
	}))
	assert.Greater(t, flagTimes(t, db, database.MediaUserFlagPlayLater)["PSX|"+disc1], int64(1))
}

func TestSetMediaUserFlagsKeepsAllOrNothing(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	disc1, disc2 := batchTestPaths()

	require.NoError(t, db.SetMediaUserFlag("PSX", disc1, database.MediaUserFlagFavorite, true))
	revision, _, err := db.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
	require.NoError(t, err)

	// An unknown flag is refused before anything is written.
	err = db.SetMediaUserFlags([]database.MediaUserFlagWrite{
		{SystemID: "PSX", Path: disc1, Flag: database.MediaUserFlagFavorite, Value: false},
		{SystemID: "PSX", Path: disc2, Flag: "bogus", Value: true},
	})
	require.Error(t, err)

	// A write that fails part way through the transaction undoes the ones
	// before it.
	_, err = db.sql.Load().ExecContext(context.Background(), `
		create trigger fail_disc2 before insert on MediaUserData
		when new.Path = '`+disc2+`'
		begin select raise(abort, 'refused'); end;`)
	require.NoError(t, err)
	err = db.SetMediaUserFlags([]database.MediaUserFlagWrite{
		{SystemID: "PSX", Path: disc1, Flag: database.MediaUserFlagFavorite, Value: false},
		{SystemID: "PSX", Path: disc2, Flag: database.MediaUserFlagFavorite, Value: true},
	})
	require.Error(t, err)

	row, found, err := db.GetMediaUserData("PSX", disc1)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, row.IsFavorite, "the first write is rolled back with the second")
	_, found, err = db.GetMediaUserData("PSX", disc2)
	require.NoError(t, err)
	assert.False(t, found)
	after, _, err := db.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
	require.NoError(t, err)
	assert.Equal(t, revision, after, "a failed batch leaves the revision alone")
}
