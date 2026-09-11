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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cancelOnReaddirFile struct {
	afero.File
	cancel context.CancelFunc
	calls  int
}

func (file *cancelOnReaddirFile) Readdir(count int) ([]os.FileInfo, error) {
	entries, err := file.File.Readdir(count)
	file.calls++
	file.cancel()
	return entries, err //nolint:wrapcheck // Test boundary preserves underlying directory behavior.
}

type cancelOnReaddirFS struct {
	afero.Fs
	file   *cancelOnReaddirFile
	cancel context.CancelFunc
	path   string
}

func (fs *cancelOnReaddirFS) Open(name string) (afero.File, error) {
	file, err := fs.Fs.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", name, err)
	}
	if filepath.Clean(name) != filepath.Clean(fs.path) {
		return file, nil
	}
	fs.file = &cancelOnReaddirFile{File: file, cancel: fs.cancel}
	return fs.file, nil
}

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

	got, err := loadArtworkRecords(context.Background(), fs, dir, nil)
	require.NoError(t, err)
	require.Len(t, got.Artwork, 2)
	assert.Equal(t, "Game (USA)", got.Artwork[0].Name)
	assert.Equal(t, filepath.Join(dir, "Canonical Game.jpg"), got.Artwork[0].ImagePath)
	assert.True(t, got.Artwork[0].SlugUnique)
	// The image the index points at is also reachable under its own key, but
	// only by exact name: it is not an index row, so no bare-title fallback.
	assert.Equal(t, artworkRecord{
		Name: "Canonical Game", Key: "Canonical Game", ImagePath: filepath.Join(dir, "Canonical Game.jpg"),
	}, got.Artwork[1])
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

	got, err := loadArtworkRecords(context.Background(), fs, dir, nil)
	require.NoError(t, err)
	// Neither key may claim the shared name, but each image still resolves
	// under its own key.
	require.Len(t, got.Artwork, 2)
	assert.Equal(t, "First", got.Artwork[0].Name)
	assert.Equal(t, "Second", got.Artwork[1].Name)
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

	got, err := loadArtworkRecords(context.Background(), fs, dir, nil)
	require.NoError(t, err)
	require.Len(t, got.Artwork, 1)
	assert.Empty(t, got.GameInfo)
	assert.Empty(t, got.Synopsis)
	assert.Equal(t, 2, got.RowErrors)
}

func TestLoadArtworkRecords_KeepsFirstOfDuplicateOptionalMetadataKeys(t *testing.T) {
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

	got, err := loadArtworkRecords(context.Background(), fs, dir, nil)
	require.NoError(t, err)
	require.Len(t, got.Artwork, 1)
	assert.Equal(t, "1994", got.GameInfo["Game"].Year)
	assert.Equal(t, "First", got.Synopsis["Game"])
	assert.Equal(t, 2, got.RowErrors)
}

func TestLoadArtworkRecords_RequiresColumns(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "SNES", "Artwork")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "index.tsv"), []byte("#name\nGame\n"), 0o600))

	_, err := loadArtworkRecords(context.Background(), fs, dir, nil)
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

	got, err := loadSourceRecords(context.Background(), fs, sourceDir{Path: dir, Kind: sourceManuals}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{manualPath}, got.Manuals)

	_, err = loadSourceRecords(context.Background(), fs, sourceDir{Path: dir, Kind: sourceKind(255)}, nil)
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

	images, err := imageFilesByStem(context.Background(), fs, dir)
	require.NoError(t, err)
	assert.NotContains(t, images, "game")
}

