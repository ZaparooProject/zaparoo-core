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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

	var startCalled atomic.Bool
	var gotAppID atomic.Int32

	tracker := New(
		func(appID int, _ int, _ string) {
			gotAppID.Store(int32(appID)) //nolint:gosec // G115: appID is always small
			startCalled.Store(true)
		},
		nil,
	)

	// Simulate registry callback for game start
	tracker.onAppIDChange(12345)

	assert.True(t, startCalled.Load())
	assert.Equal(t, int32(12345), gotAppID.Load())

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
		func(appID int) {
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

	startCount := atomic.Int32{}

	tracker := New(
		func(_ int, _ int, _ string) {
			startCount.Add(1)
		},
		nil,
	)

	// Simulate game start
	tracker.onAppIDChange(12345)
	assert.Equal(t, int32(1), startCount.Load())

	// Simulate same game again (shouldn't trigger callback)
	tracker.onAppIDChange(12345)
	assert.Equal(t, int32(1), startCount.Load())
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
	assert.NoError(t, err)
	assert.Equal(t, 0, appID)
}

func TestIsSteamInstalled(t *testing.T) {
	// This just verifies the function doesn't panic
	// The actual result depends on whether Steam is installed
	_ = IsSteamInstalled()
}
