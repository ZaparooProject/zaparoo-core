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

package catalog_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/mister/catalog"
)

func TestDVDPlayerDefinition(t *testing.T) {
	t.Parallel()

	core, err := catalog.Get("DVDPlayer")
	if err != nil {
		t.Fatal(err)
	}
	if core.RBF != "_Other/DVD" || core.SetName != "DVD" {
		t.Fatalf("unexpected DVD core: %+v", core)
	}
	params, err := catalog.PathToMGLDef(core, "Movie.ISO")
	if err != nil {
		t.Fatal(err)
	}
	if params.Method != "s" || params.Index != 0 || params.Delay != 2 {
		t.Fatalf("unexpected DVD mount parameters: %+v", params)
	}
	if _, err := catalog.PathToMGLDef(core, "Movie.mkv"); err == nil {
		t.Fatal("DVD core must not accept general video files")
	}
}

func TestCatalogDefinitions(t *testing.T) {
	t.Parallel()

	all := catalog.All()
	if len(all) != 182 {
		t.Fatalf("expected 182 systems, got %d", len(all))
	}
	if all[0].ID != "3DO" {
		t.Fatalf("catalog is not sorted: first ID %q", all[0].ID)
	}
	for _, core := range all {
		if len(core.Folders) == 0 || len(core.Extensions) == 0 {
			t.Fatalf("standalone scan metadata missing for %s", core.ID)
		}
	}

	nes, err := catalog.Get("NES")
	if err != nil {
		t.Fatal(err)
	}
	if len(nes.Folders) == 0 || nes.Folders[0] != "NES" {
		t.Fatalf("unexpected NES folders: %#v", nes.Folders)
	}
	if len(nes.Slots) == 0 || len(nes.Slots[0].Exts) == 0 {
		t.Fatalf("NES slots missing: %#v", nes.Slots)
	}
}

func TestCatalogReturnsDeepCopies(t *testing.T) {
	t.Parallel()

	first, err := catalog.Get("NES")
	if err != nil {
		t.Fatal(err)
	}
	first.Folders[0] = "changed"
	first.Slots[0].Exts[0] = ".changed"
	first.Slots[0].Mgl.Delay = 999

	second, err := catalog.Get("NES")
	if err != nil {
		t.Fatal(err)
	}
	if second.Folders[0] == "changed" || second.Slots[0].Exts[0] == ".changed" || second.Slots[0].Mgl.Delay == 999 {
		t.Fatal("catalog caller mutated canonical definition")
	}
}

func TestLookupAndGroups(t *testing.T) {
	t.Parallel()

	core, err := catalog.Lookup("neS")
	if err != nil {
		t.Fatal(err)
	}
	if core.ID != "NES" {
		t.Fatalf("unexpected lookup result: %#v", core)
	}

	group, err := catalog.GetGroup("NES")
	if err != nil {
		t.Fatal(err)
	}
	base, _ := catalog.Get("NES")
	music, _ := catalog.Get("NESMusic")
	fds, _ := catalog.Get("FDS")
	wantSlots := len(base.Slots) + len(music.Slots) + len(fds.Slots)
	if len(group.Slots) != wantSlots {
		t.Fatalf("group slots: want %d, got %d", wantSlots, len(group.Slots))
	}
}

func TestPathToMGLDef(t *testing.T) {
	t.Parallel()

	core, err := catalog.Get("Nintendo64")
	if err != nil {
		t.Fatal(err)
	}
	params, err := catalog.PathToMGLDef(core, "MARIO.V64")
	if err != nil {
		t.Fatal(err)
	}
	if params == nil || params.Method == "" {
		t.Fatalf("unexpected params: %#v", params)
	}
	if _, err = catalog.PathToMGLDef(core, "unknown.zip"); err == nil {
		t.Fatal("expected unmatched extension error")
	}
}

// A slot with no mgl block is how the catalog says "this extension launches
// directly", and that has to stay distinguishable from an extension no slot
// claims: reporting Arcade's .mra as unmatched would fail every arcade launch.
func TestPathToMGLDefReportsSlotsWithoutParams(t *testing.T) {
	t.Parallel()

	core := &catalog.Core{
		ID: "Test",
		Slots: []catalog.Slot{
			{Exts: []string{".bin"}},
			{Exts: []string{".rom"}, Mgl: &catalog.MGLParams{Delay: 1, Method: "f", Index: 0}},
		},
	}

	direct, err := catalog.PathToMGLDef(core, "game.bin")
	if !errors.Is(err, catalog.ErrLaunchesDirectly) {
		t.Fatalf("a matched slot without params must say so: %v", err)
	}
	if direct != nil {
		t.Fatalf("expected no params alongside the sentinel, got %#v", direct)
	}

	_, err = catalog.PathToMGLDef(core, "game.iso")
	if err == nil || errors.Is(err, catalog.ErrLaunchesDirectly) {
		t.Fatalf("an unclaimed extension is not a direct launch: %v", err)
	}

	params, err := catalog.PathToMGLDef(core, "game.rom")
	if err != nil {
		t.Fatal(err)
	}
	if params == nil || params.Method != "f" {
		t.Fatalf("unexpected params: %#v", params)
	}
}

// The catalog ships this shape, so pin it against the real Arcade entry too.
func TestPathToMGLDefArcadeMRANeedsNoMGL(t *testing.T) {
	t.Parallel()

	core, err := catalog.Get("Arcade")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.PathToMGLDef(core, "maze_game.mra"); !errors.Is(err, catalog.ErrLaunchesDirectly) {
		t.Fatalf("arcade .mra launches directly: %v", err)
	}
}

