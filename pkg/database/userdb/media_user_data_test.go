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
	"sync"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediaUserDataCRUD(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	path := filepath.Join("roms", "NES", "Game.nes")

	// Missing row reports not found.
	_, found, err := userDB.GetMediaUserData("NES", path)
	require.NoError(t, err)
	assert.False(t, found)

	// Insert.
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID:   "NES",
		Path:       path,
		IsFavorite: true,
	}))
	got, found, err := userDB.GetMediaUserData("NES", path)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, got.IsFavorite)
	assert.Empty(t, got.LauncherOverride)
	assert.NotZero(t, got.CreatedAt)
	assert.NotZero(t, got.UpdatedAt)
	createdAt := got.CreatedAt

	// Upsert overwrites fields by (SystemID, Path) and preserves CreatedAt.
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID:         "NES",
		Path:             path,
		IsFavorite:       false,
		LauncherOverride: "RetroArch",
	}))
	got, found, err = userDB.GetMediaUserData("NES", path)
	require.NoError(t, err)
	require.True(t, found)
	assert.False(t, got.IsFavorite)
	assert.Equal(t, "RetroArch", got.LauncherOverride)
	assert.Equal(t, createdAt, got.CreatedAt, "CreatedAt must be preserved across upserts")

	// Delete.
	require.NoError(t, userDB.DeleteMediaUserData("NES", path))
	_, found, err = userDB.GetMediaUserData("NES", path)
	require.NoError(t, err)
	assert.False(t, found)

	// Deleting a missing row is not an error.
	require.NoError(t, userDB.DeleteMediaUserData("NES", path))
}

func TestMediaUserDataUniquePerSystemAndPath(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	path := filepath.Join("roms", "NES", "Game.nes")
	// Same path under two systems are distinct rows.
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: path, IsFavorite: true,
	}))
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "SNES", Path: path, LauncherOverride: "RetroArch",
	}))

	nes, found, err := userDB.GetMediaUserData("NES", path)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, nes.IsFavorite)

	snes, found, err := userDB.GetMediaUserData("SNES", path)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "RetroArch", snes.LauncherOverride)
}

func TestSetMediaUserFavoriteColumnScoped(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	path := filepath.Join("roms", "NES", "Game.nes")

	// Favouriting an absent path creates the row.
	require.NoError(t, userDB.SetMediaUserFavorite("NES", path, true))
	got, found, err := userDB.GetMediaUserData("NES", path)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, got.IsFavorite)

	// An override on the same row must survive a favourite toggle...
	require.NoError(t, userDB.SetMediaUserLauncherOverride("NES", path, "RetroArch"))
	require.NoError(t, userDB.SetMediaUserFavorite("NES", path, false))
	got, found, err = userDB.GetMediaUserData("NES", path)
	require.NoError(t, err)
	require.True(t, found, "row with an override survives clearing the favourite")
	assert.False(t, got.IsFavorite)
	assert.Equal(t, "RetroArch", got.LauncherOverride)

	// ...and the favourite must survive clearing the override.
	require.NoError(t, userDB.SetMediaUserFavorite("NES", path, true))
	require.NoError(t, userDB.SetMediaUserLauncherOverride("NES", path, ""))
	got, found, err = userDB.GetMediaUserData("NES", path)
	require.NoError(t, err)
	require.True(t, found, "row with a favourite survives clearing the override")
	assert.True(t, got.IsFavorite)
	assert.Empty(t, got.LauncherOverride)

	// Clearing both intents deletes the row.
	require.NoError(t, userDB.SetMediaUserFavorite("NES", path, false))
	_, found, err = userDB.GetMediaUserData("NES", path)
	require.NoError(t, err)
	assert.False(t, found, "row with no favourite and no override is pruned")
}

// TestSetMediaUserDataConcurrentColumnsDoNotClobber runs a favourite write and a
// launcher-override write against the same path concurrently. Because each write
// is column-scoped, the final row must carry BOTH intents — a read-modify-write
// implementation would lose one. Run under -race to catch interleaving bugs.
func TestSetMediaUserDataConcurrentColumnsDoNotClobber(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	for i := range 50 {
		path := filepath.Join("roms", "NES", "Game.nes")
		require.NoError(t, userDB.DeleteMediaUserData("NES", path))

		var wg sync.WaitGroup
		wg.Add(2)
		var favErr, ovrErr error
		go func() {
			defer wg.Done()
			favErr = userDB.SetMediaUserFavorite("NES", path, true)
		}()
		go func() {
			defer wg.Done()
			ovrErr = userDB.SetMediaUserLauncherOverride("NES", path, "RetroArch")
		}()
		wg.Wait()
		require.NoError(t, favErr)
		require.NoError(t, ovrErr)

		got, found, err := userDB.GetMediaUserData("NES", path)
		require.NoError(t, err)
		require.True(t, found, "iteration %d: row must exist", i)
		assert.True(t, got.IsFavorite, "iteration %d: favourite must survive concurrent override write", i)
		assert.Equal(t, "RetroArch", got.LauncherOverride,
			"iteration %d: override must survive concurrent favourite write", i)
	}
}

func TestListMediaUserData(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	empty, err := userDB.ListMediaUserData()
	require.NoError(t, err)
	assert.Empty(t, empty)

	for _, name := range []string{"A.nes", "B.nes", "C.nes"} {
		require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
			SystemID:   "NES",
			Path:       filepath.Join("roms", "NES", name),
			IsFavorite: true,
		}))
	}

	all, err := userDB.ListMediaUserData()
	require.NoError(t, err)
	assert.Len(t, all, 3)
}
