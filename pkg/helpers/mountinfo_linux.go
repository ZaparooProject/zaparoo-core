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

//go:build linux

package helpers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A var rather than a const so tests can point it at a fixture instead of
// faking procfs.
var mountInfoPath = filepath.Join(string(filepath.Separator), "proc", "self", "mountinfo")

// MountInfoPath is the procfs mount table this package reads, for callers that
// need to open or watch the file themselves rather than take a parsed snapshot.
func MountInfoPath() string {
	return mountInfoPath
}

// ReadMountInfo returns the current mount table.
func ReadMountInfo() ([]MountEntry, error) {
	data, err := os.ReadFile(mountInfoPath) //nolint:gosec // fixed procfs path assembled from constants
	if err != nil {
		return nil, fmt.Errorf("failed to read mountinfo: %w", err)
	}
	return ParseMountInfo(string(data)), nil
}

// StorageInfoForPath returns the mount entry that owns path: the entry whose
// Mountpoint is the longest prefix of path's absolute form. Returns false if
// mountinfo could not be read or nothing matches.
func StorageInfoForPath(path string) (StorageInfo, bool) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return StorageInfo{}, false
	}
	entries, err := ReadMountInfo()
	if err != nil {
		return StorageInfo{}, false
	}

	var best MountEntry
	found := false
	for _, e := range entries {
		if e.Mountpoint != "/" && !strings.HasPrefix(absPath, e.Mountpoint) {
			continue
		}
		if !found || len(e.Mountpoint) >= len(best.Mountpoint) {
			best = e
			found = true
		}
	}
	if !found {
		return StorageInfo{}, false
	}
	return StorageInfo{
		FSType:     best.FSType,
		Mountpoint: best.Mountpoint,
		Source:     best.Source,
		Options:    best.Options,
	}, true
}
