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

func FuzzPathWithin(f *testing.F) {
	f.Add("/media/fat/docs/SNES/Artwork/Game.jpg", "/media/fat/docs")
	f.Add("/media/fat/docs/../linux/secret", "/media/fat/docs")

	f.Fuzz(func(_ *testing.T, path, root string) {
		_ = pathWithin(path, root)
	})
}
