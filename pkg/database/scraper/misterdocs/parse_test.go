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
	"bytes"
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

func TestLoadArtworkRecords_OmitsAmbiguousDuplicateNames(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "SNES", "Artwork")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	files := map[string][]byte{
		"index.tsv":  []byte("#name\tkey\nGame\tFirst\nGame\tSecond\n"),
		"First.jpg":  []byte("first"),
		"Second.jpg": []byte("second"),
	}
	for name, content := range files {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, name), content, 0o600))
	}

	got, err := loadArtworkRecords(context.Background(), fs, dir)
	require.NoError(t, err)
	assert.Empty(t, got.Artwork)
	assert.Equal(t, 2, got.RowErrors)
}

func TestLoadArtworkRecords_SkipsMalformedOptionalMetadata(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "SNES", "Artwork")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	files := map[string][]byte{
		"index.tsv":       []byte("#name\tkey\nGame\tGame\n"),
		"Game.jpg":        []byte("image"),
		"gameinfo.tsv":    []byte("#name\nGame\n"),
		"synopsis_en.tsv": []byte("#key\nGame\n"),
	}
	for name, content := range files {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, name), content, 0o600))
	}

	got, err := loadArtworkRecords(context.Background(), fs, dir)
	require.NoError(t, err)
	require.Len(t, got.Artwork, 1)
	assert.Empty(t, got.GameInfo)
	assert.Empty(t, got.Synopsis)
	assert.Equal(t, 2, got.RowErrors)
}

func TestLoadArtworkRecords_SkipsDuplicateOptionalMetadataKeys(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "SNES", "Artwork")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	files := map[string][]byte{
		"index.tsv": []byte("#name\tkey\nGame\tGame\n"),
		"Game.jpg":  []byte("image"),
		"gameinfo.tsv": []byte("#key\tyear\n" +
			"Game\t1994\nGame\t1995\n"),
		"synopsis_en.tsv": []byte("#key\tsynopsis\n" +
			"Game\tFirst\nGame\tSecond\n"),
	}
	for name, content := range files {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, name), content, 0o600))
	}

	got, err := loadArtworkRecords(context.Background(), fs, dir)
	require.NoError(t, err)
	require.Len(t, got.Artwork, 1)
	assert.Empty(t, got.GameInfo)
	assert.Empty(t, got.Synopsis)
	assert.Equal(t, 2, got.RowErrors)
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

func TestLoadSourceRecords_HandlesManualsAndRejectsUnknownKinds(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "SNES", "Manuals")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	manualPath := filepath.Join(dir, "Game.pdf")
	require.NoError(t, afero.WriteFile(fs, manualPath, []byte("pdf"), 0o600))

	got, err := loadSourceRecords(context.Background(), fs, sourceDir{Path: dir, Kind: sourceManuals})
	require.NoError(t, err)
	assert.Equal(t, []string{manualPath}, got.Manuals)

	_, err = loadSourceRecords(context.Background(), fs, sourceDir{Path: dir, Kind: sourceKind(255)})
	require.ErrorContains(t, err, "unknown source kind")
}

func TestReadTSV_RejectsExcessInvalidUTF8Rows(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := filepath.Join("docs", "index.tsv")
	records := bytes.Repeat([]byte{0xff, '\n'}, maxMetadataRecords+1)
	data := append([]byte("#name\n"), records...)
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, afero.WriteFile(fs, path, data, 0o600))

	_, err := readTSV(context.Background(), fs, path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "metadata exceeds 100000-record limit")
}

func TestReadTSV_RejectsOversizedMetadata(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := filepath.Join("docs", "index.tsv")
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, afero.WriteFile(fs, path, bytes.Repeat([]byte{'x'}, int(maxMetadataBytes)+1), 0o600))

	_, err := readTSV(context.Background(), fs, path)
	require.ErrorContains(t, err, "metadata exceeds 8388608-byte limit")
}

func TestImageFilesByStem_OmitsAmbiguousStems(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "SNES", "Artwork")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "Game.jpg"), []byte("jpg"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "Game.png"), []byte("png"), 0o600))

	images, err := imageFilesByStem(fs, dir)
	require.NoError(t, err)
	assert.NotContains(t, images, "game")
}

func TestAppendManualRecord_FiltersBeforeEnforcingLimit(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "SNES", "Manuals")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	readmePath := filepath.Join(dir, "README.txt")
	manualPath := filepath.Join(dir, "Extra.pdf")
	require.NoError(t, afero.WriteFile(fs, readmePath, []byte("text"), 0o600))
	require.NoError(t, afero.WriteFile(fs, manualPath, []byte("pdf"), 0o600))
	readmeInfo, err := fs.Stat(readmePath)
	require.NoError(t, err)
	manualInfo, err := fs.Stat(manualPath)
	require.NoError(t, err)
	full := make([]string, maxMetadataRecords)

	got, err := appendManualRecord(fs, dir, readmeInfo, full)
	require.NoError(t, err)
	assert.Len(t, got, maxMetadataRecords)

	_, err = appendManualRecord(fs, dir, manualInfo, full)
	require.ErrorContains(t, err, "manuals directory exceeds 100000 records")
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
