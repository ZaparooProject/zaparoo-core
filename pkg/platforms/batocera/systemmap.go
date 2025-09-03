//go:build linux

package batocera

import (
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
)

type SystemInfo struct {
	SystemID   string
	Extensions []string
}

var SystemMap = map[string]SystemInfo{
	"3do": {
		SystemID:   systemdefs.System3DO,
		Extensions: []string{".iso", ".chd", ".cue"},
	},
	"abuse": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".game"},
	},
	"adam": {
		SystemID: systemdefs.SystemColecoAdam,
		Extensions: []string{
			".wav", ".ddp", ".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".d77", ".d88",
			".1dd", ".cqm", ".cqi", ".dsk", ".rom", ".col", ".bin", ".zip", ".7z",
		},
	},
	"advision": {
		SystemID:   systemdefs.SystemAdventureVision,
		Extensions: []string{".bin", ".zip", ".7z"},
	},
	"amiga1200": {
		SystemID:   systemdefs.SystemAmiga1200,
		Extensions: []string{".adf", ".uae", ".ipf", ".dms", ".dmz", ".adz", ".lha", ".hdf", ".exe", ".m3u", ".zip"},
	},
	"amiga500": {
		SystemID:   systemdefs.SystemAmiga500,
		Extensions: []string{".adf", ".uae", ".ipf", ".dms", ".dmz", ".adz", ".lha", ".hdf", ".exe", ".m3u", ".zip"},
	},
	"amigacd32": {
		SystemID:   systemdefs.SystemAmigaCD32,
		Extensions: []string{".bin", ".cue", ".iso", ".chd"},
	},
	"amigacdtv": {
		SystemID:   systemdefs.SystemAmiga,
		Extensions: []string{".bin", ".cue", ".iso", ".chd", ".m3u"},
	},
	"amstradcpc": {
		SystemID:   systemdefs.SystemAmstrad,
		Extensions: []string{".dsk", ".sna", ".tap", ".cdt", ".voc", ".m3u", ".zip", ".7z"},
	},
	"apfm1000": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".bin", ".zip", ".7z"},
	},
	"apple2": {
		SystemID: systemdefs.SystemAppleII,
		Extensions: []string{
			".nib", ".do", ".po", ".dsk", ".mfi", ".dfi", ".rti", ".edd", ".woz", ".wav", ".zip", ".7z",
		},
	},
	"apple2gs": {
		SystemID: systemdefs.SystemAppleII,
		Extensions: []string{
			".nib", ".do", ".po", ".dsk", ".mfi", ".dfi", ".rti", ".edd", ".woz", ".wav", ".zip", ".7z",
		},
	},
	"arcadia": {
		SystemID:   systemdefs.SystemArcadia,
		Extensions: []string{".bin", ".zip", ".7z"},
	},
	"arcade": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".zip", ".7z"},
	},
	"archimedes": {
		SystemID: systemdefs.SystemPC,
		Extensions: []string{
			".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".d77", ".d88", ".1dd", ".cqm", ".cqi",
			".dsk", ".ima", ".img", ".ufi", ".360", ".ipf", ".adf", ".apd", ".jfd", ".ads", ".adm",
			".adl", ".ssd", ".bbc", ".dsd", ".st", ".msa", ".chd", ".zip", ".7z",
		},
	},
	"arduboy": {
		SystemID:   systemdefs.SystemArduboy,
		Extensions: []string{".hex", ".zip", ".7z"},
	},
	"astrocde": {
		SystemID:   systemdefs.SystemAstrocade,
		Extensions: []string{".bin", ".zip", ".7z"},
	},
	"atari2600": {
		SystemID:   systemdefs.SystemAtari2600,
		Extensions: []string{".a26", ".bin", ".zip", ".7z"},
	},
	"atari5200": {
		SystemID: systemdefs.SystemAtari5200,
		Extensions: []string{
			".rom", ".xfd", ".atr", ".atx", ".cdm", ".cas", ".car", ".bin", ".a52", ".xex", ".zip", ".7z",
		},
	},
	"atari7800": {
		SystemID:   systemdefs.SystemAtari7800,
		Extensions: []string{".a78", ".bin", ".zip", ".7z"},
	},
	"atari800": {
		SystemID: systemdefs.SystemAtari800,
		Extensions: []string{
			".rom", ".xfd", ".atr", ".atx", ".cdm", ".cas", ".car", ".bin", ".a52", ".xex", ".zip", ".7z",
		},
	},
	"atarist": {
		SystemID:   systemdefs.SystemAtariST,
		Extensions: []string{".st", ".msa", ".stx", ".dim", ".ipf", ".m3u", ".zip", ".7z"},
	},
	"atom": {
		SystemID: systemdefs.SystemAcornAtom,
		Extensions: []string{
			".wav", ".tap", ".csw", ".uef", ".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".d77",
			".d88", ".1dd", ".cqm", ".cqi", ".dsk", ".40t", ".atm", ".bin", ".rom", ".zip", ".7z",
		},
	},
	"atomiswave": {
		SystemID:   systemdefs.SystemAtomiswave,
		Extensions: []string{".lst", ".bin", ".dat", ".zip", ".7z"},
	},
	"bbc": {
		SystemID: systemdefs.SystemBBCMicro,
		Extensions: []string{
			".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".d77", ".d88", ".1dd", ".cqm", ".cqi",
			".dsk", ".ima", ".img", ".ufi", ".360", ".ipf", ".ssd", ".bbc", ".dsd", ".adf", ".ads",
			".adm", ".adl", ".fsd", ".wav", ".tap", ".bin", ".zip", ".7z",
		},
	},
	"c128": {
		SystemID:   systemdefs.SystemC64,
		Extensions: []string{".d64", ".d81", ".prg", ".lnx", ".m3u", ".zip", ".7z"},
	},
	"c20": {
		SystemID:   systemdefs.SystemVIC20,
		Extensions: []string{".a0", ".b0", ".crt", ".d64", ".d81", ".prg", ".tap", ".t64", ".m3u", ".zip", ".7z"},
	},
	"c64": {
		SystemID:   systemdefs.SystemC64,
		Extensions: []string{".d64", ".d81", ".crt", ".prg", ".tap", ".t64", ".m3u", ".zip", ".7z"},
	},
	"camplynx": {
		SystemID:   systemdefs.SystemLynx48,
		Extensions: []string{".wav", ".tap", ".zip", ".7z"},
	},
	"cannonball": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".cannonball"},
	},
	"cavestory": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".exe"},
	},
	"cdi": {
		SystemID:   systemdefs.SystemCDI,
		Extensions: []string{".chd", ".cue", ".toc", ".nrg", ".gdi", ".iso", ".cdr"},
	},
	"cdogs": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".game"},
	},
	"cgenius": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".zip", ".7z"},
	},
	"channelf": {
		SystemID:   systemdefs.SystemChannelF,
		Extensions: []string{".zip", ".rom", ".bin", ".chf"},
	},
	"coco": {
		SystemID:   systemdefs.SystemCoCo2,
		Extensions: []string{".wav", ".cas", ".ccc", ".rom", ".zip", ".7z"},
	},
	"colecovision": {
		SystemID:   systemdefs.SystemColecoVision,
		Extensions: []string{".bin", ".col", ".rom", ".zip", ".7z"},
	},
	"commanderx16": {
		SystemID:   systemdefs.SystemCommanderX16,
		Extensions: []string{".prg", ".crt", ".bin", ".zip"},
	},
	"corsixth": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".game"},
	},
	"cplus4": {
		SystemID:   systemdefs.SystemC16,
		Extensions: []string{".d64", ".prg", ".tap", ".m3u", ".zip", ".7z"},
	},
	"crvision": {
		SystemID:   systemdefs.SystemCreatiVision,
		Extensions: []string{".bin", ".rom", ".zip", ".7z"},
	},
	"daphne": {
		SystemID:   systemdefs.SystemDAPHNE,
		Extensions: []string{".daphne", ".squashfs"},
	},
	"devilutionx": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".mpq"},
	},
	"doom3": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".boom3"},
	},
	"dos": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".pc", ".dos", ".zip", ".squashfs", ".dosz", ".m3u", ".iso", ".cue"},
	},
	"dreamcast": {
		SystemID:   systemdefs.SystemDreamcast,
		Extensions: []string{".cdi", ".cue", ".gdi", ".chd", ".m3u"},
	},
	"easyrpg": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".easyrpg", ".squashfs", ".zip"},
	},
	"ecwolf": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".ecwolf", ".pk3", ".squashfs"},
	},
	"eduke32": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".eduke32"},
	},
	"electron": {
		SystemID: systemdefs.SystemAcornElectron,
		Extensions: []string{
			".wav", ".csw", ".uef", ".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".d77", ".d88",
			".1dd", ".cqm", ".cqi", ".dsk", ".ssd", ".bbc", ".img", ".dsd", ".adf", ".ads", ".adm",
			".adl", ".rom", ".bin", ".zip", ".7z",
		},
	},
	"fallout1-ce": {
		SystemID:   systemdefs.SystemWindows,
		Extensions: []string{".f1ce"},
	},
	"fallout2-ce": {
		SystemID:   systemdefs.SystemWindows,
		Extensions: []string{".f2ce"},
	},
	"fbneo": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".zip", ".7z"},
	},
	"fds": {
		SystemID:   systemdefs.SystemFDS,
		Extensions: []string{".fds", ".zip", ".7z"},
	},
	"flash": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".swf"},
	},
	"fm7": {
		SystemID: systemdefs.SystemFM7,
		Extensions: []string{
			".wav", ".t77", ".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".d77", ".d88",
			".1dd", ".cqm", ".cqi", ".dsk", ".zip", ".7z",
		},
	},
	"fmtowns": {
		SystemID: systemdefs.SystemFMTowns,
		Extensions: []string{
			".bin", ".m3u", ".cue", ".d88", ".d77", ".xdf", ".iso", ".chd", ".toc", ".nrg", ".gdi",
			".cdr", ".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".1dd", ".cqm", ".cqi", ".dsk",
			".zip", ".7z",
		},
	},
	"gamate": {
		SystemID:   systemdefs.SystemGamate,
		Extensions: []string{".bin", ".zip", ".7z"},
	},
	"gameandwatch": {
		SystemID:   systemdefs.SystemGameNWatch,
		Extensions: []string{".mgw", ".zip", ".7z"},
	},
	"gamecom": {
		SystemID:   systemdefs.SystemGameCom,
		Extensions: []string{".bin", ".tgc", ".zip", ".7z"},
	},
	"gamecube": {
		SystemID:   systemdefs.SystemGameCube,
		Extensions: []string{".gcm", ".iso", ".gcz", ".ciso", ".wbfs", ".rvz", ".elf", ".dol", ".m3u"},
	},
	"gamegear": {
		SystemID:   systemdefs.SystemGameGear,
		Extensions: []string{".bin", ".gg", ".zip", ".7z"},
	},
	"gamepock": {
		SystemID:   systemdefs.SystemGamePocket,
		Extensions: []string{".bin", ".zip", ".7z"},
	},
	"gb": {
		SystemID:   systemdefs.SystemGameboy,
		Extensions: []string{".gb", ".zip", ".7z"},
	},
	"gb2players": {
		SystemID:   systemdefs.SystemGameboy2P,
		Extensions: []string{".gb", ".gb2", ".gbc2", ".zip", ".7z"},
	},
	"gba": {
		SystemID:   systemdefs.SystemGBA,
		Extensions: []string{".gba", ".zip", ".7z"},
	},
	"gbc": {
		SystemID:   systemdefs.SystemGameboyColor,
		Extensions: []string{".gbc", ".zip", ".7z"},
	},
	"gbc2players": {
		SystemID:   systemdefs.SystemGameboy2P,
		Extensions: []string{".gbc", ".gb2", ".gbc2", ".zip", ".7z"},
	},
	"gmaster": {
		SystemID:   systemdefs.SystemGameMaster,
		Extensions: []string{".bin", ".zip", ".7z"},
	},
	"gp32": {
		SystemID:   systemdefs.SystemGP32,
		Extensions: []string{".smc", ".zip", ".7z"},
	},
	"gx4000": {
		SystemID:   systemdefs.SystemAmstrad,
		Extensions: []string{".dsk", ".m3u", ".cpr", ".zip", ".7z"},
	},
	"gzdoom": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".wad", ".iwad", ".pwad", ".gzdoom"},
	},
	"hcl": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".game"},
	},
	"intellivision": {
		SystemID:   systemdefs.SystemIntellivision,
		Extensions: []string{".int", ".bin", ".rom", ".zip", ".7z"},
	},
	"jaguar": {
		SystemID:   systemdefs.SystemJaguar,
		Extensions: []string{".cue", ".j64", ".jag", ".cof", ".abs", ".cdi", ".rom", ".zip", ".7z"},
	},
	"jaguarcd": {
		SystemID:   systemdefs.SystemJaguarCD,
		Extensions: []string{".cue", ".chd"},
	},
	"laser310": {
		SystemID:   systemdefs.SystemLaser,
		Extensions: []string{".vz", ".wav", ".cas", ".zip", ".7z"},
	},
	"lcdgames": {
		SystemID:   systemdefs.SystemGameNWatch,
		Extensions: []string{".mgw", ".zip", ".7z"},
	},
	"lynx": {
		SystemID:   systemdefs.SystemAtariLynx,
		Extensions: []string{".lnx", ".zip", ".7z"},
	},
	"macintosh": {
		SystemID: systemdefs.SystemMacOS,
		Extensions: []string{
			".dsk", ".zip", ".7z", ".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".d77", ".d88",
			".1dd", ".cqm", ".cqi", ".ima", ".img", ".ufi", ".ipf", ".dc42", ".woz", ".2mg", ".360",
			".chd", ".cue", ".toc", ".nrg", ".gdi", ".iso", ".cdr", ".hd", ".hdv", ".hdi",
		},
	},
	"mame": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".zip", ".7z"},
	},
	"mastersystem": {
		SystemID:   systemdefs.SystemMasterSystem,
		Extensions: []string{".bin", ".sms", ".zip", ".7z"},
	},
	"megadrive": {
		SystemID:   systemdefs.SystemGenesis,
		Extensions: []string{".bin", ".gen", ".md", ".sg", ".smd", ".zip", ".7z"},
	},
	"megaduck": {
		SystemID:   systemdefs.SystemMegaDuck,
		Extensions: []string{".bin", ".zip", ".7z"},
	},
	"model3": {
		SystemID:   systemdefs.SystemModel3,
		Extensions: []string{".zip"},
	},
	"moonlight": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".moonlight"},
	},
	"msx1": {
		SystemID:   systemdefs.SystemMSX,
		Extensions: []string{".dsk", ".mx1", ".rom", ".zip", ".7z", ".cas", ".m3u"},
	},
	"msx2": {
		SystemID:   systemdefs.SystemMSX,
		Extensions: []string{".dsk", ".mx2", ".rom", ".zip", ".7z", ".cas", ".m3u"},
	},
	"msx2+": {
		SystemID:   systemdefs.SystemMSX,
		Extensions: []string{".dsk", ".mx2", ".rom", ".zip", ".7z", ".cas", ".m3u"},
	},
	"msxturbor": {
		SystemID:   systemdefs.SystemMSX,
		Extensions: []string{".dsk", ".mx2", ".rom", ".zip", ".7z"},
	},
	"n64": {
		SystemID:   systemdefs.SystemNintendo64,
		Extensions: []string{".z64", ".n64", ".v64", ".zip", ".7z"},
	},
	"n64dd": {
		SystemID:   systemdefs.SystemNintendo64,
		Extensions: []string{".z64", ".z64.ndd"},
	},
	"naomi": {
		SystemID:   systemdefs.SystemNAOMI,
		Extensions: []string{".lst", ".bin", ".dat", ".zip", ".7z"},
	},
	"naomi2": {
		SystemID:   systemdefs.SystemNAOMI2,
		Extensions: []string{".zip", ".7z"},
	},
	"nds": {
		SystemID:   systemdefs.SystemNDS,
		Extensions: []string{".nds", ".bin", ".zip", ".7z"},
	},
	"neogeo": {
		SystemID:   systemdefs.SystemNeoGeo,
		Extensions: []string{".7z", ".zip"},
	},
	"neogeocd": {
		SystemID:   systemdefs.SystemNeoGeoCD,
		Extensions: []string{".cue", ".iso", ".chd"},
	},
	"nes": {
		SystemID:   systemdefs.SystemNES,
		Extensions: []string{".nes", ".unif", ".unf", ".zip", ".7z"},
	},
	"ngp": {
		SystemID:   systemdefs.SystemNeoGeoPocket,
		Extensions: []string{".ngp", ".zip", ".7z"},
	},
	"ngpc": {
		SystemID:   systemdefs.SystemNeoGeoPocketColor,
		Extensions: []string{".ngc", ".zip", ".7z"},
	},
	"o2em": {
		SystemID:   systemdefs.SystemOdyssey2,
		Extensions: []string{".bin", ".zip", ".7z"},
	},
	"pc88": {
		SystemID:   systemdefs.SystemPC88,
		Extensions: []string{".d88", ".u88", ".m3u"},
	},
	"pc98": {
		SystemID: systemdefs.SystemPC98,
		Extensions: []string{
			".d98", ".zip", ".98d", ".fdi", ".fdd", ".2hd", ".tfd", ".d88", ".88d", ".hdm",
			".xdf", ".dup", ".cmd", ".hdi", ".thd", ".nhd", ".hdd", ".hdn", ".m3u",
		},
	},
	"pcengine": {
		SystemID:   systemdefs.SystemTurboGrafx16,
		Extensions: []string{".pce", ".bin", ".zip", ".7z"},
	},
	"pcenginecd": {
		SystemID:   systemdefs.SystemTurboGrafx16CD,
		Extensions: []string{".pce", ".cue", ".ccd", ".iso", ".img", ".chd"},
	},
	"pcfx": {
		SystemID:   systemdefs.SystemPCFX,
		Extensions: []string{".cue", ".ccd", ".toc", ".chd", ".zip", ".7z"},
	},
	"pdp1": {
		SystemID:   systemdefs.SystemPDP1,
		Extensions: []string{".zip", ".7z", ".tap", ".rim", ".drm"},
	},
	"pet": {
		SystemID:   systemdefs.SystemPET2001,
		Extensions: []string{".a0", ".b0", ".crt", ".d64", ".d81", ".prg", ".tap", ".t64", ".m3u", ".zip", ".7z"},
	},
	"pico": {
		SystemID:   systemdefs.SystemGenesis,
		Extensions: []string{".bin", ".md", ".zip", ".7z"},
	},
	"pico8": {
		SystemID:   systemdefs.SystemPico8,
		Extensions: []string{".p8", ".png", ".m3u"},
	},
	"pokemini": {
		SystemID:   systemdefs.SystemPokemonMini,
		Extensions: []string{".min", ".zip", ".7z"},
	},
	"psp": {
		SystemID:   systemdefs.SystemPSP,
		Extensions: []string{".iso", ".cso", ".pbp", ".chd"},
	},
	"psx": {
		SystemID:   systemdefs.SystemPSX,
		Extensions: []string{".cue", ".img", ".mdf", ".pbp", ".toc", ".cbn", ".m3u", ".ccd", ".chd", ".iso"},
	},
	"pv1000": {
		SystemID:   systemdefs.SystemCasioPV1000,
		Extensions: []string{".bin", ".zip", ".7z"},
	},
	"samcoupe": {
		SystemID:   systemdefs.SystemSAMCoupe,
		Extensions: []string{".cpm", ".dsk", ".sad", ".mgt", ".sdf", ".td0", ".sbt", ".zip"},
	},
	"satellaview": {
		SystemID:   systemdefs.SystemSNES,
		Extensions: []string{".bs", ".smc", ".sfc", ".zip", ".7z"},
	},
	"saturn": {
		SystemID:   systemdefs.SystemSaturn,
		Extensions: []string{".cue", ".ccd", ".m3u", ".chd", ".iso", ".zip"},
	},
	"scummvm": {
		SystemID:   systemdefs.SystemScummVM,
		Extensions: []string{".scummvm", ".squashfs"},
	},
	"scv": {
		SystemID:   systemdefs.SystemSG1000,
		Extensions: []string{".bin", ".zip", ".0"},
	},
	"sega32x": {
		SystemID:   systemdefs.SystemSega32X,
		Extensions: []string{".32x", ".chd", ".smd", ".bin", ".md", ".zip", ".7z"},
	},
	"segacd": {
		SystemID:   systemdefs.SystemMegaCD,
		Extensions: []string{".cue", ".iso", ".chd", ".m3u"},
	},
	"sg1000": {
		SystemID:   systemdefs.SystemSG1000,
		Extensions: []string{".bin", ".sg", ".zip", ".7z"},
	},
	"sgb": {
		SystemID:   systemdefs.SystemSuperGameboy,
		Extensions: []string{".gb", ".gbc", ".zip", ".7z"},
	},
	"snes": {
		SystemID:   systemdefs.SystemSNES,
		Extensions: []string{".smc", ".fig", ".sfc", ".gd3", ".gd7", ".dx2", ".bsx", ".swc", ".zip", ".7z"},
	},
	"snes-msu1": {
		SystemID: systemdefs.SystemSNESMSU1,
		Extensions: []string{
			".smc", ".fig", ".sfc", ".gd3", ".gd7", ".dx2", ".bsx", ".swc", ".zip", ".7z", ".squashfs",
		},
	},
	"supergrafx": {
		SystemID:   systemdefs.SystemSuperGrafx,
		Extensions: []string{".pce", ".sgx", ".cue", ".ccd", ".chd", ".zip", ".7z"},
	},
	"supervision": {
		SystemID:   systemdefs.SystemSuperVision,
		Extensions: []string{".sv", ".zip", ".7z"},
	},
	"ti99": {
		SystemID:   systemdefs.SystemTI994A,
		Extensions: []string{".rpk", ".wav", ".zip", ".7z"},
	},
	"tutor": {
		SystemID:   systemdefs.SystemTomyTutor,
		Extensions: []string{".bin", ".wav", ".zip", ".7z"},
	},
	"vc4000": {
		SystemID:   systemdefs.SystemVC4000,
		Extensions: []string{".bin", ".rom", ".pgm", ".tvc", ".zip", ".7z"},
	},
	"vectrex": {
		SystemID:   systemdefs.SystemVectrex,
		Extensions: []string{".bin", ".gam", ".vec", ".zip", ".7z"},
	},
	"virtualboy": {
		SystemID:   systemdefs.SystemVirtualBoy,
		Extensions: []string{".vb", ".zip", ".7z"},
	},
	"wii": {
		SystemID:   systemdefs.SystemWii,
		Extensions: []string{".gcm", ".iso", ".gcz", ".ciso", ".wbfs", ".wad", ".rvz", ".elf", ".dol", ".m3u", ".json"},
	},
	"wswan": {
		SystemID:   systemdefs.SystemWonderSwan,
		Extensions: []string{".ws", ".zip", ".7z"},
	},
	"wswanc": {
		SystemID:   systemdefs.SystemWonderSwanColor,
		Extensions: []string{".wsc", ".zip", ".7z"},
	},
	"x1": {
		SystemID: systemdefs.SystemX1,
		Extensions: []string{
			".dx1", ".zip", ".2d", ".2hd", ".tfd", ".d88", ".88d", ".hdm", ".xdf", ".dup", ".cmd", ".7z",
		},
	},
	"x68000": {
		SystemID: systemdefs.SystemX68000,
		Extensions: []string{
			".dim", ".img", ".d88", ".88d", ".hdm", ".dup", ".2hd", ".xdf", ".hdf", ".cmd", ".m3u", ".zip", ".7z",
		},
	},
	"xegs": {
		SystemID:   systemdefs.SystemAtariXEGS,
		Extensions: []string{".atr", ".dsk", ".xfd", ".bin", ".rom", ".car", ".zip", ".7z"},
	},
	"zx81": {
		SystemID:   systemdefs.SystemZX81,
		Extensions: []string{".tzx", ".p", ".zip", ".7z"},
	},
	"zxspectrum": {
		SystemID:   systemdefs.SystemZXSpectrum,
		Extensions: []string{".tzx", ".tap", ".z80", ".rzx", ".scl", ".trd", ".dsk", ".zip", ".7z"},
	},
}

