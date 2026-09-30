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

package retroarch

import (
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
)

// coreDefinitions is ordered so launcher precedence remains deterministic.
// Core names match libretro buildbot filenames without the _libretro.so suffix.
//
//nolint:gochecknoglobals // Static launcher data.
var coreDefinitions = []CoreDef{
	{SystemID: systemdefs.System3DO, DefaultCore: "opera", Policy: PolicyNonCommercial, ESFolder: "3do"},
	{SystemID: systemdefs.SystemAmiga1200, DefaultCore: "puae", Policy: PolicyFree, ESFolder: "amiga1200"},
	{SystemID: systemdefs.SystemAmiga500, DefaultCore: "puae", Policy: PolicyFree, ESFolder: "amiga500"},
	{SystemID: systemdefs.SystemAmigaCD32, DefaultCore: "puae", Policy: PolicyFree, ESFolder: "amigacd32"},
	{SystemID: systemdefs.SystemCommodoreCDTV, DefaultCore: "puae", Policy: PolicyFree, ESFolder: "amigacdtv"},
	{SystemID: systemdefs.SystemAmstrad, DefaultCore: "cap32", Policy: PolicyFree, ESFolder: "amstradcpc"},
	{SystemID: systemdefs.SystemAmstradGX4000, DefaultCore: "cap32", Policy: PolicyFree, ESFolder: "gx4000"},
	{SystemID: systemdefs.SystemAppleII, DefaultCore: "applewin", Policy: PolicyFree, ESFolder: "apple2"},
	{
		SystemID: systemdefs.SystemArcade, DefaultCore: "mame", Policy: PolicyFree, ESFolder: "arcade",
		PerProfileCore: map[Profile]string{
			ProfileApplianceARM: "fbneo", ProfileAndroid: "fbneo",
		},
		PerProfilePolicy: map[Profile]DownloadPolicy{
			ProfileApplianceARM: PolicyNonCommercial, ProfileAndroid: PolicyNonCommercial,
		},
	},
	{SystemID: systemdefs.SystemAtari2600, DefaultCore: "stella", Policy: PolicyFree, ESFolder: "atari2600"},
	{SystemID: systemdefs.SystemAtari5200, DefaultCore: "atari800", Policy: PolicyFree, ESFolder: "atari5200"},
	{SystemID: systemdefs.SystemAtari7800, DefaultCore: "prosystem", Policy: PolicyFree, ESFolder: "atari7800"},
	{SystemID: systemdefs.SystemAtari800, DefaultCore: "atari800", Policy: PolicyFree, ESFolder: "atari800"},
	{SystemID: systemdefs.SystemAtariST, DefaultCore: "hatari", Policy: PolicyFree, ESFolder: "atarist"},
	{SystemID: systemdefs.SystemBBCMicro, DefaultCore: "b2", Policy: PolicyFree, ESFolder: "bbc"},
	{
		SystemID: systemdefs.SystemC64, DefaultCore: "vice_x128", Policy: PolicyFree, ESFolder: "c128",
		// Android doesn't distinguish a separate C128 folder/system; its C64
		// alternates (below) already attach to the "c64" folder entry.
		PerProfileCore: map[Profile]string{ProfileAndroid: ""},
	},
	{SystemID: systemdefs.SystemVIC20, DefaultCore: "vice_xvic", Policy: PolicyFree, ESFolder: "c20"},
	{SystemID: systemdefs.SystemC64, DefaultCore: "vice_x64", Policy: PolicyFree, ESFolder: "c64"},
	{SystemID: systemdefs.SystemChannelF, DefaultCore: "freechaf", Policy: PolicyFree, ESFolder: "channelf"},
	{
		SystemID: systemdefs.SystemColecoVision, DefaultCore: "bluemsx", Policy: PolicyFree, ESFolder: "colecovision",
		PerProfileCore: map[Profile]string{ProfileAndroid: "gearcoleco"},
	},
	{SystemID: systemdefs.SystemCommodorePlus4, DefaultCore: "vice_xplus4", Policy: PolicyFree, ESFolder: "cplus4"},
	{SystemID: systemdefs.SystemCPS1, DefaultCore: "fbneo", Policy: PolicyNonCommercial, ESFolder: "cps1"},
	{SystemID: systemdefs.SystemCPS2, DefaultCore: "fbneo", Policy: PolicyNonCommercial, ESFolder: "cps2"},
	{SystemID: systemdefs.SystemCPS3, DefaultCore: "fbneo", Policy: PolicyNonCommercial, ESFolder: "cps3"},
	{SystemID: systemdefs.SystemDOS, DefaultCore: "dosbox_pure", Policy: PolicyFree, ESFolder: "dos"},
	{
		SystemID: systemdefs.SystemDreamcast, DefaultCore: "flycast", Policy: PolicyFree,
		ESFolder: "dreamcast", PerProfileCore: map[Profile]string{ProfileApplianceARM: ""},
	},
	{
		SystemID: systemdefs.SystemFDS, DefaultCore: "mesen", Policy: PolicyFree,
		ESFolder: "fds", PerProfileCore: map[Profile]string{ProfileApplianceARM: "fceumm"},
	},
	{SystemID: systemdefs.SystemGameNWatch, DefaultCore: "gw", Policy: PolicyFree, ESFolder: "gameandwatch"},
	{
		SystemID: systemdefs.SystemGameGear, DefaultCore: "genesis_plus_gx",
		Policy: PolicyNonCommercial, ESFolder: "gamegear",
	},
	{SystemID: systemdefs.SystemGameboy, DefaultCore: "gambatte", Policy: PolicyFree, ESFolder: "gb"},
	{SystemID: systemdefs.SystemGBA, DefaultCore: "mgba", Policy: PolicyFree, ESFolder: "gba"},
	{SystemID: systemdefs.SystemGameboyColor, DefaultCore: "gambatte", Policy: PolicyFree, ESFolder: "gbc"},
	{SystemID: systemdefs.SystemIntellivision, DefaultCore: "freeintv", Policy: PolicyFree, ESFolder: "intellivision"},
	{SystemID: systemdefs.SystemJaguar, DefaultCore: "virtualjaguar", Policy: PolicyFree, ESFolder: "jaguar"},
	{
		SystemID: systemdefs.SystemAtariLynx, DefaultCore: "handy", Policy: PolicyFree, ESFolder: "lynx",
		PerProfileCore: map[Profile]string{ProfileAndroid: "mednafen_lynx"},
	},
	{
		SystemID: systemdefs.SystemArcade, DefaultCore: "mame", Policy: PolicyFree, ESFolder: "mame",
		// Android doesn't distinguish a separate "mame" folder/system; its
		// Arcade alternates (below) already attach to the "arcade" folder entry.
		PerProfileCore: map[Profile]string{ProfileAndroid: ""},
	},
	{
		SystemID: systemdefs.SystemMasterSystem, DefaultCore: "genesis_plus_gx",
		Policy: PolicyNonCommercial, ESFolder: "mastersystem",
	},
	{
		SystemID: systemdefs.SystemMegaCD, DefaultCore: "genesis_plus_gx",
		Policy: PolicyNonCommercial, ESFolder: "megacd",
	},
	{
		SystemID: systemdefs.SystemGenesis, DefaultCore: "genesis_plus_gx",
		Policy: PolicyNonCommercial, ESFolder: "megadrive",
		PerProfileCore:   map[Profile]string{ProfileApplianceARM: "clownmdemu"},
		PerProfilePolicy: map[Profile]DownloadPolicy{ProfileApplianceARM: PolicyFree},
	},
	{
		SystemID: systemdefs.SystemSegaPico, DefaultCore: "genesis_plus_gx",
		Policy: PolicyNonCommercial, ESFolder: "pico",
	},
	{SystemID: systemdefs.SystemMSX, DefaultCore: "bluemsx", Policy: PolicyFree, ESFolder: "msx1"},
	{
		SystemID: systemdefs.SystemMSX, DefaultCore: "bluemsx", Policy: PolicyFree, ESFolder: "msx2",
		// Android doesn't distinguish a separate MSX2 folder/system; its MSX
		// alternates (below) already attach to the "msx1" folder entry.
		PerProfileCore: map[Profile]string{ProfileAndroid: ""},
	},
	{SystemID: systemdefs.SystemMSX2Plus, DefaultCore: "bluemsx", Policy: PolicyFree, ESFolder: "msx2+"},
	{
		SystemID: systemdefs.SystemNintendo64, DefaultCore: "mupen64plus_next", Policy: PolicyFree,
		ESFolder: "n64", PerProfileCore: map[Profile]string{
			ProfileApplianceARM: "", ProfileAndroid: "mupen64plus_next_gles3",
		},
	},
	{
		SystemID: systemdefs.SystemNDS, DefaultCore: "melondsds", Policy: PolicyFree, ESFolder: "nds",
		PerProfileCore: map[Profile]string{ProfileApplianceARM: ""},
	},
	{SystemID: systemdefs.SystemNeoGeo, DefaultCore: "fbneo", Policy: PolicyNonCommercial, ESFolder: "neogeo"},
	{SystemID: systemdefs.SystemNeoGeoCD, DefaultCore: "neocd", Policy: PolicyFree, ESFolder: "neogeocd"},
	{
		SystemID: systemdefs.SystemNES, DefaultCore: "mesen", Policy: PolicyFree,
		ESFolder: "nes", PerProfileCore: map[Profile]string{ProfileApplianceARM: "fceumm"},
	},
	{SystemID: systemdefs.SystemNeoGeoPocket, DefaultCore: "mednafen_ngp", Policy: PolicyFree, ESFolder: "ngp"},
	{
		SystemID: systemdefs.SystemNeoGeoPocketColor, DefaultCore: "mednafen_ngp",
		Policy: PolicyFree, ESFolder: "ngpc",
	},
	{SystemID: systemdefs.SystemOdyssey2, DefaultCore: "o2em", Policy: PolicyFree, ESFolder: "odyssey2"},
	{SystemID: systemdefs.SystemPC88, DefaultCore: "quasi88", Policy: PolicyFree, ESFolder: "pc88"},
	{SystemID: systemdefs.SystemPC98, DefaultCore: "np2kai", Policy: PolicyFree, ESFolder: "pc98"},
	{
		SystemID: systemdefs.SystemTurboGrafx16, DefaultCore: "mednafen_pce_fast",
		Policy: PolicyFree, ESFolder: "pcengine",
		PerProfileCore: map[Profile]string{ProfileAndroid: "mednafen_pce"},
	},
	{
		SystemID: systemdefs.SystemTurboGrafx16CD, DefaultCore: "mednafen_pce_fast",
		Policy: PolicyFree, ESFolder: "pcenginecd",
		PerProfileCore: map[Profile]string{ProfileAndroid: "mednafen_pce"},
	},
	{SystemID: systemdefs.SystemPCFX, DefaultCore: "mednafen_pcfx", Policy: PolicyFree, ESFolder: "pcfx"},
	{SystemID: systemdefs.SystemPET2001, DefaultCore: "vice_xpet", Policy: PolicyFree, ESFolder: "pet"},
	{SystemID: systemdefs.SystemPokemonMini, DefaultCore: "pokemini", Policy: PolicyFree, ESFolder: "pokemini"},
	{
		SystemID: systemdefs.SystemDOS, DefaultCore: "prboom", Policy: PolicyFree, ESFolder: "prboom",
		// Android doesn't distinguish a separate PrBoom folder/system; it lists
		// prboom as one of its "DOS" alternates (below), attached to the "dos"
		// folder entry.
		PerProfileCore: map[Profile]string{ProfileAndroid: ""},
	},
	{
		SystemID: systemdefs.SystemPSP, DefaultCore: "ppsspp", Policy: PolicyFree, ESFolder: "psp",
		PerProfileCore: map[Profile]string{ProfileApplianceARM: ""},
	},
	{
		SystemID: systemdefs.SystemPSX, DefaultCore: "mednafen_psx_hw", Policy: PolicyFree, ESFolder: "psx",
		PerProfileCore: map[Profile]string{
			ProfileApplianceARM: "pcsx_rearmed", ProfileAndroid: "swanstation",
		},
	},
	{
		SystemID: systemdefs.SystemSaturn, DefaultCore: "mednafen_saturn", Policy: PolicyFree, ESFolder: "saturn",
		PerProfileCore: map[Profile]string{ProfileApplianceARM: ""},
	},
	{
		SystemID: systemdefs.SystemScummVM, DefaultCore: "scummvm",
		Policy: PolicyFree, ESFolder: "scummvm",
	},
	{SystemID: systemdefs.SystemSega32X, DefaultCore: "picodrive", Policy: PolicyNonCommercial, ESFolder: "sega32x"},
	{
		SystemID: systemdefs.SystemSG1000, DefaultCore: "genesis_plus_gx",
		Policy: PolicyNonCommercial, ESFolder: "sg1000",
	},
	{
		SystemID: systemdefs.SystemSNES, DefaultCore: "snes9x",
		Policy: PolicyNonCommercial, ESFolder: "snes",
	},
	{
		SystemID: systemdefs.SystemSuperGrafx, DefaultCore: "mednafen_supergrafx",
		Policy: PolicyFree, ESFolder: "supergrafx",
	},
	{SystemID: systemdefs.SystemTIC80, DefaultCore: "tic80", Policy: PolicyFree, ESFolder: "tic80"},
	{SystemID: systemdefs.SystemVectrex, DefaultCore: "vecx", Policy: PolicyFree, ESFolder: "vectrex"},
	{SystemID: systemdefs.SystemVirtualBoy, DefaultCore: "mednafen_vb", Policy: PolicyFree, ESFolder: "virtualboy"},
	{SystemID: systemdefs.SystemWonderSwan, DefaultCore: "mednafen_wswan", Policy: PolicyFree, ESFolder: "wswan"},
	{SystemID: systemdefs.SystemWonderSwanColor, DefaultCore: "mednafen_wswan", Policy: PolicyFree, ESFolder: "wswanc"},
	{SystemID: systemdefs.SystemX68000, DefaultCore: "px68k", Policy: PolicyFree, ESFolder: "x68000"},
	{SystemID: systemdefs.SystemZX81, DefaultCore: "81", Policy: PolicyFree, ESFolder: "zx81"},
	{SystemID: systemdefs.SystemZXSpectrum, DefaultCore: "fuse", Policy: PolicyFree, ESFolder: "zxspectrum"},

	// The systems below were absent from this table before Android's own
	// catalog (pkg/platforms/android/catalog) was generalized onto it
	// (2026-09-30): real libretro cores desktop platforms simply hadn't
	// picked up yet, not systems unique to Android. Arcade-board emulators
	// (flycast, supermodel, same_cdi) get PolicyNonCommercial to match the
	// existing CPS1/2/3/NeoGeo precedent; everything else is PolicyFree.
	{SystemID: systemdefs.System3DS, DefaultCore: "azahar", Policy: PolicyFree, ESFolder: "3ds"},
	{SystemID: systemdefs.SystemArcadia, DefaultCore: "amiarcadia", Policy: PolicyFree, ESFolder: "arcadia"},
	{SystemID: systemdefs.SystemArduboy, DefaultCore: "ardens", Policy: PolicyFree, ESFolder: "arduboy"},
	{
		SystemID: systemdefs.SystemAtomiswave, DefaultCore: "flycast",
		Policy: PolicyNonCommercial, ESFolder: "atomiswave",
	},
	{SystemID: systemdefs.SystemCDI, DefaultCore: "same_cdi", Policy: PolicyNonCommercial, ESFolder: "cdi"},
	{SystemID: systemdefs.SystemGameCube, DefaultCore: "dolphin", Policy: PolicyFree, ESFolder: "gamecube"},
	{SystemID: systemdefs.SystemJ2ME, DefaultCore: "squirreljme", Policy: PolicyFree, ESFolder: "j2me"},
	{SystemID: systemdefs.SystemMacOS, DefaultCore: "minivmac", Policy: PolicyFree, ESFolder: "macintosh"},
	{SystemID: systemdefs.SystemMegaDuck, DefaultCore: "sameduck", Policy: PolicyFree, ESFolder: "megaduck"},
	{
		SystemID: systemdefs.SystemModel3, DefaultCore: "supermodel",
		Policy: PolicyNonCommercial, ESFolder: "model3",
	},
	{SystemID: systemdefs.SystemNAOMI, DefaultCore: "flycast", Policy: PolicyNonCommercial, ESFolder: "naomi"},
	{SystemID: systemdefs.SystemNAOMI2, DefaultCore: "flycast", Policy: PolicyNonCommercial, ESFolder: "naomi2"},
	{SystemID: systemdefs.SystemPS2, DefaultCore: "pcsx2", Policy: PolicyFree, ESFolder: "ps2"},
	{SystemID: systemdefs.SystemPico8, DefaultCore: "retro8", Policy: PolicyFree, ESFolder: "pico8"},
	{
		SystemID: systemdefs.SystemSuperCassetteVision, DefaultCore: "emuscv",
		Policy: PolicyFree, ESFolder: "scv",
	},
	{SystemID: systemdefs.SystemSuperVision, DefaultCore: "potator", Policy: PolicyFree, ESFolder: "supervision"},
	{SystemID: systemdefs.SystemThomson, DefaultCore: "theodore", Policy: PolicyFree, ESFolder: "thomson"},
	{SystemID: systemdefs.SystemUzebox, DefaultCore: "uzem", Policy: PolicyFree, ESFolder: "uzebox"},
	{SystemID: systemdefs.SystemWii, DefaultCore: "dolphin", Policy: PolicyFree, ESFolder: "wii"},
	{SystemID: systemdefs.SystemWiiU, DefaultCore: "cemu", Policy: PolicyFree, ESFolder: "wiiu"},
	{SystemID: systemdefs.SystemX1, DefaultCore: "x1", Policy: PolicyFree, ESFolder: "x1"},
}

