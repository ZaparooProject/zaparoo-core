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
	"strconv"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// testRetryDelays matches the production retry count with negligible waits so
// failing-retry sweeps don't burn real time in tests.
var testRetryDelays = []time.Duration{time.Millisecond, time.Millisecond}

// stubSettledMediaDB makes the mock report an idle media database whose last
// completed index finished at lastGenerated.
func stubSettledMediaDB(mediaDB *testhelpers.MockMediaDBI, lastGenerated time.Time) {
	mediaDB.On("GetLastGenerated").Return(lastGenerated, nil)
	mediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil)
	mediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil)
}

// unresolvableBatch builds a full-size batch whose rows are all outside the
// media index, so a sweep walks the whole batch without any enrichment work.
func unresolvableBatch(size int) []database.MediaHistoryEntry {
	batch := make([]database.MediaHistoryEntry, 0, size)
	for i := range size {
		batch = append(batch, database.MediaHistoryEntry{
			DBID:      int64(i + 1),
			SystemID:  "NES",
			MediaPath: filepath.Join("outside-index", "Missing.nes"),
		})
	}
	return batch
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
		testRetryDelays,
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
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, nil, nil, 0, testRetryDelays,
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
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, nil, nil, 0, testRetryDelays,
	)

	userDB.AssertNotCalled(t, "GetMediaHistoryIdentityBackfillBatch",
		mock.Anything, mock.Anything, mock.Anything)
	userDB.AssertNotCalled(t, "SetDeviceState", mock.Anything, mock.Anything)
	userDB.AssertExpectations(t)
}

func TestMediaHistoryIdentitySweep_DefersWhileOptimizing(t *testing.T) {
	t.Parallel()

	userDB := testhelpers.NewMockUserDBI()
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetLastGenerated").Return(time.Unix(1754000000, 0), nil)
	mediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil)
	// Optimization runs the disambiguation backfill, which can still change
	// what a row resolves to, so sweeping now would enrich rows twice.
	mediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusPending, nil)
	userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
		Return("", false, nil).Once()

	runMediaHistoryIdentitySweep(
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, nil, nil, 0, testRetryDelays,
	)

	userDB.AssertNotCalled(t, "GetMediaHistoryIdentityBackfillBatch",
		mock.Anything, mock.Anything, mock.Anything)
	userDB.AssertNotCalled(t, "SetDeviceState", mock.Anything, mock.Anything)
	userDB.AssertExpectations(t)
}

// A media database too busy to answer a status query is a media database too
// busy to sweep: unreadable status must never be read as "idle".
func TestMediaHistoryIdentitySweep_TreatsStatusReadFailuresAsUnsettled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		stub func(*testhelpers.MockMediaDBI)
		name string
	}{
		{
			name: "indexing status unreadable",
			stub: func(mediaDB *testhelpers.MockMediaDBI) {
				mediaDB.On("GetIndexingStatus").
					Return(mediadb.IndexingStatusCompleted, errors.New("database busy"))
			},
		},
		{
			name: "optimization status unreadable",
			stub: func(mediaDB *testhelpers.MockMediaDBI) {
				mediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil)
				mediaDB.On("GetOptimizationStatus").
					Return(mediadb.IndexingStatusCompleted, errors.New("database busy"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			userDB := testhelpers.NewMockUserDBI()
			mediaDB := testhelpers.NewMockMediaDBI()
			mediaDB.On("GetLastGenerated").Return(time.Unix(1754000000, 0), nil)
			tt.stub(mediaDB)
			userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
				Return("", false, nil).Once()

			runMediaHistoryIdentitySweep(
				context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB},
				nil, nil, 0, testRetryDelays,
			)

			userDB.AssertNotCalled(t, "GetMediaHistoryIdentityBackfillBatch",
				mock.Anything, mock.Anything, mock.Anything)
			userDB.AssertNotCalled(t, "SetDeviceState", mock.Anything, mock.Anything)
			userDB.AssertExpectations(t)
		})
	}
}

