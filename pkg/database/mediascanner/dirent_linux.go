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

package mediascanner

import "syscall"

// Filesystem magics for the FAT family. exFAT is what MiSTer and most handheld
// images use for the storage holding games.
const (
	exfatSuperMagic = 0x2011BAB0
	msdosSuperMagic = 0x4D44
)

// direntTypesReportSymlinks reports whether readdir on the filesystem holding
// path fills in the symlink type for directory entries.
//
// The FAT family does not. Linux's exFAT driver stores symlinks and reports
// them correctly through lstat, but returns DT_REG for them in getdents64, so
// a walk that trusts the dirent type sees an ordinary file. Measured on a
// MiSTer: a tree of 88 symlinks walked with symlinksEncountered = 0, while
// lstat on the same entries reported every one as a link.
//
// A filesystem this cannot identify is treated as reporting types correctly,
// so the walk keeps its current cost everywhere it already works.
func direntTypesReportSymlinks(path string) bool {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return true
	}
	switch int64(stat.Type) { //nolint:unconvert // Type is int32 on 32-bit
	case exfatSuperMagic, msdosSuperMagic:
		return false
	default:
		return true
	}
}
