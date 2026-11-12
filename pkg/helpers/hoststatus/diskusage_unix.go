//go:build !windows

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
	"strconv"
	"syscall"
)

// DiskUsageOf reports the size of the filesystem holding path.
func DiskUsageOf(path string) (DiskUsage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return DiskUsage{}, fmt.Errorf("statfs %s: %w", path, err)
	}
	if stat.Bsize <= 0 {
		return DiskUsage{}, fmt.Errorf("statfs %s: invalid block size %d", path, stat.Bsize)
	}
	blockSize := uint64(stat.Bsize) //nolint:gosec // Bsize validated positive above
	return DiskUsage{
		Total:     stat.Blocks * blockSize,
		Free:      stat.Bfree * blockSize,
		Available: uint64(stat.Bavail) * blockSize, //nolint:gosec,unconvert // signed on some unix targets
	}, nil
}

// deviceID identifies the filesystem holding path by its device number.
func deviceID(path string) (string, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	return strconv.FormatUint(uint64(stat.Dev), 10), nil //nolint:gosec,unconvert // width differs per unix target
}
