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

package misterarcade

import (
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
)

// maxFuzzInput bounds each fuzzed column. Real catalog cells are short; the
// bound keeps the corpus meaningful rather than exercising the allocator.
const maxFuzzInput = 512

// FuzzBuildWrite treats the catalog as the untrusted download it is. Whatever a
// column says, the mapping must produce tags that can be stored and matched:
// no panic, no empty tag type or value, and no value carrying the separator the
// tag vocabulary reserves for its own structure.
func FuzzBuildWrite(f *testing.F) {
	f.Add("1941", "World", "900227", "Capcom CPS-1", "2 (simultaneous)", "8-way", "2", "15kHz")
	f.Add("pacman", "bootleg", "Set 1", "Namco Pac-Man hardware", "1", "4-way", "1", "31kHz")
	f.Add("", "n-a", "n-a", "", "n-a", "n-a", "", "n-a")
	f.Add("x", "USA - Asia", "FD1094 317-0154", "Sega System 16", "2-4 (alternating)", "Stick - Pedal", "20", "")
	f.Fuzz(func(
		t *testing.T, setName, region, version, platform, players, moveInputs, numButtons, resolution string,
	) {
		for _, column := range []string{
			setName, region, version, platform, players, moveInputs, numButtons, resolution,
		} {
			if len(column) > maxFuzzInput {
				t.Skip()
			}
		}
		entry := Entry{
			SetName: setName, Region: region, Version: version, Platform: platform,
			Players: players, MoveInputs: moveInputs, NumButtons: numButtons,
			Resolution: resolution,
			// Fixed columns keep the fuzzed ones in a realistic row.
			Year: "1990", Manufacturer: "Capcom", Category: "Shooter - Flying Vertical",
			Rotation: "vertical (ccw)", Flip: "yes", Alternative: "yes", Homebrew: "no",
			Bootleg: "no", Series: "19XX",
		}
		write := buildWrite(&entry, "")

		for _, list := range [][]database.TagInfo{write.TitleTags, write.MediaTags} {
			for _, tag := range list {
				assert.NotEmpty(t, tag.Type, "a tag with no type cannot be stored")
				assert.NotEmpty(t, tag.Tag, "a tag with no value cannot be matched")
				assert.Equal(t, strings.ToLower(tag.Tag), tag.Tag, "stored values are lower-cased")
				assert.NotContains(t, tag.Tag, " ", "a stored value never carries whitespace")
			}
		}
		// Player counts stay inside the vocabulary's range, whatever the column
		// claimed, so a corrupt cell cannot invent a 900-player cabinet.
		for _, value := range tagValues(write.TitleTags, tags.TagTypePlayers) {
			if value == string(tags.TagPlayersSimultaneous) || value == string(tags.TagPlayersAlt) {
				continue
			}
			assert.LessOrEqual(t, len(value), 2)
		}
		for _, prop := range write.MediaProps {
			assert.NotEmpty(t, prop.TypeTag)
			assert.NotEmpty(t, prop.Text)
		}
	})
}
