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

package screenscraper

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
)

// PlatformMapper handles mapping between zaparoo system IDs and ScreenScraper platform IDs
// Based on Batocera EmulationStation's screenscraper_platformid_map
type PlatformMapper struct {
	*scraper.BasePlatformMapper
	// ScreenScraper-specific platform ID mappings
	screenScraperMappings map[string]string
}

// NewPlatformMapper creates a new ScreenScraper platform mapper
func NewPlatformMapper() *PlatformMapper {
	return &PlatformMapper{
		BasePlatformMapper: scraper.NewBasePlatformMapper(),
		// ScreenScraper platform IDs - copied EXACTLY from Batocera's implementation
		screenScraperMappings: map[string]string{
			// Nintendo systems
			"nes":    "1",   // Nintendo Entertainment System
			"snes":   "2",   // Super Nintendo Entertainment System
			"n64":    "14",  // Nintendo 64
			"gb":     "9",   // Game Boy
			"gbc":    "10",  // Game Boy Color
			"gba":    "12",  // Game Boy Advance
			"nds":    "15",  // Nintendo DS
			"3ds":    "17",  // Nintendo 3DS
			"gc":     "13",  // Nintendo GameCube
			"wii":    "16",  // Nintendo Wii
			"wiiu":   "18",  // Nintendo Wii U
			"switch": "225", // Nintendo Switch

			// Sega systems
			"sg1000":    "109", // Sega SG-1000
			"sms":       "2",   // Sega Master System
			"genesis":   "1",   // Sega Genesis/Mega Drive
			"megadrive": "1",   // Sega Genesis/Mega Drive (alt name)
			"scd":       "20",  // Sega CD
			"s32x":      "19",  // Sega 32X
			"saturn":    "22",  // Sega Saturn
			"dreamcast": "23",  // Sega Dreamcast
			"gg":        "21",  // Sega Game Gear

			// Sony systems
			"psx":  "57", // Sony PlayStation
			"ps2":  "58", // Sony PlayStation 2
			"ps3":  "59", // Sony PlayStation 3
			"ps4":  "60", // Sony PlayStation 4
			"ps5":  "61", // Sony PlayStation 5
			"psp":  "62", // Sony PlayStation Portable
			"vita": "63", // Sony PlayStation Vita

			// Microsoft systems
			"xbox":    "32", // Microsoft Xbox
			"xbox360": "33", // Microsoft Xbox 360
			"xboxone": "34", // Microsoft Xbox One

			// Arcade
			"arcade": "75",  // Arcade
			"mame":   "75",  // MAME
			"fba":    "75",  // FinalBurn Alpha
			"cps1":   "6",   // Capcom Play System 1
			"cps2":   "7",   // Capcom Play System 2
			"cps3":   "8",   // Capcom Play System 3
			"neogeo": "142", // SNK Neo Geo

			// Atari systems
			"atari2600": "26", // Atari 2600
			"atari5200": "40", // Atari 5200
			"atari7800": "41", // Atari 7800
			"lynx":      "28", // Atari Lynx
			"jaguar":    "27", // Atari Jaguar

			// Other consoles
			"coleco":        "48",  // ColecoVision
			"intellivision": "115", // Mattel Intellivision
			"vectrex":       "102", // Vectrex
			"odyssey2":      "104", // Magnavox Odyssey 2
			"pcengine":      "31",  // PC Engine/TurboGrafx-16
			"tg16":          "31",  // TurboGrafx-16
			"pcfx":          "72",  // PC-FX
			"wonderswan":    "45",  // WonderSwan
			"wswan":         "45",  // WonderSwan (alt name)
			"wswanc":        "46",  // WonderSwan Color
			"ngp":           "25",  // Neo Geo Pocket
			"ngpc":          "82",  // Neo Geo Pocket Color

			// Computer systems
			"amstradcpc": "65",  // Amstrad CPC
			"apple2":     "86",  // Apple II
			"c64":        "66",  // Commodore 64
			"amiga":      "64",  // Commodore Amiga
			"msx":        "113", // MSX
			"msx2":       "116", // MSX2
			"zxspectrum": "76",  // Sinclair ZX Spectrum
			"atarist":    "42",  // Atari ST
			"pc":         "135", // PC
			"dos":        "135", // DOS

			// Handheld systems
			"gameandwatch":  "52",  // Game & Watch
			"tigerhandheld": "207", // Tiger Handheld

			// Other systems
			"channelf":    "80",  // Fairchild Channel F
			"o2em":        "104", // Odyssey 2
			"thomson":     "141", // Thomson
			"to8":         "141", // Thomson TO8
			"mo5":         "121", // Thomson MO5
			"sam":         "213", // SAM Coupé
			"x68000":      "79",  // Sharp X68000
			"x1":          "220", // Sharp X1
			"fm7":         "97",  // Fujitsu FM-7
			"fmtowns":     "105", // FM Towns
			"pc88":        "221", // NEC PC-88
			"pc98":        "83",  // NEC PC-98
			"scv":         "67",  // Epoch Super Cassette Vision
			"supervision": "207", // Watara Supervision
			"gp32":        "146", // GamePark GP32
			"gp2x":        "146", // GamePark GP2X
			"cavestory":   "135", // Cave Story (PC)
			"pico8":       "234", // PICO-8
			"tic80":       "232", // TIC-80
		},
	}
}

// MapToScraperPlatform maps a zaparoo system ID to a ScreenScraper platform ID
func (pm *PlatformMapper) MapToScraperPlatform(systemID string) (string, bool) {
	// Check ScreenScraper-specific mappings
	if platformID, exists := pm.screenScraperMappings[systemID]; exists {
		return platformID, true
	}

	// For systems not specifically mapped, check if they exist in base mapper
	if pm.BasePlatformMapper.HasSystemID(systemID) {
		return "", true // System exists but no specific ScreenScraper ID
	}

	return "", false
}

// MapFromScraperPlatform maps a ScreenScraper platform ID back to zaparoo system ID
func (pm *PlatformMapper) MapFromScraperPlatform(scraperPlatformID string) (string, bool) {
	for systemID, platformID := range pm.screenScraperMappings {
		if platformID == scraperPlatformID {
			return systemID, true
		}
	}
	return "", false
}

// GetSupportedSystems returns all zaparoo system IDs supported by ScreenScraper
func (pm *PlatformMapper) GetSupportedSystems() []string {
	// Return all systems from base mapper since ScreenScraper can potentially scrape
	// any system (even if we don't have a specific platform ID for it)
	return pm.BasePlatformMapper.GetSupportedSystems()
}

// GetScreenScraperPlatformID returns the numeric platform ID for ScreenScraper
func (pm *PlatformMapper) GetScreenScraperPlatformID(systemID string) (string, bool) {
	platformID, exists := pm.screenScraperMappings[systemID]
	return platformID, exists
}