// An unreadable marker is not a current marker: the sweep must still run,
// because skipping it would strand legacy rows until the next media index.
func TestMediaHistoryIdentitySweep_SweepsWhenMarkerUnreadable(t *testing.T) {
	t.Parallel()

	userDB := testhelpers.NewMockUserDBI()
	mediaDB := testhelpers.NewMockMediaDBI()
	stubSettledMediaDB(mediaDB, time.Unix(1754000000, 0))
	userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
		Return("", false, errors.New("database busy")).Once()
	userDB.On("GetMediaHistoryIdentityBackfillBatch", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.MediaHistoryEntry{}, nil).Once()
	userDB.On(
		"SetDeviceState",
		database.DeviceStateKeyMediaHistoryIdentitySweep, mediaHistoryIdentitySweepMarker(mediaDB),
	).Return(nil).Once()

	runMediaHistoryIdentitySweep(
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, nil, nil, 0, testRetryDelays,
	)

	userDB.AssertExpectations(t)
}

// The marker pairs the policy version with the last completed index. A
// missing or unreadable index time must still produce a stable marker,
// otherwise every trigger would re-walk the whole table.
func TestMediaHistoryIdentitySweepMarker_FallsBackWithoutIndexTime(t *testing.T) {
	t.Parallel()

	current := strconv.Itoa(database.CurrentMediaIdentityPolicyVersion)

	unreadable := testhelpers.NewMockMediaDBI()
	unreadable.On("GetLastGenerated").Return(time.Time{}, errors.New("database busy"))
	assert.Equal(t, current+":0", mediaHistoryIdentitySweepMarker(unreadable))

	neverIndexed := testhelpers.NewMockMediaDBI()
	neverIndexed.On("GetLastGenerated").Return(time.Time{}, nil)
	assert.Equal(t, current+":0", mediaHistoryIdentitySweepMarker(neverIndexed))

	indexed := testhelpers.NewMockMediaDBI()
	indexed.On("GetLastGenerated").Return(time.Unix(1754000000, 0), nil)
	assert.Equal(t, current+":1754000000", mediaHistoryIdentitySweepMarker(indexed))
}

// Every failure that stops a pass short must leave the marker unwritten, so
// the next trigger resumes instead of declaring the table swept.
func TestMediaHistoryIdentitySweep_AbortsWithoutMarkerOnFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		stub func(*testhelpers.MockUserDBI, *testhelpers.MockMediaDBI)
		name string
	}{
		{
			name: "batch read fails",
			stub: func(userDB *testhelpers.MockUserDBI, _ *testhelpers.MockMediaDBI) {
				userDB.On("GetMediaHistoryIdentityBackfillBatch",
					mock.Anything, mock.Anything, mock.Anything).
					Return([]database.MediaHistoryEntry(nil), errors.New("database busy")).Once()
			},
		},
		{
			name: "identity write fails",
			stub: func(userDB *testhelpers.MockUserDBI, mediaDB *testhelpers.MockMediaDBI) {
				indexedPath := filepath.Join("roms", "NES", "Indexed.nes")
				userDB.On("GetMediaHistoryIdentityBackfillBatch",
					mock.Anything, mock.Anything, mock.Anything).
					Return([]database.MediaHistoryEntry{
						{DBID: 60, SystemID: "NES", MediaPath: indexedPath},
					}, nil).Once()
				mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, indexedPath).
					Return([]database.SearchResult{{
						SystemID: "NES", Name: "Indexed", Path: indexedPath, Slug: "indexed", MediaID: 9,
					}}, nil).Once()
				mediaDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(9)).
					Return([]database.TagInfo{}, nil).Once()
				userDB.On("UpdateMediaHistoryIdentity", int64(60), mock.Anything).
					Return(false, errors.New("database is locked")).Once()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			userDB := testhelpers.NewMockUserDBI()
			mediaDB := testhelpers.NewMockMediaDBI()
			stubSettledMediaDB(mediaDB, time.Unix(1754000000, 0))
			userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
				Return("", false, nil).Once()
			tt.stub(userDB, mediaDB)

			runMediaHistoryIdentitySweep(
				context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB},
				nil, nil, 0, testRetryDelays,
			)

			userDB.AssertNotCalled(t, "SetDeviceState", mock.Anything, mock.Anything)
			userDB.AssertExpectations(t)
			mediaDB.AssertExpectations(t)
		})
	}
}

