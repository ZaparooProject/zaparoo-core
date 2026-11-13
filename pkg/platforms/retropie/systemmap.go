//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package retropie

import "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"

// requestKind says how the autostart loop starts a launch request.
type requestKind string

const (
	// requestSystem runs runcommand.sh with _SYS_, the way EmulationStation
	// launches a game, so the user's emulator choice for the system applies.
	requestSystem requestKind = "sys"
	// requestPort runs a ports script with bash, the way EmulationStation
	// launches the ports system.
	requestPort requestKind = "port"
)

type systemInfo struct {
	launcherID string
	systemID   string
	kind       requestKind
	extensions []string
}

// systemMap maps RetroPie system names, which are both the ROM folder names
// and the runcommand system names, to Zaparoo systems. Extensions come from
// RetroPie-Setup's platforms.cfg.
//
//nolint:gochecknoglobals // Static lookup table.
var systemMap = map[string]systemInfo{
	"3do": {
		launcherID: "3DO", systemID: systemdefs.System3DO, kind: requestSystem,
		extensions: []string{".chd", ".cue", ".iso", ".zip"},
	},
	"ags": {launcherID: "AGS", systemID: systemdefs.SystemPC, kind: requestSystem, extensions: []string{".exe"}},
	"amiga": {
		launcherID: "Amiga", systemID: systemdefs.SystemAmiga, kind: requestSystem,
		extensions: []string{
			".adf", ".adz", ".chd", ".cue", ".dms", ".hdf", ".hdz", ".ipf", ".lha", ".m3u", ".sh", ".uae",
			".zip",
		},
	},
	"amstradcpc": {
		launcherID: "AmstradCPC", systemID: systemdefs.SystemAmstrad, kind: requestSystem,
		extensions: []string{".cdt", ".cpc", ".cpr", ".dsk", ".m3u", ".tap", ".zip"},
	},
	"apple2": {
		launcherID: "Apple2", systemID: systemdefs.SystemAppleII, kind: requestSystem,
		extensions: []string{".dsk", ".nib", ".po"},
	},
	"arcade": {
		launcherID: "Arcade", systemID: systemdefs.SystemArcade, kind: requestSystem,
		extensions: []string{".7z", ".cue", ".fba", ".iso", ".zip"},
	},
	"arcadia": {
		launcherID: "Arcadia", systemID: systemdefs.SystemArcadia, kind: requestSystem,
		extensions: []string{".bin", ".zip"},
	},
	"atari2600": {
		launcherID: "Atari2600", systemID: systemdefs.SystemAtari2600, kind: requestSystem,
		extensions: []string{".7z", ".a26", ".bin", ".gz", ".rom", ".zip"},
	},
	"atari5200": {
		launcherID: "Atari5200", systemID: systemdefs.SystemAtari5200, kind: requestSystem,
		extensions: []string{".7z", ".a52", ".bin", ".car", ".zip"},
	},
	"atari7800": {
		launcherID: "Atari7800", systemID: systemdefs.SystemAtari7800, kind: requestSystem,
		extensions: []string{".7z", ".a78", ".bin", ".zip"},
	},
	"atari800": {
		launcherID: "Atari800", systemID: systemdefs.SystemAtari800, kind: requestSystem,
		extensions: []string{
			".7z", ".atr", ".atr.gz", ".atx", ".bas", ".bin", ".car", ".cas", ".com", ".dcm", ".rom", ".xex",
			".xfd", ".xfd.gz", ".zip",
		},
	},
	"atarijaguar": {
		launcherID: "AtariJaguar", systemID: systemdefs.SystemJaguar, kind: requestSystem,
		extensions: []string{".j64", ".jag", ".zip"},
	},
	"atarilynx": {
		launcherID: "AtariLynx", systemID: systemdefs.SystemAtariLynx, kind: requestSystem,
		extensions: []string{".7z", ".lnx", ".zip"},
	},
	"atarist": {
		launcherID: "AtariST", systemID: systemdefs.SystemAtariST, kind: requestSystem,
		extensions: []string{".ctr", ".img", ".ipf", ".m3u", ".raw", ".rom", ".st", ".stx", ".zip"},
	},
	"c64": {
		launcherID: "C64", systemID: systemdefs.SystemC64, kind: requestSystem,
		extensions: []string{
			".cmd", ".crt", ".d64", ".d71", ".d80", ".d81", ".g64", ".m3u", ".prg", ".t64", ".tap", ".vsf",
			".x64", ".zip",
		},
	},
	"channelf": {
		launcherID: "Channelf", systemID: systemdefs.SystemChannelF, kind: requestSystem,
		extensions: []string{".7z", ".bin", ".rom", ".zip"},
	},
	"coco": {
		launcherID: "CoCo", systemID: systemdefs.SystemCoCo2, kind: requestSystem,
		extensions: []string{
			".asc", ".bas", ".bin", ".cas", ".ccc", ".dmk", ".dsk", ".jvc", ".os9", ".rom", ".sna", ".vdk",
			".wav",
		},
	},
	"coleco": {
		launcherID: "Coleco", systemID: systemdefs.SystemColecoVision, kind: requestSystem,
		extensions: []string{".bin", ".col", ".rom", ".zip"},
	},
	"crvision": {
		launcherID: "CRVision", systemID: systemdefs.SystemCreatiVision, kind: requestSystem,
		extensions: []string{".rom", ".zip"},
	},
	"daphne": {
		launcherID: "Daphne", systemID: systemdefs.SystemDAPHNE, kind: requestSystem,
		extensions: []string{".daphne", ".ogv"},
	},
	"dragon32": {
		launcherID: "Dragon32", systemID: systemdefs.SystemDragon32, kind: requestSystem,
		extensions: []string{
			".asc", ".bas", ".bin", ".cas", ".ccc", ".dmk", ".dsk", ".jvc", ".os9", ".rom", ".sna", ".vdk",
			".wav",
		},
	},
	"dreamcast": {
		launcherID: "Dreamcast", systemID: systemdefs.SystemDreamcast, kind: requestSystem,
		extensions: []string{".cdi", ".chd", ".cue", ".gdi", ".m3u", ".sh", ".zip"},
	},
	"enterprise": {
		launcherID: "Enterprise", systemID: systemdefs.SystemEnterprise, kind: requestSystem,
		extensions: []string{".128", ".bas", ".com", ".dsk", ".dtf", ".img", ".tap", ".trn", ".wav"},
	},
	"fba": {
		launcherID: "FBA", systemID: systemdefs.SystemArcade, kind: requestSystem,
		extensions: []string{".7z", ".cue", ".fba", ".iso", ".zip"},
	},
	"fds": {
		launcherID: "FDS", systemID: systemdefs.SystemFDS, kind: requestSystem,
		extensions: []string{".7z", ".fds", ".nes", ".zip"},
	},
	"fm7": {
		launcherID: "FM7", systemID: systemdefs.SystemFM7, kind: requestSystem,
		extensions: []string{".2d", ".d77", ".d88", ".t77"},
	},
	"gameandwatch": {
		launcherID: "GameAndWatch", systemID: systemdefs.SystemGameNWatch, kind: requestSystem,
		extensions: []string{".mgw"},
	},
	"gamegear": {
		launcherID: "GameGear", systemID: systemdefs.SystemGameGear, kind: requestSystem,
		extensions: []string{".7z", ".bin", ".gg", ".sms", ".zip"},
	},
	"gba": {
		launcherID: "GBA", systemID: systemdefs.SystemGBA, kind: requestSystem,
		extensions: []string{".7z", ".gba", ".zip"},
	},
	"gb": {
		launcherID: "GB", systemID: systemdefs.SystemGameboy, kind: requestSystem,
		extensions: []string{".7z", ".gb", ".zip"},
	},
	"gbc": {
		launcherID: "GBC", systemID: systemdefs.SystemGameboyColor, kind: requestSystem,
		extensions: []string{".7z", ".gbc", ".zip"},
	},
	"gc": {
		launcherID: "GameCube", systemID: systemdefs.SystemGameCube, kind: requestSystem,
		extensions: []string{".ciso", ".gcm", ".gcz", ".iso", ".m3u", ".rvz"},
	},
	"intellivision": {
		launcherID: "Intellivision", systemID: systemdefs.SystemIntellivision, kind: requestSystem,
		extensions: []string{".7z", ".bin", ".int", ".itv", ".rom", ".zip"},
	},
	"love": {launcherID: "LOVE", systemID: systemdefs.SystemPC, kind: requestSystem, extensions: []string{".love"}},
	"macintosh": {
		launcherID: "Macintosh", systemID: systemdefs.SystemMacOS, kind: requestSystem,
		extensions: []string{".dsk", ".txt"},
	},
	"mame": {
		launcherID: "MAME", systemID: systemdefs.SystemArcade, kind: requestSystem,
		extensions: []string{".7z", ".zip"},
	},
	"mame-advmame": {
		launcherID: "MAMEAdvMAME", systemID: systemdefs.SystemArcade, kind: requestSystem,
		extensions: []string{".zip"},
	},
	"mame-libretro": {
		launcherID: "MAMELibretro", systemID: systemdefs.SystemArcade, kind: requestSystem,
		extensions: []string{".zip"},
	},
	"mame-mame4all": {
		launcherID: "MAME4All", systemID: systemdefs.SystemArcade, kind: requestSystem,
		extensions: []string{".zip"},
	},
	"mastersystem": {
		launcherID: "MasterSystem", systemID: systemdefs.SystemMasterSystem, kind: requestSystem,
		extensions: []string{".7z", ".bin", ".sms", ".zip"},
	},
	"megadrive": {
		launcherID: "MegaDrive", systemID: systemdefs.SystemGenesis, kind: requestSystem,
		extensions: []string{".7z", ".bin", ".gen", ".md", ".sg", ".smd", ".zip"},
	},
	"msx": {
		launcherID: "MSX", systemID: systemdefs.SystemMSX, kind: requestSystem,
		extensions: []string{".cas", ".col", ".dsk", ".m3u", ".mx1", ".mx2", ".rom", ".zip"},
	},
	"moto": {
		launcherID: "Thomson", systemID: systemdefs.SystemThomson, kind: requestSystem,
		extensions: []string{".fd", ".k7", ".m5", ".m7", ".rom", ".sap"},
	},
	"n64": {
		launcherID: "N64", systemID: systemdefs.SystemNintendo64, kind: requestSystem,
		extensions: []string{".n64", ".v64", ".z64", ".zip"},
	},
	"nds": {
		launcherID: "NDS", systemID: systemdefs.SystemNDS, kind: requestSystem,
		extensions: []string{".nds", ".zip"},
	},
	"neogeo": {
		launcherID: "NeoGeo", systemID: systemdefs.SystemNeoGeo, kind: requestSystem,
		extensions: []string{".7z", ".chd", ".cue", ".fba", ".iso", ".zip"},
	},
	"nes": {
		launcherID: "NES", systemID: systemdefs.SystemNES, kind: requestSystem,
		extensions: []string{".7z", ".nes", ".zip"},
	},
	"ngp": {
		launcherID: "NGP", systemID: systemdefs.SystemNeoGeoPocket, kind: requestSystem,
		extensions: []string{".7z", ".ngp", ".zip"},
	},
	"ngpc": {
		launcherID: "NGPC", systemID: systemdefs.SystemNeoGeoPocketColor, kind: requestSystem,
		extensions: []string{".7z", ".ngc", ".zip"},
	},
	"np2pi": {
		launcherID: "NP2Pi", systemID: systemdefs.SystemPC98, kind: requestSystem,
		extensions: []string{
			".2hd", ".88d", ".98d", ".d88", ".d98", ".dup", ".fdi", ".hdd", ".hdi", ".hdm", ".nhd", ".tfd",
			".thd", ".xdf",
		},
	},
	"oric": {
		launcherID: "Oric", systemID: systemdefs.SystemOric, kind: requestSystem,
		extensions: []string{".dsk", ".tap"},
	},
	"pc88": {
		launcherID: "PC88", systemID: systemdefs.SystemPC88, kind: requestSystem,
		extensions: []string{".cmt", ".d88", ".m3u", ".t88"},
	},
	"pc98": {
		launcherID: "PC98", systemID: systemdefs.SystemPC98, kind: requestSystem,
		extensions: []string{
			".2hd", ".88d", ".98d", ".cmd", ".d88", ".d98", ".dup", ".fdd", ".fdi", ".hdd", ".hdi", ".hdm",
			".nhd", ".tfd", ".thd", ".xdf", ".zip",
		},
	},
	"pc": {
		launcherID: "DOS", systemID: systemdefs.SystemDOS, kind: requestSystem,
		extensions: []string{
			".bat", ".com", ".conf", ".cue", ".dosz", ".exe", ".ima", ".img", ".ins", ".iso", ".m3u", ".m3u8",
			".sh", ".vhd", ".zip",
		},
	},
	"pcengine": {
		launcherID: "PCEngine", systemID: systemdefs.SystemTurboGrafx16, kind: requestSystem,
		extensions: []string{".7z", ".ccd", ".chd", ".cue", ".pce", ".zip"},
	},
	"pcfx": {
		launcherID: "PCFX", systemID: systemdefs.SystemPCFX, kind: requestSystem,
		extensions: []string{".ccd", ".chd", ".cue", ".iso", ".pce"},
	},
	"pico8": {
		launcherID: "Pico8", systemID: systemdefs.SystemPico8, kind: requestSystem,
		extensions: []string{".p8", ".png", ".zip"},
	},
	"pokemini": {
		launcherID: "PokeMini", systemID: systemdefs.SystemPokemonMini, kind: requestSystem,
		extensions: []string{".min", ".zip"},
	},
	"ports": {launcherID: "Ports", systemID: systemdefs.SystemPC, kind: requestPort, extensions: []string{".sh"}},
	"ps2": {
		launcherID: "PS2", systemID: systemdefs.SystemPS2, kind: requestSystem,
		extensions: []string{
			".bin", ".bz2", ".chd", ".cso", ".dump", ".gz", ".ima", ".img", ".iso", ".mdf", ".z", ".z2",
		},
	},
	"psp": {
		launcherID: "PSP", systemID: systemdefs.SystemPSP, kind: requestSystem,
		extensions: []string{".chd", ".cso", ".iso", ".pbp"},
	},
	"psx": {
		launcherID: "PSX", systemID: systemdefs.SystemPSX, kind: requestSystem,
		extensions: []string{".cbn", ".chd", ".cue", ".img", ".iso", ".m3u", ".mdf", ".pbp", ".toc", ".z", ".znx"},
	},
	"samcoupe": {
		launcherID: "SamCoupe", systemID: systemdefs.SystemSAMCoupe, kind: requestSystem,
		extensions: []string{".dsk", ".mgt", ".sad", ".sbt"},
	},
	"saturn": {
		launcherID: "Saturn", systemID: systemdefs.SystemSaturn, kind: requestSystem,
		extensions: []string{".chd", ".cue", ".m3u", ".mdf", ".zip"},
	},
	"scummvm": {
		launcherID: "ScummVM", systemID: systemdefs.SystemScummVM, kind: requestSystem,
		extensions: []string{".sh", ".svm"},
	},
	"sega32x": {
		launcherID: "Sega32x", systemID: systemdefs.SystemSega32X, kind: requestSystem,
		extensions: []string{".32x", ".7z", ".bin", ".md", ".smd", ".zip"},
	},
	"segacd": {
		launcherID: "SegaCD", systemID: systemdefs.SystemMegaCD, kind: requestSystem,
		extensions: []string{".chd", ".cue", ".iso", ".m3u"},
	},
	"sg-1000": {
		launcherID: "SG1000", systemID: systemdefs.SystemSG1000, kind: requestSystem,
		extensions: []string{".7z", ".bin", ".sg", ".zip"},
	},
	"snes": {
		launcherID: "SNES", systemID: systemdefs.SystemSNES, kind: requestSystem,
		extensions: []string{".7z", ".bin", ".bs", ".fig", ".mgd", ".sfc", ".smc", ".swc", ".zip"},
	},
	"solarus": {
		launcherID: "Solarus", systemID: systemdefs.SystemPC, kind: requestSystem,
		extensions: []string{".solarus", ".zip"},
	},
	"ti99": {launcherID: "TI99", systemID: systemdefs.SystemTI994A, kind: requestSystem, extensions: []string{".ctg"}},
	"tic80": {
		launcherID: "TIC80", systemID: systemdefs.SystemTIC80, kind: requestSystem,
		extensions: []string{".tic", ".zip"},
	},
	"trs-80": {
		launcherID: "TRS80", systemID: systemdefs.SystemTRS80, kind: requestSystem,
		extensions: []string{".dsk"},
	},
	"tvc": {
		launcherID: "TVC", systemID: systemdefs.SystemTVC, kind: requestSystem,
		extensions: []string{".cas", ".dsk", ".img", ".tvcwav"},
	},
	"vectrex": {
		launcherID: "Vectrex", systemID: systemdefs.SystemVectrex, kind: requestSystem,
		extensions: []string{".7z", ".bin", ".gam", ".vec", ".zip"},
	},
	"videopac": {
		launcherID: "Videopac", systemID: systemdefs.SystemOdyssey2, kind: requestSystem,
		extensions: []string{".7z", ".bin", ".zip"},
	},
	"virtualboy": {
		launcherID: "VirtualBoy", systemID: systemdefs.SystemVirtualBoy, kind: requestSystem,
		extensions: []string{".7z", ".vb", ".zip"},
	},
	"wii": {
		launcherID: "Wii", systemID: systemdefs.SystemWii, kind: requestSystem,
		extensions: []string{".ciso", ".gcm", ".gcz", ".iso", ".m3u", ".rvz", ".wad", ".wbfs"},
	},
	"wonderswan": {
		launcherID: "WonderSwan", systemID: systemdefs.SystemWonderSwan, kind: requestSystem,
		extensions: []string{".7z", ".ws", ".zip"},
	},
	"wonderswancolor": {
		launcherID: "WonderSwanColor", systemID: systemdefs.SystemWonderSwanColor, kind: requestSystem,
		extensions: []string{".7z", ".wsc", ".zip"},
	},
	"x1": {
		launcherID: "X1", systemID: systemdefs.SystemX1, kind: requestSystem,
		extensions: []string{".2d", ".2hd", ".88d", ".cmd", ".d88", ".dup", ".dx1", ".hdm", ".tfd", ".xdf", ".zip"},
	},
	"x68000": {
		launcherID: "X68000", systemID: systemdefs.SystemX68000, kind: requestSystem,
		extensions: []string{".2hd", ".d88", ".dim", ".hdf", ".hdm", ".m3u", ".xdf"},
	},
	"zmachine": {
		launcherID: "ZMachine", systemID: systemdefs.SystemZMachine, kind: requestSystem,
		extensions: []string{".dat", ".z1", ".z2", ".z3", ".z4", ".z5", ".z6", ".z7", ".z8", ".zip"},
	},
	"zx81": {
		launcherID: "ZX81", systemID: systemdefs.SystemZX81, kind: requestSystem,
		extensions: []string{".7z", ".p", ".t81", ".tzx", ".zip"},
	},
	"zxspectrum": {
		launcherID: "ZXSpectrum", systemID: systemdefs.SystemZXSpectrum, kind: requestSystem,
		extensions: []string{
			".7z", ".dsk", ".gz", ".img", ".mgt", ".rzx", ".scl", ".sh", ".sna", ".szx", ".tap", ".trd",
			".tzx", ".udi", ".z80", ".zip",
		},
	},
}
