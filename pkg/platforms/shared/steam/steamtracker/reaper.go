//go:build linux

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

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

// Package steamtracker provides Steam game lifecycle tracking on Linux.
// It detects game starts by monitoring Steam's reaper processes and
// game exits using pidfd/polling.
package steamtracker

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ReaperProcess represents a running Steam game detected via its reaper process.
type ReaperProcess struct {
	PID   int
	AppID int
}

// appIDRegex matches "AppId=XXXXX" in process command line.
var appIDRegex = regexp.MustCompile(`AppId=(\d+)`)

// ScanReaperProcesses finds all Steam reaper processes and their AppIDs.
// Steam wraps game launches: ~/.local/share/Steam/ubuntu12_32/reaper SteamLaunch AppId=XXXXX -- [cmd]
func ScanReaperProcesses() ([]ReaperProcess, error) {
	return ScanReaperProcessesWithProcPath("/proc")
}

// ScanReaperProcessesWithProcPath scans for reaper processes using a custom proc path.
// This allows testing with mock filesystems.
func ScanReaperProcessesWithProcPath(procPath string) ([]ReaperProcess, error) {
	entries, err := os.ReadDir(procPath)
	if err != nil {
		return nil, fmt.Errorf("read proc dir: %w", err)
	}

	var results []ReaperProcess

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if directory name is a PID (all numeric)
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Check if this is a reaper process
		appID, ok := checkReaperProcess(procPath, pid)
		if ok {
			results = append(results, ReaperProcess{PID: pid, AppID: appID})
		}
	}

	return results, nil
}

// checkReaperProcess checks if a process is a Steam reaper and extracts its AppID.
func checkReaperProcess(procPath string, pid int) (int, bool) {
	pidStr := strconv.Itoa(pid)

	// Read process name from /proc/{pid}/comm
	commPath := filepath.Join(procPath, pidStr, "comm")
	commData, err := os.ReadFile(commPath) //nolint:gosec // G304: procPath is controlled by caller
	if err != nil {
		return 0, false
	}

	comm := strings.TrimSpace(string(commData))
	if comm != "reaper" {
		return 0, false
	}

	// Read command line from /proc/{pid}/cmdline
	cmdlinePath := filepath.Join(procPath, pidStr, "cmdline")
	cmdlineData, err := os.ReadFile(cmdlinePath) //nolint:gosec // G304: procPath is controlled by caller
	if err != nil {
		// Process may have exited between reading comm and cmdline
		return 0, false
	}

	// cmdline is null-separated
	cmdline := string(cmdlineData)
	appID, ok := parseAppIDFromCmdline(cmdline)
	if !ok {
		return 0, false
	}

	// Verify it's a Steam reaper by checking for SteamLaunch in cmdline
	if !strings.Contains(cmdline, "SteamLaunch") {
		return 0, false
	}

	return appID, true
}

// parseAppIDFromCmdline extracts AppId=XXXXX from a process command line.
func parseAppIDFromCmdline(cmdline string) (int, bool) {
	// Replace null bytes with spaces for easier matching
	cmdline = strings.ReplaceAll(cmdline, "\x00", " ")

	matches := appIDRegex.FindStringSubmatch(cmdline)
	if len(matches) < 2 {
		return 0, false
	}

	appID, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, false
	}

	return appID, true
}
