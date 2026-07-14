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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReapplyMediaUserData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	favPath := filepath.Join("roms", "NES", "Fav.nes")
	overridePath := filepath.Join("roms", "NES", "Override.nes")
	bothPath := filepath.Join("roms", "NES", "Both.nes")
	plainPath := filepath.Join("roms", "NES", "Plain.nes")

	// Build a freshly indexed media.db with no user data yet.
	indexMediaPaths(t, mediaDB, "NES", favPath, overridePath, bothPath, plainPath)

	// Seed UserDB truth, including orphans whose path/system is not indexed.
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: favPath, IsFavorite: true,
	}))
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: overridePath, LauncherOverride: "RetroArch",
	}))
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: bothPath, IsFavorite: true, LauncherOverride: "RetroArch",
	}))
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: filepath.Join("roms", "NES", "Ghost.nes"), IsFavorite: true,
	}))
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "SNES", Path: filepath.Join("roms", "SNES", "Ghost.sfc"), IsFavorite: true,
	}))

	applied, err := reapplyMediaUserData(ctx, mediaDB, userDB)
	require.NoError(t, err)
	assert.Equal(t, 3, applied, "only indexed rows are materialized; orphans are skipped")

	assert.True(t, mediaHasFavorite(ctx, t, mediaDB, "NES", favPath))
	assert.Empty(t, mediaLauncherOverride(ctx, t, mediaDB, "NES", favPath))

	assert.False(t, mediaHasFavorite(ctx, t, mediaDB, "NES", overridePath))
	assert.Equal(t, "RetroArch", mediaLauncherOverride(ctx, t, mediaDB, "NES", overridePath))

	assert.True(t, mediaHasFavorite(ctx, t, mediaDB, "NES", bothPath))
	assert.Equal(t, "RetroArch", mediaLauncherOverride(ctx, t, mediaDB, "NES", bothPath))

	assert.False(t, mediaHasFavorite(ctx, t, mediaDB, "NES", plainPath))
	assert.Empty(t, mediaLauncherOverride(ctx, t, mediaDB, "NES", plainPath))

	// Re-running is idempotent: no duplicate tags, no error.
	applied2, err := reapplyMediaUserData(ctx, mediaDB, userDB)
	require.NoError(t, err)
	assert.Equal(t, 3, applied2)
	assert.True(t, mediaHasFavorite(ctx, t, mediaDB, "NES", favPath))
	assert.Equal(t, "RetroArch", mediaLauncherOverride(ctx, t, mediaDB, "NES", bothPath))
}

func TestReapplyMediaUserDataEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	applied, err := reapplyMediaUserData(ctx, mediaDB, userDB)
	require.NoError(t, err)
	assert.Equal(t, 0, applied)
}

func mediaDBIDForPath(
	ctx context.Context, t *testing.T, db database.MediaDBI, systemID, path string,
) int64 {
	t.Helper()
	system, err := db.FindSystemBySystemID(systemID)
	require.NoError(t, err)
	media, err := db.FindMediaBySystemAndPath(ctx, system.DBID, pathutil.CanonicalMediaPath(path))
	require.NoError(t, err)
	require.NotNil(t, media)
	return media.DBID
}

func mediaHasFavorite(ctx context.Context, t *testing.T, db database.MediaDBI, systemID, path string) bool {
	t.Helper()
	tagInfos, err := db.GetMediaTagsByMediaDBID(ctx, mediaDBIDForPath(ctx, t, db, systemID, path))
	require.NoError(t, err)
	for _, ti := range tagInfos {
		if ti.Type == string(tags.TagTypeUser) && ti.Tag == string(tags.TagUserFavorite) {
			return true
		}
	}
	return false
}

func mediaLauncherOverride(ctx context.Context, t *testing.T, db database.MediaDBI, systemID, path string) string {
	t.Helper()
	props, err := db.GetMediaPropertyMetadata(ctx, mediaDBIDForPath(ctx, t, db, systemID, path))
	require.NoError(t, err)
	want := tags.PropertyTypeTag(tags.TagPropertyLauncherOverride)
	for _, p := range props {
		if p.TypeTag == want {
			return p.Text
		}
	}
	return ""
}
