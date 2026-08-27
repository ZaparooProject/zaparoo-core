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

package helpers

import (
	"strconv"
	"strings"
)

// Parsing /proc/self/mountinfo is deliberately in a portable file even though
// the file only exists on Linux: it is pure string handling, so keeping it here
// means the format tests run on every platform rather than only where the
// procfs read is possible.

// MountEntry is one line of /proc/self/mountinfo, reduced to the fields
// callers need. Entries appear in mount order, so of two entries with the same
// Mountpoint the later one shadows the earlier.
type MountEntry struct {
	Root       string // path inside the source filesystem, e.g. /zaparoo/profiles/x/saves
	Mountpoint string // where it is mounted, e.g. /media/fat
	FSType     string // e.g. exfat, vfat, cifs
	Source     string // device or share, e.g. /dev/mmcblk0p1, //nas/share
	Options    string // per-mount options, e.g. rw,noatime
}

// StorageInfo is the longest-matching mount for a path, for logging what
// storage a file actually sits on.
type StorageInfo struct {
	FSType     string
	Mountpoint string
	Source     string
	Options    string
}

// ParseMountInfo parses /proc/self/mountinfo content. Format per line:
//
//	36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw
//	(1) (2) (3) (root) (mountpoint) (opts) (optional...) - (fstype) (source) (superopts)
//
// The number of optional fields between the options and the "-" separator
// varies, which is why the separator is searched for rather than indexed.
// Malformed lines are skipped.
func ParseMountInfo(data string) []MountEntry {
	var entries []MountEntry
	for line := range strings.Lines(data) {
		fields := strings.Fields(line)
		sep := -1
		for i, f := range fields {
			if f == "-" && i >= 6 {
				sep = i
				break
			}
		}
		if sep < 0 || sep+2 >= len(fields) || len(fields) < 5 {
			continue
		}
		entries = append(entries, MountEntry{
			Root:       unescapeMountField(fields[3]),
			Mountpoint: unescapeMountField(fields[4]),
			Options:    fields[5],
			FSType:     fields[sep+1],
			Source:     unescapeMountField(fields[sep+2]),
		})
	}
	return entries
}

// unescapeMountField decodes the octal escapes the kernel uses for
// whitespace in mountinfo fields (e.g. \040 for space).
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if code, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				_ = b.WriteByte(byte(code))
				i += 3
				continue
			}
		}
		_ = b.WriteByte(s[i])
	}
	return b.String()
}
