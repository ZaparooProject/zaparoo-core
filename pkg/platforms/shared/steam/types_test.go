// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package steam

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultLinuxOptions(t *testing.T) {
	t.Parallel()

	opts := DefaultLinuxOptions()

	assert.Equal(t, "/usr/games/steam", opts.FallbackPath)
	assert.True(t, opts.UseXdgOpen, "desktop Linux should use xdg-open")
	assert.True(t, opts.CheckFlatpak, "desktop Linux should check Flatpak")
	assert.Empty(t, opts.ExtraPaths)
}

func TestDefaultSteamOSOptions(t *testing.T) {
	t.Parallel()

	opts := DefaultSteamOSOptions()

	assert.Equal(t, "/home/deck/.steam/steam", opts.FallbackPath)
	assert.False(t, opts.UseXdgOpen, "SteamOS should use direct steam command")
	assert.False(t, opts.CheckFlatpak, "SteamOS uses native Steam")
	assert.Contains(t, opts.ExtraPaths, "/home/deck/.local/share/Steam")
}

func TestDefaultBazziteOptions(t *testing.T) {
	t.Parallel()

	opts := DefaultBazziteOptions()

	assert.Equal(t, "/usr/games/steam", opts.FallbackPath)
	assert.True(t, opts.UseXdgOpen, "Bazzite should use xdg-open")
	assert.True(t, opts.CheckFlatpak, "Bazzite should check Flatpak")
}

func TestDefaultChimeraOSOptions(t *testing.T) {
	t.Parallel()

	opts := DefaultChimeraOSOptions()

	assert.Equal(t, "/home/gamer/.steam/steam", opts.FallbackPath)
	assert.False(t, opts.UseXdgOpen, "ChimeraOS should use direct steam command")
	assert.False(t, opts.CheckFlatpak, "ChimeraOS uses native Steam")
}

func TestDefaultWindowsOptions(t *testing.T) {
	t.Parallel()

	opts := DefaultWindowsOptions()

	assert.Equal(t, `C:\Program Files (x86)\Steam`, opts.FallbackPath)
	// Windows ignores these fields, but they should be at default values
	assert.False(t, opts.UseXdgOpen)
	assert.False(t, opts.CheckFlatpak)
}