// An index or optimization starting mid-sweep must hand the media database
// back immediately rather than finishing the walk alongside it.
func TestMediaHistoryIdentitySweep_AbortsWhenMediaUpdateStartsMidSweep(t *testing.T) {
	t.Parallel()

	userDB := testhelpers.NewMockUserDBI()
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetLastGenerated").Return(time.Unix(1754000000, 0), nil)
	// Settled for the pre-walk check and the first batch's re-check...
	mediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil).Twice()
	mediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil).Twice()
	// ...then an index starts, so the second batch is never read.
	mediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusRunning, nil).Once()
	userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
		Return("", false, nil).Once()
	userDB.On("GetMediaHistoryIdentityBackfillBatch",
		int64(0), database.CurrentMediaIdentityPolicyVersion, mediaHistoryIdentityBackfillBatchSize).
		Return(unresolvableBatch(mediaHistoryIdentityBackfillBatchSize), nil).Once()
	mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.SearchResult{}, nil)

	runMediaHistoryIdentitySweep(
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, nil, nil, 0, testRetryDelays,
	)

	userDB.AssertNumberOfCalls(t, "GetMediaHistoryIdentityBackfillBatch", 1)
	userDB.AssertNotCalled(t, "SetDeviceState", mock.Anything, mock.Anything)
	userDB.AssertExpectations(t)
	mediaDB.AssertExpectations(t)
}

// Shutdown must not wait out the trickle delay between batches, and must not
// keep processing rows once the service context is gone.
func TestMediaHistoryIdentitySweep_StopsPromptlyOnCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		batchSize   int
		cancelAfter int
		batchDelay  time.Duration
	}{
		// Cancelled with rows left in the batch: the remaining rows are not
		// looked up.
		{name: "mid batch", batchSize: 4, cancelAfter: 2, batchDelay: 0},
		// A full batch reaches the inter-batch delay; a cancelled sweep must
		// exit there instead of sleeping it out.
		{
			name:        "during batch delay",
			batchSize:   mediaHistoryIdentityBackfillBatchSize,
			cancelAfter: mediaHistoryIdentityBackfillBatchSize,
			batchDelay:  time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			userDB := testhelpers.NewMockUserDBI()
			mediaDB := testhelpers.NewMockMediaDBI()
			stubSettledMediaDB(mediaDB, time.Unix(1754000000, 0))
			userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
				Return("", false, nil).Once()
			userDB.On("GetMediaHistoryIdentityBackfillBatch",
				mock.Anything, mock.Anything, mock.Anything).
				Return(unresolvableBatch(tt.batchSize), nil).Once()
			lookups := 0
			mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, mock.Anything).
				Return([]database.SearchResult{}, nil).Run(func(mock.Arguments) {
				lookups++
				if lookups == tt.cancelAfter {
					cancel()
				}
			})

			done := make(chan struct{})
			go func() {
				runMediaHistoryIdentitySweep(
					ctx, &database.Database{UserDB: userDB, MediaDB: mediaDB},
					nil, nil, tt.batchDelay, testRetryDelays,
				)
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("sweep did not stop after context cancellation")
			}
			assert.Equal(t, tt.cancelAfter, lookups, "no rows looked up after cancellation")
			userDB.AssertNotCalled(t, "SetDeviceState", mock.Anything, mock.Anything)
			userDB.AssertExpectations(t)
		})
	}
}

