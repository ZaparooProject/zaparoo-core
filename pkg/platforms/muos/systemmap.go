//go:build linux && !android

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

package muos

import "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"

// systemKeyMap maps muOS system keys, the manifest file names that appear on
// the third line of a content file, to Zaparoo system IDs. Keys with no
// matching Zaparoo system (Book Reader, Texas Instruments TI-83) are left out.
//
//nolint:gochecknoglobals // Static lookup table.
var systemKeyMap = map[string]string{
	"2048":                                 systemdefs.SystemPC,
	"Amstrad":                              systemdefs.SystemAmstrad,
	"Apple Macintosh":                      systemdefs.SystemMacOS,
	"Arcade":                               systemdefs.SystemArcade,
	"Arduboy":                              systemdefs.SystemArduboy,
	"Atari 2600":                           systemdefs.SystemAtari2600,
	"Atari 5200":                           systemdefs.SystemAtari5200,
	"Atari 7800":                           systemdefs.SystemAtari7800,
	"Atari 800":                            systemdefs.SystemAtari800,
	"Atari Jaguar":                         systemdefs.SystemJaguar,
	"Atari Lynx":                           systemdefs.SystemAtariLynx,
	"Atari ST-STE-TT-Falcon":               systemdefs.SystemAtariST,
	"Bandai WonderSwan Color":              systemdefs.SystemWonderSwanColor,
	"BennuGD":                              systemdefs.SystemBennuGD,
	"Cannonball":                           systemdefs.SystemArcade,
	"Cave Story":                           systemdefs.SystemPC,
	"ChaiLove":                             systemdefs.SystemPC,
	"CHIP-8":                               systemdefs.SystemChip8,
	"ColecoVision":                         systemdefs.SystemColecoVision,
	"Commodore Amiga":                      systemdefs.SystemAmiga,
	"Commodore C128":                       systemdefs.SystemC128,
	"Commodore C64":                        systemdefs.SystemC64,
	"Commodore CBM-II":                     systemdefs.SystemCBMII,
	"Commodore PET":                        systemdefs.SystemPET2001,
	"Commodore Plus-4":                     systemdefs.SystemCommodorePlus4,
	"Commodore VIC-20":                     systemdefs.SystemVIC20,
	"Crisp Game Lib":                       systemdefs.SystemPC,
	"Discrete Integrated Circuit Emulator": systemdefs.SystemDICE,
	"Doom":                                 systemdefs.SystemDoom,
	"Dragon and Tandy":                     systemdefs.SystemDragon32,
	"Elektronika BK":                       systemdefs.SystemBK0011M,
	"Enterprise 128":                       systemdefs.SystemEnterprise,
	"Epoch Super Cassette Vision":          systemdefs.SystemSuperCassetteVision,
	"External - Ports":                     systemdefs.SystemPC,
	"Fairchild Channel F":                  systemdefs.SystemChannelF,
	"Galaksija Retro Computer":             systemdefs.SystemGalaksija,
	"Game Music":                           systemdefs.SystemAudio,
	"Game Tank":                            systemdefs.SystemGameTank,
	"GCE Vectrex":                          systemdefs.SystemVectrex,
	"Handheld Electronic - Game and Watch": systemdefs.SystemGameNWatch,
	"Java J2ME":                            systemdefs.SystemJ2ME,
	"Karaoke":                              systemdefs.SystemAudio,
	"LowRes NX":                            systemdefs.SystemPC,
	"Lua Engine":                           systemdefs.SystemPC,
	"Magnavox Odyssey - VideoPac":          systemdefs.SystemOdyssey2,
	"Mattel Intellivision":                 systemdefs.SystemIntellivision,
	"Media Player":                         systemdefs.SystemVideo,
	"Mega Duck - Cougar Boy":               systemdefs.SystemMegaDuck,
	"Microsoft MSX":                        systemdefs.SystemMSX,
	"MicroW8":                              systemdefs.SystemPC,
	"NEC PC-8000 - PC-8800 Series":         systemdefs.SystemPC88,
	"NEC PC98":                             systemdefs.SystemPC98,
	"NEC PC Engine":                        systemdefs.SystemTurboGrafx16,
	"NEC PC Engine CD":                     systemdefs.SystemTurboGrafx16CD,
	"NEC PC Engine SuperGrafx":             systemdefs.SystemSuperGrafx,
	"NEC PC-FX":                            systemdefs.SystemPCFX,
	"Nintendo 3DS":                         systemdefs.System3DS,
	"Nintendo DS":                          systemdefs.SystemNDS,
	"Nintendo Famicom Disk System":         systemdefs.SystemFDS,
	"Nintendo Game Boy":                    systemdefs.SystemGameboy,
	"Nintendo Game Boy Advance":            systemdefs.SystemGBA,
	"Nintendo Game Boy Color":              systemdefs.SystemGameboyColor,
	"Nintendo N64":                         systemdefs.SystemNintendo64,
	"Nintendo NES - Famicom":               systemdefs.SystemNES,
	"Nintendo Pokemon Mini":                systemdefs.SystemPokemonMini,
	"Nintendo SNES - SFC":                  systemdefs.SystemSNES,
	"Nintendo Virtual Boy":                 systemdefs.SystemVirtualBoy,
	"Onscripter":                           systemdefs.SystemPC,
	"OpenBOR":                              systemdefs.SystemOpenBOR,
	"OpenLara":                             systemdefs.SystemPC,
	"Palm OS":                              systemdefs.SystemPalmOS,
	"PC DOS":                               systemdefs.SystemDOS,
	"Philips CDi":                          systemdefs.SystemCDI,
	"PICO-8":                               systemdefs.SystemPico8,
	"PuzzleScript":                         systemdefs.SystemPC,
	"Pyxel":                                systemdefs.SystemPC,
	"Quake":                                systemdefs.SystemPC,
	"Quake II":                             systemdefs.SystemPC,
	"RPG Maker 2000 - 2003":                systemdefs.SystemPC,
	"ScummVM":                              systemdefs.SystemScummVM,
	"Sega 32X":                             systemdefs.SystemSega32X,
	"Sega Atomiswave Naomi":                systemdefs.SystemNAOMI,
	"Sega Dreamcast":                       systemdefs.SystemDreamcast,
	"Sega Game Gear":                       systemdefs.SystemGameGear,
	"Sega Master System":                   systemdefs.SystemMasterSystem,
	"Sega Mega CD - Sega CD":               systemdefs.SystemMegaCD,
	"Sega Mega Drive - Genesis":            systemdefs.SystemGenesis,
	"Sega Pico":                            systemdefs.SystemSegaPico,
	"Sega Saturn":                          systemdefs.SystemSaturn,
	"Sega SG-1000":                         systemdefs.SystemSG1000,
	"Sharp X1":                             systemdefs.SystemX1,
	"Sharp X68000":                         systemdefs.SystemX68000,
	"Sinclair ZX 81":                       systemdefs.SystemZX81,
	"Sinclair ZX Spectrum":                 systemdefs.SystemZXSpectrum,
	"SNK Neo Geo":                          systemdefs.SystemNeoGeo,
	"SNK Neo Geo CD":                       systemdefs.SystemNeoGeoCD,
	"SNK Neo Geo Pocket - Color":           systemdefs.SystemNeoGeoPocketColor,
	"Sony PlayStation":                     systemdefs.SystemPSX,
	"Sony PlayStation Portable":            systemdefs.SystemPSP,
	"Super Mario War":                      systemdefs.SystemPC,
	"SVI - ColecoVision - SG1000":          systemdefs.SystemSpectravideo,
	"The 3DO Company - 3DO":                systemdefs.System3DO,
	"Thomson MO-TO":                        systemdefs.SystemThomson,
	"TIC-80":                               systemdefs.SystemTIC80,
	"Uzebox":                               systemdefs.SystemUzebox,
	"VeMUlator":                            systemdefs.SystemVMU,
	"Vircon32":                             systemdefs.SystemPC,
	"WASM-4":                               systemdefs.SystemPC,
	"Watara Supervision":                   systemdefs.SystemSuperVision,
	"Wolfenstein 3D":                       systemdefs.SystemPC,
	"Zelda Classic":                        systemdefs.SystemPC,
}
