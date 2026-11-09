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
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// An exited child that nobody waits on stays in /proc as a zombie.
func requireReaped(t *testing.T, pid int) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := os.Stat("/proc/" + strconv.Itoa(pid))
		return errors.Is(err, fs.ErrNotExist)
	}, 5*time.Second, 10*time.Millisecond, "pid %d was never reaped", pid)
}

func TestStartAndReap_ReapsExitedChild(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(context.Background(), "true")
	require.NoError(t, startAndReap(cmd))

	requireReaped(t, cmd.Process.Pid)
}