func TestImageFilesByStem_StopsEnumerationAfterCancellation(t *testing.T) {
	t.Parallel()

	baseFS := afero.NewMemMapFs()
	dir := filepath.Join("docs", "SNES", "Artwork")
	require.NoError(t, baseFS.MkdirAll(dir, 0o750))
	require.NoError(t, afero.WriteFile(baseFS, filepath.Join(dir, "Game.jpg"), []byte("image"), 0o600))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fs := &cancelOnReaddirFS{Fs: baseFS, path: dir, cancel: cancel}

	_, err := imageFilesByStem(ctx, fs, dir)
	require.ErrorIs(t, err, context.Canceled)
	require.NotNil(t, fs.file)
	assert.Equal(t, 1, fs.file.calls)
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

func TestLoadArtworkRecords_PicksSynopsisByPreferredLanguage(t *testing.T) {
	t.Parallel()

	writeSynopsisPack := func(t *testing.T) (afero.Fs, string) {
		t.Helper()
		fs := afero.NewMemMapFs()
		dir := filepath.Join("docs", "Genesis", "Artwork")
		require.NoError(t, fs.MkdirAll(dir, 0o750))
		files := map[string]string{
			"index.tsv":       "#name\tkey\nGame\tGame\n",
			"Game.jpg":        "image",
			"synopsis_de.tsv": "#key\tsynopsis\nGame\tDeutsch\n",
			"synopsis_fr.tsv": "#key\tsynopsis\nGame\tFrançais\n",
		}
		for name, content := range files {
			require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, name), []byte(content), 0o600))
		}
		return fs, dir
	}

	tests := []struct {
		name  string
		want  string
		langs []string
	}{
		{name: "first preferred language present", langs: []string{"fr", "en"}, want: "Français"},
		{name: "later preferred language present", langs: []string{"es", "de"}, want: "Deutsch"},
		{name: "regional tag falls back to its base", langs: []string{"fr-CA"}, want: "Français"},
		{name: "underscore regional tag falls back to its base", langs: []string{"de_DE"}, want: "Deutsch"},
		{name: "case is ignored", langs: []string{"FR"}, want: "Français"},
		{name: "no preference and no english picks deterministically", langs: nil, want: "Deutsch"},
		{name: "unavailable preference picks deterministically", langs: []string{"es"}, want: "Deutsch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs, dir := writeSynopsisPack(t)
			got, err := loadArtworkRecords(context.Background(), fs, dir, tt.langs)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Synopsis["Game"])
		})
	}
}

func TestLoadArtworkRecords_PrefersEnglishOverArbitraryLanguage(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "Genesis", "Artwork")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	files := map[string]string{
		"index.tsv":       "#name\tkey\nGame\tGame\n",
		"Game.jpg":        "image",
		"synopsis_de.tsv": "#key\tsynopsis\nGame\tDeutsch\n",
		"synopsis_en.tsv": "#key\tsynopsis\nGame\tEnglish\n",
		"synopsis_it.tsv": "#key\tsynopsis\nGame\tItaliano\n",
	}
	for name, content := range files {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, name), []byte(content), 0o600))
	}

	got, err := loadArtworkRecords(context.Background(), fs, dir, []string{"es"})
	require.NoError(t, err)
	assert.Equal(t, "English", got.Synopsis["Game"])
}

func TestLoadArtworkRecords_AddsMetadataOnlyRecordsForImagelessGames(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "SNES", "Artwork")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	files := map[string]string{
		"index.tsv": "#name\tkey\nShown (USA)\tShown (USA)\n",
		"gameinfo.tsv": "#key\tname\tyear\n" +
			"Shown (USA)\tShown\t1994\n" +
			"Unseen (USA)\tUnseen\t1995\n",
		"Shown (USA).jpg": "image",
	}
	for name, content := range files {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, name), []byte(content), 0o600))
	}

	got, err := loadArtworkRecords(context.Background(), fs, dir, nil)
	require.NoError(t, err)
	require.Len(t, got.Artwork, 2)
	assert.Equal(t, "Shown (USA)", got.Artwork[0].Key)
	assert.NotEmpty(t, got.Artwork[0].ImagePath)
	assert.Equal(t, artworkRecord{Name: "Unseen (USA)", Key: "Unseen (USA)"}, got.Artwork[1])
	assert.Equal(t, "1995", got.GameInfo["Unseen (USA)"].Year)
	assert.Zero(t, got.RowErrors)
}