// CoreDefinitions returns a defensive copy of the core table.
func CoreDefinitions() []CoreDef {
	defs := make([]CoreDef, len(coreDefinitions))
	for i := range coreDefinitions {
		defs[i] = coreDefinitions[i]
		defs[i].PerProfileCore = cloneProfileCores(coreDefinitions[i].PerProfileCore)
		defs[i].PerProfilePolicy = cloneProfilePolicies(coreDefinitions[i].PerProfilePolicy)
	}
	return defs
}

// CoreLaunches builds launch metadata for profile in deterministic order.
func CoreLaunches(profile Profile) []CoreLaunch {
	launches := make([]CoreLaunch, 0, len(coreDefinitions)+len(alternateCoreLaunches))
	coreCounts := selectedCoreCounts(profile)
	for i := range coreDefinitions {
		launch, ok := coreLaunchForDef(&coreDefinitions[i], profile, coreCounts)
		if !ok {
			continue
		}
		launches = append(launches, launch)
		if profile == ProfileAndroid {
			launches = append(launches, androidAlternateLaunches(coreDefinitions[i].SystemID, &launch, coreCounts)...)
		}
	}
	for i := range alternateCoreLaunches {
		launch, ok := alternateCoreLaunches[i].forProfile(profile)
		if !ok {
			continue
		}
		if scanSpec, found := scanSpecForSystem(profile, launch.SystemID, coreCounts); found {
			launch.Folders = scanSpec.Folders
			launch.Extensions = scanSpec.Extensions
			launch.Scan = true
		}
		launches = append(launches, launch)
	}
	return launches
}

