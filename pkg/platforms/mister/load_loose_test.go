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

package mister

import (
	"archive/zip"
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoosePlanners_TypeTheCommandForTheMedia(t *testing.T) {
	t.Parallel()
	planners := loosePlanners(memFS(t, nil))

	tests := []struct {
		system string
		path   string
		want   []string
	}{
		{systemdefs.SystemC64, "/g/a.d64", []string{`LOAD"*",8,1{enter}`, "RUN{enter}"}},
		{systemdefs.SystemC64, "/g/a.tap", []string{`LOAD"",1{enter}`}},
		{systemdefs.SystemC128, "/g/a.D81", []string{`LOAD"*",8,1{enter}`, "RUN{enter}"}},
		{systemdefs.SystemVIC20, "/g/a.crt", []string{chord("ctrl", "f11")}},
		{systemdefs.SystemC16, "/g/a.d64", []string{`LOAD"*",8,1{enter}`, "RUN{enter}"}},
		{systemdefs.SystemPET2001, "/g/a.tap", []string{"LOAD{enter}"}},
		{systemdefs.SystemZXSpectrum, "/g/a.tzx", []string{"{enter}"}},
		{systemdefs.SystemZX81, "/g/a.p", []string{`J""{enter}`, "R{enter}"}},
		{
			systemdefs.SystemAcornElectron, "/g/a.uef",
			[]string{chord("capslock", "0"), "*TAPE{enter}", "PAGE=&E00{enter}", chord("capslock", "k") + `""{enter}`},
		},
		{systemdefs.SystemAmstrad, "/g/a.cdt", []string{`|tape{enter}run"{enter}`, "{enter}"}},
		{systemdefs.SystemQL, "/g/a.mdv", []string{"{hold:f1:200ms}"}},
		{systemdefs.SystemTatungEinstein, "/g/a.dsk", []string{chord("ctrl", "tab")}},
		{systemdefs.SystemVector06C, "/g/a.fdd", []string{chord("alt", "f11")}},
	}
	for _, tt := range tests {
		t.Run(tt.system+tt.path, func(t *testing.T) {
			t.Parallel()
			plan, ok, err := planners[tt.system](nil, tt.path)
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, tt.path, plan.target)
			assert.Equal(t, tt.want, stepTexts(plan.steps))
			for _, text := range stepTexts(plan.steps) {
				_, tokenErr := keyTokens(text, keyLayout{})
				require.NoError(t, tokenErr)
			}
		})
	}
}

func TestLoosePlanners_MediaTheCoreStartsItselfHasNoPlan(t *testing.T) {
	t.Parallel()
	planners := loosePlanners(memFS(t, nil))

	for _, tt := range []struct{ system, path string }{
		{systemdefs.SystemC64, "/g/a.prg"},
		{systemdefs.SystemC64, "/g/a.crt"},
		{systemdefs.SystemZXSpectrum, "/g/a.z80"},
		{systemdefs.SystemAcornElectron, "/g/a.vhd"},
	} {
		_, ok, err := planners[tt.system](nil, tt.path)
		require.NoError(t, err)
		assert.False(t, ok, tt.path)
	}
}

func TestLoosePlanners_FloppyCoresAskForTheOptionsThatShowTheirDrive(t *testing.T) {
	t.Parallel()
	planners := loosePlanners(memFS(t, nil))

	bbc, ok, err := planners[systemdefs.SystemBBCMicro](nil, "/g/a.ssd")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Empty(t, bbc.steps, "the core autostarts the floppy itself")
	require.Len(t, bbc.options, 3)
	assert.Equal(t, mgls.Reset{Delay: 1, Hold: 1}, bbc.options[2], "the disk boots from a reset once it is in")
	assert.Equal(t, "Model", bbc.options[0].(mgls.Option).Name)
	assert.Equal(t, "Master(R65SC12)", bbc.options[0].(mgls.Option).Value)

	coco3, ok, err := planners[systemdefs.SystemCoCo3](nil, "/g/a.ccc")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "Multi-Pak", coco3.options[0].(mgls.Option).Name)
}

func decbImage(name, ext string, fileType byte) []byte {
	img := make([]byte, 35*18*256)
	dir := img[(17*18+2)*256:]
	copy(dir, name+"        "[:8-len(name)])
	copy(dir[8:], ext)
	dir[11] = fileType
	dir[13] = 1
	for i := 32; i < 9*256; i++ {
		dir[i] = 0xFF
	}
	return img
}

