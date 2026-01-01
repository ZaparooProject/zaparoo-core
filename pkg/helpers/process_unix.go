//go:build !windows

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
	"os"
	"syscall"
)

// IsProcessRunning checks if a process is still running.
// Returns false if the process is nil or has terminated.
func IsProcessRunning(proc *os.Process) bool {
	if proc == nil {
		return false
	}

	// Send signal 0 to check if process exists without affecting it
	// If the process is running, this returns nil
	// If the process doesn't exist, this returns an error
	err := proc.Signal(syscall.Signal(0))
	return err == nil
}
