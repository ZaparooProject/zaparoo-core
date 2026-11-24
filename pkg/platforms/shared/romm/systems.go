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

package romm

import (
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

// slugSystems maps RomM's platform slugs to Zaparoo systems. RomM's slugs are
// fixed by its platform list (backend/utils/platform_slugs.py) whatever the
// user names the folder. Platforms that are not ROM-based, such as PC
// storefronts and phone operating systems, are absent: their games are not
// files a launcher here can open.
//
//nolint:gochecknoglobals // Static mapping table.
var slugSystems = map[string]string{
	"3do":                         systemdefs.System3DO,
	"3ds":                         systemdefs.System3DS,
	"new-nintendo-3ds":            systemdefs.System3DS,
	"acorn-archimedes":            systemdefs.SystemArchimedes,
	"acorn-electron":              systemdefs.SystemAcornElectron,
	"acpc":                        systemdefs.SystemAmstrad,
	"adventure-vision":            systemdefs.SystemAdventureVision,
	"amiga":                       systemdefs.SystemAmiga,
	"amiga-cd32":                  systemdefs.SystemAmigaCD32,
	"amstrad-gx4000":              systemdefs.SystemAmstradGX4000,
	"amstrad-pcw":                 systemdefs.SystemAmstradPCW,
	"apple-iigs":                  systemdefs.SystemAppleIIGS,
	"appleii":                     systemdefs.SystemAppleII,
	"aquarius":                    systemdefs.SystemAquarius,
	"arcade":                      systemdefs.SystemArcade,
	"arcadia-2001":                systemdefs.SystemArcadia,
	"arduboy":                     systemdefs.SystemArduboy,
	"astrocade":                   systemdefs.SystemAstrocade,
	"atari-jaguar-cd":             systemdefs.SystemJaguarCD,
	"atari-st":                    systemdefs.SystemAtariST,
	"atari-xegs":                  systemdefs.SystemAtariXEGS,
	"atari2600":                   systemdefs.SystemAtari2600,
	"atari-vcs":                   systemdefs.SystemAtari2600,
	"atari5200":                   systemdefs.SystemAtari5200,
	"atari7800":                   systemdefs.SystemAtari7800,
	"atari800":                    systemdefs.SystemAtari800,
	"atari8bit":                   systemdefs.SystemAtari800,
	"atom":                        systemdefs.SystemAcornAtom,
	"atomiswave":                  systemdefs.SystemAtomiswave,
	"bbcmicro":                    systemdefs.SystemBBCMicro,
	"c-plus-4":                    systemdefs.SystemCommodorePlus4,
	"c128":                        systemdefs.SystemC128,
	"c16":                         systemdefs.SystemC16,
	"c64":                         systemdefs.SystemC64,
	"casio-loopy":                 systemdefs.SystemCasioLoopy,
	"casio-pv-1000":               systemdefs.SystemCasioPV1000,
	"casio-pv-2000":               systemdefs.SystemCasioPV2000,
	"colecoadam":                  systemdefs.SystemColecoAdam,
	"colecovision":                systemdefs.SystemColecoVision,
	"commander-x16":               systemdefs.SystemCommanderX16,
	"commodore-cdtv":              systemdefs.SystemCommodoreCDTV,
	"cpet":                        systemdefs.SystemPET2001,
	"creativision":                systemdefs.SystemCreatiVision,
	"daphne":                      systemdefs.SystemDAPHNE,
	"dc":                          systemdefs.SystemDreamcast,
	"dos":                         systemdefs.SystemDOS,
	"dragon-32-slash-64":          systemdefs.SystemDragon32,
	"enterprise":                  systemdefs.SystemEnterprise,
	"epoch-cassette-vision":       systemdefs.SystemCassetteVision,
	"epoch-game-pocket-computer":  systemdefs.SystemGamePocket,
	"epoch-super-cassette-vision": systemdefs.SystemSuperCassetteVision,
	"evercade":                    systemdefs.SystemEvercade,
	"fairchild-channel-f":         systemdefs.SystemChannelF,
	"famicom":                     systemdefs.SystemNES,
	"fba":                         systemdefs.SystemArcade,
	"fbneo":                       systemdefs.SystemArcade,
	"fds":                         systemdefs.SystemFDS,
	"fm-7":                        systemdefs.SystemFM7,
	"fm-towns":                    systemdefs.SystemFMTowns,
	"g-and-w":                     systemdefs.SystemGameNWatch,
	"gamate":                      systemdefs.SystemGamate,
	"game-dot-com":                systemdefs.SystemGameCom,
	"gamegear":                    systemdefs.SystemGameGear,
	"gb":                          systemdefs.SystemGameboy,
	"gba":                         systemdefs.SystemGBA,
	"gbc":                         systemdefs.SystemGameboyColor,
	"genesis":                     systemdefs.SystemGenesis,
	"gp32":                        systemdefs.SystemGP32,
	"hyperscan":                   systemdefs.SystemHyperScan,
	"intellivision":               systemdefs.SystemIntellivision,
	"jaguar":                      systemdefs.SystemJaguar,
	"jupiter-ace":                 systemdefs.SystemJupiter,
	"laseractive":                 systemdefs.SystemLaserActive,
	"leapster":                    systemdefs.SystemLeapster,
	"lynx":                        systemdefs.SystemAtariLynx,
	"mame":                        systemdefs.SystemArcade,
	"mega-duck-slash-cougar-boy":  systemdefs.SystemMegaDuck,
	"microvision":                 systemdefs.SystemMicrovision,
	"model2":                      systemdefs.SystemModel2,
	"model3":                      systemdefs.SystemModel3,
	"msx":                         systemdefs.SystemMSX,
	"msx2":                        systemdefs.SystemMSX2,
	"msx2plus":                    systemdefs.SystemMSX2Plus,
	"multivision":                 systemdefs.SystemMultivision,
	"n64":                         systemdefs.SystemNintendo64,
	"naomi":                       systemdefs.SystemNAOMI,
	"naomi2":                      systemdefs.SystemNAOMI2,
	"nds":                         systemdefs.SystemNDS,
	"nintendo-dsi":                systemdefs.SystemNDS,
	"neo-geo-cd":                  systemdefs.SystemNeoGeoCD,
	"neo-geo-pocket":              systemdefs.SystemNeoGeoPocket,
	"neo-geo-pocket-color":        systemdefs.SystemNeoGeoPocketColor,
	"neogeoaes":                   systemdefs.SystemNeoGeoAES,
	"neogeomvs":                   systemdefs.SystemNeoGeoMVS,
	"nes":                         systemdefs.SystemNES,
	"ngage":                       systemdefs.SystemNGage,
	"ngc":                         systemdefs.SystemGameCube,
	"nuon":                        systemdefs.SystemNuon,
	"odyssey":                     systemdefs.SystemMagnavoxOdyssey,
	"odyssey-2":                   systemdefs.SystemOdyssey2,
	"openbor":                     systemdefs.SystemOpenBOR,
	"orao":                        systemdefs.SystemOrao,
	"oric":                        systemdefs.SystemOric,
	"pc-8800-series":              systemdefs.SystemPC88,
	"pc-9800-series":              systemdefs.SystemPC98,
	"pc-fx":                       systemdefs.SystemPCFX,
	"philips-cd-i":                systemdefs.SystemCDI,
	"pico":                        systemdefs.SystemPico8,
	"playdate":                    systemdefs.SystemPlaydate,
	"pocket-challenge-v2":         systemdefs.SystemPocketChallengeV2,
	"pocketstation":               systemdefs.SystemPocketStation,
	"pokemon-mini":                systemdefs.SystemPokemonMini,
	"ps2":                         systemdefs.SystemPS2,
	"ps3":                         systemdefs.SystemPS3,
	"psp":                         systemdefs.SystemPSP,
	"psvita":                      systemdefs.SystemVita,
	"psx":                         systemdefs.SystemPSX,
	"rca-studio-ii":               systemdefs.SystemStudioII,
	"sam-coupe":                   systemdefs.SystemSAMCoupe,
	"saturn":                      systemdefs.SystemSaturn,
	"scummvm":                     systemdefs.SystemScummVM,
	"sega-pico":                   systemdefs.SystemSegaPico,
	"sega32":                      systemdefs.SystemSega32X,
	"segacd":                      systemdefs.SystemMegaCD,
	"segacd32":                    systemdefs.SystemSegaCD32X,
	"sfam":                        systemdefs.SystemSNES,
	"sg1000":                      systemdefs.SystemSG1000,
	"sharp-x68000":                systemdefs.SystemX68000,
	"sinclair-ql":                 systemdefs.SystemQL,
	"singe":                       systemdefs.SystemSinge,
	"sms":                         systemdefs.SystemMasterSystem,
	"snes":                        systemdefs.SystemSNES,
	"socrates":                    systemdefs.SystemSocrates,
	"sord-m5":                     systemdefs.SystemSordM5,
	"spectravideo":                systemdefs.SystemSpectravideo,
	"stv":                         systemdefs.SystemSegaSTV,
	"sufami-turbo":                systemdefs.SystemSufami,
	"super-acan":                  systemdefs.SystemSuperACan,
	"super-vision-8000":           systemdefs.SystemSuperVision8000,
	"supergrafx":                  systemdefs.SystemSuperGrafx,
	"supervision":                 systemdefs.SystemSuperVision,
	"switch":                      systemdefs.SystemSwitch,
	"tatung-einstein":             systemdefs.SystemTatungEinstein,
	"tg16":                        systemdefs.SystemTurboGrafx16,
	"thomson-mo5":                 systemdefs.SystemThomson,
	"thomson-to":                  systemdefs.SystemThomson,
	"ti-994a":                     systemdefs.SystemTI994A,
	"tic-80":                      systemdefs.SystemTIC80,
	"tomy-tutor":                  systemdefs.SystemTomyTutor,
	"triforce":                    systemdefs.SystemTriforce,
	"trs-80":                      systemdefs.SystemTRS80,
	"trs-80-color-computer":       systemdefs.SystemCoCo2,
	"turbografx-cd":               systemdefs.SystemTurboGrafx16CD,
	"uzebox":                      systemdefs.SystemUzebox,
	"vc-4000":                     systemdefs.SystemVC4000,
	"interton-vc-4000":            systemdefs.SystemVC4000,
	"vectrex":                     systemdefs.SystemVectrex,
	"vic-20":                      systemdefs.SystemVIC20,
	"videopac-g7400":              systemdefs.SystemVideopacPlus,
	"virtualboy":                  systemdefs.SystemVirtualBoy,
	"vmu":                         systemdefs.SystemVMU,
	"vsmile":                      systemdefs.SystemVSmile,
	"wii":                         systemdefs.SystemWii,
	"wiiu":                        systemdefs.SystemWiiU,
	"wonderswan":                  systemdefs.SystemWonderSwan,
	"wonderswan-color":            systemdefs.SystemWonderSwanColor,
	"x1":                          systemdefs.SystemX1,
	"xbox":                        systemdefs.SystemXbox,
	"xbox360":                     systemdefs.SystemXbox360,
	"z-machine":                   systemdefs.SystemZMachine,
	"zeebo":                       systemdefs.SystemZeebo,
	"zx-spectrum-next":            systemdefs.SystemZXNext,
	"zx81":                        systemdefs.SystemZX81,
	"zxs":                         systemdefs.SystemZXSpectrum,
}

// SystemForPlatform returns the Zaparoo system a RomM platform's ROMs index
// under. The universal slug decides; the folder slug is a fallback for a
// custom platform RomM could not identify, matched against Zaparoo's own
// system IDs and aliases the way a folder name would be.
func SystemForPlatform(slug, fsSlug string) (string, bool) {
	if id, ok := slugSystems[strings.ToLower(strings.TrimSpace(slug))]; ok {
		return id, true
	}
	fsSlug = strings.TrimSpace(fsSlug)
	if id, ok := slugSystems[strings.ToLower(fsSlug)]; ok {
		return id, true
	}
	if fsSlug == "" {
		return "", false
	}
	system, err := systemdefs.LookupSystem(fsSlug)
	if err != nil {
		return "", false
	}
	return system.ID, true
}

// PlatformSlugs lists, sorted, the RomM platform slugs whose ROMs index under
// systemID.
func PlatformSlugs(systemID string) []string {
	var out []string
	for slug, id := range slugSystems {
		if id == systemID {
			out = append(out, slug)
		}
	}
	slices.Sort(out)
	return out
}
