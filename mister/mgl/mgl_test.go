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

package mgl_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/mister/catalog"
	"github.com/ZaparooProject/zaparoo-core/mister/mgl"
)

func TestGenerate(t *testing.T) {
	t.Parallel()

	core, err := catalog.Get("NES")
	if err != nil {
		t.Fatal(err)
	}
	got, err := mgl.Generate(core, "_Console/NES", `/media/fat/games/NES/Mario & "Luigi".nes`, "")
	if err != nil {
		t.Fatal(err)
	}
	want := "<mistergamedescription>\n" +
		"\t<rbf>_Console/NES</rbf>\n" +
		"\t<file delay=\"2\" type=\"f\" index=\"1\" path=\"../../../../../media/fat/games/NES/" +
		"Mario &amp; &quot;Luigi&quot;.nes\"/>\n" +
		"</mistergamedescription>"
	if got != want {
		t.Fatalf("unexpected MGL:\n%s\nwant:\n%s", got, want)
	}
}

func TestGenerateSetNameAndCoreOnly(t *testing.T) {
	t.Parallel()

	core := &catalog.Core{SetName: `Alt & "Core"`, SetNameSameDir: true}
	got, err := mgl.Generate(core, "_Console/Test", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `<setname same_dir="1">Alt &amp; &quot;Core&quot;</setname>`) {
		t.Fatalf("setname not escaped: %s", got)
	}
}

func TestGenerateOverrideAndReset(t *testing.T) {
	t.Parallel()

	jaguar, err := catalog.Get("Jaguar")
	if err != nil {
		t.Fatal(err)
	}
	got, err := mgl.Generate(jaguar, jaguar.RBF, "/media/fat/games/Jaguar/game.jag", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `<reset delay="1" hold="1"/>`) {
		t.Fatalf("reset missing: %s", got)
	}

	override := "\t<file type=\"f\" path=\"custom\"/>\n"
	got, err = mgl.Generate(jaguar, jaguar.RBF, "ignored.jag", override)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, override) || strings.Contains(got, "ignored.jag") {
		t.Fatalf("override not preserved: %s", got)
	}
}

func TestGenerateErrors(t *testing.T) {
	t.Parallel()

	if _, err := mgl.Generate(nil, "", "", ""); err == nil {
		t.Fatal("expected nil core error")
	}
	core, _ := catalog.Get("NES")
	if _, err := mgl.Generate(core, core.RBF, "game.zip", ""); err == nil {
		t.Fatal("expected extension error")
	}
}

func TestGenerateEscapesRBFPath(t *testing.T) {
	t.Parallel()

	core := &catalog.Core{}
	got, err := mgl.Generate(core, `_Console/A&B<C>`, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `<rbf>_Console/A&amp;B&lt;C&gt;</rbf>`) {
		t.Fatalf("rbf path not escaped: %s", got)
	}
}

// Generate is not the launch path for media that needs no MGL, and the nil
// params such a slot returns used to be dereferenced straight into a panic.
func TestGenerateRejectsMediaThatNeedsNoMGL(t *testing.T) {
	t.Parallel()

	core, err := catalog.Get("Arcade")
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgl.Generate(core, "_Arcade/core.rbf", "maze_game.mra", "")
	if !errors.Is(err, catalog.ErrLaunchesDirectly) {
		t.Fatalf("expected the direct-launch sentinel, got: %v", err)
	}
}

func TestGenerateOptions(t *testing.T) {
	t.Parallel()

	core, err := catalog.Get("NES")
	if err != nil {
		t.Fatal(err)
	}
	got, err := mgl.Generate(core, "_Console/NES", "/media/fat/games/NES/a.nes", "",
		mgl.Option{Name: "Model", Value: "Master"},
		mgl.Option{Name: "Slot & 1", Value: "Mega\"SCC", Page: "Machine", Default: true},
		mgl.Option{Name: "", Value: "ignored"},
		mgl.Option{Name: "ignored", Value: ""},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "\t<option name=\"Model\" value=\"Master\"/>\n" +
		"\t<option name=\"Slot &amp; 1\" value=\"Mega&quot;SCC\" page=\"Machine\" default=\"1\"/>\n" +
		"</mistergamedescription>"
	if !strings.HasSuffix(got, want) {
		t.Fatalf("options missing from MGL:\n%s", got)
	}
}

func TestGenerateOptionsWithOverrideAndNoMedia(t *testing.T) {
	t.Parallel()

	core := &catalog.Core{}
	opt := mgl.Option{Name: "Model", Value: "Master"}
	got, err := mgl.Generate(core, "_Computer/BBCMicro", "", "", opt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `<option name="Model" value="Master"/>`) {
		t.Fatalf("core-only MGL lost its option:\n%s", got)
	}
	got, err = mgl.Generate(core, "_Computer/BBCMicro", "/x.ssd", "\t<file/>\n", opt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "\t<file/>\n\t<option name=\"Model\" value=\"Master\"/>\n</mistergamedescription>") {
		t.Fatalf("override MGL lost its option:\n%s", got)
	}
}

func TestGenerateReset(t *testing.T) {
	t.Parallel()

	core, err := catalog.Get("NES")
	if err != nil {
		t.Fatal(err)
	}
	got, err := mgl.Generate(core, "_Console/NES", "/media/fat/games/NES/a.nes", "",
		mgl.Option{Name: "Model", Value: "Master"}, mgl.Reset{Delay: 2, Hold: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := "\t<option name=\"Model\" value=\"Master\"/>\n\t<reset delay=\"2\" hold=\"1\"/>\n</mistergamedescription>"
	if !strings.HasSuffix(got, want) {
		t.Fatalf("reset missing from MGL:\n%s", got)
	}
}
