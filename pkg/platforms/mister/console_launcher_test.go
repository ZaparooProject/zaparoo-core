//go:build linux && !android

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

package mister

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testConsoleManager is a simple mock console manager for testing
type testConsoleManager struct {
	openErr     error
	closeErr    error
	cleanErr    error
	restoreErr  error
	openVT      string
	openCalled  bool
	closeCalled bool
}

func (m *testConsoleManager) Open(_ context.Context, vt string) error {
	m.openCalled = true
	m.openVT = vt
	return m.openErr
}

func (m *testConsoleManager) Close() error {
	m.closeCalled = true
	return m.closeErr
}

func (m *testConsoleManager) Clean(_ string) error {
	return m.cleanErr
}

func (m *testConsoleManager) Restore(_ string) error {
	return m.restoreErr
}

func TestRunTrackedProcess_InvalidCommand(t *testing.T) {
	// Cannot use t.Parallel() because we're testing with actual Platform instance

	pl := &Platform{
		consoleManager: newConsoleManager(&Platform{}),
	}
	ctx := context.Background()

	// Create a command that will fail to start (invalid executable)
	cmd := exec.CommandContext(ctx, "/nonexistent/command")

	restoreFunc := func() {
		// Simple restore function for test
		pl.consoleManager.mu.Lock()
		pl.consoleManager.active = false
		pl.consoleManager.mu.Unlock()
	}

	process, err := runTrackedProcess(pl, cmd, restoreFunc, "test", trackedExit{})

	// Should get an error from starting invalid command
	require.Error(t, err)
	assert.Nil(t, process)

	// Verify restore was called (active flag cleared)
	pl.consoleManager.mu.RLock()
	isActive := pl.consoleManager.active
	pl.consoleManager.mu.RUnlock()
	assert.False(t, isActive)
}

func TestRunTrackedProcess_QuickExit(t *testing.T) {
	// Cannot use t.Parallel() because we're testing with actual Platform instance

	pl := &Platform{
		consoleManager: newConsoleManager(&Platform{}),
	}
	ctx := context.Background()

	// Create a command that exits immediately
	cmd := exec.CommandContext(ctx, "true")

	var restoreCalled bool
	var mu syncutil.Mutex
	restoreFunc := func() {
		mu.Lock()
		restoreCalled = true
		mu.Unlock()
		pl.consoleManager.mu.Lock()
		pl.consoleManager.active = false
		pl.consoleManager.mu.Unlock()
	}

	process, err := runTrackedProcess(pl, cmd, restoreFunc, "test", trackedExit{})

	// Should start successfully
	require.NoError(t, err)
	require.NotNil(t, process)

	// Wait for cleanup goroutine to run
	time.Sleep(100 * time.Millisecond)

	// Verify restore was called
	mu.Lock()
	called := restoreCalled
	mu.Unlock()
	assert.True(t, called, "Restore function should be called after process exits")

	// Verify tracked process was cleared
	pl.processMu.RLock()
	trackedProc := pl.trackedProcess
	pl.processMu.RUnlock()
	assert.Nil(t, trackedProc)
}

func TestRunTrackedProcess_CancelledContext(t *testing.T) {
	// Cannot use t.Parallel() because we're testing with actual Platform instance

	pl := &Platform{
		consoleManager: newConsoleManager(&Platform{}),
	}

	// Create a command that would run
	cmd := exec.CommandContext(context.Background(), "sleep", "10")

	var restoreCalled bool
	var mu syncutil.Mutex
	restoreFunc := func() {
		mu.Lock()
		restoreCalled = true
		mu.Unlock()
	}

	process, err := runTrackedProcess(pl, cmd, restoreFunc, "test", trackedExit{})

	// Should start successfully
	require.NoError(t, err)
	require.NotNil(t, process)

	// Kill the process so cleanup goroutine runs
	_ = process.Kill()

	// Wait for cleanup goroutine to complete
	time.Sleep(100 * time.Millisecond)

	// Restore MUST be called even when the launcher is preempted
	// This is CRITICAL: console state (VT, cursor, video mode) must be restored
	// regardless of whether the launcher was superseded by a new launch
	mu.Lock()
	called := restoreCalled
	mu.Unlock()
	assert.True(t, called, "Restore must always be called (fixes black screen bug)")
}