func scanSpecForSystem(profile Profile, systemID string, coreCounts map[string]int) (CoreLaunch, bool) {
	for i := range coreDefinitions {
		if coreDefinitions[i].SystemID == systemID {
			return coreLaunchForDef(&coreDefinitions[i], profile, coreCounts)
		}
	}
	return CoreLaunch{}, false
}

// CoreLaunchForFolder returns launch metadata for one ES-DE folder.
func CoreLaunchForFolder(profile Profile, folder string) (CoreLaunch, bool) {
	for i := range coreDefinitions {
		if strings.EqualFold(coreDefinitions[i].ESFolder, folder) {
			return coreLaunchForDef(&coreDefinitions[i], profile, selectedCoreCounts(profile))
		}
	}
	return CoreLaunch{}, false
}

// CorePolicyForFolder returns the selected core's download policy.
func CorePolicyForFolder(profile Profile, folder string) (DownloadPolicy, bool) {
	for i := range coreDefinitions {
		def := coreDefinitions[i]
		if !strings.EqualFold(def.ESFolder, folder) {
			continue
		}
		if _, ok := selectedCore(&def, profile); !ok {
			return "", false
		}
		if policy, ok := def.PerProfilePolicy[profile]; ok {
			return policy, true
		}
		return def.Policy, true
	}
	return "", false
}

