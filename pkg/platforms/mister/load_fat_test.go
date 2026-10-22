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
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cardDir = "/media/fat/games/Card"

func cardFS(t *testing.T, files ...testFATFile) (fs afero.Fs, image string) {
	t.Helper()
	image = cardDir + "/boot.vhd"
	return memFS(t, map[string][]byte{image: buildTestFAT16(files)}), image
}

func stepTexts(steps []loadStep) []string {
	texts := make([]string, 0, len(steps))
	for _, s := range steps {
		texts = append(texts, s.text)
	}
	return texts
}

func TestFATCore_ListSkipsMachineFoldersAndOtherFiles(t *testing.T) {
	t.Parallel()
	fs, image := cardFS(t,
		testFATFile{dir: "GAMES", short: "JETSET~1.TAP", long: "Jet Set Willy.tap", data: []byte{1}},
		testFATFile{dir: "GAMES", short: "NOTES.TXT", data: []byte{1}},
		testFATFile{dir: "BIN", short: "TOOL.TAP", data: []byte{1}},
		testFATFile{short: "ROOT.Z80", data: []byte{1}},
	)

	got := spectrumCore().list(fs, image)

	assert.ElementsMatch(t, []string{"ROOT.Z80", "GAMES/Jet Set Willy.tap"}, got)
}

func TestFATCore_ListOfANonFATFileIsEmpty(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{cardDir + "/boot.vhd": make([]byte, 2048)})

	assert.Empty(t, spectrumCore().list(fs, cardDir+"/boot.vhd"))
	assert.Empty(t, spectrumCore().list(fs, cardDir+"/missing.vhd"))
}

func TestSelectFAT_ResolvesLongNamesToShortOnes(t *testing.T) {
	t.Parallel()
	fs, image := cardFS(t,
		testFATFile{dir: "GAMES", short: "JETSET~1.TAP", long: "Jet Set Willy.tap", data: []byte{1}},
	)

	sel, err := selectFAT(fs, image, "games/jet set willy.TAP")

	require.NoError(t, err)
	assert.Equal(t, "/GAMES/Jet Set Willy.tap", sel.long)
	assert.Equal(t, "/GAMES/JETSET~1.TAP", sel.short)
	assert.Equal(t, ".tap", sel.ext)
}

func TestSelectFAT_MissingFile(t *testing.T) {
	t.Parallel()
	fs, image := cardFS(t, testFATFile{short: "A.TAP", data: []byte{1}})

	_, err := selectFAT(fs, image, "nothing.tap")
	require.ErrorIs(t, err, errNoSuchFile)

	_, err = selectFAT(fs, image, "Volume 2/a.tap")
	require.ErrorIs(t, err, errNoSuchFile)
}

func TestSpectrumCore_TapeLoadsThroughEsxDOS(t *testing.T) {
	t.Parallel()
	fs, image := cardFS(t,
		testFATFile{dir: "GAMES", short: "JETSET~1.TAP", long: "Jet Set Willy.tap", data: []byte{1}},
	)

	plan, ok, err := spectrumCore().planner(fs)(nil, image+"/games/Jet Set Willy.tap")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, image, plan.target)
	assert.Equal(t, []string{
		chord("ctrl", "f11"), "{down}{down}{down}{enter}", ".tapein /GAMES/JETSET~1.TAP{enter}", `J""{enter}`,
	}, stepTexts(plan.steps))
	for _, text := range stepTexts(plan.steps) {
		_, tokenErr := keyTokens(text, keyLayout{})
		require.NoError(t, tokenErr)
	}
}

func TestSpectrumCore_SnapshotLoadsWithSnapload(t *testing.T) {
	t.Parallel()
	fs, image := cardFS(t, testFATFile{short: "GAME.Z80", data: []byte{1}})

	plan, ok, err := spectrumCore().planner(fs)(nil, image+"/game.z80")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, ".snapload /GAME.Z80{enter}", stepTexts(plan.steps)[2])
}

