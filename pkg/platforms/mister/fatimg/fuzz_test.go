//go:build linux && !android

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

package fatimg

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func FuzzOpenAndWalk(f *testing.F) {
	f.Add(stdImage(variants[0]))
	f.Add(stdImage(variants[1]))
	f.Add(mbrImage(part{stdImage(variants[0]), 0x01}))
	f.Add(stdImage(variants[2])[:1<<20])
	f.Add([]byte{})

	const maxEntries = 200
	f.Fuzz(func(t *testing.T, data []byte) {
		fs := afero.NewMemMapFs()
		p := filepath.Join("img", "disk.vhd")
		require.NoError(t, afero.WriteFile(fs, p, data, 0o644))
		img, err := Open(fs, p)
		if err != nil {
			return
		}
		defer func() { _ = img.Close() }()
		for _, vol := range img.Volumes() {
			count := 0
			_ = vol.Walk("", WalkOptions{MaxDepth: 8, MaxEntries: maxEntries}, func(_ Path, e Entry) error {
				count++
				if !e.IsDir {
					_, _ = vol.ReadFile("/"+e.Name, 1<<16)
				}
				return nil
			})
			if count > maxEntries {
				t.Fatalf("walk visited %d entries, limit %d", count, maxEntries)
			}
			_, _ = vol.ReadDir("/")
			_, _ = vol.ReadFile("/x", 1<<16)
		}
	})
}
