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

package trackmeta

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testMaxBytes = 2 << 20

func copyFixture(t *testing.T, name, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // test fixture
	require.NoError(t, err)
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, data, 0o600)) //nolint:gosec // test path from t.TempDir()
	return path
}

func TestRead_ID3v23(t *testing.T) {
	t.Parallel()

	meta, err := Read(filepath.Join("testdata", "tagged.mp3"), testMaxBytes)
	require.NoError(t, err)
	assert.Equal(t, &Meta{
		Title: "Intro", Artist: "Test Artist", Album: "Test Album", AlbumArtist: "Album Artist",
		Genre: "Chiptune", Year: 1999, TrackNumber: 3, TrackTotal: 12, DiscNumber: 1, DiscTotal: 2,
		HasArt: true,
	}, meta)
}

func TestRead_FLAC(t *testing.T) {
	t.Parallel()

	meta, err := Read(filepath.Join("testdata", "tagged.flac"), testMaxBytes)
	require.NoError(t, err)
	assert.Equal(t, "Flac Song", meta.Title)
	assert.Equal(t, "Flac Artist", meta.Artist)
	assert.Equal(t, "Flac Album", meta.Album)
	assert.Equal(t, "Various", meta.AlbumArtist)
	assert.Equal(t, "Ambient", meta.Genre)
	assert.Equal(t, 2004, meta.Year)
	assert.Equal(t, 5, meta.TrackNumber)
	assert.Equal(t, 9, meta.TrackTotal)
	assert.Equal(t, 2, meta.DiscNumber)
	assert.Equal(t, 3, meta.DiscTotal)
	assert.True(t, meta.HasArt)
}

func TestRead_Ogg(t *testing.T) {
	t.Parallel()

	meta, err := Read(filepath.Join("testdata", "tagged.ogg"), testMaxBytes)
	require.NoError(t, err)
	assert.Equal(t, "Ogg Song", meta.Title)
	assert.Equal(t, "Ogg Artist", meta.Artist)
	assert.Equal(t, "Ogg Album", meta.Album)
	assert.False(t, meta.HasArt)
}

func TestRead_OverCapSkipsTags(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"tagged.mp3", "tagged.flac", "tagged.ogg"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Read(filepath.Join("testdata", name), 64)
			require.ErrorIs(t, err, ErrTagsTooLarge)
		})
	}
}

func TestRead_OverCapStillReportsFolderArt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := copyFixture(t, "tagged.ogg", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Folder.JPG"), []byte{0xFF, 0xD8, 0xFF, 0xE0}, 0o600))

	meta, err := Read(path, 64)
	require.NoError(t, err)
	assert.Equal(t, &Meta{HasArt: true}, meta)
}

func TestRead_NoTags(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "plain.wav")
	require.NoError(t, os.WriteFile(path, make([]byte, 300), 0o600))

	_, err := Read(path, testMaxBytes)
	require.ErrorIs(t, err, ErrNoTags)
}

func TestRead_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := Read(filepath.Join(t.TempDir(), "gone.mp3"), testMaxBytes)
	require.Error(t, err)
}

func TestRead_FLACPictureLengthBeyondBlockIsRefused(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "tagged.flac"))
	require.NoError(t, err)
	// The picture data length is the last length field before the image.
	idx := strings.LastIndex(string(data), "\x89PNG")
	require.Positive(t, idx)
	binary.BigEndian.PutUint32(data[idx-4:idx], 0x7fffffff)
	path := filepath.Join(t.TempDir(), "bad.flac")
	require.NoError(t, os.WriteFile(path, data, 0o600)) //nolint:gosec // test path from t.TempDir()

	_, err = Read(path, testMaxBytes)
	require.ErrorIs(t, err, ErrMalformedTags)
}

func TestReadPicture_Embedded(t *testing.T) {
	t.Parallel()

	pic, err := ReadPicture(filepath.Join("testdata", "tagged.mp3"), testMaxBytes)
	require.NoError(t, err)
	assert.Equal(t, SourceEmbedded, pic.Source)
	assert.Equal(t, "image/png", pic.MIMEType)
	assert.True(t, strings.HasPrefix(string(pic.Data), "\x89PNG"))
}

func TestReadPicture_FolderFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := copyFixture(t, "tagged.ogg", dir)
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "front.png"), []byte("\x89PNG\r\n\x1a\nxx"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "COVER.jpeg"), jpeg, 0o600))

	pic, err := ReadPicture(path, testMaxBytes)
	require.NoError(t, err)
	assert.Equal(t, SourceFolder, pic.Source)
	assert.Equal(t, "image/jpeg", pic.MIMEType)
	assert.Equal(t, filepath.Join(dir, "COVER.jpeg"), pic.Path)
	assert.Equal(t, jpeg, pic.Data)
}

func TestReadPicture_NoArt(t *testing.T) {
	t.Parallel()

	path := copyFixture(t, "tagged.ogg", t.TempDir())
	_, err := ReadPicture(path, testMaxBytes)
	require.ErrorIs(t, err, ErrNoArt)
}

func TestReadPicture_FolderArtOverCap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := copyFixture(t, "tagged.ogg", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cover.jpg"), make([]byte, 4096), 0o600))

	_, err := ReadPicture(path, 1024)
	require.ErrorIs(t, err, ErrTagsTooLarge)
}

func TestSanitize(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "ab", sanitize("a\x00\x07b"))
	assert.Equal(t, "ok", sanitize("  ok\n "))
	assert.Equal(t, "bad", sanitize("b\xffad"))

	long := strings.Repeat("é", 200) // 400 bytes
	out := sanitize(long)
	assert.LessOrEqual(t, len(out), maxFieldBytes)
	assert.True(t, utf8.ValidString(out))
}

// UTF-8 text a file labels as Latin-1 reads back garbled; it is repaired.
// Genuine Latin-1 accents, and text that is not in Latin-1 range at all,
// are left as they are.
func TestSanitize_RepairsDoubleEncodedUTF8(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, in, want string }{
		{"accents", "MÃªlÃ©e Island", "Mêlée Island"},
		{"umlaut", "BjÃ¶rk", "Björk"},
		{"plain ASCII", "Green Hill Zone", "Green Hill Zone"},
		{"real Latin-1 accent", "Café", "Café"},
		{"real accents together", "Mêlée Island", "Mêlée Island"},
		{"already UTF-8 beyond Latin-1", "\u65e5\u672c Ã©", "\u65e5\u672c Ã©"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, sanitize(tt.in))
		})
	}
}
