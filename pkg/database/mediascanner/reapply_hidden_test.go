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

package mediascanner

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReapplyHiddenAcrossRebuild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	userDB, cleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	path := filepath.Join("roms", "NES", "Hidden.nes")
	moved := filepath.Join("roms", "NES", "Moved.nes")
	require.NoError(t, userDB.SetMediaUserHidden("NES", path, true))
	require.NoError(t, userDB.SetMediaUserFavorite("NES", path, true))

	for _, indexedPath := range []string{path, moved, path} {
		// A new MediaDB stands in for a full rebuild; identity is still the
		// canonical system/path pair, exactly like favorites.
		mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
		indexMediaPaths(t, mediaDB, "NES", indexedPath)
		for range 2 {
			_, err := reapplyMediaUserData(ctx, mediaDB, userDB)
			require.NoError(t, err)
			id := mediaDBIDForPath(ctx, t, mediaDB, "NES", indexedPath)
			tags, err := mediaDB.GetMediaTagsByMediaDBID(ctx, id)
			require.NoError(t, err)
			if indexedPath == path {
				assert.Contains(t, tags, database.TagInfo{Type: "user", Tag: "hidden"})
				assert.True(t, mediaHasFavorite(ctx, t, mediaDB, "NES", path))
			} else {
				assert.NotContains(t, tags, database.TagInfo{Type: "user", Tag: "hidden"})
				assert.False(t, mediaHasFavorite(ctx, t, mediaDB, "NES", moved))
			}
		}
		mediaCleanup()
	}
	row, found, err := userDB.GetMediaUserData("NES", path)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, row.IsHidden)
}
