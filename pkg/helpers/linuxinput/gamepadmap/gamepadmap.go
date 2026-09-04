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

// Package gamepadmap holds the ZapScript gamepad button names and the raw
// evdev button codes they resolve to. The codes are the canonical namespace
// the input engine works in; backends translate them to whatever their own
// API expects. It is split out of the uinput-dependent parent package so
// platforms without uinput can share the same button names.
package gamepadmap

// Raw evdev BTN_* codes. These match the uinput package's Button* constants,
// but are declared here so non-Linux builds can use the same namespace.
const (
	ButtonSouth        = 0x130 // BTN_SOUTH
	ButtonEast         = 0x131 // BTN_EAST
	ButtonNorth        = 0x133 // BTN_NORTH
	ButtonWest         = 0x134 // BTN_WEST
	ButtonBumperLeft   = 0x136 // BTN_TL
	ButtonBumperRight  = 0x137 // BTN_TR
	ButtonTriggerLeft  = 0x138 // BTN_TL2
	ButtonTriggerRight = 0x139 // BTN_TR2
	ButtonSelect       = 0x13a // BTN_SELECT
	ButtonStart        = 0x13b // BTN_START
	ButtonMode         = 0x13c // BTN_MODE
	ButtonDpadUp       = 0x220 // BTN_DPAD_UP
	ButtonDpadDown     = 0x221 // BTN_DPAD_DOWN
	ButtonDpadLeft     = 0x222 // BTN_DPAD_LEFT
	ButtonDpadRight    = 0x223 // BTN_DPAD_RIGHT
)

var GamepadMap = map[string]int{
	"^":        ButtonDpadUp,
	"{up}":     ButtonDpadUp,
	"v":        ButtonDpadDown,
	"V":        ButtonDpadDown,
	"{down}":   ButtonDpadDown,
	"<":        ButtonDpadLeft,
	"{left}":   ButtonDpadLeft,
	">":        ButtonDpadRight,
	"{right}":  ButtonDpadRight,
	"A":        ButtonEast,
	"a":        ButtonEast,
	"{east}":   ButtonEast,
	"B":        ButtonSouth,
	"b":        ButtonSouth,
	"{south}":  ButtonSouth,
	"X":        ButtonNorth,
	"x":        ButtonNorth,
	"{north}":  ButtonNorth,
	"Y":        ButtonWest,
	"y":        ButtonWest,
	"{west}":   ButtonWest,
	"{start}":  ButtonStart,
	"{select}": ButtonSelect,
	"{menu}":   ButtonMode,
	"L":        ButtonBumperLeft,
	"l":        ButtonBumperLeft,
	"{l1}":     ButtonBumperLeft,
	"R":        ButtonBumperRight,
	"r":        ButtonBumperRight,
	"{r1}":     ButtonBumperRight,
	"{l2}":     ButtonTriggerLeft,
	"{r2}":     ButtonTriggerRight,
}

// ToGamepadCode converts a single ZapScript button symbol to an evdev code.
func ToGamepadCode(name string) (int, bool) {
	v, ok := GamepadMap[name]
	return v, ok
}
