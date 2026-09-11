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

func TestMediaHiddenMigrationPreservesExistingPreferences(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	path := filepath.Join("roms", "NES", "Favorite.nes")
	require.NoError(t, db.SetMediaUserFavorite("NES", path, true))
	require.NoError(t, db.SetMediaUserLauncherOverride("NES", path, "RetroArch"))

	// Restore the immediately preceding schema in this disposable test DB,
	// then exercise the normal migration runner with existing user data.
	_, err := db.sql.Load().ExecContext(t.Context(), `ALTER TABLE MediaUserData DROP COLUMN IsHidden;
		DELETE FROM goose_db_version WHERE version_id = 20260827120000;`)
	require.NoError(t, err)
	// Bypass the sidecar written for the newer schema by setupTempUserDB.
	require.NoError(t, sqlMigrateUp(db.sql.Load(), ""))
	row, found, err := db.GetMediaUserData("NES", path)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, row.IsFavorite)
	assert.Equal(t, "RetroArch", row.LauncherOverride)
	assert.False(t, row.IsHidden, "existing entries remain visible after upgrade")
}

func TestMediaHiddenPreferencePreservesOtherIntent(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	path := filepath.Join("roms", "NES", "Game.nes")
	require.NoError(t, db.SetMediaUserHidden("NES", path, true))
	require.NoError(t, db.SetMediaUserFavorite("NES", path, true))
	require.NoError(t, db.SetMediaUserLauncherOverride("NES", path, "RetroArch"))
	require.NoError(t, db.SetMediaUserFavorite("NES", path, false))
	require.NoError(t, db.SetMediaUserLauncherOverride("NES", path, ""))
	row, found, err := db.GetMediaUserData("NES", path)
	require.NoError(t, err)
	require.True(t, found, "hidden-only rows must not be pruned")
	assert.True(t, row.IsHidden)
	assert.False(t, row.IsFavorite)
	rows, err := db.ListMediaUserData()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.True(t, rows[0].IsHidden)

	before, _, err := db.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
	require.NoError(t, err)
	require.NoError(t, db.SetMediaUserHidden("NES", path, false))
	_, found, err = db.GetMediaUserData("NES", path)
	require.NoError(t, err)
	assert.False(t, found)
	after, _, err := db.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
	require.NoError(t, err)
	assert.NotEqual(t, before, after, "unhide must invalidate cached cursor totals even when row is deleted")

	require.NoError(t, db.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: path, IsHidden: true, IsFavorite: true,
	}))
	require.NoError(t, db.SetMediaUserHidden("NES", path, false))
	row, found, err = db.GetMediaUserData("NES", path)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, row.IsFavorite)
	assert.False(t, row.IsHidden)
}
