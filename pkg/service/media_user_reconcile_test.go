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

package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reconcilerFixture struct {
	db      *database.Database
	path    string
	mediaID int64
}

// newReconcilerFixture indexes one file whose media.db favorite tag has no
// UserDB row behind it, as after a restore that dropped the favorite.
func newReconcilerFixture(t *testing.T) *reconcilerFixture {
	t.Helper()
	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	path := filepath.Join("roms", "NES", "Game.nes")
	scantest.IndexMediaPaths(t, mediaDB, "NES", path)
	rows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NoError(t, mediaDB.UpdateMediaTags(context.Background(), rows[0].DBID, nil,
		[]database.MediaTagRef{{Type: string(tags.TagTypeUser), Tag: string(tags.TagUserFavorite)}}))
	return &reconcilerFixture{
		db:      &database.Database{MediaDB: mediaDB, UserDB: userDB},
		path:    path,
		mediaID: rows[0].DBID,
	}
}

func (f *reconcilerFixture) markerSaved(t *testing.T) bool {
	t.Helper()
	_, found, err := f.db.UserDB.GetDeviceState(database.DeviceStateKeyMediaUserDataReconcile)
	require.NoError(t, err)
	return found
}

func (f *reconcilerFixture) favoriteProjected(t *testing.T) bool {
	t.Helper()
	fileTags, err := f.db.MediaDB.GetMediaTagsByMediaDBID(context.Background(), f.mediaID)
	require.NoError(t, err)
	for _, tag := range fileTags {
		if tag.Type == string(tags.TagTypeUser) && tag.Tag == string(tags.TagUserFavorite) {
			return true
		}
	}
	return false
}

func TestMediaUserReconcilerIdleWithoutRequest(t *testing.T) {
	t.Parallel()
	f := newReconcilerFixture(t)

	assert.True(t, newMediaUserReconciler(f.db).reconcilePending(context.Background()))

	assert.True(t, f.favoriteProjected(t), "nothing asked for a reconcile")
}

// The restore handler queues, then the service restarts: the request has to be
// saved before any work, and a new reconciler has to pick it up.
func TestMediaUserReconcilerRequestSurvivesRestart(t *testing.T) {
	t.Parallel()
	f := newReconcilerFixture(t)

	newMediaUserReconciler(f.db).QueueMediaUserDataReconcile()
	require.True(t, f.markerSaved(t), "the request is saved before it is acted on")
	require.True(t, f.favoriteProjected(t))

	assert.True(t, newMediaUserReconciler(f.db).reconcilePending(context.Background()))

	assert.False(t, f.favoriteProjected(t), "the stale favorite is removed")
	assert.False(t, f.markerSaved(t), "a finished reconcile leaves nothing saved")
}

// failingListUserDB fails ListMediaUserData while fail is set.
type failingListUserDB struct {
	database.UserDBI
	fail bool
}

func (f *failingListUserDB) ListMediaUserData() ([]database.MediaUserData, error) {
	if f.fail {
		return nil, errors.New("list boom")
	}
	return f.UserDBI.ListMediaUserData() //nolint:wrapcheck // test passthrough
}

func TestMediaUserReconcilerKeepsFailedRequest(t *testing.T) {
	t.Parallel()
	f := newReconcilerFixture(t)
	failing := &failingListUserDB{UserDBI: f.db.UserDB, fail: true}
	r := newMediaUserReconciler(&database.Database{MediaDB: f.db.MediaDB, UserDB: failing})
	r.QueueMediaUserDataReconcile()

	assert.False(t, r.reconcilePending(context.Background()), "a failed reconcile is still owed")
	assert.True(t, f.markerSaved(t))
	assert.True(t, f.favoriteProjected(t))

	failing.fail = false
	assert.True(t, r.reconcilePending(context.Background()))
	assert.False(t, f.markerSaved(t))
	assert.False(t, f.favoriteProjected(t))
}

// requeueingUserDB queues another reconcile from inside the first one, as a
// second restore landing mid-reconcile would.
type requeueingUserDB struct {
	database.UserDBI
	requeue func()
	lists   int
}

func (r *requeueingUserDB) ListMediaUserData() ([]database.MediaUserData, error) {
	r.lists++
	if r.lists == 1 {
		r.requeue()
	}
	return r.UserDBI.ListMediaUserData() //nolint:wrapcheck // test passthrough
}

func TestMediaUserReconcilerRequeueDuringReconcile(t *testing.T) {
	t.Parallel()
	f := newReconcilerFixture(t)
	userDB := &requeueingUserDB{UserDBI: f.db.UserDB}
	r := newMediaUserReconciler(&database.Database{MediaDB: f.db.MediaDB, UserDB: userDB})
	userDB.requeue = r.QueueMediaUserDataReconcile
	r.QueueMediaUserDataReconcile()

	assert.True(t, r.reconcilePending(context.Background()))

	assert.Equal(t, 2, userDB.lists, "the request made during the first reconcile gets its own")
	assert.False(t, f.markerSaved(t))
}

func TestMediaUserReconcilerWaitsForMediaWrites(t *testing.T) {
	t.Parallel()
	f := newReconcilerFixture(t)
	r := newMediaUserReconciler(f.db)
	r.busyPoll = 5 * time.Millisecond
	r.QueueMediaUserDataReconcile()

	coordinator, err := database.GetMediaDBWriteCoordinator(f.db.MediaDB)
	require.NoError(t, err)
	lease, err := coordinator.AcquireMediaWrite(database.MediaWriteOperationIndexing)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	r.reconcilePending(ctx)
	assert.True(t, f.favoriteProjected(t), "nothing is written while an index owns the media database")
	assert.True(t, f.markerSaved(t))

	lease.Release()
	assert.True(t, r.reconcilePending(context.Background()))
	assert.False(t, f.favoriteProjected(t))
}

// Run picks up a request left by the previous run, then ones queued while it
// runs, and returns when its context ends.
func TestMediaUserReconcilerRun(t *testing.T) {
	t.Parallel()
	f := newReconcilerFixture(t)
	newMediaUserReconciler(f.db).QueueMediaUserDataReconcile()

	r := newMediaUserReconciler(f.db)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
	}()
	require.Eventually(t, func() bool { return !f.markerSaved(t) }, 5*time.Second, 5*time.Millisecond)
	assert.False(t, f.favoriteProjected(t))

	_, err := database.ApplyMediaUserFlags(context.Background(), f.db, "NES", f.path, 0,
		map[database.MediaUserFlag]bool{database.MediaUserFlagHidden: true})
	require.NoError(t, err)
	r.QueueMediaUserDataReconcile()
	require.Eventually(t, func() bool { return !f.markerSaved(t) }, 5*time.Second, 5*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
}
