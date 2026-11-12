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

package decb

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

func FuzzFiles(f *testing.F) {
	f.Add([]byte{})
	f.Add(buildDisk(35, 1, []entry{{"AUTO", "BAS", 0}, {"G", "BIN", 2}}, true))
	f.Add(buildDisk(35, 2, []entry{{"AUTO", "BAS", 0}}, false))
	f.Fuzz(func(t *testing.T, data []byte) {
		fs := afero.NewMemMapFs()
		p := filepath.Join("d", "x.dsk")
		if err := afero.WriteFile(fs, p, data, 0o644); err != nil {
			t.Skip()
		}
		files, err := Files(fs, p)
		if err != nil {
			return
		}
		for _, file := range files {
			_ = file.RunCommand()
		}
		_, _ = Runnable(files)
	})
}
