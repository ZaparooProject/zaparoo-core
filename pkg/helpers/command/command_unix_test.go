//go:build !windows

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
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireReaped(t *testing.T, reaped <-chan struct{}) {
	t.Helper()
	select {
	case <-reaped:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "child was never reaped")
	}
}

func TestStartAndReap_ReapsExitedChild(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(context.Background(), "true")
	reaped, err := startReaped(cmd)
	require.NoError(t, err)

	requireReaped(t, reaped)
	require.NotNil(t, cmd.ProcessState, "Wait did not record the child's exit")
	assert.True(t, cmd.ProcessState.Exited())
	assert.True(t, cmd.ProcessState.Success())
}

func TestStartAndReap_ReturnsStartError(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing")
	cmd := exec.CommandContext(context.Background(), missing) //nolint:gosec // G204: test-owned temp path

	require.Error(t, StartAndReap(cmd))
	assert.Nil(t, cmd.Process)
}
