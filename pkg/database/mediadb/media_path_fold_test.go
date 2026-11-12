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

package mediadb_test

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A case-insensitive filesystem opens a file under any spelling of its path,
// and the index holds the one spelling the disk has.
func TestFindMediaPathIgnoringCase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	scantest.IndexMediaPaths(t, mediaDB, "PSX",
		"/games/PSX/Crash Bandicoot (USA)/Crash Bandicoot (USA).chd",
		"/games/PSX/Twin/Game.chd",
		"/games/PSX/twin/game.chd",
	)
	scantest.IndexMediaPaths(t, mediaDB, "NES", "/games/NES/Other.nes")
	psx, err := mediaDB.FindSystemBySystemID("PSX")
	require.NoError(t, err)

	spelled, found, err := mediaDB.FindMediaPathIgnoringCase(
		ctx, psx.DBID, "/games/psx/crash bandicoot (usa)/CRASH BANDICOOT (usa).chd")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "/games/PSX/Crash Bandicoot (USA)/Crash Bandicoot (USA).chd", spelled)

	_, found, err = mediaDB.FindMediaPathIgnoringCase(ctx, psx.DBID, "/games/psx/missing.chd")
	require.NoError(t, err)
	assert.False(t, found)

	_, found, err = mediaDB.FindMediaPathIgnoringCase(ctx, psx.DBID, "/games/psx/TWIN/GAME.chd")
	require.NoError(t, err)
	assert.False(t, found, "two files that differ only in case leave nothing to choose between")

	_, found, err = mediaDB.FindMediaPathIgnoringCase(ctx, psx.DBID, "/games/nes/other.nes")
	require.NoError(t, err)
	assert.False(t, found, "another system's file is not this system's")

	spelled, found, err = mediaDB.FindMediaPathIgnoringCase(ctx, 0, "/games/nes/other.nes")
	require.NoError(t, err)
	require.True(t, found, "no system given means any system")
	assert.Equal(t, "/games/NES/Other.nes", spelled)
}

// A virtual path is indexed as scheme://id/name, and a launch often names the
// ID alone.
func TestFindMediaPathByPrefix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	scantest.IndexMediaPaths(t, mediaDB, "PC",
		"steam://620/Portal%202",
		"steam://6200/Ghost%20Master",
		"steam://70/Half-Life",
		"steam://70/Half-Life%20Again",
	)

	path, found, err := mediaDB.FindMediaPathByPrefix(ctx, "steam://620/")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "steam://620/Portal%202", path)

	_, found, err = mediaDB.FindMediaPathByPrefix(ctx, "steam://999/")
	require.NoError(t, err)
	assert.False(t, found)

	_, found, err = mediaDB.FindMediaPathByPrefix(ctx, "steam://70/")
	require.NoError(t, err)
	assert.False(t, found, "two entries under one ID leave nothing to choose between")
}