func TestLoosePlanners_CoCo2DiskRunsTheProgramOnTheDisk(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{"/g/auto.dsk": decbImage("AUTO", "BAS", 0)})

	plan, ok, err := loosePlanners(fs)[systemdefs.SystemCoCo2](nil, "/g/auto.dsk")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{`RUN"AUTO"{enter}`}, stepTexts(plan.steps))
	require.Len(t, plan.options, 1)
	assert.Equal(t, "Cart Slot", plan.options[0].(mgls.Option).Name)
}

// dskImage is an extended DSK with ten 512-byte sectors a track, numbered from
// firstID, and the directory on dirTrack.
func dskImage(firstID byte, dirTrack int, files ...string) []byte {
	const tracks, secs, secSize = 3, 10, 512
	info := make([]byte, 0x100)
	copy(info, "EXTENDED CPC DSK File\r\nDisk-Info\r\n")
	info[0x30], info[0x31] = tracks, 1
	for i := range tracks {
		info[0x34+i] = byte((0x100 + secs*secSize) >> 8)
	}
	img := make([]byte, 0, len(info)+tracks*(0x100+secs*secSize))
	img = append(img, info...)
	for t := range tracks {
		hdr := make([]byte, 0x100)
		copy(hdr, "Track-Info\r\n")
		hdr[0x10], hdr[0x14], hdr[0x15] = byte(t), 2, secs
		for s := range secs {
			e := hdr[0x18+s*8:]
			e[0], e[2], e[3], e[7] = byte(t), firstID+byte(s), 2, secSize>>8
		}
		data := make([]byte, secs*secSize)
		for i := range data {
			data[i] = 0xE5
		}
		if t == dirTrack {
			for i, f := range files {
				e := data[i*32 : i*32+32]
				for j := range e {
					e[j] = 0
				}
				name, ext, _ := strings.Cut(f, ".")
				copy(e[1:12], fmt.Sprintf("%-8s%-3s", name, ext))
				e[15] = 1
			}
		}
		img = append(img, hdr...)
		img = append(img, data...)
	}
	return img
}

func TestLoosePlanners_CPCDiskTypesItsProgramInLowercase(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{"/g/a.dsk": dskImage(0xC1, 0, "2DISC.BAS")})

	plan, ok, err := loosePlanners(fs)[systemdefs.SystemAmstrad](nil, "/g/a.dsk")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{`run"2disc{enter}`}, stepTexts(plan.steps))
	tokens, err := keyTokens(plan.steps[0].text, plan.layout)
	require.NoError(t, err)
	assert.Contains(t, tokens, "{press:shift}", "the quote is a held chord")
}

// Hardware, 2026-10-26: the CPC read the digit typed straight after the quote's
// shift came up as shifted, so RUN"1943 arrived as RUN"!943 and found nothing.
func TestLoosePlanners_CPCDigitAfterQuoteIsNotShifted(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{"/g/a.dsk": dskImage(0xC1, 0, "1943")})

	plan, ok, err := loosePlanners(fs)[systemdefs.SystemAmstrad](nil, "/g/a.dsk")
	require.NoError(t, err)
	require.True(t, ok)
	tokens, err := keyTokens(plan.steps[0].text, plan.layout)
	require.NoError(t, err)

	digit := slices.Index(tokens, "1")
	require.Positive(t, digit)
	assert.Equal(t, "{delay:"+chordHold.String()+"}", tokens[digit-1],
		"the shift is released well before the digit")
	assert.Equal(t, "{release:shift}", tokens[digit-2])
}

func TestLoosePlanners_EinsteinDiskRunsItsOnlyProgram(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{
		"/g/one.dsk":  dskImage(0, 2, "ALICE.COM", "ALICE.DAT"),
		"/g/many.dsk": dskImage(0, 2, "HUST.COM", "LAZY.COM"),
	})
	planner := loosePlanners(fs)[systemdefs.SystemTatungEinstein]

	plan, ok, err := planner(nil, "/g/one.dsk")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{chord("ctrl", "tab"), "ALICE{enter}"}, stepTexts(plan.steps))

	plan, ok, err = planner(nil, "/g/many.dsk")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{chord("ctrl", "tab")}, stepTexts(plan.steps),
		"a disk of several programs stays at the prompt")
}

func TestLoosePlanners_UnreadableDisksLaunchPlain(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{"/g/a.dsk": make([]byte, 100), "/g/b.dsk": make([]byte, 100)})
	planners := loosePlanners(fs)

	_, ok, err := planners[systemdefs.SystemCoCo2](nil, "/g/a.dsk")
	require.NoError(t, err)
	assert.False(t, ok)

	_, ok, err = planners[systemdefs.SystemAmstrad](nil, "/g/b.dsk")
	require.NoError(t, err)
	assert.False(t, ok)

	_, ok, err = planners[systemdefs.SystemTRS80](nil, "/g/missing.cas")
	require.NoError(t, err)
	assert.False(t, ok)
}

