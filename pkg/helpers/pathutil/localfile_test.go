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

package pathutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalFileUnder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "art"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "cover.png"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "art", "front.png"), []byte("xy"), 0o600))

	path, info, err := LocalFileUnder(root, "cover.png")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "cover.png"), path)
	assert.Equal(t, int64(1), info.Size())

	path, info, err = LocalFileUnder(root, "art/front.png")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "art", "front.png"), path)
	assert.Equal(t, int64(2), info.Size())

	path, _, err = LocalFileUnder(root, "./art/../cover.png")
	require.NoError(t, err, "a path that stays under the root is cleaned")
	assert.Equal(t, filepath.Join(root, "cover.png"), path)
}

func TestLocalFileUnderRefusesEscapes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "art"), 0o750))
	outside := filepath.Join(t.TempDir(), "secret.png")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o600))

	for _, rel := range []string{
		"",
		"art",
		"missing.png",
		"../secret.png",
		"art/../../secret.png",
		outside,
		filepath.ToSlash(outside),
		`art\..\..\secret.png`,
	} {
		_, _, err := LocalFileUnder(root, rel)
		require.Error(t, err, "rel %q", rel)
	}
	_, _, err := LocalFileUnder("", "cover.png")
	assert.ErrorIs(t, err, ErrNotLocalFile)
}

func TestLocalFileUnderRefusesLinks(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires Windows privileges")
	}
	root := t.TempDir()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "secret.png")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "cover.png")))
	require.NoError(t, os.Symlink(outsideDir, filepath.Join(root, "art")))

	_, _, err := LocalFileUnder(root, "cover.png")
	require.ErrorIs(t, err, ErrNotLocalFile, "a linked file is refused")
	_, _, err = LocalFileUnder(root, "art/secret.png")
	require.ErrorIs(t, err, ErrNotLocalFile, "a linked directory on the way is refused")
}
