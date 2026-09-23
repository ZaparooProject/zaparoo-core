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

package tags

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLookupArcadeBoardResolvesSourceSpellings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw  string
		want TagValue
	}{
		// The MiSTer arcade catalog's own names.
		{raw: "Capcom CPS-1", want: TagArcadeBoardCapcomCPS},
		{raw: "Capcom CPS-1.5", want: TagArcadeBoardCapcomCPSDash},
		{raw: "Capcom CPS-2", want: TagArcadeBoardCapcomCPS2},
		{raw: "Namco Pac-Man hardware", want: TagArcadeBoardNamcoPacMan},
		{raw: "Taito Bubble Booble based", want: TagArcadeBoardTaitoBubbleBobble},
		{raw: "DATA EAST MEC-M1", want: TagArcadeBoardDataEastMECM1},
		{raw: "Toaplan 2", want: TagArcadeBoardToaplanVersion2},
		{raw: "Sega ST-V", want: TagArcadeBoardSegaSTV},
		// Short and alternative names other sources use.
		{raw: "CPS1", want: TagArcadeBoardCapcomCPS},
		{raw: "cps-2", want: TagArcadeBoardCapcomCPS2},
		{raw: "CP System III", want: TagArcadeBoardCapcomCPS3},
		{raw: "Neo-Geo", want: TagArcadeBoardSNKMVS},
		{raw: "MVS", want: TagArcadeBoardSNKMVS},
		{raw: "Naomi", want: TagArcadeBoardSegaNaomi},
		{raw: "System 16", want: TagArcadeBoardSegaSystem16},
		{raw: "Taito F3", want: TagArcadeBoardTaitoF3System},
		// A board family is found with either catalog suffix or none.
		{raw: "Namco Galaga based", want: TagArcadeBoardNamcoGalaga},
		{raw: "Namco Galaga hardware", want: TagArcadeBoardNamcoGalaga},
		{raw: "Namco Galaga", want: TagArcadeBoardNamcoGalaga},
		{raw: "Atari Missile Command hardware", want: TagArcadeBoardAtariMissileCommand},
		// A canonical value is found by its own spelling.
		{raw: "capcom:cps", want: TagArcadeBoardCapcomCPS},
		{raw: "Irem M72", want: TagArcadeBoardIremM72},
	} {
		got, ok := LookupArcadeBoard(tc.raw)
		if assert.True(t, ok, "%q should resolve", tc.raw) {
			assert.Equal(t, tc.want, got, "%q", tc.raw)
		}
	}
}

func TestLookupArcadeBoardRefusesWhatNamesNoOneBoard(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"", "Konami Unique", "Taito Licensed", "Atari 6502", "Capcom CPS-0",
		// A vendor stripped of "hardware" is not a board.
		"Tecmo", "Tehkan", "Stern",
		// "System 1" is a Sega, Namco and Atari board alike.
		"System 1",
	} {
		_, ok := LookupArcadeBoard(raw)
		assert.False(t, ok, "%q must not resolve", raw)
	}
}

func TestEveryArcadeBoardIsFoundByItsOwnSpelling(t *testing.T) {
	t.Parallel()
	for _, value := range CanonicalTagDefinitions[TagTypeArcadeBoard] {
		got, ok := LookupArcadeBoard(string(value))
		if assert.True(t, ok, "%s", value) {
			assert.Equal(t, value, got, "an explicit alias shadows the canonical value %s", value)
		}
	}
}

// Two spellings that fold to one key must agree, or the lookup would depend
// on map iteration order.
func TestArcadeBoardSpellingsDoNotConflict(t *testing.T) {
	t.Parallel()
	seen := make(map[string]TagValue)
	for spelling, value := range arcadeBoardSpellings {
		assert.True(t, IsCanonicalValue(TagTypeArcadeBoard, value), "%q maps to %s", spelling, value)
		for key := range withBoardSuffixes(map[string]TagValue{spelling: value}) {
			if prior, dup := seen[key]; dup {
				assert.Equal(t, prior, value, "key %q from %q", key, spelling)
			}
			seen[key] = value
		}
	}
}
