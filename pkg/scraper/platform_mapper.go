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

package scraper

// PlatformMapper handles mapping between zaparoo system IDs and scraper-specific platform IDs
type PlatformMapper interface {
	// MapToScraperPlatform maps a zaparoo system ID to a scraper-specific platform ID
	MapToScraperPlatform(systemID string) (string, bool)

	// MapFromScraperPlatform maps a scraper-specific platform ID to a zaparoo system ID
	MapFromScraperPlatform(scraperPlatformID string) (string, bool)

	// GetSupportedSystems returns all zaparoo system IDs supported by this mapper
	GetSupportedSystems() []string
}

// BasePlatformMapper provides common platform mappings based on Batocera EmulationStation
type BasePlatformMapper struct {
	// Common mappings that most scrapers should support
	commonMappings map[string]string
}

// NewBasePlatformMapper creates a new base platform mapper with common mappings
func NewBasePlatformMapper() *BasePlatformMapper {
	return &BasePlatformMapper{
		commonMappings: map[string]string{
			// Nintendo systems
			"nes":    "nintendo",
			"snes":   "super-nintendo",
			"n64":    "nintendo-64",
			"gb":     "game-boy",
			"gbc":    "game-boy-color",
			"gba":    "game-boy-advance",
			"nds":    "nintendo-ds",
			"3ds":    "nintendo-3ds",
			"gc":     "nintendo-gamecube",
			"wii":    "nintendo-wii",
			"wiiu":   "nintendo-wii-u",
			"switch": "nintendo-switch",

			// Sega systems
			"sg1000":    "sega-sg-1000",
			"sms":       "sega-master-system",
			"genesis":   "sega-genesis",
			"megadrive": "sega-genesis",
			"scd":       "sega-cd",
			"s32x":      "sega-32x",
			"saturn":    "sega-saturn",
			"dreamcast": "sega-dreamcast",
			"gg":        "sega-game-gear",

			// Sony systems
			"psx":  "sony-playstation",
			"ps2":  "sony-playstation-2",
			"ps3":  "sony-playstation-3",
			"ps4":  "sony-playstation-4",
			"ps5":  "sony-playstation-5",
			"psp":  "sony-psp",
			"vita": "sony-vita",

			// Microsoft systems
			"xbox":    "microsoft-xbox",
			"xbox360": "microsoft-xbox-360",
			"xboxone": "microsoft-xbox-one",

			// Arcade
			"arcade": "arcade",
			"mame":   "arcade",
			"fba":    "arcade",
			"cps1":   "arcade",
			"cps2":   "arcade",
			"cps3":   "arcade",
			"neogeo": "neo-geo",

			// Other systems
			"atari2600":     "atari-2600",
			"atari5200":     "atari-5200",
			"atari7800":     "atari-7800",
			"lynx":          "atari-lynx",
			"jaguar":        "atari-jaguar",
			"coleco":        "colecovision",
			"intellivision": "mattel-intellivision",
			"vectrex":       "vectrex",
			"odyssey2":      "magnavox-odyssey-2",
			"pcengine":      "turbografx-16",
			"tg16":          "turbografx-16",
			"pcfx":          "pc-fx",
			"wonderswan":    "wonderswan",
			"wswan":         "wonderswan",
			"wswanc":        "wonderswan-color",
			"ngp":           "neo-geo-pocket",
			"ngpc":          "neo-geo-pocket-color",

			// Computer systems
			"amstradcpc": "amstrad-cpc",
			"apple2":     "apple-ii",
			"c64":        "commodore-64",
			"amiga":      "commodore-amiga",
			"msx":        "msx",
			"msx2":       "msx2",
			"zxspectrum": "sinclair-zx-spectrum",
			"atarist":    "atari-st",
			"pc":         "pc",
			"dos":        "pc",
		},
	}
}

// MapToScraperPlatform maps a zaparoo system ID to a common platform name
func (m *BasePlatformMapper) MapToScraperPlatform(systemID string) (string, bool) {
	platform, exists := m.commonMappings[systemID]
	return platform, exists
}

// MapFromScraperPlatform maps a common platform name back to zaparoo system ID
func (m *BasePlatformMapper) MapFromScraperPlatform(scraperPlatformID string) (string, bool) {
	for systemID, platform := range m.commonMappings {
		if platform == scraperPlatformID {
			return systemID, true
		}
	}
	return "", false
}

// GetSupportedSystems returns all zaparoo system IDs supported by this mapper
func (m *BasePlatformMapper) GetSupportedSystems() []string {
	systems := make([]string, 0, len(m.commonMappings))
	for systemID := range m.commonMappings {
		systems = append(systems, systemID)
	}
	return systems
}
