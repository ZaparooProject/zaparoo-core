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

package esde

import (
	"path/filepath"
	"testing"
)

func FuzzResolveGamePath(f *testing.F) {
	f.Add("./game.rom", "/roms", "nes")
	f.Add("../../outside.rom", "/roms", "nes")
	f.Add("/outside/game.rom", "/roms", "nes")

	f.Fuzz(func(t *testing.T, gamePath, romsBasePath, systemFolder string) {
		resolved := ResolveGamePath(gamePath, romsBasePath, systemFolder)
		if resolved == "" {
			return
		}
		root := filepath.Clean(filepath.Join(romsBasePath, systemFolder))
		if !pathWithin(root, resolved) {
			t.Fatalf("resolved path escaped root: root=%q path=%q", root, resolved)
		}
	})
}
