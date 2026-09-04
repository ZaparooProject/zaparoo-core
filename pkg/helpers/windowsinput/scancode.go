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

// Package windowsinput drives virtual keyboard input on Windows. The input
// engine works in raw evdev keycodes, so this package translates them to the
// AT scancode set 1 values SendInput expects.
package windowsinput

// extendedScancodes holds the evdev keycodes whose scancodes need the 0xE0
// extended prefix. These are the keys the original AT keyboard did not have,
// so they were added on an escaped code rather than a free one.
//
//nolint:gochecknoglobals // immutable translation table
var extendedScancodes = map[int]uint16{
	96:  0x1C, // KEY_KPENTER
	97:  0x1D, // KEY_RIGHTCTRL
	98:  0x35, // KEY_KPSLASH
	99:  0x37, // KEY_SYSRQ
	100: 0x38, // KEY_RIGHTALT
	102: 0x47, // KEY_HOME
	103: 0x48, // KEY_UP
	104: 0x49, // KEY_PAGEUP
	105: 0x4B, // KEY_LEFT
	106: 0x4D, // KEY_RIGHT
	107: 0x4F, // KEY_END
	108: 0x50, // KEY_DOWN
	109: 0x51, // KEY_PAGEDOWN
	110: 0x52, // KEY_INSERT
	111: 0x53, // KEY_DELETE
	114: 0x2E, // KEY_VOLUMEDOWN
	115: 0x30, // KEY_VOLUMEUP
	125: 0x5B, // KEY_LEFTMETA
	126: 0x5C, // KEY_RIGHTMETA
	127: 0x5D, // KEY_COMPOSE
}

// Scancode returns the AT set 1 scancode for an evdev keycode, whether it needs
// the extended prefix, and whether the key is known at all.
//
// Linux assigned its keycodes 1-83 to match the scancodes the AT keyboard
// already sent, so that range needs no translation; F11 and F12 kept the same
// trick at 87 and 88. Everything else was added later and has to be looked up.
func Scancode(code int) (scancode uint16, extended, ok bool) {
	if sc, isExtended := extendedScancodes[code]; isExtended {
		return sc, true, true
	}
	if code < 1 || code > 88 {
		return 0, false, false
	}
	// 84 to 86 are the keys Linux added in the gap before F11, and they have
	// no unprefixed AT scancode.
	if code >= 84 && code <= 86 {
		return 0, false, false
	}
	return uint16(code), false, true
}
