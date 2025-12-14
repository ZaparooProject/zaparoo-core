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
	GamePath string
	PID      int
	AppID    int
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
		appID, gamePath, ok := checkReaperProcess(procPath, pid)
		if ok {
			results = append(results, ReaperProcess{PID: pid, AppID: appID, GamePath: gamePath})
		}
	}

	return results, nil
}

// checkReaperProcess checks if a process is a Steam reaper and extracts its AppID and game path.
func checkReaperProcess(procPath string, pid int) (appID int, gamePath string, ok bool) {
	pidStr := strconv.Itoa(pid)

	commPath := filepath.Join(procPath, pidStr, "comm")
	commData, err := os.ReadFile(commPath) //nolint:gosec // G304: procPath is controlled
	if err != nil {
		return 0, "", false
	}

	comm := strings.TrimSpace(string(commData))
	if comm != "reaper" {
		return 0, "", false
	}

	cmdlinePath := filepath.Join(procPath, pidStr, "cmdline")
	cmdlineData, err := os.ReadFile(cmdlinePath) //nolint:gosec // G304: procPath is controlled
	if err != nil {
		return 0, "", false
	}

	cmdline := string(cmdlineData)
	appID, ok = parseAppIDFromCmdline(cmdline)
	if !ok {
		return 0, "", false
	}

	if !strings.Contains(cmdline, "SteamLaunch") {
		return 0, "", false
	}

	gamePath = parseGamePathFromCmdline(cmdline)
	return appID, gamePath, true
}

// parseAppIDFromCmdline extracts AppId=XXXXX from a process command line.
func parseAppIDFromCmdline(cmdline string) (int, bool) {
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

// parseGamePathFromCmdline extracts the game executable path from a reaper cmdline.
// Format: reaper SteamLaunch AppId=XXX -- [runtime args] -- ... -- /path/to/game
func parseGamePathFromCmdline(cmdline string) string {
	args := strings.Split(cmdline, "\x00")

	lastDashIndex := -1
	for i, arg := range args {
		if arg == "--" {
			lastDashIndex = i
		}
	}

	if lastDashIndex == -1 || lastDashIndex >= len(args)-1 {
		return ""
	}

	gamePath := args[lastDashIndex+1]
	gamePath = strings.TrimSpace(gamePath)
	gamePath = strings.TrimRight(gamePath, "\x00")

	return gamePath
}

// FindGamePID finds a running process that matches the game executable path.
func FindGamePID(gamePath string) (int, bool) {
	return FindGamePIDWithProcPath("/proc", gamePath)
}

// FindGamePIDWithProcPath finds a running process matching the game path using a custom proc path.
// It first tries to find an exact match for the game executable, then falls back to
// searching for any process in the game's install directory.
func FindGamePIDWithProcPath(procPath, gamePath string) (int, bool) {
	if gamePath == "" {
		return 0, false
	}

	entries, err := os.ReadDir(procPath)
	if err != nil {
		return 0, false
	}

	gameDir := filepath.Dir(gamePath)
	var fallbackPID int
	foundFallback := false

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		commPath := filepath.Join(procPath, entry.Name(), "comm")
		commData, _ := os.ReadFile(commPath) //nolint:gosec // G304
		if strings.TrimSpace(string(commData)) == "reaper" {
			continue
		}

		cmdlinePath := filepath.Join(procPath, entry.Name(), "cmdline")
		cmdlineData, err := os.ReadFile(cmdlinePath) //nolint:gosec // G304: procPath is controlled
		if err != nil {
			continue
		}

		cmdline := string(cmdlineData)
		firstArg := strings.SplitN(cmdline, "\x00", 2)[0]

		// Exact match - return immediately
		if firstArg == gamePath {
			return pid, true
		}

		// Track first process in game directory as fallback
		if !foundFallback && strings.HasPrefix(firstArg, gameDir) {
			fallbackPID = pid
			foundFallback = true
		}
	}

	// Return fallback if no exact match found
	if foundFallback {
		return fallbackPID, true
	}

	return 0, false
}
