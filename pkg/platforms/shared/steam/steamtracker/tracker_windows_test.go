//go:build windows

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

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

package steamtracker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_Windows(t *testing.T) {
	t.Parallel()

	tracker := New(nil, nil)
	assert.NotNil(t, tracker.tracked)
}

func TestTracker_TrackedGames_Windows(t *testing.T) {
	t.Parallel()

	tracker := New(nil, nil)

	// Initially empty
	games := tracker.TrackedGames()
	assert.Empty(t, games)
}

func TestTracker_OnAppIDChange_GameStart(t *testing.T) {
	t.Parallel()

	startCalled := make(chan int, 1)

	tracker := New(
		func(appID int, _ int, _ string) {
			startCalled <- appID
		},
		nil,
	)

	// Simulate registry callback for game start
	tracker.onAppIDChange(12345)

	// Wait for async callback
	select {
	case appID := <-startCalled:
		assert.Equal(t, 12345, appID)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for start callback")
	}

	// Verify game is tracked
	games := tracker.TrackedGames()
	assert.Len(t, games, 1)
	assert.Equal(t, 12345, games[0].AppID)
}

func TestTracker_OnAppIDChange_GameStop(t *testing.T) {
	t.Parallel()

	stopCalled := make(chan int, 1)

	tracker := New(
		nil,
		func(appID, _ int) {
			stopCalled <- appID
		},
	)

	// Simulate game start
	tracker.onAppIDChange(12345)

	// Verify game is tracked
	games := tracker.TrackedGames()
	assert.Len(t, games, 1)

	// Simulate game stop (appID = 0)
	tracker.onAppIDChange(0)

	// Verify callback was called
	select {
	case appID := <-stopCalled:
		assert.Equal(t, 12345, appID)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for stop callback")
	}

	// Verify game is no longer tracked
	games = tracker.TrackedGames()
	assert.Empty(t, games)
}

func TestTracker_OnAppIDChange_DeduplicatesByAppID(t *testing.T) {
	t.Parallel()

	startCalled := make(chan int, 2)

	tracker := New(
		func(appID int, _ int, _ string) {
			startCalled <- appID
		},
		nil,
	)

	// Simulate game start
	tracker.onAppIDChange(12345)

	// Wait for first callback
	select {
	case <-startCalled:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for first start callback")
	}

	// Simulate same game again (shouldn't trigger callback)
	tracker.onAppIDChange(12345)

	// Brief wait to ensure no second callback
	select {
	case <-startCalled:
		t.Fatal("callback should not be called for duplicate appID")
	case <-time.After(100 * time.Millisecond):
		// Expected - no callback for duplicate
	}
}

func TestTracker_OnAppIDChange_NilCallbacks(t *testing.T) {
	t.Parallel()

	// Should not panic with nil callbacks
	tracker := New(nil, nil)

	// Simulate game start and stop
	tracker.onAppIDChange(12345)
	tracker.onAppIDChange(0)

	// If we reach here without panic, test passes
}

func TestRegistryWatcher_NewRegistryWatcher(t *testing.T) {
	t.Parallel()

	called := false
	watcher := NewRegistryWatcher(func(_ int) {
		called = true
	})

	assert.NotNil(t, watcher)
	assert.NotNil(t, watcher.onChange)
	assert.NotNil(t, watcher.done)
	assert.False(t, called)
}

func TestGetRunningAppID_NoSteam(t *testing.T) {
	// Skip if Steam is installed (this test is for when Steam is NOT installed)
	if IsSteamInstalled() {
		t.Skip("skipping test: Steam is installed")
	}

	appID, err := GetRunningAppID()
	require.NoError(t, err)
	assert.Equal(t, 0, appID)
}

func TestIsSteamInstalled(_ *testing.T) {
	// This just verifies the function doesn't panic
	// The actual result depends on whether Steam is installed
	_ = IsSteamInstalled()
}

func TestTracker_OnAppIDChange_SwitchingGamesStopsPrevious(t *testing.T) {
	t.Parallel()

	stopped := make(chan int, 2)
	started := make(chan int, 2)

	tracker := New(
		func(appID, _ int, _ string) { started <- appID },
		func(appID, _ int) { stopped <- appID },
	)

	tracker.onAppIDChange(111)
	requireChanValue(t, started, 111)

	// Steam moves straight from one game to another without publishing zero
	// in between. The first game must still be reported as stopped, or it is
	// left tracked forever and the switch back is swallowed as "already
	// tracked".
	tracker.onAppIDChange(222)
	requireChanValue(t, stopped, 111)
	requireChanValue(t, started, 222)

	games := tracker.TrackedGames()
	require.Len(t, games, 1)
	assert.Equal(t, 222, games[0].AppID)
}

func TestTracker_OnAppIDChange_SwitchingBackReportsBothTransitions(t *testing.T) {
	t.Parallel()

	stopped := make(chan int, 2)
	started := make(chan int, 2)

	tracker := New(
		func(appID, _ int, _ string) { started <- appID },
		func(appID, _ int) { stopped <- appID },
	)

	tracker.onAppIDChange(111)
	requireChanValue(t, started, 111)
	tracker.onAppIDChange(222)
	requireChanValue(t, stopped, 111)
	requireChanValue(t, started, 222)

	// Steam reverts to the first game once the second exits.
	tracker.onAppIDChange(111)
	requireChanValue(t, stopped, 222)
	requireChanValue(t, started, 111)

	games := tracker.TrackedGames()
	require.Len(t, games, 1)
	assert.Equal(t, 111, games[0].AppID)
}

func TestTracker_OnAppIDChange_RepeatedSameAppIDIsIdempotent(t *testing.T) {
	t.Parallel()

	started := make(chan int, 4)
	// Stop callbacks run on their own goroutine, so record rather than fail
	// from inside one: a t.Error after the test returns panics instead of
	// reporting a readable failure.
	stopped := make(chan int, 4)
	tracker := New(
		func(appID, _ int, _ string) { started <- appID },
		func(appID, _ int) { stopped <- appID },
	)

	tracker.onAppIDChange(999)
	requireChanValue(t, started, 999)
	tracker.onAppIDChange(999)

	select {
	case appID := <-started:
		t.Fatalf("unexpected second start callback for appID %d", appID)
	case <-time.After(200 * time.Millisecond):
	}

	assert.Empty(t, stopped, "a repeated AppID must not report a stop")
	assert.Len(t, tracker.TrackedGames(), 1)
}

func TestTracker_StopIsCleanWhileWaitingForSteam(t *testing.T) {
	// Not parallel: shortens a package-level interval.
	original := steamInstallPollInterval
	steamInstallPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { steamInstallPollInterval = original })

	tracker := New(nil, nil)
	// Start either attaches a watcher or begins waiting for Steam to appear;
	// either way Stop must return promptly rather than waiting out a poll.
	// The package's goleak TestMain covers the "no goroutine left behind" half.
	require.NoError(t, tracker.Start())

	done := make(chan struct{})
	go func() {
		tracker.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for tracker to stop")
	}
}

// requireChanValue asserts the next value on ch equals want.
func requireChanValue(t *testing.T, ch <-chan int, want int) {
	t.Helper()
	select {
	case got := <-ch:
		require.Equal(t, want, got)
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for value %d", want)
	}
}
