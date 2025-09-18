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

package igdb

import (
	"strconv"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
)

// PlatformMapper maps zaparoo system IDs to IGDB platform IDs
// Based on Batocera EmulationStation's IGDB platform mappings
type PlatformMapper struct {
	*scraper.BasePlatformMapper
	igdbPlatformMap map[string]int
}

// NewPlatformMapper creates a new platform mapper with IGDB mappings
func NewPlatformMapper() *PlatformMapper {
	// Build IGDB platform map from central definitions
	igdbPlatformMap := make(map[string]int)
	for zaparooID, platformIDs := range scraper.PlatformDefinitions {
		if platformIDs.IGDB != 0 {
			igdbPlatformMap[zaparooID] = platformIDs.IGDB
		}
	}

	// Add IGDB-specific mappings not in central definitions
	igdbSpecificMappings := map[string]int{
		"virtualboy":      87,  // Virtual Boy
		"pokemini":        152, // Pokémon Mini
		"mastersystem":    64,  // Sega Master System (alias for sms)
		"segacd":          78,  // Sega CD
		"sega32x":         30,  // Sega 32X
		"gamegear":        35,  // Game Gear (alias for gg)
		"sg1000":          84,  // Sega SG-1000
		"psvita":          46,  // PlayStation Vita (alias for vita)
		"xboxseries":      169, // Microsoft Xbox Series X/S
		"atari5200":       66,  // Atari 5200
		"atarist":         63,  // Atari ST
		"atarilynx":       28,  // Atari Lynx (alias for lynx)
		"atarijaguar":     62,  // Atari Jaguar (alias for jaguar)
		"pcengine":        86,  // PC Engine/TurboGrafx-16
		"pcenginecd":      150, // PC Engine CD/TurboGrafx-CD
		"supergrafx":      128, // SuperGrafx
		"neogeocd":        136, // Neo Geo CD
		"ngp":             119, // Neo Geo Pocket
		"ngpc":            120, // Neo Geo Pocket Color
		"colecovision":    68,  // ColecoVision
		"intellivision":   67,  // Intellivision
		"vectrex":         70,  // Vectrex
		"channelf":        69,  // Fairchild Channel F
		"odyssey2":        75,  // Magnavox Odyssey²
		"fbneo":           52,  // FBNeo (use arcade)
		"amstradcpc":      25,  // Amstrad CPC
		"zxspectrum":      26,  // ZX Spectrum
		"msx":             27,  // MSX
		"msx2":            27,  // MSX2 (use same as MSX)
		"apple2":          31,  // Apple II
		"windows":         6,   // Windows PC (alias for pc)
		"linux":           3,   // Linux
		"mac":             14,  // Mac
		"gp32":            121, // GamePark GP32
		"gp2x":            122, // GamePark GP2X
		"wiz":             123, // GamePark Wiz
		"caanoo":          124, // GamePark Caanoo
		"3do":             50,  // 3DO
		"cdi":             133, // Philips CD-i
		"cdtv":            129, // Commodore CDTV
		"cd32":            130, // Amiga CD32
		"pippin":          112, // Apple Pippin
		"wonderswan":      57,  // WonderSwan
		"wonderswancolor": 58,  // WonderSwan Color
	}

	// Merge IGDB-specific mappings
	for systemID, platformID := range igdbSpecificMappings {
		igdbPlatformMap[systemID] = platformID
	}

	return &PlatformMapper{
		BasePlatformMapper: scraper.NewBasePlatformMapper(),
		igdbPlatformMap:    igdbPlatformMap,
	}
}

// MapToScraperPlatform maps a zaparoo system ID to IGDB platform ID
func (pm *PlatformMapper) MapToScraperPlatform(systemID string) (string, bool) {
	// Check IGDB-specific mappings first
	if platformID, exists := pm.igdbPlatformMap[systemID]; exists {
		return strconv.Itoa(platformID), true
	}

	// For systems not in IGDB, check if they exist in base mapper
	// This maintains compatibility but doesn't return an ID
	if pm.HasSystemID(systemID) {
		return "", true // System exists but no IGDB ID
	}

	return "", false
}

// GetSupportedSystems returns a list of all supported system IDs
func (pm *PlatformMapper) GetSupportedSystems() []string {
	// Return all systems from base mapper since IGDB can potentially scrape
	// any system (even if we don't have a specific platform ID for it)
	return pm.BasePlatformMapper.GetSupportedSystems()
}

// GetIGDBPlatformID returns the specific IGDB platform ID for a system
func (pm *PlatformMapper) GetIGDBPlatformID(systemID string) (int, bool) {
	platformID, exists := pm.igdbPlatformMap[systemID]
	return platformID, exists
}
