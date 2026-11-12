//go:build linux

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
	"os"
	"path/filepath"
	"strings"
)

var mountInfoPath = filepath.Join(string(filepath.Separator), "proc", "self", "mountinfo")

// FilesystemID identifies the filesystem holding path, so two directories on
// the same one are counted once, and reports where it is mounted.
//
// The device number alone is not enough: every subvolume of a btrfs
// filesystem has its own, though they all share one pool of space. Where the
// mount is backed by a block device or a network share, that is the identity.
func FilesystemID(path string) (id, mountPoint string, err error) {
	id, err = deviceID(path)
	if err != nil {
		return "", "", err
	}
	source, mountPoint, ok := mountOf(path)
	if ok && strings.HasPrefix(source, "/") {
		id = source
	}
	return id, mountPoint, nil
}

// mountOf looks path up in the mount table. Any failure just means the device
// number is used and the mount point is not known.
func mountOf(path string) (source, mountPoint string, ok bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", false
	}
	data, err := os.ReadFile(mountInfoPath) //nolint:gosec // G304: fixed procfs path
	if err != nil {
		return "", "", false
	}
	return MountSource(string(data), resolved)
}

// MountSource returns what is mounted at the deepest mount point holding
// path, and that mount point, from the contents of /proc/self/mountinfo. The
// source is a block device or a network share for a mount that has one behind
// it; for a mount such as tmpfs it names a kind of filesystem and not one
// instance of it.
func MountSource(mountInfo, path string) (source, mountPoint string, ok bool) {
	for line := range strings.SplitSeq(mountInfo, "\n") {
		// "<id> <parent> <dev> <root> <mount point> <options> [tags] - <type> <source> <options>"
		before, after, found := strings.Cut(line, " - ")
		if !found {
			continue
		}
		fields := strings.Fields(before)
		tail := strings.Fields(after)
		if len(fields) < 5 || len(tail) < 2 {
			continue
		}
		candidate := unescapeMountField(fields[4])
		if !pathWithin(path, candidate) {
			continue
		}
		// The deepest mount point containing the path is the one it is on. A
		// later line for the same mount point is mounted over the earlier.
		if !ok || len(candidate) >= len(mountPoint) {
			source, mountPoint, ok = unescapeMountField(tail[1]), candidate, true
		}
	}
	return source, mountPoint, ok
}

func pathWithin(path, dir string) bool {
	if dir == string(filepath.Separator) {
		return strings.HasPrefix(path, dir)
	}
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// unescapeMountField undoes the octal escapes the kernel uses for space, tab,
// newline and backslash in mountinfo.
func unescapeMountField(field string) string {
	if !strings.Contains(field, `\`) {
		return field
	}
	var out strings.Builder
	for i := 0; i < len(field); i++ {
		if field[i] == '\\' && i+3 < len(field) {
			value := 0
			valid := true
			for _, digit := range field[i+1 : i+4] {
				if digit < '0' || digit > '7' {
					valid = false
					break
				}
				value = value*8 + int(digit-'0')
			}
			if valid && value < 256 {
				_ = out.WriteByte(byte(value))
				i += 3
				continue
			}
		}
		_ = out.WriteByte(field[i])
	}
	return out.String()
}