func fromBatoceraSystem(batoceraSystem string) (string, error) {
	v, ok := SystemMap[batoceraSystem]
	if !ok {
		return "", fmt.Errorf("unknown system: %s", batoceraSystem)
	}
	return v.SystemID, nil
}

func toBatoceraSystems(zaparooSystem string) ([]string, error) {
	var results []string
	for k, v := range SystemMap {
		if strings.EqualFold(zaparooSystem, v.SystemID) {
			results = append(results, k)
		}
	}
	return results, nil
}

var LauncherMap = map[string]string{
	"3do":           "3DO",
	"abuse":         "Abuse",
	"adam":          "Adam",
	"advision":      "AdVision",
	"amiga1200":     "Amiga1200",
	"amiga500":      "Amiga500",
	"amigacd32":     "AmigaCD32",
	"amigacdtv":     "AmigaCDTV",
	"amstradcpc":    "AmstradCPC",
	"apfm1000":      "APFM1000",
	"apple2":        "Apple2",
	"apple2gs":      "Apple2GS",
	"arcadia":       "Arcadia",
	"arcade":        "Arcade",
	"archimedes":    "Archimedes",
	"arduboy":       "Arduboy",
	"astrocde":      "Astrocde",
	"atari2600":     "Atari2600",
	"atari5200":     "Atari5200",
	"atari7800":     "Atari7800",
	"atari800":      "Atari800",
	"atarist":       "AtariST",
	"atom":          "Atom",
	"atomiswave":    "Atomiswave",
	"bbc":           "BBC",
	"c128":          "C128",
	"c20":           "C20",
	"c64":           "C64",
	"camplynx":      "CampLynx",
	"cannonball":    "Cannonball",
	"cavestory":     "CaveStory",
	"cdi":           "CDI",
	"cdogs":         "CCogs",
	"cgenius":       "CGenius",
	"channelf":      "Channelf",
	"coco":          "CoCo",
	"colecovision":  "ColecoVision",
	"commanderx16":  "CommanderX16",
	"corsixth":      "CorsixTH",
	"cplus4":        "CPlus4",
	"crvision":      "CRVision",
	"daphne":        "Daphne",
	"devilutionx":   "DevilutionX",
	"doom3":         "Doom3",
	"dos":           "DOS",
	"dreamcast":     "Dreamcast",
	"easyrpg":       "EasyRPG",
	"ecwolf":        "ECWolf",
	"eduke32":       "ESuke32",
	"electron":      "Electron",
	"fallout1-ce":   "Fallout1CE",
	"fallout2-ce":   "Fallout2CE",
	"fbneo":         "FBNeo",
	"fds":           "FDS",
	"flash":         "Flash",
	"fm7":           "FM7",
	"fmtowns":       "FMTowns",
	"gamate":        "Gamate",
	"gameandwatch":  "GameAndWatch",
	"gamecom":       "GameCom",
	"gamecube":      "GameCube",
	"gamegear":      "GameGear",
	"gamepock":      "GamePock",
	"gb":            "GB",
	"gb2players":    "GB2Players",
	"gba":           "GBA",
	"gbc":           "GBC",
	"gbc2players":   "GBC2Players",
	"gmaster":       "GMaster",
	"gp32":          "GP32",
	"gx4000":        "GX4000",
	"gzdoom":        "GZDoom",
	"hcl":           "HCL",
	"intellivision": "Intellivision",
	"jaguar":        "Jaguar",
	"jaguarcd":      "JaguarCD",
	"laser310":      "Laser310",
	"lcdgames":      "LCDGames",
	"lynx":          "Lynx",
	"macintosh":     "Macintosh",
	"mame":          "MAME",
	"mastersystem":  "MasterSystem",
	"megadrive":     "MegaDrive",
	"megaduck":      "MegaDuck",
	"model3":        "Model3",
	"moonlight":     "Moonlight",
	"msx1":          "MSX1",
	"msx2":          "MSX2",
	"msx2+":         "MSX2Plus",
	"msxturbor":     "MSXTurboR",
	"n64":           "N64",
	"n64dd":         "N64DD",
	"naomi":         "NAOMI",
	"naomi2":        "NAOMI2",
	"nds":           "NDS",
	"neogeo":        "NeoGeo",
	"neogeocd":      "NeoGeoCD",
	"nes":           "NES",
	"ngp":           "NGP",
	"ngpc":          "NGPC",
	"o2em":          "O2EM",
	"pc88":          "PC88",
	"pc98":          "PC98",
	"pcengine":      "PCEngine",
	"pcenginecd":    "PCEngineCD",
	"pcfx":          "PCFX",
	"pdp1":          "PDP1",
	"pet":           "PET",
	"pico":          "Pico",
	"pico8":         "Pico8",
	"pokemini":      "PokeMini",
	"psp":           "PSP",
	"psx":           "PSX",
	"pv1000":        "PV1000",
	"samcoupe":      "SamCoupe",
	"satellaview":   "Satellaview",
	"saturn":        "Saturn",
	"scummvm":       "ScummVM",
	"scv":           "SCV",
	"sega32x":       "Sega32x",
	"segacd":        "SegaCD",
	"sg1000":        "SG1000",
	"sgb":           "SGB",
	"snes":          "SNES",
	"snes-msu1":     "SNESMSU1",
	"supergrafx":    "SuperGrafx",
	"supervision":   "SuperVision",
	"ti99":          "TI99",
	"tutor":         "Tutor",
	"vc4000":        "VC4000",
	"vectrex":       "Vectrex",
	"virtualboy":    "VirtualBoy",
	"wii":           "Wii",
	"wswan":         "WSwan",
	"wswanc":        "WSwanC",
	"x1":            "X1",
	"x68000":        "X68000",
	"xegs":          "XEGX",
	"zx81":          "ZX81",
	"zxspectrum":    "ZXSpectrum",
}
