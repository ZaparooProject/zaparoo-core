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
	"strings"
	"testing"

	"github.com/spf13/afero"
)

func FuzzParseTSV(f *testing.F) {
	f.Add([]byte("#name\tkey\nGame\tGame\n"))
	f.Add([]byte("#key\tsynopsis\nGame\tA description\n"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if int64(len(data)) > maxMetadataBytes {
			t.Skip()
		}
		fs := afero.NewMemMapFs()
		path := filepath.Join("docs", "index.tsv")
		if err := fs.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := afero.WriteFile(fs, path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _ = readTSV(context.Background(), fs, path)
	})
}

func TestPathWithin(t *testing.T) {
	t.Parallel()

	root := filepath.Join("media", "fat", "docs")
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "descendant", path: filepath.Join(root, "SNES", "Artwork", "Game.jpg"), want: true},
		{name: "parent", path: filepath.Dir(root), want: false},
		{name: "sibling", path: filepath.Join(filepath.Dir(root), "docs-old", "Game.jpg"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPathWithin(t, tt.path, root, tt.want)
		})
	}
}

func assertPathWithin(t *testing.T, path, root string, want bool) {
	t.Helper()
	if got := pathWithin(path, root); got != want {
		t.Errorf("pathWithin(%q, %q) = %t, want %t", path, root, got, want)
	}
}

func FuzzPathWithin(f *testing.F) {
	f.Add("/media/fat/docs/SNES/Artwork/Game.jpg", "/media/fat/docs")
	f.Add("/media/fat/docs/../linux/secret", "/media/fat/docs")

	f.Fuzz(func(t *testing.T, path, root string) {
		if !pathWithin(path, root) {
			return
		}
		rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Fatalf("pathWithin accepted path %q outside root %q", path, root)
		}
	})
}

func FuzzReadMRASetName(f *testing.F) {
	f.Add([]byte("<misterromdescription><setname>sfa3</setname></misterromdescription>"))
	f.Add([]byte(`<?xml version="1.0" encoding="ISO-8859-1"?><a><setname>x</setname></a>`))
	f.Add([]byte("<setname>"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if int64(len(data)) > maxMRABytes {
			t.Skip()
		}
		fs := afero.NewMemMapFs()
		path := filepath.Join("games", "_Arcade", "Game.mra")
		if err := fs.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := afero.WriteFile(fs, path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		setName, ok := readMRASetName(fs, path)
		if ok && strings.TrimSpace(setName) == "" {
			t.Fatalf("readMRASetName reported ok with a blank setname for %q", data)
		}
		if !ok && setName != "" {
			t.Fatalf("readMRASetName returned %q without ok", setName)
		}
	})
}
