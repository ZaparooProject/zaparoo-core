// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

//go:build darwin

package helpers

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

// GetSystemUptime returns the duration since the system booted.
// On macOS/Darwin, this uses sysctl to read kern.boottime.
func GetSystemUptime() (time.Duration, error) {
	// Use sysctl to get kern.boottime
	mib := []int32{syscall.CTL_KERN, syscall.KERN_BOOTTIME}
	var bootTime syscall.Timeval
	n := uintptr(unsafe.Sizeof(bootTime))

	//nolint:gosec // G103: unsafe required for sysctl syscall
	_, _, errno := syscall.Syscall6(
		syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])),
		uintptr(len(mib)),
		uintptr(unsafe.Pointer(&bootTime)),
		uintptr(unsafe.Pointer(&n)),
		0,
		0,
	)

	if errno != 0 {
		return 0, fmt.Errorf("sysctl kern.boottime failed: %w", errno)
	}

	// Calculate uptime by subtracting boot time from current time
	bootTimeGo := time.Unix(bootTime.Sec, int64(bootTime.Usec)*1000)
	uptime := time.Since(bootTimeGo)

	return uptime, nil
}
