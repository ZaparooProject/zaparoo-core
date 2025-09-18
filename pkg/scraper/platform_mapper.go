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
// It contains the authoritative list of system IDs that all scrapers should support
type BasePlatformMapper struct {
	// Common system IDs to normalized platform names - base for all scrapers
	commonMappings map[string]string
}

// NewBasePlatformMapper creates a new base platform mapper with common mappings
func NewBasePlatformMapper() *BasePlatformMapper {
	return &BasePlatformMapper{
		commonMappings: map[string]string{
			// Nintendo systems
			"nes":          "nintendo-entertainment-system",
			"famicom":      "nintendo-entertainment-system",
			"snes":         "super-nintendo",
			"superfamicom": "super-nintendo",
			"n64":          "nintendo-64",
			"gb":           "game-boy",
			"gbc":          "game-boy-color",
			"gba":          "game-boy-advance",
			"nds":          "nintendo-ds",
			"3ds":          "nintendo-3ds",
			"gc":           "nintendo-gamecube",
			"gamecube":     "nintendo-gamecube",
			"wii":          "nintendo-wii",
			"wiiu":         "nintendo-wii-u",
			"switch":       "nintendo-switch",
			"virtualboy":   "nintendo-virtual-boy",
			"pokemini":     "pokemon-mini",

			// Sega systems
			"sg1000":       "sega-sg-1000",
			"sms":          "sega-master-system",
			"mastersystem": "sega-master-system",
			"genesis":      "sega-genesis",
			"megadrive":    "sega-genesis",
			"scd":          "sega-cd",
			"segacd":       "sega-cd",
			"s32x":         "sega-32x",
			"sega32x":      "sega-32x",
			"saturn":       "sega-saturn",
			"dreamcast":    "sega-dreamcast",
			"gg":           "sega-game-gear",
			"gamegear":     "sega-game-gear",

			// Sony systems
			"psx":    "sony-playstation",
			"ps2":    "sony-playstation-2",
			"ps3":    "sony-playstation-3",
			"ps4":    "sony-playstation-4",
			"ps5":    "sony-playstation-5",
			"psp":    "sony-psp",
			"vita":   "sony-vita",
			"psvita": "sony-vita",

			// Microsoft systems
			"xbox":       "microsoft-xbox",
			"xbox360":    "microsoft-xbox-360",
			"xboxone":    "microsoft-xbox-one",
			"xboxseries": "microsoft-xbox-series",

			// Atari systems
			"atari2600":   "atari-2600",
			"atari5200":   "atari-5200",
			"atari7800":   "atari-7800",
			"atarist":     "atari-st",
			"lynx":        "atari-lynx",
			"atarilynx":   "atari-lynx",
			"jaguar":      "atari-jaguar",
			"atarijaguar": "atari-jaguar",

			// NEC systems
			"pcengine":   "turbografx-16",
			"tg16":       "turbografx-16",
			"pcenginecd": "turbografx-cd",
			"supergrafx": "supergrafx",
			"pcfx":       "pc-fx",

			// SNK systems
			"neogeo":   "neo-geo",
			"neogeocd": "neo-geo-cd",
			"ngp":      "neo-geo-pocket",
			"ngpc":     "neo-geo-pocket-color",

			// Arcade
			"arcade": "arcade",
			"mame":   "arcade",
			"fba":    "arcade",
			"fbneo":  "arcade",
			"cps1":   "arcade",
			"cps2":   "arcade",
			"cps3":   "arcade",

			// Other consoles
			"coleco":        "colecovision",
			"colecovision":  "colecovision",
			"intellivision": "intellivision",
			"vectrex":       "vectrex",
			"channelf":      "fairchild-channel-f",
			"odyssey2":      "magnavox-odyssey-2",
			"o2em":          "magnavox-odyssey-2",

			// Computer systems
			"amiga":      "commodore-amiga",
			"c64":        "commodore-64",
			"amstradcpc": "amstrad-cpc",
			"zxspectrum": "sinclair-zx-spectrum",
			"msx":        "msx",
			"msx2":       "msx2",
			"apple2":     "apple-ii",
			"dos":        "pc",
			"pc":         "pc",
			"windows":    "pc",
			"linux":      "linux",
			"mac":        "mac",

			// Handheld systems
			"wonderswan":      "wonderswan",
			"wswan":           "wonderswan",
			"wswanc":          "wonderswan-color",
			"wonderswancolor": "wonderswan-color",
			"gp32":            "gamepark-gp32",
			"gp2x":            "gamepark-gp2x",
			"wiz":             "gamepark-wiz",
			"caanoo":          "gamepark-caanoo",

			// Mobile
			"android": "android",
			"ios":     "ios",

			// Other systems
			"3do":    "3do",
			"cdi":    "philips-cd-i",
			"cdtv":   "commodore-cdtv",
			"cd32":   "amiga-cd32",
			"pippin": "apple-pippin",
		},
	}
}

// MapToScraperPlatform maps a zaparoo system ID to a normalized platform name
func (m *BasePlatformMapper) MapToScraperPlatform(systemID string) (string, bool) {
	platform, exists := m.commonMappings[systemID]
	return platform, exists
}

// GetNormalizedPlatform returns the normalized platform name for a system ID
// This is useful for scrapers that need the common platform name
func (m *BasePlatformMapper) GetNormalizedPlatform(systemID string) string {
	if platform, exists := m.commonMappings[systemID]; exists {
		return platform
	}
	return ""
}

// MapFromScraperPlatform maps a normalized platform name back to zaparoo system ID
func (m *BasePlatformMapper) MapFromScraperPlatform(scraperPlatformID string) (string, bool) {
	for systemID, platform := range m.commonMappings {
		if platform == scraperPlatformID {
			return systemID, true
		}
	}
	return "", false
}

// HasSystemID checks if a system ID is supported
func (m *BasePlatformMapper) HasSystemID(systemID string) bool {
	_, exists := m.commonMappings[systemID]
	return exists
}

// GetSupportedSystems returns all zaparoo system IDs supported by this mapper
func (m *BasePlatformMapper) GetSupportedSystems() []string {
	systems := make([]string, 0, len(m.commonMappings))
	for systemID := range m.commonMappings {
		systems = append(systems, systemID)
	}
	return systems
}
