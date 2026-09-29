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

package libretrothumbs

import "slices"

// playlists maps Core system IDs to libretro-thumbnails playlist folders, in
// the order they are tried. The names are the server's directory names, which
// follow the libretro database, so they change only when libretro renames a
// system.
var playlists = map[string][]string{
	"3DO":                   {"The 3DO Company - 3DO"},
	"3DS":                   {"Nintendo - Nintendo 3DS"},
	"AdventureVision":       {"Entex - Adventure Vision"},
	"Amiga":                 {"Commodore - Amiga"},
	"Amiga500":              {"Commodore - Amiga"},
	"Amiga1200":             {"Commodore - Amiga"},
	"AmigaCD32":             {"Commodore - CD32"},
	"Amstrad":               {"Amstrad - CPC"},
	"AmstradGX4000":         {"Amstrad - GX4000"},
	"Arcade":                {"MAME", "FBNeo - Arcade Games"},
	"Arcadia":               {"Emerson - Arcadia 2001"},
	"Arduboy":               {"Arduboy Inc - Arduboy"},
	"Atari2600":             {"Atari - 2600"},
	"Atari5200":             {"Atari - 5200"},
	"Atari7800":             {"Atari - 7800"},
	"Atari800":              {"Atari - 8-bit"},
	"AtariLynx":             {"Atari - Lynx"},
	"AtariST":               {"Atari - ST"},
	"AtariXEGS":             {"Atari - 8-bit"},
	"Atomiswave":            {"Atomiswave"},
	"C64":                   {"Commodore - 64"},
	"CasioLoopy":            {"Casio - Loopy"},
	"CasioPV1000":           {"Casio - PV-1000"},
	"Cave68000":             {"FBNeo - Arcade Games", "MAME"},
	"CDI":                   {"Philips - CD-i"},
	"ChannelF":              {"Fairchild - Channel F"},
	"ColecoVision":          {"Coleco - ColecoVision"},
	"CommodoreCDTV":         {"Commodore - CDTV"},
	"CommodorePlus4":        {"Commodore - Plus-4"},
	"CPS1":                  {"FBNeo - Arcade Games", "MAME"},
	"CPS2":                  {"FBNeo - Arcade Games", "MAME"},
	"CPS3":                  {"FBNeo - Arcade Games", "MAME"},
	"CreatiVision":          {"VTech - CreatiVision"},
	"DOS":                   {"DOS"},
	"Dreamcast":             {"Sega - Dreamcast"},
	"FDS":                   {"Nintendo - Family Computer Disk System"},
	"GameCom":               {"Tiger - Game.com"},
	"GameCube":              {"Nintendo - GameCube"},
	"GameGear":              {"Sega - Game Gear"},
	"GameMaster":            {"Hartung - Game Master"},
	"Gameboy":               {"Nintendo - Game Boy"},
	"GameboyColor":          {"Nintendo - Game Boy Color"},
	"GBA":                   {"Nintendo - Game Boy Advance"},
	"Genesis":               {"Sega - Mega Drive - Genesis"},
	"GP32":                  {"GamePark - GP32"},
	"HandheldElectronicLCD": {"Handheld Electronic Game"},
	"Intellivision":         {"Mattel - Intellivision"},
	"Jaguar":                {"Atari - Jaguar"},
	"Leapster":              {"LeapFrog - Leapster Learning Game System"},
	"MasterSystem":          {"Sega - Master System - Mark III"},
	"MegaCD":                {"Sega - Mega-CD - Sega CD"},
	"MSX":                   {"Microsoft - MSX", "Microsoft - MSX2"},
	"MSX1":                  {"Microsoft - MSX"},
	"MSX2":                  {"Microsoft - MSX2"},
	"NAOMI":                 {"Sega - Naomi"},
	"NAOMI2":                {"Sega - Naomi 2"},
	"NDS":                   {"Nintendo - Nintendo DS"},
	"NeoGeo":                {"SNK - Neo Geo", "FBNeo - Arcade Games"},
	"NeoGeoAES":             {"SNK - Neo Geo", "FBNeo - Arcade Games"},
	"NeoGeoMVS":             {"SNK - Neo Geo", "FBNeo - Arcade Games"},
	"NeoGeoCD":              {"SNK - Neo Geo CD"},
	"NeoGeoPocket":          {"SNK - Neo Geo Pocket"},
	"NeoGeoPocketColor":     {"SNK - Neo Geo Pocket Color"},
	"NES":                   {"Nintendo - Nintendo Entertainment System"},
	"Nintendo64":            {"Nintendo - Nintendo 64"},
	"Odyssey2":              {"Magnavox - Odyssey2"},
	"PC88":                  {"NEC - PC-8001 - PC-8801"},
	"PC98":                  {"NEC - PC-98"},
	"PCFX":                  {"NEC - PC-FX"},
	"PET2001":               {"Commodore - PET"},
	"PGM":                   {"FBNeo - Arcade Games", "MAME"},
	"PokemonMini":           {"Nintendo - Pokemon Mini"},
	"PS2":                   {"Sony - PlayStation 2"},
	"PS3":                   {"Sony - PlayStation 3"},
	"PS4":                   {"Sony - PlayStation 4"},
	"PSP":                   {"Sony - PlayStation Portable"},
	"PSX":                   {"Sony - PlayStation"},
	"Saturn":                {"Sega - Saturn"},
	"ScummVM":               {"ScummVM"},
	"Sega32X":               {"Sega - 32X"},
	"SegaPico":              {"Sega - PICO"},
	"SG1000":                {"Sega - SG-1000"},
	"SNES":                  {"Nintendo - Super Nintendo Entertainment System"},
	"Sufami":                {"Nintendo - Sufami Turbo"},
	"SuperACan":             {"Funtech - Super Acan"},
	"SuperCassetteVision":   {"Epoch - Super Cassette Vision"},
	"SuperGrafx":            {"NEC - PC Engine SuperGrafx"},
	"SuperVision":           {"Watara - Supervision"},
	"SVI328":                {"Spectravideo - SVI-318 - SVI-328"},
	"Thomson":               {"Thomson - MOTO"},
	"TIC80":                 {"TIC-80"},
	"TurboGrafx16":          {"NEC - PC Engine - TurboGrafx 16"},
	"TurboGrafx16CD":        {"NEC - PC Engine CD - TurboGrafx-CD"},
	"Vectrex":               {"GCE - Vectrex"},
	"VIC20":                 {"Commodore - VIC-20"},
	"VideopacPlus":          {"Philips - Videopac+"},
	"VirtualBoy":            {"Nintendo - Virtual Boy"},
	"Vita":                  {"Sony - PlayStation Vita"},
	"VSmile":                {"VTech - V.Smile"},
	"Wii":                   {"Nintendo - Wii"},
	"WiiU":                  {"Nintendo - Wii U"},
	"WonderSwan":            {"Bandai - WonderSwan"},
	"WonderSwanColor":       {"Bandai - WonderSwan Color"},
	"X1":                    {"Sharp - X1"},
	"X68000":                {"Sharp - X68000"},
	"Xbox":                  {"Microsoft - Xbox"},
	"Xbox360":               {"Microsoft - Xbox 360"},
	"ZX81":                  {"Sinclair - ZX 81"},
	"ZXSpectrum":            {"Sinclair - ZX Spectrum"},
}

// SupportedSystems lists every Core system with a libretro playlist.
func SupportedSystems() []string {
	ids := make([]string, 0, len(playlists))
	for id := range playlists {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