func TestSetupConsoleEnvironment_CancelledContext(t *testing.T) {
	// Cannot use t.Parallel() because we're testing with actual Platform instance

	pl := &Platform{
		consoleManager: newConsoleManager(&Platform{}),
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// setupConsoleEnvironment should detect cancelled context during cm.Open()
	cm, err := setupConsoleEnvironment(ctx, pl)

	// Should get context.Canceled error
	require.Error(t, err)
	assert.Nil(t, cm)
}

func TestSetupConsoleManager_UsesArmVTAndCentralizedClose(t *testing.T) {
	t.Parallel()

	cm := &testConsoleManager{}
	configured, err := setupConsoleManager(context.Background(), cm)
	require.NoError(t, err)
	assert.Same(t, cm, configured)
	assert.Equal(t, armLauncherVT, cm.openVT)

	restore := createConsoleRestoreFunc(cm)
	restore()
	assert.True(t, cm.closeCalled)
}

func TestSetupConsoleManager_OpenFailure(t *testing.T) {
	t.Parallel()

	cm := &testConsoleManager{openErr: assert.AnError}
	configured, err := setupConsoleManager(context.Background(), cm)
	require.ErrorIs(t, err, assert.AnError)
	assert.Nil(t, configured)
	assert.False(t, cm.closeCalled)
}

func TestSetupConsoleManager_CleanFailureClosesConsole(t *testing.T) {
	t.Parallel()

	cm := &testConsoleManager{cleanErr: assert.AnError}
	configured, err := setupConsoleManager(context.Background(), cm)
	require.ErrorIs(t, err, assert.AnError)
	assert.Nil(t, configured)
	assert.True(t, cm.closeCalled)
}

func TestSetupConsoleManager_CleanAndCloseFailuresAreJoined(t *testing.T) {
	t.Parallel()

	cleanErr := errors.New("clean failed")
	closeErr := errors.New("close failed")
	cm := &testConsoleManager{cleanErr: cleanErr, closeErr: closeErr}
	configured, err := setupConsoleManager(context.Background(), cm)
	require.ErrorIs(t, err, cleanErr)
	require.ErrorIs(t, err, closeErr)
	assert.Nil(t, configured)
	assert.True(t, cm.closeCalled)
}

func TestConsoleManager_ErrorPropagation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		openErr    error
		closeErr   error
		cleanErr   error
		restoreErr error
		name       string
	}{
		{
			name:    "Open returns error",
			openErr: errors.New("open failed"),
		},
		{
			name:     "Close returns error",
			closeErr: errors.New("close failed"),
		},
		{
			name:     "Clean returns error",
			cleanErr: errors.New("clean failed"),
		},
		{
			name:       "Restore returns error",
			restoreErr: errors.New("restore failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockCM := &testConsoleManager{
				openErr:    tt.openErr,
				closeErr:   tt.closeErr,
				cleanErr:   tt.cleanErr,
				restoreErr: tt.restoreErr,
			}

			// Test that methods return expected errors
			ctx := context.Background()
			if tt.openErr != nil {
				err := mockCM.Open(ctx, "7")
				assert.Equal(t, tt.openErr, err)
			}
			if tt.closeErr != nil {
				err := mockCM.Close()
				assert.Equal(t, tt.closeErr, err)
			}
			if tt.cleanErr != nil {
				err := mockCM.Clean("1")
				assert.Equal(t, tt.cleanErr, err)
			}
			if tt.restoreErr != nil {
				err := mockCM.Restore("1")
				assert.Equal(t, tt.restoreErr, err)
			}
		})
	}
}

type exitTestPlatform struct {
	pl      *Platform
	media   *models.ActiveMedia
	cleared int
	mu      syncutil.Mutex
}

func newExitTestPlatform(launcherID string) *exitTestPlatform {
	et := &exitTestPlatform{media: &models.ActiveMedia{LauncherID: launcherID}}
	et.pl = &Platform{
		consoleManager: newConsoleManager(&Platform{}),
		activeMedia: func() *models.ActiveMedia {
			et.mu.Lock()
			defer et.mu.Unlock()
			return et.media
		},
		setActiveMedia: func(m *models.ActiveMedia) {
			et.mu.Lock()
			defer et.mu.Unlock()
			if m == nil {
				et.cleared++
				et.media = nil
			}
		},
	}
	return et
}

