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

package database_test

import (
	"context"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediaUserFlagsMatchMutableUserTags(t *testing.T) {
	t.Parallel()
	require.Len(t, database.MediaUserFlags, len(tags.MutableUserTags))
	for i, flag := range database.MediaUserFlags {
		assert.Equal(t, string(tags.MutableUserTags[i]), string(flag))
	}
}

func TestMediaUserDataFlagHelpers(t *testing.T) {
	t.Parallel()
	var row database.MediaUserData
	assert.False(t, row.HasIntent())
	assert.False(t, row.Flag(database.MediaUserFlag("bogus")))
	require.NoError(t, row.ValidateFlags())

	row.LauncherOverride = "RetroArch"
	assert.True(t, row.HasIntent())

	for _, flag := range database.MediaUserFlags {
		single := flagRow(flag)
		assert.True(t, single.HasIntent(), "%s", flag)
		for _, other := range database.MediaUserFlags {
			assert.Equal(t, other == flag, single.Flag(other), "%s reading %s", flag, other)
		}
	}

	for _, tc := range []struct {
		row     database.MediaUserData
		invalid bool
	}{
		{row: database.MediaUserData{IsLiked: true, IsDisliked: true}, invalid: true},
		{row: database.MediaUserData{IsFavorite: true, IsDisliked: true}, invalid: true},
		{row: database.MediaUserData{IsFavorite: true, IsLiked: true, IsPlayLater: true, IsHidden: true}},
		{row: database.MediaUserData{IsDisliked: true, IsPlayLater: true, IsHidden: true}},
	} {
		err := tc.row.ValidateFlags()
		if tc.invalid {
			require.ErrorIs(t, err, database.ErrMediaUserFlagConflict, "%+v", tc.row)
		} else {
			require.NoError(t, err, "%+v", tc.row)
		}
	}
}

func flagRow(flag database.MediaUserFlag) database.MediaUserData {
	switch flag {
	case database.MediaUserFlagFavorite:
		return database.MediaUserData{IsFavorite: true}
	case database.MediaUserFlagHidden:
		return database.MediaUserData{IsHidden: true}
	case database.MediaUserFlagLiked:
		return database.MediaUserData{IsLiked: true}
	case database.MediaUserFlagDisliked:
		return database.MediaUserData{IsDisliked: true}
	case database.MediaUserFlagPlayLater:
		return database.MediaUserData{IsPlayLater: true}
	}
	return database.MediaUserData{}
}

func newFlagTestDB(t *testing.T) (db *database.Database, path string, mediaDBID int64) {
	t.Helper()
	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	path = filepath.Join("roms", "NES", "Game.nes")
	scantest.IndexMediaPaths(t, mediaDB, "NES", path)
	rows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return &database.Database{MediaDB: mediaDB, UserDB: userDB}, path, rows[0].DBID
}

func projectedUserTags(t *testing.T, db *database.Database, mediaDBID int64) map[database.MediaUserFlag]bool {
	t.Helper()
	fileTags, err := db.MediaDB.GetMediaTagsByMediaDBID(context.Background(), mediaDBID)
	require.NoError(t, err)
	got := make(map[database.MediaUserFlag]bool)
	for _, tag := range fileTags {
		if tag.Type == string(tags.TagTypeUser) {
			got[database.MediaUserFlag(tag.Tag)] = true
		}
	}
	return got
}

func storedFlags(t *testing.T, db *database.Database, path string) map[database.MediaUserFlag]bool {
	t.Helper()
	row, _, err := db.UserDB.GetMediaUserData("NES", path)
	require.NoError(t, err)
	got := make(map[database.MediaUserFlag]bool)
	for _, flag := range database.MediaUserFlags {
		if row.Flag(flag) {
			got[flag] = true
		}
	}
	return got
}

func TestApplyMediaUserFlagsProjectsStoredState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, path, mediaDBID := newFlagTestDB(t)

	written, err := database.ApplyMediaUserFlags(ctx, db, "NES", path, mediaDBID, map[database.MediaUserFlag]bool{
		database.MediaUserFlagFavorite:  true,
		database.MediaUserFlagLiked:     true,
		database.MediaUserFlagPlayLater: true,
	})
	require.NoError(t, err)
	want := map[database.MediaUserFlag]bool{
		database.MediaUserFlagFavorite: true, database.MediaUserFlagLiked: true, database.MediaUserFlagPlayLater: true,
	}
	assert.Equal(t, want, written)
	assert.Equal(t, want, storedFlags(t, db, path))
	assert.Equal(t, want, projectedUserTags(t, db, mediaDBID))

	// Disliking clears the like and the favorite in both stores and leaves
	// play later alone; only the changed tags are written.
	written, err = database.ApplyMediaUserFlags(ctx, db, "NES", path, mediaDBID, map[database.MediaUserFlag]bool{
		database.MediaUserFlagDisliked: true,
	})
	require.NoError(t, err)
	assert.Equal(t, map[database.MediaUserFlag]bool{
		database.MediaUserFlagFavorite: false, database.MediaUserFlagLiked: false, database.MediaUserFlagDisliked: true,
	}, written)
	want = map[database.MediaUserFlag]bool{database.MediaUserFlagDisliked: true, database.MediaUserFlagPlayLater: true}
	assert.Equal(t, want, storedFlags(t, db, path))
	assert.Equal(t, want, projectedUserTags(t, db, mediaDBID))

	// The same request again changes nothing.
	written, err = database.ApplyMediaUserFlags(ctx, db, "NES", path, mediaDBID, map[database.MediaUserFlag]bool{
		database.MediaUserFlagDisliked: true,
	})
	require.NoError(t, err)
	assert.Empty(t, written)
}

