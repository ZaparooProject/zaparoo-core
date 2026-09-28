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

package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArcadeKeepsOnlyRunnableArcadeMachines(t *testing.T) {
	t.Parallel()
	base := machine{Runnable: "yes", ROMs: []present{{}}, Displays: []present{{}}}
	tests := []struct {
		edit func(*machine)
		name string
		want bool
	}{
		{name: "a plain arcade set", edit: func(*machine) {}, want: true},
		{name: "a set with a joystick control", edit: func(m *machine) {
			m.Controls = []control{{Type: "joy"}}
		}, want: true},
		{name: "a BIOS entry", edit: func(m *machine) { m.IsBIOS = "yes" }, want: false},
		{name: "a device entry", edit: func(m *machine) { m.IsDevice = "yes" }, want: false},
		{name: "a mechanical machine", edit: func(m *machine) { m.IsMechanical = "yes" }, want: false},
		{name: "a non-runnable machine", edit: func(m *machine) { m.Runnable = "no" }, want: false},
		{name: "no ROMs or disks", edit: func(m *machine) { m.ROMs = nil }, want: false},
		{name: "a disk instead of ROMs", edit: func(m *machine) {
			m.ROMs = nil
			m.Disks = []present{{}}
		}, want: true},
		{name: "no display", edit: func(m *machine) { m.Displays = nil }, want: false},
		{name: "a software list, like a console", edit: func(m *machine) {
			m.SoftLists = []present{{}}
		}, want: false},
		{name: "a keyboard control, like a computer", edit: func(m *machine) {
			m.Controls = []control{{Type: "keyboard"}}
		}, want: false},
		{name: "a keypad control", edit: func(m *machine) {
			m.Controls = []control{{Type: "keypad"}}
		}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := base
			tt.edit(&m)
			assert.Equal(t, tt.want, arcade(&m))
		})
	}
}

func TestCleanCollapsesWhitespace(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Donkey Kong", clean("Donkey Kong"))
	assert.Equal(t, "US set 1", clean("US\tset\n1"))
	assert.Equal(t, "a b", clean("  a   b  "))
	assert.Empty(t, clean("\t\n "))
}

func TestOpenReadsPlainAndZippedMachineLists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	plain := filepath.Join(dir, "mame.xml")
	require.NoError(t, os.WriteFile(plain, []byte("<mame/>"), 0o600))
	r, err := open(plain)
	require.NoError(t, err)
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "<mame/>", string(data))
	require.NoError(t, r.Close())

	zipped := filepath.Join(dir, "mame0289lx.zip")
	writeZip(t, zipped, map[string]string{"mame0289.xml": "<mame/>"})
	r, err = open(zipped)
	require.NoError(t, err)
	data, err = io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "<mame/>", string(data))
	require.NoError(t, r.Close())

	noXML := filepath.Join(dir, "empty.zip")
	writeZip(t, noXML, map[string]string{"readme.txt": "hi"})
	_, err = open(noXML)
	require.ErrorContains(t, err, "no .xml machine list")

	_, err = open(filepath.Join(dir, "missing.zip"))
	require.Error(t, err)
	_, err = open(filepath.Join(dir, "missing.xml"))
	require.Error(t, err)
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path) //nolint:gosec // test fixture path
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	for name, contents := range files {
		w, createErr := zw.Create(name)
		require.NoError(t, createErr)
		_, writeErr := w.Write([]byte(contents))
		require.NoError(t, writeErr)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
}

const testMachineList = `<?xml version="1.0"?>
<mame build="0.267 (mame4all)">
	<machine name="dkong" runnable="yes">
		<description>Donkey Kong (US set 1)</description>
		<year>1981</year>
		<manufacturer>Nintendo of America</manufacturer>
		<rom/>
		<display/>
	</machine>
	<machine name="neogeo" isbios="yes" runnable="yes">
		<description>Neo-Geo</description>
		<year>1990</year>
		<manufacturer>SNK</manufacturer>
		<rom/>
		<display/>
	</machine>
	<machine name="pacman" runnable="yes">
		<description>	Pac-Man  </description>
		<year>1980</year>
		<manufacturer>Namco</manufacturer>
		<rom/>
		<display/>
	</machine>
	<machine name="apple2" runnable="yes">
		<description>Apple II</description>
		<year>1977</year>
		<manufacturer>Apple</manufacturer>
		<rom/>
		<display/>
		<softwarelist name="apple2_flop"/>
	</machine>
	<machine name="nodesc" runnable="yes">
		<description>   </description>
		<year>1999</year>
		<manufacturer>Nobody</manufacturer>
		<rom/>
		<display/>
	</machine>
</mame>
`

func TestRunWritesSortedArcadeCatalog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in := filepath.Join(dir, "mame.xml")
	require.NoError(t, os.WriteFile(in, []byte(testMachineList), 0o600))
	out := filepath.Join(dir, "names.tsv.gz")

	require.NoError(t, run(in, out))

	rows := readCatalog(t, out)
	require.Len(t, rows, 3, "the header plus only runnable arcade sets with a description")
	assert.Equal(t, "# MAME 0.267", rows[0], "the build's version, not its full string")
	assert.Equal(t, "dkong\tDonkey Kong (US set 1)\t1981\tNintendo of America", rows[1])
	// pacman sorts after dkong and its description is trimmed and collapsed.
	assert.Equal(t, "pacman\tPac-Man\t1980\tNamco", rows[2])
}

func readCatalog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test fixture path
	require.NoError(t, err)
	zr, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	t.Cleanup(func() { _ = zr.Close() })
	contents, err := io.ReadAll(zr)
	require.NoError(t, err)
	return strings.Split(strings.TrimRight(string(contents), "\n"), "\n")
}

func TestRunReportsAnUnreadableInput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "names.tsv.gz")

	err := run(filepath.Join(dir, "missing.xml"), out)

	require.Error(t, err)
	_, statErr := os.Stat(out)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestRunReportsUnparsableXML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in := filepath.Join(dir, "mame.xml")
	require.NoError(t, os.WriteFile(in, []byte("<mame><machine name=\"x\">"), 0o600))
	out := filepath.Join(dir, "names.tsv.gz")

	err := run(in, out)

	require.Error(t, err)
	_, statErr := os.Stat(out)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}
