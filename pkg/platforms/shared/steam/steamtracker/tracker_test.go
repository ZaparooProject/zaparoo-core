//go:build linux

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
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tracker := New(nil, nil)
	defer tracker.Stop()

	assert.NotNil(t, tracker.tracked)
	assert.NotNil(t, tracker.appIDToPID)
	assert.NotNil(t, tracker.procTracker)
	assert.Equal(t, DefaultPollInterval, tracker.pollInterval)
	assert.Equal(t, "/proc", tracker.procPath)
}

func TestNew_WithOptions(t *testing.T) {
	t.Parallel()

	tracker := New(nil, nil,
		WithPollInterval(5*time.Second),
		WithProcPath("/custom/proc"),
	)
	defer tracker.Stop()

	assert.Equal(t, 5*time.Second, tracker.pollInterval)
	assert.Equal(t, "/custom/proc", tracker.procPath)
}

func TestTracker_DetectsNewGame(t *testing.T) {
	t.Parallel()

	procDir := t.TempDir()

	var startCalled atomic.Bool
	var gotAppID, gotPID atomic.Int32

	tracker := New(
		func(appID int, pid int, _ string) {
			gotAppID.Store(int32(appID)) //nolint:gosec // G115: appID is always small
			gotPID.Store(int32(pid))     //nolint:gosec // G115: pid is always small
			startCalled.Store(true)
		},
		nil,
		WithProcPath(procDir),
		WithPollInterval(50*time.Millisecond),
	)

	// Start tracker (it will do an initial scan)
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	// Initial scan should find nothing (no mock processes yet)
	assert.False(t, startCalled.Load())

	// Add a reaper process
	createMockProcess(t, procDir, 12345, "reaper",
		"/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=348550\x00--\x00game.exe")

	// Wait for poll cycle to detect the game
	require.Eventually(t, startCalled.Load, time.Second, 10*time.Millisecond, "callback should be called")

	assert.Equal(t, int32(348550), gotAppID.Load())
	assert.Equal(t, int32(12345), gotPID.Load())
}

func TestTracker_TrackedGames(t *testing.T) {
	t.Parallel()

	procDir := t.TempDir()

	// Create a reaper process before starting tracker
	createMockProcess(t, procDir, 12345, "reaper",
		"/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=100\x00--\x00game")
	createMockProcess(t, procDir, 12346, "reaper",
		"/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=200\x00--\x00game")

	tracker := New(nil, nil,
		WithProcPath(procDir),
		WithPollInterval(50*time.Millisecond),
	)

	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	// Wait for initial scan to detect games
	require.Eventually(t, func() bool {
		return len(tracker.TrackedGames()) == 2
	}, time.Second, 10*time.Millisecond, "should detect 2 games")

	games := tracker.TrackedGames()

	appIDs := make(map[int]bool)
	for _, g := range games {
		appIDs[g.AppID] = true
	}
	assert.True(t, appIDs[100])
	assert.True(t, appIDs[200])
}

func TestTracker_DeduplicatesByAppID(t *testing.T) {
	t.Parallel()

	procDir := t.TempDir()

	startCount := atomic.Int32{}

	tracker := New(
		func(_ int, _ int, _ string) {
			startCount.Add(1)
		},
		nil,
		WithProcPath(procDir),
		WithPollInterval(50*time.Millisecond),
	)

	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	// Add same AppID with different PIDs (shouldn't happen in real life, but test dedup)
	createMockProcess(t, procDir, 12345, "reaper",
		"/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=100\x00--\x00game")

	// Wait for first detection
	require.Eventually(t, func() bool {
		return startCount.Load() == 1
	}, time.Second, 10*time.Millisecond, "first game should be detected")

	// Add another process with same AppID
	createMockProcess(t, procDir, 12346, "reaper",
		"/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=100\x00--\x00game")

	// Verify dedup: callback count should NOT increase (stays at 1)
	// Use Never to assert the count doesn't go above 1
	require.Never(t, func() bool {
		return startCount.Load() > 1
	}, 150*time.Millisecond, 10*time.Millisecond, "should dedupe by AppID")
}

func TestTracker_StopCallback(t *testing.T) {
	t.Parallel()

	// This test is tricky because we can't easily simulate process exit
	// with a mock /proc filesystem. We'll test that the callback plumbing works.

	procDir := t.TempDir()

	// Create mock reaper BEFORE starting tracker so initial scan finds it
	pidDir := filepath.Join(procDir, "12345")
	//nolint:gosec // G301: test directory permissions are fine
	require.NoError(t, os.Mkdir(pidDir, 0o755))
	//nolint:gosec // G306: test file permissions are fine
	require.NoError(t, os.WriteFile(filepath.Join(pidDir, "comm"), []byte("reaper\n"), 0o644))
	//nolint:gosec // G306: test file permissions are fine
	require.NoError(t, os.WriteFile(filepath.Join(pidDir, "cmdline"),
		[]byte("/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=100\x00--\x00game"), 0o644))

	stopCalled := make(chan int, 1)

	tracker := New(
		nil,
		func(appID int) {
			stopCalled <- appID
		},
		WithProcPath(procDir),
		WithPollInterval(500*time.Millisecond),
	)

	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	// Wait for tracker to detect the game (polls until true or timeout)
	require.Eventually(t, func() bool {
		return len(tracker.TrackedGames()) == 1
	}, time.Second, 10*time.Millisecond, "tracker should detect the game")

	// "Kill" the process by removing its /proc entry before handleGameExit
	require.NoError(t, os.RemoveAll(pidDir))

	// Call handleGameExit directly to test the callback path
	tracker.handleGameExit(12345, 100)

	// Wait for callback (it's called in a goroutine)
	select {
	case appID := <-stopCalled:
		assert.Equal(t, 100, appID)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for stop callback")
	}

	// Should be removed from tracked (procDir entry deleted, no re-add)
	games := tracker.TrackedGames()
	assert.Empty(t, games)
}

func TestTracker_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	procDir := t.TempDir()

	tracker := New(nil, nil,
		WithProcPath(procDir),
		WithPollInterval(10*time.Millisecond),
	)

	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	// Pre-create mock processes (must be done in main goroutine due to require)
	for i := range 10 {
		createMockProcess(t, procDir, 10000+i, "reaper",
			"/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId="+
				string(rune('0'+i/10))+string(rune('0'+i%10))+"\x00--\x00game")
	}

	var wg sync.WaitGroup

	// Concurrent reads while tracker is polling
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_ = tracker.TrackedGames()
			}
		}()
	}

	wg.Wait()
}

func TestTracker_NilCallbacks(t *testing.T) {
	t.Parallel()

	procDir := t.TempDir()

	// Should not panic with nil callbacks
	tracker := New(nil, nil,
		WithProcPath(procDir),
		WithPollInterval(50*time.Millisecond),
	)

	require.NoError(t, tracker.Start())

	// Add a reaper
	createMockProcess(t, procDir, 12345, "reaper",
		"/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=100\x00--\x00game")

	// Wait for game to be tracked
	require.Eventually(t, func() bool {
		return len(tracker.TrackedGames()) == 1
	}, time.Second, 10*time.Millisecond, "game should be tracked")

	// Trigger exit callback path
	tracker.handleGameExit(12345, 100)

	tracker.Stop()
}
