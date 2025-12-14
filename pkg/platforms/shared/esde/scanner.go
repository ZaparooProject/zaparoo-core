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

package esde

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// GameEntry represents a game from an EmulationStation gamelist.xml file.
type GameEntry struct {
	// Name is the display name of the game
	Name string `xml:"name"`
	// Path is the path to the ROM file (may be relative to the system folder)
	Path string `xml:"path"`
}

// GameList represents the structure of an EmulationStation gamelist.xml file.
type GameList struct {
	XMLName xml.Name    `xml:"gameList"`
	Games   []GameEntry `xml:"game"`
}

// ReadGameList reads and parses a gamelist.xml file from the given path.
func ReadGameList(path string) (GameList, error) {
	cleanPath := filepath.Clean(path)
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Join(".", cleanPath)
	}

	xmlFile, err := os.Open(cleanPath) // #nosec G304 - path is validated above
	if err != nil {
		return GameList{}, fmt.Errorf("failed to open gamelist.xml at %s: %w", cleanPath, err)
	}
	defer func() {
		if closeErr := xmlFile.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing gamelist.xml file")
		}
	}()

	data, err := io.ReadAll(xmlFile)
	if err != nil {
		return GameList{}, fmt.Errorf("failed to read gamelist.xml at %s: %w", cleanPath, err)
	}

	var gameList GameList
	if err := xml.Unmarshal(data, &gameList); err != nil {
		return GameList{}, fmt.Errorf("failed to parse gamelist.xml: %w", err)
	}

	return gameList, nil
}

// ScannerConfig holds configuration for scanning EmulationStation gamelists.
type ScannerConfig struct {
	// RomsBasePath is the base path where ROM folders are located
	RomsBasePath string
	// GamelistBasePath is the base path where gamelist.xml files are located
	// (may differ from RomsBasePath for some platforms like ES-DE)
	GamelistBasePath string
	// SystemFolder is the name of the system folder (e.g., "nes", "snes")
	SystemFolder string
}

// ResolveGamePath resolves a game path from gamelist.xml to an absolute path.
// Gamelist paths may be:
// - Absolute (starts with /)
// - Relative with ./ prefix (./game.rom)
// - Relative without prefix (game.rom)
func ResolveGamePath(gamePath, romsBasePath, systemFolder string) string {
	// Already absolute
	if filepath.IsAbs(gamePath) {
		return filepath.Clean(gamePath)
	}

	// Remove ./ prefix if present
	cleanPath := gamePath
	if strings.HasPrefix(gamePath, "./") {
		cleanPath = gamePath[2:]
	}

	// Build absolute path from roms base + system folder + game path
	return filepath.Join(romsBasePath, systemFolder, cleanPath)
}

// ScanGamelist scans a system's gamelist.xml and returns scan results.
// This function reads the gamelist.xml from the configured path and resolves
// all game paths relative to the roms directory.
func ScanGamelist(cfg ScannerConfig) ([]platforms.ScanResult, error) {
	// Determine gamelist path
	gamelistDir := cfg.RomsBasePath
	if cfg.GamelistBasePath != "" {
		gamelistDir = cfg.GamelistBasePath
	}
	gamelistPath := filepath.Join(gamelistDir, cfg.SystemFolder, "gamelist.xml")

	gameList, err := ReadGameList(gamelistPath)
	if err != nil {
		// Not an error - just no gamelist available
		log.Debug().
			Str("path", gamelistPath).
			Err(err).
			Msg("gamelist.xml not found or unreadable")
		return nil, nil
	}

	results := make([]platforms.ScanResult, 0, len(gameList.Games))
	for _, game := range gameList.Games {
		if game.Path == "" {
			continue
		}

		absPath := ResolveGamePath(game.Path, cfg.RomsBasePath, cfg.SystemFolder)
		results = append(results, platforms.ScanResult{
			Name: game.Name,
			Path: absPath,
		})
	}

	log.Debug().
		Str("system", cfg.SystemFolder).
		Int("count", len(results)).
		Msg("scanned gamelist.xml")

	return results, nil
}

// EnhanceResultsFromGamelist updates scan results with names from gamelist.xml.
// This is useful when filesystem scanning finds files but we want display names
// from the gamelist metadata.
func EnhanceResultsFromGamelist(results map[string]platforms.ScanResult, cfg ScannerConfig) error {
	gamelistDir := cfg.RomsBasePath
	if cfg.GamelistBasePath != "" {
		gamelistDir = cfg.GamelistBasePath
	}
	gamelistPath := filepath.Join(gamelistDir, cfg.SystemFolder, "gamelist.xml")

	gameList, err := ReadGameList(gamelistPath)
	if err != nil {
		// Not an error if gamelist doesn't exist
		log.Debug().
			Str("path", gamelistPath).
			Msg("no gamelist.xml for name enhancement")
		return nil //nolint:nilerr // missing gamelist is not an error condition
	}

	// Build lookup map from gamelist
	nameByPath := make(map[string]string)
	for _, game := range gameList.Games {
		if game.Path == "" || game.Name == "" {
			continue
		}
		absPath := ResolveGamePath(game.Path, cfg.RomsBasePath, cfg.SystemFolder)
		nameByPath[absPath] = game.Name
	}

	// Update results with gamelist names
	for path, result := range results {
		if name, ok := nameByPath[path]; ok {
			result.Name = name
			results[path] = result
		}
	}

	return nil
}

// CreateSystemScanner creates a scanner function for a specific ES system.
// This returns a function compatible with platforms.Launcher.Scanner.
func CreateSystemScanner(
	romsBasePath, gamelistBasePath, systemFolder string,
) func() ([]platforms.ScanResult, error) {
	return func() ([]platforms.ScanResult, error) {
		return ScanGamelist(ScannerConfig{
			RomsBasePath:     romsBasePath,
			GamelistBasePath: gamelistBasePath,
			SystemFolder:     systemFolder,
		})
	}
}
