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
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
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
	// Pre-defined slug variations for natural language matching (manufacturer prefixes, regional names, etc.)
	Slugs []string
}

// Lazy initialization for system lookup map
var (
	lookupMap     map[string]*System
	lookupMapOnce sync.Once
	errLookupMap  error
)

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

// buildLookupMap initializes the system lookup map with all possible lookup keys.
// This includes: lowercase IDs, lowercase aliases, slugified IDs, slugified aliases,
// and custom slugs. It detects and reports collisions at initialization time.
func buildLookupMap() error {
	lookupMapOnce.Do(func() {
		lookupMap = make(map[string]*System)
		keyOwner := make(map[string]string) // Track which system owns each key for collision detection

		addKey := func(key, systemID, sourceType string) {
			if key == "" {
				return // Skip empty keys
			}

			if ownerID, exists := keyOwner[key]; exists {
				if ownerID != systemID {
					// Collision detected between different systems
					if errLookupMap == nil {
						errLookupMap = fmt.Errorf(
							"system lookup collision: key %q (from %s of %s) is already owned by system %q",
							key, sourceType, systemID, ownerID,
						)
					}
				}
				return // Redundant key within the same system is acceptable
			}

			keyOwner[key] = systemID
			// Get pointer to the system in the Systems map
			sys := Systems[systemID]
			lookupMap[key] = &sys
		}

		// Process all systems in a deterministic order
		for _, id := range AlphaMapKeys(Systems) {
			system := Systems[id]

			// 1. Add lowercase ID
			addKey(strings.ToLower(system.ID), system.ID, "ID")

			// 2. Add lowercase aliases
			for _, alias := range system.Aliases {
				addKey(strings.ToLower(alias), system.ID, "Alias")
			}

			// 3. Add slugified ID (auto-derived)
			addKey(slugs.SlugifyString(system.ID), system.ID, "Slug(ID)")

			// 4. Add slugified aliases (auto-derived)
			for _, alias := range system.Aliases {
				addKey(slugs.SlugifyString(alias), system.ID, "Slug(Alias)")
			}

			// 5. Add custom slugs
			for _, slug := range system.Slugs {
				addKey(slug, system.ID, "CustomSlug")
			}
		}
	})

	return errLookupMap
}

