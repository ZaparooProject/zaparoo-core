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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
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

// isRetroBatRunning checks if RetroBat (EmulationStation) is running and API is accessible
func isRetroBatRunning() bool {
	return esapi.IsAvailable()
}

// getRetroBatSystemMapping maps RetroBat system folder names to Zaparoo system IDs
// Based on RetroBat's es_systems.cfg: https://github.com/RetroBat-Official/retrobat-setup
func getRetroBatSystemMapping() map[string]string {
	return map[string]string{
		// Arcade Systems
		"mame":       systemdefs.SystemArcade,
		"fbneo":      systemdefs.SystemArcade,
		"atomiswave": systemdefs.SystemAtomiswave,
		"naomi":      systemdefs.SystemNAOMI,
		"naomi2":     systemdefs.SystemNAOMI2,
		"model2":     systemdefs.SystemModel2,
		"model3":     systemdefs.SystemModel3,
		"triforce":   systemdefs.SystemTriforce,
		"chihiro":    systemdefs.SystemChihiro,
		"hikaru":     systemdefs.SystemHikaru,
		"gaelco":     systemdefs.SystemGaelco,
		"cps1":       systemdefs.SystemCPS1,
		"cps2":       systemdefs.SystemCPS2,
		"cps3":       systemdefs.SystemCPS3,
		"daphne":     systemdefs.SystemDAPHNE,
		"singe":      systemdefs.SystemSinge,
		"dice":       systemdefs.SystemDICE,

		// Sega Consoles
		"sg1000":       systemdefs.SystemSG1000,
		"mastersystem": systemdefs.SystemMasterSystem,
		"megadrive":    systemdefs.SystemGenesis,
		"megacd":       systemdefs.SystemMegaCD,
		"sega32x":      systemdefs.SystemSega32X,
		"saturn":       systemdefs.SystemSaturn,
		"dreamcast":    systemdefs.SystemDreamcast,
		"gamegear":     systemdefs.SystemGameGear,

		// Nintendo Consoles
		"nes":         systemdefs.SystemNES,
		"fds":         systemdefs.SystemFDS,
		"snes":        systemdefs.SystemSNES,
		"snes-msu1":   systemdefs.SystemSNESMSU1,
		"satellaview": systemdefs.SystemSNES, // Satellaview uses SNES hardware
		"sufami":      systemdefs.SystemSufami,
		"n64":         systemdefs.SystemNintendo64,
		"gamecube":    systemdefs.SystemGameCube,
		"wii":         systemdefs.SystemWii,
		"wiiu":        systemdefs.SystemWiiU,
		"switch":      systemdefs.SystemSwitch,
		"virtualboy":  systemdefs.SystemVirtualBoy,

		// Nintendo Handhelds
		"gb":          systemdefs.SystemGameboy,
		"gb2players":  systemdefs.SystemGameboy2P,
		"gb-msu":      systemdefs.SystemSGBMSU1,
		"gbc":         systemdefs.SystemGameboyColor,
		"gbc2players": systemdefs.SystemGameboyColor, // Uses same system
		"gba":         systemdefs.SystemGBA,
		"gba2players": systemdefs.SystemGBA2P,
		"nds":         systemdefs.SystemNDS,
		"pokemini":    systemdefs.SystemPokemonMini,
		"gw":          systemdefs.SystemGameNWatch,

		// Sony Consoles
		"psx": systemdefs.SystemPSX,
		"ps2": systemdefs.SystemPS2,
		"ps3": systemdefs.SystemPS3,
		"ps4": systemdefs.SystemPS4,

		// Microsoft Consoles
		"xbox":    systemdefs.SystemXbox,
		"xbox360": systemdefs.SystemXbox360,

		// NEC Consoles
		"pcengine":   systemdefs.SystemTurboGrafx16,
		"pcenginecd": systemdefs.SystemTurboGrafx16CD,
		"supergrafx": systemdefs.SystemSuperGrafx,
		"pcfx":       systemdefs.SystemPCFX,

		// SNK Consoles
		"neogeo":   systemdefs.SystemNeoGeo,
		"neogeocd": systemdefs.SystemNeoGeoCD,

		// Atari Consoles
		"atari2600": systemdefs.SystemAtari2600,
		"atari5200": systemdefs.SystemAtari5200,
		"atari7800": systemdefs.SystemAtari7800,
		"lynx":      systemdefs.SystemAtariLynx,
		"jaguar":    systemdefs.SystemJaguar,
		"jaguarcd":  systemdefs.SystemJaguarCD,

		// Other Consoles
		"3do":           systemdefs.System3DO,
		"colecovision":  systemdefs.SystemColecoVision,
		"intellivision": systemdefs.SystemIntellivision,
		"channelf":      systemdefs.SystemChannelF,
		"vectrex":       systemdefs.SystemVectrex,
		"odyssey2":      systemdefs.SystemOdyssey2,
		"multivision":   systemdefs.SystemMultivision,

		// Computer Systems
		"amiga500":     systemdefs.SystemAmiga500,
		"amiga1200":    systemdefs.SystemAmiga1200,
		"amigacd32":    systemdefs.SystemAmigaCD32,
		"amstradcpc":   systemdefs.SystemAmstrad,
		"atari800":     systemdefs.SystemAtari800,
		"atarist":      systemdefs.SystemAtariST,
		"xegs":         systemdefs.SystemAtariXEGS,
		"c64":          systemdefs.SystemC64,
		"msx1":         systemdefs.SystemMSX1,
		"msx2":         systemdefs.SystemMSX2,
		"msx2+":        systemdefs.SystemMSX2Plus,
		"zxspectrum":   systemdefs.SystemZXSpectrum,
		"zx81":         systemdefs.SystemZX81,
		"x68000":       systemdefs.SystemX68000,
		"x1":           systemdefs.SystemX1,
		"pc88":         systemdefs.SystemPC88,
		"pc98":         systemdefs.SystemPC98,
		"aquarius":     systemdefs.SystemAquarius,
		"samcoupe":     systemdefs.SystemSAMCoupe,
		"thomson":      systemdefs.SystemThomson,
		"spectravideo": systemdefs.SystemSpectravideo,
		"oricatmos":    systemdefs.SystemOric,
	}
}

