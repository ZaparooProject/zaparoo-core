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

package proctracker

import (
	"context"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tracker := New()
	defer tracker.Stop()

	assert.NotNil(t, tracker.tracked)
	assert.NotNil(t, tracker.done)
}

func TestTrack_NonexistentProcess(t *testing.T) {
	t.Parallel()

	tracker := New()
	defer tracker.Stop()

	// Use a very high PID that's unlikely to exist
	err := tracker.Track(999999999, func(_ int) {})

	require.ErrorIs(t, err, ErrProcessNotFound)
}

func TestTrack_ProcessExit(t *testing.T) {
	t.Parallel()

	tracker := New()
	defer tracker.Stop()

	// Start a short-lived process
	cmd := exec.CommandContext(context.Background(), "sleep", "0.1")
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid

	exitCalled := make(chan int, 1)
	err := tracker.Track(pid, func(exitedPid int) {
		exitCalled <- exitedPid
	})
	require.NoError(t, err)

	// Wait for process to exit and callback to be called
	select {
	case exitedPid := <-exitCalled:
		assert.Equal(t, pid, exitedPid)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for exit callback")
	}
}

func TestTrack_ProcessKilled(t *testing.T) {
	t.Parallel()

	tracker := New()
	defer tracker.Stop()

	// Start a long-running process
	cmd := exec.CommandContext(context.Background(), "sleep", "60")
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid

	exitCalled := make(chan int, 1)
	err := tracker.Track(pid, func(exitedPid int) {
		exitCalled <- exitedPid
	})
	require.NoError(t, err)

	// Kill the process
	require.NoError(t, cmd.Process.Kill())

	// Wait for callback
	select {
	case exitedPid := <-exitCalled:
		assert.Equal(t, pid, exitedPid)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for exit callback")
	}
}

func TestTrack_DuplicateTracking(t *testing.T) {
	t.Parallel()

	tracker := New()
	defer tracker.Stop()

	// Start a process
	cmd := exec.CommandContext(context.Background(), "sleep", "60")
	require.NoError(t, cmd.Start())
	defer func() { _ = cmd.Process.Kill() }()

	pid := cmd.Process.Pid
	callback := func(_ int) {}

	// Track twice - should succeed without error
	require.NoError(t, tracker.Track(pid, callback))
	require.NoError(t, tracker.Track(pid, callback))
}

func TestUntrack(t *testing.T) {
	t.Parallel()

	tracker := New()
	defer tracker.Stop()

	// Start a process
	cmd := exec.CommandContext(context.Background(), "sleep", "60")
	require.NoError(t, cmd.Start())
	defer func() { _ = cmd.Process.Kill() }()

	pid := cmd.Process.Pid
	callbackCalled := atomic.Bool{}

	err := tracker.Track(pid, func(_ int) {
		callbackCalled.Store(true)
	})
	require.NoError(t, err)

	// Untrack before killing
	tracker.Untrack(pid)

	// Kill the process
	require.NoError(t, cmd.Process.Kill())

	// Wait a bit to ensure callback isn't called
	time.Sleep(200 * time.Millisecond)
	assert.False(t, callbackCalled.Load(), "callback should not be called after untrack")
}

func TestUntrack_NonexistentPid(t *testing.T) {
	t.Parallel()

	tracker := New()
	defer tracker.Stop()

	// Should not panic
	tracker.Untrack(999999999)
}

func TestStop(t *testing.T) {
	t.Parallel()

	tracker := New()

	// Start multiple processes
	cmds := make([]*exec.Cmd, 0, 3)
	for range 3 {
		cmd := exec.CommandContext(context.Background(), "sleep", "60")
		require.NoError(t, cmd.Start())
		cmds = append(cmds, cmd)

		err := tracker.Track(cmd.Process.Pid, func(_ int) {})
		require.NoError(t, err)
	}

	// Stop should clean up all tracking
	tracker.Stop()

	// Clean up processes
	for _, cmd := range cmds {
		_ = cmd.Process.Kill()
	}
}

func TestConcurrentTracking(t *testing.T) {
	t.Parallel()

	tracker := New()
	defer tracker.Stop()

	const numProcesses = 10
	var wg sync.WaitGroup
	exitCount := atomic.Int32{}

	for range numProcesses {
		cmd := exec.CommandContext(context.Background(), "sleep", "0.1")
		require.NoError(t, cmd.Start())
		pid := cmd.Process.Pid

		wg.Add(1)
		err := tracker.Track(pid, func(_ int) {
			exitCount.Add(1)
			wg.Done()
		})
		require.NoError(t, err)
	}

	// Wait for all processes to exit
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		assert.Equal(t, int32(numProcesses), exitCount.Load())
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout: only %d/%d processes detected", exitCount.Load(), numProcesses)
	}
}

func TestCheckPidfdSupport(t *testing.T) {
	t.Parallel()

	// This test just verifies the function doesn't panic
	// Actual support depends on kernel version
	result := checkPidfdSupport()
	t.Logf("pidfd_open support: %v", result)
}

func TestTrack_ProcessAlreadyExited(t *testing.T) {
	t.Parallel()

	tracker := New()
	defer tracker.Stop()

	// Start and immediately kill a process
	cmd := exec.CommandContext(context.Background(), "sleep", "60")
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid
	require.NoError(t, cmd.Process.Kill())
	_, _ = cmd.Process.Wait()

	// Small delay to ensure process is fully gone
	time.Sleep(50 * time.Millisecond)

	// Tracking should fail with ErrProcessNotFound
	err := tracker.Track(pid, func(_ int) {})
	// Either ErrProcessNotFound or no error (if we caught it just before it died)
	if err != nil {
		require.ErrorIs(t, err, ErrProcessNotFound)
	}
}

func TestTrack_MultipleCallbacksForSamePid(t *testing.T) {
	t.Parallel()

	tracker := New()
	defer tracker.Stop()

	// Start a process
	cmd := exec.CommandContext(context.Background(), "sleep", "0.1")
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid

	callback1Called := atomic.Bool{}
	callback2Called := atomic.Bool{}

	// First track
	require.NoError(t, tracker.Track(pid, func(_ int) {
		callback1Called.Store(true)
	}))

	// Second track with different callback - should be ignored (already tracking)
	require.NoError(t, tracker.Track(pid, func(_ int) {
		callback2Called.Store(true)
	}))

	// Wait for process to exit
	time.Sleep(300 * time.Millisecond)

	// Only the first callback should be called
	assert.True(t, callback1Called.Load(), "first callback should be called")
	assert.False(t, callback2Called.Load(), "second callback should not be called")
}

func TestTrack_VerifyProcessExistsWithKill(t *testing.T) {
	t.Parallel()

	// Verify that syscall.Kill with signal 0 works as expected
	cmd := exec.CommandContext(context.Background(), "sleep", "60")
	require.NoError(t, cmd.Start())
	defer func() { _ = cmd.Process.Kill() }()

	// Process should exist
	err := syscall.Kill(cmd.Process.Pid, 0)
	require.NoError(t, err)

	// Non-existent PID should fail
	err = syscall.Kill(999999999, 0)
	require.ErrorIs(t, err, syscall.ESRCH)
}
