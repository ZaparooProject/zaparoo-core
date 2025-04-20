package systemdefs

import (
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
)

// The Systems list contains all the supported systems such as consoles,
// computers and media types that are indexable by Zaparoo. This is the reference
// list of hardcoded system IDs used throughout Zaparoo. A platform can choose
// not to support any of them.
//
// This list also contains some basic heuristics which, given a file path, can
// be used to attempt to associate a file with a system.

type System struct {
	Id      string
	Aliases []string
}

// GetSystem looks up an exact system definition by ID.
func GetSystem(id string) (*System, error) {
	if system, ok := Systems[id]; ok {
		return &system, nil
	} else {
		return nil, fmt.Errorf("unknown system: %s", id)
	}
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
	var systems []System

	keys := utils.AlphaMapKeys(Systems)
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
)

// Computers
const (
	SystemAcornAtom      = "AcornAtom"
	SystemAcornElectron  = "AcornElectron"
	SystemAliceMC10      = "AliceMC10"
	SystemAmiga          = "Amiga" // TODO: Amiga1200/500/etc.
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
	SystemX68000         = "X68000"
	SystemZX81           = "ZX81"
	SystemZXSpectrum     = "ZXSpectrum"
	SystemZXNext         = "ZXNext"
)

// Other
const (
	SystemAndroid = "Android"
	SystemArcade  = "Arcade"
	SystemArduboy = "Arduboy"
	SystemChip8   = "Chip8"
	SystemIOS     = "iOS"
	SystemVideo   = "Video"
	SystemGroovy  = "Groovy"
)

