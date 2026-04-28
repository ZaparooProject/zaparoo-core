//go:build windows

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

package windows

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

const (
	retroBatKillRetries        = 5
	retroBatKillRetryDelay     = 500 * time.Millisecond
	retroBatKillVerifyDelay    = 500 * time.Millisecond
	retroBatProcessKillTimeout = 5 * time.Second
	maxWindowsProcessPath      = uint32(32768)
)

var retroBatFrontendProcesses = map[string]struct{}{
	"emulationstation.exe": {},
	"emulatorlauncher.exe": {},
	"retrobat.exe":         {},
}

var (
	retroBatAPIRunningGame = esapi.APIRunningGame
	retroBatAPIEmuKill     = esapi.APIEmuKill
	retroBatFindDir        = findRetroBatDir
	retroBatListProcesses  = listWindowsProcesses
	retroBatKillPIDTree    = killWindowsProcessTree
	retroBatProcessPath    = windowsProcessImagePath
	retroBatRunTaskKill    = runTaskKillPIDTree
	retroBatSleep          = time.Sleep
)

type windowsProcessInfo struct {
	ExePath string
	PID     uint32
}

// findRetroBatDir attempts to locate the RetroBat installation directory
// and returns the path with the actual filesystem case to prevent case-sensitivity
// issues with EmulationStation's path comparisons.
func findRetroBatDir(cfg *config.Instance) (string, error) {
	// Check user-configured directory first
	if def := cfg.LookupLauncherDefaults("RetroBat", nil); def.InstallDir != "" {
		if normalizedPath, err := mediascanner.FindPath(context.Background(), def.InstallDir); err == nil {
			log.Debug().Msgf("using user-configured RetroBat directory: %s", normalizedPath)
			return normalizedPath, nil
		}
		log.Warn().Msgf("user-configured RetroBat directory not found: %s", def.InstallDir)
	}

	// Common RetroBat installation paths
	paths := []string{
		`C:\RetroBat`,
		`D:\RetroBat`,
		`E:\RetroBat`,
		`C:\Program Files\RetroBat`,
		`C:\Program Files (x86)\RetroBat`,
		`C:\Games\RetroBat`,
	}

	for _, path := range paths {
		if stat, err := os.Stat(path); err == nil && stat.IsDir() {
			// Verify it looks like a RetroBat installation by checking for key files
			retroBatExe := filepath.Join(path, "retrobat.exe")
			if _, err := os.Stat(retroBatExe); err == nil {
				// Use FindPath to get the actual filesystem case
				if normalizedPath, err := mediascanner.FindPath(context.Background(), path); err == nil {
					return normalizedPath, nil
				}
				// Fallback to original path if FindPath fails (shouldn't happen)
				return path, nil
			}
			log.Debug().Msgf("directory exists at %s but retrobat.exe not found", path)
		}
	}

	return "", errors.New("RetroBat installation directory not found")
}

func killRetroBatGame(cfg *config.Instance) error {
	log.Debug().Msg("killing game via EmulationStation API")

	running, err := killRetroBatViaESAPI()
	if err == nil {
		return nil
	}
	if !running {
		return err
	}

	log.Debug().Err(err).Msg("RetroBat ES API kill failed, trying path-scoped process fallback")
	retroBatDir, dirErr := retroBatFindDir(cfg)
	if dirErr != nil {
		return fmt.Errorf(
			"RetroBat ES API kill failed and install directory was not found: %w",
			errors.Join(err, dirErr),
		)
	}

	killed, fallbackErr := killRetroBatEmulatorProcesses(retroBatDir)
	if fallbackErr != nil {
		return fmt.Errorf("RetroBat ES API kill failed and process fallback failed: %w", fallbackErr)
	}
	if killed == 0 {
		return fmt.Errorf("RetroBat ES API kill failed and no RetroBat emulator process was found: %w", err)
	}

	retroBatSleep(retroBatKillVerifyDelay)
	_, running, checkErr := retroBatAPIRunningGame()
	if checkErr != nil {
		log.Debug().Err(checkErr).Msg("ES API unavailable while verifying process fallback")
		return nil
	}
	if !running {
		log.Info().Int("processes", killed).Msg("game stopped after RetroBat process fallback")
		return nil
	}

	return fmt.Errorf("game still running after RetroBat process fallback killed %d process(es)", killed)
}

