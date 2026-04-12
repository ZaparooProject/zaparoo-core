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
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// executeMemoryLimit is the per-process memory limit for execute commands.
	executeMemoryLimit = 512 * 1024 * 1024 // 512 MiB

	// executeProcessLimit prevents spawned processes from creating children.
	executeProcessLimit = 1
)

// RunInJob starts the command, assigns it to a restricted Windows Job Object,
// and waits for it to complete. The Job Object enforces:
//   - Kill on job close (cleanup guarantee)
//   - Active process limit of 1 (prevents child process spawning)
//   - Per-process memory limit of 512 MiB
//
// The job object is created per-invocation and cleaned up when the function
// returns. KILL_ON_JOB_CLOSE ensures any still-running process is terminated
// when the job handle is closed.
func RunInJob(cmd *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("creating job object: %w", err)
	}
	defer func() { _ = windows.CloseHandle(job) }()

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
				windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS |
				windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY,
			ActiveProcessLimit: executeProcessLimit,
		},
		ProcessMemoryLimit: executeMemoryLimit,
	}

	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		return fmt.Errorf("configuring job object: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return err //nolint:wrapcheck // Wrapping exec errors loses important context
	}

	proc, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		// Process started but we can't restrict it — kill it.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("opening process for job assignment: %w", err)
	}
	defer func() { _ = windows.CloseHandle(proc) }()

	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("assigning process to job object: %w", err)
	}

	return cmd.Wait() //nolint:wrapcheck // Wrapping exec errors loses important context
}
