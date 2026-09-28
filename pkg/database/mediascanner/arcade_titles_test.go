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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func arcadeFragments(t *testing.T, systemID, path, provided string) MediaPathFragments {
	t.Helper()
	return GetPathFragments(&PathFragmentParams{Path: path, SystemID: systemID, ProvidedName: provided})
}

func TestArcadeSetArchivesTakeMAMETitles(t *testing.T) {
	t.Parallel()
	source := "source://" + strings.Repeat("a", 64) + "/Arcade/"
	f := arcadeFragments(t, systemdefs.SystemArcade, source+"dkong.zip", "")
	assert.Equal(t, "Donkey Kong", f.Title)
	assert.Equal(t, "donkeykong", f.Slug)
	assert.Subset(t, f.Tags, []string{"region:us", "set:1", "year:1981"})
	assert.Equal(t, "Donkey Kong", f.DisplayTitle)
	// A clone of the same game stays distinguishable by its tags.
	j := arcadeFragments(t, systemdefs.SystemArcade, source+"dkongj.zip", "")
	assert.Equal(t, f.Slug, j.Slug)
	assert.NotEqual(t, f.Tags, j.Tags)

	// A provided name that is only the file name again is replaced too.
	f = arcadeFragments(t, systemdefs.SystemArcade, "/roms/arcade/1943.zip", "1943.zip")
	assert.Equal(t, "1943: The Battle of Midway", f.Title)

	// Arcade boards that fall back to Arcade and other MAME hardware.
	f = arcadeFragments(t, systemdefs.SystemCPS2, "/roms/cps2/19xx.zip", "")
	assert.NotEqual(t, "19xx", f.Title)
	f = arcadeFragments(t, systemdefs.SystemNeoGeo, "/roms/neogeo/mslug.zip", "")
	assert.Equal(t, "Metal Slug - Super Vehicle-001", f.Title)
}

func TestArcadeTitlesLeaveOtherFilesAlone(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		system, path, provided, want string
	}{
		{systemdefs.SystemArcade, "/roms/arcade/dkong.zip", "My Donkey Kong", "My Donkey Kong"},
		{systemdefs.SystemNES, "/roms/nes/dkong.zip", "", "dkong"},
		{systemdefs.SystemArcade, "/roms/arcade/Donkey Kong (US set 1).mra", "", "Donkey Kong"},
		{systemdefs.SystemArcade, "/roms/arcade/notaset.zip", "", "notaset"},
		{systemdefs.SystemPinball, "/tables/dkong.zip", "", "dkong"},
	} {
		f := arcadeFragments(t, c.system, c.path, c.provided)
		require.Equal(t, c.want, f.Title, c.path)
	}
}
