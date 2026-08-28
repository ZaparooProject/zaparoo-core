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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/mister/v2/catalog"
)

func TestCatalogDefinitions(t *testing.T) {
	t.Parallel()

	all := catalog.All()
	if len(all) != 118 {
		t.Fatalf("expected 118 systems, got %d", len(all))
	}
	if all[0].ID != "3DO" {
		t.Fatalf("catalog is not sorted: first ID %q", all[0].ID)
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
