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

package systemdefs

import (
	"fmt"
	"sort"
	"strings"
)

// The Systems list contains all the supported systems such as consoles,
// computers and media types that are indexable by Zaparoo. This is the reference
// list of hardcoded system IDs used throughout Zaparoo. A platform can choose
// not to support any of them.
//
// This list also contains some basic heuristics which, given a file path, can
// be used to attempt to associate a file with a system.

type System struct {
	ID        string
	Aliases   []string
	Fallbacks []string
}

// MapKeys returns a list of all keys in a map.
func MapKeys[K comparable, V any](m map[K]V) []K {
	// Copied from utils for circular
	keys := make([]K, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	return keys
}

func AlphaMapKeys[V any](m map[string]V) []string {
	// Copied from utils for circular
	keys := MapKeys(m)
	sort.Strings(keys)
	return keys
}

// GetSystem looks up an exact system definition by ID.
func GetSystem(id string) (*System, error) {
	if system, ok := Systems[id]; ok {
		return &system, nil
	}
	return nil, fmt.Errorf("unknown system: %s", id)
}

// LookupSystem case-insensitively looks up system ID definition including aliases.
func LookupSystem(id string) (*System, error) {
	for k, v := range Systems {
		if strings.EqualFold(k, id) {
			return &v, nil
		}

		for _, alias := range v.Aliases {
			if strings.EqualFold(alias, id) {
				return &v, nil
			}
		}
	}

	return nil, fmt.Errorf("unknown system: %s", id)
}

func AllSystems() []System {
	systems := make([]System, 0, len(Systems))

	keys := AlphaMapKeys(Systems)
	for _, k := range keys {
		systems = append(systems, Systems[k])
	}

	return systems
}

// Consoles
const (
	System3DO               = "3DO"
	System3DS               = "3DS"
	SystemAdventureVision   = "AdventureVision"
	SystemArcadia           = "Arcadia"
	SystemAstrocade         = "Astrocade"
	SystemAmigaCD32         = "AmigaCD32"
	SystemAtari2600         = "Atari2600"
	SystemAtari5200         = "Atari5200"
	SystemAtari7800         = "Atari7800"
	SystemAtariLynx         = "AtariLynx"
	SystemAtariXEGS         = "AtariXEGS"
	SystemCasioPV1000       = "CasioPV1000"
	SystemCDI               = "CDI"
	SystemChannelF          = "ChannelF"
	SystemColecoVision      = "ColecoVision"
	SystemCreatiVision      = "CreatiVision"
	SystemDreamcast         = "Dreamcast"
	SystemFDS               = "FDS"
	SystemGamate            = "Gamate"
	SystemGameboy           = "Gameboy"
	SystemGameboyColor      = "GameboyColor"
	SystemGameboy2P         = "Gameboy2P"
	SystemGameCube          = "GameCube"
	SystemGameGear          = "GameGear"
	SystemGameNWatch        = "GameNWatch"
	SystemGameCom           = "GameCom"
	SystemGBA               = "GBA"
	SystemGBA2P             = "GBA2P"
	SystemGenesis           = "Genesis"
	SystemGenesisMSU        = "GenesisMSU"
	SystemIntellivision     = "Intellivision"
	SystemJaguar            = "Jaguar"
	SystemJaguarCD          = "JaguarCD"
	SystemMasterSystem      = "MasterSystem"
	SystemMegaCD            = "MegaCD"
	SystemMegaDuck          = "MegaDuck"
	SystemNDS               = "NDS"
	SystemNeoGeo            = "NeoGeo"
	SystemNeoGeoCD          = "NeoGeoCD"
	SystemNeoGeoPocket      = "NeoGeoPocket"
	SystemNeoGeoPocketColor = "NeoGeoPocketColor"
	SystemNES               = "NES"
	SystemNESMusic          = "NESMusic"
	SystemNintendo64        = "Nintendo64"
	SystemOdyssey2          = "Odyssey2"
	SystemOuya              = "Ouya"
	SystemPCFX              = "PCFX"
	SystemPocketChallengeV2 = "PocketChallengeV2"
	SystemPokemonMini       = "PokemonMini"
	SystemPSX               = "PSX"
	SystemPS2               = "PS2"
	SystemPS3               = "PS3"
	SystemPS4               = "PS4"
	SystemPS5               = "PS5"
	SystemPSP               = "PSP"
	SystemSega32X           = "Sega32X"
	SystemSeriesXS          = "SeriesXS"
	SystemSG1000            = "SG1000"
	SystemSuperGameboy      = "SuperGameboy"
	SystemSuperVision       = "SuperVision"
	SystemSaturn            = "Saturn"
	SystemSNES              = "SNES"
	SystemSNESMSU1          = "SNESMSU1"
	SystemSGBMSU1           = "SGBMSU1"
	SystemSNESMusic         = "SNESMusic"
	SystemSuperGrafx        = "SuperGrafx"
	SystemSwitch            = "Switch"
	SystemTurboGrafx16      = "TurboGrafx16"
	SystemTurboGrafx16CD    = "TurboGrafx16CD"
	SystemVC4000            = "VC4000"
	SystemVectrex           = "Vectrex"
	SystemVirtualBoy        = "VirtualBoy"
	SystemVita              = "Vita"
	SystemWii               = "Wii"
	SystemWiiU              = "WiiU"
	SystemWonderSwan        = "WonderSwan"
	SystemWonderSwanColor   = "WonderSwanColor"
	SystemXbox              = "Xbox"
	SystemXbox360           = "Xbox360"
	SystemXboxOne           = "XboxOne"
	SystemMultivision       = "Multivision"
	SystemVideopacPlus      = "VideopacPlus"
	SystemNGage             = "NGage"
	SystemSocrates          = "Socrates"
	SystemSuperACan         = "SuperACan"
	SystemSufami            = "Sufami"
	SystemVSmile            = "VSmile"
)

// Computers
const (
	SystemAcornAtom      = "AcornAtom"
	SystemAcornElectron  = "AcornElectron"
	SystemArchimedes     = "Archimedes"
	SystemAliceMC10      = "AliceMC10"
	SystemAmiga          = "Amiga"
	SystemAmiga500       = "Amiga500"
	SystemAmiga1200      = "Amiga1200"
	SystemAmstrad        = "Amstrad"
	SystemAmstradPCW     = "AmstradPCW"
	SystemApogee         = "Apogee"
	SystemAppleI         = "AppleI"
	SystemAppleII        = "AppleII"
	SystemAquarius       = "Aquarius"
	SystemAtari800       = "Atari800"
	SystemBBCMicro       = "BBCMicro"
	SystemBK0011M        = "BK0011M"
	SystemC16            = "C16"
	SystemC64            = "C64"
	SystemCasioPV2000    = "CasioPV2000"
	SystemCoCo2          = "CoCo2"
	SystemDOS            = "DOS"
	SystemEDSAC          = "EDSAC"
	SystemGalaksija      = "Galaksija"
	SystemInteract       = "Interact"
	SystemJupiter        = "Jupiter"
	SystemLaser          = "Laser"
	SystemLynx48         = "Lynx48"
	SystemMacPlus        = "MacPlus"
	SystemMacOS          = "MacOS"
	SystemMSX            = "MSX"
	SystemMSX1           = "MSX1"
	SystemMSX2           = "MSX2"
	SystemMSX2Plus       = "MSX2Plus"
	SystemMultiComp      = "MultiComp"
	SystemOrao           = "Orao"
	SystemOric           = "Oric"
	SystemPC             = "PC"
	SystemPCXT           = "PCXT"
	SystemPDP1           = "PDP1"
	SystemPET2001        = "PET2001"
	SystemPMD85          = "PMD85"
	SystemQL             = "QL"
	SystemRX78           = "RX78"
	SystemSAMCoupe       = "SAMCoupe"
	SystemScummVM        = "ScummVM"
	SystemSordM5         = "SordM5"
	SystemSpecialist     = "Specialist"
	SystemSVI328         = "SVI328"
	SystemTatungEinstein = "TatungEinstein"
	SystemTI994A         = "TI994A"
	SystemTomyTutor      = "TomyTutor"
	SystemTRS80          = "TRS80"
	SystemTSConf         = "TSConf"
	SystemUK101          = "UK101"
	SystemVector06C      = "Vector06C"
	SystemVIC20          = "VIC20"
	SystemWindows        = "Windows"
	SystemX68000         = "X68000"
	SystemZX81           = "ZX81"
	SystemZXSpectrum     = "ZXSpectrum"
	SystemZXNext         = "ZXNext"
	SystemAtariST        = "AtariST"
	SystemColecoAdam     = "ColecoAdam"
	SystemFM7            = "FM7"
	SystemFMTowns        = "FMTowns"
	SystemGamePocket     = "GamePocket"
	SystemGameMaster     = "GameMaster"
	SystemGP32           = "GP32"
	SystemPC88           = "PC88"
	SystemPC98           = "PC98"
	SystemX1             = "X1"
	SystemCommanderX16   = "CommanderX16"
	SystemSpectravideo   = "Spectravideo"
	SystemThomson        = "Thomson"
)

// Other
const (
	SystemAndroid     = "Android"
	SystemArcade      = "Arcade"
	SystemAtomiswave  = "Atomiswave"
	SystemArduboy     = "Arduboy"
	SystemChip8       = "Chip8"
	SystemCPS1        = "CPS1"
	SystemCPS2        = "CPS2"
	SystemCPS3        = "CPS3"
	SystemDAPHNE      = "DAPHNE"
	SystemDICE        = "DICE"
	SystemSinge       = "Singe"
	SystemModel1      = "Model1"
	SystemModel2      = "Model2"
	SystemNamco2X6    = "Namco2X6"
	SystemNamco22     = "Namco22"
	SystemTriforce    = "Triforce"
	SystemLindbergh   = "Lindbergh"
	SystemChihiro     = "Chihiro"
	SystemGaelco      = "Gaelco"
	SystemHikaru      = "Hikaru"
	SystemIOS         = "iOS"
	SystemModel3      = "Model3"
	SystemNAOMI       = "NAOMI"
	SystemNAOMI2      = "NAOMI2"
	SystemPico8       = "Pico8"
	SystemTIC80       = "TIC80"
	SystemVideo       = "Video"
	SystemAudio       = "Audio"
	SystemMovie       = "Movie"
	SystemTV          = "TV"
	SystemTVShow      = "TVShow"
	SystemMusic       = "Music"
	SystemMusicArtist = "MusicArtist"
	SystemMusicAlbum  = "MusicAlbum"
	SystemImage       = "Image"
	SystemJ2ME        = "J2ME"
	SystemGroovy      = "Groovy"
	SystemPlugNPlay   = "PlugNPlay"
)

var Systems = map[string]System{
	// Consoles
	System3DO: {
		ID: System3DO,
	},
	System3DS: {
		ID: System3DS,
	},
	SystemAdventureVision: {
		ID:      SystemAdventureVision,
		Aliases: []string{"AVision"},
	},
	SystemArcadia: {
		ID: SystemArcadia,
	},
	SystemAmigaCD32: {
		ID:        SystemAmigaCD32,
		Fallbacks: []string{SystemAmiga},
	},
	SystemAstrocade: {
		ID: SystemAstrocade,
	},
	SystemAtari2600: {
		ID:        SystemAtari2600,
		Fallbacks: []string{SystemAtari7800},
	},
	SystemAtari5200: {
		ID: SystemAtari5200,
	},
	SystemAtari7800: {
		ID:        SystemAtari7800,
		Fallbacks: []string{SystemAtari2600},
	},
	SystemAtariLynx: {
		ID: SystemAtariLynx,
	},
	SystemAtariXEGS: {
		ID: SystemAtariXEGS,
	},
	SystemCasioPV1000: {
		ID:      SystemCasioPV1000,
		Aliases: []string{"Casio_PV-1000"},
	},
	SystemCDI: {
		ID:      SystemCDI,
		Aliases: []string{"CD-i"},
	},
	SystemChannelF: {
		ID: SystemChannelF,
	},
	SystemColecoVision: {
		ID:        SystemColecoVision,
		Aliases:   []string{"Coleco"},
		Fallbacks: []string{SystemSG1000},
	},
	SystemCreatiVision: {
		ID: SystemCreatiVision,
	},
	SystemDreamcast: {
		ID: SystemDreamcast,
	},
	SystemFDS: {
		ID:      SystemFDS,
		Aliases: []string{"FamicomDiskSystem"},
	},
	SystemGamate: {
		ID: SystemGamate,
	},
	SystemGameboy: {
		ID:      SystemGameboy,
		Aliases: []string{"GB"},
	},
	SystemGameboyColor: {
		ID:        SystemGameboyColor,
		Aliases:   []string{"GBC"},
		Fallbacks: []string{SystemGameboy},
	},
	SystemGameboy2P: {
		// TODO: Split 2P core into GB and GBC?
		ID: SystemGameboy2P,
	},
	SystemGameCube: {
		ID: SystemGameCube,
	},
	SystemGameGear: {
		ID:      SystemGameGear,
		Aliases: []string{"GG"},
	},
	SystemGameNWatch: {
		ID: SystemGameNWatch,
	},
	SystemGameCom: {
		ID: SystemGameCom,
	},
	SystemGBA: {
		ID:      SystemGBA,
		Aliases: []string{"GameboyAdvance"},
	},
	SystemGBA2P: {
		ID: SystemGBA2P,
	},
	SystemGenesis: {
		ID:      SystemGenesis,
		Aliases: []string{"MegaDrive"},
	},
	SystemGenesisMSU: {
		ID:        SystemGenesisMSU,
		Aliases:   []string{"MegaDriveMSU", "MSU-MD"},
		Fallbacks: []string{SystemGenesis},
	},
	SystemIntellivision: {
		ID: SystemIntellivision,
	},
	SystemJaguar: {
		ID: SystemJaguar,
	},
	SystemJaguarCD: {
		ID:        SystemJaguarCD,
		Fallbacks: []string{SystemJaguar},
	},
	SystemMasterSystem: {
		ID:      SystemMasterSystem,
		Aliases: []string{"SMS"},
	},
	SystemMegaCD: {
		ID:        SystemMegaCD,
		Aliases:   []string{"SegaCD"},
		Fallbacks: []string{SystemGenesis},
	},
	SystemMegaDuck: {
		ID: SystemMegaDuck,
	},
	SystemNDS: {
		ID:      SystemNDS,
		Aliases: []string{"NintendoDS"},
	},
	SystemNeoGeo: {
		ID: SystemNeoGeo,
	},
	SystemNeoGeoCD: {
		ID:        SystemNeoGeoCD,
		Fallbacks: []string{SystemNeoGeo},
	},
	SystemNeoGeoPocket: {
		ID: SystemNeoGeoPocket,
	},
	SystemNeoGeoPocketColor: {
		ID:        SystemNeoGeoPocketColor,
		Fallbacks: []string{SystemNeoGeoPocket},
	},
	SystemNES: {
		ID: SystemNES,
	},
	SystemNESMusic: {
		ID:        SystemNESMusic,
		Fallbacks: []string{SystemNES},
	},
	SystemNintendo64: {
		ID:      SystemNintendo64,
		Aliases: []string{"N64"},
	},
	SystemOdyssey2: {
		ID: SystemOdyssey2,
	},
	SystemOuya: {
		ID: SystemOuya,
	},
	SystemPCFX: {
		ID: SystemPCFX,
	},
	SystemPocketChallengeV2: {
		ID: SystemPocketChallengeV2,
	},
	SystemPokemonMini: {
		ID: SystemPokemonMini,
	},
	SystemPSX: {
		ID:      SystemPSX,
		Aliases: []string{"Playstation", "PS1"},
	},
	SystemPS2: {
		ID:      SystemPS2,
		Aliases: []string{"Playstation2"},
	},
	SystemPS3: {
		ID:      SystemPS3,
		Aliases: []string{"Playstation3"},
	},
	SystemPS4: {
		ID:      SystemPS4,
		Aliases: []string{"Playstation4"},
	},
	SystemPS5: {
		ID:      SystemPS5,
		Aliases: []string{"Playstation5"},
	},
	SystemPSP: {
		ID:      SystemPSP,
		Aliases: []string{"PlaystationPortable"},
	},
	SystemSega32X: {
		ID:      SystemSega32X,
		Aliases: []string{"S32X", "32X"},
	},
	SystemSeriesXS: {
		ID:      SystemSeriesXS,
		Aliases: []string{"SeriesX", "SeriesS"},
	},
	SystemSG1000: {
		ID:        SystemSG1000,
		Fallbacks: []string{SystemColecoVision},
	},
	SystemSuperGameboy: {
		ID:        SystemSuperGameboy,
		Aliases:   []string{"SGB"},
		Fallbacks: []string{SystemGameboy},
	},
	SystemSuperVision: {
		ID: SystemSuperVision,
	},
	SystemSaturn: {
		ID: SystemSaturn,
	},
	SystemSNES: {
		ID:      SystemSNES,
		Aliases: []string{"SuperNintendo"},
	},
	SystemSNESMSU1: {
		ID:        SystemSNESMSU1,
		Aliases:   []string{"MSU1", "MSU-1"},
		Fallbacks: []string{SystemSNES},
	},
	SystemSGBMSU1: {
		ID:        SystemSGBMSU1,
		Fallbacks: []string{SystemSuperGameboy},
	},
	SystemSNESMusic: {
		ID:        SystemSNESMusic,
		Fallbacks: []string{SystemSNES},
	},
	SystemSuperGrafx: {
		ID:        SystemSuperGrafx,
		Fallbacks: []string{SystemTurboGrafx16},
	},
	SystemSwitch: {
		ID:      SystemSwitch,
		Aliases: []string{"NintendoSwitch"},
	},
	SystemTurboGrafx16: {
		ID:        SystemTurboGrafx16,
		Aliases:   []string{"TGFX16", "PCEngine"},
		Fallbacks: []string{SystemSuperGrafx},
	},
	SystemTurboGrafx16CD: {
		ID:        SystemTurboGrafx16CD,
		Aliases:   []string{"TGFX16-CD", "PCEngineCD"},
		Fallbacks: []string{SystemTurboGrafx16},
	},
	SystemVC4000: {
		ID: SystemVC4000,
	},
	SystemVectrex: {
		ID: SystemVectrex,
	},
	SystemVirtualBoy: {
		ID: SystemVirtualBoy,
	},
	SystemVita: {
		ID:      SystemVita,
		Aliases: []string{"PSVita"},
	},
	SystemWii: {
		ID:      SystemWii,
		Aliases: []string{"NintendoWii"},
	},
	SystemWiiU: {
		ID:      SystemWiiU,
		Aliases: []string{"NintendoWiiU"},
	},
	SystemWonderSwan: {
		ID: SystemWonderSwan,
	},
	SystemWonderSwanColor: {
		ID:        SystemWonderSwanColor,
		Fallbacks: []string{SystemWonderSwan},
	},
	SystemXbox: {
		ID: SystemXbox,
	},
	SystemXbox360: {
		ID: SystemXbox360,
	},
	SystemXboxOne: {
		ID: SystemXboxOne,
	},
	SystemMultivision: {
		ID: SystemMultivision,
	},
	SystemVideopacPlus: {
		ID: SystemVideopacPlus,
	},
	SystemNGage: {
		ID:      SystemNGage,
		Aliases: []string{"N-Gage"},
	},
	SystemSocrates: {
		ID: SystemSocrates,
	},
	SystemSuperACan: {
		ID: SystemSuperACan,
	},
	SystemSufami: {
		ID: SystemSufami,
	},
	SystemVSmile: {
		ID: SystemVSmile,
	},
	// Computers
	SystemAcornAtom: {
		ID: SystemAcornAtom,
	},
	SystemAcornElectron: {
		ID: SystemAcornElectron,
	},
	SystemArchimedes: {
		ID: SystemArchimedes,
	},
	SystemAliceMC10: {
		ID: SystemAliceMC10,
	},
	SystemAmiga: {
		ID:        SystemAmiga,
		Aliases:   []string{"Minimig"},
		Fallbacks: []string{SystemAmiga500, SystemAmiga1200},
	},
	SystemAmiga500: {
		ID:        SystemAmiga500,
		Aliases:   []string{"A500"},
		Fallbacks: []string{SystemAmiga},
	},
	SystemAmiga1200: {
		ID:        SystemAmiga1200,
		Aliases:   []string{"A1200"},
		Fallbacks: []string{SystemAmiga},
	},
	SystemAmstrad: {
		ID: SystemAmstrad,
	},
	SystemAmstradPCW: {
		ID:      SystemAmstradPCW,
		Aliases: []string{"Amstrad-PCW"},
	},
	SystemDOS: {
		ID:        SystemDOS,
		Aliases:   []string{"ao486", "MS-DOS"},
		Fallbacks: []string{SystemPC},
	},
	SystemApogee: {
		ID: SystemApogee,
	},
	SystemAppleI: {
		ID:      SystemAppleI,
		Aliases: []string{"Apple-I"},
	},
	SystemAppleII: {
		ID:      SystemAppleII,
		Aliases: []string{"Apple-II"},
	},
	SystemAquarius: {
		ID: SystemAquarius,
	},
	SystemAtari800: {
		ID: SystemAtari800,
	},
	SystemBBCMicro: {
		ID: SystemBBCMicro,
	},
	SystemBK0011M: {
		ID: SystemBK0011M,
	},
	SystemC16: {
		ID: SystemC16,
	},
	SystemC64: {
		ID: SystemC64,
	},
	SystemCasioPV2000: {
		ID:      SystemCasioPV2000,
		Aliases: []string{"Casio_PV-2000"},
	},
	SystemCoCo2: {
		ID: SystemCoCo2,
	},
	SystemEDSAC: {
		ID: SystemEDSAC,
	},
	SystemGalaksija: {
		ID: SystemGalaksija,
	},
	SystemInteract: {
		ID: SystemInteract,
	},
	SystemJupiter: {
		ID: SystemJupiter,
	},
	SystemLaser: {
		ID:      SystemLaser,
		Aliases: []string{"Laser310"},
	},
	SystemLynx48: {
		ID: SystemLynx48,
	},
	SystemMacPlus: {
		ID: SystemMacPlus,
	},
	SystemMacOS: {
		ID: SystemMacOS,
	},
	SystemMSX: {
		ID:        SystemMSX,
		Fallbacks: []string{SystemMSX1, SystemMSX2},
	},
	SystemMSX1: {
		ID:        SystemMSX1,
		Fallbacks: []string{SystemMSX},
	},
	SystemMSX2: {
		ID:        SystemMSX2,
		Fallbacks: []string{SystemMSX},
	},
	SystemMSX2Plus: {
		ID:        SystemMSX2Plus,
		Fallbacks: []string{SystemMSX2, SystemMSX},
	},
	SystemMultiComp: {
		ID: SystemMultiComp,
	},
	SystemOrao: {
		ID: SystemOrao,
	},
	SystemOric: {
		ID: SystemOric,
	},
	SystemPC: {
		ID:        SystemPC,
		Fallbacks: []string{SystemDOS, SystemWindows},
	},
	SystemPCXT: {
		ID: SystemPCXT,
	},
	SystemPDP1: {
		ID: SystemPDP1,
	},
	SystemPET2001: {
		ID: SystemPET2001,
	},
	SystemPMD85: {
		ID: SystemPMD85,
	},
	SystemQL: {
		ID: SystemQL,
	},
	SystemRX78: {
		ID: SystemRX78,
	},
	SystemSAMCoupe: {
		ID: SystemSAMCoupe,
	},
	SystemScummVM: {
		ID: SystemScummVM,
	},
	SystemSordM5: {
		ID:      SystemSordM5,
		Aliases: []string{"Sord M5"},
	},
	SystemSpecialist: {
		ID:      SystemSpecialist,
		Aliases: []string{"SPMX"},
	},
	SystemSVI328: {
		ID: SystemSVI328,
	},
	SystemTatungEinstein: {
		ID: SystemTatungEinstein,
	},
	SystemTI994A: {
		ID:      SystemTI994A,
		Aliases: []string{"TI-99_4A"},
	},
	SystemTomyTutor: {
		ID: SystemTomyTutor,
	},
	SystemTRS80: {
		ID: SystemTRS80,
	},
	SystemTSConf: {
		ID: SystemTSConf,
	},
	SystemUK101: {
		ID: SystemUK101,
	},
	SystemVector06C: {
		ID:      SystemVector06C,
		Aliases: []string{"Vector06"},
	},
	SystemVIC20: {
		ID: SystemVIC20,
	},
	SystemWindows: {
		ID:        SystemWindows,
		Aliases:   []string{"Win32", "Win16"},
		Fallbacks: []string{SystemPC},
	},
	SystemX68000: {
		ID: SystemX68000,
	},
	SystemZX81: {
		ID: SystemZX81,
	},
	SystemZXSpectrum: {
		ID:      SystemZXSpectrum,
		Aliases: []string{"Spectrum"},
	},
	SystemZXNext: {
		ID: SystemZXNext,
	},
	// Other
	SystemAndroid: {
		ID: SystemAndroid,
	},
	SystemArcade: {
		ID:      SystemArcade,
		Aliases: []string{"MAME"},
	},
	SystemAtomiswave: {
		ID: SystemAtomiswave,
	},
	SystemArduboy: {
		ID: SystemArduboy,
	},
	SystemChip8: {
		ID: SystemChip8,
	},
	SystemDAPHNE: {
		ID:      SystemDAPHNE,
		Aliases: []string{"LaserDisc"},
	},
	SystemGroovy: {
		ID: SystemGroovy,
	},
	SystemPlugNPlay: {
		ID: SystemPlugNPlay,
	},
	SystemIOS: {
		ID: SystemIOS,
	},
	SystemModel3: {
		ID: SystemModel3,
	},
	SystemNAOMI: {
		ID: SystemNAOMI,
	},
	SystemNAOMI2: {
		ID: SystemNAOMI2,
	},
	SystemVideo: {
		ID: SystemVideo,
	},
	SystemAudio: {
		ID: SystemAudio,
	},
	SystemMovie: {
		ID: SystemMovie,
	},
	SystemTV: {
		ID: SystemTV,
	},
	SystemTVShow: {
		ID: SystemTVShow,
	},
	SystemMusic: {
		ID: SystemMusic,
	},
	SystemMusicArtist: {
		ID: SystemMusicArtist,
	},
	SystemMusicAlbum: {
		ID: SystemMusicAlbum,
	},
	SystemImage: {
		ID: SystemImage,
	},
	SystemJ2ME: {
		ID: SystemJ2ME,
	},
	SystemCPS1: {
		ID: SystemCPS1,
	},
	SystemCPS2: {
		ID: SystemCPS2,
	},
	SystemCPS3: {
		ID: SystemCPS3,
	},
	SystemAtariST: {
		ID: SystemAtariST,
	},
	SystemColecoAdam: {
		ID: SystemColecoAdam,
	},
	SystemFM7: {
		ID: SystemFM7,
	},
	SystemFMTowns: {
		ID: SystemFMTowns,
	},
	SystemGamePocket: {
		ID: SystemGamePocket,
	},
	SystemGameMaster: {
		ID: SystemGameMaster,
	},
	SystemGP32: {
		ID: SystemGP32,
	},
	SystemPico8: {
		ID: SystemPico8,
	},
	SystemTIC80: {
		ID: SystemTIC80,
	},
	SystemPC88: {
		ID: SystemPC88,
	},
	SystemPC98: {
		ID: SystemPC98,
	},
	SystemX1: {
		ID: SystemX1,
	},
	SystemCommanderX16: {
		ID: SystemCommanderX16,
	},
	SystemSpectravideo: {
		ID: SystemSpectravideo,
	},
	SystemThomson: {
		ID: SystemThomson,
	},
	SystemDICE: {
		ID: SystemDICE,
	},
	SystemSinge: {
		ID: SystemSinge,
	},
	SystemModel1: {
		ID: SystemModel1,
	},
	SystemModel2: {
		ID: SystemModel2,
	},
	SystemNamco2X6: {
		ID: SystemNamco2X6,
	},
	SystemNamco22: {
		ID: SystemNamco22,
	},
	SystemTriforce: {
		ID: SystemTriforce,
	},
	SystemLindbergh: {
		ID: SystemLindbergh,
	},
	SystemChihiro: {
		ID: SystemChihiro,
	},
	SystemGaelco: {
		ID: SystemGaelco,
	},
	SystemHikaru: {
		ID: SystemHikaru,
	},
}
