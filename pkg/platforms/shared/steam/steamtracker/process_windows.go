//go:build windows

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

package steamtracker

import (
	"os"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

// FindGameProcess finds the running Steam game process by appID.
// It first tries to find the exact executable from appinfo.vdf,
// then falls back to searching for any process in the game's install directory.
// Returns the process handle and PID, or nil/0 if not found.
func FindGameProcess(appID int) (*os.Process, int, error) {
	// Try exact executable lookup first (from appinfo.vdf)
	if proc, pid, found := findByExactExecutable(appID); found {
		return proc, pid, nil
	}

	// Fallback: search by install directory
	return findByInstallDir(appID)
}

// findByExactExecutable looks up the exact game executable from appinfo.vdf
// and searches for a process running that specific binary.
func findByExactExecutable(appID int) (*os.Process, int, bool) {
	// Find Steam directory
	steamDir := findSteamDir()
	if steamDir == "" {
		return nil, 0, false
	}

	// Get exact executable path from appinfo.vdf
	exePath, found := steam.GetGameExecutable(steamDir, appID)
	if !found {
		log.Debug().Int("appID", appID).Msg("could not find executable in appinfo.vdf")
		return nil, 0, false
	}

	log.Debug().Int("appID", appID).Str("exe", exePath).Msg("looking for exact executable")

	exePathLower := strings.ToLower(exePath)

	procs, err := process.Processes()
	if err != nil {
		return nil, 0, false
	}

	for _, p := range procs {
		exe, err := p.Exe()
		if err != nil {
			continue
		}

		if strings.ToLower(exe) == exePathLower {
			pid := int(p.Pid)
			proc, err := os.FindProcess(pid)
			if err != nil {
				continue
			}
			log.Debug().Int("pid", pid).Str("exe", exe).Msg("found game process by exact match")
			return proc, pid, true
		}
	}

	return nil, 0, false
}

// findByInstallDir searches for any process running from the game's install directory.
// This is the fallback method when exact executable lookup fails.
func findByInstallDir(appID int) (*os.Process, int, error) {
	installDir, found := steam.FindInstallDirByAppID(appID)
	if !found {
		log.Debug().Int("appID", appID).Msg("could not find install directory for appID")
		return nil, 0, nil //nolint:nilnil // No install dir means can't find process
	}

	log.Debug().Int("appID", appID).Str("installDir", installDir).Msg("searching for game process by install dir (fallback)")

	procs, err := process.Processes()
	if err != nil {
		return nil, 0, err
	}

	installDirLower := strings.ToLower(installDir)
	for _, p := range procs {
		exe, err := p.Exe()
		if err != nil {
			continue
		}

		if strings.HasPrefix(strings.ToLower(exe), installDirLower) {
			pid := int(p.Pid)
			proc, err := os.FindProcess(pid)
			if err != nil {
				continue
			}
			log.Debug().Int("pid", pid).Str("exe", exe).Msg("found game process by install dir")
			return proc, pid, nil
		}
	}

	return nil, 0, nil //nolint:nilnil // No matching process found
}

// findSteamDir returns the Steam installation directory.
func findSteamDir() string {
	// Check common Windows paths
	paths := []string{
		"C:\\Program Files (x86)\\Steam",
		"C:\\Program Files\\Steam",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}
