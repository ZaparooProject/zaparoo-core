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

package platforms

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNativeLaunchPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"forward_slash_windows_path", "C:/Games/SNES/mario.sfc", filepath.FromSlash("C:/Games/SNES/mario.sfc")},
		{"unix_absolute_path", "/home/user/roms/game.nes", filepath.FromSlash("/home/user/roms/game.nes")},
		{"relative_path", "snes/mario.sfc", filepath.FromSlash("snes/mario.sfc")},
		{"steam_uri_unchanged", "steam://run/12345", "steam://run/12345"},
		{"kodi_uri_unchanged", "kodi://movies/12345", "kodi://movies/12345"},
		{"http_uri_unchanged", "http://example.com/file.rom", "http://example.com/file.rom"},
		{"https_uri_unchanged", "https://example.com/file.rom", "https://example.com/file.rom"},
		{"empty_string", "", ""},
		{"no_slashes", "game.rom", "game.rom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, nativeLaunchPath(tt.input))
		})
	}
}
