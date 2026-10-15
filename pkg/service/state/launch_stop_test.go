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

package state

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestUserStopSeq_CountsStopsWithOrWithoutMedia(t *testing.T) {
	t.Parallel()
	st, _ := newObservedState(t)

	require.Zero(t, st.UserStopSeq())
	st.RecordMediaStop(models.MediaStopReasonStopped)
	assert.Equal(t, uint64(1), st.UserStopSeq(), "a stop that finds no media still counts")

	st.SetActiveMedia(testMedia("a.sfc"))
	st.RecordMediaStop(models.MediaStopReasonStopped)
	assert.Equal(t, uint64(2), st.UserStopSeq())

	st.RecordMediaStop(models.MediaStopReasonReplaced)
	assert.Equal(t, uint64(2), st.UserStopSeq(), "replacing media for a launch is not a stop request")
}

func newLaunchStopState(t *testing.T) (*State, *mocks.MockPlatform, chan struct{}) {
	t.Helper()
	pl := mocks.NewMockPlatform()
	stopped := make(chan struct{}, 4)
	pl.On("StopActiveLauncher", platforms.StopForMenu).Run(func(_ mock.Arguments) {
		stopped <- struct{}{}
	}).Return(nil).Maybe()
	st, _ := NewState(pl, "test-boot-uuid")
	t.Cleanup(st.StopService)
	return st, pl, stopped
}

// A stop that lands after a launch began and before its media is published
// calls the launch off: nothing is published and the new process is stopped.
func TestLaunch_StopBeforePublishCancelsIt(t *testing.T) {
	t.Parallel()
	st, _, stopped := newLaunchStopState(t)

	access, err := st.AcquireMediaLaunch()
	require.NoError(t, err)
	st.RecordMediaStop(models.MediaStopReasonStopped) // nothing is running yet
	access.SetActiveMedia(testMedia("new.sfc"))

	assert.Nil(t, st.ActiveMedia(), "the overtaken launch publishes nothing")
	// The launch holds the launch gate until it returns, and the stop waits
	// for it.
	select {
	case <-stopped:
		t.Fatal("the stop ran before the launch released the launch gate")
	default:
	}
	access.Release()
	<-stopped
}

// Media a newer launch published before the deferred stop ran is not the
// media that stop was meant for.
func TestLaunch_AbandonedStopLeavesNewerMediaAlone(t *testing.T) {
	t.Parallel()
	st, _, stopped := newLaunchStopState(t)
	abandoned := make(chan string, 1)
	st.SetOnLaunchAbandoned(func(media *models.ActiveMedia) { abandoned <- media.Path })

	overtaken, err := st.AcquireMediaLaunch()
	require.NoError(t, err)
	st.RecordMediaStop(models.MediaStopReasonStopped)

	// A newer launch is in flight, on its own goroutine as launches are,
	// while the first is called off.
	newerReady := make(chan struct{})
	newerGo := make(chan struct{})
	newerDone := make(chan struct{})
	go func() {
		defer close(newerDone)
		newer, acquireErr := st.AcquireMediaLaunch()
		if acquireErr != nil {
			t.Error(acquireErr)
			close(newerReady)
			return
		}
		close(newerReady)
		<-newerGo
		newer.SetActiveMedia(testMedia("newer.sfc"))
		newer.Release()
	}()
	<-newerReady

	overtaken.SetActiveMedia(testMedia("abandoned.sfc"))
	close(newerGo)
	<-newerDone
	overtaken.Release()

	assert.Never(t, func() bool {
		select {
		case <-stopped:
			return true
		case <-abandoned:
			return true
		default:
			return false
		}
	}, 200*time.Millisecond, 10*time.Millisecond, "the newer launch's media must not be stopped")
	require.NotNil(t, st.ActiveMedia())
	assert.Equal(t, "newer.sfc", st.ActiveMedia().Path)
}

// What the platform's own stop cannot find because nothing was published, such
// as native audio, is stopped through the abandon callback.
func TestLaunch_AbandonedLaunchRunsTheAbandonCallback(t *testing.T) {
	t.Parallel()
	st, _, stopped := newLaunchStopState(t)
	abandoned := make(chan string, 1)
	st.SetOnLaunchAbandoned(func(media *models.ActiveMedia) { abandoned <- media.Path })

	access, err := st.AcquireMediaLaunch()
	require.NoError(t, err)
	st.RecordMediaStop(models.MediaStopReasonStopped)
	access.SetActiveMedia(testMedia("new.sfc"))
	access.Release()

	assert.Equal(t, "new.sfc", <-abandoned)
	<-stopped
}

func TestLaunch_PublishesWhenNoStopCame(t *testing.T) {
	t.Parallel()
	st, _, _ := newLaunchStopState(t)

	st.RecordMediaStop(models.MediaStopReasonStopped) // an earlier stop
	access, err := st.AcquireMediaLaunch()
	require.NoError(t, err)
	defer access.Release()
	access.SetActiveMedia(testMedia("new.sfc"))

	require.NotNil(t, st.ActiveMedia())
}

func TestLaunch_ItsOwnPreemptionIsNotAStop(t *testing.T) {
	t.Parallel()
	st, _, _ := newLaunchStopState(t)
	st.SetActiveMedia(testMedia("old.sfc"))

	access, err := st.AcquireMediaLaunch()
	require.NoError(t, err)
	defer access.Release()
	access.RecordMediaStop(models.MediaStopReasonReplaced)
	access.SetActiveMedia(testMedia("new.sfc"))

	require.NotNil(t, st.ActiveMedia())
	assert.Equal(t, "new.sfc", st.ActiveMedia().Path)
}

func TestLaunch_StopAfterPublishStopsNormally(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)

	access, err := st.AcquireMediaLaunch()
	require.NoError(t, err)
	access.SetActiveMedia(testMedia("new.sfc"))
	access.Release()
	require.NotNil(t, st.ActiveMedia())

	st.RecordMediaStop(models.MediaStopReasonStopped)
	st.SetActiveMedia(nil)

	assert.Equal(t, models.MediaStopReasonStopped, observer.lastEnd(t).Reason)
}

func TestStop_GateHoldCountsAsInProgressAndCompletesAsAStop(t *testing.T) {
	t.Parallel()
	st, _ := newObservedState(t)

	require.False(t, st.StopInProgress())
	startUser, startEpoch := st.UserStopSeq(), st.StopEpoch()

	release, err := st.AcquireMediaStop(t.Context())
	require.NoError(t, err)
	assert.True(t, st.StopInProgress())
	st.RecordMediaStop(models.MediaStopReasonStopped) // the stop itself, once it begins
	assert.Equal(t, startUser+1, st.UserStopSeq())
	assert.Equal(t, startEpoch+1, st.StopEpoch())

	release()
	assert.False(t, st.StopInProgress())
	assert.Equal(t, startUser+1, st.UserStopSeq(), "an update issued during the stop survives its end")
	assert.Equal(t, startEpoch+2, st.StopEpoch(), "the engine's moves decided during it are")
}

func TestStop_UpdateGateIsNotAStop(t *testing.T) {
	t.Parallel()
	st, _ := newObservedState(t)

	before := st.StopEpoch()
	release, err := st.AcquireUpdateMediaGate(t.Context())
	require.NoError(t, err)
	assert.False(t, st.StopInProgress())
	release()
	assert.Equal(t, before, st.StopEpoch())
}
