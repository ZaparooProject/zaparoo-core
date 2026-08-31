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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	testmocks "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestRetryDurableMediaWriteOperationsIdleDoesNotTouchDurableState(t *testing.T) {
	pendingMediaWriteRetries.Store(0)
	mockDB := helpers.NewMockMediaDBI()
	pl := testmocks.NewMockPlatform()
	st, _ := state.NewState(pl, "test-boot-uuid")
	t.Cleanup(st.StopService)

	retryDurableMediaWriteOperations(
		context.Background(), pl, nil, &database.Database{MediaDB: mockDB}, st, nil, nil,
	)

	mockDB.AssertNotCalled(t, "GetIndexingStatus")
	mockDB.AssertNotCalled(t, "ResetIndexResumeAttempts")
	mockDB.AssertNotCalled(t, "GetScrapingStatus")
	mockDB.AssertNotCalled(t, "GetOptimizationStatus")
	mockDB.AssertNotCalled(t, "TemporaryRepairJobsPending", mock.Anything)
}

func TestRetryDurableMediaWriteOperationsDefersThenResumesOptimizationOnce(t *testing.T) {
	pendingMediaWriteRetries.Store(mediaWriteRetryOptimization)
	t.Cleanup(func() { pendingMediaWriteRetries.Store(0) })

	optimizationStarted := make(chan struct{})
	releaseOptimization := make(chan struct{})
	optimizationDone := make(chan struct{})
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusPending, nil).Once()
	mockDB.On("TrackBackgroundOperation").Return().Once()
	mockDB.On("BackgroundOperationDone").Run(func(mock.Arguments) {
		close(optimizationDone)
	}).Return().Once()
	mockDB.On("RunBackgroundOptimizationWithLease", mock.Anything, mock.Anything, mock.Anything).
		Run(func(mock.Arguments) {
			close(optimizationStarted)
			<-releaseOptimization
		}).Return(nil).Once()

	pl := testmocks.NewMockPlatform()
	st, _ := state.NewState(pl, "test-boot-uuid")
	t.Cleanup(st.StopService)
	db := &database.Database{MediaDB: mockDB}

	scrapeLease, err := mockDB.AcquireMediaWrite(database.MediaWriteOperationScraping)
	require.NoError(t, err)
	retryDurableMediaWriteOperations(context.Background(), pl, nil, db, st, nil, nil)
	mockDB.AssertNotCalled(
		t, "RunBackgroundOptimizationWithLease", mock.Anything, mock.Anything, mock.Anything,
	)

	scrapeLease.Release()
	retryDurableMediaWriteOperations(context.Background(), pl, nil, db, st, nil, nil)
	select {
	case <-optimizationStarted:
	case <-time.After(time.Second):
		t.Fatal("deferred optimization did not start")
	}
	// Watcher call returned even though optimization remains blocked in background.
	retryDurableMediaWriteOperations(context.Background(), pl, nil, db, st, nil, nil)
	close(releaseOptimization)
	select {
	case <-optimizationDone:
	case <-time.After(time.Second):
		t.Fatal("deferred optimization did not finish")
	}
	mockDB.AssertNumberOfCalls(t, "RunBackgroundOptimizationWithLease", 1)
	mockDB.AssertExpectations(t)
}

func TestRetryDurableMediaWriteOperationsDropsOptimizationRetryAfterStatusError(t *testing.T) {
	pendingMediaWriteRetries.Store(mediaWriteRetryOptimization)
	t.Cleanup(func() { pendingMediaWriteRetries.Store(0) })

	optimizationDone := make(chan struct{})
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("TrackBackgroundOperation").Return().Once()
	mockDB.On("GetOptimizationStatus").Return("", errors.New("temporary status read failure")).Once()
	mockDB.On("BackgroundOperationDone").Run(func(mock.Arguments) {
		close(optimizationDone)
	}).Return().Once()

	pl := testmocks.NewMockPlatform()
	st, _ := state.NewState(pl, "test-boot-uuid")
	t.Cleanup(st.StopService)
	retryDurableMediaWriteOperations(
		context.Background(), pl, nil, &database.Database{MediaDB: mockDB}, st, syncutil.NewPauser(), nil,
	)

	select {
	case <-optimizationDone:
	case <-time.After(time.Second):
		t.Fatal("deferred optimization status check did not finish")
	}
	require.Zero(t, pendingMediaWriteRetries.Load()&mediaWriteRetryOptimization)
	mockDB.AssertExpectations(t)
}

