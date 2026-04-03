//go:build windows

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
	"fmt"

	"golang.org/x/sys/windows"
)

// FreeDiskSpace returns the number of bytes available to the calling user
// on the filesystem containing the given path.
func FreeDiskSpace(path string) (uint64, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("invalid path %s: %w", path, err)
	}

	var freeBytesAvailable uint64
	err = windows.GetDiskFreeSpaceEx(
		pathPtr,
		&freeBytesAvailable,
		nil,
		nil,
	)
	if err != nil {
		return 0, fmt.Errorf("GetDiskFreeSpaceEx %s: %w", path, err)
	}
	return freeBytesAvailable, nil
}