// Losing the marker write only costs a redundant pass later, so it must not
// look like a failed sweep to the caller.
func TestMediaHistoryIdentitySweep_ToleratesMarkerWriteFailure(t *testing.T) {
	t.Parallel()

	userDB := testhelpers.NewMockUserDBI()
	mediaDB := testhelpers.NewMockMediaDBI()
	stubSettledMediaDB(mediaDB, time.Unix(1754000000, 0))
	userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
		Return("", false, nil).Once()
	userDB.On("GetMediaHistoryIdentityBackfillBatch", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.MediaHistoryEntry{}, nil).Once()
	userDB.On("SetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep, mock.Anything).
		Return(errors.New("database is locked")).Once()

	runMediaHistoryIdentitySweep(
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, nil, nil, 0, testRetryDelays,
	)

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
		Times(len(testRetryDelays) + 1)

	runMediaHistoryIdentitySweep(
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, nil, nil, 0, testRetryDelays,
	)

	userDB.AssertNotCalled(t, "UpdateMediaHistoryIdentity", mock.Anything, mock.Anything)
	userDB.AssertNotCalled(t, "SetDeviceState", mock.Anything, mock.Anything)
	userDB.AssertExpectations(t)
	mediaDB.AssertExpectations(t)
}

func TestMediaHistoryIdentitySweep_AbortAfterUpdateStillRequestsSync(t *testing.T) {
	t.Parallel()

	updatedPath := filepath.Join("roms", "NES", "Updated.nes")
	flakyPath := filepath.Join("roms", "NES", "Flaky.nes")
	userDB := testhelpers.NewMockUserDBI()
	mediaDB := testhelpers.NewMockMediaDBI()
	stubSettledMediaDB(mediaDB, time.Unix(1754000000, 0))
	userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
		Return("", false, nil).Once()
	userDB.On(
		"GetMediaHistoryIdentityBackfillBatch",
		int64(0), database.CurrentMediaIdentityPolicyVersion, mediaHistoryIdentityBackfillBatchSize,
	).Return([]database.MediaHistoryEntry{
		{DBID: 30, SystemID: "NES", MediaPath: updatedPath},
		{DBID: 31, SystemID: "NES", MediaPath: flakyPath},
	}, nil).Once()
	mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, updatedPath).
		Return([]database.SearchResult{{
			SystemID: "NES", Name: "Updated", Path: updatedPath, Slug: "updated", MediaID: 8,
		}}, nil).Once()
	mediaDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(8)).
		Return([]database.TagInfo{}, nil).Once()
	userDB.On("UpdateMediaHistoryIdentity", int64(30), mock.Anything).Return(true, nil).Once()
	// The second row fails every bounded retry, aborting the sweep mid-batch.
	// The already-enriched first row has SyncedAt cleared, so the abort path
	// must still flush the pending play-sync request on the way out.
	mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, flakyPath).
		Return([]database.SearchResult(nil), errors.New("database busy")).
		Times(len(testRetryDelays) + 1)
	syncRequests := 0

	runMediaHistoryIdentitySweep(
		context.Background(),
		&database.Database{UserDB: userDB, MediaDB: mediaDB},
		nil,
		func() { syncRequests++ },
		0,
		testRetryDelays,
	)

	assert.Equal(t, 1, syncRequests,
		"abort after a successful update must still request a play sync")
	userDB.AssertNotCalled(t, "SetDeviceState", mock.Anything, mock.Anything)
	userDB.AssertExpectations(t)
	mediaDB.AssertExpectations(t)
}

// The watcher starts before the databases are guaranteed open, so a missing
// one must end it quietly rather than panic a boot goroutine.
func TestWatchMediaHistoryBackfill_ReturnsWithoutDatabases(t *testing.T) {
	t.Parallel()

	for name, db := range map[string]*database.Database{
		"nil database": nil,
		"nil user db":  {MediaDB: testhelpers.NewMockMediaDBI()},
		"nil media db": {UserDB: testhelpers.NewMockUserDBI()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			done := make(chan struct{})
			go func() {
				watchMediaHistoryBackfillAtInterval(
					context.Background(), nil, db, nil, nil, time.Hour, time.Hour, 0,
				)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("watcher did not return without a usable database")
			}
		})
	}
}