func coreLaunchForDef(def *CoreDef, profile Profile, coreCounts map[string]int) (CoreLaunch, bool) {
	core, ok := selectedCore(def, profile)
	if !ok {
		return CoreLaunch{}, false
	}
	info, ok := esde.LookupByFolderName(def.ESFolder)
	if !ok {
		return CoreLaunch{}, false
	}
	filename, err := normalizeCoreFilename(core)
	if err != nil {
		return CoreLaunch{}, false
	}
	return CoreLaunch{
		ID:         coreLauncherID(filename, def.SystemID, def.ESFolder, coreCounts[filename]),
		SystemID:   def.SystemID,
		Core:       filename,
		Folders:    []string{def.ESFolder},
		Extensions: append([]string(nil), info.Extensions...),
		Scan:       true,
	}, true
}

type alternateCoreLaunch struct {
	Profiles []Profile
	CoreLaunch
}

// alternateCoreLaunches mirrors MiSTer's non-scanning alternate-core launcher
// registrations. System defaults select these launchers; indexed media remains
// owned by the scanning default launcher for that system.
var alternateCoreLaunches = []alternateCoreLaunch{
	{
		CoreLaunch: CoreLaunch{
			ID: "RetroArchBSNES", SystemID: systemdefs.SystemSNES, Core: "bsnes_libretro.so",
		},
		Profiles: []Profile{ProfileDesktop},
	},
	{
		CoreLaunch: CoreLaunch{
			ID: "RetroArchFCEUMM", SystemID: systemdefs.SystemNES, Core: "fceumm_libretro.so",
		},
		Profiles: []Profile{ProfileDesktop},
	},
}