func TestFATCore_BareImageLaunchesPlain(t *testing.T) {
	t.Parallel()
	fs, image := cardFS(t, testFATFile{short: "GAME.Z80", data: []byte{1}})

	_, ok, err := spectrumCore().planner(fs)(nil, image)

	require.NoError(t, err)
	assert.False(t, ok)
}

func TestNextCore_LoadsNEXProgramsOnly(t *testing.T) {
	t.Parallel()
	fs, image := cardFS(t,
		testFATFile{dir: "GAMES", short: "ELITE.NEX", data: []byte{1}},
		testFATFile{dir: "GAMES", short: "JSW.TAP", data: []byte{1}},
	)

	assert.Equal(t, []string{"GAMES/ELITE.NEX"}, nextCore().list(fs, image))

	plan, ok, err := nextCore().planner(fs)(nil, image+"/games/ELITE.NEX")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{
		"{f1}", "l", strings.Repeat("{backspace}", 20) + "{enter}", ".nexload /games/elite.nex{enter}",
	}, stepTexts(plan.steps))
	for _, text := range stepTexts(plan.steps) {
		_, tokenErr := keyTokens(text, plan.layout)
		require.NoError(t, tokenErr)
	}
}

func TestNextCore_SkipsTheOperatingSystemFolders(t *testing.T) {
	t.Parallel()
	fs, image := cardFS(t,
		testFATFile{dir: "NEXTZXOS", short: "SYS.TAP", data: []byte{1}},
		testFATFile{dir: "GAMES", short: "ELITE.NEX", data: []byte{1}},
	)

	assert.Equal(t, []string{"GAMES/ELITE.NEX"}, nextCore().list(fs, image))
}

func TestMSXCore_RomNeedsTheMegaSCCSlot(t *testing.T) {
	t.Parallel()
	fs, image := cardFS(t,
		testFATFile{dir: "ROMS", short: "GAME.ROM", data: []byte{1}},
		testFATFile{dir: "DISKS", short: "GAME.DSK", data: []byte{1}},
	)
	plan := msxCore().planner(fs)

	rom, ok, err := plan(nil, image+"/roms/game.rom")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"{down}", "{down}", "{space}", `romload a:\ROMS\GAME.ROM /r{enter}`}, stepTexts(rom.steps))
	require.Len(t, rom.options, 1)
	assert.Equal(t, "Slot1", rom.options[0].(mgls.Option).Name)
	assert.Equal(t, "MegaSCC+ 1MB", rom.options[0].(mgls.Option).Value)

	assert.Equal(t, []string{"ROMS/GAME.ROM"}, msxCore().list(fs, image), "disk images are not listed")
}

func TestAtomCore_RunsByAbsolutePath(t *testing.T) {
	t.Parallel()
	fs, image := cardFS(t, testFATFile{dir: "GAMES", short: "ASTEROID", data: []byte{1}})

	plan, ok, err := atomCore().planner(fs)(nil, image+"/games/asteroid")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"{f10}", `*RUN "/GAMES/ASTEROID"{enter}`}, stepTexts(plan.steps))
}

func TestEnterpriseCore_EntersTheFolderOnTheVolumesDrive(t *testing.T) {
	t.Parallel()
	fs, image := cardFS(t, testFATFile{dir: "GAMES", short: "TETRIS.COM", data: []byte{1}})

	plan, ok, err := enterpriseCore().planner(fs)(nil, image+"/games/tetris.com")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{
		chord("ctrl", "f9"), "{enter}", ":EXDOS{enter}", "F:{enter}", "CD GAMES{enter}", "LOAD TETRIS.COM{enter}",
	}, stepTexts(plan.steps))
}

func TestDosPath(t *testing.T) {
	t.Parallel()
	assert.Equal(t, `GAMES\A.ROM`, dosPath("/GAMES/A.ROM"))
	assert.Equal(t, "A.ROM", dosPath("A.ROM"))
}