func TestLoadArtworkRecords_DegradesToExactKeysWithoutIndex(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "SNES", "Artwork")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	files := map[string]string{
		"Zelda (USA).jpg": "image",
		"Mario (USA).jpg": "image",
		"gameinfo.tsv":    "#key\tyear\nMario (USA)\t1991\n",
	}
	for name, content := range files {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, name), []byte(content), 0o600))
	}

	got, err := loadArtworkRecords(context.Background(), fs, dir, nil)
	require.NoError(t, err)
	assert.Equal(t, []artworkRecord{
		{Name: "Mario (USA)", Key: "Mario (USA)", ImagePath: filepath.Join(dir, "Mario (USA).jpg")},
		{Name: "Zelda (USA)", Key: "Zelda (USA)", ImagePath: filepath.Join(dir, "Zelda (USA).jpg")},
	}, got.Artwork)
	assert.Equal(t, "1991", got.GameInfo["Mario (USA)"].Year)
	assert.Zero(t, got.RowErrors)
}

func TestLoadArtworkRecords_MarksSlugUniquenessPerKey(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "GAMEBOY", "Artwork")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	files := map[string]string{
		// Two dumps of one game share a bare title and a key: still unique.
		// Two games whose bare titles collide across keys are not.
		"index.tsv": "#name\tkey\n" +
			"Blaster Master Boy (USA)\tBlaster Master Boy (USA)\n" +
			"Blaster Master Boy (USA) (Beta)\tBlaster Master Boy (USA)\n" +
			"Tetris (World)\tTetris (World)\n" +
			"Tetris (Japan)\tTetris (Japan)\n",
		"Blaster Master Boy (USA).jpg": "image",
		"Tetris (World).jpg":           "image",
		"Tetris (Japan).jpg":           "image",
	}
	for name, content := range files {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, name), []byte(content), 0o600))
	}

	got, err := loadArtworkRecords(context.Background(), fs, dir, nil)
	require.NoError(t, err)
	unique := make(map[string]bool, len(got.Artwork))
	for _, record := range got.Artwork {
		unique[record.Name] = record.SlugUnique
	}
	assert.True(t, unique["Blaster Master Boy (USA)"])
	assert.True(t, unique["Blaster Master Boy (USA) (Beta)"])
	assert.False(t, unique["Tetris (World)"])
	assert.False(t, unique["Tetris (Japan)"])
}

func TestLoadArtworkRecords_DoesNotDuplicateDumpsAlreadyNamedInIndex(t *testing.T) {
	t.Parallel()

	// Real PSX pack shape: a demo dump is an index name resolving to the
	// representative key, and also a gameinfo key with no image of its own.
	fs := afero.NewMemMapFs()
	dir := filepath.Join("docs", "PSX", "Artwork")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	files := map[string]string{
		"index.tsv": "#name\tcrc\tsize\tkey\n" +
			"'98 Koushien (Japan) (Demo)\t6ab3e4ce\t93\t'98 Koushien - Koukou Yakyuu Simulation (Japan)\n" +
			"'98 Koushien - Koukou Yakyuu Simulation (Japan)\tda95e43a\t113\t" +
			"'98 Koushien - Koukou Yakyuu Simulation (Japan)\n",
		"gameinfo.tsv": "#key\tname\tyear\n" +
			"'98 Koushien (Japan) (Demo)\t'98 Koushien\t1998\n" +
			"'98 Koushien - Koukou Yakyuu Simulation (Japan)\t'98 Koushien\t1998\n",
		"'98 Koushien - Koukou Yakyuu Simulation (Japan).jpg": "image",
	}
	for name, content := range files {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, name), []byte(content), 0o600))
	}

	got, err := loadArtworkRecords(context.Background(), fs, dir, nil)
	require.NoError(t, err)
	names := make([]string, 0, len(got.Artwork))
	for _, record := range got.Artwork {
		names = append(names, record.Name)
	}
	assert.Equal(t, []string{
		"'98 Koushien (Japan) (Demo)",
		"'98 Koushien - Koukou Yakyuu Simulation (Japan)",
	}, names)
	assert.Zero(t, got.RowErrors)
}
