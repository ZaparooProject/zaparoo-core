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
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

const (
	// executeMemoryLimit caps address space via RLIMIT_AS.
	executeMemoryLimit = 512 * 1024 * 1024 // 512 MiB

	// executeCPULimit caps CPU time in seconds.
	executeCPULimit = 5
)

// RunInJob runs the command in a new process group with resource limits.
//   - Process group ensures the entire tree is killed on timeout/cleanup
//   - Pdeathsig kills the child if the parent dies unexpectedly
//   - prlimit64 sets RLIMIT_AS (memory) and RLIMIT_CPU on the child
//   - cmd.Cancel kills the process group (not just the top-level PID)
func RunInJob(cmd *exec.Cmd) error {
	// Lock this goroutine to its OS thread so that Pdeathsig (which is tied
	// to the creating thread, not the process) remains valid for the lifetime
	// of the child process.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL

	// Override Cancel to kill the entire process group.
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second

	if err := cmd.Start(); err != nil {
		return err //nolint:wrapcheck // Wrapping exec errors loses important context
	}

	// Apply resource limits to the child process via prlimit64. If limits
	// cannot be set, kill the process rather than letting it run unbounded.
	pid := cmd.Process.Pid
	if err := applyResourceLimits(pid); err != nil {
		// ESRCH means the process already exited — limits are irrelevant.
		if !errors.Is(err, syscall.ESRCH) {
			log.Warn().Err(err).Int("pid", pid).Msg("failed to apply resource limits, killing process")
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("applying resource limits: %w", err)
		}
	}

	return cmd.Wait() //nolint:wrapcheck // Wrapping exec errors loses important context
}

// applyResourceLimits sets RLIMIT_AS and RLIMIT_CPU on the given PID using
// the prlimit64 syscall. This affects only the target process, not the parent.
func applyResourceLimits(pid int) error {
	memLimit := unix.Rlimit{
		Cur: executeMemoryLimit,
		Max: executeMemoryLimit,
	}
	if err := unix.Prlimit(pid, unix.RLIMIT_AS, &memLimit, nil); err != nil {
		return fmt.Errorf("setting RLIMIT_AS: %w", err)
	}

	cpuLimit := unix.Rlimit{
		Cur: executeCPULimit,
		Max: executeCPULimit,
	}
	if err := unix.Prlimit(pid, unix.RLIMIT_CPU, &cpuLimit, nil); err != nil {
		return fmt.Errorf("setting RLIMIT_CPU: %w", err)
	}

	return nil
}
