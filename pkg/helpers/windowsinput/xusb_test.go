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

package windowsinput

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput/gamepadmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every button ZapScript can name has to reach the driver. This fails when a
// button is added to gamepadmap without a Windows mapping.
func TestEveryMappedButtonIsSupported(t *testing.T) {
	t.Parallel()

	for name, code := range gamepadmap.GamepadMap {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			report := xusbReport{}
			require.NoError(t, report.setButton(code, true), "button %q (code %d)", name, code)
		})
	}
}

func TestButtonBitsAreDistinct(t *testing.T) {
	t.Parallel()

	seen := make(map[uint16]int, len(buttonBits))
	for code, bit := range buttonBits {
		if other, ok := seen[bit]; ok {
			t.Errorf("codes %d and %d both set bit 0x%04X", other, code, bit)
		}
		seen[bit] = code
	}
}

func TestSetButtonHoldsSeveralAtOnce(t *testing.T) {
	t.Parallel()

	report := xusbReport{}
	require.NoError(t, report.setButton(gamepadmap.ButtonSouth, true))
	require.NoError(t, report.setButton(gamepadmap.ButtonStart, true))
	assert.Equal(t, uint16(xusbA|xusbStart), report.buttons)

	// Releasing one must leave the other held.
	require.NoError(t, report.setButton(gamepadmap.ButtonSouth, false))
	assert.Equal(t, uint16(xusbStart), report.buttons)

	require.NoError(t, report.setButton(gamepadmap.ButtonStart, false))
	assert.Equal(t, uint16(0), report.buttons)
}

func TestSetButtonReleasingUnheldButtonIsHarmless(t *testing.T) {
	t.Parallel()

	report := xusbReport{buttons: xusbA}
	require.NoError(t, report.setButton(gamepadmap.ButtonStart, false))
	assert.Equal(t, uint16(xusbA), report.buttons)
}

// The triggers are analogue on a real pad, but the engine only knows pressed
// and released, so they swing between the limits.
func TestSetButtonDrivesTriggers(t *testing.T) {
	t.Parallel()

	report := xusbReport{}

	require.NoError(t, report.setButton(gamepadmap.ButtonTriggerLeft, true))
	assert.Equal(t, uint8(triggerFull), report.leftTrigger)
	assert.Equal(t, uint8(0), report.rightTrigger)
	assert.Equal(t, uint16(0), report.buttons, "a trigger must not set a button bit")

	require.NoError(t, report.setButton(gamepadmap.ButtonTriggerRight, true))
	assert.Equal(t, uint8(triggerFull), report.rightTrigger)

	require.NoError(t, report.setButton(gamepadmap.ButtonTriggerLeft, false))
	assert.Equal(t, uint8(0), report.leftTrigger)
	assert.Equal(t, uint8(triggerFull), report.rightTrigger)
}

func TestSetButtonRejectsUnknownCode(t *testing.T) {
	t.Parallel()

	report := xusbReport{}
	err := report.setButton(0x999, true)
	require.ErrorIs(t, err, ErrUnknownButton)
}

// The face buttons follow physical positions, so a ZapScript name means the
// same key on Windows as it does through uinput on Linux.
func TestFaceButtonPositions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		code int
		bit  uint16
	}{
		{"south is A", gamepadmap.ButtonSouth, xusbA},
		{"east is B", gamepadmap.ButtonEast, xusbB},
		{"west is X", gamepadmap.ButtonWest, xusbX},
		{"north is Y", gamepadmap.ButtonNorth, xusbY},
		{"select is back", gamepadmap.ButtonSelect, xusbBack},
		{"mode is guide", gamepadmap.ButtonMode, xusbGuide},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.bit, buttonBits[tc.code])
		})
	}
}
