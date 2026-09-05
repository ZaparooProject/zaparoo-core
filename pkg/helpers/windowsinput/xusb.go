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
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput/gamepadmap"
)

// XUSB button bits, matching the XINPUT_GAMEPAD_* constants an Xbox 360 pad
// reports.
const (
	xusbDpadUp        = 0x0001
	xusbDpadDown      = 0x0002
	xusbDpadLeft      = 0x0004
	xusbDpadRight     = 0x0008
	xusbStart         = 0x0010
	xusbBack          = 0x0020
	xusbLeftShoulder  = 0x0100
	xusbRightShoulder = 0x0200
	xusbGuide         = 0x0400
	xusbA             = 0x1000
	xusbB             = 0x2000
	xusbX             = 0x4000
	xusbY             = 0x8000
)

// triggerFull is the analogue value a digital trigger press reports. The
// engine only knows about pressed and released, so a trigger goes straight to
// its limit the way it does through uinput on Linux.
const triggerFull = 0xFF

// ErrUnknownButton is returned for a button code with no XUSB equivalent.
var ErrUnknownButton = errors.New("gamepad button is not supported on windows")

// buttonBits maps evdev button codes to the XUSB bit they set. The face
// buttons follow the same physical positions the Linux xpad driver reports,
// so a ZapScript button means the same key on both platforms.
//
//nolint:gochecknoglobals // lookup table
var buttonBits = map[int]uint16{
	gamepadmap.ButtonSouth:       xusbA,
	gamepadmap.ButtonEast:        xusbB,
	gamepadmap.ButtonWest:        xusbX,
	gamepadmap.ButtonNorth:       xusbY,
	gamepadmap.ButtonBumperLeft:  xusbLeftShoulder,
	gamepadmap.ButtonBumperRight: xusbRightShoulder,
	gamepadmap.ButtonSelect:      xusbBack,
	gamepadmap.ButtonStart:       xusbStart,
	gamepadmap.ButtonMode:        xusbGuide,
	gamepadmap.ButtonDpadUp:      xusbDpadUp,
	gamepadmap.ButtonDpadDown:    xusbDpadDown,
	gamepadmap.ButtonDpadLeft:    xusbDpadLeft,
	gamepadmap.ButtonDpadRight:   xusbDpadRight,
}

// xusbReport mirrors the XUSB_REPORT structure the driver expects.
type xusbReport struct {
	buttons      uint16
	leftTrigger  uint8
	rightTrigger uint8
	// The engine only knows buttons, so the four thumb axes are always sent
	// centred. They are named rather than reserved because the driver reads
	// the report by offset and the layout has to stay recognisable.
	thumbLX int16 //nolint:unused // part of the driver's report layout
	thumbLY int16 //nolint:unused // part of the driver's report layout
	thumbRX int16 //nolint:unused // part of the driver's report layout
	thumbRY int16 //nolint:unused // part of the driver's report layout
}

// setButton records a press or release of an evdev button code. The report is
// cumulative: the caller submits the whole thing after every change, which is
// how XUSB reports work and how holding several buttons at once stays correct.
func (r *xusbReport) setButton(code int, down bool) error {
	if bit, ok := buttonBits[code]; ok {
		if down {
			r.buttons |= bit
		} else {
			r.buttons &^= bit
		}
		return nil
	}

	var trigger uint8
	if down {
		trigger = triggerFull
	}

	switch code {
	case gamepadmap.ButtonTriggerLeft:
		r.leftTrigger = trigger
	case gamepadmap.ButtonTriggerRight:
		r.rightTrigger = trigger
	default:
		return fmt.Errorf("%w: code %d", ErrUnknownButton, code)
	}
	return nil
}
