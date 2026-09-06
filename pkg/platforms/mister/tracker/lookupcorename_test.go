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

package tracker

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A CORENAME is not a system ID. generateNameMap adds alternate cores such as
// RA_GBC precisely so the tracker can place them, so validating the core name
// instead of the system it maps to discarded every one of them and left a
// running RetroAchievements core unidentified.
func TestLookupCoreNameAlternateCores(t *testing.T) {
	t.Parallel()

	tr := &Tracker{NameMap: []NameMapping{
		{CoreName: "GBC", System: systemdefs.SystemGameboyColor, Name: systemdefs.SystemGameboyColor},
		{CoreName: "RA_GBC", System: systemdefs.SystemGameboyColor, Name: systemdefs.SystemGameboyColor},
		{CoreName: "Gameboy_LLAPI", System: systemdefs.SystemGameboy, Name: systemdefs.SystemGameboy},
		{CoreName: "SonicBoom", System: ArcadeSystem, Name: ArcadeSystem, ArcadeName: "Sonic Boom"},
		{CoreName: "Bogus", System: "NotASystem", Name: "NotASystem"},
	}}

	for _, tc := range []struct {
		name, core, wantSystem string
	}{
		{name: "stock core", core: "GBC", wantSystem: systemdefs.SystemGameboyColor},
		{name: "retroachievements core", core: "RA_GBC", wantSystem: systemdefs.SystemGameboyColor},
		{name: "llapi core", core: "Gameboy_LLAPI", wantSystem: systemdefs.SystemGameboy},
		{name: "case insensitive", core: "ra_gbc", wantSystem: systemdefs.SystemGameboyColor},
		{name: "arcade set name", core: "SonicBoom", wantSystem: ArcadeSystem},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mapping := tr.LookupCoreName(tc.core)
			require.NotNil(t, mapping, "core %q must resolve", tc.core)
			assert.Equal(t, tc.wantSystem, mapping.System)
		})
	}

	for _, tc := range []struct{ name, core string }{
		{name: "empty", core: ""},
		{name: "unmapped core", core: "Utility"},
		{name: "mapping to an unknown system", core: "Bogus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Nil(t, tr.LookupCoreName(tc.core))
		})
	}
}
