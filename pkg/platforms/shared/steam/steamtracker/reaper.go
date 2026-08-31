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

	sharedsteam "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
)

// ReaperProcess represents a running Steam game detected via its reaper process.
type ReaperProcess struct {
	GamePath string
	PID      int
	AppID    int
}

// appIDRegex matches "AppId=XXXXX" in process command line.
var appIDRegex = regexp.MustCompile(`(?i)AppId=(\d+)`)

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
	if !strings.EqualFold(comm, "reaper") {
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

	if !strings.Contains(strings.ToLower(cmdline), "steamlaunch") {
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

// parseGamePathFromCmdline extracts the most likely game executable path from a
// Steam reaper cmdline. Proton adds multiple runtime separators, so the last
// separator is not necessarily followed by the game executable.
func parseGamePathFromCmdline(cmdline string) string {
	if !strings.Contains(cmdline, "\x00") {
		return ""
	}

	args := strings.Split(cmdline, "\x00")
	firstSeparator := -1
	for i, arg := range args {
		if cleanProcArg(arg) == "--" {
			firstSeparator = i
			break
		}
	}
	if firstSeparator == -1 {
		return ""
	}

	for i := len(args) - 1; i > firstSeparator; i-- {
		arg := cleanProcArg(args[i])
		if isPathArgument(arg) {
			return arg
		}
	}
	return ""
}

func cleanProcArg(arg string) string {
	return strings.Trim(strings.TrimSpace(arg), "'\"")
}

func isPathArgument(arg string) bool {
	if arg == "" || arg == "--" || strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
		return false
	}
	return strings.Contains(arg, "/") || strings.Contains(arg, "\\")
}

// FindGamePID finds a running process that matches the game executable path.
func FindGamePID(gamePath string) (int, bool) {
	return FindGamePIDWithProcPath("/proc", gamePath)
}

// FindGamePIDForAppID finds a process for an AppID using Steam's install and
// launch metadata before falling back to the reaper-derived path.
func FindGamePIDForAppID(steamDir string, appID int, gamePath string) (int, bool) {
	return FindGamePIDForAppIDWithProcPath("/proc", steamDir, appID, gamePath)
}

// FindGamePIDForAppIDWithProcPath is the testable form of FindGamePIDForAppID.
func FindGamePIDForAppIDWithProcPath(
	procPath, steamDir string, appID int, gamePath string,
) (int, bool) {
	targets, installDir := resolveGameProcessTargets(steamDir, appID, gamePath)
	if len(targets) == 0 && installDir == "" {
		return 0, false
	}
	return findGamePIDWithPaths(procPath, targets, installDir)
}

func resolveGameProcessTargets(
	steamDir string, appID int, gamePath string,
) (targets []string, installDir string) {
	installDir, _ = sharedsteam.FindInstallDirByAppIDInSteamDir(steamDir, appID)
	executablePath, _ := sharedsteam.GetGameExecutable(steamDir, appID)

	// A runtime/Proton path parsed from the reaper command line is not useful
	// once manifest metadata gives us the game's install directory.
	if installDir != "" && !pathWithin(gamePath, installDir) {
		gamePath = ""
	}

	targets = make([]string, 0, 2)
	if gamePath != "" {
		targets = append(targets, gamePath)
	}
	if executablePath != "" && executablePath != gamePath {
		targets = append(targets, executablePath)
	}
	return targets, installDir
}

// FindGamePIDWithProcPath finds a running process matching the game path using a custom proc path.
// It first tries an exact executable/argument match, then falls back to a
// process whose command line or executable is inside the game's install directory.
func FindGamePIDWithProcPath(procPath, gamePath string) (int, bool) {
	if gamePath == "" {
		return 0, false
	}
	return findGamePIDWithPaths(procPath, []string{gamePath}, filepath.Dir(gamePath))
}

func findGamePIDWithPaths(procPath string, targets []string, installDir string) (int, bool) {
	entries, err := os.ReadDir(procPath)
	if err != nil {
		return 0, false
	}

	var argumentPID, installArgumentPID, fallbackPID int
	foundArgument, foundInstallArgument, foundFallback := false, false, false

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
		if strings.EqualFold(strings.TrimSpace(string(commData)), "reaper") {
			continue
		}

		cmdlinePath := filepath.Join(procPath, entry.Name(), "cmdline")
		cmdlineData, err := os.ReadFile(cmdlinePath) //nolint:gosec // G304: procPath is controlled
		if err != nil {
			continue
		}

		args := strings.Split(string(cmdlineData), "\x00")
		firstArg := cleanProcArg(args[0])

		for _, target := range targets {
			cleanTarget := cleanProcArg(target)
			for _, arg := range args {
				cleanArg := cleanProcArg(arg)
				if cleanArg == cleanTarget {
					return pid, true
				}
				if !foundArgument && cleanTarget != "" && strings.Contains(cleanArg, cleanTarget) {
					argumentPID = pid
					foundArgument = true
				}
			}
		}

		if installDir != "" {
			for _, arg := range args {
				if pathWithin(cleanProcArg(arg), installDir) {
					if !foundInstallArgument {
						installArgumentPID = pid
						foundInstallArgument = true
					}
					break
				}
			}
			if !foundFallback && pathWithin(firstArg, installDir) {
				fallbackPID = pid
				foundFallback = true
			}
		}
	}

	if foundArgument {
		return argumentPID, true
	}
	if foundInstallArgument {
		return installArgumentPID, true
	}
	if foundFallback {
		return fallbackPID, true
	}
	return 0, false
}

func pathWithin(path, dir string) bool {
	path = cleanProcArg(path)
	dir = cleanProcArg(dir)
	if path == "" || dir == "" {
		return false
	}

	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil || filepath.IsAbs(rel) || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
