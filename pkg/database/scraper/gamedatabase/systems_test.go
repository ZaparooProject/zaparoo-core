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

package gamedatabase

import (
	"testing"

	gdb "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/gamedatabase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
)

func TestSystemBundles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		systemID string
		want     []string
	}{
		{systemID: systemdefs.SystemSNES, want: []string{"snes", "satellaview"}},
		{systemID: systemdefs.SystemGameboy, want: []string{"gameboy"}},
		{systemID: systemdefs.SystemGenesis, want: []string{"megadrive"}},
		{systemID: systemdefs.SystemArcade, want: []string{arcadeBundle}},
		{systemID: systemdefs.SystemPSX, want: nil},
		{systemID: "NotASystem", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.systemID, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, SystemBundles(tt.systemID))
		})
	}
}

func TestSystemBundlesReturnsCopy(t *testing.T) {
	t.Parallel()

	got := SystemBundles(systemdefs.SystemSNES)
	got[0] = "changed"
	assert.Equal(t, "snes", SystemBundles(systemdefs.SystemSNES)[0])
}

func TestSystemBundlesAreEmbedded(t *testing.T) {
	t.Parallel()

	embedded := gdb.Bundles()
	for _, id := range SupportedSystems() {
		for _, bundle := range SystemBundles(id) {
			assert.Contains(t, embedded, bundle, "system %s", id)
		}
	}
}
