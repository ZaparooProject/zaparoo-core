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

//go:build windows

package helpers

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

// GetSystemUptime returns the duration since the system booted.
// On Windows, this uses the GetTickCount64 function which returns milliseconds since boot.
func GetSystemUptime() (time.Duration, error) {
	kernel32, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return 0, fmt.Errorf("failed to load kernel32.dll: %w", err)
	}
	defer func() {
		_ = kernel32.Release()
	}()

	getTickCount64, err := kernel32.FindProc("GetTickCount64")
	if err != nil {
		return 0, fmt.Errorf("failed to find GetTickCount64: %w", err)
	}

	ret, _, callErr := getTickCount64.Call()

	// Check if the call failed. Note: ret can legitimately be 0 immediately after boot.
	// The actual error is indicated by callErr having a non-zero errno.
	if callErr != nil {
		if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
			return 0, fmt.Errorf("GetTickCount64 failed: %w", callErr)
		}
	}

	// Convert milliseconds to time.Duration
	uptimeMs := *(*uint64)(unsafe.Pointer(&ret))
	uptime := time.Duration(uptimeMs) * time.Millisecond

	return uptime, nil
}
