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

package inputactivity

// winmm joystick axes report 0..65535 with the stick centred near the middle.
const (
	joyAxisDeadzone = int32(65535 * 12 / 100)
	joyAxisCount    = 6
)

// joySample is one reading of a winmm joystick.
type joySample struct {
	axes    [joyAxisCount]uint32
	buttons uint32
	pov     uint32
}

// applyJoySample feeds a winmm reading for the joystick id into snap and
// reports whether a button, the hat or an axis beyond the deadzone changed.
func applyJoySample(snap *Snapshot, id uint32, s joySample) bool {
	base := id << 8
	active := snap.Button(base|0x40, int32(s.buttons)) //nolint:gosec // bit pattern, only compared
	if snap.Button(base|0x41, int32(s.pov)) {          //nolint:gosec // bit pattern, only compared
		active = true
	}
	for i, v := range s.axes {
		if snap.Axis(base|uint32(i), int32(v), joyAxisDeadzone) { //nolint:gosec // 16-bit axis
			active = true
		}
	}
	return active
}
