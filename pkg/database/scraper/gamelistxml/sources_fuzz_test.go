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

package gamelistxml

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func FuzzScummVMMarkerSource(f *testing.F) {
	f.Add("monkey")
	f.Add("\xef\xbb\xbfmonkey\r\n")
	f.Add("monkey\nunknown")
	f.Add("unknown")
	root := f.TempDir()
	source := scraper.MediaSource{MediaPath: "scummvm://monkey/Title", Directory: filepath.Join(root, "monkey")}
	index := scraper.NewSourceIndex([]scraper.MediaSource{source})
	f.Fuzz(func(t *testing.T, data string) {
		if len(data) > 4096 {
			t.Skip()
		}
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(root, 0o750))
		path := filepath.Join(root, "game.scummvm")
		require.NoError(t, afero.WriteFile(fs, path, []byte(data), 0o600))
		got, ok := (&GamelistXMLScraper{fs: fs}).scummVMMarkerSource(index, path)
		if ok {
			assert.Equal(t, source, got, "a marker cannot manufacture an unconfigured target")
			assert.LessOrEqual(t, len(data), 1024)
		}
	})
}
