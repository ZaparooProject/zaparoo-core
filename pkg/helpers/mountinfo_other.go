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

//go:build !linux

package helpers

import "errors"

// errNoMountInfo is returned wherever the mount table is requested on a
// platform that has no /proc/self/mountinfo to read it from.
var errNoMountInfo = errors.New("mount table unavailable: no /proc/self/mountinfo on this platform")

// MountInfoPath returns "" off Linux: there is no procfs mount table to open.
func MountInfoPath() string {
	return ""
}

// ReadMountInfo always fails off Linux. Callers treat an unreadable mount table
// as "unknown storage" already, so this needs no separate handling from them.
func ReadMountInfo() ([]MountEntry, error) {
	return nil, errNoMountInfo
}

// StorageInfoForPath always returns false on non-Linux platforms: there is
// no portable equivalent of /proc/self/mountinfo to read it from.
func StorageInfoForPath(string) (StorageInfo, bool) {
	return StorageInfo{}, false
}
