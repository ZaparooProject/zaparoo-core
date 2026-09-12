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
package pinup

import (
	"fmt"

	"github.com/shirou/gopsutil/v4/process"
)

// ProcessInfo is one running process as the integration sees it: enough to
// match it against an emulator's executables.
type ProcessInfo struct {
	Exe string
	PID int
}

// ProcessLister enumerates running processes. It is an interface so tests can
// script which emulator and Popper processes exist at each moment.
type ProcessLister interface {
	List() ([]ProcessInfo, error)
}

// NewProcessLister returns the real process lister.
func NewProcessLister() ProcessLister {
	return gopsutilLister{}
}

type gopsutilLister struct{}

// List reports every process by image name. Name() comes from the process
// snapshot on Windows, so it works for processes Core has no rights to open,
// which a full image path would not.
func (gopsutilLister) List() ([]ProcessInfo, error) {
	procs, err := process.Processes()
	if err != nil {
		return nil, fmt.Errorf("enumerate processes: %w", err)
	}
	infos := make([]ProcessInfo, 0, len(procs))
	for _, p := range procs {
		name, nameErr := p.Name()
		if nameErr != nil || name == "" {
			continue
		}
		infos = append(infos, ProcessInfo{Exe: name, PID: int(p.Pid)})
	}
	return infos, nil
}

// matchingProcesses returns the processes whose image is one of the candidate
// executables.
func matchingProcesses(procs []ProcessInfo, candidates []string) []ProcessInfo {
	matches := make([]ProcessInfo, 0, 2)
	for _, p := range procs {
		if MatchesExecutable(p.Exe, candidates) {
			matches = append(matches, p)
		}
	}
	return matches
}

// processRunning reports whether any process runs the given image.
func processRunning(procs []ProcessInfo, exe string) (int, bool) {
	for _, p := range procs {
		if MatchesExecutable(p.Exe, []string{exe}) {
			return p.PID, true
		}
	}
	return 0, false
}

// pidRunning reports whether pid is still alive and still runs one of the
// candidate images, so a recycled PID is not mistaken for the table.
func pidRunning(procs []ProcessInfo, pid int, candidates []string) bool {
	for _, p := range procs {
		if p.PID != pid {
			continue
		}
		return len(candidates) == 0 || MatchesExecutable(p.Exe, candidates)
	}
	return false
}
