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

func TestHiddenDirectoriesAreOneRowPerSystemAndPath(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	defer cleanup()

	revision := func() string {
		value, _, err := db.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
		require.NoError(t, err)
		return value
	}
	folder := filepath.ToSlash(filepath.Join("roms", "Arcade", "_alternatives"))
	before := revision()

	// The path is stored canonically, so a trailing slash is the same folder.
	require.NoError(t, db.SetDirectoryHidden("Arcade", folder+"/", true))
	require.NoError(t, db.SetDirectoryHidden("Arcade", folder, true))
	require.NoError(t, db.SetDirectoryHidden("NES", folder, true))
	dirs, err := db.ListHiddenDirectories()
	require.NoError(t, err)
	assert.Equal(t, []database.HiddenDirectory{
		{SystemID: "Arcade", Path: folder},
		{SystemID: "NES", Path: folder},
	}, dirs)
	assert.NotEqual(t, before, revision(), "a hide invalidates listings like any preference")

	require.NoError(t, db.SetDirectoryHidden("Arcade", folder, false))
	dirs, err = db.ListHiddenDirectories()
	require.NoError(t, err)
	assert.Equal(t, []database.HiddenDirectory{{SystemID: "NES", Path: folder}}, dirs)

	// A folder hide is not a media preference row.
	rows, err := db.ListMediaUserData()
	require.NoError(t, err)
	assert.Empty(t, rows)
}
