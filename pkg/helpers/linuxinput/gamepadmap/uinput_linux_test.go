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

package gamepadmap

import (
	"testing"

	"github.com/bendahl/uinput"
	"github.com/stretchr/testify/assert"
)

// TestCodesMatchUinput pins the locally declared evdev codes to the uinput
// package's constants. The uinput device is driven with these codes, so any
// drift would silently press the wrong button.
func TestCodesMatchUinput(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		local  int
		uinput int
	}{
		"south":         {ButtonSouth, uinput.ButtonSouth},
		"east":          {ButtonEast, uinput.ButtonEast},
		"north":         {ButtonNorth, uinput.ButtonNorth},
		"west":          {ButtonWest, uinput.ButtonWest},
		"bumper left":   {ButtonBumperLeft, uinput.ButtonBumperLeft},
		"bumper right":  {ButtonBumperRight, uinput.ButtonBumperRight},
		"trigger left":  {ButtonTriggerLeft, uinput.ButtonTriggerLeft},
		"trigger right": {ButtonTriggerRight, uinput.ButtonTriggerRight},
		"select":        {ButtonSelect, uinput.ButtonSelect},
		"start":         {ButtonStart, uinput.ButtonStart},
		"mode":          {ButtonMode, uinput.ButtonMode},
		"dpad up":       {ButtonDpadUp, uinput.ButtonDpadUp},
		"dpad down":     {ButtonDpadDown, uinput.ButtonDpadDown},
		"dpad left":     {ButtonDpadLeft, uinput.ButtonDpadLeft},
		"dpad right":    {ButtonDpadRight, uinput.ButtonDpadRight},
	} {
		assert.Equal(t, tc.uinput, tc.local, "%s code drifted from uinput", name)
	}
}