func TestAtari800Definition(t *testing.T) {
	t.Parallel()

	// Atari800 core CONF_STR: S6=Boot D1, F8=Load Cart, S0-S3=Mount D1-D4 (no reboot)
	core, err := catalog.Get("Atari800")
	if err != nil {
		t.Fatal(err)
	}

	// Disk boot: method="s" index=6 matches core's "S6 Boot D1" token
	params, err := catalog.PathToMGLDef(core, "Bandits.atr")
	if err != nil {
		t.Fatalf("PathToMGLDef(%s, \"Bandits.atr\") failed: %v", core.ID, err)
	}
	if params.Method != "s" || params.Index != 6 {
		t.Fatalf("expected Disk D1 boot: method=\"s\" index=6, got %+v", params)
	}

	// Cartridge load: method="f" index=8 matches core's "F8 Load Cart" token
	params, err = catalog.PathToMGLDef(core, "atariblast.car")
	if err != nil {
		t.Fatalf("PathToMGLDef(%s, \"atariblast.car\") failed: %v", core.ID, err)
	}
	if params.Method != "f" || params.Index != 8 {
		t.Fatalf("expected Cartridge load: method=\"f\" index=8, got %+v", params)
	}
}

func TestGroupsIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	// Groups enriches member folders and extensions; readers must not observe
	// writes into the canonical catalog while doing so.
	var readers [8]struct{}
	var passes [20]struct{}

	var wg sync.WaitGroup
	wg.Add(len(readers))
	for range readers {
		go func() {
			defer wg.Done()
			for range passes {
				for _, members := range catalog.Groups() {
					for i := range members {
						_ = members[i].Folders
					}
				}
			}
		}()
	}
	wg.Wait()
}

// Each row pins a slot to the index its core declares in CONF_STR: an F or S
// entry's digit, which Main_MiSTer matches against the MGL index attribute.
func TestAddedCoreSlots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		core   string
		file   string
		method string
		index  int
	}{
		{"C128", "game.d71", "s", 0},
		{"C128", "game.crt", "f", 2},
		{"CBMII", "game.d80", "s", 0},
		{"CBMII", "game.prg", "f", 9},
		{"Enterprise", "disk.img", "s", 1},
		{"Enterprise", "cart.rom", "f", 0},
		{"JR100", "game.prg", "f", 1},
		{"JR100", "game.bas", "f", 2},
		{"JR100", "tape.cmt", "s", 1},
		{"FM7", "tape.t77", "f", 1},
		{"FM7", "disk.d77", "s", 0},
		{"PC88", "disk.d88", "s", 0},
		{"Thomson", "cart.rom", "f", 1},
		{"Thomson", "tape.wav", "f", 2},
		{"Thomson", "disk.fd", "f", 3},
		{"StudioII", "game.st2", "f", 1},
		{"StudioII", "game.ch8", "f", 3},
		{"PocketStation", "card.gme", "f", 1},
		{"ColecoAdam", "disk.dsk", "s", 0},
		{"ColecoAdam", "tape.ddp", "s", 4},
		{"FMTowns", "disc.cue", "s", 0},
		{"FMTowns", "floppy.d88", "s", 1},
		{"FMTowns", "card.icm", "s", 2},
		{"FMTowns", "disk.vhd", "s", 4},
		{"PCFX", "disc.chd", "s", 2},
		{"MacLC", "floppy.dsk", "s", 6},
		{"MacLC", "disk.hda", "s", 0},
		{"MacLC", "disc.toast", "s", 4},
		{"MacQuadra800", "disk.vhd", "s", 0},
		{"PCjr", "cart.jrc", "f", 2},
		{"SGIIndy", "disk.img", "s", 1},
		{"SGIIndy", "disc.iso", "s", 3},
		{"NeXT", "disk.vhd", "s", 0},
		{"NeXT", "disc.cue", "s", 3},
		{"AtariLynx2P", "game.lyx", "f", 1},
		{"CoCo3", "game.ccc", "f", 1},
		{"CoCo3", "tape.cas", "f", 2},
		{"CoCo3", "disk.dsk", "s", 2},
		{"MacIIvi", "floppy.dsk", "f", 1},
		{"MacIIvi", "disc.chd", "s", 4},
		{"MacLCII", "disk.hda", "s", 0},
		{"SparcStation", "disc.iso", "s", 2},
		{"ND120", "tape.bpu", "s", 4},
		{"TI89", "os.89u", "f", 0},
		{"Solarus", "quest.sol", "s", 0},
		{"BennuGD", "game.dcb", "s", 0},
		{"SBC7", "program.h7x", "f", 1},
		{"NDS", "game.nds", "f", 3},
		{"CommanderX16", "card.img", "s", 0},
		{"CommanderX16", "cart.crt", "s", 2},
		{"PC98", "disk.d88", "s", 0},
		{"PC98", "disc.iso", "s", 4},
		{"Atari2600ARM", "game.a26", "f", 1},
		{"System80", "disk.dmk", "s", 0},
		{"System80", "tape.cas", "f", 1},
		{"Z486", "disc.chd", "s", 4},
		{"PC110", "disk.vhd", "s", 2},
		{"Raster", "film.mpg", "s", 0},
		{"Phosphor", "song.flac", "s", 0},
	}

	for i := range tests {
		tt := tests[i]
		t.Run(tt.core+"/"+tt.file, func(t *testing.T) {
			t.Parallel()

			core, err := catalog.Get(tt.core)
			if err != nil {
				t.Fatal(err)
			}
			params, err := catalog.PathToMGLDef(core, tt.file)
			if err != nil {
				t.Fatal(err)
			}
			if params.Method != tt.method || params.Index != tt.index {
				t.Fatalf("want %s/%d, got %s/%d", tt.method, tt.index, params.Method, params.Index)
			}
		})
	}
}
