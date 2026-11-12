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
//

package mmb

import (
	"testing"

	"github.com/spf13/afero"
)

func FuzzReadCatalog(f *testing.F) {
	f.Add([]byte{})
	f.Add(buildCatalog(0, map[int]entry{0: {"Elite", 0x00}, 1: {"A/B", 0x0F}}))
	f.Add(buildCatalog(0xAF, map[int]entry{510: {"\xff\x01x", 0x0F}}))

	f.Fuzz(func(t *testing.T, data []byte) {
		fs := afero.NewMemMapFs()
		if err := afero.WriteFile(fs, "fuzz.mmb", data, 0o600); err != nil {
			t.Fatal(err)
		}
		disks, err := ReadCatalog(fs, "fuzz.mmb")
		if err != nil {
			return
		}
		for _, d := range disks {
			if d.Title == "" || d.Number < 0 {
				t.Fatalf("bad disk %+v", d)
			}
			for i := range len(d.Title) {
				if c := d.Title[i]; c < 0x20 || c > 0x7E {
					t.Fatalf("non-printable title %q", d.Title)
				}
			}
		}
	})
}
