/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// stubSettledMediaDB makes the mock report an idle media database whose last
// completed index finished at lastGenerated.
func stubSettledMediaDB(mediaDB *testhelpers.MockMediaDBI, lastGenerated time.Time) {
	mediaDB.On("GetLastGenerated").Return(lastGenerated, nil)
	mediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil)
	mediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil)
}

func TestMediaHistoryIdentitySweep_EnrichesAndCompletesDespiteUnresolvableRows(t *testing.T) {
	t.Parallel()

	indexedPath := filepath.Join("roms", "NES", "Indexed.nes")
	unindexedPath := filepath.Join("outside-index", "Missing.nes")
	userDB := testhelpers.NewMockUserDBI()
	mediaDB := testhelpers.NewMockMediaDBI()
	stubSettledMediaDB(mediaDB, time.Unix(1754000000, 0))
	userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
		Return("", false, nil).Once()
	userDB.On(
		"GetMediaHistoryIdentityBackfillBatch",
		int64(0), database.CurrentMediaIdentityPolicyVersion, mediaHistoryIdentityBackfillBatchSize,
	).Return([]database.MediaHistoryEntry{
		{DBID: 10, SystemID: "NES", MediaPath: indexedPath},
		{DBID: 11, SystemID: "NES", MediaPath: unindexedPath},
	}, nil).Once()
	mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, indexedPath).
		Return([]database.SearchResult{{
			SystemID: "NES", Name: "Indexed", Path: indexedPath, Slug: "indexed", MediaID: 7,
		}}, nil).Once()
	mediaDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(7)).
		Return([]database.TagInfo{{Type: "region", Tag: "us"}}, nil).Once()
	mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, unindexedPath).
		Return([]database.SearchResult{}, nil).Once()
	userDB.On("UpdateMediaHistoryIdentity", int64(10), mock.MatchedBy(
		func(identity *database.MediaIdentity) bool {
			return identity != nil && identity.DisplayName == "Indexed" && identity.CoreSlug == "indexed" &&
				identity.ObservationFingerprint != ""
		},
	)).Return(true, nil).Once()
	// The pass must complete (and stamp the marker) even though one row was
	// unresolvable: skipped rows wait for the next index, never block a sweep.
	userDB.On(
		"SetDeviceState",
		database.DeviceStateKeyMediaHistoryIdentitySweep, mediaHistoryIdentitySweepMarker(mediaDB),
	).Return(nil).Once()
	syncRequests := 0

	runMediaHistoryIdentitySweep(
		context.Background(),
		&database.Database{UserDB: userDB, MediaDB: mediaDB},
		nil,
		func() { syncRequests++ },
		0,
	)

	assert.Equal(t, 1, syncRequests, "sync requested once per batch, not per row")
	userDB.AssertNotCalled(t, "UpdateMediaHistoryIdentity", int64(11), mock.Anything)
	userDB.AssertExpectations(t)
	mediaDB.AssertExpectations(t)
}

func TestMediaHistoryIdentitySweep_SkipsTableWalkWhenMarkerCurrent(t *testing.T) {
	t.Parallel()

	userDB := testhelpers.NewMockUserDBI()
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetLastGenerated").Return(time.Unix(1754000000, 0), nil)
	userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
		Return(mediaHistoryIdentitySweepMarker(mediaDB), true, nil).Once()

	runMediaHistoryIdentitySweep(
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, nil, nil, 0,
	)

	userDB.AssertNotCalled(t, "GetMediaHistoryIdentityBackfillBatch",
		mock.Anything, mock.Anything, mock.Anything)
	userDB.AssertNotCalled(t, "SetDeviceState", mock.Anything, mock.Anything)
	mediaDB.AssertNotCalled(t, "GetIndexingStatus")
	userDB.AssertExpectations(t)
}

func TestMediaHistoryIdentitySweep_DefersWhileMediaUpdating(t *testing.T) {
	t.Parallel()

	userDB := testhelpers.NewMockUserDBI()
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetLastGenerated").Return(time.Unix(1754000000, 0), nil)
	mediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusRunning, nil)
	userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
		Return("", false, nil).Once()

	runMediaHistoryIdentitySweep(
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, nil, nil, 0,
	)

	userDB.AssertNotCalled(t, "GetMediaHistoryIdentityBackfillBatch",
		mock.Anything, mock.Anything, mock.Anything)
	userDB.AssertNotCalled(t, "SetDeviceState", mock.Anything, mock.Anything)
	userDB.AssertExpectations(t)
}