func killRetroBatViaESAPI() (bool, error) {
	sawRunning := false
	for i := range retroBatKillRetries {
		_, running, err := retroBatAPIRunningGame()
		switch {
		case err != nil:
			log.Debug().Err(err).Msg("ES API unavailable while checking running game")
		case !running:
			log.Info().Msg("game no longer running")
			return false, nil
		default:
			sawRunning = true
		}

		log.Debug().Msgf("game still running, attempting ES API kill: %d/%d", i+1, retroBatKillRetries)
		err = retroBatAPIEmuKill()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			log.Debug().Err(err).Msg("ES API kill attempt failed")
		}

		if i < retroBatKillRetries-1 {
			retroBatSleep(retroBatKillRetryDelay)
		}
	}

	_, running, err := retroBatAPIRunningGame()
	if err == nil && !running {
		log.Info().Msg("game stopped after retries")
		return false, nil
	}
	if err == nil {
		sawRunning = true
	}
	if !sawRunning {
		return false, fmt.Errorf(
			"failed to verify running game via RetroBat ES API after %d retries",
			retroBatKillRetries,
		)
	}

	return true, fmt.Errorf("failed to kill game via RetroBat ES API after %d retries", retroBatKillRetries)
}

func killRetroBatEmulatorProcesses(retroBatDir string) (int, error) {
	processes, err := retroBatListProcesses()
	if err != nil {
		return 0, err
	}

	emulatorsDir := filepath.Join(retroBatDir, "emulators")
	killed := 0
	var killErrs []error
	for _, proc := range processes {
		if !isRetroBatEmulatorProcess(emulatorsDir, proc) {
			continue
		}

		log.Debug().Uint32("pid", proc.PID).Str("path", proc.ExePath).Msg("killing RetroBat emulator process")
		ctx, cancel := context.WithTimeout(context.Background(), retroBatProcessKillTimeout)
		err := retroBatKillPIDTree(ctx, proc.PID, emulatorsDir)
		cancel()
		if err != nil {
			killErrs = append(killErrs, fmt.Errorf("kill pid %d: %w", proc.PID, err))
			continue
		}

		killed++
	}

	if killed == 0 && len(killErrs) > 0 {
		return 0, errors.Join(killErrs...)
	}
	return killed, nil
}

func isRetroBatEmulatorProcess(emulatorsDir string, proc windowsProcessInfo) bool {
	if proc.PID == 0 || proc.ExePath == "" {
		return false
	}
	if _, excluded := retroBatFrontendProcesses[strings.ToLower(filepath.Base(proc.ExePath))]; excluded {
		return false
	}
	return helpers.PathHasPrefix(proc.ExePath, emulatorsDir)
}

func listWindowsProcesses() ([]windowsProcessInfo, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("creating process snapshot: %w", err)
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if firstErr := windows.Process32First(snap, &entry); firstErr != nil {
		return nil, fmt.Errorf("enumerating processes: %w", firstErr)
	}

	var processes []windowsProcessInfo
	for {
		if entry.ProcessID != 0 {
			if exePath, pathErr := windowsProcessImagePath(entry.ProcessID); pathErr == nil {
				processes = append(processes, windowsProcessInfo{
					PID:     entry.ProcessID,
					ExePath: exePath,
				})
			}
		}

		err = windows.Process32Next(snap, &entry)
		if err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, fmt.Errorf("enumerating processes: %w", err)
		}
	}

	return processes, nil
}

