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

package helpers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPathInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected PathInfo
	}{
		{
			name: "unix_path_with_extension",
			path: "/games/snes/mario.sfc",
			expected: PathInfo{
				Path:      "/games/snes/mario.sfc",
				Base:      "/games/snes",
				Filename:  "mario.sfc",
				Extension: ".sfc",
				Name:      "mario",
			},
		},
		{
			name: "windows_style_path",
			path: `C:/Games/Mario Bros.smc`,
			expected: PathInfo{
				Path:      `C:/Games/Mario Bros.smc`,
				Base:      `C:/Games`,
				Filename:  "Mario Bros.smc",
				Extension: ".smc",
				Name:      "Mario Bros",
			},
		},
		{
			name: "no_extension",
			path: "/roms/mario",
			expected: PathInfo{
				Path:      "/roms/mario",
				Base:      "/roms",
				Filename:  "mario",
				Extension: "",
				Name:      "mario",
			},
		},
		{
			name: "current_directory_file",
			path: "game.rom",
			expected: PathInfo{
				Path:      "game.rom",
				Base:      ".",
				Filename:  "game.rom",
				Extension: ".rom",
				Name:      "game",
			},
		},
		{
			name: "empty_path",
			path: "",
			expected: PathInfo{
				Path:      "",
				Base:      ".",
				Filename:  ".",
				Extension: "",
				Name:      ".",
			},
		},
		{
			name: "path_with_spaces",
			path: "/games/arcade/Street Fighter II.zip",
			expected: PathInfo{
				Path:      "/games/arcade/Street Fighter II.zip",
				Base:      "/games/arcade",
				Filename:  "Street Fighter II.zip",
				Extension: ".zip",
				Name:      "Street Fighter II",
			},
		},
		{
			name: "nested_path",
			path: "/home/user/roms/nes/classics/super_mario_bros.nes",
			expected: PathInfo{
				Path:      "/home/user/roms/nes/classics/super_mario_bros.nes",
				Base:      "/home/user/roms/nes/classics",
				Filename:  "super_mario_bros.nes",
				Extension: ".nes",
				Name:      "super_mario_bros",
			},
		},
		{
			name: "multiple_dots_in_name",
			path: "/games/v1.2.final.rom",
			expected: PathInfo{
				Path:      "/games/v1.2.final.rom",
				Base:      "/games",
				Filename:  "v1.2.final.rom",
				Extension: ".rom",
				Name:      "v1.2.final",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := GetPathInfo(tt.path)
			assert.Equal(t, tt.expected, result, "GetPathInfo result mismatch")
		})
	}
}
