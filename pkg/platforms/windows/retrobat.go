//go:build windows

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/rs/zerolog/log"
)

// findRetroBatDir attempts to locate the RetroBat installation directory
func findRetroBatDir(cfg *config.Instance) (string, error) {
	// Check user-configured directory first
	if def, ok := cfg.LookupLauncherDefaults("RetroBat"); ok && def.InstallDir != "" {
		if _, err := os.Stat(def.InstallDir); err == nil {
			log.Debug().Msgf("using user-configured RetroBat directory: %s", def.InstallDir)
			return def.InstallDir, nil
		}
		log.Warn().Msgf("user-configured RetroBat directory not found: %s", def.InstallDir)
	}

	// Common RetroBat installation paths
	paths := []string{
		`C:\Retrobat`,
		`D:\Retrobat`,
		`E:\Retrobat`,
		`C:\Program Files\Retrobat`,
		`C:\Program Files (x86)\Retrobat`,
		`C:\Games\Retrobat`,
	}

	for _, path := range paths {
		if stat, err := os.Stat(path); err == nil && stat.IsDir() {
			// Verify it looks like a RetroBat installation by checking for key files
			if _, err := os.Stat(filepath.Join(path, "retrobat.exe")); err == nil {
				log.Debug().Msgf("found RetroBat installation at: %s", path)
				return path, nil
			}
		}
	}

	return "", errors.New("RetroBat installation directory not found")
}

// isRetroBatRunning checks if RetroBat (EmulationStation) is running and API is accessible
func isRetroBatRunning() bool {
	return esapi.IsAvailable()
}

// getRetroBatSystemMapping maps RetroBat system folder names to Zaparoo system IDs
func getRetroBatSystemMapping() map[string]string {
	// This maps RetroBat system folder names to Zaparoo SystemIDs
	// Based on common RetroBat system folders
	return map[string]string{
		"3do":             systemdefs.System3DO,
		"amiga":           systemdefs.SystemAmiga,
		"amstradcpc":      systemdefs.SystemAmstrad,
		"arcade":          systemdefs.SystemArcade,
		"atari2600":       systemdefs.SystemAtari2600,
		"atari5200":       systemdefs.SystemAtari5200,
		"atari7800":       systemdefs.SystemAtari7800,
		"atarilynx":       systemdefs.SystemAtariLynx,
		"atarist":         systemdefs.SystemAtariST,
		"c64":             systemdefs.SystemC64,
		"dreamcast":       systemdefs.SystemDreamcast,
		"fds":             systemdefs.SystemFDS,
		"gamegear":        systemdefs.SystemGameGear,
		"gb":              systemdefs.SystemGameboy,
		"gba":             systemdefs.SystemGBA,
		"gbc":             systemdefs.SystemGameboyColor,
		"gc":              systemdefs.SystemGameCube,
		"genesis":         systemdefs.SystemGenesis,
		"mame":            systemdefs.SystemArcade,
		"mastersystem":    systemdefs.SystemMasterSystem,
		"msx":             systemdefs.SystemMSX,
		"n64":             systemdefs.SystemNintendo64,
		"nds":             systemdefs.SystemNDS,
		"neogeo":          systemdefs.SystemNeoGeo,
		"neogeocd":        systemdefs.SystemNeoGeoCD,
		"nes":             systemdefs.SystemNES,
		"ngp":             systemdefs.SystemNeoGeoPocket,
		"ngpc":            systemdefs.SystemNeoGeoPocketColor,
		"pc":              systemdefs.SystemPC,
		"pcengine":        systemdefs.SystemTurboGrafx16,
		"pcenginecd":      systemdefs.SystemTurboGrafx16CD,
		"pokemini":        systemdefs.SystemPokemonMini,
		"psx":             systemdefs.SystemPSX,
		"ps2":             systemdefs.SystemPS2,
		"saturn":          systemdefs.SystemSaturn,
		"snes":            systemdefs.SystemSNES,
		"virtualboy":      systemdefs.SystemVirtualBoy,
		"wonderswan":      systemdefs.SystemWonderSwan,
		"wonderswancolor": systemdefs.SystemWonderSwanColor,
	}
}

// createRetroBatLauncher creates a launcher for a specific RetroBat system
func createRetroBatLauncher(systemFolder, systemID, _ string) platforms.Launcher {
	return platforms.Launcher{
		ID:                 fmt.Sprintf("RetroBat%s", systemID),
		SystemID:           systemID,
		Folders:            []string{filepath.Join("roms", systemFolder)},
		SkipFilesystemScan: true, // Use gamelist.xml via Scanner
		Test: func(cfg *config.Instance, path string) bool {
			// Check if path is within this RetroBat system directory
			retroBatDir, err := findRetroBatDir(cfg)
			if err != nil {
				return false
			}

			systemDir := filepath.Join(retroBatDir, "roms", systemFolder)
			cleanPath := filepath.Clean(strings.ToLower(path))
			cleanSystemDir := filepath.Clean(strings.ToLower(systemDir))

			if strings.HasPrefix(cleanPath, cleanSystemDir) {
				// Don't match directories or .txt files
				if filepath.Ext(path) == "" || filepath.Ext(path) == ".txt" {
					return false
				}
				return true
			}
			return false
		},
		Launch: func(_ *config.Instance, path string) (*os.Process, error) {
			if !isRetroBatRunning() {
				return nil, errors.New("RetroBat/EmulationStation is not running or API not accessible")
			}
			return nil, esapi.APILaunch(path)
		},
		Kill: func(_ *config.Instance) error {
			if !isRetroBatRunning() {
				return errors.New("RetroBat/EmulationStation is not running")
			}
			return esapi.APIEmuKill()
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

// getRetroBatLaunchers returns all available RetroBat launchers
func getRetroBatLaunchers(cfg *config.Instance) []platforms.Launcher {
	retroBatDir, err := findRetroBatDir(cfg)
	if err != nil {
		log.Debug().Msgf("RetroBat not found: %s", err)
		return nil
	}

	if !isRetroBatRunning() {
		log.Debug().Msg("RetroBat/EmulationStation not running, launchers disabled")
		return nil
	}

	systemMapping := getRetroBatSystemMapping()
	var launchers []platforms.Launcher

	romsDir := filepath.Join(retroBatDir, "roms")
	entries, err := os.ReadDir(romsDir)
	if err != nil {
		log.Debug().Msgf("error reading RetroBat roms directory: %s", err)
		return nil
	}

	for _, entry := range entries {
		if entry.IsDir() {
			systemFolder := entry.Name()
			if systemID, exists := systemMapping[systemFolder]; exists {
				launcher := createRetroBatLauncher(systemFolder, systemID, retroBatDir)
				launchers = append(launchers, launcher)
			} else {
				log.Debug().Msgf("unmapped RetroBat system: %s", systemFolder)
			}
		}
	}

	log.Info().Msgf("loaded %d RetroBat launchers", len(launchers))
	return launchers
}
