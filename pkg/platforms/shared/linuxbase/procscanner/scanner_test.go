//go:build linux

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

package procscanner

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	scanner := New()
	defer scanner.Stop()

	assert.NotNil(t, scanner.watchers)
	assert.NotNil(t, scanner.procTracker)
	assert.NotNil(t, scanner.tracked)
	assert.Equal(t, DefaultPollInterval, scanner.pollInterval)
	assert.Equal(t, "/proc", scanner.procPath)
}

func TestNew_WithOptions(t *testing.T) {
	t.Parallel()

	scanner := New(
		WithPollInterval(5*time.Second),
		WithProcPath("/custom/proc"),
	)
	defer scanner.Stop()

	assert.Equal(t, 5*time.Second, scanner.pollInterval)
	assert.Equal(t, "/custom/proc", scanner.procPath)
}

func TestScanner_Watch(t *testing.T) {
	t.Parallel()

	scanner := New()
	defer scanner.Stop()

	matcher := NewCommMatcher([]string{"test"})
	id := scanner.Watch(matcher, Callbacks{})

	assert.Equal(t, WatchID(1), id)
	assert.Contains(t, scanner.watchers, id)
}

func TestScanner_Unwatch(t *testing.T) {
	t.Parallel()

	scanner := New()
	defer scanner.Stop()

	matcher := NewCommMatcher([]string{"test"})
	id := scanner.Watch(matcher, Callbacks{})

	scanner.Unwatch(id)
	assert.NotContains(t, scanner.watchers, id)
}

func TestScanner_DetectsProcess(t *testing.T) {
	t.Parallel()

	procDir := t.TempDir()

	var startCalled atomic.Bool
	var gotPID atomic.Int32
	var gotComm atomic.Value

	scanner := New(
		WithProcPath(procDir),
		WithPollInterval(50*time.Millisecond),
	)

	scanner.Watch(
		NewCommMatcher([]string{"testproc"}),
		Callbacks{
			OnStart: func(proc ProcessInfo) {
				gotPID.Store(int32(proc.PID)) //nolint:gosec // G115: pid is small
				gotComm.Store(proc.Comm)
				startCalled.Store(true)
			},
		},
	)

	require.NoError(t, scanner.Start())
	defer scanner.Stop()

	// Initial scan should find nothing
	assert.False(t, startCalled.Load())

	// Add a mock process
	createMockProcess(t, procDir, 12345, "testproc", "test command")

	// Wait for poll cycle to detect the process
	require.Eventually(t, startCalled.Load, time.Second, 10*time.Millisecond,
		"callback should be called")

	assert.Equal(t, int32(12345), gotPID.Load())
	assert.Equal(t, "testproc", gotComm.Load())
}

func TestScanner_MultipleWatchers(t *testing.T) {
	t.Parallel()

	procDir := t.TempDir()

	var watcher1Called, watcher2Called atomic.Bool

	scanner := New(
		WithProcPath(procDir),
		WithPollInterval(50*time.Millisecond),
	)

	scanner.Watch(
		NewCommMatcher([]string{"proc1"}),
		Callbacks{
			OnStart: func(_ ProcessInfo) {
				watcher1Called.Store(true)
			},
		},
	)

	scanner.Watch(
		NewCommMatcher([]string{"proc2"}),
		Callbacks{
			OnStart: func(_ ProcessInfo) {
				watcher2Called.Store(true)
			},
		},
	)

	require.NoError(t, scanner.Start())
	defer scanner.Stop()

	// Add processes for both watchers
	createMockProcess(t, procDir, 12345, "proc1", "cmd1")
	createMockProcess(t, procDir, 12346, "proc2", "cmd2")

	// Wait for both to be detected
	require.Eventually(t, watcher1Called.Load, time.Second, 10*time.Millisecond,
		"watcher1 should be called")
	require.Eventually(t, watcher2Called.Load, time.Second, 10*time.Millisecond,
		"watcher2 should be called")
}

func TestScanner_SameProcessMatchesMultipleWatchers(t *testing.T) {
	t.Parallel()

	procDir := t.TempDir()

	var watcher1Called, watcher2Called atomic.Bool

	scanner := New(
		WithProcPath(procDir),
		WithPollInterval(50*time.Millisecond),
	)

	// Both watchers match the same process
	scanner.Watch(
		NewCommMatcher([]string{"testproc"}),
		Callbacks{
			OnStart: func(_ ProcessInfo) {
				watcher1Called.Store(true)
			},
		},
	)

	scanner.Watch(
		NewCommMatcher([]string{"testproc"}),
		Callbacks{
			OnStart: func(_ ProcessInfo) {
				watcher2Called.Store(true)
			},
		},
	)

	require.NoError(t, scanner.Start())
	defer scanner.Stop()

	// Add one process that matches both
	createMockProcess(t, procDir, 12345, "testproc", "cmd")

	// Both should be called
	require.Eventually(t, watcher1Called.Load, time.Second, 10*time.Millisecond,
		"watcher1 should be called")
	require.Eventually(t, watcher2Called.Load, time.Second, 10*time.Millisecond,
		"watcher2 should be called")
}

func TestScanner_DoesNotDuplicateDetection(t *testing.T) {
	t.Parallel()

	procDir := t.TempDir()

	callCount := atomic.Int32{}

	scanner := New(
		WithProcPath(procDir),
		WithPollInterval(50*time.Millisecond),
	)

	scanner.Watch(
		NewCommMatcher([]string{"testproc"}),
		Callbacks{
			OnStart: func(_ ProcessInfo) {
				callCount.Add(1)
			},
		},
	)

	// Create process before starting
	createMockProcess(t, procDir, 12345, "testproc", "cmd")

	require.NoError(t, scanner.Start())
	defer scanner.Stop()

	// Wait for first detection
	require.Eventually(t, func() bool {
		return callCount.Load() == 1
	}, time.Second, 10*time.Millisecond, "should detect once")

	// Wait a few more poll cycles
	time.Sleep(150 * time.Millisecond)

	// Should still only be called once
	assert.Equal(t, int32(1), callCount.Load(), "should not duplicate")
}

func TestScanner_MatcherFunc(t *testing.T) {
	t.Parallel()

	procDir := t.TempDir()

	var called atomic.Bool

	scanner := New(
		WithProcPath(procDir),
		WithPollInterval(50*time.Millisecond),
	)

	// Use MatcherFunc adapter
	scanner.Watch(
		MatcherFunc(func(proc ProcessInfo) bool {
			return proc.Comm == "custom"
		}),
		Callbacks{
			OnStart: func(_ ProcessInfo) {
				called.Store(true)
			},
		},
	)

	require.NoError(t, scanner.Start())
	defer scanner.Stop()

	createMockProcess(t, procDir, 12345, "custom", "cmd")

	require.Eventually(t, called.Load, time.Second, 10*time.Millisecond,
		"callback should be called")
}

// createMockProcess creates a mock /proc/<pid>/ directory with comm and cmdline.
func createMockProcess(t *testing.T, procDir string, pid int, comm, cmdline string) {
	t.Helper()

	pidDir := filepath.Join(procDir, intToStr(pid))

	err := os.MkdirAll(pidDir, 0o750)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(pidDir, "comm"), []byte(comm+"\n"), 0o600)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(pidDir, "cmdline"), []byte(cmdline), 0o600)
	require.NoError(t, err)
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
