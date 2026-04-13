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

package command

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunInJob_Success(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "echo", "hello")
	err := RunInJob(cmd)
	require.NoError(t, err)
}

func TestRunInJob_FailedCommand(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "false")
	err := RunInJob(cmd)
	assert.Error(t, err)
}

func TestRunInJob_NonexistentCommand(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "nonexistent-command-that-does-not-exist")
	err := RunInJob(cmd)
	assert.Error(t, err)
}

func TestRunInJob_ProcessGroupKill(t *testing.T) {
	t.Parallel()

	// Short timeout so the context cancellation triggers cmd.Cancel, which
	// kills the entire process group (not just the top-level PID).
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 60 & wait")
	err := RunInJob(cmd)
	assert.Error(t, err)
}
