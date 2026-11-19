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

package libretrothumbs

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemsForDatabase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want []string
	}{
		{"Nintendo - Super Nintendo Entertainment System", []string{systemdefs.SystemSNES}},
		{"Sony - PlayStation", []string{systemdefs.SystemPSX}},
		{"Sega - Mega Drive - Genesis", []string{systemdefs.SystemGenesis}},
		{"sega - mega drive - genesis", []string{systemdefs.SystemGenesis}},
		{" Sony - PlayStation ", []string{systemdefs.SystemPSX}},
		{"MAME", []string{
			systemdefs.SystemArcade, systemdefs.SystemCPS1, systemdefs.SystemCPS2, systemdefs.SystemCPS3,
			systemdefs.SystemCave68000, systemdefs.SystemPGM,
		}},
		{"Microsoft - MSX2", []string{systemdefs.SystemMSX2, systemdefs.SystemMSX}},
		{"Commodore - Amiga", []string{systemdefs.SystemAmiga, systemdefs.SystemAmiga500, systemdefs.SystemAmiga1200}},
		{"Images", nil},
		{"", nil},
	}
	for _, tt := range tests {
		got := SystemsForDatabase(tt.name)
		if tt.want == nil {
			assert.Empty(t, got, tt.name)
			continue
		}
		assert.Equal(t, tt.want, got, tt.name)
	}

	fbneo := SystemsForDatabase("FBNeo - Arcade Games")
	require.NotEmpty(t, fbneo)
	assert.Equal(t, systemdefs.SystemArcade, fbneo[0])
}

func TestSystemsForDatabaseReturnsCopy(t *testing.T) {
	t.Parallel()
	got := SystemsForDatabase("MAME")
	got[0] = "changed"
	assert.Equal(t, systemdefs.SystemArcade, SystemsForDatabase("MAME")[0])
}

// Every libretro database shared by several systems must say which system
// owns it, or a playlist named after it would land on whichever sorts first.
func TestSharedDatabasesHaveOwners(t *testing.T) {
	t.Parallel()
	shared := make(map[string]int)
	for _, names := range playlists {
		for _, name := range names {
			shared[name]++
		}
	}
	for name, count := range shared {
		if count > 1 {
			assert.Contains(t, databaseOwners, name, "%s is shared by %d systems", name, count)
		}
	}
	for name, owners := range databaseOwners {
		for _, id := range owners {
			assert.Contains(t, playlists[id], name, "%s does not list %s", id, name)
		}
	}
}
