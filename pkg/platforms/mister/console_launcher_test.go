//go:build linux

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	process, err := runTrackedProcess(ctx, pl, cmd, restoreFunc, "test")

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
	var mu sync.Mutex
	restoreFunc := func() {
		mu.Lock()
		restoreCalled = true
		mu.Unlock()
		pl.consoleManager.mu.Lock()
		pl.consoleManager.active = false
		pl.consoleManager.mu.Unlock()
	}

	process, err := runTrackedProcess(ctx, pl, cmd, restoreFunc, "test")

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
	assert.Nil(t, pl.trackedProcess)
}

func TestRunTrackedProcess_CancelledContext(t *testing.T) {
	// Cannot use t.Parallel() because we're testing with actual Platform instance

	pl := &Platform{
		consoleManager: newConsoleManager(&Platform{}),
	}

	// Create context that will be cancelled during execution
	ctx, cancel := context.WithCancel(context.Background())

	// Create a command that would run (use background context for command)
	cmd := exec.CommandContext(context.Background(), "sleep", "10")

	var restoreCalled bool
	var mu sync.Mutex
	restoreFunc := func() {
		mu.Lock()
		restoreCalled = true
		mu.Unlock()
	}

	process, err := runTrackedProcess(ctx, pl, cmd, restoreFunc, "test")

	// Should start successfully
	require.NoError(t, err)
	require.NotNil(t, process)

	// Cancel the launcher context (simulating launcher supersession)
	cancel()

	// Kill the process so cleanup goroutine runs
	_ = process.Kill()

	// Wait for cleanup goroutine to detect cancellation
	time.Sleep(100 * time.Millisecond)

	// Restore should NOT be called because launcher context was cancelled
	mu.Lock()
	called := restoreCalled
	mu.Unlock()
	assert.False(t, called, "Restore should not be called when launcher context is cancelled")
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