func TestRetryDurableMediaWriteOperationsDropsOptimizationRetryAfterRunError(t *testing.T) {
	pendingMediaWriteRetries.Store(mediaWriteRetryOptimization)
	t.Cleanup(func() { pendingMediaWriteRetries.Store(0) })

	optimizationDone := make(chan struct{})
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("TrackBackgroundOperation").Return().Once()
	mockDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusPending, nil).Once()
	mockDB.On("RunBackgroundOptimizationWithLease", mock.Anything, mock.Anything, mock.Anything).
		Return(errors.New("hard optimization failure")).Once()
	mockDB.On("BackgroundOperationDone").Run(func(mock.Arguments) {
		close(optimizationDone)
	}).Return().Once()

	pl := testmocks.NewMockPlatform()
	st, _ := state.NewState(pl, "test-boot-uuid")
	t.Cleanup(st.StopService)
	retryDurableMediaWriteOperations(
		context.Background(), pl, nil, &database.Database{MediaDB: mockDB}, st, syncutil.NewPauser(), nil,
	)

	select {
	case <-optimizationDone:
	case <-time.After(time.Second):
		t.Fatal("deferred optimization did not finish after hard failure")
	}
	require.Zero(t, pendingMediaWriteRetries.Load()&mediaWriteRetryOptimization)
	mockDB.AssertExpectations(t)
}

func TestRetryDurableMediaWriteOperationsDropsBrowseHealRetryAfterCountError(t *testing.T) {
	pendingMediaWriteRetries.Store(mediaWriteRetryBrowseCacheHeal)
	t.Cleanup(func() { pendingMediaWriteRetries.Store(0) })

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil).Once()
	mockDB.On("GetTotalMediaCount").Return(0, errors.New("temporary count failure")).Once()

	pl := testmocks.NewMockPlatform()
	st, _ := state.NewState(pl, "test-boot-uuid")
	t.Cleanup(st.StopService)
	retryDurableMediaWriteOperations(
		context.Background(), pl, nil, &database.Database{MediaDB: mockDB}, st, syncutil.NewPauser(), nil,
	)

	require.Zero(t, pendingMediaWriteRetries.Load()&mediaWriteRetryBrowseCacheHeal)
	mockDB.AssertExpectations(t)
}

func TestRetryDurableMediaWriteOperationsTracksMaintenanceOnce(t *testing.T) {
	pendingMediaWriteRetries.Store(mediaWriteRetryMaintenance)
	t.Cleanup(func() { pendingMediaWriteRetries.Store(0) })

	maintenanceDone := make(chan struct{})
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("TrackBackgroundOperation").Return().Once()
	mockDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil).Once()
	mockDB.On("TemporaryRepairJobsPending", mock.Anything).Return(false, nil).Once()
	mockDB.On("BackgroundOperationDone").Run(func(mock.Arguments) {
		close(maintenanceDone)
	}).Return().Once()

	pl := testmocks.NewMockPlatform()
	st, _ := state.NewState(pl, "test-boot-uuid")
	t.Cleanup(st.StopService)
	retryDurableMediaWriteOperations(
		context.Background(), pl, nil, &database.Database{MediaDB: mockDB}, st, syncutil.NewPauser(), nil,
	)

	select {
	case <-maintenanceDone:
	case <-time.After(time.Second):
		t.Fatal("deferred startup maintenance did not finish")
	}
	mockDB.AssertExpectations(t)
}

