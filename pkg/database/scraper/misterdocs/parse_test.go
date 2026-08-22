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

package misterdocs

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadArtworkRecords_ImportsIndexAndOptionalMetadata(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "SNES", "Artwork")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	files := map[string]string{
		"index.tsv": "#name\tcrc\tsize\tkey\n" +
			"Game (USA)\t1234\t42\tCanonical Game\nMissing\t\t\tNo Image\n",
		"gameinfo.tsv": "#key\tname\tyear\tgenre\tdeveloper\tplayers\n" +
			"Canonical Game\tGame\t1994\tPlatform\tStudio\t1-4\n",
		"synopsis_en.tsv":    "#key\tsynopsis\nCanonical Game\tA &amp; B\n",
		"Canonical Game.jpg": "image",
	}
	for name, content := range files {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, name), []byte(content), 0o600))
	}

	got, err := loadArtworkRecords(context.Background(), fs, dir)
	require.NoError(t, err)
	require.Len(t, got.Artwork, 1)
	assert.Equal(t, "Game (USA)", got.Artwork[0].Name)
	assert.Equal(t, filepath.Join(dir, "Canonical Game.jpg"), got.Artwork[0].ImagePath)
	assert.Equal(t, "1994", got.GameInfo["Canonical Game"].Year)
	assert.Equal(t, "A &amp; B", got.Synopsis["Canonical Game"])
	assert.Equal(t, 1, got.RowErrors)
}

func TestLoadArtworkRecords_RequiresColumns(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "SNES", "Artwork")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "index.tsv"), []byte("#name\nGame\n"), 0o600))

	_, err := loadArtworkRecords(context.Background(), fs, dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires name and key")
}

func TestLoadManualRecords_FiltersDirectPDFs(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "SNES", "Manuals")
	require.NoError(t, fs.MkdirAll(filepath.Join(dir, "nested"), 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "Game.pdf"), []byte("pdf"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "readme.txt"), []byte("text"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "nested", "Other.pdf"), []byte("pdf"), 0o600))

	got, err := loadManualRecords(context.Background(), fs, dir)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, "Game.pdf")}, got)
}

func TestNormalizePlayers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "4", normalizePlayers("1-4"))
	assert.Equal(t, "8", normalizePlayers("1, 2 / 8"))
	assert.Empty(t, normalizePlayers("unknown"))
}