func windowsProcessImagePath(pid uint32) (string, error) {
	proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err //nolint:wrapcheck // Access denied is expected for some system processes.
	}
	defer func() { _ = windows.CloseHandle(proc) }()

	buf := make([]uint16, int(maxWindowsProcessPath))
	size := maxWindowsProcessPath
	if err := windows.QueryFullProcessImageName(proc, 0, &buf[0], &size); err != nil {
		return "", err //nolint:wrapcheck // Preserve Windows error for diagnostics.
	}

	return windows.UTF16ToString(buf[:size]), nil
}

func killWindowsProcessTree(ctx context.Context, pid uint32, emulatorsDir string) error {
	exePath, err := retroBatProcessPath(pid)
	if err != nil {
		return fmt.Errorf("revalidating process path for pid %d: %w", pid, err)
	}
	if !isRetroBatEmulatorProcess(emulatorsDir, windowsProcessInfo{PID: pid, ExePath: exePath}) {
		return fmt.Errorf("process pid %d no longer matches RetroBat emulator path", pid)
	}
	return retroBatRunTaskKill(ctx, pid)
}

func runTaskKillPIDTree(ctx context.Context, pid uint32) error {
	pidArg := strconv.FormatUint(uint64(pid), 10)
	cmd := exec.CommandContext( //nolint:gosec // PID comes from local process enumeration.
		ctx,
		"taskkill.exe",
		"/PID",
		pidArg,
		"/T",
		"/F",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("taskkill pid %d: %w: %s", pid, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// createRetroBatLauncher creates a launcher for a specific RetroBat system.
func createRetroBatLauncher(systemFolder string, info esde.SystemInfo) platforms.Launcher {
	launcherID := info.GetLauncherID()
	systemID := info.SystemID
	return platforms.Launcher{
		ID:                 "RetroBat" + launcherID,
		SystemID:           systemID,
		SkipFilesystemScan: true, // Use gamelist.xml via Scanner
		Test: func(cfg *config.Instance, path string) bool {
			retroBatDir, err := findRetroBatDir(cfg)
			if err != nil {
				return false
			}

			systemDir := filepath.Join(retroBatDir, "roms", systemFolder)

			// Use helper to safely check if path is within systemDir
			// Handles Windows slash normalization and prevents "roms" matching "roms2"
			if helpers.PathHasPrefix(path, systemDir) {
				// Don't match directories or .txt files
				if filepath.Ext(path) == "" || filepath.Ext(path) == ".txt" {
					return false
				}
				return true
			}
			return false
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			log.Debug().Str("path", path).Msg("launching game via EmulationStation API")
			err := esapi.APILaunch(path)
			if err != nil {
				return nil, fmt.Errorf("RetroBat ES API launch failed: %w", err)
			}

			log.Info().Str("path", path).Msg("game launched successfully via ES API")
			return nil, nil //nolint:nilnil // API launches don't return a process handle
		},
		Kill: killRetroBatGame,
		Scanner: func(
			_ context.Context,
			cfg *config.Instance,
			_ string,
			_ []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			retroBatDir, err := findRetroBatDir(cfg)
			if err != nil {
				return nil, err
			}

			var results []platforms.ScanResult
			gameListPath := filepath.Join(retroBatDir, "roms", systemFolder, "gamelist.xml")

			gameList, err := esapi.ReadGameListXML(gameListPath)
			if err != nil {
				log.Debug().Msgf("error reading gamelist.xml for %s: %s", systemFolder, err)
				return results, nil // Return empty results, don't error
			}

			for _, game := range gameList.Games {
				results = append(results, platforms.ScanResult{
					Name: game.Name,
					Path: filepath.Join(retroBatDir, "roms", systemFolder, game.Path),
				})
			}

			return results, nil
		},
	}
}

// getRetroBatLaunchers returns RetroBat launchers for all known ES-DE systems.
// Launchers are registered statically; the Test function handles runtime detection.
func getRetroBatLaunchers() []platforms.Launcher {
	launchers := make([]platforms.Launcher, 0, len(esde.SystemMap))
	for folder, info := range esde.SystemMap {
		launchers = append(launchers, createRetroBatLauncher(folder, info))
	}
	return launchers
}
