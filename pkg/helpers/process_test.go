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

package helpers

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getShortLivedCommand returns a command that exits immediately with code 0.
func getShortLivedCommand(ctx context.Context) *exec.Cmd {
	if runtime.GOOS == "windows" {
		// cmd /c exit 0 exits immediately with code 0
		return exec.CommandContext(ctx, "cmd", "/c", "exit", "0")
	}
	// 'true' command exits immediately with code 0 on Unix
	return exec.CommandContext(ctx, "true")
}

// getLongRunningCommand returns a command that runs for ~10 seconds.
func getLongRunningCommand(ctx context.Context) *exec.Cmd {
	if runtime.GOOS == "windows" {
		// ping with count 11 runs for ~10 seconds (1 second per ping)
		return exec.CommandContext(ctx, "ping", "-n", "11", "127.0.0.1")
	}
	// sleep for 10 seconds on Unix
	return exec.CommandContext(ctx, "sleep", "10")
}

func TestIsProcessRunning(t *testing.T) {
	t.Parallel()

	t.Run("nil process returns false", func(t *testing.T) {
		t.Parallel()

		running := IsProcessRunning(nil)
		assert.False(t, running, "nil process should not be running")
	})

	t.Run("current process returns true", func(t *testing.T) {
		t.Parallel()

		currentProc, err := os.FindProcess(os.Getpid())
		require.NoError(t, err)

		running := IsProcessRunning(currentProc)
		assert.True(t, running, "current process should be running")
	})

	t.Run("terminated process returns false", func(t *testing.T) {
		t.Parallel()

		// Start a process that immediately exits
		ctx := context.Background()
		cmd := getShortLivedCommand(ctx)
		err := cmd.Start()
		require.NoError(t, err)

		proc := cmd.Process
		assert.NotNil(t, proc)

		// Wait for it to finish
		err = cmd.Wait()
		require.NoError(t, err)

		// Give it a moment to ensure process cleanup
		time.Sleep(10 * time.Millisecond)

		// Process should no longer be running
		running := IsProcessRunning(proc)
		assert.False(t, running, "terminated process should not be running")
	})

	t.Run("long-running process returns true", func(t *testing.T) {
		t.Parallel()

		// Start a process that runs for a while
		ctx := context.Background()
		cmd := getLongRunningCommand(ctx)
		err := cmd.Start()
		require.NoError(t, err)

		proc := cmd.Process
		assert.NotNil(t, proc)

		// Process should be running
		running := IsProcessRunning(proc)
		assert.True(t, running, "long-running process should be running")

		// Clean up: kill the process
		err = proc.Kill()
		require.NoError(t, err)

		// Wait for it to be killed
		_, _ = proc.Wait()
	})
}
