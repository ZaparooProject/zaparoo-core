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
	// Only map the systems that are actually different from the base
	// or need specific IGDB platform IDs
	igdbPlatformMap := map[string]int{
		// Nintendo Consoles
		"nes":          18,  // Nintendo Entertainment System
		"famicom":      18,  // Famicom (same as NES)
		"snes":         19,  // Super Nintendo Entertainment System
		"superfamicom": 19,  // Super Famicom (same as SNES)
		"n64":          4,   // Nintendo 64
		"gb":           33,  // Game Boy
		"gbc":          22,  // Game Boy Color
		"gba":          24,  // Game Boy Advance
		"nds":          20,  // Nintendo DS
		"3ds":          37,  // Nintendo 3DS
		"gamecube":     21,  // GameCube
		"wii":          5,   // Nintendo Wii
		"wiiu":         41,  // Nintendo Wii U
		"switch":       130, // Nintendo Switch
		"virtualboy":   87,  // Virtual Boy
		"pokemini":     152, // Pokémon Mini

		// Sega Consoles
		"mastersystem": 64, // Sega Master System
		"megadrive":    29, // Sega Mega Drive/Genesis
		"genesis":      29, // Sega Genesis (same as megadrive)
		"segacd":       78, // Sega CD
		"sega32x":      30, // Sega 32X
		"saturn":       32, // Sega Saturn
		"dreamcast":    23, // Sega Dreamcast
		"gamegear":     35, // Sega Game Gear
		"sg1000":       84, // Sega SG-1000

		// Sony Consoles
		"psx":    7,   // Sony PlayStation
		"ps2":    8,   // Sony PlayStation 2
		"ps3":    9,   // Sony PlayStation 3
		"ps4":    48,  // Sony PlayStation 4
		"ps5":    167, // Sony PlayStation 5
		"psp":    38,  // PlayStation Portable
		"psvita": 46,  // PlayStation Vita

		// Microsoft Consoles
		"xbox":       11,  // Microsoft Xbox
		"xbox360":    12,  // Microsoft Xbox 360
		"xboxone":    49,  // Microsoft Xbox One
		"xboxseries": 169, // Microsoft Xbox Series X/S

		// Atari Systems
		"atari2600":   59, // Atari 2600
		"atari5200":   66, // Atari 5200
		"atari7800":   60, // Atari 7800
		"atarist":     63, // Atari ST
		"atarilynx":   28, // Atari Lynx
		"atarijaguar": 62, // Atari Jaguar

		// NEC Systems
		"pcengine":   86,  // PC Engine/TurboGrafx-16
		"pcenginecd": 150, // PC Engine CD/TurboGrafx-CD
		"supergrafx": 128, // SuperGrafx

		// SNK Systems
		"neogeo":   80,  // Neo Geo
		"neogeocd": 136, // Neo Geo CD
		"ngp":      119, // Neo Geo Pocket
		"ngpc":     120, // Neo Geo Pocket Color

		// Other Consoles
		"colecovision":  68, // ColecoVision
		"intellivision": 67, // Intellivision
		"vectrex":       70, // Vectrex
		"channelf":      69, // Fairchild Channel F
		"odyssey2":      75, // Magnavox Odyssey²

		// Arcade
		"arcade": 52, // Arcade
		"mame":   52, // MAME (use arcade)
		"fbneo":  52, // FBNeo (use arcade)

		// Computer Systems
		"amiga":      16, // Commodore Amiga
		"c64":        15, // Commodore 64
		"amstradcpc": 25, // Amstrad CPC
		"zxspectrum": 26, // ZX Spectrum
		"msx":        27, // MSX
		"msx2":       27, // MSX2 (use same as MSX)
		"apple2":     31, // Apple II
		"dos":        13, // PC (DOS)
		"pc":         6,  // PC
		"windows":    6,  // Windows PC
		"linux":      3,  // Linux
		"mac":        14, // Mac

		// Modern Handhelds
		"gp32":   121, // GamePark GP32
		"gp2x":   122, // GamePark GP2X
		"wiz":    123, // GamePark Wiz
		"caanoo": 124, // GamePark Caanoo

		// Mobile
		"android": 34, // Android
		"ios":     39, // iOS

		// Other Systems
		"3do":             50,  // 3DO
		"jaguar":          62,  // Atari Jaguar (duplicate mapping for completeness)
		"cdi":             133, // Philips CD-i
		"cdtv":            129, // Commodore CDTV
		"cd32":            130, // Amiga CD32
		"pippin":          112, // Apple Pippin
		"wonderswan":      57,  // WonderSwan
		"wonderswancolor": 58,  // WonderSwan Color
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
