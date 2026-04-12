//go:build darwin

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
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

// rlimitMu serializes the getrlimit/setrlimit/start/restore sequence.
// macOS has no prlimit (cannot set limits on another PID), so we must
// temporarily modify the parent's limits before fork. The mutex prevents
// concurrent RunInJob calls from clobbering each other's saved limits.
var rlimitMu syncutil.Mutex

// RunInJob runs the command in a new process group with CPU time limits.
//   - Process group ensures the entire tree is killed on timeout/cleanup
//   - RLIMIT_CPU is set before start (inherited by the child)
//   - cmd.Cancel kills the process group (not just the top-level PID)
//
// macOS lacks prlimit (cannot set limits on another PID) and Pdeathsig.
// RLIMIT_AS enforcement is unreliable on macOS, so only CPU time is limited.
func RunInJob(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true

	// Override Cancel to kill the entire process group.
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second

	// macOS has no prlimit — set CPU limit before fork so the child inherits
	// it. Save and restore the parent's limit under a mutex to prevent
	// concurrent calls from clobbering each other.
	rlimitMu.Lock()
	var oldCPU unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CPU, &oldCPU); err == nil {
		newCPU := unix.Rlimit{Cur: executeCPULimit, Max: executeCPULimit}
		if err := unix.Setrlimit(unix.RLIMIT_CPU, &newCPU); err != nil {
			log.Debug().Err(err).Msg("failed to set RLIMIT_CPU before fork")
		}
	}

	startErr := cmd.Start()

	// Restore parent limits and release the mutex immediately after fork.
	_ = unix.Setrlimit(unix.RLIMIT_CPU, &oldCPU)
	rlimitMu.Unlock()

	if startErr != nil {
		return startErr //nolint:wrapcheck // Wrapping exec errors loses important context
	}

	return cmd.Wait() //nolint:wrapcheck // Wrapping exec errors loses important context
}
