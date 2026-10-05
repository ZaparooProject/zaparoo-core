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

package playnite

import (
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

// specificationSystems maps Playnite's platform specification IDs to Zaparoo
// systems. The specification ID is fixed by Playnite's platform definitions
// and does not change when a user renames the platform. Specifications with
// no Zaparoo counterpart are absent, and their games are not indexed.
//
//nolint:gochecknoglobals // Static mapping table.
var specificationSystems = map[string]string{
	"3do":                     systemdefs.System3DO,
	"amstrad_cpc":             systemdefs.SystemAmstrad,
	"apple_2":                 systemdefs.SystemAppleII,
	"arcade":                  systemdefs.SystemArcade,
	"arduboy":                 systemdefs.SystemArduboy,
	"atari_2600":              systemdefs.SystemAtari2600,
	"atari_5200":              systemdefs.SystemAtari5200,
	"atari_7800":              systemdefs.SystemAtari7800,
	"atari_8bit":              systemdefs.SystemAtari800,
	"atari_jaguar":            systemdefs.SystemJaguar,
	"atari_lynx":              systemdefs.SystemAtariLynx,
	"atari_st":                systemdefs.SystemAtariST,
	"bandai_wonderswan":       systemdefs.SystemWonderSwan,
	"bandai_wonderswan_color": systemdefs.SystemWonderSwanColor,
	"coleco_vision":           systemdefs.SystemColecoVision,
	"commodore_64":            systemdefs.SystemC64,
	"commodore_amiga":         systemdefs.SystemAmiga,
	"commodore_amiga_cd32":    systemdefs.SystemAmigaCD32,
	"commodore_cbm2":          systemdefs.SystemCBMII,
	"commodore_cbm5x0":        systemdefs.SystemCBMII,
	"commodore_pet":           systemdefs.SystemPET2001,
	"commodore_plus4":         systemdefs.SystemCommodorePlus4,
	"commodore_vci20":         systemdefs.SystemVIC20,
	"fairchild_channelf":      systemdefs.SystemChannelF,
	"macintosh":               systemdefs.SystemMacOS,
	"magnavox_odyssey_2":      systemdefs.SystemOdyssey2,
	"mattel_intellivision":    systemdefs.SystemIntellivision,
	"megaduck":                systemdefs.SystemMegaDuck,
	"microsoft_msx":           systemdefs.SystemMSX,
	"microsoft_msx2":          systemdefs.SystemMSX2,
	"nec_pc88":                systemdefs.SystemPC88,
	"nec_pc98":                systemdefs.SystemPC98,
	"nec_pcfx":                systemdefs.SystemPCFX,
	"nec_supergrafx":          systemdefs.SystemSuperGrafx,
	"nec_turbografx_16":       systemdefs.SystemTurboGrafx16,
	"nec_turbografx_cd":       systemdefs.SystemTurboGrafx16CD,
	"nintendo_3ds":            systemdefs.System3DS,
	"nintendo_64":             systemdefs.SystemNintendo64,
	"nintendo_ds":             systemdefs.SystemNDS,
	"nintendo_dsi":            systemdefs.SystemNDS,
	"nintendo_famicom_disk":   systemdefs.SystemFDS,
	"nintendo_gameandwatch":   systemdefs.SystemGameNWatch,
	"nintendo_gameboy":        systemdefs.SystemGameboy,
	"nintendo_gameboyadvance": systemdefs.SystemGBA,
	"nintendo_gameboycolor":   systemdefs.SystemGameboyColor,
	"nintendo_gamecube":       systemdefs.SystemGameCube,
	"nintendo_nes":            systemdefs.SystemNES,
	"nintendo_super_nes":      systemdefs.SystemSNES,
	"nintendo_switch":         systemdefs.SystemSwitch,
	"nintendo_switch2":        systemdefs.SystemNintendoSwitch2,
	"nintendo_virtualboy":     systemdefs.SystemVirtualBoy,
	"nintendo_wii":            systemdefs.SystemWii,
	"nintendo_wiiu":           systemdefs.SystemWiiU,
	"pc_dos":                  systemdefs.SystemDOS,
	"pc_linux":                systemdefs.SystemLinux,
	"pc_windows":              systemdefs.SystemPC,
	"philips_cdi":             systemdefs.SystemCDI,
	"pokemon_mini":            systemdefs.SystemPokemonMini,
	"sega_32x":                systemdefs.SystemSega32X,
	"sega_cd":                 systemdefs.SystemMegaCD,
	"sega_dreamcast":          systemdefs.SystemDreamcast,
	"sega_gamegear":           systemdefs.SystemGameGear,
	"sega_genesis":            systemdefs.SystemGenesis,
	"sega_mastersystem":       systemdefs.SystemMasterSystem,
	"sega_saturn":             systemdefs.SystemSaturn,
	"sega_sg1000":             systemdefs.SystemSG1000,
	"sharp_x1":                systemdefs.SystemX1,
	"sharp_x68000":            systemdefs.SystemX68000,
	"sinclair_zx81":           systemdefs.SystemZX81,
	"sinclair_zxspectrum":     systemdefs.SystemZXSpectrum,
	"sinclair_zxspectrum3":    systemdefs.SystemZXSpectrum,
	"snk_neogeo_aes":          systemdefs.SystemNeoGeoAES,
	"snk_neogeo_cd":           systemdefs.SystemNeoGeoCD,
	"snk_neogeopocket":        systemdefs.SystemNeoGeoPocket,
	"snk_neogeopocket_color":  systemdefs.SystemNeoGeoPocketColor,
	"sony_playstation":        systemdefs.SystemPSX,
	"sony_playstation2":       systemdefs.SystemPS2,
	"sony_playstation3":       systemdefs.SystemPS3,
	"sony_playstation4":       systemdefs.SystemPS4,
	"sony_playstation5":       systemdefs.SystemPS5,
	"sony_psp":                systemdefs.SystemPSP,
	"sony_vita":               systemdefs.SystemVita,
	"thomson_mo5":             systemdefs.SystemThomson,
	"thomson_to7":             systemdefs.SystemThomson,
	"tic_80":                  systemdefs.SystemTIC80,
	"uzebox":                  systemdefs.SystemUzebox,
	"vectrex":                 systemdefs.SystemVectrex,
	"watara_supervision":      systemdefs.SystemSuperVision,
	"xbox":                    systemdefs.SystemXbox,
	"xbox360":                 systemdefs.SystemXbox360,
	"xbox_one":                systemdefs.SystemXboxOne,
	"xbox_series":             systemdefs.SystemSeriesXS,
}

// SystemForGame returns the Zaparoo system a game belongs to. A game with no
// platform at all is a PC game: that is how Playnite leaves a manually added
// program. Otherwise the first platform with a known specification decides,
// and a game whose platforms are all unknown has no system.
func SystemForGame(game *Game) (string, bool) {
	if len(game.Platforms) == 0 {
		return systemdefs.SystemPC, true
	}
	for i := range game.Platforms {
		spec := strings.ToLower(strings.TrimSpace(game.Platforms[i].SpecificationID))
		if systemID, ok := specificationSystems[spec]; ok {
			return systemID, true
		}
	}
	return "", false
}

// Systems returns every system a Playnite game can be indexed under, sorted.
func Systems() []string {
	seen := map[string]struct{}{systemdefs.SystemPC: {}}
	for _, systemID := range specificationSystems {
		seen[systemID] = struct{}{}
	}
	systems := make([]string, 0, len(seen))
	for systemID := range seen {
		systems = append(systems, systemID)
	}
	slices.Sort(systems)
	return systems
}
