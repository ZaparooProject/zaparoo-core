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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A Windows media path has no leading slash, but the browse cache hangs every
// filesystem directory under "/", so C:/roms/ is stored as /C:/roms/. The root
// listing looked the root up in the platform's own form and never found it,
// reporting every filesystem root on Windows as empty and dropping it from
// media.browse. The cache is pure string handling, so the same keys reproduce
// the miss on any OS.
func TestBrowseRootCountsFindWindowsRootsInTheCache(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	insertSystemMedia(t, mediaDB, system, "Alpha", "C:/roms/NES/Alpha.nes")
	insertSystemMedia(t, mediaDB, system, "Beta", "C:/roms/NES/Beta.nes")
	insertSystemMedia(t, mediaDB, system, "Gamma", "C:/roms/NES/Folder/Gamma.nes")
	require.NoError(t, mediaDB.PopulateBrowseCache(ctx))
	ready, err := sqlBrowseCacheReady(ctx, mediaDB.sql.Load())
	require.NoError(t, err)
	require.True(t, ready, "the cached root counts are what this test covers")

	// Roots arrive as the platform reports them: cleaned forward-slash form
	// from most platforms, native backslashes from Windows.
	for _, root := range []string{"C:/roms", `C:\roms`, "C:/roms/NES"} {
		counts, countErr := mediaDB.BrowseRootCounts(ctx, []string{root}, false)
		require.NoError(t, countErr)
		require.NotNil(t, counts[root], "root %q must be answered from the cache", root)
		assert.Equal(t, 3, *counts[root], "root %q", root)
	}

	// Hidden rows are subtracted by Media.Path prefix, which is the
	// forward-slash form whatever the root looked like.
	hideMediaPaths(t, mediaDB, "C:/roms/NES/Folder/Gamma.nes")
	for _, root := range []string{"C:/roms", `C:\roms`} {
		counts, countErr := mediaDB.BrowseRootCounts(ctx, []string{root}, true)
		require.NoError(t, countErr)
		require.NotNil(t, counts[root])
		assert.Equal(t, 2, *counts[root], "root %q with one hidden file", root)
	}

	// The same key mismatch hid cached directory listings behind the media
	// fallback; the cache must answer for the Windows prefix directly.
	_, found, err := sqlBrowseDirectoriesFromCache(ctx, mediaDB.sql.Load(), database.BrowseDirectoriesOptions{
		PathPrefix: "C:/roms/NES/",
	})
	require.NoError(t, err)
	assert.True(t, found, "the cache must know the Windows directory")
}
