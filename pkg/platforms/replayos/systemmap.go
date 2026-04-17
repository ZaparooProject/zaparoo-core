//go:build linux

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

package replayos

import "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"

// SystemInfo maps a ReplayOS ROM folder name to Zaparoo system information.
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

// SystemMap maps ReplayOS ROM folder names to Zaparoo system information.
// Folder names use ReplayOS's manufacturer_system convention.
//
//nolint:gochecknoglobals // Package-level configuration
var SystemMap = map[string]SystemInfo{
	// Nintendo
	"nintendo_nes": {
		SystemID:   systemdefs.SystemNES,
		Extensions: []string{".nes", ".unif", ".unf", ".fds", ".zip", ".7z"},
	},
	"nintendo_snes": {
		SystemID:   systemdefs.SystemSNES,
		Extensions: []string{".smc", ".sfc", ".swc", ".fig", ".bs", ".st", ".zip", ".7z"},
	},
	"nintendo_gb": {
		SystemID:   systemdefs.SystemGameboy,
		Extensions: []string{".gb", ".zip", ".7z"},
	},
	"nintendo_gbc": {
		SystemID:   systemdefs.SystemGameboyColor,
		Extensions: []string{".gbc", ".zip", ".7z"},
	},
	"nintendo_gba": {
		SystemID:   systemdefs.SystemGBA,
		Extensions: []string{".gba", ".zip", ".7z"},
	},
	"nintendo_n64": {
		SystemID:   systemdefs.SystemNintendo64,
		Extensions: []string{".z64", ".n64", ".v64", ".bin", ".u1", ".zip", ".7z"},
	},
	"nintendo_ds": {
		SystemID:   systemdefs.SystemNDS,
		Extensions: []string{".nds", ".bin", ".zip", ".7z"},
	},

	// Sega
	"sega_smd": {
		SystemID:   systemdefs.SystemGenesis,
		Extensions: []string{".bin", ".gen", ".md", ".sg", ".smd", ".zip", ".7z"},
	},
	"sega_sms": {
		SystemID:   systemdefs.SystemMasterSystem,
		Extensions: []string{".bin", ".sms", ".zip", ".7z"},
	},
	"sega_gg": {
		SystemID:   systemdefs.SystemGameGear,
		Extensions: []string{".bin", ".gg", ".zip", ".7z"},
	},
	"sega_32x": {
		SystemID:   systemdefs.SystemSega32X,
		Extensions: []string{".32x", ".chd", ".smd", ".bin", ".md", ".zip", ".7z"},
	},
	"sega_cd": {
		SystemID:   systemdefs.SystemMegaCD,
		Extensions: []string{".cue", ".iso", ".chd", ".m3u"},
	},
	"sega_dc": {
		SystemID:   systemdefs.SystemDreamcast,
		Extensions: []string{".cdi", ".cue", ".gdi", ".chd", ".m3u"},
	},
	"sega_saturn": {
		SystemID:   systemdefs.SystemSaturn,
		Extensions: []string{".cue", ".ccd", ".m3u", ".chd", ".iso", ".zip"},
	},
	"sega_sg1000": {
		SystemID:   systemdefs.SystemSG1000,
		Extensions: []string{".bin", ".sg", ".zip", ".7z"},
	},

	// Arcade
	"arcade_fbneo": {
		SystemID:   systemdefs.SystemArcade,
		LauncherID: "ArcadeFBNeo",
		Extensions: []string{".zip", ".7z"},
	},
	"arcade_mame": {
		SystemID:   systemdefs.SystemArcade,
		LauncherID: "ArcadeMAME",
		Extensions: []string{".zip", ".7z"},
	},
	"arcade_mame_2k3p": {
		SystemID:   systemdefs.SystemArcade,
		LauncherID: "ArcadeMAME2K3P",
		Extensions: []string{".zip", ".7z"},
	},
	"arcade_dc": {
		SystemID:   systemdefs.SystemAtomiswave,
		LauncherID: "ArcadeDC",
		Extensions: []string{".zip", ".chd", ".lst", ".bin", ".dat", ".7z"},
	},

	// Atari
	"atari_2600": {
		SystemID:   systemdefs.SystemAtari2600,
		Extensions: []string{".a26", ".bin", ".zip", ".7z"},
	},
	"atari_5200": {
		SystemID: systemdefs.SystemAtari5200,
		Extensions: []string{
			".rom", ".xfd", ".atr", ".atx", ".cdm", ".cas",
			".car", ".bin", ".a52", ".xex", ".zip", ".7z",
		},
	},
	"atari_7800": {
		SystemID:   systemdefs.SystemAtari7800,
		Extensions: []string{".a78", ".bin", ".zip", ".7z"},
	},
	"atari_jaguar": {
		SystemID:   systemdefs.SystemJaguar,
		Extensions: []string{".cue", ".j64", ".jag", ".cof", ".abs", ".cdi", ".rom", ".zip", ".7z"},
	},
	"atari_lynx": {
		SystemID:   systemdefs.SystemAtariLynx,
		Extensions: []string{".lnx", ".zip", ".7z"},
	},

	// Sony
	"sony_psx": {
		SystemID:   systemdefs.SystemPSX,
		Extensions: []string{".cue", ".img", ".mdf", ".pbp", ".toc", ".cbn", ".m3u", ".ccd", ".chd", ".iso"},
	},

	// NEC
	"nec_pce": {
		SystemID:   systemdefs.SystemTurboGrafx16,
		Extensions: []string{".pce", ".bin", ".zip", ".7z"},
	},
	"nec_pcecd": {
		SystemID:   systemdefs.SystemTurboGrafx16CD,
		Extensions: []string{".pce", ".cue", ".ccd", ".iso", ".img", ".chd"},
	},

	// SNK
	"snk_neogeo": {
		SystemID:   systemdefs.SystemNeoGeo,
		Extensions: []string{".7z", ".zip"},
	},
	"snk_neocd": {
		SystemID:   systemdefs.SystemNeoGeoCD,
		Extensions: []string{".cue", ".iso", ".chd"},
	},
	"snk_ngp": {
		SystemID:   systemdefs.SystemNeoGeoPocket,
		Extensions: []string{".ngp", ".zip", ".7z"},
	},

	// Commodore
	"commodore_c64": {
		SystemID:   systemdefs.SystemC64,
		Extensions: []string{".d64", ".d81", ".crt", ".prg", ".tap", ".t64", ".m3u", ".zip", ".7z"},
	},
	"commodore_ami": {
		SystemID:   systemdefs.SystemAmiga,
		Extensions: []string{".adf", ".uae", ".ipf", ".dms", ".dmz", ".adz", ".lha", ".hdf", ".exe", ".m3u", ".zip"},
	},
	"commodore_amicd": {
		SystemID:   systemdefs.SystemAmigaCD32,
		Extensions: []string{".bin", ".cue", ".iso", ".chd"},
	},

	// MSX
	"msx_msx": {
		SystemID:   systemdefs.SystemMSX1,
		Extensions: []string{".dsk", ".mx1", ".rom", ".zip", ".7z", ".cas", ".m3u"},
	},
	"msx_msx2": {
		SystemID:   systemdefs.SystemMSX2,
		Extensions: []string{".dsk", ".mx2", ".rom", ".zip", ".7z", ".cas", ".m3u"},
	},

	// Other computers
	"sinclair_zxs": {
		SystemID:   systemdefs.SystemZXSpectrum,
		Extensions: []string{".tzx", ".tap", ".z80", ".rzx", ".scl", ".trd", ".dsk", ".zip", ".7z"},
	},
	"sharp_x68k": {
		SystemID: systemdefs.SystemX68000,
		Extensions: []string{
			".dim", ".img", ".d88", ".88d", ".hdm", ".dup",
			".2hd", ".xdf", ".hdf", ".cmd", ".m3u", ".zip", ".7z",
		},
	},
	"panasonic_3do": {
		SystemID:   systemdefs.System3DO,
		LauncherID: "3DO",
		Extensions: []string{".iso", ".chd", ".cue"},
	},
	"philips_cdi": {
		SystemID:   systemdefs.SystemCDI,
		Extensions: []string{".chd", ".cue", ".toc", ".nrg", ".gdi", ".iso", ".cdr"},
	},
	"pc_dos": {
		SystemID:   systemdefs.SystemDOS,
		Extensions: []string{".pc", ".dos", ".zip", ".squashfs", ".dosz", ".m3u", ".iso", ".cue"},
	},
	"pc_scummvm": {
		SystemID:   systemdefs.SystemScummVM,
		Extensions: []string{".scummvm", ".squashfs"},
	},
	"amstrad_cpc": {
		SystemID:   systemdefs.SystemAmstrad,
		Extensions: []string{".dsk", ".sna", ".tap", ".cdt", ".voc", ".cpr", ".zip", ".7z"},
	},

	// Media
	"alpha_player": {
		SystemID:   systemdefs.SystemVideo,
		Extensions: []string{".mkv", ".avi", ".mp4", ".flac", ".ogg", ".nsf", ".vgm"},
	},
}
