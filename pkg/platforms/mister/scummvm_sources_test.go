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

//go:build linux

package mister

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/require"
)

func TestScummVMMetadataSources(t *testing.T) {
	t.Parallel()
	absolute := filepath.Join(t.TempDir(), "games", "Monkey.v1")
	got := scummVMMetadataSources([]ScummVMGame{{TargetID: "monkey", Path: absolute}})
	require.Equal(t, []*platforms.MediaSource{
		{Path: absolute, Root: filepath.Dir(absolute), Kind: platforms.MediaSourceDirectory},
	}, got)

	relative := scummVMMetadataSources([]ScummVMGame{{TargetID: "monkey", Path: "games/Monkey"}})[0]
	require.Equal(t, filepath.Join(scummvmBaseDir, "games", "Monkey"), relative.Path)
	require.Equal(t, filepath.Join(scummvmBaseDir, "games"), relative.Root)

	for _, path := range []string{"", "remote://game", "bad\x00path", string(filepath.Separator)} {
		require.Equal(t, []*platforms.MediaSource{nil}, scummVMMetadataSources([]ScummVMGame{{Path: path}}))
	}
}

func TestScummVMMetadataSourcesFoldVariantRoots(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	games, other := filepath.Join(base, "SD", "games"), filepath.Join(base, "NAS", "games")
	roots := func(paths ...string) []string {
		configured := make([]ScummVMGame, 0, len(paths))
		for _, path := range paths {
			configured = append(configured, ScummVMGame{Path: path})
		}
		got := make([]string, 0, len(paths))
		for i, source := range scummVMMetadataSources(configured) {
			require.NotNil(t, source)
			require.Equal(t, paths[i], source.Path)
			got = append(got, source.Root)
		}
		return got
	}

	require.Equal(t, []string{games, games, games, filepath.Join(games, "sierra", "kq")}, roots(
		filepath.Join(games, "monkey"),
		filepath.Join(games, "kyra3", "dos-english"),
		filepath.Join(games, "kyra3", "dos-french"),
		filepath.Join(games, "sierra", "kq", "amiga"),
	), "a root folds only into its own parent, never across an unconfigured folder")

	require.Equal(t, []string{games, games, games}, roots(
		filepath.Join(games, "monkey"),
		filepath.Join(games, "sierra", "kq1"),
		filepath.Join(games, "sierra", "kq2", "amiga"),
	), "folding follows a chain of roots")

	kyra := filepath.Join(games, "kyra3")
	require.Equal(t, []string{kyra, kyra, other}, roots(
		filepath.Join(kyra, "dos-english"),
		filepath.Join(kyra, "dos-french"),
		filepath.Join(other, "monkey"),
	), "a root with no enclosing root stays where it is")
}