func TestMediaHistoryIdentitySweep_TransientFailureAbortsWithoutMarker(t *testing.T) {
	t.Parallel()

	path := filepath.Join("roms", "NES", "Flaky.nes")
	userDB := testhelpers.NewMockUserDBI()
	mediaDB := testhelpers.NewMockMediaDBI()
	stubSettledMediaDB(mediaDB, time.Unix(1754000000, 0))
	userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
		Return("", false, nil).Once()
	userDB.On(
		"GetMediaHistoryIdentityBackfillBatch",
		int64(0), database.CurrentMediaIdentityPolicyVersion, mediaHistoryIdentityBackfillBatchSize,
	).Return([]database.MediaHistoryEntry{
		{DBID: 20, SystemID: "NES", MediaPath: path},
	}, nil).Once()
	// Every bounded retry attempt fails: the sweep must abort so a later
	// trigger retries, and must not stamp the completion marker.
	mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, path).
		Return([]database.SearchResult(nil), errors.New("database busy")).
		Times(len(mediaIdentityRetryDelays) + 1)

	runMediaHistoryIdentitySweep(
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, nil, nil, 0,
	)

	userDB.AssertNotCalled(t, "UpdateMediaHistoryIdentity", mock.Anything, mock.Anything)
	userDB.AssertNotCalled(t, "SetDeviceState", mock.Anything, mock.Anything)
	userDB.AssertExpectations(t)
	mediaDB.AssertExpectations(t)
}

func TestWatchMediaHistoryBackfill_SweepsOnIndexingNotification(t *testing.T) {
	t.Parallel()

	userDB := testhelpers.NewMockUserDBI()
	mediaDB := testhelpers.NewMockMediaDBI()
	userDB.On("BackfillMediaHistoryUUIDs").Return(int64(0), nil).Once()
	stubSettledMediaDB(mediaDB, time.Unix(1754000000, 0))
	userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
		Return("", false, nil)
	userDB.On("GetMediaHistoryIdentityBackfillBatch", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.MediaHistoryEntry{}, nil)
	marked := make(chan struct{}, 1)
	userDB.On("SetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep, mock.Anything).
		Return(nil).Run(func(mock.Arguments) {
		select {
		case marked <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	source := make(chan models.Notification, 10)
	b := broker.NewBroker(ctx, source)
	b.Start()
	t.Cleanup(b.Stop)

	done := make(chan struct{})
	go func() {
		watchMediaHistoryBackfillAtInterval(
			ctx, b, &database.Database{UserDB: userDB, MediaDB: mediaDB},
			nil, nil, time.Hour, time.Hour, 0,
		)
		close(done)
	}()

	// Re-publish until the watcher has subscribed and completed a sweep.
	// Re-publishing is harmless: each sweep with an empty batch is idempotent.
	require.Eventually(t, func() bool {
		select {
		case source <- models.Notification{Method: models.NotificationMediaIndexing}:
		default:
		}
		select {
		case <-marked:
			return true
		default:
			return false
		}
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not exit after context cancellation")
	}
}

func TestWatchMediaHistoryBackfill_StartupSweepAndUUIDBackfillSync(t *testing.T) {
	t.Parallel()

	userDB := testhelpers.NewMockUserDBI()
	mediaDB := testhelpers.NewMockMediaDBI()
	userDB.On("BackfillMediaHistoryUUIDs").Return(int64(2), nil).Once()
	stubSettledMediaDB(mediaDB, time.Unix(1754000000, 0))
	userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
		Return("", false, nil)
	userDB.On("GetMediaHistoryIdentityBackfillBatch", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.MediaHistoryEntry{}, nil)
	marked := make(chan struct{}, 1)
	userDB.On("SetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep, mock.Anything).
		Return(nil).Run(func(mock.Arguments) {
		select {
		case marked <- struct{}{}:
		default:
		}
	})
	syncRequests := make(chan struct{}, 4)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	source := make(chan models.Notification, 1)
	b := broker.NewBroker(ctx, source)
	b.Start()
	t.Cleanup(b.Stop)

	done := make(chan struct{})
	go func() {
		watchMediaHistoryBackfillAtInterval(
			ctx, b, &database.Database{UserDB: userDB, MediaDB: mediaDB},
			nil, func() { syncRequests <- struct{}{} },
			10*time.Millisecond, time.Hour, 0,
		)
		close(done)
	}()

	select {
	case <-syncRequests:
		// UUID backfill mutated rows, so an upload was requested.
	case <-time.After(2 * time.Second):
		t.Fatal("UUID backfill did not request a play sync")
	}
	select {
	case <-marked:
		// The startup check ran a sweep without any indexing notification.
	case <-time.After(2 * time.Second):
		t.Fatal("startup sweep did not complete")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not exit after context cancellation")
	}
}
