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
	"os"
	"path/filepath"
	"testing"
	"unicode"
	"unicode/utf8"
)

// FuzzRead feeds arbitrary bytes through Read as an mp3, a flac and an ogg
// file. It must never panic, and whatever it returns must be safe to show.
func FuzzRead(f *testing.F) {
	for _, name := range []string{"tagged.mp3", "tagged.flac", "tagged.ogg"} {
		data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // test fixture
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte("ID3\x03\x00\x00\x7f\x7f\x7f\x7f"))
	f.Add([]byte("fLaC\x84\xff\xff\xff"))
	f.Add([]byte("OggS"))

	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		path := filepath.Join(dir, "track.mp3")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		meta, err := Read(path, 1<<16)
		if err != nil {
			return
		}
		for _, field := range []string{meta.Title, meta.Artist, meta.Album, meta.AlbumArtist, meta.Genre} {
			if len(field) > maxFieldBytes {
				t.Fatalf("field longer than cap: %d bytes", len(field))
			}
			if !utf8.ValidString(field) {
				t.Fatalf("invalid UTF-8: %q", field)
			}
			for _, r := range field {
				if unicode.IsControl(r) {
					t.Fatalf("control character in %q", field)
				}
			}
		}
		if meta.Year < 0 || meta.TrackNumber < 0 || meta.TrackTotal < 0 || meta.DiscNumber < 0 || meta.DiscTotal < 0 {
			t.Fatalf("negative number in %+v", meta)
		}
	})
}
