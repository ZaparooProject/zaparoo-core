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

package arcadenames

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/require"
)

func TestLookup(t *testing.T) {
	t.Parallel()
	entry, ok := Lookup("DKONG")
	require.True(t, ok)
	require.Equal(t, Entry{Title: "Donkey Kong (US set 1)", Year: "1981", Manufacturer: "Nintendo of America"}, entry)
	_, ok = Lookup("nes")
	require.False(t, ok, "consoles are not arcade sets")
	_, ok = Lookup("qsound")
	require.False(t, ok, "devices are not games")
	require.Regexp(t, `^0\.\d+$`, Version())
}

func TestVariantNotes(t *testing.T) {
	t.Parallel()
	for title, want := range map[string]string{
		"Donkey Kong (US set 1)":                              "(USA) (Set 1)",
		"Galaga (Namco rev. B)":                               "(Rev B)",
		"Street Fighter II: The World Warrior (World 910522)": "(World)",
		"1943: The Battle of Midway (Euro)":                   "(Europe)",
		"Puck Man (Japan, set 1)":                             "(Japan) (Set 1)",
		"Pac-Man (Midway)":                                    "",
		"Mortal Kombat II (rev L3.1)":                         "(Rev L3.1)",
		"Metal Slug - Super Vehicle-001":                      "",
		"Double Dragon (Hong Kong, revision 2)":               "(Hong Kong) (Rev 2)",
	} {
		require.Equal(t, want, VariantNotes(title), title)
	}
}

func TestIsArcadeSystem(t *testing.T) {
	t.Parallel()
	for _, id := range []string{
		systemdefs.SystemArcade, systemdefs.SystemCPS2, systemdefs.SystemNeoGeo,
		systemdefs.SystemNAOMI,
	} {
		require.True(t, IsArcadeSystem(id), id)
	}
	for _, id := range []string{systemdefs.SystemNES, systemdefs.SystemPinball, "Unknown"} {
		require.False(t, IsArcadeSystem(id), id)
	}
	require.True(t, SetArchive(".ZIP"))
	require.False(t, SetArchive(".mra"))
}
