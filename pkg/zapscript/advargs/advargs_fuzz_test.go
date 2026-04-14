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

package advargs

import (
	"testing"

	"github.com/ZaparooProject/go-zapscript"
)

// FuzzAdvargsParse tests the advanced argument parser with arbitrary key-value
// pairs to discover edge cases in mapstructure decoding and validation.
func FuzzAdvargsParse(f *testing.F) {
	// Valid inputs
	f.Add("launcher", "steam")
	f.Add("system", "nes")
	f.Add("action", "run")
	f.Add("action", "details")
	f.Add("mode", "shuffle")
	f.Add("when", "true")
	f.Add("when", "1")
	f.Add("tags", "genre:rpg,region:usa")
	f.Add("tags", "genre:rpg")

	// Invalid values
	f.Add("launcher", "nonexistent_launcher_xyz")
	f.Add("system", "invalid_system_xyz")
	f.Add("action", "dance")
	f.Add("mode", "invalid_mode")

	// Unknown keys (typos)
	f.Add("lancher", "steam")
	f.Add("unknown_key", "value")

	// Edge cases
	f.Add("", "")
	f.Add("launcher", "")
	f.Add("", "steam")
	f.Add("tags", "")
	f.Add("tags", "::::")
	f.Add("tags", "a:b,c:d,e:f,g:h")
	f.Add("launcher", "\x00\x01\x02")
	f.Add("system", "\u65e5\u672c\u8a9e")
	f.Add("when", "false")
	f.Add("when", "0")

	ctx := NewParseContext([]string{"steam", "retroarch", "mister"})

	f.Fuzz(func(t *testing.T, key, value string) {
		raw := map[string]string{key: value}

		// Test against LaunchRandomArgs (has the most fields)
		var launchArgs zapscript.LaunchRandomArgs
		err1 := Parse(raw, &launchArgs, ctx)

		// Test against PlaylistArgs
		var playlistArgs zapscript.PlaylistArgs
		err2 := Parse(raw, &playlistArgs, nil)

		// Test against GlobalArgs (minimal)
		var globalArgs zapscript.GlobalArgs
		err3 := Parse(raw, &globalArgs, nil)

		// Determinism: same input must produce same result for each dest type
		var launchArgs2 zapscript.LaunchRandomArgs
		err1b := Parse(raw, &launchArgs2, ctx)
		if (err1 == nil) != (err1b == nil) {
			t.Errorf("non-deterministic error for LaunchRandomArgs with key=%q value=%q", key, value)
		}

		var playlistArgs2 zapscript.PlaylistArgs
		err2b := Parse(raw, &playlistArgs2, nil)
		if (err2 == nil) != (err2b == nil) {
			t.Errorf("non-deterministic error for PlaylistArgs with key=%q value=%q", key, value)
		}

		var globalArgs2 zapscript.GlobalArgs
		err3b := Parse(raw, &globalArgs2, nil)
		if (err3 == nil) != (err3b == nil) {
			t.Errorf("non-deterministic error for GlobalArgs with key=%q value=%q", key, value)
		}
	})
}
