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

package mediascanner

import (
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/require"
)

func TestScummVMTargetsTakeScummVMTitles(t *testing.T) {
	t.Parallel()
	source := "source://" + strings.Repeat("a", 64) + "/ScummVM/"
	for _, c := range []struct {
		path, provided, want string
	}{
		// ES-DE's folder-per-game layout, as source paths and files.
		{source + "Beneath%20a%20Steel%20Sky%20%28CD%20VGA%29/sky.scummvm", "", "Beneath a Steel Sky"},
		{
			source + "Indiana%20Jones%20and%20the%20Fate%20of%20Atlantis%20%28CD%20DOS%29/atlantis.scummvm", "",
			"Indiana Jones and the Fate of Atlantis",
		},
		{"/roms/scummvm/Day of the Tentacle/tentacle.scummvm", "", "Day of the Tentacle"},
		{"/roms/scummvm/sword2.scummvm", "sword2.scummvm", "Broken Sword II: The Smoking Mirror"},
		// An ID the catalog does not know falls back to its game folder...
		{"/roms/scummvm/My Fan Game (Windows)/myfangame.scummvm", "", "My Fan Game"},
		// ...but never to the system's own folder or a root.
		{"/roms/ScummVM/myfangame.scummvm", "", "myfangame"},
		{source + "myfangame.scummvm", "", "myfangame"},
		// Readable names and names the source provided stay.
		{"/roms/scummvm/Games/Monkey Island.scummvm", "", "Monkey Island"},
		{"/roms/scummvm/sky.scummvm", "My Sky", "My Sky"},
	} {
		f := GetPathFragments(&PathFragmentParams{
			Path: c.path, SystemID: systemdefs.SystemScummVM, ProvidedName: c.provided,
		})
		require.Equal(t, c.want, f.Title, c.path)
	}
	// Only ScummVM launch files on the ScummVM system.
	f := GetPathFragments(&PathFragmentParams{Path: "/roms/dos/sky.scummvm", SystemID: systemdefs.SystemDOS})
	require.Equal(t, "sky", f.Title)
	f = GetPathFragments(&PathFragmentParams{
		Path: "/roms/scummvm/Sky Game/sky.zip", SystemID: systemdefs.SystemScummVM,
	})
	require.Equal(t, "sky", f.Title)
}

// A ScummVM ID that looks like a numbered prefix must keep its catalog
// title in full: "leading numbers" stripping is for a directory of ranked
// files, not for a name the catalog has already resolved. Without the fix,
// stripping "11-" from the ID leaves only "11-11" of the catalog's
// "11-11-11".
func TestScummVMTitleSurvivesLeadingNumberStripping(t *testing.T) {
	t.Parallel()
	f := GetPathFragments(&PathFragmentParams{
		Path: "/roms/scummvm/11-11-11.scummvm", SystemID: systemdefs.SystemScummVM,
		StripLeadingNumbers: true,
	})
	require.Equal(t, "11 11 11", f.Title, "the full catalog title, not the rank-stripped ID")
}
