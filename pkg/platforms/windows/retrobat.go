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
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/rs/zerolog/log"
)

// findRetroBatDir attempts to locate the RetroBat installation directory
// and returns the path with the actual filesystem case to prevent case-sensitivity
// issues with EmulationStation's path comparisons.
func findRetroBatDir(cfg *config.Instance) (string, error) {
	// Check user-configured directory first
	if def, ok := cfg.LookupLauncherDefaults("RetroBat"); ok && def.InstallDir != "" {
		if normalizedPath, err := mediascanner.FindPath(def.InstallDir); err == nil {
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
				if normalizedPath, err := mediascanner.FindPath(path); err == nil {
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
		Kill: func(_ *config.Instance) error {
			log.Debug().Msg("killing game via EmulationStation API")

			// Try to kill via API with retries and verification (like Batocera does)
			maxRetries := 5
			for i := range maxRetries {
				// Check if game is still running
				_, running, err := esapi.APIRunningGame()
				if err != nil {
					log.Debug().Err(err).Msg("ES API unavailable while checking running game")
				} else if !running {
					log.Info().Msg("game no longer running")
					return nil
				}

				// Game still running, try to kill
				log.Debug().Msgf("game still running, attempting ES API kill: %d/%d", i+1, maxRetries)
				err = esapi.APIEmuKill()
				if err != nil && !errors.Is(err, context.DeadlineExceeded) {
					log.Debug().Err(err).Msg("ES API kill attempt failed")
				}

				if i < maxRetries-1 {
					time.Sleep(500 * time.Millisecond)
				}
			}

			// Final verification
			_, running, _ := esapi.APIRunningGame()
			if !running {
				log.Info().Msg("game stopped after retries")
				return nil
			}

			return fmt.Errorf("failed to kill game via RetroBat ES API after %d retries", maxRetries)
		},
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