func (a *alternateCoreLaunch) forProfile(profile Profile) (CoreLaunch, bool) {
	for _, supported := range a.Profiles {
		if supported == profile {
			return cloneCoreLaunch(&a.CoreLaunch), true
		}
	}
	return CoreLaunch{}, false
}

// androidAlternates names the extra cores Android's own catalog offers for a
// system beyond whichever core CoreLaunches(ProfileAndroid) already resolves
// as that system's default (via DefaultCore or a PerProfileCore[ProfileAndroid]
// override). Cores are listed in Android's own documented precedence order
// (pkg/platforms/android/catalog/README.md, "Order is precedence") -
// transcribed from that catalog's JSON array order, not re-derived.
type androidAlternates struct {
	SystemID string
	Cores    []string
}

//nolint:gochecknoglobals // Static launcher data mirroring Android's own catalog precedence.
var androidCoreAlternates = []androidAlternates{
	{SystemID: systemdefs.System3DS, Cores: []string{"citra2018", "citra", "panda3ds"}},
	{SystemID: systemdefs.SystemAmiga1200, Cores: []string{"amiberry", "puae2021"}},
	{SystemID: systemdefs.SystemAmiga500, Cores: []string{"amiberry", "puae2021"}},
	{SystemID: systemdefs.SystemAmigaCD32, Cores: []string{"amiberry", "puae2021"}},
	{SystemID: systemdefs.SystemAmstrad, Cores: []string{"crocods", "ep128emu_core"}},
	{
		SystemID: systemdefs.SystemArcade,
		Cores: []string{
			"mame2003_plus", "mamearcade", "fbalpha2012", "hbmame", "mame2000", "mame2003",
			"mame2003_midway", "mame2010", "mame2015", "mame2016", "mamemess", "same_cdi",
		},
	},
	{SystemID: systemdefs.SystemArduboy, Cores: []string{"arduous"}},
	{SystemID: systemdefs.SystemAtari2600, Cores: []string{"stella2014", "stella2023", "tia"}},
	{SystemID: systemdefs.SystemAtari5200, Cores: []string{"a5200"}},
	{SystemID: systemdefs.SystemAtariLynx, Cores: []string{"handy", "gearlynx", "holani"}},
	{SystemID: systemdefs.SystemAtariST, Cores: []string{"hatari2014", "hatarib"}},
	{SystemID: systemdefs.SystemC64, Cores: []string{"frodo", "vice_x64sc", "vice_xscpu64"}},
	{SystemID: systemdefs.SystemCPS1, Cores: []string{"fbalpha2012_cps1"}},
	{SystemID: systemdefs.SystemCPS2, Cores: []string{"fbalpha2012_cps2"}},
	{SystemID: systemdefs.SystemCPS3, Cores: []string{"fbalpha2012_cps3"}},
	{SystemID: systemdefs.SystemColecoVision, Cores: []string{"bluemsx", "jollycv"}},
	{SystemID: systemdefs.SystemCommodoreCDTV, Cores: []string{"amiberry", "puae2021"}},
	{SystemID: systemdefs.SystemDOS, Cores: []string{"prboom", "dosbox_core", "dosbox", "dosbox_svn"}},
	{SystemID: systemdefs.SystemFDS, Cores: []string{"fceumm", "fixnes", "mesen2", "nestopia", "rustynes"}},
	{
		SystemID: systemdefs.SystemGBA,
		Cores:    []string{"gpsp", "mednafen_gba", "mesen2", "meteor", "skyemu", "vbam", "vba_next"},
	},
	{
		SystemID: systemdefs.SystemGameGear,
		Cores:    []string{"blastem", "gearsystem", "genesis_plus_gx_wide", "picodrive", "smsplus"},
	},
	{
		SystemID: systemdefs.SystemGameboy,
		Cores: []string{
			"sameboy", "DoubleCherryGB", "fixgb", "gearboy", "irogb",
			"mesen-s", "mesen2", "mgba", "skyemu", "tgbdual", "vbam",
		},
	},
	{
		SystemID: systemdefs.SystemGameboyColor,
		Cores: []string{
			"sameboy", "DoubleCherryGB", "fixgb", "gearboy", "irogb",
			"mesen-s", "mesen2", "mgba", "skyemu", "tgbdual", "vbam",
		},
	},
	{
		SystemID: systemdefs.SystemGenesis,
		Cores:    []string{"blastem", "clownmdemu", "genesis_plus_gx_wide", "picodrive"},
	},
	{SystemID: systemdefs.SystemMSX, Cores: []string{"fmsx"}},
	{SystemID: systemdefs.SystemMSX2Plus, Cores: []string{"fmsx"}},
	{
		SystemID: systemdefs.SystemMasterSystem,
		Cores:    []string{"blastem", "gearsystem", "genesis_plus_gx_wide", "picodrive", "smsplus"},
	},
	{
		SystemID: systemdefs.SystemMegaCD,
		Cores:    []string{"blastem", "clownmdemu", "genesis_plus_gx_wide", "picodrive"},
	},
	{
		SystemID: systemdefs.SystemNDS,
		Cores:    []string{"melonds", "desmume2015", "desmume", "noods", "skyemu"},
	},
	{
		SystemID: systemdefs.SystemNES,
		Cores:    []string{"fceumm", "fixnes", "mesen2", "nestopia", "quicknes", "rustynes"},
	},
	{SystemID: systemdefs.SystemNeoGeo, Cores: []string{"fbalpha2012_neogeo", "geolith"}},
	{SystemID: systemdefs.SystemNeoGeoPocket, Cores: []string{"race"}},
	{SystemID: systemdefs.SystemNeoGeoPocketColor, Cores: []string{"race"}},
	{SystemID: systemdefs.SystemNintendo64, Cores: []string{"mupen64plus_next_gles2", "parallel_n64"}},
	{SystemID: systemdefs.SystemPC98, Cores: []string{"nekop2"}},
	{SystemID: systemdefs.SystemPS2, Cores: []string{"armsx2", "pcee2", "play"}},
	{SystemID: systemdefs.SystemPSX, Cores: []string{"mednafen_psx_hw", "mednafen_psx", "pcsx_rearmed"}},
	{
		SystemID: systemdefs.SystemSG1000,
		Cores:    []string{"blastem", "bluemsx", "gearsystem", "genesis_plus_gx_wide"},
	},
	{
		SystemID: systemdefs.SystemSNES,
		Cores: []string{
			"bsnes", "bsnes-jg", "bsnes2014_accuracy", "bsnes2014_balanced", "bsnes2014_performance",
			"bsnes_cplusplus98", "bsnes_hd_beta", "bsnes_mercury_accuracy", "bsnes_mercury_balanced",
			"bsnes_mercury_performance", "mednafen_snes", "mednafen_supafaust", "mesen-s", "mesen2",
			"snes9x2002", "snes9x2005", "snes9x2005_plus", "snes9x2010",
		},
	},
	{SystemID: systemdefs.SystemSaturn, Cores: []string{"yabasanshiro", "yabause", "ymir"}},
	{SystemID: systemdefs.SystemSega32X, Cores: []string{"blastem"}},
	{
		SystemID: systemdefs.SystemSegaPico,
		Cores:    []string{"blastem", "genesis_plus_gx_wide", "picodrive"},
	},
	{SystemID: systemdefs.SystemSuperGrafx, Cores: []string{"geargrafx", "mednafen_pce", "mesen2"}},
	{
		SystemID: systemdefs.SystemTurboGrafx16,
		Cores:    []string{"mednafen_pce_fast", "geargrafx", "mednafen_supergrafx", "mesen2"},
	},
	{
		SystemID: systemdefs.SystemTurboGrafx16CD,
		Cores:    []string{"mednafen_pce_fast", "geargrafx", "mednafen_supergrafx", "mesen2"},
	},
	{SystemID: systemdefs.SystemWonderSwan, Cores: []string{"mesen2"}},
	{SystemID: systemdefs.SystemWonderSwanColor, Cores: []string{"mesen2"}},
	{SystemID: systemdefs.SystemZXSpectrum, Cores: []string{"ep128emu_core"}},
}

