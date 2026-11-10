//go:build linux && !android

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

// Package rocknix is ROCKNIX, whose EmulationStation is Batocera's fork but
// whose /emukill does nothing: games are stopped the way its own hotkey
// daemon stops them.
package esbase

// ELECSystemAliases maps the EmulationStation system names and ROM folders
// ROCKNIX and AmberELEC share, from their common 351ELEC lineage, to the
// ES-DE folders for the same systems. Systems with no ES-DE counterpart, and
// non-game systems, are left out.
var ELECSystemAliases = map[string]string{
	"amiga":              "amiga500",
	"atarijaguar":        "jaguar",
	"atarilynx":          "lynx",
	"bbcmicro":           "bbc",
	"coleco":             "colecovision",
	"famicom":            "nes",
	"fbn":                "fbneo",
	"gamegearh":          "gamegear",
	"gamepocketcomputer": "gamepock",
	"gbah":               "gba",
	"gbav":               "gba",
	"gbch":               "gbc",
	"gbh":                "gb",
	"genesis":            "megadrive",
	"genh":               "megadrive",
	"ggh":                "gamegear",
	"laserdisc":          "daphne",
	"mac":                "macintosh",
	"megadrive-japan":    "megadrive",
	"megadriveh":         "megadrive",
	"moto":               "thomson",
	"msx":                "msx1",
	"ndsiware":           "nds",
	"neocd":              "neogeocd",
	"nesh":               "nes",
	"odyssey":            "odyssey2",
	"pc":                 "dos",
	"pc-8800":            "pc88",
	"pc-9800":            "pc98",
	"pico-8":             "pico8",
	"pspminis":           "psp",
	"segacd":             "megacd",
	"sfc":                "snes",
	"sg-1000":            "sg1000",
	"sgfx":               "supergrafx",
	"snesh":              "snes",
	"snesmsu1":           "snes-msu1",
	"tg16":               "pcengine",
	"tg16cd":             "pcenginecd",
	"tic-80":             "tic80",
	"vic20":              "c20",
	"videopac":           "odyssey2",
	"wasm-4":             "wasm4",
	"wiiware":            "wii",
	"wonderswan":         "wswan",
	"wonderswancolor":    "wswanc",
}
