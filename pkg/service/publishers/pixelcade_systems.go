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

package publishers

import (
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

// systemToPixelCade maps Zaparoo system IDs to PixelCade console folder names.
// See https://pixelcade.org/developers for the list of supported console names.
var systemToPixelCade = map[string]string{
	// Nintendo consoles
	systemdefs.SystemNES:          "nes",
	systemdefs.SystemNESMusic:     "nes",
	systemdefs.SystemFDS:          "nintendo_famicom_disk_system",
	systemdefs.SystemSNES:         "snes",
	systemdefs.SystemSNESMSU1:     "snes",
	systemdefs.SystemSNESMusic:    "snes",
	systemdefs.SystemNintendo64:   "n64",
	systemdefs.SystemGameCube:     "nintendo_gamecube",
	systemdefs.SystemWii:          "nintendo_wii",
	systemdefs.SystemSwitch:       "switch",
	systemdefs.SystemGameboy:      "gb",
	systemdefs.SystemGameboy2P:    "gb",
	systemdefs.SystemGameboyColor: "gbc",
	systemdefs.SystemGBA:          "gba",
	systemdefs.SystemGBA2P:        "gba",
	systemdefs.SystemSuperGameboy: "nintendo_super_game_boy",
	systemdefs.SystemSGBMSU1:      "nintendo_super_game_boy",
	systemdefs.SystemGameNWatch:   "nintendo_game_and_watch",
	systemdefs.SystemVirtualBoy:   "nintendo_virtual_boy",
	systemdefs.SystemNDS:          "nds",
	systemdefs.System3DS:          "3ds",

	// Sega consoles
	systemdefs.SystemMasterSystem: "mastersystem",
	systemdefs.SystemGenesis:      "genesis",
	systemdefs.SystemGenesisMSU:   "genesis",
	systemdefs.SystemSega32X:      "genesis",
	systemdefs.SystemGameGear:     "gamegear",
	systemdefs.SystemMegaCD:       "segacd",
	systemdefs.SystemSaturn:       "sega_saturn",
	systemdefs.SystemDreamcast:    "dreamcast",
	systemdefs.SystemSG1000:       "sg1000",
	systemdefs.SystemNAOMI:        "sega_naomi",
	systemdefs.SystemNAOMI2:       "sega_naomi",

	// Sony consoles
	systemdefs.SystemPSX:  "psx",
	systemdefs.SystemPS2:  "ps2",
	systemdefs.SystemPSP:  "psp",
	systemdefs.SystemVita: "vita",

	// Atari
	systemdefs.SystemAtari2600: "atari2600",
	systemdefs.SystemAtari5200: "atari5200",
	systemdefs.SystemAtari7800: "atari7800",
	systemdefs.SystemAtariLynx: "atarilynx",
	systemdefs.SystemJaguar:    "atarijaguar",
	systemdefs.SystemJaguarCD:  "atarijaguar",

	// NEC
	systemdefs.SystemTurboGrafx16:   "pcengine",
	systemdefs.SystemTurboGrafx16CD: "pcengine",
	systemdefs.SystemSuperGrafx:     "nec_supergrafx",
	systemdefs.SystemPCFX:           "pcfx",

	// SNK
	systemdefs.SystemNeoGeo:            "neogeo",
	systemdefs.SystemNeoGeoAES:         "neogeo",
	systemdefs.SystemNeoGeoMVS:         "neogeo",
	systemdefs.SystemNeoGeoCD:          "neogeo",
	systemdefs.SystemNeoGeoPocket:      "neogeopocket",
	systemdefs.SystemNeoGeoPocketColor: "neogeopocketcolor",

	// Other consoles
	systemdefs.System3DO:             "panasonic_3do",
	systemdefs.SystemColecoVision:    "coleco",
	systemdefs.SystemIntellivision:   "intellivision",
	systemdefs.SystemVectrex:         "vectrex",
	systemdefs.SystemOdyssey2:        "magnavox_odyssey_2",
	systemdefs.SystemVideopacPlus:    "magnavox_odyssey_2",
	systemdefs.SystemCDI:             "cdi",
	systemdefs.SystemChannelF:        "channelf",
	systemdefs.SystemWonderSwan:      "bandai_wonderswan",
	systemdefs.SystemWonderSwanColor: "bandai_wonderswan",
	systemdefs.SystemXbox:            "microsoft_xbox",
	systemdefs.SystemXbox360:         "xbox360",

	// Arcade
	systemdefs.SystemArcade:     "mame",
	systemdefs.SystemCPS1:       "capcom_play_system",
	systemdefs.SystemCPS2:       "capcom_play_system_ii",
	systemdefs.SystemCPS3:       "capcom_play_system_iii",
	systemdefs.SystemDAPHNE:     "daphne",
	systemdefs.SystemSinge:      "daphne",
	systemdefs.SystemAtomiswave: "atomiswave",

	// Computers
	systemdefs.SystemC64:       "c64",
	systemdefs.SystemC16:       "c64",
	systemdefs.SystemVIC20:     "commodore_vic-20",
	systemdefs.SystemAmiga:     "amiga",
	systemdefs.SystemAmiga500:  "amiga",
	systemdefs.SystemAmiga1200: "amiga",
	systemdefs.SystemAmigaCD32: "amiga",
	systemdefs.SystemAmstrad:   "amstradcpc",
	systemdefs.SystemAppleII:   "apple2",
	systemdefs.SystemDOS:       "dos",
	systemdefs.SystemPC:        "windows",
	systemdefs.SystemWindows:   "windows",
	systemdefs.SystemMSX:       "msx",
	systemdefs.SystemMSX1:      "msx",
	systemdefs.SystemMSX2:      "msx",
	systemdefs.SystemMSX2Plus:  "msx",
	systemdefs.SystemScummVM:   "scummvm",
	systemdefs.SystemAtari800:  "atari800",
	systemdefs.SystemAtariST:   "atarist",
}

// pixelCadeConsoleName returns the PixelCade console folder name for a given
// Zaparoo system ID. If no mapping exists, it falls back to lowercasing the
// system ID.
func pixelCadeConsoleName(systemID string) string {
	if name, ok := systemToPixelCade[systemID]; ok {
		return name
	}
	return strings.ToLower(systemID)
}
