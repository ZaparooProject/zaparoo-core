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
	// TheGamesDB-specific platform ID mappings
	theGamesDBPlatformMap := map[string]int{
		// Nintendo Consoles
		"nes":        7,    // Nintendo Entertainment System
		"snes":       6,    // Super Nintendo Entertainment System
		"n64":        3,    // Nintendo 64
		"gb":         4,    // Game Boy
		"gbc":        5,    // Game Boy Color
		"gba":        12,   // Game Boy Advance
		"nds":        20,   // Nintendo DS
		"3ds":        4912, // Nintendo 3DS
		"gamecube":   2,    // GameCube
		"wii":        9,    // Nintendo Wii
		"wiiu":       38,   // Nintendo Wii U
		"switch":     4971, // Nintendo Switch
		"virtualboy": 28,   // Virtual Boy
		"pokemini":   4957, // Pokémon Mini

		// Sega Consoles
		"mastersystem": 35, // Sega Master System
		"megadrive":    18, // Sega Mega Drive/Genesis
		"genesis":      18, // Sega Genesis (same as megadrive)
		"segacd":       21, // Sega CD
		"sega32x":      33, // Sega 32X
		"saturn":       17, // Sega Saturn
		"dreamcast":    16, // Sega Dreamcast
		"gamegear":     25, // Sega Game Gear

		// Sony Consoles
		"psx":    10,   // Sony PlayStation
		"ps2":    11,   // Sony PlayStation 2
		"ps3":    12,   // Sony PlayStation 3
		"ps4":    4919, // Sony PlayStation 4
		"ps5":    4976, // Sony PlayStation 5
		"psp":    13,   // PlayStation Portable
		"psvita": 39,   // PlayStation Vita

		// Microsoft Consoles
		"xbox":       14,   // Microsoft Xbox
		"xbox360":    15,   // Microsoft Xbox 360
		"xboxone":    4920, // Microsoft Xbox One
		"xboxseries": 4977, // Microsoft Xbox Series X/S

		// Atari Systems
		"atari2600":   22,   // Atari 2600
		"atari5200":   26,   // Atari 5200
		"atari7800":   27,   // Atari 7800
		"atarist":     4943, // Atari ST
		"atarilynx":   24,   // Atari Lynx
		"atarijaguar": 29,   // Atari Jaguar

		// NEC Systems
		"pcengine":   34,   // PC Engine/TurboGrafx-16
		"pcenginecd": 4955, // PC Engine CD/TurboGrafx-CD
		"supergrafx": 4955, // SuperGrafx (use same as PC Engine CD)

		// SNK Systems
		"neogeo":   24,   // Neo Geo
		"neogeocd": 4956, // Neo Geo CD
		"ngp":      4922, // Neo Geo Pocket
		"ngpc":     4923, // Neo Geo Pocket Color

		// Other Consoles
		"colecovision":  31,   // ColecoVision
		"intellivision": 32,   // Intellivision
		"vectrex":       4945, // Vectrex
		"channelf":      4928, // Fairchild Channel F
		"odyssey2":      4927, // Magnavox Odyssey²
		"sg1000":        4949, // Sega SG-1000

		// Arcade
		"arcade": 23, // Arcade
		"mame":   23, // MAME (use arcade)
		"fbneo":  23, // FBNeo (use arcade)

		// Computer Systems
		"amiga":      4911, // Commodore Amiga
		"c64":        40,   // Commodore 64
		"amstradcpc": 4946, // Amstrad CPC
		"zxspectrum": 4913, // ZX Spectrum
		"msx":        4929, // MSX
		"msx2":       4929, // MSX2 (use same as MSX)
		"apple2":     4942, // Apple II
		"dos":        1,    // PC (DOS)
		"pc":         1,    // PC
		"windows":    1,    // Windows PC

		// Modern Handhelds
		"gp32":   4936, // GamePark GP32
		"gp2x":   4937, // GamePark GP2X
		"wiz":    4938, // GamePark Wiz
		"caanoo": 4939, // GamePark Caanoo
		"dingux": 4940, // Dingux devices

		// Mobile
		"android": 4916, // Android
		"ios":     4915, // iOS
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
