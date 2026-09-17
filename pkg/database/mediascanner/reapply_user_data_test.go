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
		IsLiked: true, IsPlayLater: true, IsHidden: true,
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
	for _, want := range []tags.TagValue{tags.TagUserHidden, tags.TagUserLiked, tags.TagUserPlayLater} {
		assert.True(t, mediaHasUserTag(ctx, t, mediaDB, "NES", bothPath, want), "%s", want)
	}
	assert.False(t, mediaHasUserTag(ctx, t, mediaDB, "NES", bothPath, tags.TagUserDisliked))

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
	return mediaHasUserTag(ctx, t, db, systemID, path, tags.TagUserFavorite)
}

func mediaHasUserTag(
	ctx context.Context, t *testing.T, db database.MediaDBI, systemID, path string, value tags.TagValue,
) bool {
	t.Helper()
	tagInfos, err := db.GetMediaTagsByMediaDBID(ctx, mediaDBIDForPath(ctx, t, db, systemID, path))
	require.NoError(t, err)
	for _, ti := range tagInfos {
		if ti.Type == string(tags.TagTypeUser) && ti.Tag == string(value) {
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

// countingMediaDB counts tag writes and exposes the batch method.
type countingMediaDB struct {
	database.MediaDBI
	batcher     database.MediaTagBatchUpdater
	batchCalls  int
	batchEdits  int
	singleCalls int
}

func (c *countingMediaDB) UpdateMediaTags(
	ctx context.Context, mediaDBID int64, remove, add []database.MediaTagRef,
) error {
	c.singleCalls++
	return c.MediaDBI.UpdateMediaTags(ctx, mediaDBID, remove, add) //nolint:wrapcheck // test passthrough
}

func (c *countingMediaDB) UpdateMediaTagsBatch(ctx context.Context, updates []database.MediaTagUpdate) error {
	c.batchCalls++
	c.batchEdits += len(updates)
	return c.batcher.UpdateMediaTagsBatch(ctx, updates) //nolint:wrapcheck // test passthrough
}

// singleOnlyMediaDB hides the batch method, as a MediaDBI without batch
// support would.
type singleOnlyMediaDB struct {
	database.MediaDBI
	singleCalls int
}

func (s *singleOnlyMediaDB) UpdateMediaTags(
	ctx context.Context, mediaDBID int64, remove, add []database.MediaTagRef,
) error {
	s.singleCalls++
	return s.MediaDBI.UpdateMediaTags(ctx, mediaDBID, remove, add) //nolint:wrapcheck // test passthrough
}

// Re-apply writes the missing tags in one batch and nothing when they are
// already in place, which is the incremental reindex case.
func TestReapplyMediaUserDataWritesOnlyMissingTags(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	paths := []string{
		filepath.Join("roms", "NES", "One.nes"),
		filepath.Join("roms", "NES", "Two.nes"),
		filepath.Join("roms", "NES", "Three.nes"),
	}
	indexMediaPaths(t, mediaDB, "NES", paths...)
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: paths[0], IsLiked: true, IsPlayLater: true,
	}))
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: paths[1], IsFavorite: true, IsHidden: true,
	}))
	require.NoError(t, userDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: paths[2], LauncherOverride: "RetroArch",
	}))

	counting := &countingMediaDB{MediaDBI: mediaDB, batcher: mediaDB}
	applied, err := reapplyMediaUserData(ctx, counting, userDB)
	require.NoError(t, err)
	assert.Equal(t, 3, applied)
	assert.Equal(t, 1, counting.batchCalls)
	assert.Equal(t, 2, counting.batchEdits, "only files with flags need tags")
	assert.Zero(t, counting.singleCalls)
	assert.True(t, mediaHasUserTag(ctx, t, mediaDB, "NES", paths[0], tags.TagUserPlayLater))
	assert.True(t, mediaHasFavorite(ctx, t, mediaDB, "NES", paths[1]))
	assert.True(t, mediaHasUserTag(ctx, t, mediaDB, "NES", paths[1], tags.TagUserHidden))

	counting = &countingMediaDB{MediaDBI: mediaDB, batcher: mediaDB}
	applied, err = reapplyMediaUserData(ctx, counting, userDB)
	require.NoError(t, err)
	assert.Equal(t, 3, applied)
	assert.Zero(t, counting.batchCalls, "tags already in place are not rewritten")

	// A tag lost from media.db is the only one written back.
	oneID := mediaDBIDForPath(ctx, t, mediaDB, "NES", paths[0])
	require.NoError(t, mediaDB.UpdateMediaTags(ctx, oneID, []database.MediaTagRef{
		{Type: string(tags.TagTypeUser), Tag: string(tags.TagUserPlayLater)},
	}, nil))
	counting = &countingMediaDB{MediaDBI: mediaDB, batcher: mediaDB}
	_, err = reapplyMediaUserData(ctx, counting, userDB)
	require.NoError(t, err)
	assert.Equal(t, 1, counting.batchEdits)
	assert.True(t, mediaHasUserTag(ctx, t, mediaDB, "NES", paths[0], tags.TagUserPlayLater))
}

func TestReapplyMediaUserDataWithoutBatchSupport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	paths := []string{filepath.Join("roms", "NES", "One.nes"), filepath.Join("roms", "NES", "Two.nes")}
	indexMediaPaths(t, mediaDB, "NES", paths...)
	for _, path := range paths {
		require.NoError(t, userDB.SetMediaUserFlag("NES", path, database.MediaUserFlagDisliked, true))
	}

	single := &singleOnlyMediaDB{MediaDBI: mediaDB}
	_, err := reapplyMediaUserData(ctx, single, userDB)
	require.NoError(t, err)
	assert.Equal(t, 2, single.singleCalls)
	for _, path := range paths {
		assert.True(t, mediaHasUserTag(ctx, t, mediaDB, "NES", path, tags.TagUserDisliked))
	}
}
