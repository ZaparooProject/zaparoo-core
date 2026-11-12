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
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/zstdembed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const headerLine = "Title,Title (exact),Title screen,Title screen (exact),ID,Region,Release date," +
	"Developer,Publisher,Tags,MD5,SHA1,SHA256,SHA512\n"

// checkout builds a directory that looks like an upstream checkout at commit,
// with a header-only copy of every expected CSV. rows replaces the content of
// the files it names.
func checkout(t *testing.T, commit string, rows map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git", "refs", "heads"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "refs", "heads", "main"), []byte(commit+"\n"), 0o600))
	for name := range bundles {
		content := headerLine
		if body, ok := rows[name]; ok {
			content = body
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tags.yml"), []byte("genre:\n"), 0o600))
	return dir
}

func readBundle(t *testing.T, dir, bundle string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, bundle+bundleSuffix)) //nolint:gosec // test fixture path
	require.NoError(t, err)
	zr, err := zstdembed.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	t.Cleanup(func() { _ = zr.Close() })
	contents, err := io.ReadAll(zr)
	require.NoError(t, err)
	if len(contents) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(string(contents), "\n"), "\n")
}

func TestRunWritesBundlesFromThePinnedCommit(t *testing.T) {
	t.Parallel()
	in := checkout(t, pinnedCommit, map[string]string{
		"console_nintendo_virtualboy.csv": headerLine +
			"Mario Clash,Mario Clash マリオクラッシュ,=,=,marioclash,Japan,1995-09-28,Nintendo,Nintendo," +
			"#players:1 #genre:action>platformer,md5,sha1,sha256,sha512\n" +
			"\"Red, Alarm\",x,\"Red\tAlarm\",=,\tredalarm ,USA/Europe,1995,T&E,T&E,  #players:1   #lang:en ,a,b,c,d\n",
		"arcade_irem.csv":   headerLine + "Air Duel,=,=,=,airduel,Japan,1990,Irem,Irem,#mamerom:airduel,a,b,c,d\n",
		"arcade_capcom.csv": headerLine + "1941,=,=,=,1941,World,1990,Capcom,Capcom,#mamerom:1941,a,b,c,d\n",
	})
	out := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.MkdirAll(out, 0o750))
	stale := filepath.Join(out, "retired"+bundleSuffix)
	require.NoError(t, os.WriteFile(stale, []byte("old"), 0o600))

	require.NoError(t, run(in, out))

	assert.Equal(t, []string{
		"Mario Clash\t=\tmarioclash\tJapan\t1995-09-28\tNintendo\tNintendo\t#players:1 #genre:action>platformer",
		"Red, Alarm\tRed Alarm\tredalarm\tUSA/Europe\t1995\tT&E\tT&E\t#players:1 #lang:en",
	}, readBundle(t, out, "virtualboy"), "kept columns only, whitespace collapsed, rows in upstream order")
	assert.Equal(t, []string{
		"1941\t=\t1941\tWorld\t1990\tCapcom\tCapcom\t#mamerom:1941",
		"Air Duel\t=\tairduel\tJapan\t1990\tIrem\tIrem\t#mamerom:airduel",
	}, readBundle(t, out, "arcade"), "arcade files merge in file-name order")
	assert.Empty(t, readBundle(t, out, "snes"))

	version, err := os.ReadFile(filepath.Join(out, versionFile)) //nolint:gosec // test fixture path
	require.NoError(t, err)
	assert.Equal(t, pinnedCommit+" "+pinnedDate+"\n", string(version))
	assert.NoFileExists(t, stale, "a bundle no file feeds any more is removed")

	first, err := os.ReadFile(filepath.Join(out, "virtualboy"+bundleSuffix)) //nolint:gosec // test fixture path
	require.NoError(t, err)
	require.NoError(t, run(in, out))
	second, err := os.ReadFile(filepath.Join(out, "virtualboy"+bundleSuffix)) //nolint:gosec // test fixture path
	require.NoError(t, err)
	assert.Equal(t, first, second, "the same input gives the same bytes")
}

