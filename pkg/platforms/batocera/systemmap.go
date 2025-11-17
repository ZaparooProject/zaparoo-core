//go:build linux

package batocera

import (
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
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
	"3ds": {
		SystemID:   systemdefs.System3DS,
		Extensions: []string{".3ds", ".cci", ".cxi"},
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
		SystemID: systemdefs.SystemArchimedes,
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
	"bennugd": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".zip", ".7z"},
	},
	"bstone": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".bstone"},
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
	"catacomb": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".game"},
	},
	"cave3rd": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".zip", ".7z"},
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
	"chihiro": {
		SystemID:   systemdefs.SystemChihiro,
		Extensions: []string{".chd"},
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
	"dice": {
		SystemID:   systemdefs.SystemDICE,
		Extensions: []string{".zip", ".dmy"},
	},
	"doom3": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".d3"},
	},
	"dos": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".pc", ".dos", ".zip", ".squashfs", ".dosz", ".m3u", ".iso", ".cue"},
	},
	"dreamcast": {
		SystemID:   systemdefs.SystemDreamcast,
		Extensions: []string{".cdi", ".cue", ".gdi", ".chd", ".m3u"},
	},
	"dxx-rebirth": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".d1x", ".d2x"},
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
	"etlegacy": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".etlegacy"},
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
	"fury": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".grp"},
	},
	"gaelco": {
		SystemID:   systemdefs.SystemGaelco,
		Extensions: []string{".zip"},
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
	"gong": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".game"},
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
	"hikaru": {
		SystemID:   systemdefs.SystemHikaru,
		Extensions: []string{".chd", ".zip"},
	},
	"hurrican": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".game"},
	},
	"imageviewer": {
		SystemID:   systemdefs.SystemImage,
		Extensions: []string{".jpg", ".png", ".gif", ".bmp"},
	},
	"intellivision": {
		SystemID:   systemdefs.SystemIntellivision,
		Extensions: []string{".int", ".bin", ".rom", ".zip", ".7z"},
	},
	"iortcw": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".iortcw"},
	},
	"j2me": {
		SystemID:   systemdefs.SystemJ2ME,
		Extensions: []string{".jar"},
	},
	"jaguar": {
		SystemID:   systemdefs.SystemJaguar,
		Extensions: []string{".cue", ".j64", ".jag", ".cof", ".abs", ".cdi", ".rom", ".zip", ".7z"},
	},
	"jaguarcd": {
		SystemID:   systemdefs.SystemJaguarCD,
		Extensions: []string{".cue", ".chd"},
	},
	"jazz2": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".jazz2"},
	},
	"jkdf2": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".jkdf2"},
	},
	"jknight": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".jknight"},
	},
	"laser310": {
		SystemID:   systemdefs.SystemLaser,
		Extensions: []string{".vz", ".wav", ".cas", ".zip", ".7z"},
	},
	"lcdgames": {
		SystemID:   systemdefs.SystemGameNWatch,
		Extensions: []string{".mgw", ".zip", ".7z"},
	},
	"library": {
		SystemID: systemdefs.SystemPC,
		Extensions: []string{
			".jpg", ".jpeg", ".png", ".bmp", ".psd", ".tga", ".gif", ".hdr", ".pic", ".ppm", ".pgm",
			".mkv", ".pdf", ".mp4", ".avi", ".webm", ".cbz", ".mp3", ".wav", ".ogg", ".flac",
			".mod", ".xm", ".stm", ".s3m", ".far", ".it", ".669", ".mtm",
		},
	},
	"lindbergh": {
		SystemID:   systemdefs.SystemLindbergh,
		Extensions: []string{".zip"},
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
	"mohaa": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".mohaa"},
	},
	"model1": {
		SystemID:   systemdefs.SystemModel1,
		Extensions: []string{".zip"},
	},
	"model2": {
		SystemID:   systemdefs.SystemModel2,
		Extensions: []string{".zip"},
	},
	"model3": {
		SystemID:   systemdefs.SystemModel3,
		Extensions: []string{".zip"},
	},
	"moonlight": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".moonlight"},
	},
	"mrboom": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".libretro"},
	},
	"msu-md": {
		SystemID:   systemdefs.SystemGenesisMSU,
		Extensions: []string{".msu", ".md"},
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
		SystemID:   systemdefs.SystemMSX2Plus,
		Extensions: []string{".dsk", ".mx2", ".rom", ".zip", ".7z", ".cas", ".m3u"},
	},
	"msxturbor": {
		SystemID:   systemdefs.SystemMSX,
		Extensions: []string{".dsk", ".mx2", ".rom", ".zip", ".7z"},
	},
	"multivision": {
		SystemID:   systemdefs.SystemMultivision,
		Extensions: []string{".bin", ".gg", ".rom", ".sg", ".sms", ".zip"},
	},
	"n64": {
		SystemID:   systemdefs.SystemNintendo64,
		Extensions: []string{".z64", ".n64", ".v64", ".zip", ".7z"},
	},
	"n64dd": {
		SystemID:   systemdefs.SystemNintendo64,
		Extensions: []string{".z64", ".z64.ndd"},
	},
	"namco22": {
		SystemID:   systemdefs.SystemNamco22,
		Extensions: []string{".zip"},
	},
	"namco2x6": {
		SystemID:   systemdefs.SystemNamco2X6,
		Extensions: []string{".zip"},
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
	"ngage": {
		SystemID:   systemdefs.SystemNGage,
		Extensions: []string{".ngage", ".jar"},
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
	"psvita": {
		SystemID:   systemdefs.SystemVita,
		Extensions: []string{".vpk", ".mai"},
	},
	"ps3": {
		SystemID:   systemdefs.SystemPS3,
		Extensions: []string{".ps3", ".ps3dir"},
	},
	"ps4": {
		SystemID:   systemdefs.SystemPS4,
		Extensions: []string{".ps4"},
	},
	"psp": {
		SystemID:   systemdefs.SystemPSP,
		Extensions: []string{".iso", ".cso", ".pbp", ".chd"},
	},
	"psx": {
		SystemID:   systemdefs.SystemPSX,
		Extensions: []string{".cue", ".img", ".mdf", ".pbp", ".toc", ".cbn", ".m3u", ".ccd", ".chd", ".iso"},
	},
	"ps2": {
		SystemID:   systemdefs.SystemPS2,
		Extensions: []string{".iso", ".mdf", ".nrg", ".bin", ".img", ".dump", ".gz", ".cso", ".chd"},
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
	"megacd": {
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
	"sgb-msu1": {
		SystemID:   systemdefs.SystemSGBMSU1,
		Extensions: []string{".gb", ".gbc", ".zip", ".7z"},
	},
	"singe": {
		SystemID:   systemdefs.SystemSinge,
		Extensions: []string{".singe"},
	},
	"snes": {
		SystemID:   systemdefs.SystemSNES,
		Extensions: []string{".smc", ".fig", ".sfc", ".gd3", ".gd7", ".dx2", ".bsx", ".swc", ".zip", ".7z"},
	},
	"socrates": {
		SystemID:   systemdefs.SystemSocrates,
		Extensions: []string{".bin", ".zip"},
	},
	"spectravideo": {
		SystemID:   systemdefs.SystemSpectravideo,
		Extensions: []string{".cas", ".rom", ".ri", ".mx1", ".mx2", ".dsk", ".zip"},
	},
	"sufami": {
		SystemID:   systemdefs.SystemSufami,
		Extensions: []string{".st", ".zip"},
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
	"supracan": {
		SystemID:   systemdefs.SystemSuperACan,
		Extensions: []string{".bin", ".zip"},
	},
	"supervision": {
		SystemID:   systemdefs.SystemSuperVision,
		Extensions: []string{".sv", ".zip", ".7z"},
	},
	"switch": {
		SystemID:   systemdefs.SystemSwitch,
		Extensions: []string{".nsp", ".xci", ".nca", ".nro"},
	},
	"thomson": {
		SystemID:   systemdefs.SystemThomson,
		Extensions: []string{".fd", ".sap", ".k7", ".m7", ".m5", ".rom", ".zip"},
	},
	"ti99": {
		SystemID:   systemdefs.SystemTI994A,
		Extensions: []string{".rpk", ".wav", ".zip", ".7z"},
	},
	"triforce": {
		SystemID:   systemdefs.SystemTriforce,
		Extensions: []string{".iso", ".gcz"},
	},
	"tutor": {
		SystemID:   systemdefs.SystemTomyTutor,
		Extensions: []string{".bin", ".wav", ".zip", ".7z"},
	},
	"videopacplus": {
		SystemID:   systemdefs.SystemVideopacPlus,
		Extensions: []string{".bin", ".zip"},
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
	"vsmile": {
		SystemID:   systemdefs.SystemVSmile,
		Extensions: []string{".zip", ".7z"},
	},
	"wii": {
		SystemID:   systemdefs.SystemWii,
		Extensions: []string{".gcm", ".iso", ".gcz", ".ciso", ".wbfs", ".wad", ".rvz", ".elf", ".dol", ".m3u", ".json"},
	},
	"wiiu": {
		SystemID:   systemdefs.SystemWiiU,
		Extensions: []string{".wud", ".wux", ".rpx"},
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
	"xbox": {
		SystemID:   systemdefs.SystemXbox,
		Extensions: []string{".iso", ".squashfs"},
	},
	"xbox360": {
		SystemID:   systemdefs.SystemXbox360,
		Extensions: []string{".iso", ".xex", ".god"},
	},
	"xash3d_fwgs": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".xash3d"},
	},
	"xegs": {
		SystemID:   systemdefs.SystemAtariXEGS,
		Extensions: []string{".atr", ".dsk", ".xfd", ".bin", ".rom", ".car", ".zip", ".7z"},
	},
	"xrick": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".xrick"},
	},
	"zc210": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".zc210"},
	},
	"zx81": {
		SystemID:   systemdefs.SystemZX81,
		Extensions: []string{".tzx", ".p", ".zip", ".7z"},
	},
	"zxspectrum": {
		SystemID:   systemdefs.SystemZXSpectrum,
		Extensions: []string{".tzx", ".tap", ".z80", ".rzx", ".scl", ".trd", ".dsk", ".zip", ".7z"},
	},
	"ikemen": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".ikemen"},
	},
	"lowresnx": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".nx"},
	},
	"lutro": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".lua", ".lutro"},
	},
	"mugen": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".mugen"},
	},
	"odcommander": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".odcommander"},
	},
	"openbor": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".pak"},
	},
	"openjazz": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".openjazz"},
	},
	"oricatmos": {
		SystemID:   systemdefs.SystemOric,
		Extensions: []string{".dsk", ".tap"},
	},
	"ports": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".sh", ".squashfs"},
	},
	"prboom": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".wad", ".iwad", ".pwad"},
	},
	"pygame": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".py", ".pygame"},
	},
	"quake": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".quake"},
	},
	"quake2": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".quake2", ".zip", ".7zip"},
	},
	"quake3": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".quake3"},
	},
	"raze": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".raze"},
	},
	"recordings": {
		SystemID:   systemdefs.SystemVideo,
		Extensions: []string{".mp4", ".avi", ".mkv"},
	},
	"reminiscence": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".reminiscence"},
	},
	"rott": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".rott"},
	},
	"sdlpop": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".sdlpop"},
	},
	"solarus": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".solarus"},
	},
	"sonic3-air": {
		SystemID:   systemdefs.SystemGenesis,
		Extensions: []string{".sonic3air"},
	},
	"sonic-mania": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".sman"},
	},
	"sonicretro": {
		SystemID:   systemdefs.SystemGenesis,
		Extensions: []string{".sonicretro"},
	},
	"superbroswar": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".superbroswar"},
	},
	"systemsp": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".zip"},
	},
	"theforceengine": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".theforceengine"},
	},
	"thextech": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".thextech"},
	},
	"tic80": {
		SystemID:   systemdefs.SystemTIC80,
		Extensions: []string{".tic"},
	},
	"tyrquake": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".pak"},
	},
	"traider1": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".traider1"},
	},
	"traider2": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".traider2"},
	},
	"tyrian": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".game"},
	},
	"uqm": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".uqm"},
	},
	"uzebox": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".uze"},
	},
	"vemulator": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".vemulator"},
	},
	"vgmplay": {
		SystemID:   systemdefs.SystemAudio,
		Extensions: []string{".vgm", ".vgz"},
	},
	"wine": {
		SystemID:   systemdefs.SystemWindows,
		Extensions: []string{".wine", ".exe"},
	},
	"pyxel": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".pyxel", ".pyxapp"},
	},
	"vircon32": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".v32"},
	},
	"vis": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".vis"},
	},
	"vpinball": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".vpx", ".vpt"},
	},
	"wasm4": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".wasm"},
	},
	"flatpak": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".flatpak"},
	},
	"steam": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".steam"},
	},
	"windows": {
		SystemID:   systemdefs.SystemWindows,
		Extensions: []string{".wine", ".exe", ".bat"},
	},
	"windows_installers": {
		SystemID:   systemdefs.SystemWindows,
		Extensions: []string{".exe", ".msi"},
	},
	"plugnplay": {
		SystemID:   systemdefs.SystemPlugNPlay,
		Extensions: []string{".game"},
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
	"3do":                "3DO",
	"3ds":                "3DS",
	"abuse":              "Abuse",
	"adam":               "Adam",
	"advision":           "AdVision",
	"amiga1200":          "Amiga1200",
	"amiga500":           "Amiga500",
	"amigacd32":          "AmigaCD32",
	"amigacdtv":          "AmigaCDTV",
	"amstradcpc":         "AmstradCPC",
	"apfm1000":           "APFM1000",
	"apple2":             "Apple2",
	"apple2gs":           "Apple2GS",
	"arcadia":            "Arcadia",
	"arcade":             "Arcade",
	"archimedes":         "Archimedes",
	"arduboy":            "Arduboy",
	"astrocde":           "Astrocde",
	"atari2600":          "Atari2600",
	"atari5200":          "Atari5200",
	"atari7800":          "Atari7800",
	"atari800":           "Atari800",
	"atarist":            "AtariST",
	"atom":               "Atom",
	"atomiswave":         "Atomiswave",
	"bbc":                "BBC",
	"bennugd":            "BennuGD",
	"bstone":             "BStone",
	"c128":               "C128",
	"c20":                "C20",
	"c64":                "C64",
	"camplynx":           "CampLynx",
	"cannonball":         "Cannonball",
	"catacomb":           "Catacomb",
	"cave3rd":            "Cave3rd",
	"cavestory":          "CaveStory",
	"cdi":                "CDI",
	"cdogs":              "CDogs",
	"cgenius":            "CGenius",
	"channelf":           "Channelf",
	"chihiro":            "Chihiro",
	"coco":               "CoCo",
	"colecovision":       "ColecoVision",
	"commanderx16":       "CommanderX16",
	"corsixth":           "CorsixTH",
	"cplus4":             "CPlus4",
	"crvision":           "CRVision",
	"daphne":             "Daphne",
	"devilutionx":        "DevilutionX",
	"dice":               "DICE",
	"doom3":              "Doom3",
	"dos":                "DOS",
	"dreamcast":          "Dreamcast",
	"dxx-rebirth":        "DXX-Rebirth",
	"easyrpg":            "EasyRPG",
	"ecwolf":             "ECWolf",
	"eduke32":            "EDuke32",
	"electron":           "Electron",
	"etlegacy":           "ETLegacy",
	"fallout1-ce":        "Fallout1CE",
	"fallout2-ce":        "Fallout2CE",
	"fbneo":              "FBNeo",
	"fds":                "FDS",
	"flash":              "Flash",
	"fm7":                "FM7",
	"fury":               "Fury",
	"gaelco":             "Gaelco",
	"fmtowns":            "FMTowns",
	"gamate":             "Gamate",
	"gameandwatch":       "GameAndWatch",
	"gamecom":            "GameCom",
	"gamecube":           "GameCube",
	"gamegear":           "GameGear",
	"gamepock":           "GamePock",
	"gb":                 "GB",
	"gb2players":         "GB2Players",
	"gba":                "GBA",
	"gbc":                "GBC",
	"gbc2players":        "GBC2Players",
	"gmaster":            "GMaster",
	"gong":               "Gong",
	"gp32":               "GP32",
	"gx4000":             "GX4000",
	"gzdoom":             "GZDoom",
	"hcl":                "HCL",
	"hikaru":             "Hikaru",
	"hurrican":           "Hurrican",
	"imageviewer":        "Image",
	"intellivision":      "Intellivision",
	"iortcw":             "IORTCW",
	"j2me":               "J2ME",
	"jaguar":             "Jaguar",
	"jaguarcd":           "JaguarCD",
	"jazz2":              "Jazz2",
	"jkdf2":              "JKDF2",
	"jknight":            "JKnight",
	"laser310":           "Laser310",
	"lcdgames":           "LCDGames",
	"library":            "Library",
	"lindbergh":          "Lindbergh",
	"lynx":               "Lynx",
	"macintosh":          "Macintosh",
	"mame":               "MAME",
	"mastersystem":       "MasterSystem",
	"megadrive":          "MegaDrive",
	"megaduck":           "MegaDuck",
	"mohaa":              "MoHAA",
	"model1":             "Model1",
	"model2":             "Model2",
	"model3":             "Model3",
	"moonlight":          "Moonlight",
	"mrboom":             "MrBoom",
	"msu-md":             "GenesisMSU",
	"msx1":               "MSX1",
	"msx2":               "MSX2",
	"msx2+":              "MSX2Plus",
	"msxturbor":          "MSXTurboR",
	"multivision":        "Multivision",
	"n64":                "N64",
	"n64dd":              "N64DD",
	"namco22":            "Namco22",
	"namco2x6":           "Namco2X6",
	"naomi":              "NAOMI",
	"naomi2":             "NAOMI2",
	"nds":                "NDS",
	"ngage":              "NGage",
	"neogeo":             "NeoGeo",
	"neogeocd":           "NeoGeoCD",
	"nes":                "NES",
	"ngp":                "NGP",
	"ngpc":               "NGPC",
	"o2em":               "O2EM",
	"odcommander":        "ODCommander",
	"openjazz":           "OpenJazz",
	"pc88":               "PC88",
	"pc98":               "PC98",
	"pcengine":           "PCEngine",
	"pcenginecd":         "PCEngineCD",
	"pcfx":               "PCFX",
	"pdp1":               "PDP1",
	"pet":                "PET",
	"pico":               "Pico",
	"pico8":              "Pico8",
	"pokemini":           "PokeMini",
	"ports":              "Ports",
	"psvita":             "PSVita",
	"ps3":                "PS3",
	"ps4":                "PS4",
	"psp":                "PSP",
	"psx":                "PSX",
	"ps2":                "PS2",
	"pv1000":             "PV1000",
	"samcoupe":           "SamCoupe",
	"satellaview":        "Satellaview",
	"saturn":             "Saturn",
	"scummvm":            "ScummVM",
	"scv":                "SCV",
	"sega32x":            "Sega32x",
	"megacd":             "MegaCD",
	"sg1000":             "SG1000",
	"sgb":                "SGB",
	"sgb-msu1":           "SGBMSU1",
	"singe":              "Singe",
	"snes":               "SNES",
	"socrates":           "Socrates",
	"spectravideo":       "Spectravideo",
	"sufami":             "Sufami",
	"snes-msu1":          "SNESMSU1",
	"supergrafx":         "SuperGrafx",
	"supracan":           "SuperACan",
	"supervision":        "SuperVision",
	"switch":             "Switch",
	"thomson":            "Thomson",
	"ti99":               "TI99",
	"triforce":           "Triforce",
	"tutor":              "Tutor",
	"videopacplus":       "VideopacPlus",
	"vc4000":             "VC4000",
	"vectrex":            "Vectrex",
	"virtualboy":         "VirtualBoy",
	"vsmile":             "VSmile",
	"wii":                "Wii",
	"wiiu":               "WiiU",
	"wswan":              "WSwan",
	"wswanc":             "WSwanC",
	"x1":                 "X1",
	"x68000":             "X68000",
	"xbox":               "Xbox",
	"xbox360":            "Xbox360",
	"xash3d_fwgs":        "Xash3D_FWGS",
	"xegs":               "XEGS",
	"xrick":              "XRick",
	"zc210":              "ZC210",
	"zx81":               "ZX81",
	"zxspectrum":         "ZXSpectrum",
	"ikemen":             "Ikemen",
	"lowresnx":           "LowResNX",
	"lutro":              "Lutro",
	"mugen":              "MUGEN",
	"openbor":            "OpenBOR",
	"oricatmos":          "OricAtmos",
	"prboom":             "PRBoom",
	"pygame":             "PyGame",
	"quake":              "Quake",
	"quake2":             "Quake2",
	"quake3":             "Quake3",
	"raze":               "Raze",
	"recordings":         "Recordings",
	"reminiscence":       "Reminiscence",
	"rott":               "ROTT",
	"sdlpop":             "SDLPoP",
	"solarus":            "Solarus",
	"sonic3-air":         "Sonic3AIR",
	"sonic-mania":        "SonicMania",
	"sonicretro":         "SonicRetro",
	"superbroswar":       "SuperBrosWar",
	"systemsp":           "SystemSP",
	"theforceengine":     "TheForceEngine",
	"thextech":           "TheXTech",
	"tic80":              "TIC80",
	"tyrquake":           "TyrQuake",
	"traider1":           "Traider1",
	"traider2":           "Traider2",
	"tyrian":             "Tyrian",
	"uqm":                "UQM",
	"uzebox":             "Uzebox",
	"vemulator":          "VEmulator",
	"vgmplay":            "VGMPlay",
	"wine":               "Wine",
	"pyxel":              "Pyxel",
	"vircon32":           "Vircon32",
	"vis":                "VIS",
	"vpinball":           "VPinball",
	"wasm4":              "Wasm4",
	"flatpak":            "Flatpak",
	"steam":              "Steam",
	"windows":            "Windows",
	"windows_installers": "WindowsInstallers",
	"plugnplay":          "PlugNPlay",
}