func casImage(kind byte, name string, size int) []byte {
	data := make([]byte, size)
	data[0] = 0
	data[256] = 0xA5
	switch kind {
	case 0x55:
		data[257] = 0x55
		copy(data[258:], (name + "      ")[:6])
	default:
		data[257], data[258], data[259] = 0xD3, 0xD3, 0xD3
		data[260] = name[0]
	}
	return data
}

func TestLoosePlanners_TRS80TapeLoadsThenRuns(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{"/g/game.cas": casImage(0x55, "GAME", 9000)})

	plan, ok, err := loosePlanners(fs)[systemdefs.SystemTRS80](nil, "/g/game.cas")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"{enter}", "SYSTEM{enter}", "G{enter}", "/{enter}"}, stepTexts(plan.steps))
	assert.Equal(t, 2*time.Second+tapeMargin, plan.steps[3].pause, "9000 bytes at the core's 4000 bytes a second")
}

func TestLoosePlanners_TRS80BasicTapeLoadsThenRuns(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{"/g/b.cas": casImage(0xD3, "B", 1000)})

	plan, ok, err := loosePlanners(fs)[systemdefs.SystemTRS80](nil, "/g/b.cas")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"{enter}", "CLOAD{enter}", "RUN{enter}"}, stepTexts(plan.steps))
}

func TestTapeWait(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{"/small": make([]byte, 100), "/huge": make([]byte, 100<<20)})

	assert.Equal(t, 2*time.Second+tapeMargin, tapeWait(fs, "/small", 50))
	assert.Equal(t, maxTapeWait, tapeWait(fs, "/huge", 50))
	assert.Equal(t, maxTapeWait, tapeWait(fs, "/missing", 50))
	assert.Equal(t, maxTapeWait, tapeWait(fs, "/small", 0))
}

func TestFirstPlan_UsesTheFirstThatHasOne(t *testing.T) {
	t.Parallel()
	none := func(string) loadPlanner {
		return func(_ *config.Instance, _ string) (loadPlan, bool, error) { return loadPlan{}, false, nil }
	}
	some := func(target string) loadPlanner {
		return func(_ *config.Instance, _ string) (loadPlan, bool, error) { return loadPlan{target: target}, true, nil }
	}

	plan, ok, err := firstPlan(none("a"), some("b"), some("c"))(nil, "/x")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "b", plan.target)

	_, ok, err = firstPlan(none("a"))(nil, "/x")
	require.NoError(t, err)
	assert.False(t, ok)

	boom := func(_ *config.Instance, _ string) (loadPlan, bool, error) { return loadPlan{}, false, errNoSuchDisk }
	_, _, err = firstPlan(boom, some("c"))(nil, "/x")
	require.ErrorIs(t, err, errNoSuchDisk)
}

func zipBytes(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func TestReadableMedia_ReadsAFileOutOfAZip(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{
		"/g/pack.zip":  zipBytes(t, "Inner Game.DSK", []byte("disk bytes")),
		"/g/plain.dsk": []byte("plain"),
	})

	readFS, readPath, err := readableMedia(fs, "/g/pack.zip/inner game.dsk")
	require.NoError(t, err)
	got, err := afero.ReadFile(readFS, readPath)
	require.NoError(t, err)
	assert.Equal(t, "disk bytes", string(got))
	assert.Equal(t, ".dsk", filepath.Ext(readPath))

	sameFS, samePath, err := readableMedia(fs, "/g/plain.dsk")
	require.NoError(t, err)
	assert.Equal(t, "/g/plain.dsk", samePath)
	assert.Same(t, fs, sameFS)

	_, _, err = readableMedia(fs, "/g/pack.zip/missing.dsk")
	require.ErrorIs(t, err, errNoSuchFile)

	_, _, err = readableMedia(fs, "/g/gone.zip/x.dsk")
	require.Error(t, err)
}

func TestLoosePlanners_CoCo2DiskInsideAZipIsRead(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{"/g/games.zip": zipBytes(t, "auto.dsk", decbImage("AUTO", "BAS", 0))})

	plan, ok, err := loosePlanners(fs)[systemdefs.SystemCoCo2](nil, "/g/games.zip/auto.dsk")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{`RUN"AUTO"{enter}`}, stepTexts(plan.steps))
	assert.Equal(t, "/g/games.zip/auto.dsk", plan.target)
}

func tapImage(version byte, pulses ...byte) []byte {
	data := make([]byte, tapHeader, tapHeader+len(pulses))
	copy(data, "C64-TAPE-RAW")
	data[12] = version
	return append(data, pulses...)
}