func (et *exitTestPlatform) clearedCount() int {
	et.mu.Lock()
	defer et.mu.Unlock()
	return et.cleared
}

func (et *exitTestPlatform) waitUntrackedAfter(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		et.pl.processMu.RLock()
		defer et.pl.processMu.RUnlock()
		return et.pl.trackedProcess == nil
	}, 5*time.Second, 5*time.Millisecond)
}

func TestRunTrackedProcess_NaturalExitClearsMedia(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  string
		args []string
	}{
		{"clean exit", "true", nil},
		{"crash exit", "false", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			et := newExitTestPlatform("GenericVideo")
			cmd := exec.CommandContext(context.Background(), tc.cmd, tc.args...) //nolint:gosec // fixed test commands
			_, err := runTrackedProcess(et.pl, cmd, func() {}, "test", trackedExit{launcherID: "GenericVideo"})
			require.NoError(t, err)
			et.waitUntrackedAfter(t)
			assert.Equal(t, 1, et.clearedCount())
		})
	}
}

func TestRunTrackedProcess_NaturalExitLeavesOtherLauncherMedia(t *testing.T) {
	et := newExitTestPlatform("Other")
	cmd := exec.CommandContext(context.Background(), "true")
	_, err := runTrackedProcess(et.pl, cmd, func() {}, "test", trackedExit{launcherID: "GenericVideo"})
	require.NoError(t, err)
	et.waitUntrackedAfter(t)
	assert.Zero(t, et.clearedCount())
}

func TestRunTrackedProcess_StoppedByCoreDoesNotClearMedia(t *testing.T) {
	et := newExitTestPlatform("GenericVideo")
	finished := make(chan struct{}, 1)
	cmd := exec.CommandContext(context.Background(), "sleep", "10")
	proc, err := runTrackedProcess(et.pl, cmd, func() {}, "test", trackedExit{
		launcherID:    "GenericVideo",
		mediaFinished: func() { finished <- struct{}{} },
	})
	require.NoError(t, err)
	et.pl.processMu.Lock()
	et.pl.stopRequestedProc = proc
	et.pl.processMu.Unlock()
	require.NoError(t, proc.Signal(syscall.SIGTERM))
	et.waitUntrackedAfter(t)
	assert.Zero(t, et.clearedCount())
	select {
	case <-finished:
		t.Fatal("mediaFinished called for a Core-initiated stop")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRunTrackedProcess_MediaFinishedOnlyOnCleanExit(t *testing.T) {
	t.Run("clean exit finishes instead of clearing", func(t *testing.T) {
		et := newExitTestPlatform("GenericVideo")
		finished := make(chan struct{}, 1)
		cmd := exec.CommandContext(context.Background(), "true")
		_, err := runTrackedProcess(et.pl, cmd, func() {}, "test", trackedExit{
			launcherID:    "GenericVideo",
			mediaFinished: func() { finished <- struct{}{} },
		})
		require.NoError(t, err)
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatal("mediaFinished not called")
		}
		assert.Zero(t, et.clearedCount(), "Core clears media itself when finished")
	})

	t.Run("failed exit clears without finishing", func(t *testing.T) {
		et := newExitTestPlatform("GenericVideo")
		finished := make(chan struct{}, 1)
		cmd := exec.CommandContext(context.Background(), "false")
		_, err := runTrackedProcess(et.pl, cmd, func() {}, "test", trackedExit{
			launcherID:    "GenericVideo",
			mediaFinished: func() { finished <- struct{}{} },
		})
		require.NoError(t, err)
		et.waitUntrackedAfter(t)
		assert.Equal(t, 1, et.clearedCount())
		assert.Empty(t, finished)
	})
}

func TestStopActiveLauncher_WatcherDoesNotClearMediaAgain(t *testing.T) {
	et := newExitTestPlatform("GenericVideo")
	cmd := exec.CommandContext(context.Background(), "sleep", "10")
	proc, err := runTrackedProcess(et.pl, cmd, func() {}, "test", trackedExit{launcherID: "GenericVideo"})
	require.NoError(t, err)
	require.NoError(t, et.pl.StopActiveLauncher(platforms.StopForPreemption))
	assert.False(t, et.pl.isTrackedProcess(proc))
	assert.Equal(t, 1, et.clearedCount(), "only the stop path clears media, not the exit watcher")
}
