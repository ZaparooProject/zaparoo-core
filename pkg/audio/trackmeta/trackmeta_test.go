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
	"slices"
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

func TestFolderArt_SkipsUnreadableCandidates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := copyFixture(t, "tagged.ogg", dir)
	// The best-ranked names are a dangling symlink and a directory-like
	// non-regular file; the lower-ranked cover.png is the only usable image.
	require.NoError(t, os.Symlink(filepath.Join(dir, "missing.jpg"), filepath.Join(dir, "cover.jpg")))
	require.NoError(t, os.Symlink(dir, filepath.Join(dir, "cover.jpeg")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "folder.png"), []byte("\x89PNG\r\n\x1a\nxx"), 0o600))

	got, ok := FolderArtPath(path)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "folder.png"), got)

	meta, err := Read(path, testMaxBytes)
	require.NoError(t, err)
	assert.True(t, meta.HasArt)
}

func TestFolderArt_NoUsableCandidate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := copyFixture(t, "tagged.ogg", dir)
	require.NoError(t, os.Symlink(filepath.Join(dir, "missing.jpg"), filepath.Join(dir, "cover.jpg")))

	_, ok := FolderArtPath(path)
	assert.False(t, ok)
}

func TestReadPicture_TagLimitSurvivesMissingFolderArt(t *testing.T) {
	t.Parallel()

	path := copyFixture(t, "tagged.mp3", t.TempDir())
	_, err := ReadPicture(path, 64)
	require.ErrorIs(t, err, ErrTagsTooLarge, "embedded art may exist but was not read")
	require.NotErrorIs(t, err, ErrNoArt)
}

func TestReadPicture_TagLimitAndFolderArtFailureAreBothReported(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := copyFixture(t, "tagged.mp3", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cover.jpg"), make([]byte, 4096), 0o600))

	_, err := ReadPicture(path, 128)
	require.ErrorIs(t, err, ErrTagsTooLarge)
	assert.Contains(t, err.Error(), "folder art")
	assert.Contains(t, err.Error(), "after tags")
}

func TestReadPicture_SmallTagsLargeArtIsNotATagFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := copyFixture(t, "tagged.ogg", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cover.jpg"), make([]byte, 4096), 0o600))

	_, err := ReadPicture(path, 1024)
	require.ErrorIs(t, err, ErrTagsTooLarge)
	assert.NotContains(t, err.Error(), "after tags")
}

// id3v2 builds an ID3v2 tag of the given version whose body is body.
func id3v2(version, flags byte, body []byte) []byte {
	n := len(body)
	out := make([]byte, 0, 10+n)
	out = append(out, 'I', 'D', '3', version, 0, flags,
		byte(n>>21&0x7f), byte(n>>14&0x7f), byte(n>>7&0x7f), byte(n&0x7f))
	return append(out, body...)
}

func TestRead_ID3v2FrameLengthPastTagIsRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tag  []byte
	}{
		{"v2.2", id3v2(2, 0, []byte{'T', 'T', '2', 0x7f, 0xff, 0xff, 0, 'a', 'b'})},
		{"v2.3", id3v2(3, 0, []byte{'T', 'I', 'T', '2', 0x7f, 0xff, 0xff, 0xff, 0, 0, 0, 'a'})},
		{"v2.4", id3v2(4, 0, []byte{'T', 'I', 'T', '2', 0x7f, 0x7f, 0x7f, 0x7f, 0, 0, 0, 'a'})},
		{"v2.3 one byte over", id3v2(3, 0, []byte{'T', 'I', 'T', '2', 0, 0, 0, 3, 0, 0, 'a', 'b'})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "bad.mp3")
			require.NoError(t, os.WriteFile(path, append(tt.tag, make([]byte, 64)...), 0o600))
			_, err := Read(path, testMaxBytes)
			require.ErrorIs(t, err, ErrMalformedTags)
		})
	}
}

func TestValidID3v2Frames_AcceptsWellFormedTags(t *testing.T) {
	t.Parallel()

	frame := []byte{'T', 'I', 'T', '2', 0, 0, 0, 2, 0, 0, 0, 'a'}
	padded := append(slices.Clone(frame), 0, 0, 0, 0)
	assert.True(t, validID3v2Frames(id3v2(3, 0, padded)), "padding after frames")
	assert.True(t, validID3v2Frames(id3v2(3, 0, frame[:0])), "no frames")
	withExt := append([]byte{0, 0, 0, 2, 0, 0, 0, 0}, frame...)
	assert.True(t, validID3v2Frames(id3v2(3, 0x40, withExt)), "extended header")
	unknown := id3v2(9, 0, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11})
	assert.True(t, validID3v2Frames(unknown), "an unknown version is the parser's to refuse")
}