func TestTapDuration(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{
		"/short.tap": tapImage(0, 0x30, 0x42, 0x56),
		"/long.tap":  tapImage(1, 0x00, 0x40, 0x42, 0x0F, 0x10),
		"/bad.tap":   []byte("not a tape image at all"),
	})

	short, ok := tapDuration(fs, "/short.tap", 1_000_000)
	require.True(t, ok)
	assert.Equal(t, time.Duration(0x30+0x42+0x56)*8*time.Second/985248, short)

	long, ok := tapDuration(fs, "/long.tap", 1_000_000)
	require.True(t, ok)
	assert.Equal(t, time.Duration(0x0F4240+0x10*8)*time.Second/985248, long, "a long pulse is its own cycle count")

	c16 := tapImage(1, 0x00, 0x40, 0x42, 0x0F)
	copy(c16, "C16-TAPE-RAW")
	c16[tapMachineOff] = 2
	require.NoError(t, afero.WriteFile(fs, "/c16.tap", c16, 0o644))
	got, ok := tapDuration(fs, "/c16.tap", 1_000_000)
	require.True(t, ok)
	assert.Equal(t, time.Duration(1_000_000)*time.Second/886724, got, "the header's machine sets the clock")

	_, ok = tapDuration(fs, "/bad.tap", 1_000_000)
	assert.False(t, ok)
	_, ok = tapDuration(fs, "/missing.tap", 1_000_000)
	assert.False(t, ok)
}

func TestLoosePlanners_CommodoreTapeRunsAfterThePlay(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{"/g/a.tap": tapImage(1, 0x00, 0x40, 0x42, 0x0F)})

	plan, ok, err := loosePlanners(fs)[systemdefs.SystemC64](nil, "/g/a.tap")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{`LOAD"",1{enter}`, "RUN{enter}"}, stepTexts(plan.steps))
	assert.Equal(t, time.Duration(1_000_000)*time.Second/clockC64+tapeMargin, plan.steps[1].pause)
}

func TestLoosePlanners_BK0010BinStartsAtItsLoadAddress(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{
		"/g/game.bin":  {0x00, 0x02, 1, 0}, // loads at 1000 octal
		"/g/low.bin":   {0xF0, 0x01, 1, 0}, // loads at 760 octal, below the code
		"/g/high.bin":  {0x00, 0x40, 1, 0}, // loads at 40000 octal
		"/g/short.bin": {0x00},
	})
	planner := loosePlanners(fs)[systemdefs.SystemBK0011M]

	for path, want := range map[string]string{
		"/g/game.bin": "1000G",
		"/g/low.bin":  "1000G",
		"/g/high.bin": "40000G",
	} {
		plan, ok, err := planner(nil, path)
		require.NoError(t, err)
		require.True(t, ok, path)
		assert.Equal(t, []string{want}, stepTexts(plan.steps), path)
		require.Len(t, plan.options, 1)
		assert.Equal(t, "BK0010", plan.options[0].(mgls.Option).Value)
	}

	_, ok, err := planner(nil, "/g/short.bin")
	require.NoError(t, err)
	assert.False(t, ok, "a file too short to hold a load address launches as it always has")
}

func TestLoosePlanners_DisksMountedAfterBootAskForAReset(t *testing.T) {
	t.Parallel()
	planners := loosePlanners(memFS(t, nil))

	for _, tt := range []struct{ system, path string }{
		{systemdefs.SystemOric, "/g/a.dsk"},
		{systemdefs.SystemTRS80, "/g/a.dsk"},
		{systemdefs.SystemTRS80, "/g/a.jv1"},
	} {
		plan, ok, err := planners[tt.system](nil, tt.path)
		require.NoError(t, err)
		require.True(t, ok, tt.system)
		assert.Empty(t, plan.steps, tt.system)
		require.Len(t, plan.options, 1, tt.system)
		assert.IsType(t, mgls.Reset{}, plan.options[0], tt.system)
	}
}

func TestLoosePlanners_CommodoreTapesUseTheStockLoaders(t *testing.T) {
	t.Parallel()
	planners := loosePlanners(memFS(t, nil))

	c64, ok, err := planners[systemdefs.SystemC64](nil, "/g/a.tap")
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, c64.options, 1)
	assert.Equal(t, mgls.Option{Name: "System ROM", Value: "Standard C64"}, c64.options[0],
		"a DolphinDOS kernal sends tape loads to the disk drive")

	c128, ok, err := planners[systemdefs.SystemC128](nil, "/g/a.tap")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"GO64{enter}", "Y{enter}", `LOAD"",1{enter}`}, stepTexts(c128.steps))
}