func TestRunRefusesWhatItWasNotPinnedTo(t *testing.T) {
	t.Parallel()
	row := "A,=,=,=,a,Japan,1990,Dev,Pub,#players:1,a,b,c,d\n"
	for _, tc := range []struct {
		mutate func(t *testing.T, dir string)
		rows   map[string]string
		name   string
		commit string
		want   string
	}{
		{name: "another commit", commit: strings.Repeat("0", 40), want: "pinned to"},
		{
			name: "a new upstream file", commit: pinnedCommit, want: "not in the bundle table",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				added := filepath.Join(dir, "console_sony_playstation.csv")
				require.NoError(t, os.WriteFile(added, []byte(headerLine), 0o600))
			},
		},
		{
			name: "a missing upstream file", commit: pinnedCommit, want: "expected CSV files",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.Remove(filepath.Join(dir, "arcade_snk.csv")))
			},
		},
		{
			name: "a changed header", commit: pinnedCommit, want: "unexpected header",
			rows: map[string]string{"arcade_snk.csv": strings.Replace(headerLine, "Region", "Territory", 1)},
		},
		{
			name: "a short row", commit: pinnedCommit, want: "wrong number of fields",
			rows: map[string]string{"arcade_snk.csv": headerLine + "A,=,=,=,a,Japan\n"},
		},
		{
			name: "an empty title", commit: pinnedCommit, want: "empty title",
			rows: map[string]string{"arcade_snk.csv": headerLine + " ,=,=,=,a,Japan,1990,Dev,Pub,#players:1,a,b,c,d\n"},
		},
		{
			name: "invalid UTF-8", commit: pinnedCommit, want: "invalid UTF-8",
			rows: map[string]string{"arcade_snk.csv": headerLine + strings.Replace(row, "Dev", "D\xffv", 1)},
		},
		{
			name: "an oversized cell", commit: pinnedCommit, want: "cell of",
			rows: map[string]string{
				"arcade_snk.csv": headerLine + strings.Replace(row, "Dev", strings.Repeat("d", maxCellBytes+1), 1),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := checkout(t, tc.commit, tc.rows)
			if tc.mutate != nil {
				tc.mutate(t, in)
			}
			err := run(in, t.TempDir())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestCheckoutCommit(t *testing.T) {
	t.Parallel()
	dir := checkout(t, pinnedCommit, nil)
	commit, err := checkoutCommit(dir)
	require.NoError(t, err)
	assert.Equal(t, pinnedCommit, commit)

	// A detached HEAD holds the commit itself.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte(pinnedCommit+"\n"), 0o600))
	commit, err = checkoutCommit(dir)
	require.NoError(t, err)
	assert.Equal(t, pinnedCommit, commit)

	// A packed ref has no file of its own.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o600))
	require.NoError(t, os.Remove(filepath.Join(dir, ".git", "refs", "heads", "main")))
	_, err = checkoutCommit(dir)
	require.Error(t, err)
	packed := "# pack-refs with: peeled fully-peeled sorted\n" + pinnedCommit + " refs/heads/main\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "packed-refs"), []byte(packed), 0o600))
	commit, err = checkoutCommit(dir)
	require.NoError(t, err)
	assert.Equal(t, pinnedCommit, commit)

	_, err = checkoutCommit(t.TempDir())
	require.Error(t, err, "a directory that is not a checkout")
}

func TestEveryKeptColumnIsNamedAndEveryBundleIsFed(t *testing.T) {
	t.Parallel()
	assert.Len(t, keptColumns, 8)
	for _, column := range keptColumns {
		assert.Less(t, column, 10, "hash columns are never kept")
	}
	names := map[string]struct{}{}
	for _, bundle := range bundles {
		names[bundle] = struct{}{}
	}
	assert.Len(t, names, 24)
	assert.Len(t, bundles, 31)
}