// LookupSystem case-insensitively looks up system ID definition including aliases and slugs.
// It uses a two-step strategy:
//  1. Fast path: Check lowercase input against the lookup map (exact ID/alias matches)
//  2. Natural language path: Slugify input and check against the map (handles manufacturer
//     prefixes, regional names, etc.)
func LookupSystem(id string) (*System, error) {
	// Initialize the lookup map if needed
	if err := buildLookupMap(); err != nil {
		return nil, fmt.Errorf("failed to build system lookup map: %w", err)
	}

	// Step 1: Try case-insensitive match (fast path for exact/alias matches)
	lowerID := strings.ToLower(id)
	if system, ok := lookupMap[lowerID]; ok {
		return system, nil
	}

	// Step 2: Try slugified match (natural language path)
	slugifiedID := slugs.SlugifyString(id)
	if slugifiedID != lowerID {
		// Only check if slugification changed the string
		if system, ok := lookupMap[slugifiedID]; ok {
			return system, nil
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
	SystemTVEpisode   = "TVEpisode"
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
		ID:    System3DO,
		Slugs: []string{"panasonic3do"},
	},
	System3DS: {
		ID:    System3DS,
		Slugs: []string{"nintendo3ds", "n3ds", "3dsxl", "2ds", "new3ds", "new2ds"},
	},
	SystemAdventureVision: {
		ID:      SystemAdventureVision,
		Aliases: []string{"AVision"},
		Slugs:   []string{"entexadventurevision"},
	},
	SystemArcadia: {
		ID:    SystemArcadia,
		Slugs: []string{"arcadia2001", "emersonarcadia"},
	},
	SystemAmigaCD32: {
		ID:        SystemAmigaCD32,
		Slugs:     []string{"cd32", "commodoreamigacd32"},
		Fallbacks: []string{SystemAmiga},
	},
	SystemAstrocade: {
		ID:    SystemAstrocade,
		Slugs: []string{"ballyastrocade", "bally"},
	},
	SystemAtari2600: {
		ID:        SystemAtari2600,
		Slugs:     []string{"vcs", "atari2600vcs", "atarivcs"},
		Fallbacks: []string{SystemAtari7800},
	},
	SystemAtari5200: {
		ID: SystemAtari5200,
	},
	SystemAtari7800: {
		ID:        SystemAtari7800,
		Slugs:     []string{"prosystem"},
		Fallbacks: []string{SystemAtari2600},
	},
	SystemAtariLynx: {
		ID: SystemAtariLynx,
	},
	SystemAtariXEGS: {
		ID:    SystemAtariXEGS,
		Slugs: []string{"xegs", "atarixe"},
	},
	SystemCasioPV1000: {
		ID:      SystemCasioPV1000,
		Aliases: []string{"Casio_PV-1000"},
	},
	SystemCDI: {
		ID:      SystemCDI,
		Aliases: []string{"CD-i"},
		Slugs:   []string{"philipscdi"},
	},
	SystemChannelF: {
		ID:    SystemChannelF,
		Slugs: []string{"fairchildchannelf"},
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
		ID:    SystemDreamcast,
		Slugs: []string{"segadreamcast", "dc"},
	},
	SystemFDS: {
		ID:      SystemFDS,
		Aliases: []string{"FamicomDiskSystem"},
		Slugs:   []string{"nintendofds", "famicomdisk"},
	},
	SystemGamate: {
		ID:    SystemGamate,
		Slugs: []string{"bitcorpgamate"},
	},
	SystemGameboy: {
		ID:      SystemGameboy,
		Aliases: []string{"GB"},
		Slugs: []string{
			"nintendogameboy", "dmg", "pocketgameboy", "gameboyoriginal",
			"gameboypocket", "gameboylight", "gbpocket", "gblight",
		},
	},
	SystemGameboyColor: {
		ID:        SystemGameboyColor,
		Aliases:   []string{"GBC"},
		Slugs:     []string{"nintendogbc", "gameboycolour", "cgb"},
		Fallbacks: []string{SystemGameboy},
	},
	SystemGameboy2P: {
		// TODO: Split 2P core into GB and GBC?
		ID: SystemGameboy2P,
	},
	SystemGameCube: {
		ID:    SystemGameCube,
		Slugs: []string{"nintendogamecube", "gc", "ngc", "gcn", "dolphin"},
	},
	SystemGameGear: {
		ID:      SystemGameGear,
		Aliases: []string{"GG"},
		Slugs:   []string{"segagamegear"},
	},
	SystemGameNWatch: {
		ID:    SystemGameNWatch,
		Slugs: []string{"gameandwatch", "gnw", "nintendogamewatch"},
	},
	SystemGameCom: {
		ID:    SystemGameCom,
		Slugs: []string{"tigerelectronicsgamecom", "tigergamecom"},
	},
	SystemGBA: {
		ID:      SystemGBA,
		Aliases: []string{"GameboyAdvance"},
		Slugs:   []string{"nintendogba", "advancesp", "gbasp", "gbamicro"},
	},
	SystemGBA2P: {
		ID: SystemGBA2P,
	},
	SystemGenesis: {
		ID:      SystemGenesis,
		Aliases: []string{"MegaDrive"},
		Slugs:   []string{"segagenesis", "segamegadrive", "md", "gen", "smd"},
	},
	SystemGenesisMSU: {
		ID:        SystemGenesisMSU,
		Aliases:   []string{"MegaDriveMSU", "MSU-MD"},
		Fallbacks: []string{SystemGenesis},
	},
	SystemIntellivision: {
		ID:    SystemIntellivision,
		Slugs: []string{"mattelintellivision", "intv"},
	},
	SystemJaguar: {
		ID:    SystemJaguar,
		Slugs: []string{"atarijaguar", "jag"},
	},
	SystemJaguarCD: {
		ID:        SystemJaguarCD,
		Slugs:     []string{"atarijaguarcd", "jagcd"},
		Fallbacks: []string{SystemJaguar},
	},
	SystemMasterSystem: {
		ID:      SystemMasterSystem,
		Aliases: []string{"SMS"},
		Slugs:   []string{"segamastersystem", "mk3", "markiii", "segamark3"},
	},
	SystemMegaCD: {
		ID:        SystemMegaCD,
		Aliases:   []string{"SegaCD"},
		Slugs:     []string{"mcd", "scd", "megadrivecd", "genesiscd"},
		Fallbacks: []string{SystemGenesis},
	},
	SystemMegaDuck: {
		ID:    SystemMegaDuck,
		Slugs: []string{"creatronic", "cougar"},
	},
	SystemNDS: {
		ID:      SystemNDS,
		Aliases: []string{"NintendoDS"},
		Slugs:   []string{"ndsl", "ndsi", "dsi", "dslite", "dsixl"},
	},
	SystemNeoGeo: {
		ID:    SystemNeoGeo,
		Slugs: []string{"snk", "snkneogeo", "aes", "mvs", "neogeoaes", "neogeomvs"},
	},
	SystemNeoGeoCD: {
		ID:        SystemNeoGeoCD,
		Slugs:     []string{"snkneocd", "ngcd", "neocd", "neogeocdz"},
		Fallbacks: []string{SystemNeoGeo},
	},
	SystemNeoGeoPocket: {
		ID:    SystemNeoGeoPocket,
		Slugs: []string{"ngp", "snkngp", "neopocket", "neogeop"},
	},
	SystemNeoGeoPocketColor: {
		ID:        SystemNeoGeoPocketColor,
		Slugs:     []string{"ngpc", "snkngpc", "neopocketcolor", "neogeopocketcolour"},
		Fallbacks: []string{SystemNeoGeoPocket},
	},
	SystemNES: {
		ID: SystemNES,
		Slugs: []string{
			"nintendoentertainmentsystem", "famicom", "fc", "familycomputer",
			"nintendinho", "fami", "nintendoaes",
		},
	},
	SystemNESMusic: {
		ID:        SystemNESMusic,
		Fallbacks: []string{SystemNES},
	},
	SystemNintendo64: {
		ID:      SystemNintendo64,
		Aliases: []string{"N64"},
		Slugs:   []string{"nintendon64", "ultra64", "nintendo64dd", "n64dd"},
	},
	SystemOdyssey2: {
		ID:    SystemOdyssey2,
		Slugs: []string{"odyssey", "magnavoxodyssey2", "videopac", "o2"},
	},
	SystemOuya: {
		ID:    SystemOuya,
		Slugs: []string{"ouyaconsole"},
	},
	SystemPCFX: {
		ID:    SystemPCFX,
		Slugs: []string{"necpcfx"},
	},
	SystemPocketChallengeV2: {
		ID:    SystemPocketChallengeV2,
		Slugs: []string{"pcv2", "pocketchallenge"},
	},
	SystemPokemonMini: {
		ID:    SystemPokemonMini,
		Slugs: []string{"pokemini", "nintendopokemonmini"},
	},
	SystemPSX: {
		ID:      SystemPSX,
		Aliases: []string{"Playstation", "PS1"},
		Slugs:   []string{"sonyplaystation", "playstation1", "psone", "playstationone"},
	},
	SystemPS2: {
		ID:      SystemPS2,
		Aliases: []string{"Playstation2"},
		Slugs:   []string{"sonyps2", "playstationii", "psii"},
	},
	SystemPS3: {
		ID:      SystemPS3,
		Aliases: []string{"Playstation3"},
		Slugs:   []string{"sonyps3", "playstationiii", "psiii"},
	},
	SystemPS4: {
		ID:      SystemPS4,
		Aliases: []string{"Playstation4"},
		Slugs:   []string{"sonyps4", "ps4pro", "playstationiv", "psiv", "ps4slim"},
	},
	SystemPS5: {
		ID:      SystemPS5,
		Aliases: []string{"Playstation5"},
		Slugs:   []string{"sonyps5", "playstationv", "psv", "ps5digital"},
	},
	SystemPSP: {
		ID:      SystemPSP,
		Aliases: []string{"PlaystationPortable"},
		Slugs:   []string{"sonypsp", "pspgo", "pspp", "psp1000", "psp2000", "psp3000"},
	},
	SystemSega32X: {
		ID:      SystemSega32X,
		Aliases: []string{"S32X", "32X"},
		Slugs:   []string{"genesismars", "superx32", "megadrive32x", "genesis32x", "mars"},
	},
	SystemSeriesXS: {
		ID:      SystemSeriesXS,
		Aliases: []string{"SeriesX", "SeriesS"},
		Slugs:   []string{"xboxseriesx", "xboxseriess", "xsx", "xss"},
	},
	SystemSG1000: {
		ID:        SystemSG1000,
		Slugs:     []string{"segasg1000"},
		Fallbacks: []string{SystemColecoVision},
	},
	SystemSuperGameboy: {
		ID:        SystemSuperGameboy,
		Aliases:   []string{"SGB"},
		Fallbacks: []string{SystemGameboy},
	},
	SystemSuperVision: {
		ID:    SystemSuperVision,
		Slugs: []string{"watara"},
	},
	SystemSaturn: {
		ID:    SystemSaturn,
		Slugs: []string{"segasaturn", "sat", "hisaturn", "vsaturn"},
	},
	SystemSNES: {
		ID:      SystemSNES,
		Aliases: []string{"SuperNintendo"},
		Slugs:   []string{"superfamicom", "sfc", "supercomboy", "supernes", "superfam", "snesclassic"},
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
		Slugs:     []string{"sgx", "necsupergrafx"},
		Fallbacks: []string{SystemTurboGrafx16},
	},
	SystemSwitch: {
		ID:      SystemSwitch,
		Aliases: []string{"NintendoSwitch"},
		Slugs:   []string{"ns", "switchlite", "switcholed", "nx"},
	},
	SystemTurboGrafx16: {
		ID:        SystemTurboGrafx16,
		Aliases:   []string{"TGFX16", "PCEngine"},
		Slugs:     []string{"pce", "tg16", "necpcengine", "necturbografx16", "turbografx", "pcenginehucard", "tgx"},
		Fallbacks: []string{SystemSuperGrafx},
	},
	SystemTurboGrafx16CD: {
		ID:        SystemTurboGrafx16CD,
		Aliases:   []string{"TGFX16-CD", "PCEngineCD"},
		Slugs:     []string{"turbografxcd", "tg16cd", "pcecd", "cdrom2", "supercd"},
		Fallbacks: []string{SystemTurboGrafx16},
	},
	SystemVC4000: {
		ID:    SystemVC4000,
		Slugs: []string{"interton", "intertonvc4000"},
	},
	SystemVectrex: {
		ID:    SystemVectrex,
		Slugs: []string{"smithengineeringvectrex", "gcevectrex"},
	},
	SystemVirtualBoy: {
		ID:    SystemVirtualBoy,
		Slugs: []string{"nintendovirtualboy", "vb", "virtualboy3d"},
	},
	SystemVita: {
		ID:      SystemVita,
		Aliases: []string{"PSVita"},
		Slugs:   []string{"playstationvita", "psvitaslim", "psvita1000", "psvita2000", "pstv", "playstationtv"},
	},
	SystemWii: {
		ID:      SystemWii,
		Aliases: []string{"NintendoWii"},
		Slugs:   []string{"revolution"},
	},
	SystemWiiU: {
		ID:      SystemWiiU,
		Aliases: []string{"NintendoWiiU"},
	},
	SystemWonderSwan: {
		ID:    SystemWonderSwan,
		Slugs: []string{"ws", "bandaiwonderswan"},
	},
	SystemWonderSwanColor: {
		ID:        SystemWonderSwanColor,
		Slugs:     []string{"wsc", "bandaiwsc", "wonderswancolour"},
		Fallbacks: []string{SystemWonderSwan},
	},
	SystemXbox: {
		ID:    SystemXbox,
		Slugs: []string{"microsoftxbox", "xb", "xboxoriginal"},
	},
	SystemXbox360: {
		ID:    SystemXbox360,
		Slugs: []string{"microsoftxbox360", "x360", "360"},
	},
	SystemXboxOne: {
		ID:    SystemXboxOne,
		Slugs: []string{"microsoftxboxone", "xbone", "xb1", "xone"},
	},
	SystemMultivision: {
		ID:    SystemMultivision,
		Slugs: []string{"vtech", "o2multivision"},
	},
	SystemVideopacPlus: {
		ID:    SystemVideopacPlus,
		Slugs: []string{"odyssey3", "g7400"},
	},
	SystemNGage: {
		ID:      SystemNGage,
		Aliases: []string{"N-Gage"},
		Slugs:   []string{"nokiangage"},
	},
	SystemSocrates: {
		ID:    SystemSocrates,
		Slugs: []string{"vtechsocrates", "socratesedusystem"},
	},
	SystemSuperACan: {
		ID:    SystemSuperACan,
		Slugs: []string{"acan", "funtech"},
	},
	SystemSufami: {
		ID:    SystemSufami,
		Slugs: []string{"sufamiturbo", "nintendosufami"},
	},
	SystemVSmile: {
		ID:    SystemVSmile,
		Slugs: []string{"vtechvsmile"},
	},
	// Computers
	SystemAcornAtom: {
		ID: SystemAcornAtom,
	},
	SystemAcornElectron: {
		ID: SystemAcornElectron,
	},
	SystemArchimedes: {
		ID:    SystemArchimedes,
		Slugs: []string{"acornarchimedes", "riscos"},
	},
	SystemAliceMC10: {
		ID:    SystemAliceMC10,
		Slugs: []string{"mc10"},
	},
	SystemAmiga: {
		ID:        SystemAmiga,
		Aliases:   []string{"Minimig"},
		Slugs:     []string{"commodoreamiga"},
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
		ID:    SystemAmstrad,
		Slugs: []string{"amstradcpc", "cpc", "amstradcpc464"},
	},
	SystemAmstradPCW: {
		ID:      SystemAmstradPCW,
		Aliases: []string{"Amstrad-PCW"},
	},
	SystemDOS: {
		ID:        SystemDOS,
		Aliases:   []string{"ao486", "MS-DOS"},
		Slugs:     []string{"ibmpc", "pcdos", "microsoftdos", "ibmdos", "dosbox"},
		Fallbacks: []string{SystemPC},
	},
	SystemApogee: {
		ID:    SystemApogee,
		Slugs: []string{"apogeebk01", "bk01"},
	},
	SystemAppleI: {
		ID:      SystemAppleI,
		Aliases: []string{"Apple-I"},
	},
	SystemAppleII: {
		ID:      SystemAppleII,
		Aliases: []string{"Apple-II"},
		Slugs:   []string{"appleiiplus", "appleiie", "appleiic"},
	},
	SystemAquarius: {
		ID:    SystemAquarius,
		Slugs: []string{"mattelaquarius"},
	},
	SystemAtari800: {
		ID: SystemAtari800,
		Slugs: []string{
			"atari400", "atari800xl", "atari130xe",
			"atari8bit", "atari600xl", "atarihomecomputer",
		},
	},
	SystemBBCMicro: {
		ID:    SystemBBCMicro,
		Slugs: []string{"acornbbc", "bbcmaster"},
	},
	SystemBK0011M: {
		ID:    SystemBK0011M,
		Slugs: []string{"bk11", "bk0010", "electronika"},
	},
	SystemC16: {
		ID:    SystemC16,
		Slugs: []string{"commodorecommodore16", "plus4"},
	},
	SystemC64: {
		ID:    SystemC64,
		Slugs: []string{"commodore64", "cbm64", "vic64", "vc64"},
	},
	SystemCasioPV2000: {
		ID:      SystemCasioPV2000,
		Aliases: []string{"Casio_PV-2000"},
	},
	SystemCoCo2: {
		ID:    SystemCoCo2,
		Slugs: []string{"trs80coco", "colorcomputer", "coco"},
	},
	SystemEDSAC: {
		ID:    SystemEDSAC,
		Slugs: []string{"cambridgeedsac"},
	},
	SystemGalaksija: {
		ID: SystemGalaksija,
	},
	SystemInteract: {
		ID:    SystemInteract,
		Slugs: []string{"interactmodel1", "victor"},
	},
	SystemJupiter: {
		ID:    SystemJupiter,
		Slugs: []string{"jupiterace", "cantab"},
	},
	SystemLaser: {
		ID:      SystemLaser,
		Aliases: []string{"Laser310"},
	},
	SystemLynx48: {
		ID:    SystemLynx48,
		Slugs: []string{"camberlynx", "camber"},
	},
	SystemMacPlus: {
		ID:    SystemMacPlus,
		Slugs: []string{"macintosh", "applemacintosh"},
	},
	SystemMacOS: {
		ID:    SystemMacOS,
		Slugs: []string{"applemac", "osx"},
	},
	SystemMSX: {
		ID:        SystemMSX,
		Slugs:     []string{"microsoftmsx"},
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
		ID:    SystemMultiComp,
		Slugs: []string{"multicomputer"},
	},
	SystemOrao: {
		ID:    SystemOrao,
		Slugs: []string{"oraocomputer"},
	},
	SystemOric: {
		ID:    SystemOric,
		Slugs: []string{"oric1", "oricatmos", "tangerine"},
	},
	SystemPC: {
		ID:        SystemPC,
		Fallbacks: []string{SystemDOS, SystemWindows},
	},
	SystemPCXT: {
		ID:    SystemPCXT,
		Slugs: []string{"ibmpcxt"},
	},
	SystemPDP1: {
		ID:    SystemPDP1,
		Slugs: []string{"decpdp1"},
	},
	SystemPET2001: {
		ID:    SystemPET2001,
		Slugs: []string{"commodorepet", "cbm"},
	},
	SystemPMD85: {
		ID:    SystemPMD85,
		Slugs: []string{"pmd", "tesla"},
	},
	SystemQL: {
		ID:    SystemQL,
		Slugs: []string{"sinclairql"},
	},
	SystemRX78: {
		ID:    SystemRX78,
		Slugs: []string{"gundamrx78", "bandairx78"},
	},
	SystemSAMCoupe: {
		ID: SystemSAMCoupe,
	},
	SystemScummVM: {
		ID:    SystemScummVM,
		Slugs: []string{"scumm"},
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
		ID:    SystemSVI328,
		Slugs: []string{"svi", "spectravideo328"},
	},
	SystemTatungEinstein: {
		ID:    SystemTatungEinstein,
		Slugs: []string{"einstein"},
	},
	SystemTI994A: {
		ID:      SystemTI994A,
		Aliases: []string{"TI-99_4A"},
	},
	SystemTomyTutor: {
		ID:    SystemTomyTutor,
		Slugs: []string{"tomy", "pyuutajr"},
	},
	SystemTRS80: {
		ID:    SystemTRS80,
		Slugs: []string{"tandy", "radioshacktrs80"},
	},
	SystemTSConf: {
		ID:    SystemTSConf,
		Slugs: []string{"tsc", "tslab"},
	},
	SystemUK101: {
		ID:    SystemUK101,
		Slugs: []string{"compukit", "ohio"},
	},
	SystemVector06C: {
		ID:      SystemVector06C,
		Aliases: []string{"Vector06"},
	},
	SystemVIC20: {
		ID:    SystemVIC20,
		Slugs: []string{"commodorevic20", "vc20"},
	},
	SystemWindows: {
		ID:        SystemWindows,
		Aliases:   []string{"Win32", "Win16"},
		Slugs:     []string{"microsoftwindows", "win", "win95", "win98", "winxp", "win7", "win10", "win11"},
		Fallbacks: []string{SystemPC},
	},
	SystemX68000: {
		ID:    SystemX68000,
		Slugs: []string{"sharpx68000", "x68k"},
	},
	SystemZX81: {
		ID:    SystemZX81,
		Slugs: []string{"sinclairzx81", "timex1000"},
	},
	SystemZXSpectrum: {
		ID:      SystemZXSpectrum,
		Aliases: []string{"Spectrum"},
		Slugs: []string{
			"sinclairspectrum", "speccy", "zx", "sinclair",
			"spectrum48k", "spectrum128k", "spectrumplus",
		},
	},
	SystemZXNext: {
		ID:    SystemZXNext,
		Slugs: []string{"zxspectrumnext", "spectrumnext"},
	},
	// Other
	SystemAndroid: {
		ID:    SystemAndroid,
		Slugs: []string{"androidgame", "androidgames"},
	},
	SystemArcade: {
		ID:      SystemArcade,
		Aliases: []string{"MAME"},
		Slugs:   []string{"arcademachine", "arcadegame", "arcadegames", "coinop"},
	},
	SystemAtomiswave: {
		ID:    SystemAtomiswave,
		Slugs: []string{"sammy", "segaatomiswave"},
	},
	SystemArduboy: {
		ID:    SystemArduboy,
		Slugs: []string{"arduino", "miniarcade"},
	},
	SystemChip8: {
		ID:    SystemChip8,
		Slugs: []string{"cosmacvip", "superchip"},
	},
	SystemDAPHNE: {
		ID:      SystemDAPHNE,
		Aliases: []string{"LaserDisc"},
		Slugs:   []string{"daphnelaserdisc"},
	},
	SystemGroovy: {
		ID:    SystemGroovy,
		Slugs: []string{"groovymister", "groovymame"},
	},
	SystemPlugNPlay: {
		ID:    SystemPlugNPlay,
		Slugs: []string{"plugandplay", "tvgame", "tvgames"},
	},
	SystemIOS: {
		ID:    SystemIOS,
		Slugs: []string{"iphone", "ipad", "applegame", "applegames"},
	},
	SystemModel3: {
		ID:    SystemModel3,
		Slugs: []string{"segamodel3"},
	},
	SystemNAOMI: {
		ID:    SystemNAOMI,
		Slugs: []string{"seganaomi"},
	},
	SystemNAOMI2: {
		ID:    SystemNAOMI2,
		Slugs: []string{"seganaomi2"},
	},
	SystemVideo: {
		ID:    SystemVideo,
		Slugs: []string{"videos", "videofile"},
	},
	SystemAudio: {
		ID:    SystemAudio,
		Slugs: []string{"audiofile"},
	},
	SystemMovie: {
		ID:    SystemMovie,
		Slugs: []string{"movies", "film", "cinema"},
	},
	SystemTVEpisode: {
		ID:      SystemTVEpisode,
		Aliases: []string{"TV"},
		Slugs:   []string{"television", "tvchannel"},
	},
	SystemTVShow: {
		ID:    SystemTVShow,
		Slugs: []string{"tvshows", "tvseries"},
	},
	SystemMusic: {
		ID:    SystemMusic,
		Slugs: []string{"musicfile", "song", "songs"},
	},
	SystemMusicArtist: {
		ID:    SystemMusicArtist,
		Slugs: []string{"musician", "musicians", "band", "bands"},
	},
	SystemMusicAlbum: {
		ID:    SystemMusicAlbum,
		Slugs: []string{"lp", "album", "albums"},
	},
	SystemImage: {
		ID:    SystemImage,
		Slugs: []string{"picture", "pictures", "photo", "photos", "images"},
	},
	SystemJ2ME: {
		ID:    SystemJ2ME,
		Slugs: []string{"javame", "javamobile", "mobilephone"},
	},
	SystemCPS1: {
		ID:    SystemCPS1,
		Slugs: []string{"cpsystem1", "capcomsystem1", "capcomplay1"},
	},
	SystemCPS2: {
		ID:    SystemCPS2,
		Slugs: []string{"cpsystem2", "capcomsystem2", "capcomplay2"},
	},
	SystemCPS3: {
		ID:    SystemCPS3,
		Slugs: []string{"cpsystem3", "capcomsystem3", "capcomplay3"},
	},
	SystemAtariST: {
		ID:    SystemAtariST,
		Slugs: []string{"atariste", "ataritt", "atarifalcon"},
	},
	SystemColecoAdam: {
		ID:    SystemColecoAdam,
		Slugs: []string{"adam"},
	},
	SystemFM7: {
		ID:    SystemFM7,
		Slugs: []string{"fujitsufm7", "fm77"},
	},
	SystemFMTowns: {
		ID:    SystemFMTowns,
		Slugs: []string{"fujitsufmtowns"},
	},
	SystemGamePocket: {
		ID:    SystemGamePocket,
		Slugs: []string{"gpcomputer"},
	},
	SystemGameMaster: {
		ID:    SystemGameMaster,
		Slugs: []string{"gmcomputer", "hartung"},
	},
	SystemGP32: {
		ID:    SystemGP32,
		Slugs: []string{"gamepark", "gp32handheld"},
	},
	SystemPico8: {
		ID:    SystemPico8,
		Slugs: []string{"lexaloffle"},
	},
	SystemTIC80: {
		ID: SystemTIC80,
	},
	SystemPC88: {
		ID:    SystemPC88,
		Slugs: []string{"necpc88", "pc8801"},
	},
	SystemPC98: {
		ID:    SystemPC98,
		Slugs: []string{"necpc98", "pc9801"},
	},
	SystemX1: {
		ID:    SystemX1,
		Slugs: []string{"sharpx1"},
	},
	SystemCommanderX16: {
		ID:    SystemCommanderX16,
		Slugs: []string{"x16", "cx16"},
	},
	SystemSpectravideo: {
		ID:    SystemSpectravideo,
		Slugs: []string{"sv318"},
	},
	SystemThomson: {
		ID:    SystemThomson,
		Slugs: []string{"thomsonto7", "mo5", "to8"},
	},
	SystemDICE: {
		ID:    SystemDICE,
		Slugs: []string{"segadice"},
	},
	SystemSinge: {
		ID:    SystemSinge,
		Slugs: []string{"singelaserdisc"},
	},
	SystemModel1: {
		ID:    SystemModel1,
		Slugs: []string{"segamodel1"},
	},
	SystemModel2: {
		ID:    SystemModel2,
		Slugs: []string{"segamodel2"},
	},
	SystemNamco2X6: {
		ID:    SystemNamco2X6,
		Slugs: []string{"namcosystem2", "system2"},
	},
	SystemNamco22: {
		ID:    SystemNamco22,
		Slugs: []string{"namcosystem22", "system22"},
	},
	SystemTriforce: {
		ID:    SystemTriforce,
		Slugs: []string{"nintendotriforce", "namcotriforce", "segatriforce"},
	},
	SystemLindbergh: {
		ID:    SystemLindbergh,
		Slugs: []string{"segalindbergh"},
	},
	SystemChihiro: {
		ID:    SystemChihiro,
		Slugs: []string{"segachihiro"},
	},
	SystemGaelco: {
		ID:    SystemGaelco,
		Slugs: []string{"gaelcoarcade"},
	},
	SystemHikaru: {
		ID:    SystemHikaru,
		Slugs: []string{"segahikaru"},
	},
}
