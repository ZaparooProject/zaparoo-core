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

package thegamesdb

import (
	"strconv"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
)

// PlatformMapper maps zaparoo system IDs to TheGamesDB platform IDs
// Based on Batocera EmulationStation's TheGamesDB platform mappings
type PlatformMapper struct {
	*scraper.BasePlatformMapper
	theGamesDBPlatformMap map[string]int
}

// NewPlatformMapper creates a new platform mapper with TheGamesDB mappings
func NewPlatformMapper() *PlatformMapper {
	// Build TheGamesDB platform map from central definitions
	theGamesDBPlatformMap := make(map[string]int)
	for zaparooID, platformIDs := range scraper.PlatformDefinitions {
		if platformIDs.TheGamesDB != 0 {
			theGamesDBPlatformMap[zaparooID] = platformIDs.TheGamesDB
		}
	}

	// Add TheGamesDB-specific mappings not in central definitions
	theGamesDBSpecificMappings := map[string]int{
		"virtualboy":    28,   // Virtual Boy
		"pokemini":      4957, // Pokémon Mini
		"mastersystem":  35,   // Sega Master System (alias for sms)
		"segacd":        21,   // Sega CD
		"sega32x":       33,   // Sega 32X
		"gamegear":      25,   // Sega Game Gear (alias for gg)
		"psvita":        39,   // PlayStation Vita (alias for vita)
		"xboxseries":    4977, // Microsoft Xbox Series X/S
		"atari5200":     26,   // Atari 5200
		"atarist":       4943, // Atari ST
		"atarilynx":     24,   // Atari Lynx (alias for lynx)
		"atarijaguar":   29,   // Atari Jaguar (alias for jaguar)
		"pcengine":      34,   // PC Engine/TurboGrafx-16
		"pcenginecd":    4955, // PC Engine CD/TurboGrafx-CD
		"supergrafx":    4955, // SuperGrafx (use same as PC Engine CD)
		"neogeocd":      4956, // Neo Geo CD
		"ngp":           4922, // Neo Geo Pocket
		"ngpc":          4923, // Neo Geo Pocket Color
		"colecovision":  31,   // ColecoVision
		"intellivision": 32,   // Intellivision
		"vectrex":       4945, // Vectrex
		"channelf":      4928, // Fairchild Channel F
		"odyssey2":      4927, // Magnavox Odyssey²
		"sg1000":        4949, // Sega SG-1000
		"fbneo":         23,   // FBNeo (use arcade)
		"amstradcpc":    4946, // Amstrad CPC
		"zxspectrum":    4913, // ZX Spectrum
		"msx":           4929, // MSX
		"msx2":          4929, // MSX2 (use same as MSX)
		"apple2":        4942, // Apple II
		"windows":       1,    // Windows PC (alias for pc)
		"gp32":          4936, // GamePark GP32
		"gp2x":          4937, // GamePark GP2X
		"wiz":           4938, // GamePark Wiz
		"caanoo":        4939, // GamePark Caanoo
		"dingux":        4940, // Dingux devices
	}

	// Merge TheGamesDB-specific mappings
	for systemID, platformID := range theGamesDBSpecificMappings {
		theGamesDBPlatformMap[systemID] = platformID
	}

	return &PlatformMapper{
		BasePlatformMapper:    scraper.NewBasePlatformMapper(),
		theGamesDBPlatformMap: theGamesDBPlatformMap,
	}
}

// MapToScraperPlatform maps a zaparoo system ID to TheGamesDB platform ID
func (pm *PlatformMapper) MapToScraperPlatform(systemID string) (string, bool) {
	// Check TheGamesDB-specific mappings first
	if platformID, exists := pm.theGamesDBPlatformMap[systemID]; exists {
		return strconv.Itoa(platformID), true
	}

	// For systems not in TheGamesDB, check if they exist in base mapper
	if pm.HasSystemID(systemID) {
		return "", true // System exists but no TheGamesDB ID
	}

	return "", false
}

// GetSupportedSystems returns a list of all supported system IDs
func (pm *PlatformMapper) GetSupportedSystems() []string {
	// Return all systems from base mapper since TheGamesDB can potentially scrape
	// any system (even if we don't have a specific platform ID for it)
	return pm.BasePlatformMapper.GetSupportedSystems()
}

// GetTheGamesDBPlatformID returns the specific TheGamesDB platform ID for a system
func (pm *PlatformMapper) GetTheGamesDBPlatformID(systemID string) (int, bool) {
	platformID, exists := pm.theGamesDBPlatformMap[systemID]
	return platformID, exists
}
