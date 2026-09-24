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

package sourcepath

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdentityRoundTripAndBoundaries(t *testing.T) {
	t.Parallel()
	id := ID("opaque-provider-reference")
	parts := []string{"NES", "Pokémon + 50% (USA).NES"}
	identity, err := Format(id, parts)
	require.NoError(t, err)
	gotID, gotParts, err := Parse(identity)
	require.NoError(t, err)
	require.Equal(t, id, gotID)
	require.Equal(t, parts, gotParts)
	require.NotEqual(t, id, ID("different-source"))
	for _, invalid := range []string{"", ".", "..", "a/b", `a\b`, "bad\x00name", strings.Repeat("x", 1025)} {
		_, err := Format(id, []string{invalid})
		require.ErrorIs(t, err, ErrInvalid)
	}
	invalidPaths := []string{
		identity + "?q=1", identity + "#", "source://user@" + id + "/game.nes",
		"source://" + id + "/%2e%2e/game.nes", "source://" + id + "/a%2Fb.nes",
		"file:///storage/game.nes", strings.Replace(identity, "source://", "SOURCE://", 1),
	}
	for _, invalid := range invalidPaths {
		_, _, err := Parse(invalid)
		require.ErrorIs(t, err, ErrInvalid)
	}
}

func FuzzSourcePath(f *testing.F) {
	valid, err := Format(ID("fixture"), []string{"nes", "Test + % (USA).nes"})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add("source://bad/%2e%2e")
	f.Add("")
	f.Fuzz(func(t *testing.T, value string) {
		id, parts, err := Parse(value)
		if err != nil {
			return
		}
		got, err := Format(id, parts)
		require.NoError(t, err)
		require.Equal(t, value, got)
	})
}
