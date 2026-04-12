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
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// executeProcessLimit caps concurrent processes in the job. Allows the command
// plus one child (covers shell wrappers). Windows-only — Linux/macOS use
// process groups for tree management instead.
const executeProcessLimit = 2

// RunInJob starts the command in a suspended state, assigns it to a restricted
// Windows Job Object, then resumes execution. The Job Object enforces:
//   - Kill on job close (cleanup guarantee)
//   - Active process limit of 2 (command + one child for shell wrappers)
//   - Total job memory limit of 512 MiB
//
// The process is created suspended (CREATE_SUSPENDED) so that Job Object
// restrictions are in effect before any user code runs, closing the TOCTOU
// window between process creation and job assignment.
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
				windows.JOB_OBJECT_LIMIT_JOB_MEMORY,
			ActiveProcessLimit: executeProcessLimit,
		},
		JobMemoryLimit: executeMemoryLimit,
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

	// Create the process suspended so we can assign it to the job before
	// any user code runs.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED

	if err := cmd.Start(); err != nil {
		return err //nolint:wrapcheck // Wrapping exec errors loses important context
	}

	// Assign the suspended process to the job object.
	proc, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("opening process for job assignment: %w", err)
	}

	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(proc)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("assigning process to job object: %w", err)
	}
	_ = windows.CloseHandle(proc)

	// Resume the main thread now that job restrictions are in effect.
	if err := resumeProcessThreads(uint32(cmd.Process.Pid)); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("resuming process after job assignment: %w", err)
	}

	return cmd.Wait() //nolint:wrapcheck // Wrapping exec errors loses important context
}

// resumeProcessThreads enumerates and resumes all threads belonging to the
// given process ID. Used after creating a process with CREATE_SUSPENDED and
// assigning it to a Job Object.
func resumeProcessThreads(pid uint32) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("creating thread snapshot: %w", err)
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	var entry windows.ThreadEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	err = windows.Thread32First(snap, &entry)
	if err != nil {
		return fmt.Errorf("enumerating threads: %w", err)
	}

	for {
		if entry.OwnerProcessID == pid {
			thread, openErr := windows.OpenThread(
				windows.THREAD_SUSPEND_RESUME,
				false,
				entry.ThreadID,
			)
			if openErr == nil {
				_, _ = windows.ResumeThread(thread)
				_ = windows.CloseHandle(thread)
			}
		}

		err = windows.Thread32Next(snap, &entry)
		if err != nil {
			break
		}
	}

	return nil
}
