//go:build windows

/*
Zaparoo Core
Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"os"

	"golang.org/x/sys/windows"
)

const stillActive = 259 // STILL_ACTIVE exit code for running processes

// IsProcessRunning checks if a process is still running.
// Returns false if the process is nil or has terminated.
func IsProcessRunning(proc *os.Process) bool {
	if proc == nil {
		return false
	}

	// PIDs must be positive and fit in uint32
	if proc.Pid < 0 {
		return false
	}

	// Open process handle with PROCESS_QUERY_LIMITED_INFORMATION access
	// This is the minimum access required to query process information
	//nolint:gosec // G115 false positive - Windows PIDs are 32-bit, and negative check above ensures safe conversion
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(proc.Pid))
	if err != nil {
		// If we can't open the process, it's not running (or we don't have permissions)
		return false
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	// Query exit code
	var exitCode uint32
	err = windows.GetExitCodeProcess(handle, &exitCode)
	if err != nil {
		// If we can't get the exit code, assume process is not running
		return false
	}

	// STILL_ACTIVE (259) means the process is still running
	return exitCode == stillActive
}
