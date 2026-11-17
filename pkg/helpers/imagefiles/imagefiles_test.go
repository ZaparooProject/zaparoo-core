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

package imagefiles_test

import (
	"bytes"
	"image"
	"image/png"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/imagefiles"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pngBytes(t *testing.T, side int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, side, side))))
	return buf.Bytes()
}

func TestStoreSavePathAndRead(t *testing.T) {
	t.Parallel()
	store := &imagefiles.Store{FS: afero.NewMemMapFs(), Dir: filepath.Join("data", "images")}
	data := pngBytes(t, 4)

	name, err := store.Save("IMG", data)
	require.NoError(t, err)
	assert.Regexp(t, `^img-[0-9a-f]{16}\.png$`, name)
	again, err := store.Save("img", data)
	require.NoError(t, err)
	assert.Equal(t, name, again, "the same image is the same file")

	path, err := store.Path(name)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("data", "images", name), path)
	got, err := store.Read(name)
	require.NoError(t, err)
	assert.Equal(t, data, got)

	for _, missing := range []string{"", "..", "other.png", filepath.Join("..", name)} {
		_, err = store.Path(missing)
		require.ErrorIs(t, err, imagefiles.ErrNotFound, "name %q", missing)
	}
}

func TestStoreRejectsWhatItDoesNotKeep(t *testing.T) {
	t.Parallel()
	store := &imagefiles.Store{FS: afero.NewMemMapFs(), Dir: "images"}

	_, err := store.Save("img", []byte("plain text"))
	require.ErrorIs(t, err, imagefiles.ErrNotImage)
	_, err = store.Save("img", make([]byte, imagefiles.MaxBytes+1))
	require.ErrorIs(t, err, imagefiles.ErrTooLarge)
	_, err = store.Save("../escape", pngBytes(t, 4))
	require.Error(t, err)
}

func TestStorePruneKeepsNamedAndRecentFiles(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	store := &imagefiles.Store{FS: fs, Dir: "images"}
	kept, err := store.Save("img", pngBytes(t, 4))
	require.NoError(t, err)
	unused, err := store.Save("img", pngBytes(t, 5))
	require.NoError(t, err)
	recent, err := store.Save("img", pngBytes(t, 6))
	require.NoError(t, err)
	now := time.Now()
	old := now.Add(-time.Hour)
	for _, name := range []string{kept, unused} {
		require.NoError(t, fs.Chtimes(filepath.Join("images", name), old, old))
	}

	store.Prune(map[string]struct{}{kept: {}}, now)

	_, err = store.Path(kept)
	require.NoError(t, err)
	_, err = store.Path(unused)
	require.ErrorIs(t, err, imagefiles.ErrNotFound)
	_, err = store.Path(recent)
	require.NoError(t, err, "a file written moments ago may not be named yet")
}