func TestRetryDurableMediaWriteOperationsEndsTrackingBeforeCorruptionRecovery(t *testing.T) {
	pendingMediaWriteRetries.Store(mediaWriteRetryOptimization)
	mediaDBRecoveryAttempts.Store(0)
	t.Cleanup(func() {
		pendingMediaWriteRetries.Store(0)
		mediaDBRecoveryAttempts.Store(0)
	})

	backgroundDone := make(chan struct{})
	recoveryOrdering := make(chan bool, 1)
	recoveryDone := make(chan struct{})
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("TrackBackgroundOperation").Return().Once()
	mockDB.On("BackgroundOperationDone").Run(func(mock.Arguments) {
		close(backgroundDone)
	}).Return().Once()
	mockDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusFailed, nil).Once()
	mockDB.On("QuickCheck").Return(false, nil).Once()
	mockDB.On("IntegrityReport").Return([]string{"Page 42: corrupt"})
	mockDB.On("MarkCorrupt", "quick_check failed before optimization resume").Return().Once()
	mockDB.On("SetIndexingStatus", mediadb.IndexingStatusCorrupt).Return(nil).Once()
	mockDB.On("IsMarkedCorrupt").Return(true).Once()
	mockDB.On("HasBackgroundOperations").Return(false).Once()
	mockDB.On("BeginRecovery").Run(func(mock.Arguments) {
		select {
		case <-backgroundDone:
			recoveryOrdering <- true
		default:
			recoveryOrdering <- false
		}
	}).Return().Once()
	mockDB.On("Recreate", mock.Anything).Return(errors.New("stop after recovery ordering check")).Once()
	mockDB.On("EndRecovery").Run(func(mock.Arguments) {
		close(recoveryDone)
	}).Return().Once()
	mockDB.On("GetLastGenerated").Return(time.Now(), nil).Once()

	pl := testmocks.NewMockPlatform()
	st, _ := state.NewState(pl, "test-boot-uuid")
	t.Cleanup(st.StopService)
	retryDurableMediaWriteOperations(
		context.Background(), pl, nil, &database.Database{MediaDB: mockDB}, st, syncutil.NewPauser(), nil,
	)

	select {
	case ordered := <-recoveryOrdering:
		require.True(t, ordered, "recovery began before retry background tracking ended")
	case <-time.After(time.Second):
		t.Fatal("deferred optimization did not enter corruption recovery")
	}
	select {
	case <-recoveryDone:
	case <-time.After(time.Second):
		t.Fatal("corruption recovery did not finish")
	}
	mockDB.AssertExpectations(t)
}

func TestRetryDurableMediaWriteOperationsResumesBrowseCacheHealAfterConflict(t *testing.T) {
	pendingMediaWriteRetries.Store(0)
	t.Cleanup(func() { pendingMediaWriteRetries.Store(0) })

	healDone := make(chan struct{}, 2)
	populateCalled := make(chan struct{})
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil).Times(4)
	mockDB.On("GetTotalMediaCount").Return(1000, nil).Twice()
	mockDB.On("BrowseCacheNeedsRebuild", mock.Anything).Return(true, nil).Twice()
	mockDB.On("TrackBackgroundOperation").Return().Twice()
	mockDB.On("BackgroundOperationDone").Run(func(mock.Arguments) {
		healDone <- struct{}{}
	}).Return().Twice()
	mockDB.On("PopulateBrowseCache", mock.Anything).Run(func(mock.Arguments) {
		close(populateCalled)
	}).Return(nil).Once()

	pl := testmocks.NewMockPlatform()
	st, _ := state.NewState(pl, "test-boot-uuid")
	t.Cleanup(st.StopService)
	db := &database.Database{MediaDB: mockDB}
	pauser := syncutil.NewPauser()

	scrapeLease, err := mockDB.AcquireMediaWrite(database.MediaWriteOperationScraping)
	require.NoError(t, err)
	checkAndHealBrowseCache(context.Background(), db, st.Notifications, pauser)
	select {
	case <-healDone:
	case <-time.After(time.Second):
		t.Fatal("browse cache heal did not finish after write conflict")
	}
	require.NotZero(t, pendingMediaWriteRetries.Load()&mediaWriteRetryBrowseCacheHeal)
	mockDB.AssertNotCalled(t, "PopulateBrowseCache", mock.Anything)

	scrapeLease.Release()
	retryDurableMediaWriteOperations(context.Background(), pl, nil, db, st, pauser, nil)
	select {
	case <-populateCalled:
	case <-time.After(time.Second):
		t.Fatal("deferred browse cache heal did not retry")
	}
	select {
	case <-healDone:
	case <-time.After(time.Second):
		t.Fatal("retried browse cache heal did not finish")
	}

	require.Zero(t, pendingMediaWriteRetries.Load()&mediaWriteRetryBrowseCacheHeal)
	mockDB.AssertExpectations(t)
}
