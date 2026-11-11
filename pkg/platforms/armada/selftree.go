//go:build linux && !android

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

package armada

import (
	"bytes"
	"os"
	"strconv"
	"strings"
)

// ownsCore reports whether pid is Core or one of its ancestors. Started from
// a Steam shortcut, Core runs inside a Steam reaper's tree, and the Steam
// tracker would claim that reaper as the running game: recording Core as
// something the user played, and terminating Core when that "game" is
// stopped. Ancestry is read on each call because Core is reparented to the
// reaper when the process that started it exits.
func ownsCore(pid int) bool {
	self := os.Getpid()
	return pid > 0 && (pid == self || processHasAncestor(self, pid))
}

// processHasAncestor walks pid's parents looking for ancestor. Bounded: a
// broken or racing /proc must not spin, and no real tree is this deep.
func processHasAncestor(pid, ancestor int) bool {
	const maxDepth = 24
	for depth := 0; depth < maxDepth && pid > 1; depth++ {
		parent, ok := processParent(pid)
		if !ok {
			return false
		}
		if parent == ancestor {
			return true
		}
		pid = parent
	}
	return false
}

// processParent reads the parent pid out of /proc/<pid>/stat. The comm field
// can contain spaces and brackets, so the fields are read after the last ')'.
func processParent(pid int) (int, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	commEnd := bytes.LastIndexByte(data, ')')
	if commEnd < 0 {
		return 0, false
	}
	// After the comm field: state, ppid, ...
	fields := strings.Fields(string(data[commEnd+1:]))
	if len(fields) < 2 {
		return 0, false
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	return parent, true
}
