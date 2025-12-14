//go:build darwin

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
	"path/filepath"
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
	steamDir := findSteamDir()
	if steamDir == "" {
		return nil, 0, false
	}

	exePath, found := steam.GetGameExecutable(steamDir, appID)
	if !found {
		log.Debug().Int("appID", appID).Msg("could not find executable in appinfo.vdf")
		return nil, 0, false
	}

	log.Debug().Int("appID", appID).Str("exe", exePath).Msg("looking for exact executable")

	procs, err := process.Processes()
	if err != nil {
		return nil, 0, false
	}

	for _, p := range procs {
		exe, err := p.Exe()
		if err != nil {
			continue
		}

		if exe == exePath {
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

	for _, p := range procs {
		exe, err := p.Exe()
		if err != nil {
			continue
		}

		if strings.HasPrefix(exe, installDir) {
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

// findSteamDir returns the Steam installation directory on macOS.
func findSteamDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	steamDir := filepath.Join(home, "Library", "Application Support", "Steam")
	if _, err := os.Stat(steamDir); err == nil {
		return steamDir
	}

	return ""
}

// SteamProcess represents a running process that may be a Steam game.
type SteamProcess struct {
	PID         int
	Exe         string
	InstallDir  string
	AppID       int
	GameName    string
}

// FindSteamGameProcesses finds all running processes that appear to be Steam games.
// It scans all processes and checks if they're running from a Steam game directory.
func FindSteamGameProcesses() ([]SteamProcess, error) {
	steamDir := findSteamDir()
	if steamDir == "" {
		return nil, nil
	}

	steamAppsDir := steam.FindSteamAppsDir(steamDir)
	commonDir := filepath.Join(steamAppsDir, "common")

	// Build a map of install directories to appIDs
	installDirMap, err := buildInstallDirMap(steamAppsDir)
	if err != nil {
		log.Debug().Err(err).Msg("failed to build install dir map")
		return nil, nil
	}

	procs, err := process.Processes()
	if err != nil {
		return nil, err
	}

	var results []SteamProcess

	for _, p := range procs {
		exe, err := p.Exe()
		if err != nil {
			continue
		}

		// Check if executable is in Steam's common directory
		if !strings.HasPrefix(exe, commonDir) {
			continue
		}

		// Find which game directory this belongs to
		for installDir, appID := range installDirMap {
			if strings.HasPrefix(exe, installDir) {
				gameName, _ := steam.LookupAppNameInLibraries(steamAppsDir, appID)
				results = append(results, SteamProcess{
					PID:        int(p.Pid),
					Exe:        exe,
					InstallDir: installDir,
					AppID:      appID,
					GameName:   gameName,
				})
				break
			}
		}
	}

	return results, nil
}

// buildInstallDirMap builds a map of install directories to appIDs.
func buildInstallDirMap(steamAppsDir string) (map[string]int, error) {
	result := make(map[string]int)

	// Scan for appmanifest files
	entries, err := os.ReadDir(steamAppsDir)
	if err != nil {
		return nil, err
	}

	commonDir := filepath.Join(steamAppsDir, "common")

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasPrefix(name, "appmanifest_") || !strings.HasSuffix(name, ".acf") {
			continue
		}

		// Extract appID from filename
		appIDStr := strings.TrimPrefix(name, "appmanifest_")
		appIDStr = strings.TrimSuffix(appIDStr, ".acf")

		var appID int
		if _, err := strings.CutPrefix(appIDStr, ""); err || appIDStr == "" {
			continue
		}

		for i := 0; i < len(appIDStr); i++ {
			if appIDStr[i] < '0' || appIDStr[i] > '9' {
				continue
			}
		}

		// Parse appID
		appID = 0
		for _, c := range appIDStr {
			if c < '0' || c > '9' {
				appID = 0
				break
			}
			appID = appID*10 + int(c-'0')
		}

		if appID == 0 {
			continue
		}

		// Read manifest to get install directory name
		info, ok := steam.ReadAppManifest(steamAppsDir, appID)
		if !ok || info.InstallDir == "" {
			continue
		}

		installPath := filepath.Join(commonDir, info.InstallDir)
		result[installPath] = appID
	}

	return result, nil
}