// createRetroBatLauncher creates a launcher for a specific RetroBat system
func createRetroBatLauncher(systemFolder, systemID, _ string) platforms.Launcher {
	return platforms.Launcher{
		ID:                 fmt.Sprintf("RetroBat%s", systemID),
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
			if !isRetroBatRunning() {
				return nil, errors.New("RetroBat is not running")
			}

			log.Debug().Str("path", path).Msg("launching game via EmulationStation API")
			err := esapi.APILaunch(path)
			if err != nil {
				return nil, fmt.Errorf("RetroBat API request failed: %w", err)
			}

			log.Info().Str("path", path).Msg("game launched successfully via ES API")
			return nil, nil
		},
		Kill: func(_ *config.Instance) error {
			if !isRetroBatRunning() {
				return errors.New("RetroBat is not running")
			}

			log.Debug().Msg("killing game via EmulationStation API")

			// Try to kill via API with retries (like Batocera does)
			maxRetries := 3
			for i := range maxRetries {
				err := esapi.APIEmuKill()
				if err == nil {
					log.Info().Msg("emulator killed successfully via ES API")
					return nil
				}

				if errors.Is(err, context.DeadlineExceeded) {
					log.Debug().Msg("ES API timeout, assuming kill succeeded")
					return nil
				}

				log.Debug().Err(err).Msgf("ES API kill attempt %d/%d failed", i+1, maxRetries)

				if i < maxRetries-1 {
					time.Sleep(500 * time.Millisecond)
				}
			}

			return fmt.Errorf("failed to kill game via RetroBat API after %d retries", maxRetries)
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
	log.Info().Msg("initializing RetroBat launchers")

	retroBatDir, err := findRetroBatDir(cfg)
	if err != nil {
		log.Info().Msgf("RetroBat not found: %s", err)
		return nil
	}

	// Always register launchers if RetroBat directory is found
	log.Info().Msgf("found RetroBat at %s, registering launchers", retroBatDir)

	systemMapping := getRetroBatSystemMapping()
	var launchers []platforms.Launcher

	romsDir := filepath.Join(retroBatDir, "roms")
	entries, err := os.ReadDir(romsDir)
	if err != nil {
		log.Warn().Msgf("error reading RetroBat roms directory at %s: %s", romsDir, err)
		return nil
	}

	log.Info().Msgf("found %d directories in roms folder", len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			systemFolder := entry.Name()
			if systemID, exists := systemMapping[systemFolder]; exists {
				log.Info().Msgf("registering RetroBat launcher for system: %s (mapped to %s)", systemFolder, systemID)
				launcher := createRetroBatLauncher(systemFolder, systemID, retroBatDir)
				launchers = append(launchers, launcher)
			} else {
				log.Debug().Msgf("unmapped RetroBat system folder: %s", systemFolder)
			}
		}
	}

	log.Info().Msgf("successfully registered %d RetroBat launchers", len(launchers))
	return launchers
}
