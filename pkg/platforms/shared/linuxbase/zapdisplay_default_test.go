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

//go:build linux

package linuxbase

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
)

func TestZapdisplayDefaultEnabledPerPlatform(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		platform string
		want     bool
	}{
		// ReplayOS ships the display as a first-party accessory.
		{platform: ids.ReplayOS, want: true},
		// Everything else sharing the Linux reader set stays opt-in, because
		// detection opens a serial port and writes to it.
		{platform: ids.Linux, want: false},
		{platform: ids.SteamOS, want: false},
		{platform: ids.Bazzite, want: false},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			t.Parallel()
			pl := mocks.NewMockPlatform()
			pl.On("ID").Return(tc.platform)
			assert.Equal(t, tc.want, zapdisplayDefaultEnabled(pl))
		})
	}
}

func TestZapdisplayDefaultEnabledHandlesNilPlatform(t *testing.T) {
	t.Parallel()
	assert.False(t, zapdisplayDefaultEnabled(nil))
}
