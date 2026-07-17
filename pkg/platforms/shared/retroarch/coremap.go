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
		PerProfileCore:   map[Profile]string{ProfileApplianceARM: "fbneo"},
		PerProfilePolicy: map[Profile]DownloadPolicy{ProfileApplianceARM: PolicyNonCommercial},
	},
	{SystemID: systemdefs.SystemAtari2600, DefaultCore: "stella", Policy: PolicyFree, ESFolder: "atari2600"},
	{SystemID: systemdefs.SystemAtari5200, DefaultCore: "atari800", Policy: PolicyFree, ESFolder: "atari5200"},
	{SystemID: systemdefs.SystemAtari7800, DefaultCore: "prosystem", Policy: PolicyFree, ESFolder: "atari7800"},
	{SystemID: systemdefs.SystemAtari800, DefaultCore: "atari800", Policy: PolicyFree, ESFolder: "atari800"},
	{SystemID: systemdefs.SystemAtariST, DefaultCore: "hatari", Policy: PolicyFree, ESFolder: "atarist"},
	{SystemID: systemdefs.SystemBBCMicro, DefaultCore: "b2", Policy: PolicyFree, ESFolder: "bbc"},
	{SystemID: systemdefs.SystemC64, DefaultCore: "vice_x128", Policy: PolicyFree, ESFolder: "c128"},
	{SystemID: systemdefs.SystemVIC20, DefaultCore: "vice_xvic", Policy: PolicyFree, ESFolder: "c20"},
	{SystemID: systemdefs.SystemC64, DefaultCore: "vice_x64", Policy: PolicyFree, ESFolder: "c64"},
	{SystemID: systemdefs.SystemChannelF, DefaultCore: "freechaf", Policy: PolicyFree, ESFolder: "channelf"},
	{SystemID: systemdefs.SystemColecoVision, DefaultCore: "bluemsx", Policy: PolicyFree, ESFolder: "colecovision"},
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
	{SystemID: systemdefs.SystemAtariLynx, DefaultCore: "handy", Policy: PolicyFree, ESFolder: "lynx"},
	{SystemID: systemdefs.SystemArcade, DefaultCore: "mame", Policy: PolicyFree, ESFolder: "mame"},
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
	{SystemID: systemdefs.SystemMSX, DefaultCore: "bluemsx", Policy: PolicyFree, ESFolder: "msx2"},
	{SystemID: systemdefs.SystemMSX2Plus, DefaultCore: "bluemsx", Policy: PolicyFree, ESFolder: "msx2+"},
	{
		SystemID: systemdefs.SystemNintendo64, DefaultCore: "mupen64plus_next", Policy: PolicyFree,
		ESFolder: "n64", PerProfileCore: map[Profile]string{ProfileApplianceARM: ""},
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
	},
	{
		SystemID: systemdefs.SystemTurboGrafx16CD, DefaultCore: "mednafen_pce_fast",
		Policy: PolicyFree, ESFolder: "pcenginecd",
	},
	{SystemID: systemdefs.SystemPCFX, DefaultCore: "mednafen_pcfx", Policy: PolicyFree, ESFolder: "pcfx"},
	{SystemID: systemdefs.SystemPET2001, DefaultCore: "vice_xpet", Policy: PolicyFree, ESFolder: "pet"},
	{SystemID: systemdefs.SystemPokemonMini, DefaultCore: "pokemini", Policy: PolicyFree, ESFolder: "pokemini"},
	{SystemID: systemdefs.SystemDOS, DefaultCore: "prboom", Policy: PolicyFree, ESFolder: "prboom"},
	{
		SystemID: systemdefs.SystemPSP, DefaultCore: "ppsspp", Policy: PolicyFree, ESFolder: "psp",
		PerProfileCore: map[Profile]string{ProfileApplianceARM: ""},
	},
	{
		SystemID: systemdefs.SystemPSX, DefaultCore: "mednafen_psx_hw", Policy: PolicyFree, ESFolder: "psx",
		PerProfileCore: map[Profile]string{ProfileApplianceARM: "pcsx_rearmed"},
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
		if ok {
			launches = append(launches, launch)
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