// androidAlternateLaunches expands androidCoreAlternates into extra
// CoreLaunch entries for systemID, reusing that system's already-resolved
// scan folder/extensions (from its coreDefinitions entry) and coreCounts
// for the same launcher-ID disambiguation coreLaunchForDef uses. Returns
// nil for a system with no listed alternates.
func androidAlternateLaunches(systemID string, scanSpec *CoreLaunch, coreCounts map[string]int) []CoreLaunch {
	for i := range androidCoreAlternates {
		alt := &androidCoreAlternates[i]
		if alt.SystemID != systemID {
			continue
		}
		launches := make([]CoreLaunch, 0, len(alt.Cores))
		for _, core := range alt.Cores {
			filename, err := normalizeCoreFilename(core)
			if err != nil {
				continue
			}
			launches = append(launches, CoreLaunch{
				ID:         coreLauncherID(filename, systemID, scanSpec.Folders[0], coreCounts[filename]),
				SystemID:   systemID,
				Core:       filename,
				Folders:    append([]string(nil), scanSpec.Folders...),
				Extensions: append([]string(nil), scanSpec.Extensions...),
				Scan:       true,
			})
		}
		return launches
	}
	return nil
}

func selectedCoreCounts(profile Profile) map[string]int {
	counts := make(map[string]int, len(coreDefinitions))
	for i := range coreDefinitions {
		core, ok := selectedCore(&coreDefinitions[i], profile)
		if !ok {
			continue
		}
		filename, err := normalizeCoreFilename(core)
		if err == nil {
			counts[filename]++
		}
	}
	// ProfileAndroid alone lists more than one core per system, so its launcher
	// IDs must disambiguate against the full set, not just each system's default.
	if profile == ProfileAndroid {
		for i := range androidCoreAlternates {
			for _, core := range androidCoreAlternates[i].Cores {
				filename, err := normalizeCoreFilename(core)
				if err == nil {
					counts[filename]++
				}
			}
		}
	}
	return counts
}