func TestApplyMediaUserFlagsRepairsStaleProjection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, path, mediaDBID := newFlagTestDB(t)

	// A stale favorite left behind by a failed projection, and a stored
	// dislike that never reached MediaDB.
	require.NoError(t, db.MediaDB.UpdateMediaTags(ctx, mediaDBID, nil, []database.MediaTagRef{
		{Type: string(tags.TagTypeUser), Tag: string(tags.TagUserFavorite)},
	}))
	require.NoError(t, db.UserDB.SetMediaUserFlag("NES", path, database.MediaUserFlagDisliked, true))

	_, err := database.ApplyMediaUserFlags(ctx, db, "NES", path, mediaDBID, map[database.MediaUserFlag]bool{
		database.MediaUserFlagDisliked: true,
	})
	require.NoError(t, err)
	assert.Equal(t, map[database.MediaUserFlag]bool{database.MediaUserFlagDisliked: true},
		projectedUserTags(t, db, mediaDBID))
}

func TestApplyMediaUserFlagsRefusesBadRequests(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, path, mediaDBID := newFlagTestDB(t)

	_, err := database.ApplyMediaUserFlags(ctx, db, "NES", path, mediaDBID, map[database.MediaUserFlag]bool{
		database.MediaUserFlagLiked: true, database.MediaUserFlagDisliked: true,
	})
	require.ErrorIs(t, err, database.ErrMediaUserFlagConflict)
	_, err = database.ApplyMediaUserFlags(ctx, db, "NES", path, mediaDBID, map[database.MediaUserFlag]bool{
		database.MediaUserFlag("bogus"): true,
	})
	require.Error(t, err)
	assert.Empty(t, storedFlags(t, db, path), "a refused request writes nothing")
	assert.Empty(t, projectedUserTags(t, db, mediaDBID))

	// Without a media row only the truth is written.
	written, err := database.ApplyMediaUserFlags(ctx, db, "NES", path, 0, map[database.MediaUserFlag]bool{
		database.MediaUserFlagLiked: true,
	})
	require.NoError(t, err)
	assert.Empty(t, written)
	assert.Equal(t, map[database.MediaUserFlag]bool{database.MediaUserFlagLiked: true}, storedFlags(t, db, path))
	assert.Empty(t, projectedUserTags(t, db, mediaDBID))
}

// overlapUserDB and overlapMediaDB mark the span from an edit's UserDB write
// to its MediaDB read, so a test can see two edits inside it at once.
type overlapUserDB struct {
	database.UserDBI
	active  *atomic.Int32
	overlap *atomic.Bool
}

func (o overlapUserDB) SetMediaUserFlag(systemID, path string, flag database.MediaUserFlag, value bool) error {
	if o.active.Add(1) > 1 {
		o.overlap.Store(true)
	}
	runtime.Gosched()
	return o.UserDBI.SetMediaUserFlag(systemID, path, flag, value) //nolint:wrapcheck // test passthrough
}

type overlapMediaDB struct {
	database.MediaDBI
	active *atomic.Int32
}

func (o overlapMediaDB) GetMediaTagsByMediaDBID(ctx context.Context, mediaDBID int64) ([]database.TagInfo, error) {
	defer o.active.Add(-1)
	return o.MediaDBI.GetMediaTagsByMediaDBID(ctx, mediaDBID) //nolint:wrapcheck // test passthrough
}

// Concurrent reaction switches on one file never interleave, and leave MediaDB
// showing exactly what UserDB holds.
func TestApplyMediaUserFlagsConcurrentEditsConverge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	realDB, path, mediaDBID := newFlagTestDB(t)
	var active atomic.Int32
	var overlap atomic.Bool
	db := &database.Database{
		UserDB:  overlapUserDB{UserDBI: realDB.UserDB, active: &active, overlap: &overlap},
		MediaDB: overlapMediaDB{MediaDBI: realDB.MediaDB, active: &active},
	}

	// One flag per request, so each edit makes exactly one UserDB write.
	requests := []map[database.MediaUserFlag]bool{
		{database.MediaUserFlagLiked: true},
		{database.MediaUserFlagDisliked: true},
		{database.MediaUserFlagFavorite: true},
		{database.MediaUserFlagPlayLater: true},
		{database.MediaUserFlagFavorite: false},
	}
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			for i := range 25 {
				changes := requests[(worker+i)%len(requests)]
				_, err := database.ApplyMediaUserFlags(ctx, db, "NES", path, mediaDBID, changes)
				assert.NoError(t, err)
			}
		})
	}
	wg.Wait()
	assert.False(t, overlap.Load(), "two edits ran between a UserDB write and its projection at once")
	assert.Equal(t, storedFlags(t, realDB, path), projectedUserTags(t, realDB, mediaDBID))
}
