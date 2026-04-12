//go:build windows

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
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunInJob_Success(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "cmd", "/c", "echo", "hello")
	err := RunInJob(cmd)
	require.NoError(t, err)
}

func TestRunInJob_FailedCommand(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "cmd", "/c", "exit", "1")
	err := RunInJob(cmd)
	assert.Error(t, err)
}

func TestRunInJob_NonexistentCommand(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "nonexistent-command-that-does-not-exist")
	err := RunInJob(cmd)
	assert.Error(t, err)
}

func TestRunInJob_ProcessTreeLimited(t *testing.T) {
	t.Parallel()

	// ActiveProcessLimit=2 allows one child (shell wrappers). A chain of 3
	// cmd.exe processes exceeds the limit — the innermost spawn should fail.
	cmd := exec.CommandContext(t.Context(), "cmd", "/c", "cmd", "/c", "cmd", "/c", "echo", "deep")
	err := RunInJob(cmd)
	assert.Error(t, err)
}
