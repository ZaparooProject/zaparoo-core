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

package database

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalScrapePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, input := range []string{
		"", "relative", "../escape", "../bad://file", root + "/../escape", root + "\x00", strings.Repeat("x", 4097),
	} {
		_, err := CanonicalScrapePath(input, true)
		require.Error(t, err)
	}
	got, err := CanonicalScrapePath(root+"/./games//", true)
	require.NoError(t, err)
	require.Equal(t, filepath.ToSlash(filepath.Join(root, "games")), got)
	if volume := filepath.VolumeName(root); volume != "" {
		got, err = CanonicalScrapePath(volume+"//games", true)
		require.NoError(t, err)
		require.Equal(t, filepath.ToSlash(filepath.Join(volume+"/", "games")), got)
	}
	got, err = CanonicalScrapePath("steam://123", false)
	require.NoError(t, err)
	require.Equal(t, "steam://123", got)
	_, err = CanonicalScrapePath("steam://123", true)
	require.Error(t, err)
}

func FuzzCanonicalScrapePath(f *testing.F) {
	for _, seed := range []string{
		"/games/foo", "/", "../escape", "/games/../escape", "steam://123", "/games/foobar", "C:\\games", "C://games",
	} {
		f.Add(seed, true)
		f.Add(seed, false)
	}
	f.Fuzz(func(t *testing.T, input string, subtree bool) {
		path, err := CanonicalScrapePath(input, subtree)
		if err != nil {
			return
		}
		second, err := CanonicalScrapePath(path, subtree)
		require.NoError(t, err)
		require.Equal(t, path, second)
		if subtree {
			require.NotContains(t, path, "://")
			require.True(t, filepath.IsAbs(path))
		}
	})
}
