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

// Package esde provides shared utilities for EmulationStation Desktop Edition
// based platforms including ES-DE, Batocera ES, and RetroBat.
package esde

import (
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

// SystemInfo maps EmulationStation folder names to Zaparoo system information.
type SystemInfo struct {
	SystemID   string
	LauncherID string
	Extensions []string
}

// GetLauncherID returns the launcher ID, falling back to SystemID if not set.
func (s SystemInfo) GetLauncherID() string {
	if s.LauncherID != "" {
		return s.LauncherID
	}
	return s.SystemID
}

// SystemMap contains the consolidated system mappings for all EmulationStation-based
// platforms. This map uses the ES folder name as the key and maps to system info.
//
//nolint:gochecknoglobals // Package-level configuration
var SystemMap = map[string]SystemInfo{
	"3do": {
		SystemID:   systemdefs.System3DO,
		Extensions: []string{".iso", ".chd", ".cue"},
		LauncherID: "3DO",
	},
	"3ds": {
		SystemID:   systemdefs.System3DS,
		Extensions: []string{".3ds", ".cci", ".cxi"},
		LauncherID: "3DS",
	},
	"abuse": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".game"},
		LauncherID: "Abuse",
	},
	"adam": {
		SystemID: systemdefs.SystemColecoAdam,
		Extensions: []string{
			".wav", ".ddp", ".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".d77", ".d88",
			".1dd", ".cqm", ".cqi", ".dsk", ".rom", ".col", ".bin", ".zip", ".7z",
		},
		LauncherID: "Adam",
	},
	"advision": {
		SystemID:   systemdefs.SystemAdventureVision,
		Extensions: []string{".bin", ".zip", ".7z"},
		LauncherID: "AdVision",
	},
	"amiga1200": {
		SystemID:   systemdefs.SystemAmiga1200,
		Extensions: []string{".adf", ".uae", ".ipf", ".dms", ".dmz", ".adz", ".lha", ".hdf", ".exe", ".m3u", ".zip"},
		LauncherID: "Amiga1200",
	},
	"amiga500": {
		SystemID:   systemdefs.SystemAmiga500,
		Extensions: []string{".adf", ".uae", ".ipf", ".dms", ".dmz", ".adz", ".lha", ".hdf", ".exe", ".m3u", ".zip"},
		LauncherID: "Amiga500",
	},
	"amigacd32": {
		SystemID:   systemdefs.SystemAmigaCD32,
		Extensions: []string{".bin", ".cue", ".iso", ".chd"},
		LauncherID: "AmigaCD32",
	},
	"amigacdtv": {
		SystemID:   systemdefs.SystemAmiga,
		Extensions: []string{".bin", ".cue", ".iso", ".chd", ".m3u"},
		LauncherID: "AmigaCDTV",
	},
	"amstradcpc": {
		SystemID:   systemdefs.SystemAmstrad,
		Extensions: []string{".dsk", ".sna", ".tap", ".cdt", ".voc", ".m3u", ".zip", ".7z"},
		LauncherID: "AmstradCPC",
	},
	"apfm1000": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".bin", ".zip", ".7z"},
		LauncherID: "APFM1000",
	},
	"apple2": {
		SystemID: systemdefs.SystemAppleII,
		Extensions: []string{
			".nib", ".do", ".po", ".dsk", ".mfi", ".dfi", ".rti", ".edd", ".woz", ".wav", ".zip", ".7z",
		},
		LauncherID: "Apple2",
	},
	"apple2gs": {
		SystemID: systemdefs.SystemAppleII,
		Extensions: []string{
			".nib", ".do", ".po", ".dsk", ".mfi", ".dfi", ".rti", ".edd", ".woz", ".wav", ".zip", ".7z",
		},
		LauncherID: "Apple2GS",
	},
	"aquarius": {
		SystemID:   systemdefs.SystemAquarius,
		Extensions: []string{".bin", ".caq", ".zip", ".7z"},
		LauncherID: "Aquarius",
	},
	"arcadia": {
		SystemID:   systemdefs.SystemArcadia,
		Extensions: []string{".bin", ".zip", ".7z"},
		LauncherID: "Arcadia",
	},
	"arcade": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".zip", ".7z"},
		LauncherID: "Arcade",
	},
	"archimedes": {
		SystemID: systemdefs.SystemArchimedes,
		Extensions: []string{
			".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".d77", ".d88", ".1dd", ".cqm", ".cqi",
			".dsk", ".ima", ".img", ".ufi", ".360", ".ipf", ".adf", ".apd", ".jfd", ".ads", ".adm",
			".adl", ".ssd", ".bbc", ".dsd", ".st", ".msa", ".chd", ".zip", ".7z",
		},
		LauncherID: "Archimedes",
	},
	"arduboy": {
		SystemID:   systemdefs.SystemArduboy,
		Extensions: []string{".hex", ".zip", ".7z"},
		LauncherID: "Arduboy",
	},
	"astrocde": {
		SystemID:   systemdefs.SystemAstrocade,
		Extensions: []string{".bin", ".zip", ".7z"},
		LauncherID: "Astrocde",
	},
	"atari2600": {
		SystemID:   systemdefs.SystemAtari2600,
		Extensions: []string{".a26", ".bin", ".zip", ".7z"},
		LauncherID: "Atari2600",
	},
	"atari5200": {
		SystemID: systemdefs.SystemAtari5200,
		Extensions: []string{
			".rom", ".xfd", ".atr", ".atx", ".cdm", ".cas", ".car", ".bin", ".a52", ".xex", ".zip", ".7z",
		},
		LauncherID: "Atari5200",
	},
	"atari7800": {
		SystemID:   systemdefs.SystemAtari7800,
		Extensions: []string{".a78", ".bin", ".zip", ".7z"},
		LauncherID: "Atari7800",
	},
	"atari800": {
		SystemID: systemdefs.SystemAtari800,
		Extensions: []string{
			".rom", ".xfd", ".atr", ".atx", ".cdm", ".cas", ".car", ".bin", ".a52", ".xex", ".zip", ".7z",
		},
		LauncherID: "Atari800",
	},
	"atarist": {
		SystemID:   systemdefs.SystemAtariST,
		Extensions: []string{".st", ".msa", ".stx", ".dim", ".ipf", ".m3u", ".zip", ".7z"},
		LauncherID: "AtariST",
	},
	"atom": {
		SystemID: systemdefs.SystemAcornAtom,
		Extensions: []string{
			".wav", ".tap", ".csw", ".uef", ".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".d77",
			".d88", ".1dd", ".cqm", ".cqi", ".dsk", ".40t", ".atm", ".bin", ".rom", ".zip", ".7z",
		},
		LauncherID: "Atom",
	},
	"atomiswave": {
		SystemID:   systemdefs.SystemAtomiswave,
		Extensions: []string{".lst", ".bin", ".dat", ".zip", ".7z"},
		LauncherID: "Atomiswave",
	},
	"bbc": {
		SystemID: systemdefs.SystemBBCMicro,
		Extensions: []string{
			".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".d77", ".d88", ".1dd", ".cqm", ".cqi",
			".dsk", ".ima", ".img", ".ufi", ".360", ".ipf", ".ssd", ".bbc", ".dsd", ".adf", ".ads",
			".adm", ".adl", ".fsd", ".wav", ".tap", ".bin", ".zip", ".7z",
		},
		LauncherID: "BBC",
	},
	"bennugd": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".zip", ".7z"},
		LauncherID: "BennuGD",
	},
	"bstone": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".bstone"},
		LauncherID: "BStone",
	},
	"c128": {
		SystemID:   systemdefs.SystemC64,
		Extensions: []string{".d64", ".d81", ".prg", ".lnx", ".m3u", ".zip", ".7z"},
		LauncherID: "C128",
	},
	"c20": {
		SystemID:   systemdefs.SystemVIC20,
		Extensions: []string{".a0", ".b0", ".crt", ".d64", ".d81", ".prg", ".tap", ".t64", ".m3u", ".zip", ".7z"},
		LauncherID: "C20",
	},
	"c64": {
		SystemID:   systemdefs.SystemC64,
		Extensions: []string{".d64", ".d81", ".crt", ".prg", ".tap", ".t64", ".m3u", ".zip", ".7z"},
		LauncherID: "C64",
	},
	"camplynx": {
		SystemID:   systemdefs.SystemLynx48,
		Extensions: []string{".wav", ".tap", ".zip", ".7z"},
		LauncherID: "CampLynx",
	},
	"cannonball": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".cannonball"},
		LauncherID: "Cannonball",
	},
	"catacomb": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".game"},
		LauncherID: "Catacomb",
	},
	"cave3rd": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".zip", ".7z"},
		LauncherID: "Cave3rd",
	},
	"cavestory": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".exe"},
		LauncherID: "CaveStory",
	},
	"cdi": {
		SystemID:   systemdefs.SystemCDI,
		Extensions: []string{".chd", ".cue", ".toc", ".nrg", ".gdi", ".iso", ".cdr"},
		LauncherID: "CDI",
	},
	"cdogs": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".game"},
		LauncherID: "CDogs",
	},
	"cgenius": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".zip", ".7z"},
		LauncherID: "CGenius",
	},
	"channelf": {
		SystemID:   systemdefs.SystemChannelF,
		Extensions: []string{".zip", ".rom", ".bin", ".chf"},
		LauncherID: "Channelf",
	},
	"chihiro": {
		SystemID:   systemdefs.SystemChihiro,
		Extensions: []string{".chd"},
		LauncherID: "Chihiro",
	},
	"coco": {
		SystemID:   systemdefs.SystemCoCo2,
		Extensions: []string{".wav", ".cas", ".ccc", ".rom", ".zip", ".7z"},
		LauncherID: "CoCo",
	},
	"colecovision": {
		SystemID:   systemdefs.SystemColecoVision,
		Extensions: []string{".bin", ".col", ".rom", ".zip", ".7z"},
		LauncherID: "ColecoVision",
	},
	"commanderx16": {
		SystemID:   systemdefs.SystemCommanderX16,
		Extensions: []string{".prg", ".crt", ".bin", ".zip"},
		LauncherID: "CommanderX16",
	},
	"corsixth": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".game"},
		LauncherID: "CorsixTH",
	},
	"cplus4": {
		SystemID:   systemdefs.SystemC16,
		Extensions: []string{".d64", ".prg", ".tap", ".m3u", ".zip", ".7z"},
		LauncherID: "CPlus4",
	},
	"cps1": {
		SystemID:   systemdefs.SystemCPS1,
		Extensions: []string{".zip", ".7z"},
		LauncherID: "CPS1",
	},
	"cps2": {
		SystemID:   systemdefs.SystemCPS2,
		Extensions: []string{".zip", ".7z"},
		LauncherID: "CPS2",
	},
	"cps3": {
		SystemID:   systemdefs.SystemCPS3,
		Extensions: []string{".zip", ".7z"},
		LauncherID: "CPS3",
	},
	"crvision": {
		SystemID:   systemdefs.SystemCreatiVision,
		Extensions: []string{".bin", ".rom", ".zip", ".7z"},
		LauncherID: "CRVision",
	},
	"daphne": {
		SystemID:   systemdefs.SystemDAPHNE,
		Extensions: []string{".daphne", ".squashfs"},
		LauncherID: "Daphne",
	},
	"devilutionx": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".mpq"},
		LauncherID: "DevilutionX",
	},
	"dice": {
		SystemID:   systemdefs.SystemDICE,
		Extensions: []string{".zip", ".dmy"},
		LauncherID: "DICE",
	},
	"doom3": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".d3"},
		LauncherID: "Doom3",
	},
	"dos": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".pc", ".dos", ".zip", ".squashfs", ".dosz", ".m3u", ".iso", ".cue"},
		LauncherID: "DOS",
	},
	"dreamcast": {
		SystemID:   systemdefs.SystemDreamcast,
		Extensions: []string{".cdi", ".cue", ".gdi", ".chd", ".m3u"},
		LauncherID: "Dreamcast",
	},
	"dxx-rebirth": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".d1x", ".d2x"},
		LauncherID: "DXX-Rebirth",
	},
	"easyrpg": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".easyrpg", ".squashfs", ".zip"},
		LauncherID: "EasyRPG",
	},
	"ecwolf": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".ecwolf", ".pk3", ".squashfs"},
		LauncherID: "ECWolf",
	},
	"eduke32": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".eduke32"},
		LauncherID: "EDuke32",
	},
	"electron": {
		SystemID: systemdefs.SystemAcornElectron,
		Extensions: []string{
			".wav", ".csw", ".uef", ".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".d77", ".d88",
			".1dd", ".cqm", ".cqi", ".dsk", ".ssd", ".bbc", ".img", ".dsd", ".adf", ".ads", ".adm",
			".adl", ".rom", ".bin", ".zip", ".7z",
		},
		LauncherID: "Electron",
	},
	"etlegacy": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".etlegacy"},
		LauncherID: "ETLegacy",
	},
	"fallout1-ce": {
		SystemID:   systemdefs.SystemWindows,
		Extensions: []string{".f1ce"},
		LauncherID: "Fallout1CE",
	},
	"fallout2-ce": {
		SystemID:   systemdefs.SystemWindows,
		Extensions: []string{".f2ce"},
		LauncherID: "Fallout2CE",
	},
	"fbneo": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".zip", ".7z"},
		LauncherID: "FBNeo",
	},
	"fds": {
		SystemID:   systemdefs.SystemFDS,
		Extensions: []string{".fds", ".zip", ".7z"},
		LauncherID: "FDS",
	},
	"flash": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".swf"},
		LauncherID: "Flash",
	},
	"flatpak": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".flatpak"},
		LauncherID: "Flatpak",
	},
	"fm7": {
		SystemID: systemdefs.SystemFM7,
		Extensions: []string{
			".wav", ".t77", ".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".d77", ".d88",
			".1dd", ".cqm", ".cqi", ".dsk", ".zip", ".7z",
		},
		LauncherID: "FM7",
	},
	"fmtowns": {
		SystemID: systemdefs.SystemFMTowns,
		Extensions: []string{
			".bin", ".m3u", ".cue", ".d88", ".d77", ".xdf", ".iso", ".chd", ".toc", ".nrg", ".gdi",
			".cdr", ".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".1dd", ".cqm", ".cqi", ".dsk",
			".zip", ".7z",
		},
		LauncherID: "FMTowns",
	},
	"fury": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".grp"},
		LauncherID: "Fury",
	},
	"gaelco": {
		SystemID:   systemdefs.SystemGaelco,
		Extensions: []string{".zip"},
		LauncherID: "Gaelco",
	},
	"gamate": {
		SystemID:   systemdefs.SystemGamate,
		Extensions: []string{".bin", ".zip", ".7z"},
		LauncherID: "Gamate",
	},
	"gameandwatch": {
		SystemID:   systemdefs.SystemGameNWatch,
		Extensions: []string{".mgw", ".zip", ".7z"},
		LauncherID: "GameAndWatch",
	},
	"gamecom": {
		SystemID:   systemdefs.SystemGameCom,
		Extensions: []string{".bin", ".tgc", ".zip", ".7z"},
		LauncherID: "GameCom",
	},
	"gamecube": {
		SystemID:   systemdefs.SystemGameCube,
		Extensions: []string{".gcm", ".iso", ".gcz", ".ciso", ".wbfs", ".rvz", ".elf", ".dol", ".m3u"},
		LauncherID: "GameCube",
	},
	"gamegear": {
		SystemID:   systemdefs.SystemGameGear,
		Extensions: []string{".bin", ".gg", ".zip", ".7z"},
		LauncherID: "GameGear",
	},
	"gamepock": {
		SystemID:   systemdefs.SystemGamePocket,
		Extensions: []string{".bin", ".zip", ".7z"},
		LauncherID: "GamePock",
	},
	"gb": {
		SystemID:   systemdefs.SystemGameboy,
		Extensions: []string{".gb", ".zip", ".7z"},
		LauncherID: "GB",
	},
	"gb2players": {
		SystemID:   systemdefs.SystemGameboy2P,
		Extensions: []string{".gb", ".gb2", ".gbc2", ".zip", ".7z"},
		LauncherID: "GB2Players",
	},
	"gb-msu": {
		SystemID:   systemdefs.SystemSGBMSU1,
		Extensions: []string{".gb", ".gbc", ".zip", ".7z"},
		LauncherID: "GBMSU",
	},
	"gba": {
		SystemID:   systemdefs.SystemGBA,
		Extensions: []string{".gba", ".zip", ".7z"},
		LauncherID: "GBA",
	},
	"gba2players": {
		SystemID:   systemdefs.SystemGBA2P,
		Extensions: []string{".gba", ".zip", ".7z"},
		LauncherID: "GBA2Players",
	},
	"gbc": {
		SystemID:   systemdefs.SystemGameboyColor,
		Extensions: []string{".gbc", ".zip", ".7z"},
		LauncherID: "GBC",
	},
	"gbc2players": {
		SystemID:   systemdefs.SystemGameboy2P,
		Extensions: []string{".gbc", ".gb2", ".gbc2", ".zip", ".7z"},
		LauncherID: "GBC2Players",
	},
	"gmaster": {
		SystemID:   systemdefs.SystemGameMaster,
		Extensions: []string{".bin", ".zip", ".7z"},
		LauncherID: "GMaster",
	},
	"gong": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".game"},
		LauncherID: "Gong",
	},
	"gp32": {
		SystemID:   systemdefs.SystemGP32,
		Extensions: []string{".smc", ".zip", ".7z"},
		LauncherID: "GP32",
	},
	"gw": {
		SystemID:   systemdefs.SystemGameNWatch,
		Extensions: []string{".mgw", ".zip", ".7z"},
		LauncherID: "GW",
	},
	"gx4000": {
		SystemID:   systemdefs.SystemAmstrad,
		Extensions: []string{".dsk", ".m3u", ".cpr", ".zip", ".7z"},
		LauncherID: "GX4000",
	},
	"gzdoom": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".wad", ".iwad", ".pwad", ".gzdoom"},
		LauncherID: "GZDoom",
	},
	"hcl": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".game"},
		LauncherID: "HCL",
	},
	"hikaru": {
		SystemID:   systemdefs.SystemHikaru,
		Extensions: []string{".chd", ".zip"},
		LauncherID: "Hikaru",
	},
	"hurrican": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".game"},
		LauncherID: "Hurrican",
	},
	"ikemen": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".ikemen"},
		LauncherID: "Ikemen",
	},
	"imageviewer": {
		SystemID:   systemdefs.SystemImage,
		Extensions: []string{".jpg", ".png", ".gif", ".bmp"},
		LauncherID: "Image",
	},
	"intellivision": {
		SystemID:   systemdefs.SystemIntellivision,
		Extensions: []string{".int", ".bin", ".rom", ".zip", ".7z"},
		LauncherID: "Intellivision",
	},
	"iortcw": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".iortcw"},
		LauncherID: "IORTCW",
	},
	"j2me": {
		SystemID:   systemdefs.SystemJ2ME,
		Extensions: []string{".jar"},
		LauncherID: "J2ME",
	},
	"jaguar": {
		SystemID:   systemdefs.SystemJaguar,
		Extensions: []string{".cue", ".j64", ".jag", ".cof", ".abs", ".cdi", ".rom", ".zip", ".7z"},
		LauncherID: "Jaguar",
	},
	"jaguarcd": {
		SystemID:   systemdefs.SystemJaguarCD,
		Extensions: []string{".cue", ".chd"},
		LauncherID: "JaguarCD",
	},
	"jazz2": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".jazz2"},
		LauncherID: "Jazz2",
	},
	"jkdf2": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".jkdf2"},
		LauncherID: "JKDF2",
	},
	"jknight": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".jknight"},
		LauncherID: "JKnight",
	},
	"laser310": {
		SystemID:   systemdefs.SystemLaser,
		Extensions: []string{".vz", ".wav", ".cas", ".zip", ".7z"},
		LauncherID: "Laser310",
	},
	"lcdgames": {
		SystemID:   systemdefs.SystemGameNWatch,
		Extensions: []string{".mgw", ".zip", ".7z"},
		LauncherID: "LCDGames",
	},
	"library": {
		SystemID: systemdefs.SystemPC,
		Extensions: []string{
			".jpg", ".jpeg", ".png", ".bmp", ".psd", ".tga", ".gif", ".hdr", ".pic", ".ppm", ".pgm",
			".mkv", ".pdf", ".mp4", ".avi", ".webm", ".cbz", ".mp3", ".wav", ".ogg", ".flac",
			".mod", ".xm", ".stm", ".s3m", ".far", ".it", ".669", ".mtm",
		},
		LauncherID: "Library",
	},
	"lindbergh": {
		SystemID:   systemdefs.SystemLindbergh,
		Extensions: []string{".zip"},
		LauncherID: "Lindbergh",
	},
	"lowresnx": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".nx"},
		LauncherID: "LowResNX",
	},
	"lutro": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".lua", ".lutro"},
		LauncherID: "Lutro",
	},
	"lynx": {
		SystemID:   systemdefs.SystemAtariLynx,
		Extensions: []string{".lnx", ".zip", ".7z"},
		LauncherID: "Lynx",
	},
	"macintosh": {
		SystemID: systemdefs.SystemMacOS,
		Extensions: []string{
			".dsk", ".zip", ".7z", ".mfi", ".dfi", ".hfe", ".mfm", ".td0", ".imd", ".d77", ".d88",
			".1dd", ".cqm", ".cqi", ".ima", ".img", ".ufi", ".ipf", ".dc42", ".woz", ".2mg", ".360",
			".chd", ".cue", ".toc", ".nrg", ".gdi", ".iso", ".cdr", ".hd", ".hdv", ".hdi",
		},
		LauncherID: "Macintosh",
	},
	"mame": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".zip", ".7z"},
		LauncherID: "MAME",
	},
	"mastersystem": {
		SystemID:   systemdefs.SystemMasterSystem,
		Extensions: []string{".bin", ".sms", ".zip", ".7z"},
		LauncherID: "MasterSystem",
	},
	"megacd": {
		SystemID:   systemdefs.SystemMegaCD,
		Extensions: []string{".cue", ".iso", ".chd", ".m3u"},
		LauncherID: "MegaCD",
	},
	"megadrive": {
		SystemID:   systemdefs.SystemGenesis,
		Extensions: []string{".bin", ".gen", ".md", ".sg", ".smd", ".zip", ".7z"},
		LauncherID: "MegaDrive",
	},
	"megaduck": {
		SystemID:   systemdefs.SystemMegaDuck,
		Extensions: []string{".bin", ".zip", ".7z"},
		LauncherID: "MegaDuck",
	},
	"model1": {
		SystemID:   systemdefs.SystemModel1,
		Extensions: []string{".zip"},
		LauncherID: "Model1",
	},
	"model2": {
		SystemID:   systemdefs.SystemModel2,
		Extensions: []string{".zip"},
		LauncherID: "Model2",
	},
	"model3": {
		SystemID:   systemdefs.SystemModel3,
		Extensions: []string{".zip"},
		LauncherID: "Model3",
	},
	"mohaa": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".mohaa"},
		LauncherID: "MoHAA",
	},
	"moonlight": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".moonlight"},
		LauncherID: "Moonlight",
	},
	"mrboom": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".libretro"},
		LauncherID: "MrBoom",
	},
	"msu-md": {
		SystemID:   systemdefs.SystemGenesisMSU,
		Extensions: []string{".msu", ".md"},
		LauncherID: "GenesisMSU",
	},
	"msx1": {
		SystemID:   systemdefs.SystemMSX,
		Extensions: []string{".dsk", ".mx1", ".rom", ".zip", ".7z", ".cas", ".m3u"},
		LauncherID: "MSX1",
	},
	"msx2": {
		SystemID:   systemdefs.SystemMSX,
		Extensions: []string{".dsk", ".mx2", ".rom", ".zip", ".7z", ".cas", ".m3u"},
		LauncherID: "MSX2",
	},
	"msx2+": {
		SystemID:   systemdefs.SystemMSX2Plus,
		Extensions: []string{".dsk", ".mx2", ".rom", ".zip", ".7z", ".cas", ".m3u"},
		LauncherID: "MSX2Plus",
	},
	"msxturbor": {
		SystemID:   systemdefs.SystemMSX,
		Extensions: []string{".dsk", ".mx2", ".rom", ".zip", ".7z"},
		LauncherID: "MSXTurboR",
	},
	"mugen": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".mugen"},
		LauncherID: "MUGEN",
	},
	"multivision": {
		SystemID:   systemdefs.SystemMultivision,
		Extensions: []string{".bin", ".gg", ".rom", ".sg", ".sms", ".zip"},
		LauncherID: "Multivision",
	},
	"n64": {
		SystemID:   systemdefs.SystemNintendo64,
		Extensions: []string{".z64", ".n64", ".v64", ".zip", ".7z"},
		LauncherID: "N64",
	},
	"n64dd": {
		SystemID:   systemdefs.SystemNintendo64,
		Extensions: []string{".z64", ".z64.ndd"},
		LauncherID: "N64DD",
	},
	"namco22": {
		SystemID:   systemdefs.SystemNamco22,
		Extensions: []string{".zip"},
		LauncherID: "Namco22",
	},
	"namco2x6": {
		SystemID:   systemdefs.SystemNamco2X6,
		Extensions: []string{".zip"},
		LauncherID: "Namco2X6",
	},
	"naomi": {
		SystemID:   systemdefs.SystemNAOMI,
		Extensions: []string{".lst", ".bin", ".dat", ".zip", ".7z"},
		LauncherID: "NAOMI",
	},
	"naomi2": {
		SystemID:   systemdefs.SystemNAOMI2,
		Extensions: []string{".zip", ".7z"},
		LauncherID: "NAOMI2",
	},
	"nds": {
		SystemID:   systemdefs.SystemNDS,
		Extensions: []string{".nds", ".bin", ".zip", ".7z"},
		LauncherID: "NDS",
	},
	"neogeo": {
		SystemID:   systemdefs.SystemNeoGeo,
		Extensions: []string{".7z", ".zip"},
		LauncherID: "NeoGeo",
	},
	"neogeocd": {
		SystemID:   systemdefs.SystemNeoGeoCD,
		Extensions: []string{".cue", ".iso", ".chd"},
		LauncherID: "NeoGeoCD",
	},
	"nes": {
		SystemID:   systemdefs.SystemNES,
		Extensions: []string{".nes", ".unif", ".unf", ".zip", ".7z"},
		LauncherID: "NES",
	},
	"ngage": {
		SystemID:   systemdefs.SystemNGage,
		Extensions: []string{".ngage", ".jar"},
		LauncherID: "NGage",
	},
	"ngp": {
		SystemID:   systemdefs.SystemNeoGeoPocket,
		Extensions: []string{".ngp", ".zip", ".7z"},
		LauncherID: "NGP",
	},
	"ngpc": {
		SystemID:   systemdefs.SystemNeoGeoPocketColor,
		Extensions: []string{".ngc", ".zip", ".7z"},
		LauncherID: "NGPC",
	},
	"o2em": {
		SystemID:   systemdefs.SystemOdyssey2,
		Extensions: []string{".bin", ".zip", ".7z"},
		LauncherID: "O2EM",
	},
	"odyssey2": {
		SystemID:   systemdefs.SystemOdyssey2,
		Extensions: []string{".bin", ".zip", ".7z"},
		LauncherID: "Odyssey2",
	},
	"odcommander": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".odcommander"},
		LauncherID: "ODCommander",
	},
	"openbor": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".pak"},
		LauncherID: "OpenBOR",
	},
	"openjazz": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".openjazz"},
		LauncherID: "OpenJazz",
	},
	"oricatmos": {
		SystemID:   systemdefs.SystemOric,
		Extensions: []string{".dsk", ".tap"},
		LauncherID: "OricAtmos",
	},
	"pc88": {
		SystemID:   systemdefs.SystemPC88,
		Extensions: []string{".d88", ".u88", ".m3u"},
		LauncherID: "PC88",
	},
	"pc98": {
		SystemID: systemdefs.SystemPC98,
		Extensions: []string{
			".d98", ".zip", ".98d", ".fdi", ".fdd", ".2hd", ".tfd", ".d88", ".88d", ".hdm",
			".xdf", ".dup", ".cmd", ".hdi", ".thd", ".nhd", ".hdd", ".hdn", ".m3u",
		},
		LauncherID: "PC98",
	},
	"pcengine": {
		SystemID:   systemdefs.SystemTurboGrafx16,
		Extensions: []string{".pce", ".bin", ".zip", ".7z"},
		LauncherID: "PCEngine",
	},
	"pcenginecd": {
		SystemID:   systemdefs.SystemTurboGrafx16CD,
		Extensions: []string{".pce", ".cue", ".ccd", ".iso", ".img", ".chd"},
		LauncherID: "PCEngineCD",
	},
	"pcfx": {
		SystemID:   systemdefs.SystemPCFX,
		Extensions: []string{".cue", ".ccd", ".toc", ".chd", ".zip", ".7z"},
		LauncherID: "PCFX",
	},
	"pdp1": {
		SystemID:   systemdefs.SystemPDP1,
		Extensions: []string{".zip", ".7z", ".tap", ".rim", ".drm"},
		LauncherID: "PDP1",
	},
	"pet": {
		SystemID:   systemdefs.SystemPET2001,
		Extensions: []string{".a0", ".b0", ".crt", ".d64", ".d81", ".prg", ".tap", ".t64", ".m3u", ".zip", ".7z"},
		LauncherID: "PET",
	},
	"pico": {
		SystemID:   systemdefs.SystemGenesis,
		Extensions: []string{".bin", ".md", ".zip", ".7z"},
		LauncherID: "Pico",
	},
	"pico8": {
		SystemID:   systemdefs.SystemPico8,
		Extensions: []string{".p8", ".png", ".m3u"},
		LauncherID: "Pico8",
	},
	"plugnplay": {
		SystemID:   systemdefs.SystemPlugNPlay,
		Extensions: []string{".game"},
		LauncherID: "PlugNPlay",
	},
	"pokemini": {
		SystemID:   systemdefs.SystemPokemonMini,
		Extensions: []string{".min", ".zip", ".7z"},
		LauncherID: "PokeMini",
	},
	"ports": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".sh", ".squashfs"},
		LauncherID: "Ports",
	},
	"prboom": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".wad", ".iwad", ".pwad"},
		LauncherID: "PRBoom",
	},
	"ps2": {
		SystemID:   systemdefs.SystemPS2,
		Extensions: []string{".iso", ".mdf", ".nrg", ".bin", ".img", ".dump", ".gz", ".cso", ".chd", ".m3u"},
		LauncherID: "PS2",
	},
	"ps3": {
		SystemID:   systemdefs.SystemPS3,
		Extensions: []string{".ps3", ".psn", ".squashfs"},
		LauncherID: "PS3",
	},
	"ps4": {
		SystemID:   systemdefs.SystemPS4,
		Extensions: []string{".ps4"},
		LauncherID: "PS4",
	},
	"psp": {
		SystemID:   systemdefs.SystemPSP,
		Extensions: []string{".iso", ".cso", ".pbp", ".chd"},
		LauncherID: "PSP",
	},
	"psvita": {
		SystemID:   systemdefs.SystemVita,
		Extensions: []string{".zip", ".psvita"},
		LauncherID: "PSVita",
	},
	"psx": {
		SystemID:   systemdefs.SystemPSX,
		Extensions: []string{".cue", ".img", ".mdf", ".pbp", ".toc", ".cbn", ".m3u", ".ccd", ".chd", ".iso"},
		LauncherID: "PSX",
	},
	"pv1000": {
		SystemID:   systemdefs.SystemCasioPV1000,
		Extensions: []string{".bin", ".zip", ".7z"},
		LauncherID: "PV1000",
	},
	"pygame": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".py", ".pygame"},
		LauncherID: "PyGame",
	},
	"pyxel": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".pyxel", ".pyxapp"},
		LauncherID: "Pyxel",
	},
	"quake": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".quake"},
		LauncherID: "Quake",
	},
	"quake2": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".quake2", ".zip", ".7zip"},
		LauncherID: "Quake2",
	},
	"quake3": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".quake3"},
		LauncherID: "Quake3",
	},
	"raze": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".raze"},
		LauncherID: "Raze",
	},
	"recordings": {
		SystemID:   systemdefs.SystemVideo,
		Extensions: []string{".mp4", ".avi", ".mkv"},
		LauncherID: "Recordings",
	},
	"reminiscence": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".reminiscence"},
		LauncherID: "Reminiscence",
	},
	"rott": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".rott"},
		LauncherID: "ROTT",
	},
	"samcoupe": {
		SystemID:   systemdefs.SystemSAMCoupe,
		Extensions: []string{".cpm", ".dsk", ".sad", ".mgt", ".sdf", ".td0", ".sbt", ".zip"},
		LauncherID: "SamCoupe",
	},
	"satellaview": {
		SystemID:   systemdefs.SystemSNES,
		Extensions: []string{".bs", ".smc", ".sfc", ".zip", ".7z"},
		LauncherID: "Satellaview",
	},
	"saturn": {
		SystemID:   systemdefs.SystemSaturn,
		Extensions: []string{".cue", ".ccd", ".m3u", ".chd", ".iso", ".zip"},
		LauncherID: "Saturn",
	},
	"scummvm": {
		SystemID:   systemdefs.SystemScummVM,
		Extensions: []string{".scummvm", ".squashfs"},
		LauncherID: "ScummVM",
	},
	"scv": {
		SystemID:   systemdefs.SystemSG1000,
		Extensions: []string{".bin", ".zip", ".0"},
		LauncherID: "SCV",
	},
	"sdlpop": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".sdlpop"},
		LauncherID: "SDLPoP",
	},
	"sega32x": {
		SystemID:   systemdefs.SystemSega32X,
		Extensions: []string{".32x", ".chd", ".smd", ".bin", ".md", ".zip", ".7z"},
		LauncherID: "Sega32x",
	},
	"sg1000": {
		SystemID:   systemdefs.SystemSG1000,
		Extensions: []string{".bin", ".sg", ".zip", ".7z"},
		LauncherID: "SG1000",
	},
	"sgb": {
		SystemID:   systemdefs.SystemSuperGameboy,
		Extensions: []string{".gb", ".gbc", ".zip", ".7z"},
		LauncherID: "SGB",
	},
	"sgb-msu1": {
		SystemID:   systemdefs.SystemSGBMSU1,
		Extensions: []string{".gb", ".gbc", ".zip", ".7z"},
		LauncherID: "SGBMSU1",
	},
	"singe": {
		SystemID:   systemdefs.SystemSinge,
		Extensions: []string{".singe"},
		LauncherID: "Singe",
	},
	"snes": {
		SystemID:   systemdefs.SystemSNES,
		Extensions: []string{".smc", ".fig", ".sfc", ".gd3", ".gd7", ".dx2", ".bsx", ".swc", ".zip", ".7z"},
		LauncherID: "SNES",
	},
	"snes-msu1": {
		SystemID: systemdefs.SystemSNESMSU1,
		Extensions: []string{
			".smc", ".fig", ".sfc", ".gd3", ".gd7", ".dx2", ".bsx", ".swc", ".zip", ".7z", ".squashfs",
		},
		LauncherID: "SNESMSU1",
	},
	"socrates": {
		SystemID:   systemdefs.SystemSocrates,
		Extensions: []string{".bin", ".zip"},
		LauncherID: "Socrates",
	},
	"solarus": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".solarus"},
		LauncherID: "Solarus",
	},
	"sonic-mania": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".sman"},
		LauncherID: "SonicMania",
	},
	"sonic3-air": {
		SystemID:   systemdefs.SystemGenesis,
		Extensions: []string{".sonic3air"},
		LauncherID: "Sonic3AIR",
	},
	"sonicretro": {
		SystemID:   systemdefs.SystemGenesis,
		Extensions: []string{".sonicretro"},
		LauncherID: "SonicRetro",
	},
	"spectravideo": {
		SystemID:   systemdefs.SystemSpectravideo,
		Extensions: []string{".cas", ".rom", ".ri", ".mx1", ".mx2", ".dsk", ".zip"},
		LauncherID: "Spectravideo",
	},
	"steam": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".steam"},
		LauncherID: "Steam",
	},
	"sufami": {
		SystemID:   systemdefs.SystemSufami,
		Extensions: []string{".st", ".zip"},
		LauncherID: "Sufami",
	},
	"superbroswar": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".superbroswar"},
		LauncherID: "SuperBrosWar",
	},
	"supergrafx": {
		SystemID:   systemdefs.SystemSuperGrafx,
		Extensions: []string{".pce", ".sgx", ".cue", ".ccd", ".chd", ".zip", ".7z"},
		LauncherID: "SuperGrafx",
	},
	"supracan": {
		SystemID:   systemdefs.SystemSuperACan,
		Extensions: []string{".bin", ".zip"},
		LauncherID: "SuperACan",
	},
	"supervision": {
		SystemID:   systemdefs.SystemSuperVision,
		Extensions: []string{".sv", ".zip", ".7z"},
		LauncherID: "SuperVision",
	},
	"switch": {
		SystemID:   systemdefs.SystemSwitch,
		Extensions: []string{".xci", ".nsp"},
		LauncherID: "Switch",
	},
	"systemsp": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".zip"},
		LauncherID: "SystemSP",
	},
	"theforceengine": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".theforceengine"},
		LauncherID: "TheForceEngine",
	},
	"thextech": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".thextech"},
		LauncherID: "TheXTech",
	},
	"thomson": {
		SystemID:   systemdefs.SystemThomson,
		Extensions: []string{".fd", ".sap", ".k7", ".m7", ".m5", ".rom", ".zip"},
		LauncherID: "Thomson",
	},
	"ti99": {
		SystemID:   systemdefs.SystemTI994A,
		Extensions: []string{".rpk", ".wav", ".zip", ".7z"},
		LauncherID: "TI99",
	},
	"tic80": {
		SystemID:   systemdefs.SystemTIC80,
		Extensions: []string{".tic"},
		LauncherID: "TIC80",
	},
	"traider1": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".traider1"},
		LauncherID: "Traider1",
	},
	"traider2": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".traider2"},
		LauncherID: "Traider2",
	},
	"triforce": {
		SystemID:   systemdefs.SystemTriforce,
		Extensions: []string{".iso", ".gcz"},
		LauncherID: "Triforce",
	},
	"tutor": {
		SystemID:   systemdefs.SystemTomyTutor,
		Extensions: []string{".bin", ".wav", ".zip", ".7z"},
		LauncherID: "Tutor",
	},
	"tyrian": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".game"},
		LauncherID: "Tyrian",
	},
	"tyrquake": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".pak"},
		LauncherID: "TyrQuake",
	},
	"uqm": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".uqm"},
		LauncherID: "UQM",
	},
	"uzebox": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".uze"},
		LauncherID: "Uzebox",
	},
	"vc4000": {
		SystemID:   systemdefs.SystemVC4000,
		Extensions: []string{".bin", ".rom", ".pgm", ".tvc", ".zip", ".7z"},
		LauncherID: "VC4000",
	},
	"vectrex": {
		SystemID:   systemdefs.SystemVectrex,
		Extensions: []string{".bin", ".gam", ".vec", ".zip", ".7z"},
		LauncherID: "Vectrex",
	},
	"vemulator": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".vemulator"},
		LauncherID: "VEmulator",
	},
	"vgmplay": {
		SystemID:   systemdefs.SystemAudio,
		Extensions: []string{".vgm", ".vgz"},
		LauncherID: "VGMPlay",
	},
	"videopacplus": {
		SystemID:   systemdefs.SystemVideopacPlus,
		Extensions: []string{".bin", ".zip"},
		LauncherID: "VideopacPlus",
	},
	"vircon32": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".v32"},
		LauncherID: "Vircon32",
	},
	"virtualboy": {
		SystemID:   systemdefs.SystemVirtualBoy,
		Extensions: []string{".vb", ".zip", ".7z"},
		LauncherID: "VirtualBoy",
	},
	"vis": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".vis"},
		LauncherID: "VIS",
	},
	"vpinball": {
		SystemID:   systemdefs.SystemArcade,
		Extensions: []string{".vpx", ".vpt"},
		LauncherID: "VPinball",
	},
	"vsmile": {
		SystemID:   systemdefs.SystemVSmile,
		Extensions: []string{".zip", ".7z"},
		LauncherID: "VSmile",
	},
	"wasm4": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".wasm"},
		LauncherID: "Wasm4",
	},
	"wii": {
		SystemID:   systemdefs.SystemWii,
		Extensions: []string{".gcm", ".iso", ".gcz", ".ciso", ".wbfs", ".wad", ".rvz", ".elf", ".dol", ".m3u", ".json"},
		LauncherID: "Wii",
	},
	"wiiu": {
		SystemID:   systemdefs.SystemWiiU,
		Extensions: []string{".wua", ".wup", ".wud", ".wux", ".rpx", ".squashfs", ".wuhb"},
		LauncherID: "WiiU",
	},
	"windows": {
		SystemID:   systemdefs.SystemWindows,
		Extensions: []string{".wine", ".exe", ".bat"},
		LauncherID: "Windows",
	},
	"windows_installers": {
		SystemID:   systemdefs.SystemWindows,
		Extensions: []string{".exe", ".msi"},
		LauncherID: "WindowsInstallers",
	},
	"wine": {
		SystemID:   systemdefs.SystemWindows,
		Extensions: []string{".wine", ".exe"},
		LauncherID: "Wine",
	},
	"wswan": {
		SystemID:   systemdefs.SystemWonderSwan,
		Extensions: []string{".ws", ".zip", ".7z"},
		LauncherID: "WSwan",
	},
	"wswanc": {
		SystemID:   systemdefs.SystemWonderSwanColor,
		Extensions: []string{".wsc", ".zip", ".7z"},
		LauncherID: "WSwanC",
	},
	"x1": {
		SystemID: systemdefs.SystemX1,
		Extensions: []string{
			".dx1", ".zip", ".2d", ".2hd", ".tfd", ".d88", ".88d", ".hdm", ".xdf", ".dup", ".cmd", ".7z",
		},
		LauncherID: "X1",
	},
	"x68000": {
		SystemID: systemdefs.SystemX68000,
		Extensions: []string{
			".dim", ".img", ".d88", ".88d", ".hdm", ".dup", ".2hd", ".xdf", ".hdf", ".cmd", ".m3u", ".zip", ".7z",
		},
		LauncherID: "X68000",
	},
	"xash3d_fwgs": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".xash3d"},
		LauncherID: "Xash3D_FWGS",
	},
	"xbox": {
		SystemID:   systemdefs.SystemXbox,
		Extensions: []string{".iso", ".squashfs"},
		LauncherID: "Xbox",
	},
	"xbox360": {
		SystemID:   systemdefs.SystemXbox360,
		Extensions: []string{".iso", ".xex", ".xbox360", ".zar"},
		LauncherID: "Xbox360",
	},
	"xegs": {
		SystemID:   systemdefs.SystemAtariXEGS,
		Extensions: []string{".atr", ".dsk", ".xfd", ".bin", ".rom", ".car", ".zip", ".7z"},
		LauncherID: "XEGS",
	},
	"xrick": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".xrick"},
		LauncherID: "XRick",
	},
	"zc210": {
		SystemID:   systemdefs.SystemPC,
		Extensions: []string{".zc210"},
		LauncherID: "ZC210",
	},
	"zx81": {
		SystemID:   systemdefs.SystemZX81,
		Extensions: []string{".tzx", ".p", ".zip", ".7z"},
		LauncherID: "ZX81",
	},
	"zxspectrum": {
		SystemID:   systemdefs.SystemZXSpectrum,
		Extensions: []string{".tzx", ".tap", ".z80", ".rzx", ".scl", ".trd", ".dsk", ".zip", ".7z"},
		LauncherID: "ZXSpectrum",
	},
}

// LookupByFolderName returns the SystemInfo for an EmulationStation folder name.
func LookupByFolderName(folder string) (SystemInfo, bool) {
	info, ok := SystemMap[strings.ToLower(folder)]
	return info, ok
}

// GetSystemID returns the Zaparoo system ID for an ES folder name.
func GetSystemID(folder string) (string, error) {
	info, ok := LookupByFolderName(folder)
	if !ok {
		return "", fmt.Errorf("unknown system folder: %s", folder)
	}
	return info.SystemID, nil
}

// GetFoldersForSystemID returns all ES folder names that map to a given Zaparoo system ID.
func GetFoldersForSystemID(systemID string) []string {
	var folders []string
	for folder, info := range SystemMap {
		if strings.EqualFold(systemID, info.SystemID) {
			folders = append(folders, folder)
		}
	}
	return folders
}

// HasExtension checks if the given extension is supported by the specified folder.
func HasExtension(folder, ext string) bool {
	info, ok := LookupByFolderName(folder)
	if !ok {
		return false
	}
	for _, e := range info.Extensions {
		if strings.EqualFold(e, ext) {
			return true
		}
	}
	return false
}