func coreLauncherID(core, systemID, folder string, occurrences int) string {
	name := strings.TrimSuffix(core, "_libretro.so")
	name = strings.TrimSuffix(name, "_libretro")
	name = strings.ReplaceAll(name, "_", "")
	if name != "" {
		name = strings.ToUpper(name[:1]) + name[1:]
	}

	switch name {
	case "Snes9x":
		name = "SNES9x"
	case "Bsnes":
		name = "BSNES"
	case "Fceumm":
		name = "FCEUMM"
	case "Pcsxrearmed":
		name = "PCSXReARMed"
	case "Mgba":
		name = "mGBA"
	}
	if occurrences > 1 {
		name += systemID + folder
	}
	return "RetroArch" + name
}

func selectedCore(def *CoreDef, profile Profile) (string, bool) {
	if core, ok := def.PerProfileCore[profile]; ok {
		return core, core != ""
	}
	return def.DefaultCore, def.DefaultCore != ""
}

func cloneProfileCores(src map[Profile]string) map[Profile]string {
	if src == nil {
		return nil
	}
	dst := make(map[Profile]string, len(src))
	for profile, core := range src {
		dst[profile] = core
	}
	return dst
}

func cloneProfilePolicies(src map[Profile]DownloadPolicy) map[Profile]DownloadPolicy {
	if src == nil {
		return nil
	}
	dst := make(map[Profile]DownloadPolicy, len(src))
	for profile, policy := range src {
		dst[profile] = policy
	}
	return dst
}
