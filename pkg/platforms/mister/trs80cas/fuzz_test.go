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

package trs80cas

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

func FuzzProgramName(f *testing.F) {
	f.Add([]byte{})
	f.Add(cat(leader(255), []byte{0xA5, 0x55}, []byte("GALAXY"), []byte{0x3C}))
	f.Add(cat(leader(8), []byte{0xA5, 0xD3, 0xD3, 0xD3, 'A'}))
	f.Fuzz(func(t *testing.T, data []byte) {
		fs := afero.NewMemMapFs()
		p := filepath.Join("t", "x.cas")
		if err := afero.WriteFile(fs, p, data, 0o644); err != nil {
			t.Skip()
		}
		_, _ = ProgramName(fs, p)
	})
}
