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

package hoststatus

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

// DiskUsageOf reports the size of the volume holding path.
func DiskUsageOf(path string) (DiskUsage, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return DiskUsage{}, fmt.Errorf("invalid path %s: %w", path, err)
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &available, &total, &free); err != nil {
		return DiskUsage{}, fmt.Errorf("GetDiskFreeSpaceEx %s: %w", path, err)
	}
	return DiskUsage{Total: total, Free: free, Available: available}, nil
}

// FilesystemID identifies the volume holding path, so two directories on the
// same one are counted once, and reports where it is mounted.
func FilesystemID(path string) (id, mountPoint string, err error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", "", fmt.Errorf("invalid path %s: %w", path, err)
	}
	// The volume lookup below answers for any path on a drive that exists. A
	// directory that is not there is not on any volume.
	if _, err := windows.GetFileAttributes(pathPtr); err != nil {
		return "", "", fmt.Errorf("GetFileAttributes %s: %w", path, err)
	}
	buf := make([]uint16, windows.MAX_PATH+1)
	//nolint:gosec // G115: fixed buffer length fits in uint32
	if err := windows.GetVolumePathName(pathPtr, &buf[0], uint32(len(buf))); err != nil {
		return "", "", fmt.Errorf("GetVolumePathName %s: %w", path, err)
	}
	mountPoint = windows.UTF16ToString(buf)
	return strings.ToLower(mountPoint), mountPoint, nil
}
