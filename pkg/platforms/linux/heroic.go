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

package linux

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
)

// heroicGameInfo represents a game entry in Heroic's library JSON files
type heroicGameInfo struct {
	AppName     string `json:"app_name"` //nolint:tagliatelle // External JSON format from Heroic
	Title       string `json:"title"`
	Runner      string `json:"runner"`
	IsInstalled bool   `json:"is_installed"` //nolint:tagliatelle // External JSON format from Heroic
}

// ScanHeroicGames scans Heroic Games Launcher library files for installed games
func ScanHeroicGames(storeCacheDir string) ([]platforms.ScanResult, error) {
	results := make([]platforms.ScanResult, 0)

	// Check if store_cache directory exists
	if _, err := os.Stat(storeCacheDir); os.IsNotExist(err) {
		log.Debug().Msg("Heroic store_cache directory not found")
		return results, nil
	}

	// Scan Epic Games (legendary_library.json)
	epicResults, err := scanHeroicLibraryFile(
		filepath.Join(storeCacheDir, "legendary_library.json"),
		"library",
	)
	if err != nil {
		log.Warn().Err(err).Msg("failed to scan Heroic Epic Games library")
	} else {
		results = append(results, epicResults...)
	}

	// Scan GOG games (gog_library.json)
	gogResults, err := scanHeroicLibraryFile(
		filepath.Join(storeCacheDir, "gog_library.json"),
		"games",
	)
	if err != nil {
		log.Warn().Err(err).Msg("failed to scan Heroic GOG library")
	} else {
		results = append(results, gogResults...)
	}

	log.Debug().Msgf("found %d Heroic games", len(results))
	return results, nil
}

// scanHeroicLibraryFile parses a single Heroic library JSON file and returns scan results
func scanHeroicLibraryFile(filePath, jsonKey string) ([]platforms.ScanResult, error) {
	results := make([]platforms.ScanResult, 0)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Debug().Msgf("Heroic library file not found: %s", filePath)
		return results, nil
	}

	// Read the JSON file
	data, err := os.ReadFile(filePath) //nolint:gosec // filePath is constructed from known directory
	if err != nil {
		return results, fmt.Errorf("failed to read Heroic library file: %w", err)
	}

	// Parse JSON - structure is { "library": [...] } or { "games": [...] }
	var libraryData map[string][]heroicGameInfo
	if err := json.Unmarshal(data, &libraryData); err != nil {
		return results, fmt.Errorf("failed to parse Heroic library JSON: %w", err)
	}

	// Get the games array
	games, ok := libraryData[jsonKey]
	if !ok {
		log.Debug().Msgf("Heroic library file missing expected key: %s", jsonKey)
		return results, nil
	}

	// Filter for installed games and build scan results
	for _, game := range games {
		// Skip non-installed games
		if !game.IsInstalled {
			continue
		}

		// Skip games without an app_name
		if game.AppName == "" {
			log.Debug().Msgf("Heroic game missing app_name: %s", game.Title)
			continue
		}

		results = append(results, platforms.ScanResult{
			Name:  game.Title,
			Path:  helpers.CreateVirtualPath(shared.SchemeHeroic, game.AppName, game.Title),
			NoExt: true,
		})
	}

	return results, nil
}

// NewHeroicLauncher creates a launcher for Heroic Games Launcher
func NewHeroicLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:       "Heroic",
		SystemID: systemdefs.SystemPC,
		Schemes:  []string{shared.SchemeHeroic},
		Scanner: func(
			_ context.Context,
			_ *config.Instance,
			_ string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			// Check if Heroic is installed
			_, err := exec.LookPath("heroic")
			if err != nil {
				log.Debug().Err(err).Msg("Heroic Games Launcher not found in PATH, skipping scanner")
				// Not an error condition - just means Heroic isn't installed
				return results, nil
			}

			// Try to scan Heroic library at ~/.config/heroic/store_cache/
			home, err := os.UserHomeDir()
			if err != nil {
				return results, fmt.Errorf("failed to get user home directory: %w", err)
			}

			storeCacheDir := filepath.Join(home, ".config", "heroic", "store_cache")

			// Scan Heroic libraries for installed games
			heroicResults, err := ScanHeroicGames(storeCacheDir)
			if err != nil {
				log.Warn().Err(err).Msg("failed to scan Heroic games, continuing without them")
			} else {
				results = append(results, heroicResults...)
			}

			return results, nil
		},
		Launch: func(_ *config.Instance, path string) (*os.Process, error) {
			// Extract game app name from heroic://appName format
			appName, err := helpers.ExtractSchemeID(path, shared.SchemeHeroic)
			if err != nil {
				return nil, fmt.Errorf("failed to extract Heroic game name from path: %w", err)
			}

			// Launch via heroic command
			cmd := exec.CommandContext( //nolint:gosec // App name from internal database
				context.Background(),
				"heroic",
				"launch",
				appName,
			)
			err = cmd.Start()
			if err != nil {
				return nil, fmt.Errorf("failed to start Heroic Games Launcher: %w", err)
			}
			return nil, nil
		},
	}
}
