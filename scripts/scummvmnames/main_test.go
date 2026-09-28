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

// idColumnWidth is the width of the ID column in the fixtures below: the
// index of the first space on the dashed rule line, which is what parse
// itself uses to find the column boundary.
const idColumnWidth = 30

// scummvmListGames builds a fixture in the shape `scummvm --list-games`
// prints: a two-column table whose first dashed rule sets the ID column's
// width, "engine:gameid" in that column and the title after it.
func scummvmListGames(rows ...[2]string) []byte {
	lines := make([]string, 0, 3+len(rows))
	lines = append(lines,
		"All known game IDs (etc.)",
		"Game ID                        Full Title",
		strings.Repeat("-", idColumnWidth)+" "+strings.Repeat("-", 40),
	)
	for _, row := range rows {
		id := row[0]
		if pad := idColumnWidth - len(id); pad > 0 {
			id += strings.Repeat(" ", pad)
		}
		lines = append(lines, id+row[1])
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func TestParseReadsTheEngineGameIDTable(t *testing.T) {
	t.Parallel()
	data := scummvmListGames(
		[2]string{"sky:sky", "Beneath a Steel Sky (Floppy/DOS/English)"},
		[2]string{"scumm:monkey", "The Secret of Monkey Island (Floppy/DOS/English)"},
	)

	games, err := parse(data)

	require.NoError(t, err)
	require.Len(t, games, 2)
	assert.Equal(t, game{engine: "sky", id: "sky", title: "Beneath a Steel Sky (Floppy/DOS/English)"}, games[0])
	assert.Equal(t, game{
		engine: "scumm", id: "monkey", title: "The Secret of Monkey Island (Floppy/DOS/English)",
	}, games[1])
}

func TestParseSkipsRowsItCannotRead(t *testing.T) {
	t.Parallel()
	for name, row := range map[string][2]string{
		"no colon in the ID column": {"justanid", "A Title"},
		"empty engine":              {":sky", "A Title"},
		"empty ID":                  {"sky:", "A Title"},
		"a tab inside the title":    {"sky:sky", "A\tTitle"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			games, err := parse(scummvmListGames(row))
			require.Error(t, err, "no usable rows means no games found")
			assert.Nil(t, games)
		})
	}

	// A row shorter than the ID column's width is skipped, not misread.
	data := scummvmListGames([2]string{"sky:sky", "Beneath a Steel Sky"})
	data = append(data, []byte("short\n")...)
	games, err := parse(data)
	require.NoError(t, err)
	assert.Len(t, games, 1)
}

func TestParseRequiresAtLeastOneGame(t *testing.T) {
	t.Parallel()
	_, err := parse(scummvmListGames())
	require.ErrorContains(t, err, "no games found")
}

func TestRunWritesSortedDeduplicatedCatalog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in := filepath.Join(dir, "games.txt")
	require.NoError(t, os.WriteFile(in, scummvmListGames(
		[2]string{"scumm:monkey", "The Secret of Monkey Island"},
		[2]string{"sky:sky", "Beneath a Steel Sky"},
		[2]string{"sky:sky", "Beneath a Steel Sky"}, // an exact repeat, as a re-listing would print
	), 0o600))
	out := filepath.Join(dir, "names.tsv.gz")

	require.NoError(t, run("2.9.1", in, out))

	rows := readCatalog(t, out)
	require.Len(t, rows, 3, "the header plus one row per distinct id:engine pair")
	assert.Equal(t, "# ScummVM 2.9.1", rows[0])
	assert.Equal(t, "monkey\tscumm\tThe Secret of Monkey Island", rows[1])
	assert.Equal(t, "sky\tsky\tBeneath a Steel Sky", rows[2])
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

	err := run("2.9.1", filepath.Join(dir, "missing.txt"), out)

	require.Error(t, err)
	_, statErr := os.Stat(out)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestRunReportsNoGamesFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in := filepath.Join(dir, "games.txt")
	require.NoError(t, os.WriteFile(in, scummvmListGames(), 0o600))
	out := filepath.Join(dir, "names.tsv.gz")

	err := run("2.9.1", in, out)

	require.Error(t, err)
	_, statErr := os.Stat(out)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}
