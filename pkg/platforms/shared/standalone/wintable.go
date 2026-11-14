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

package standalone

import (
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

// WindowsSystem is one launcher an emulator provides for one system.
type WindowsSystem struct {
	Args     ArgsFunc
	ID       string
	SystemID string
	// Folders are the ROM folder names the launcher indexes. The Zaparoo
	// system ID folder is always added.
	Folders []string
	// Extensions replaces the extensions taken from the folders' ES-DE
	// definitions when set.
	Extensions []string
	// ExtraExtensions are added to the ES-DE extensions.
	ExtraExtensions []string
	NoScan          bool
}

// WindowsEmulator is one standalone Windows emulator and the launchers it
// provides. Emulators are listed in priority order: when two launchers
// match the same file, the earlier one is the default.
type WindowsEmulator struct {
	Name string
	// Exes are executable file names, matched case-insensitively. A name
	// ending in "*" matches any executable starting with the text before it.
	Exes []string
	// DirHints are lower-case prefixes of folder names the emulator is
	// commonly extracted or installed to. The lower-cased Name is always one.
	DirHints []string
	// Scoop are the emulator's Scoop app names.
	Scoop   []string
	Systems []WindowsSystem
	// WorkDirInstall runs the emulator from its own folder, for emulators
	// that find their data and config relative to the working directory.
	WorkDirInstall bool
	// MAMEConfig tells the launcher to ask MAME for its configured rompath.
	MAMEConfig bool
}

func sys(id, systemID string, args ArgsFunc, folders ...string) WindowsSystem {
	return WindowsSystem{ID: id, SystemID: systemID, Args: args, Folders: folders}
}

//nolint:gocritic // Value builders keep the static table readable.
func withExtensions(system WindowsSystem, extensions ...string) WindowsSystem {
	system.Extensions = extensions
	return system
}

//nolint:gocritic // Value builders keep the static table readable.
func withExtraExtensions(system WindowsSystem, extensions ...string) WindowsSystem {
	system.ExtraExtensions = extensions
	return system
}

//nolint:gocritic // Value builders keep the static table readable.
func noScan(system WindowsSystem) WindowsSystem {
	system.NoScan = true
	return system
}

func aresSystem(id, systemID, aresName string, folders ...string) WindowsSystem {
	return sys(id, systemID, Batch("--fullscreen", "--system", aresName), folders...)
}

func fusionSystem(id, systemID, mode string, folders ...string) WindowsSystem {
	return sys(id, systemID, Batch(mode, "-auto", "-fullscreen"), folders...)
}

// mednafenDisc are the disc image formats Mednafen opens. It cannot read CHD.
var mednafenDisc = []string{".cue", ".ccd", ".toc", ".m3u"}

// mednafenSystem lists extensions explicitly because Mednafen opens neither
// CHD nor 7z, which the ES-DE folder definitions include.
func mednafenSystem(id, systemID string, extensions []string, folders ...string) WindowsSystem {
	return withExtensions(sys(id, systemID, Batch("-video.fs", "1"), folders...), extensions...)
}

// jgenesisArgs opens the file fullscreen without jgenesis's game list window;
// jgenesis exits when the emulator window closes. The fullscreen override does
// not change the user's saved settings.
func jgenesisArgs(ctx *Context) ([]string, error) {
	return Batch("--no-gui", "--config-override", "common.launch_in_fullscreen=true", "--file-path")(ctx)
}

// WindowsEmulators returns the built-in Windows standalone emulators.
//
//nolint:funlen,maintidx // Static launcher data.
func WindowsEmulators() []WindowsEmulator {
	return []WindowsEmulator{
		// Arcade
		{
			Name: "GroovyMAME", Exes: []string{"groovymame.exe", "groovymame64.exe"},
			DirHints: []string{"mame"}, WorkDirInstall: true, MAMEConfig: true,
			Systems: []WindowsSystem{
				withExtensions(sys("GroovyMAME", systemdefs.SystemArcade, MAMEArgs, "arcade", "mame"), ".zip", ".7z"),
			},
		},
		{
			Name: "MAME", Exes: []string{"mame.exe", "mame64.exe"}, Scoop: []string{"mame"},
			WorkDirInstall: true, MAMEConfig: true,
			Systems: []WindowsSystem{
				withExtensions(sys("MAME", systemdefs.SystemArcade, MAMEArgs, "arcade", "mame"), ".zip", ".7z"),
			},
		},
		{
			Name: "FinalBurnNeo", Exes: []string{"fbneo64.exe", "fbneo.exe"},
			DirHints: []string{"fbneo", "finalburn"}, WorkDirInstall: true,
			Systems: []WindowsSystem{
				withExtensions(sys("FinalBurnNeo", systemdefs.SystemArcade, FBNeoArgs, "fbneo", "arcade"),
					".zip", ".7z"),
				withExtensions(sys("FinalBurnNeoNeoGeo", systemdefs.SystemNeoGeo, FBNeoArgs, "neogeo"),
					".zip", ".7z"),
			},
		},
		{
			Name: "Supermodel", Exes: []string{"supermodel.exe"}, WorkDirInstall: true,
			Systems: []WindowsSystem{sys("Supermodel", systemdefs.SystemModel3, Batch("-fullscreen"), "model3")},
		},
		{
			Name: "Flycast", Exes: []string{"flycast.exe"}, Scoop: []string{"flycast"},
			Systems: []WindowsSystem{
				sys("FlycastDreamcast", systemdefs.SystemDreamcast,
					Batch("-config", "window:fullscreen=yes"), "dreamcast", "dc"),
				sys("FlycastNaomi", systemdefs.SystemNAOMI, Batch("-config", "window:fullscreen=yes"), "naomi"),
				sys("FlycastAtomiswave", systemdefs.SystemAtomiswave,
					Batch("-config", "window:fullscreen=yes"), "atomiswave"),
			},
		},
		{
			Name: "Demul", Exes: []string{"demul.exe"}, WorkDirInstall: true,
			Systems: []WindowsSystem{
				withExtensions(sys("DemulDreamcast", systemdefs.SystemDreamcast,
					Joined("-image=", "-run=dc"), "dreamcast", "dc"), ".cdi", ".gdi", ".chd", ".cue"),
				withExtensions(sys("DemulNaomi", systemdefs.SystemNAOMI, demulSetArgs("naomi"), "naomi"),
					".zip", ".7z"),
				withExtensions(sys("DemulAtomiswave", systemdefs.SystemAtomiswave, demulSetArgs("awave"),
					"atomiswave"), ".zip", ".7z"),
			},
		},
		// Nintendo
		{
			Name: "Dolphin", Exes: []string{"dolphin.exe"}, Scoop: []string{"dolphin", "dolphin-dev"},
			Systems: []WindowsSystem{
				sys("DolphinGameCube", systemdefs.SystemGameCube, Batch("-b", "-e"), "gamecube", "gc"),
				sys("DolphinWii", systemdefs.SystemWii, Batch("-b", "-e"), "wii"),
			},
		},
		{
			Name: "PrimeHack", Exes: []string{"primehack.exe"},
			Systems: []WindowsSystem{
				noScan(sys("PrimeHackGameCube", systemdefs.SystemGameCube, Batch("-b", "-e"), "gamecube", "gc")),
				noScan(sys("PrimeHackWii", systemdefs.SystemWii, Batch("-b", "-e"), "wii")),
			},
		},
		{
			Name: "Cemu", Exes: []string{"cemu.exe"}, Scoop: []string{"cemu"},
			Systems: []WindowsSystem{
				withExtensions(sys("Cemu", systemdefs.SystemWiiU, CemuArgs, "wiiu"),
					".wud", ".wux", ".wua", ".wuhb", ".rpx", ".elf"),
			},
		},
		{
			Name: "Ryujinx", Exes: []string{"ryujinx.exe"}, DirHints: []string{"ryubing"},
			Scoop: []string{"ryujinx", "ryubing"},
			Systems: []WindowsSystem{
				withExtraExtensions(sys("Ryubing", systemdefs.SystemSwitch, Batch("--fullscreen"), "switch"),
					".nro", ".nca"),
			},
		},
		{
			Name: "Eden", Exes: []string{"eden.exe"},
			Systems: []WindowsSystem{
				withExtraExtensions(sys("Eden", systemdefs.SystemSwitch, Batch("-f", "-g"), "switch"), ".nro", ".nca"),
			},
		},
		{
			Name: "Azahar", Exes: []string{"azahar.exe"}, Scoop: []string{"azahar"},
			Systems: []WindowsSystem{
				withExtensions(sys("Azahar", systemdefs.System3DS, Batch("-f"), "3ds", "n3ds"),
					".3ds", ".cci", ".cxi", ".3dsx", ".app", ".zcci", ".zcxi", ".z3dsx"),
			},
		},
		{
			Name: "melonDS", Exes: []string{"melonds.exe"}, Scoop: []string{"melonds"},
			Systems: []WindowsSystem{
				withExtensions(sys("MelonDS", systemdefs.SystemNDS, Batch("-f"), "nds"), ".nds", ".srl", ".dsi",
					".ids", ".zip", ".7z"),
			},
		},
		{
			Name: "DeSmuME", Exes: []string{"desmume*"}, Scoop: []string{"desmume"},
			Systems: []WindowsSystem{
				withExtensions(sys("DeSmuME", systemdefs.SystemNDS, Batch(), "nds"), ".nds", ".srl", ".zip", ".7z"),
			},
		},
		{
			Name: "NO$GBA", Exes: []string{"no$gba.exe"}, DirHints: []string{"no$gba", "nogba"},
			Systems: []WindowsSystem{
				withExtensions(sys("NoGBA", systemdefs.SystemNDS, Batch(), "nds"), ".nds", ".srl"),
			},
		},
		{
			Name: "mGBA", Exes: []string{"mgba.exe"}, Scoop: []string{"mgba"},
			Systems: []WindowsSystem{
				sys("mGBAGBA", systemdefs.SystemGBA, Batch("-f"), "gba"),
				sys("mGBAGB", systemdefs.SystemGameboy, Batch("-f"), "gb"),
				sys("mGBAGBC", systemdefs.SystemGameboyColor, Batch("-f"), "gbc"),
			},
		},
		{
			Name: "SameBoy", Exes: []string{"sameboy.exe"}, Scoop: []string{"sameboy"},
			Systems: []WindowsSystem{
				sys("SameBoyGB", systemdefs.SystemGameboy, Batch("--fullscreen"), "gb"),
				sys("SameBoyGBC", systemdefs.SystemGameboyColor, Batch("--fullscreen"), "gbc"),
			},
		},
		{
			Name: "VisualBoyAdvance-M", Exes: []string{"visualboyadvance-m.exe"},
			DirHints: []string{"vba", "visualboy"}, Scoop: []string{"visualboyadvance-m"},
			Systems: []WindowsSystem{
				sys("VBAMGBA", systemdefs.SystemGBA, Batch("-f"), "gba"),
				sys("VBAMGB", systemdefs.SystemGameboy, Batch("-f"), "gb"),
				sys("VBAMGBC", systemdefs.SystemGameboyColor, Batch("-f"), "gbc"),
			},
		},
		{
			Name: "Mesen", Exes: []string{"mesen.exe"}, Scoop: []string{"mesen"},
			Systems: []WindowsSystem{
				sys("MesenNES", systemdefs.SystemNES, Batch("--fullscreen"), "nes"),
				sys("MesenFDS", systemdefs.SystemFDS, Batch("--fullscreen"), "fds"),
				sys("MesenSNES", systemdefs.SystemSNES, Batch("--fullscreen"), "snes"),
				sys("MesenGB", systemdefs.SystemGameboy, Batch("--fullscreen"), "gb"),
				sys("MesenGBC", systemdefs.SystemGameboyColor, Batch("--fullscreen"), "gbc"),
				sys("MesenGBA", systemdefs.SystemGBA, Batch("--fullscreen"), "gba"),
				sys("MesenPCEngine", systemdefs.SystemTurboGrafx16, Batch("--fullscreen"), "pcengine", "tg16"),
				sys("MesenMasterSystem", systemdefs.SystemMasterSystem, Batch("--fullscreen"), "mastersystem"),
				sys("MesenGameGear", systemdefs.SystemGameGear, Batch("--fullscreen"), "gamegear"),
				sys("MesenWonderSwan", systemdefs.SystemWonderSwan, Batch("--fullscreen"), "wswan", "wonderswan"),
				sys("MesenWonderSwanColor", systemdefs.SystemWonderSwanColor, Batch("--fullscreen"),
					"wswanc", "wonderswancolor"),
			},
		},
		{
			Name: "Snes9x", Exes: []string{"snes9x-x64.exe", "snes9x.exe"}, Scoop: []string{"snes9x"},
			Systems: []WindowsSystem{sys("Snes9x", systemdefs.SystemSNES, Batch("-fullscreen"), "snes")},
		},
		{
			Name: "bsnes", Exes: []string{"bsnes.exe"}, Scoop: []string{"bsnes"},
			Systems: []WindowsSystem{sys("BSNESStandalone", systemdefs.SystemSNES, Batch("--fullscreen"), "snes")},
		},
		{
			Name: "Project64", Exes: []string{"project64.exe"}, Scoop: []string{"project64"},
			Systems: []WindowsSystem{sys("Project64", systemdefs.SystemNintendo64, Batch(), "n64")},
		},
		{
			Name: "RMG", Exes: []string{"rmg.exe"}, DirHints: []string{"rosalie"}, Scoop: []string{"rmg"},
			Systems: []WindowsSystem{
				sys("RMG", systemdefs.SystemNintendo64, Batch("--fullscreen", "--quit-after-emulation"), "n64"),
			},
		},
		{
			Name: "Gopher64", Exes: []string{"gopher64*"},
			Systems: []WindowsSystem{sys("Gopher64", systemdefs.SystemNintendo64, Batch("--fullscreen"), "n64")},
		},
		{
			Name: "Nestopia", Exes: []string{"nestopia.exe"}, Scoop: []string{"nestopia"},
			Systems: []WindowsSystem{sys("Nestopia", systemdefs.SystemNES, Batch(), "nes")},
		},
		{
			Name: "puNES", Exes: []string{"punes64.exe", "punes.exe"}, Scoop: []string{"punes"},
			Systems: []WindowsSystem{sys("PuNES", systemdefs.SystemNES, Batch("--fullscreen", "yes"), "nes")},
		},
		// Sony
		{
			Name: "DuckStation", Exes: []string{"duckstation-qt*"}, Scoop: []string{"duckstation"},
			Systems: []WindowsSystem{
				withExtraExtensions(sys("DuckStation", systemdefs.SystemPSX, Batch("-batch", "-fullscreen", "--"),
					"psx"), ".exe", ".psexe", ".psf", ".minipsf"),
			},
		},
		{
			Name: "PCSX2", Exes: []string{"pcsx2-qt*", "pcsx2.exe"}, Scoop: []string{"pcsx2"},
			Systems: []WindowsSystem{
				withExtraExtensions(sys("PCSX2", systemdefs.SystemPS2, Batch("-batch", "-fullscreen", "--"), "ps2"),
					".elf"),
			},
		},
		{
			Name: "Play!", Exes: []string{"play.exe"}, DirHints: []string{"play"},
			Systems: []WindowsSystem{
				withExtensions(sys("PlayPS2", systemdefs.SystemPS2, Batch("--fullscreen", "--disc"), "ps2"),
					".iso", ".bin", ".cue", ".chd", ".cso", ".isz"),
			},
		},
		{
			Name: "PPSSPP", Exes: []string{"ppssppwindows64.exe", "ppssppwindows.exe"}, Scoop: []string{"ppsspp"},
			Systems: []WindowsSystem{
				withExtensions(sys("PPSSPP", systemdefs.SystemPSP, Batch("--fullscreen"), "psp"),
					".iso", ".cso", ".chd", ".pbp", ".elf", ".prx"),
			},
		},
		{
			Name: "RPCS3", Exes: []string{"rpcs3.exe"}, Scoop: []string{"rpcs3"},
			Systems: []WindowsSystem{withExtensions(sys("RPCS3", systemdefs.SystemPS3, RPCS3Args, "ps3"), ".ps3")},
		},
		{
			Name: "Vita3K", Exes: []string{"vita3k.exe"}, Scoop: []string{"vita3k"},
			Systems: []WindowsSystem{
				withExtensions(sys("Vita3K", systemdefs.SystemVita, Vita3KArgs, "psvita", "vita"), ".psvita"),
			},
		},
		{
			Name: "shadPS4", Exes: []string{"shadps4.exe"}, WorkDirInstall: true,
			Systems: []WindowsSystem{withExtensions(sys("ShadPS4", systemdefs.SystemPS4, ShadPS4Args, "ps4"), ".ps4")},
		},
		{
			Name: "Mednafen", Exes: []string{"mednafen.exe"}, Scoop: []string{"mednafen"}, WorkDirInstall: true,
			Systems: []WindowsSystem{
				mednafenSystem("MednafenPSX", systemdefs.SystemPSX, mednafenDisc, "psx"),
				mednafenSystem("MednafenSaturn", systemdefs.SystemSaturn, mednafenDisc, "saturn"),
				mednafenSystem("MednafenPCEngine", systemdefs.SystemTurboGrafx16,
					[]string{".pce", ".zip"}, "pcengine", "tg16"),
				mednafenSystem("MednafenPCEngineCD", systemdefs.SystemTurboGrafx16CD, mednafenDisc,
					"pcenginecd", "tg-cd"),
				mednafenSystem("MednafenSuperGrafx", systemdefs.SystemSuperGrafx,
					[]string{".sgx", ".pce", ".zip"}, "supergrafx"),
				mednafenSystem("MednafenPCFX", systemdefs.SystemPCFX, mednafenDisc, "pcfx"),
				mednafenSystem("MednafenLynx", systemdefs.SystemAtariLynx, []string{".lnx", ".zip"},
					"lynx", "atarilynx"),
				mednafenSystem("MednafenNeoGeoPocket", systemdefs.SystemNeoGeoPocket,
					[]string{".ngp", ".zip"}, "ngp"),
				mednafenSystem("MednafenNeoGeoPocketColor", systemdefs.SystemNeoGeoPocketColor,
					[]string{".ngc", ".ngp", ".zip"}, "ngpc"),
				mednafenSystem("MednafenWonderSwan", systemdefs.SystemWonderSwan, []string{".ws", ".zip"},
					"wswan", "wonderswan"),
				mednafenSystem("MednafenWonderSwanColor", systemdefs.SystemWonderSwanColor,
					[]string{".wsc", ".ws", ".zip"}, "wswanc", "wonderswancolor"),
				mednafenSystem("MednafenVirtualBoy", systemdefs.SystemVirtualBoy, []string{".vb", ".vboy", ".zip"},
					"virtualboy"),
			},
		},
		// Sega
		{
			Name: "Kega Fusion", Exes: []string{"fusion.exe"}, DirHints: []string{"kega", "fusion"},
			Systems: []WindowsSystem{
				fusionSystem("KegaFusionGenesis", systemdefs.SystemGenesis, "-gen", "megadrive", "genesis"),
				fusionSystem("KegaFusionMasterSystem", systemdefs.SystemMasterSystem, "-sms", "mastersystem"),
				fusionSystem("KegaFusionGameGear", systemdefs.SystemGameGear, "-gg", "gamegear"),
				fusionSystem("KegaFusion32X", systemdefs.SystemSega32X, "-32x", "sega32x"),
				// Fusion predates CHD.
				withExtensions(fusionSystem("KegaFusionMegaCD", systemdefs.SystemMegaCD, "-scd", "megacd", "segacd"),
					".cue", ".iso"),
			},
		},
		{
			Name: "BlastEm", Exes: []string{"blastem.exe"}, Scoop: []string{"blastem"},
			Systems: []WindowsSystem{
				sys("BlastEm", systemdefs.SystemGenesis, Batch("-f", "-m", "gen"), "megadrive", "genesis"),
			},
		},
		{
			Name: "jgenesis", Exes: []string{"jgenesis.exe"},
			Systems: []WindowsSystem{
				sys("JgenesisGenesis", systemdefs.SystemGenesis, jgenesisArgs, "megadrive", "genesis"),
				sys("JgenesisMasterSystem", systemdefs.SystemMasterSystem, jgenesisArgs, "mastersystem"),
				sys("JgenesisGameGear", systemdefs.SystemGameGear, jgenesisArgs, "gamegear"),
				sys("JgenesisMegaCD", systemdefs.SystemMegaCD, jgenesisArgs, "megacd", "segacd"),
				sys("Jgenesis32X", systemdefs.SystemSega32X, jgenesisArgs, "sega32x"),
			},
		},
		{
			Name: "Redream", Exes: []string{"redream.exe"}, WorkDirInstall: true,
			Systems: []WindowsSystem{
				withExtensions(sys("Redream", systemdefs.SystemDreamcast, Batch(), "dreamcast", "dc"),
					".cdi", ".gdi", ".chd", ".cue"),
			},
		},
		{
			Name: "Kronos", Exes: []string{"kronos.exe"},
			Systems: []WindowsSystem{sys("Kronos", systemdefs.SystemSaturn, Batch("-a", "-f", "-i"), "saturn")},
		},
		{
			Name: "Ymir", Exes: []string{"ymir-sdl3.exe", "ymir.exe"}, WorkDirInstall: true,
			Systems: []WindowsSystem{sys("Ymir", systemdefs.SystemSaturn, Batch("-f"), "saturn")},
		},
		// Microsoft
		{
			Name: "xemu", Exes: []string{"xemu.exe"}, Scoop: []string{"xemu"}, WorkDirInstall: true,
			Systems: []WindowsSystem{
				withExtensions(sys("Xemu", systemdefs.SystemXbox, Batch("-full-screen", "-dvd_path"), "xbox"), ".iso"),
			},
		},
		{
			Name: "Xenia Canary", Exes: []string{"xenia_canary.exe"}, DirHints: []string{"xenia"},
			WorkDirInstall: true,
			Systems: []WindowsSystem{
				withExtensions(sys("XeniaCanary", systemdefs.SystemXbox360, Batch("--fullscreen"), "xbox360"),
					".iso", ".xex", ".zar"),
			},
		},
		{
			Name: "Xenia Edge", Exes: []string{"xenia_edge.exe"}, DirHints: []string{"xenia"}, WorkDirInstall: true,
			Systems: []WindowsSystem{
				withExtensions(sys("XeniaEdge", systemdefs.SystemXbox360, Batch("--fullscreen"), "xbox360"),
					".iso", ".xex", ".zar"),
			},
		},
		// Multi-system
		{
			Name: "ares", Exes: []string{"ares.exe"}, Scoop: []string{"ares"},
			Systems: []WindowsSystem{
				aresSystem("AresNES", systemdefs.SystemNES, "Famicom", "nes"),
				aresSystem("AresSNES", systemdefs.SystemSNES, "Super Famicom", "snes"),
				aresSystem("AresN64", systemdefs.SystemNintendo64, "Nintendo 64", "n64"),
				aresSystem("AresGB", systemdefs.SystemGameboy, "Game Boy", "gb"),
				aresSystem("AresGBC", systemdefs.SystemGameboyColor, "Game Boy Color", "gbc"),
				aresSystem("AresGBA", systemdefs.SystemGBA, "Game Boy Advance", "gba"),
				aresSystem("AresGenesis", systemdefs.SystemGenesis, "Mega Drive", "megadrive", "genesis"),
				aresSystem("AresMegaCD", systemdefs.SystemMegaCD, "Mega CD", "megacd", "segacd"),
				aresSystem("Ares32X", systemdefs.SystemSega32X, "Mega 32X", "sega32x"),
				aresSystem("AresMasterSystem", systemdefs.SystemMasterSystem, "Master System", "mastersystem"),
				aresSystem("AresGameGear", systemdefs.SystemGameGear, "Game Gear", "gamegear"),
				aresSystem("AresSG1000", systemdefs.SystemSG1000, "SG-1000", "sg1000"),
				aresSystem("AresPCEngine", systemdefs.SystemTurboGrafx16, "PC Engine", "pcengine", "tg16"),
				aresSystem("AresSuperGrafx", systemdefs.SystemSuperGrafx, "SuperGrafx", "supergrafx"),
				aresSystem("AresNeoGeoPocket", systemdefs.SystemNeoGeoPocket, "Neo Geo Pocket", "ngp"),
				aresSystem("AresNeoGeoPocketColor", systemdefs.SystemNeoGeoPocketColor, "Neo Geo Pocket Color",
					"ngpc"),
				aresSystem("AresWonderSwan", systemdefs.SystemWonderSwan, "WonderSwan", "wswan", "wonderswan"),
				aresSystem("AresWonderSwanColor", systemdefs.SystemWonderSwanColor, "WonderSwan Color",
					"wswanc", "wonderswancolor"),
				aresSystem("AresColecoVision", systemdefs.SystemColecoVision, "ColecoVision", "colecovision"),
				aresSystem("AresMSX", systemdefs.SystemMSX, "MSX", "msx1", "msx"),
			},
		},
		{
			Name: "BizHawk", Exes: []string{"emuhawk.exe"}, Scoop: []string{"bizhawk"},
			Systems: []WindowsSystem{
				sys("BizHawkNES", systemdefs.SystemNES, Batch("--fullscreen"), "nes"),
				sys("BizHawkSNES", systemdefs.SystemSNES, Batch("--fullscreen"), "snes"),
				sys("BizHawkN64", systemdefs.SystemNintendo64, Batch("--fullscreen"), "n64"),
				sys("BizHawkGB", systemdefs.SystemGameboy, Batch("--fullscreen"), "gb"),
				sys("BizHawkGBC", systemdefs.SystemGameboyColor, Batch("--fullscreen"), "gbc"),
				sys("BizHawkGBA", systemdefs.SystemGBA, Batch("--fullscreen"), "gba"),
				sys("BizHawkGenesis", systemdefs.SystemGenesis, Batch("--fullscreen"), "megadrive", "genesis"),
				sys("BizHawkMasterSystem", systemdefs.SystemMasterSystem, Batch("--fullscreen"), "mastersystem"),
				sys("BizHawkGameGear", systemdefs.SystemGameGear, Batch("--fullscreen"), "gamegear"),
				sys("BizHawkPCEngine", systemdefs.SystemTurboGrafx16, Batch("--fullscreen"), "pcengine", "tg16"),
				sys("BizHawkPSX", systemdefs.SystemPSX, Batch("--fullscreen"), "psx"),
				sys("BizHawkAtari2600", systemdefs.SystemAtari2600, Batch("--fullscreen"), "atari2600"),
				sys("BizHawkAtari7800", systemdefs.SystemAtari7800, Batch("--fullscreen"), "atari7800"),
				sys("BizHawkLynx", systemdefs.SystemAtariLynx, Batch("--fullscreen"), "lynx", "atarilynx"),
				sys("BizHawkColecoVision", systemdefs.SystemColecoVision, Batch("--fullscreen"), "colecovision"),
				sys("BizHawkIntellivision", systemdefs.SystemIntellivision, Batch("--fullscreen"), "intellivision"),
				sys("BizHawkVirtualBoy", systemdefs.SystemVirtualBoy, Batch("--fullscreen"), "virtualboy"),
				sys("BizHawkWonderSwan", systemdefs.SystemWonderSwan, Batch("--fullscreen"), "wswan", "wonderswan"),
				sys("BizHawkWonderSwanColor", systemdefs.SystemWonderSwanColor, Batch("--fullscreen"),
					"wswanc", "wonderswancolor"),
				sys("BizHawkNeoGeoPocket", systemdefs.SystemNeoGeoPocket, Batch("--fullscreen"), "ngp"),
				sys("BizHawkNeoGeoPocketColor", systemdefs.SystemNeoGeoPocketColor, Batch("--fullscreen"), "ngpc"),
			},
		},
		// Computers and other systems
		{
			Name: "Stella", Exes: []string{"stella.exe"}, Scoop: []string{"stella"},
			Systems: []WindowsSystem{
				sys("StellaStandalone", systemdefs.SystemAtari2600, Batch("-fullscreen", "1"), "atari2600"),
			},
		},
		{
			Name: "Altirra", Exes: []string{"altirra64.exe", "altirra.exe"}, Scoop: []string{"altirra"},
			Systems: []WindowsSystem{
				sys("Altirra", systemdefs.SystemAtari800, Batch("/f"), "atari800"),
				sys("Altirra5200", systemdefs.SystemAtari5200, AltirraArgs("5200"), "atari5200"),
				sys("AltirraXEGS", systemdefs.SystemAtariXEGS, AltirraArgs("xegs"), "xegs"),
			},
		},
		{
			Name: "Atari800", Exes: []string{"atari800.exe"}, Scoop: []string{"atari800"},
			Systems: []WindowsSystem{sys("Atari800", systemdefs.SystemAtari800, Batch("-fullscreen"), "atari800")},
		},
		{
			Name: "A7800", Exes: []string{"a7800.exe"}, WorkDirInstall: true,
			Systems: []WindowsSystem{
				withExtensions(sys("A7800", systemdefs.SystemAtari7800, A7800Args, "atari7800"), ".a78", ".bin"),
			},
		},
		{
			Name: "BigPEmu", Exes: []string{"bigpemu.exe"},
			Systems: []WindowsSystem{
				withExtensions(sys("BigPEmu", systemdefs.SystemJaguar, Batch(), "jaguar"),
					".j64", ".jag", ".rom", ".abs", ".cof", ".bin", ".prg", ".zip"),
				withExtensions(sys("BigPEmuCD", systemdefs.SystemJaguarCD, Batch(), "jaguarcd"), ".cue", ".cdi"),
			},
		},
		{
			Name: "Hatari", Exes: []string{"hatari.exe"}, Scoop: []string{"hatari"},
			Systems: []WindowsSystem{
				withExtensions(sys("Hatari", systemdefs.SystemAtariST, Batch("-f"), "atarist"),
					".st", ".msa", ".stx", ".dim", ".ipf", ".zip"),
			},
		},
		{
			Name: "VICE C64", Exes: []string{"x64sc.exe"}, DirHints: []string{"vice", "gtk3vice", "sdl2vice"},
			Scoop: []string{"vice"},
			Systems: []WindowsSystem{
				withExtensions(sys("ViceC64", systemdefs.SystemC64, Batch("-VICIIfull", "-autostart"), "c64"),
					".d64", ".d71", ".d81", ".g64", ".crt", ".prg", ".p00", ".t64", ".tap", ".zip"),
			},
		},
		{
			Name: "VICE C128", Exes: []string{"x128.exe"}, DirHints: []string{"vice", "gtk3vice", "sdl2vice"},
			Scoop: []string{"vice"},
			Systems: []WindowsSystem{
				withExtensions(sys("ViceC128", systemdefs.SystemC128, Batch("-VICIIfull", "-autostart"), "c128"),
					".d64", ".d71", ".d81", ".g64", ".crt", ".prg", ".t64", ".tap", ".zip"),
			},
		},
		{
			Name: "VICE VIC-20", Exes: []string{"xvic.exe"}, DirHints: []string{"vice", "gtk3vice", "sdl2vice"},
			Scoop: []string{"vice"},
			Systems: []WindowsSystem{
				withExtensions(sys("ViceVIC20", systemdefs.SystemVIC20, Batch("-VICfull", "-autostart"), "c20",
					"vic20"), ".d64", ".crt", ".prg", ".t64", ".tap", ".20", ".40", ".60", ".a0", ".b0", ".zip"),
			},
		},
		{
			Name: "VICE Plus/4", Exes: []string{"xplus4.exe"}, DirHints: []string{"vice", "gtk3vice", "sdl2vice"},
			Scoop: []string{"vice"},
			Systems: []WindowsSystem{
				withExtensions(sys("VicePlus4", systemdefs.SystemCommodorePlus4, Batch("-TEDfull", "-autostart"),
					"cplus4", "plus4", "c16"), ".d64", ".crt", ".prg", ".t64", ".tap", ".zip"),
			},
		},
		{
			Name: "WinUAE", Exes: []string{"winuae64.exe", "winuae.exe"}, Scoop: []string{"winuae"},
			Systems: []WindowsSystem{
				withExtensions(sys("WinUAE", systemdefs.SystemAmiga, WinUAEArgs, "amiga"),
					".adf", ".adz", ".dms", ".ipf", ".zip"),
			},
		},
		{
			Name: "openMSX", Exes: []string{"openmsx.exe"}, Scoop: []string{"openmsx"},
			Systems: []WindowsSystem{
				withExtensions(sys("OpenMSX", systemdefs.SystemMSX, Batch(), "msx", "msx1", "msx2"),
					".rom", ".mx1", ".mx2", ".dsk", ".cas", ".zip"),
			},
		},
		{
			Name: "ZEsarUX", Exes: []string{"zesarux.exe"},
			Systems: []WindowsSystem{
				sys("ZEsarUX", systemdefs.SystemZXSpectrum, Batch("--quickexit", "--nosplash", "--fullscreen"),
					"zxspectrum"),
			},
		},
		{
			Name: "DOSBox Staging", Exes: []string{"dosbox.exe"}, DirHints: []string{"dosbox"},
			Scoop: []string{"dosbox-staging"},
			Systems: []WindowsSystem{
				withExtensions(sys("DOSBoxStaging", systemdefs.SystemDOS, DOSBoxArgs("-fullscreen", "-exit"), "dos"),
					".exe", ".com", ".bat", ".conf"),
			},
		},
		{
			Name: "DOSBox-X", Exes: []string{"dosbox-x.exe"}, Scoop: []string{"dosbox-x"},
			Systems: []WindowsSystem{
				withExtensions(sys("DOSBoxX", systemdefs.SystemDOS,
					DOSBoxArgs("-nopromptfolder", "-fastlaunch", "-fullscreen", "-exit"), "dos"),
					".exe", ".com", ".bat", ".conf"),
			},
		},
		{
			Name: "ScummVM", Exes: []string{"scummvm.exe"}, Scoop: []string{"scummvm"},
			Systems: []WindowsSystem{
				noScan(withExtensions(sys("ScummVMStandalone", systemdefs.SystemScummVM, ScummVMArgs, "scummvm"),
					".scummvm")),
			},
		},
		{
			Name: "Ruffle", Exes: []string{"ruffle.exe"}, Scoop: []string{"ruffle"},
			Systems: []WindowsSystem{
				withExtensions(sys("Ruffle", systemdefs.SystemPC, Batch("--fullscreen"), "flash"), ".swf"),
			},
		},
	}
}

// FBNeoArgs launches a ROM set by name. FinalBurn Neo has no command line
// ROM path and only finds sets through the ROM folders in its own config, so
// the set's folder is added there first when it is missing.
func FBNeoArgs(ctx *Context) ([]string, error) {
	name, err := SetName(ctx.Path, ".zip", ".7z")
	if err != nil {
		return nil, err
	}
	if ctx.Exe != "" {
		if err := ensureFBNeoROMPath(ctx.Exe, filepath.Dir(ctx.Path)); err != nil {
			return nil, err
		}
	}
	return []string{name}, nil
}

func demulSetArgs(mode string) ArgsFunc {
	return func(ctx *Context) ([]string, error) {
		name, err := SetName(ctx.Path, ".zip", ".7z")
		if err != nil {
			return nil, err
		}
		return []string{"-run=" + mode, "-rom=" + name}, nil
	}
}

// A7800Args loads a cartridge into the a7800 machine. A7800 runs from its own
// folder, so it finds the 7800 BIOS in its roms folder.
func A7800Args(ctx *Context) ([]string, error) {
	return []string{"a7800", "-cart1", ctx.Path}, nil
}

// WinUAEArgs boots a floppy image in df0: without the configuration GUI.
func WinUAEArgs(ctx *Context) ([]string, error) {
	return []string{"-0", ctx.Path, "-s", "use_gui=no"}, nil
}