// The temporary-repair path starts optimization with no status callback and
// emits no completion notification, so the poll fallback is the only trigger
// that ever fires for those rows.
func TestWatchMediaHistoryBackfill_SweepsOnPollInterval(t *testing.T) {
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
	source := make(chan models.Notification, 1)
	b := broker.NewBroker(ctx, source)
	b.Start()
	t.Cleanup(b.Stop)

	done := make(chan struct{})
	go func() {
		// No startup trigger and no notifications: only the ticker can fire.
		watchMediaHistoryBackfillAtInterval(
			ctx, b, &database.Database{UserDB: userDB, MediaDB: mediaDB},
			nil, nil, time.Hour, 10*time.Millisecond, 0,
		)
		close(done)
	}()

	select {
	case <-marked:
	case <-time.After(2 * time.Second):
		t.Fatal("poll interval did not trigger a sweep")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not exit after context cancellation")
	}
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

func TestWatchMediaHistoryBackfill_RetriesUUIDBackfillUntilSuccess(t *testing.T) {
	t.Parallel()

	userDB := testhelpers.NewMockUserDBI()
	mediaDB := testhelpers.NewMockMediaDBI()
	// First attempt loses a write race (observed on device: retention cleanup
	// holds the lock during the boot window); the next trigger must retry
	// rather than leaving UUID-less rows unsyncable until a restart.
	userDB.On("BackfillMediaHistoryUUIDs").
		Return(int64(0), errors.New("database is locked")).Once()
	userDB.On("BackfillMediaHistoryUUIDs").Return(int64(5), nil).Once()
	stubSettledMediaDB(mediaDB, time.Unix(1754000000, 0))
	// Marker current: sweeps are cheap no-ops so the test isolates the
	// UUID backfill behavior.
	userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
		Return(mediaHistoryIdentitySweepMarker(mediaDB), true, nil)
	syncRequests := make(chan struct{}, 4)

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
			nil, func() { syncRequests <- struct{}{} }, time.Hour, time.Hour, 0,
		)
		close(done)
	}()

	// Publish triggers until the retry succeeds and requests the upload of
	// the newly UUID-assigned rows.
	require.Eventually(t, func() bool {
		select {
		case source <- models.Notification{Method: models.NotificationMediaIndexing}:
		default:
		}
		select {
		case <-syncRequests:
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
	userDB.AssertExpectations(t)
	assert.Empty(t, syncRequests,
		"exactly one sync request: the succeeded backfill must not re-run on later triggers")
}

// Gameplay pauses the sweep indefinitely, so shutdown has to break the pause
// rather than wait it out — and a sweep that stops there has not finished,
// so it must leave the marker alone for the next run.
func TestMediaHistoryIdentitySweep_StopsWhilePausedForGameplay(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	userDB := testhelpers.NewMockUserDBI()
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetLastGenerated").Return(time.Unix(1754000000, 0), nil)
	mediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil)
	mediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil).Once()
	// Shutdown lands after the sweep decides to run, while it is blocked on
	// the gameplay pause.
	mediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil).Once().
		Run(func(mock.Arguments) { cancel() })
	userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
		Return("", false, nil).Once()

	pauser := syncutil.NewPauser()
	pauser.Pause()

	done := make(chan struct{})
	go func() {
		defer close(done)
		runMediaHistoryIdentitySweep(
			ctx, &database.Database{UserDB: userDB, MediaDB: mediaDB},
			pauser, nil, 0, testRetryDelays,
		)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep did not stop while paused")
	}

	userDB.AssertNotCalled(t, "GetMediaHistoryIdentityBackfillBatch",
		mock.Anything, mock.Anything, mock.Anything)
	userDB.AssertNotCalled(t, "SetDeviceState", mock.Anything, mock.Anything)
	userDB.AssertExpectations(t)
	mediaDB.AssertExpectations(t)
}

// The watcher's only wake-up sources are the ticker and the indexing
// notification channel. A closed channel is permanently ready, so a watcher
// that kept selecting on it after broker shutdown would spin a core forever:
// it has to exit instead.
func TestWatchMediaHistoryBackfill_StopsWhenBrokerCloses(t *testing.T) {
	t.Parallel()

	userDB := testhelpers.NewMockUserDBI()
	mediaDB := testhelpers.NewMockMediaDBI()
	userDB.On("BackfillMediaHistoryUUIDs").Return(int64(0), nil).Once()
	stubSettledMediaDB(mediaDB, time.Unix(1754000000, 0))
	userDB.On("GetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep).
		Return("", false, nil)
	userDB.On("GetMediaHistoryIdentityBackfillBatch", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.MediaHistoryEntry{}, nil)
	swept := make(chan struct{}, 1)
	userDB.On("SetDeviceState", database.DeviceStateKeyMediaHistoryIdentitySweep, mock.Anything).
		Return(nil).Run(func(mock.Arguments) {
		select {
		case swept <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b := broker.NewBroker(ctx, make(chan models.Notification))
	b.Start()

	done := make(chan struct{})
	go func() {
		defer close(done)
		watchMediaHistoryBackfillAtInterval(
			ctx, b, &database.Database{UserDB: userDB, MediaDB: mediaDB},
			nil, nil, time.Hour, 20*time.Millisecond, 0,
		)
	}()

	// A completed poll proves the watcher reached its select loop, so it has
	// already subscribed and the shutdown below cannot race ahead of it.
	select {
	case <-swept:
	case <-time.After(5 * time.Second):
		t.Fatal("poll interval did not trigger a sweep")
	}

	b.Stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher outlived the broker")
	}
}
