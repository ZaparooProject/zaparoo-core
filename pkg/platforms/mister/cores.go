// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package mister

import (
	"fmt"
	s "strings"
)

type MGLParams struct {
	Method string
	Delay  int
	Index  int
}

type Slot struct {
	Mgl   *MGLParams
	Label string
	Exts  []string
}

type Core struct {
	ID             string
	SetName        string
	RBF            string
	Slots          []Slot
	SetNameSameDir bool
}

// CoreGroups is a list of common MiSTer aliases that map back to a system.
// First in list takes precendence for simple attributes in case there's a
// conflict in the future.
var CoreGroups = map[string][]Core{
	"Atari7800": {Systems["Atari7800"], Systems["Atari2600"]},
	"Coleco":    {Systems["ColecoVision"], Systems["SG1000"]},
	"Gameboy":   {Systems["Gameboy"], Systems["GameboyColor"]},
	"NES":       {Systems["NES"], Systems["NESMusic"], Systems["FDS"]},
	"SMS": {Systems["MasterSystem"], Systems["GameGear"], Core{
		ID: "SG1000",
		Slots: []Slot{
			{
				Exts: []string{".sg"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	}},
	"SNES":   {Systems["SNES"], Systems["SNESMusic"]},
	"TGFX16": {Systems["TurboGrafx16"], Systems["SuperGrafx"]},
}

func PathToMGLDef(system *Core, path string) (*MGLParams, error) {
	var mglDef *MGLParams

	for _, ft := range system.Slots {
		for _, ext := range ft.Exts {
			if s.HasSuffix(s.ToLower(path), ext) {
				return ft.Mgl, nil
			}
		}
	}

	return mglDef, fmt.Errorf("system has no matching mgl args: %s, %s", system.ID, path)
}

// FIXME: launch game > launch new game same system > not working? should it?
// TODO: alternate cores (user core override)
// TODO: alternate arcade folders
// TODO: custom scan function
// TODO: custom launch function
// TODO: support globbing on extensions

var Systems = map[string]Core{
	// Consoles
	"AdventureVision": {
		ID:  "AdventureVision",
		RBF: "_Console/AdventureVision",
		Slots: []Slot{
			{
				Label: "Game",
				Exts:  []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Arcadia": {
		ID:  "Arcadia",
		RBF: "_Console/Arcadia",
		Slots: []Slot{
			{
				Label: "Cartridge",
				Exts:  []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Astrocade": {
		ID:  "Astrocade",
		RBF: "_Console/Astrocade",
		Slots: []Slot{
			{
				Exts: []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Atari2600": {
		ID:      "Atari2600",
		SetName: "Atari2600",
		RBF:     "_Console/Atari7800",
		Slots: []Slot{
			{
				Exts: []string{".a26"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Atari5200": {
		ID:  "Atari5200",
		RBF: "_Console/Atari5200",
		Slots: []Slot{
			{
				Label: "Cart",
				Exts:  []string{".car", ".a52", ".bin", ".rom"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
		},
	},
	"Atari7800": {
		ID:  "Atari7800",
		RBF: "_Console/Atari7800",
		Slots: []Slot{
			{
				Exts: []string{".a78", ".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
			//{
			//	Label: "BIOS",
			//	Exts:  []string{".rom", ".bin"},
			//	Mgl: &MGLParams{
			//		Delay:  1,
			//		Method: "f",
			//		Index:  2,
			//	},
			// },
		},
	},
	"AtariLynx": {
		ID:  "AtariLynx",
		RBF: "_Console/AtariLynx",
		Slots: []Slot{
			{
				Exts: []string{".lnx"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	// TODO: AY-3-8500
	//       Doesn't appear to have roms even though it has a folder.
	// TODO: C2650
	//       Not in official repos, think it comes with update_all.
	//       https://github.com/Grabulosaure/C2650_MiSTer
	"CasioPV1000": {
		ID:  "CasioPV1000",
		RBF: "_Console/Casio_PV-1000",
		Slots: []Slot{
			{
				Label: "Cartridge",
				Exts:  []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"CDI": {
		ID:  "CDI",
		RBF: "_Console/CDi",
		Slots: []Slot{
			{
				Exts: []string{".cue", ".chd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
		},
	},
	"ChannelF": {
		ID:  "ChannelF",
		RBF: "_Console/ChannelF",
		Slots: []Slot{
			{
				Exts: []string{".rom", ".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"ColecoVision": {
		ID:  "ColecoVision",
		RBF: "_Console/ColecoVision",
		Slots: []Slot{
			{
				Exts: []string{".col", ".bin", ".rom"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"CreatiVision": {
		ID:  "CreatiVision",
		RBF: "_Console/CreatiVision",
		Slots: []Slot{
			{
				Label: "Cartridge",
				Exts:  []string{".rom", ".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
			//{
			//	Label: "Bios",
			//	Exts:  []string{".rom", ".bin"},
			//	Mgl: &MGLParams{
			//		Delay:  1,
			//		Method: "f",
			//		Index:  2,
			//	},
			// },
			{
				Label: "BASIC",
				Exts:  []string{".bas"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  3,
				},
			},
		},
	},
	// TODO: EpochGalaxy2
	//       Has a folder and mount entry but commented as "remove".
	"FDS": {
		ID:      "FDS",
		SetName: "FDS",
		RBF:     "_Console/NES",
		Slots: []Slot{
			{
				Exts: []string{".fds"},
				Mgl: &MGLParams{
					Delay:  2,
					Method: "f",
					Index:  1,
				},
			},
			//{
			//	Label: "FDS BIOS",
			//	Exts:  []string{".bin"},
			//	Mgl: &MGLParams{
			//		Delay:  1,
			//		Method: "f",
			//		Index:  2,
			//	},
			// },
		},
	},
	"Gamate": {
		ID:  "Gamate",
		RBF: "_Console/Gamate",
		Slots: []Slot{
			{
				Exts: []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Gameboy": {
		ID:  "Gameboy",
		RBF: "_Console/Gameboy",
		Slots: []Slot{
			{
				Exts: []string{".gb"},
				Mgl: &MGLParams{
					Delay:  2,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"GameboyColor": {
		ID:      "GameboyColor",
		SetName: "GBC",
		RBF:     "_Console/Gameboy",
		Slots: []Slot{
			{
				Exts: []string{".gbc"},
				Mgl: &MGLParams{
					Delay:  2,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Gameboy2P": {
		// TODO: Split 2P core into GB and GBC?
		ID:  "Gameboy2P",
		RBF: "_Console/Gameboy2P",
		Slots: []Slot{
			{
				Exts: []string{".gb", ".gbc"},
				Mgl: &MGLParams{
					Delay:  2,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"GameGear": {
		ID:      "GameGear",
		SetName: "GameGear",
		RBF:     "_Console/SMS",
		Slots: []Slot{
			{
				Exts: []string{".gg"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  2,
				},
			},
		},
	},
	"GameNWatch": {
		ID:  "GameNWatch",
		RBF: "_Console/GnW",
		Slots: []Slot{
			{
				Exts: []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"GBA": {
		ID:  "GBA",
		RBF: "_Console/GBA",
		Slots: []Slot{
			{
				Exts: []string{".gba"},
				Mgl: &MGLParams{
					Delay:  2,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"GBA2P": {
		ID:  "GBA2P",
		RBF: "_Console/GBA2P",
		Slots: []Slot{
			{
				Exts: []string{".gba"},
				Mgl: &MGLParams{
					Delay:  2,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Genesis": {
		ID:  "Genesis",
		RBF: "_Console/MegaDrive",
		Slots: []Slot{
			{
				Exts: []string{".bin", ".gen", ".md"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Intellivision": {
		ID:  "Intellivision",
		RBF: "_Console/Intellivision",
		Slots: []Slot{
			{
				// Exts: []string{".rom", ".int", ".bin"},
				Exts: []string{".int", ".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Jaguar": {
		ID:  "Jaguar",
		RBF: "_Console/Jaguar",
		Slots: []Slot{
			{
				Exts: []string{".jag", ".j64", ".rom", ".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
		},
	},
	// TODO: Jaguar
	"MasterSystem": {
		ID:  "MasterSystem",
		RBF: "_Console/SMS",
		Slots: []Slot{
			{
				Exts: []string{".sms"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"MegaCD": {
		ID:  "MegaCD",
		RBF: "_Console/MegaCD",
		Slots: []Slot{
			{
				Label: "Disk",
				Exts:  []string{".cue", ".chd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
		},
	},
	"MegaDuck": {
		ID:  "MegaDuck",
		RBF: "_Console/Gameboy",
		Slots: []Slot{
			{
				Exts: []string{".bin"},
				Mgl: &MGLParams{
					Delay:  2,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"NeoGeo": {
		ID:  "NeoGeo",
		RBF: "_Console/NeoGeo",
		Slots: []Slot{
			{
				// TODO: This also has some special handling re: zip files (darksoft pack).
				// Exts: []strings{".*"}
				Label: "ROM set",
				Exts:  []string{".neo"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"NeoGeoCD": {
		ID:  "NeoGeoCD",
		RBF: "_Console/NeoGeo",
		Slots: []Slot{
			{
				Label: "CD Image",
				Exts:  []string{".cue", ".chd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
		},
	},
	"NES": {
		ID:  "NES",
		RBF: "_Console/NES",
		Slots: []Slot{
			{
				Exts: []string{".nes"},
				Mgl: &MGLParams{
					Delay:  2,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"NESMusic": {
		ID:  "NESMusic",
		RBF: "_Console/NES",
		Slots: []Slot{
			{
				Exts: []string{".nsf"},
				Mgl: &MGLParams{
					Delay:  2,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Nintendo64": {
		ID:  "Nintendo64",
		RBF: "_Console/N64",
		Slots: []Slot{
			{
				Exts: []string{".n64", ".z64"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Odyssey2": {
		ID:  "Odyssey2",
		RBF: "_Console/Odyssey2",
		Slots: []Slot{
			{
				Label: "Cartridge",
				Exts:  []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
			//{
			//	Label: "XROM",
			//	Exts:  []string{".rom"},
			//	Mgl: &MGLParams{
			//		Delay:  1,
			//		Method: "f",
			//		Index:  2,
			//	},
			// },
		},
	},
	"PocketChallengeV2": {
		ID:      "PocketChallengeV2",
		SetName: "PocketChallengeV2",
		RBF:     "_Console/WonderSwan",
		Slots: []Slot{
			{
				Label: "ROM",
				Exts:  []string{".pc2"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"PokemonMini": {
		ID:  "PokemonMini",
		RBF: "_Console/PokemonMini",
		Slots: []Slot{
			{
				Label: "ROM",
				Exts:  []string{".min"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"PSX": {
		ID:  "PSX",
		RBF: "_Console/PSX",
		Slots: []Slot{
			{
				Label: "CD",
				Exts:  []string{".cue", ".chd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
			{
				Label: "Exe",
				Exts:  []string{".exe"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Sega32X": {
		ID:  "Sega32X",
		RBF: "_Console/S32X",
		Slots: []Slot{
			{
				Exts: []string{".32x"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"SG1000": {
		ID:      "SG1000",
		SetName: "SG1000",
		RBF:     "_Console/ColecoVision",
		Slots: []Slot{
			{
				Label: "SG-1000",
				Exts:  []string{".sg"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  0,
				},
			},
		},
	},
	"SuperGameboy": {
		ID:  "SuperGameboy",
		RBF: "_Console/SGB",
		Slots: []Slot{
			{
				Exts: []string{".gb", ".gbc"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"SuperVision": {
		ID:  "SuperVision",
		RBF: "_Console/SuperVision",
		Slots: []Slot{
			{
				Label: "Cartridge",
				Exts:  []string{".bin", ".sv"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Saturn": {
		ID:  "Saturn",
		RBF: "_Console/Saturn",
		Slots: []Slot{
			{
				Label: "Disk",
				Exts:  []string{".cue", ".chd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
		},
	},
	"SNES": {
		ID:  "SNES",
		RBF: "_Console/SNES",
		Slots: []Slot{
			{
				Exts: []string{".sfc", ".smc", ".bin", ".bs"},
				Mgl: &MGLParams{
					Delay:  2,
					Method: "f",
					Index:  0,
				},
			},
		},
	},
	"SNESMusic": {
		ID:  "SNESMusic",
		RBF: "_Console/SNES",
		Slots: []Slot{
			{
				Exts: []string{".spc"},
				Mgl: &MGLParams{
					Delay:  2,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"SuperGrafx": {
		ID:  "SuperGrafx",
		RBF: "_Console/TurboGrafx16",
		Slots: []Slot{
			{
				Label: "SuperGrafx",
				Exts:  []string{".sgx"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"TurboGrafx16": {
		ID:  "TurboGrafx16",
		RBF: "_Console/TurboGrafx16",
		Slots: []Slot{
			{
				Label: "TurboGrafx",
				Exts:  []string{".bin", ".pce"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  0,
				},
			},
		},
	},
	"TurboGrafx16CD": {
		ID:  "TurboGrafx16CD",
		RBF: "_Console/TurboGrafx16",
		Slots: []Slot{
			{
				Label: "CD",
				Exts:  []string{".cue", ".chd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
		},
	},
	"VC4000": {
		ID:  "VC4000",
		RBF: "_Console/VC4000",
		Slots: []Slot{
			{
				Label: "Cartridge",
				Exts:  []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Vectrex": {
		ID:  "Vectrex",
		RBF: "_Console/Vectrex",
		Slots: []Slot{
			{
				Exts: []string{".vec", ".bin", ".rom"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
			//{
			//	Label: "Overlay",
			//	Exts:  []string{".ovr"},
			//	Mgl: &MGLParams{
			//		Delay:  1,
			//		Method: "f",
			//		Index:  2,
			//	},
			// },
		},
	},
	"WonderSwan": {
		ID:  "WonderSwan",
		RBF: "_Console/WonderSwan",
		Slots: []Slot{
			{
				Label: "ROM",
				Exts:  []string{".ws"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"WonderSwanColor": {
		ID:      "WonderSwanColor",
		SetName: "WonderSwanColor",
		RBF:     "_Console/WonderSwan",
		Slots: []Slot{
			{
				Label: "ROM",
				Exts:  []string{".wsc"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	// Computers
	"AcornAtom": {
		ID:  "AcornAtom",
		RBF: "_Computer/AcornAtom",
		Slots: []Slot{
			{
				Exts: []string{".vhd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
		},
	},
	"AcornElectron": {
		ID:  "AcornElectron",
		RBF: "_Computer/AcornElectron",
		Slots: []Slot{
			{
				Exts: []string{".vhd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
		},
	},
	"AliceMC10": {
		ID:  "AliceMC10",
		RBF: "_Computer/AliceMC10",
		Slots: []Slot{
			{
				Label: "Tape",
				Exts:  []string{".c10"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	// TODO: Altair8800
	//       Has a folder but roms are built in.
	"Amiga": {
		ID:  "Amiga",
		RBF: "_Computer/Minimig",
		Slots: []Slot{
			{
				Label: "df0",
				Exts:  []string{".adf"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  0,
				},
			},
		},
	},
	"AmigaCD32": {
		ID:  "AmigaCD32",
		RBF: "_Computer/Minimig",
		Slots: []Slot{
			{
				Label: "CD Image",
				Exts:  []string{".cue", ".chd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
		},
	},
	"Amstrad": {
		ID:  "Amstrad",
		RBF: "_Computer/Amstrad",
		Slots: []Slot{
			{
				Label: "A:",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "B:",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
			{
				Label: "Expansion",
				Exts:  []string{".e??"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  3,
				},
			},
			{
				Label: "Tape",
				Exts:  []string{".cdt"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  4,
				},
			},
		},
	},
	"AmstradPCW": {
		ID:  "AmstradPCW",
		RBF: "_Computer/Amstrad-PCW",
		Slots: []Slot{
			{
				Label: "A:",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "B:",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
		},
	},
	"ao486": {
		ID:  "ao486",
		RBF: "_Computer/ao486",
		Slots: []Slot{
			{
				Label: "Floppy A:",
				Exts:  []string{".img", ".ima", ".vfd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			// {
			// 	Label: "Floppy B:",
			// 	Exts:  []string{".img", ".ima", ".vfd"},
			// 	Mgl: &MGLParams{
			// 		Delay:  1,
			// 		Method: "s",
			// 		Index:  1,
			// 	},
			// },
			{
				Label: "IDE 0-0",
				Exts:  []string{".vhd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  2,
				},
			},
			// {
			// 	Label: "IDE 0-1",
			// 	Exts:  []string{".vhd"},
			// 	Mgl: &MGLParams{
			// 		Delay:  1,
			// 		Method: "s",
			// 		Index:  3,
			// 	},
			// },
			// {
			// 	Label: "IDE 1-0",
			// 	Exts:  []string{".vhd", ".iso", ".cue", ".chd"},
			// 	Mgl: &MGLParams{
			// 		Delay:  1,
			// 		Method: "s",
			// 		Index:  4,
			// 	},
			// },
			// {
			// 	Label: "IDE 1-1",
			// 	Exts:  []string{".vhd", ".iso", ".cue", ".chd"},
			// 	Mgl: &MGLParams{
			// 		Delay:  1,
			// 		Method: "s",
			// 		Index:  5,
			// 	},
			// },
		},
	},
	"Apogee": {
		ID:  "Apogee",
		RBF: "_Computer/Apogee",
		Slots: []Slot{
			{
				Exts: []string{".rka", ".rkr", ".gam"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"AppleI": {
		ID:  "AppleI",
		RBF: "_Computer/Apple-I",
		Slots: []Slot{
			{
				Label: "ASCII",
				Exts:  []string{".txt"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"AppleII": {
		ID:  "AppleII",
		RBF: "_Computer/Apple-II",
		Slots: []Slot{
			{
				Exts: []string{".nib", ".dsk", ".do", ".po"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Exts: []string{".hdv"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
		},
	},
	"Aquarius": {
		ID:  "Aquarius",
		RBF: "_Computer/Aquarius",
		Slots: []Slot{
			{
				Label: "Cartridge",
				Exts:  []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
			{
				Label: "Tape",
				Exts:  []string{".caq"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  2,
				},
			},
		},
	},
	// TODO: Archie
	//       Can't see anything in CONF_STR. Mentioned explicitly in menu.
	"Atari800": {
		ID:  "Atari800",
		RBF: "_Computer/Atari800",
		Slots: []Slot{
			{
				Label: "D1",
				Exts:  []string{".atr", ".xex", ".xfd", ".atx"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "D2",
				Exts:  []string{".atr", ".xex", ".xfd", ".atx"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
			{
				Label: "Cartridge",
				Exts:  []string{".car", ".rom", ".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  2,
				},
			},
		},
	},
	// TODO: AtariST
	//       CONF_STR does not have any information about the file types.
	"BBCMicro": {
		ID:  "BBCMicro",
		RBF: "_Computer/BBCMicro",
		Slots: []Slot{
			{
				Exts: []string{".vhd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Exts: []string{".ssd", ".dsd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
			{
				Exts: []string{".ssd", ".dsd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  2,
				},
			},
		},
	},
	"BK0011M": {
		ID:  "BK0011M",
		RBF: "_Computer/BK0011M",
		Slots: []Slot{
			{
				Exts: []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
			{
				Label: "FDD(A)",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
			{
				Label: "FDD(B)",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  2,
				},
			},
			{
				Label: "HDD",
				Exts:  []string{".vhd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
		},
	},
	"C16": {
		ID:  "C16",
		RBF: "_Computer/C16",
		Slots: []Slot{
			{
				Label: "#8",
				Exts:  []string{".d64", ".g64"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "#9",
				Exts:  []string{".d64", ".g64"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
			{
				// TODO: This has a hidden option with only .prg and .tap.
				Exts: []string{".prg", ".tap", ".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"C64": {
		ID:  "C64",
		RBF: "_Computer/C64",
		Slots: []Slot{
			{
				Label: "#8",
				Exts:  []string{".d64", ".g64", ".t64", ".d81"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "#9",
				Exts:  []string{".d64", ".g64", ".t64", ".d81"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
			{
				Exts: []string{".prg", ".crt", ".reu", ".tap"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"CasioPV2000": {
		ID:  "CasioPV2000",
		RBF: "_Computer/Casio_PV-2000",
		Slots: []Slot{
			{
				Label: "Cartridge",
				Exts:  []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"CoCo2": {
		ID:  "CoCo2",
		RBF: "_Computer/CoCo2",
		Slots: []Slot{
			{
				Label: "Cartridge",
				Exts:  []string{".rom", ".ccc"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
			{
				Label: "Disk Drive 0",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "Disk Drive 1",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
			{
				Label: "Disk Drive 2",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  2,
				},
			},
			{
				Label: "Disk Drive 3",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  3,
				},
			},
			{
				Label: "Cassette",
				Exts:  []string{".cas"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  2,
				},
			},
		},
	},
	// TODO: CoCo3
	//       This core has several menu states for different combinations of
	//       files to load. Unsure if MGL is compatible with it.
	// TODO: ColecoAdam
	//       Unsure what folder this uses. Coleco?
	"EDSAC": {
		ID:  "EDSAC",
		RBF: "_Computer/EDSAC",
		Slots: []Slot{
			{
				Label: "Tape",
				Exts:  []string{".tap"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Galaksija": {
		ID:  "Galaksija",
		RBF: "_Computer/Galaksija",
		Slots: []Slot{
			{
				Exts: []string{".tap"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Interact": {
		ID:  "Interact",
		RBF: "_Computer/Interact",
		Slots: []Slot{
			{
				Label: "Tape",
				Exts:  []string{".cin", ".k7"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Jupiter": {
		ID:  "Jupiter",
		RBF: "_Computer/Jupiter",
		Slots: []Slot{
			{
				Exts: []string{".ace"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Laser": {
		ID:  "Laser",
		RBF: "_Computer/Laser310",
		Slots: []Slot{
			{
				Label: "VZ Image",
				Exts:  []string{".vz"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Lynx48": {
		ID:  "Lynx48",
		RBF: "_Computer/Lynx48",
		Slots: []Slot{
			{
				Label: "Cassette",
				Exts:  []string{".tap"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"MacPlus": {
		ID:  "MacPlus",
		RBF: "_Computer/MacPlus",
		Slots: []Slot{
			{
				Label: "Pri Floppy",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
			{
				Label: "Sec Floppy",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  2,
				},
			},
			{
				Label: "SCSI-6",
				Exts:  []string{".img", ".vhd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "SCSI-5",
				Exts:  []string{".img", ".vhd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
		},
	},
	"MSX": {
		ID:  "MSX",
		RBF: "_Computer/MSX",
		Slots: []Slot{
			{
				Exts: []string{".vhd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
		},
	},
	"MSX1": {
		ID:  "MSX1",
		RBF: "_Computer/MSX1",
		Slots: []Slot{
			{
				Label: "Drive A:",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
			{
				Label: "SLOT A",
				Exts:  []string{".rom"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  2,
				},
			},
			{
				Label: "SLOT B",
				Exts:  []string{".rom"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  3,
				},
			},
		},
	},
	"MultiComp": {
		ID:  "MultiComp",
		RBF: "_Computer/MultiComp",
		Slots: []Slot{
			{
				Exts: []string{".img"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
		},
	},
	// TODO: OndraSPO186
	//       Nothing listed in CONF_STR but docs do mention loading files.
	"Orao": {
		ID:  "Orao",
		RBF: "_Computer/ORAO",
		Slots: []Slot{
			{
				Exts: []string{".tap"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Oric": {
		ID:  "Oric",
		RBF: "_Computer/Oric",
		Slots: []Slot{
			{
				Label: "Drive A:",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
		},
	},
	// TODO: PC88
	//       Nothing listed in CONF_STR.
	"PCXT": {
		ID:  "PCXT",
		RBF: "_Computer/PCXT",
		Slots: []Slot{
			{
				Label: "Floppy A:",
				Exts:  []string{".img", ".ima", ".vfd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "Floppy B:",
				Exts:  []string{".img", ".ima", ".vfd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
			{
				Label: "IDE 0-0",
				Exts:  []string{".vhd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  2,
				},
			},
			{
				Label: "IDE 0-1",
				Exts:  []string{".vhd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  3,
				},
			},
		},
	},
	"PDP1": {
		ID:  "PDP1",
		RBF: "_Computer/PDP1",
		Slots: []Slot{
			{
				Exts: []string{".pdp", ".rim", ".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"PET2001": {
		ID:  "PET2001",
		RBF: "_Computer/PET2001",
		Slots: []Slot{
			{
				Exts: []string{".prg", ".tap"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"PMD85": {
		ID:  "PMD85",
		RBF: "_Computer/PMD85",
		Slots: []Slot{
			{
				Label: "ROM Pack",
				Exts:  []string{".rmm"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"QL": {
		ID:  "QL",
		RBF: "_Computer/QL",
		Slots: []Slot{
			{
				Label: "HD Image",
				Exts:  []string{".win"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "MDV Image",
				Exts:  []string{".mdv"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  2,
				},
			},
		},
	},
	"RX78": {
		ID:  "RX78",
		RBF: "_Computer/RX78",
		Slots: []Slot{
			{
				Label: "Cartridge",
				Exts:  []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"SAMCoupe": {
		ID:  "SAMCoupe",
		RBF: "_Computer/SAMCoupe",
		Slots: []Slot{
			{
				Label: "Drive 1",
				Exts:  []string{".dsk", ".mgt", ".img"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "Drive 2",
				Exts:  []string{".dsk", ".mgt", ".img"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
		},
	},
	// TODO: SharpMZ
	//       Nothing listed in CONF_STR.
	"SordM5": {
		ID:  "SordM5",
		RBF: "_Computer/SordM5",
		Slots: []Slot{
			{
				Label: "ROM",
				Exts:  []string{".bin", ".rom"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
			{
				Label: "Tape",
				Exts:  []string{".cas"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  2,
				},
			},
		},
	},
	"Specialist": {
		ID:  "Specialist",
		RBF: "_Computer/Specialist",
		Slots: []Slot{
			{
				Label: "Tape",
				Exts:  []string{".rks"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  0,
				},
			},
			{
				Label: "Disk",
				Exts:  []string{".odi"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
		},
	},
	"SVI328": {
		ID:  "SVI328",
		RBF: "_Computer/Svi328",
		Slots: []Slot{
			{
				Label: "Cartridge",
				Exts:  []string{".bin", ".rom"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
			{
				Label: "CAS File",
				Exts:  []string{".cas"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  2,
				},
			},
		},
	},
	"TatungEinstein": {
		ID:  "TatungEinstein",
		RBF: "_Computer/TatungEinstein",
		Slots: []Slot{
			{
				Label: "Disk 0",
				Exts:  []string{".dsk"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
		},
	},
	"TI994A": {
		ID:  "TI994A",
		RBF: "_Computer/Ti994a",
		Slots: []Slot{
			{
				Label: "Full Cart",
				Exts:  []string{".m99", ".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
			{
				Label: "ROM Cart",
				Exts:  []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  2,
				},
			},
			{
				Label: "GROM Cart",
				Exts:  []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  3,
				},
			},
			// TODO: Also 3 .dsk entries, inactive on first load.
		},
	},
	"TomyTutor": {
		ID:  "TomyTutor",
		RBF: "_Computer/TomyTutor",
		Slots: []Slot{
			{
				Label: "Cartridge",
				Exts:  []string{".bin"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  2,
				},
			},
			{
				Label: "Tape Image",
				Exts:  []string{".cas"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
		},
	},
	"TRS80": {
		ID:  "TRS80",
		RBF: "_Computer/TRS-80",
		Slots: []Slot{
			{
				Label: "Disk 0",
				Exts:  []string{".dsk", ".jvi"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "Disk 1",
				Exts:  []string{".dsk", ".jvi"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
			{
				Label: "Program",
				Exts:  []string{".cmd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  2,
				},
			},
			{
				Label: "Cassette",
				Exts:  []string{".cas"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"TSConf": {
		ID:  "TSConf",
		RBF: "_Computer/TSConf",
		Slots: []Slot{
			{
				Label: "Virtual SD",
				Exts:  []string{".vhd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
		},
	},
	"UK101": {
		ID:  "UK101",
		RBF: "_Computer/UK101",
		Slots: []Slot{
			{
				Label: "ASCII",
				Exts:  []string{".txt", ".bas", ".lod"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Vector06C": {
		ID:  "Vector06C",
		RBF: "_Computer/Vector-06C",
		Slots: []Slot{
			{
				Exts: []string{".rom", ".com", ".c00", ".edd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
			{
				Label: "Disk A",
				Exts:  []string{".fdd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "Disk B",
				Exts:  []string{".fdd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
		},
	},
	"VIC20": {
		ID:  "VIC20",
		RBF: "_Computer/VIC20",
		Slots: []Slot{
			{
				Label: "#8",
				Exts:  []string{".d64", ".g64"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "#9",
				Exts:  []string{".d64", ".g64"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
			{
				Exts: []string{".prg", ".crt", ".ct?", ".tap"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"X68000": {
		ID:  "X68000",
		RBF: "_Computer/X68000",
		Slots: []Slot{
			{
				Label: "FDD0",
				Exts:  []string{".d88"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "FDD1",
				Exts:  []string{".d88"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
			{
				Label: "SASI Hard Disk",
				Exts:  []string{".hdf"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  2,
				},
			},
			{
				Label: "RAM",
				Exts:  []string{".ram"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  3,
				},
			},
		},
	},
	// TODO: zx48
	//       https://github.com/Kyp069/zx48-MiSTer
	"ZX81": {
		ID:  "ZX81",
		RBF: "_Computer/ZX81",
		Slots: []Slot{
			{
				Label: "Tape",
				Exts:  []string{".0", ".p"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"ZXSpectrum": {
		ID:  "ZXSpectrum",
		RBF: "_Computer/ZX-Spectrum",
		Slots: []Slot{
			{
				Label: "Disk",
				Exts:  []string{".trd", ".img", ".dsk", ".mgt"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "Tape",
				Exts:  []string{".tap", ".csw", ".tzx"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  2,
				},
			},
			{
				Label: "Snapshot",
				Exts:  []string{".z80", ".sna"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  4,
				},
			},
			{
				Label: "DivMMC",
				Exts:  []string{".vhd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
		},
	},
	"ZXNext": {
		ID:  "ZXNext",
		RBF: "_Computer/ZXNext",
		Slots: []Slot{
			{
				Label: "C:",
				Exts:  []string{".vhd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  0,
				},
			},
			{
				Label: "D:",
				Exts:  []string{".vhd"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "s",
					Index:  1,
				},
			},
			{
				Label: "Tape",
				Exts:  []string{".tzx", ".csw"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	// Other
	"Arcade": {
		ID: "Arcade",
		Slots: []Slot{
			{
				Exts: []string{".mra"},
				Mgl:  nil,
			},
		},
	},
	"Arduboy": {
		ID:  "Arduboy",
		RBF: "_Other/Arduboy",
		Slots: []Slot{
			{
				Exts: []string{".bin", ".hex"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  0,
				},
			},
		},
	},
	"Chip8": {
		ID:  "Chip8",
		RBF: "_Other/Chip8",
		Slots: []Slot{
			{
				Exts: []string{".ch8"},
				Mgl: &MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	"Groovy": {
		ID:  "Groovy",
		RBF: "_Utility/Groovy",
		Slots: []Slot{
			{
				Label: "GMC",
				Exts:  []string{".gmc"},
				Mgl: &MGLParams{
					Delay:  3,
					Method: "f",
					Index:  1,
				},
			},
		},
	},
	// TODO: Life
	//       Has loadable files, but no folder?
	// TODO: ScummVM
	//       Requires a custom scan and launch function.
	// TODO: SuperJacob
	//       A custom computer?
	//       https://github.com/dave18/MiSTER-SuperJacob
	// TODO: TomyScramble
	//       Has loadable files and a folder but is marked as "remove"?
}
