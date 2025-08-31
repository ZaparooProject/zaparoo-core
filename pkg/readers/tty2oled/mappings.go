/*
Zaparoo Core
Copyright (c) 2025 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

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

package tty2oled

import (
	"hash/fnv"
	"strconv"

	"github.com/rs/zerolog/log"
)

// systemToPicture maps Zaparoo system IDs to TTY2OLED picture names
var systemToPicture = map[string]string{
	// Consoles - Direct matches
	"3DO":               "3DO",
	"Dreamcast":         "Dreamcast",
	"Genesis":           "Genesis",
	"NES":               "NES",
	"SNES":              "SNES",
	"PSX":               "PSX",
	"Saturn":            "Saturn",
	"Atari2600":         "ATARI2600",
	"Atari5200":         "ATARI5200",
	"Atari7800":         "ATARI7800",
	"Atari800":          "ATARI800",
	"AtariLynx":         "AtariLynx",
	"GameGear":          "GameGear",
	"Gameboy":           "Gameboy",
	"GameboyColor":      "GameboyColor",
	"GBA":               "GBA",
	"MasterSystem":      "SMS",
	"MegaCD":            "MegaCD",
	"NeoGeo":            "NeoGeo",
	"NeoGeoCD":          "NeoGeoCD",
	"NeoGeoPocket":      "NGP",
	"NeoGeoPocketColor": "NGPC",
	"Nintendo64":        "N64",
	"TurboGrafx16":      "TGFX16",
	"TurboGrafx16CD":    "TGFX16-CD",
	"Vectrex":           "Vectrex",
	"VirtualBoy":        "VirtualBoy",
	"WonderSwan":        "WonderSwan",
	"WonderSwanColor":   "WonderSwanColor",

	// Computers - Direct matches
	"AcornAtom":     "AcornAtom",
	"AcornElectron": "AcornElectron",
	"AliceMC10":     "AliceMC10",
	"Amiga":         "Amiga",
	"Amstrad":       "Amstrad",
	"AppleI":        "APPLE-I",
	"AppleII":       "Apple-II",
	"Aquarius":      "AQUARIUS",
	"C64":           "C64",
	"MSX":           "MSX",
	"ZXSpectrum":    "Spectrum",
	"ZXNext":        "ZXNext",
	"ZX81":          "ZX81",

	// Special mappings
	"AdventureVision": "AVision",
	"Apogee":          "APOGEE",
	"DOS":             "AO486", // DOS games run on AO486 core
	"PC":              "AO486", // PC games run on AO486 core

	// Additional mappings based on picture repository structure
	"Arcadia":       "Arcadia",
	"Astrocade":     "Astrocade",
	"ColecoVision":  "ColecoVision",
	"Intellivision": "Intellivision",
	"Odyssey2":      "Odyssey2",
	"ChannelF":      "ChannelF",
	"CreatiVision":  "CreatiVision",
	"Gamate":        "Gamate",
	"MegaDuck":      "MegaDuck",
	"PokemonMini":   "PokemonMini",
	"SuperVision":   "SuperVision",
	"VC4000":        "VC4000",

	// Additional computer systems
	"BBCMicro": "BBC",
	"TI994A":   "TI99_4A",
	"TRS80":    "TRS-80",
	"VIC20":    "VIC20",
	"X68000":   "X68000",
	"MacPlus":  "MacPlus",
	"PET2001":  "PET2001",
	"CoCo2":    "CoCo2",
}

// picturesWithAlts tracks which pictures have alternative versions and how many
var picturesWithAlts = map[string]int{
	"Genesis":     1, // Genesis_alt1.gsc exists
	"PSX":         5, // Based on what we saw in the repo listing, PSX has multiple alts
	"Minimig":     3, // Based on what we saw in the repo listing
	"000-FMTOWNS": 1, // Based on what we saw in the repo listing
	"3wonders":    1, // Based on what we saw in the repo listing
	"AO486":       1, // Based on what we saw in the repo listing
}

// mapSystemToPicture returns the picture name for a given Zaparoo system ID
func mapSystemToPicture(systemID string) string {
	if pictureName, exists := systemToPicture[systemID]; exists {
		return pictureName
	}
	return "" // No mapping available
}

// selectPictureVariant selects between the base picture and alternative versions
// Uses a hash-based approach to ensure consistency within the same session
func selectPictureVariant(baseName string) string {
	maxAlts, hasAlts := picturesWithAlts[baseName]
	if !hasAlts || maxAlts == 0 {
		return baseName
	}

	// Create a slice with base name + all alternatives
	variants := make([]string, maxAlts+1)
	variants[0] = baseName // Base version

	for i := 1; i <= maxAlts; i++ {
		variants[i] = baseName + "_alt" + strconv.Itoa(i)
	}

	// Use hash-based selection for consistency within the same session
	// This ensures the same variant is selected every time for the same base name
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(baseName)) // Hash.Write never returns an error
	hash := hasher.Sum32()

	// Ensure positive index by using unsigned arithmetic
	variantsLen := len(variants)
	if variantsLen <= 0 {
		log.Error().Str("baseName", baseName).Msg("no variants available")
		return baseName // fallback to original name
	}
	selected := int(hash) % variantsLen

	// Safety check to prevent index out of bounds
	if selected < 0 || selected >= len(variants) {
		log.Error().
			Str("baseName", baseName).
			Int("selected", selected).
			Int("len_variants", len(variants)).
			Uint32("hash", hash).
			Msg("selectPictureVariant: invalid index, returning base name")
		return baseName
	}

	return variants[selected]
}
