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

package esde

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/rs/zerolog/log"
)

const maxMetadataLength = 4096

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

// ResolveGamePath resolves a gamelist path inside its declared system root.
// Absolute paths are accepted only when they remain under that root.
func ResolveGamePath(gamePath, romsBasePath, systemFolder string) string {
	systemRoot := filepath.Clean(filepath.Join(romsBasePath, systemFolder))
	candidate := filepath.Clean(gamePath)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(systemRoot, strings.TrimPrefix(candidate, "."+string(filepath.Separator)))
	}
	if !pathWithin(systemRoot, candidate) {
		return ""
	}
	return candidate
}

func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func trustedGamePath(gamePath, romsBasePath, systemFolder string) string {
	candidate := ResolveGamePath(gamePath, romsBasePath, systemFolder)
	if candidate == "" {
		return ""
	}
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return ""
	}
	systemRoot := filepath.Clean(filepath.Join(romsBasePath, systemFolder))
	realRoot, err := filepath.EvalSymlinks(systemRoot)
	if err != nil {
		return ""
	}
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil || !pathWithin(realRoot, realCandidate) {
		return ""
	}
	return candidate
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

	games, err := esapi.ReadGameReferencesXML(gamelistPath)
	if err != nil {
		// Not an error - just no gamelist available
		log.Debug().
			Str("path", gamelistPath).
			Err(err).
			Msg("gamelist.xml not found or unreadable")
		return nil, nil
	}

	results := make([]platforms.ScanResult, 0, len(games))
	for _, game := range games {
		if game.Path == "" || len(game.Path) > maxMetadataLength || len(game.Name) > maxMetadataLength {
			continue
		}

		absPath := trustedGamePath(game.Path, cfg.RomsBasePath, cfg.SystemFolder)
		if absPath == "" {
			log.Warn().Str("system", cfg.SystemFolder).Msg("ignored unsafe ES-DE gamelist path")
			continue
		}
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

	games, err := esapi.ReadGameReferencesXML(gamelistPath)
	if err != nil {
		// Not an error if gamelist doesn't exist
		log.Debug().
			Str("path", gamelistPath).
			Msg("no gamelist.xml for name enhancement")
		return nil //nolint:nilerr // missing gamelist is not an error condition
	}

	// Build lookup map from gamelist
	nameByPath := make(map[string]string)
	for _, game := range games {
		if game.Path == "" || game.Name == "" ||
			len(game.Path) > maxMetadataLength || len(game.Name) > maxMetadataLength {
			continue
		}
		absPath := trustedGamePath(game.Path, cfg.RomsBasePath, cfg.SystemFolder)
		if absPath != "" {
			nameByPath[absPath] = game.Name
		}
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