var Systems = map[string]System{
	// Consoles
	System3DO: {
		Id: System3DO,
	},
	System3DS: {
		Id: System3DS,
	},
	SystemAdventureVision: {
		Id:      SystemAdventureVision,
		Aliases: []string{"AVision"},
	},
	SystemArcadia: {
		Id: SystemArcadia,
	},
	SystemAmigaCD32: {
		Id: SystemAmigaCD32,
	},
	SystemAstrocade: {
		Id: SystemAstrocade,
	},
	SystemAtari2600: {
		Id: SystemAtari2600,
	},
	SystemAtari5200: {
		Id: SystemAtari5200,
	},
	SystemAtari7800: {
		Id: SystemAtari7800,
	},
	SystemAtariLynx: {
		Id: SystemAtariLynx,
	},
	SystemAtariXEGS: {
		Id: SystemAtariXEGS,
	},
	SystemCasioPV1000: {
		Id:      SystemCasioPV1000,
		Aliases: []string{"Casio_PV-1000"},
	},
	SystemCDI: {
		Id:      SystemCDI,
		Aliases: []string{"CD-i"},
	},
	SystemChannelF: {
		Id: SystemChannelF,
	},
	SystemColecoVision: {
		Id:      SystemColecoVision,
		Aliases: []string{"Coleco"},
	},
	SystemCreatiVision: {
		Id: SystemCreatiVision,
	},
	SystemDreamcast: {
		Id: SystemDreamcast,
	},
	SystemFDS: {
		Id:      SystemFDS,
		Aliases: []string{"FamicomDiskSystem"},
	},
	SystemGamate: {
		Id: SystemGamate,
	},
	SystemGameboy: {
		Id:      SystemGameboy,
		Aliases: []string{"GB"},
	},
	SystemGameboyColor: {
		Id:      SystemGameboyColor,
		Aliases: []string{"GBC"},
	},
	SystemGameboy2P: {
		// TODO: Split 2P core into GB and GBC?
		Id: SystemGameboy2P,
	},
	SystemGameCube: {
		Id: SystemGameCube,
	},
	SystemGameGear: {
		Id:      SystemGameGear,
		Aliases: []string{"GG"},
	},
	SystemGameNWatch: {
		Id: SystemGameNWatch,
	},
	SystemGameCom: {
		Id: SystemGameCom,
	},
	SystemGBA: {
		Id:      SystemGBA,
		Aliases: []string{"GameboyAdvance"},
	},
	SystemGBA2P: {
		Id: SystemGBA2P,
	},
	SystemGenesis: {
		Id:      SystemGenesis,
		Aliases: []string{"MegaDrive"},
	},
	SystemIntellivision: {
		Id: SystemIntellivision,
	},
	SystemJaguar: {
		Id: SystemJaguar,
	},
	SystemJaguarCD: {
		Id: SystemJaguarCD,
	},
	SystemMasterSystem: {
		Id:      SystemMasterSystem,
		Aliases: []string{"SMS"},
	},
	SystemMegaCD: {
		Id:      SystemMegaCD,
		Aliases: []string{"SegaCD"},
	},
	SystemMegaDuck: {
		Id: SystemMegaDuck,
	},
	SystemNDS: {
		Id:      SystemNDS,
		Aliases: []string{"NintendoDS"},
	},
	SystemNeoGeo: {
		Id: SystemNeoGeo,
	},
	SystemNeoGeoCD: {
		Id: SystemNeoGeoCD,
	},
	SystemNeoGeoPocket: {
		Id: SystemNeoGeoPocket,
	},
	SystemNeoGeoPocketColor: {
		Id: SystemNeoGeoPocketColor,
	},
	SystemNES: {
		Id: SystemNES,
	},
	SystemNESMusic: {
		Id: SystemNESMusic,
	},
	SystemNintendo64: {
		Id:      SystemNintendo64,
		Aliases: []string{"N64"},
	},
	SystemOdyssey2: {
		Id: SystemOdyssey2,
	},
	SystemOuya: {
		Id: SystemOuya,
	},
	SystemPocketChallengeV2: {
		Id: SystemPocketChallengeV2,
	},
	SystemPokemonMini: {
		Id: SystemPokemonMini,
	},
	SystemPSX: {
		Id:      SystemPSX,
		Aliases: []string{"Playstation", "PS1"},
	},
	SystemPS2: {
		Id:      SystemPS2,
		Aliases: []string{"Playstation2"},
	},
	SystemPS3: {
		Id:      SystemPS3,
		Aliases: []string{"Playstation3"},
	},
	SystemPS4: {
		Id:      SystemPS4,
		Aliases: []string{"Playstation4"},
	},
	SystemPS5: {
		Id:      SystemPS5,
		Aliases: []string{"Playstation5"},
	},
	SystemPSP: {
		Id:      SystemPSP,
		Aliases: []string{"PlaystationPortable"},
	},
	SystemSega32X: {
		Id:      SystemSega32X,
		Aliases: []string{"S32X", "32X"},
	},
	SystemSeriesXS: {
		Id:      SystemSeriesXS,
		Aliases: []string{"SeriesX", "SeriesS"},
	},
	SystemSG1000: {
		Id: SystemSG1000,
	},
	SystemSuperGameboy: {
		Id:      SystemSuperGameboy,
		Aliases: []string{"SGB"},
	},
	SystemSuperVision: {
		Id: SystemSuperVision,
	},
	SystemSaturn: {
		Id: SystemSaturn,
	},
	SystemSNES: {
		Id:      SystemSNES,
		Aliases: []string{"SuperNintendo"},
	},
	SystemSNESMusic: {
		Id: SystemSNESMusic,
	},
	SystemSuperGrafx: {
		Id: SystemSuperGrafx,
	},
	SystemSwitch: {
		Id:      SystemSwitch,
		Aliases: []string{"NintendoSwitch"},
	},
	SystemTurboGrafx16: {
		Id:      SystemTurboGrafx16,
		Aliases: []string{"TGFX16", "PCEngine"},
	},
	SystemTurboGrafx16CD: {
		Id:      SystemTurboGrafx16CD,
		Aliases: []string{"TGFX16-CD", "PCEngineCD"},
	},
	SystemVC4000: {
		Id: SystemVC4000,
	},
	SystemVectrex: {
		Id: SystemVectrex,
	},
	SystemVirtualBoy: {
		Id: SystemVirtualBoy,
	},
	SystemVita: {
		Id:      SystemVita,
		Aliases: []string{"PSVita"},
	},
	SystemWii: {
		Id:      SystemWii,
		Aliases: []string{"NintendoWii"},
	},
	SystemWiiU: {
		Id:      SystemWiiU,
		Aliases: []string{"NintendoWiiU"},
	},
	SystemWonderSwan: {
		Id: SystemWonderSwan,
	},
	SystemWonderSwanColor: {
		Id: SystemWonderSwanColor,
	},
	SystemXbox: {
		Id: SystemXbox,
	},
	SystemXbox360: {
		Id: SystemXbox360,
	},
	SystemXboxOne: {
		Id: SystemXboxOne,
	},
	// Computers
	SystemAcornAtom: {
		Id: SystemAcornAtom,
	},
	SystemAcornElectron: {
		Id: SystemAcornElectron,
	},
	SystemAliceMC10: {
		Id: SystemAliceMC10,
	},
	SystemAmiga: {
		Id:      SystemAmiga,
		Aliases: []string{"Minimig"},
	},
	SystemAmstrad: {
		Id: SystemAmstrad,
	},
	SystemAmstradPCW: {
		Id:      SystemAmstradPCW,
		Aliases: []string{"Amstrad-PCW"},
	},
	SystemDOS: {
		Id:      SystemDOS,
		Aliases: []string{"ao486", "MS-DOS"},
	},
	SystemApogee: {
		Id: SystemApogee,
	},
	SystemAppleI: {
		Id:      SystemAppleI,
		Aliases: []string{"Apple-I"},
	},
	SystemAppleII: {
		Id:      SystemAppleII,
		Aliases: []string{"Apple-II"},
	},
	SystemAquarius: {
		Id: SystemAquarius,
	},
	SystemAtari800: {
		Id: SystemAtari800,
	},
	SystemBBCMicro: {
		Id: SystemBBCMicro,
	},
	SystemBK0011M: {
		Id: SystemBK0011M,
	},
	SystemC16: {
		Id: SystemC16,
	},
	SystemC64: {
		Id: SystemC64,
	},
	SystemCasioPV2000: {
		Id:      SystemCasioPV2000,
		Aliases: []string{"Casio_PV-2000"},
	},
	SystemCoCo2: {
		Id: SystemCoCo2,
	},
	SystemEDSAC: {
		Id: SystemEDSAC,
	},
	SystemGalaksija: {
		Id: SystemGalaksija,
	},
	SystemInteract: {
		Id: SystemInteract,
	},
	SystemJupiter: {
		Id: SystemJupiter,
	},
	SystemLaser: {
		Id:      SystemLaser,
		Aliases: []string{"Laser310"},
	},
	SystemLynx48: {
		Id: SystemLynx48,
	},
	SystemMacPlus: {
		Id: SystemMacPlus,
	},
	SystemMacOS: {
		Id: SystemMacOS,
	},
	SystemMSX: {
		Id: SystemMSX,
	},
	SystemMultiComp: {
		Id: SystemMultiComp,
	},
	SystemOrao: {
		Id: SystemOrao,
	},
	SystemOric: {
		Id: SystemOric,
	},
	SystemPC: {
		Id: SystemPC,
	},
	SystemPCXT: {
		Id: SystemPCXT,
	},
	SystemPDP1: {
		Id: SystemPDP1,
	},
	SystemPET2001: {
		Id: SystemPET2001,
	},
	SystemPMD85: {
		Id: SystemPMD85,
	},
	SystemQL: {
		Id: SystemQL,
	},
	SystemRX78: {
		Id: SystemRX78,
	},
	SystemSAMCoupe: {
		Id: SystemSAMCoupe,
	},
	SystemSordM5: {
		Id:      SystemSordM5,
		Aliases: []string{"Sord M5"},
	},
	SystemSpecialist: {
		Id:      SystemSpecialist,
		Aliases: []string{"SPMX"},
	},
	SystemSVI328: {
		Id: SystemSVI328,
	},
	SystemTatungEinstein: {
		Id: SystemTatungEinstein,
	},
	SystemTI994A: {
		Id:      SystemTI994A,
		Aliases: []string{"TI-99_4A"},
	},
	SystemTomyTutor: {
		Id: SystemTomyTutor,
	},
	SystemTRS80: {
		Id: SystemTRS80,
	},
	SystemTSConf: {
		Id: SystemTSConf,
	},
	SystemUK101: {
		Id: SystemUK101,
	},
	SystemVector06C: {
		Id:      SystemVector06C,
		Aliases: []string{"Vector06"},
	},
	SystemVIC20: {
		Id: SystemVIC20,
	},
	SystemX68000: {
		Id: SystemX68000,
	},
	SystemZX81: {
		Id: SystemZX81,
	},
	SystemZXSpectrum: {
		Id:      SystemZXSpectrum,
		Aliases: []string{"Spectrum"},
	},
	SystemZXNext: {
		Id: SystemZXNext,
	},
	// Other
	SystemAndroid: {
		Id: SystemAndroid,
	},
	SystemArcade: {
		Id: SystemArcade,
	},
	SystemArduboy: {
		Id: SystemArduboy,
	},
	SystemChip8: {
		Id: SystemChip8,
	},
	SystemGroovy: {
		Id: SystemGroovy,
	},
	SystemIOS: {
		Id: SystemIOS,
	},
	SystemVideo: {
		Id: SystemVideo,
	},
}
