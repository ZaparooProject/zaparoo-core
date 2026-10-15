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

import "encoding/binary"

// joystickDeadzone is how far a legacy joystick axis (range +-32767) must move
// before it counts.
const joystickDeadzone = 2000

// samPlan is what to watch for a given device list.
type samPlan struct {
	joysticks []string
	keyboards []InputDevice
}

// planSAM picks the joysticks and keyboards worth watching. A keyboard that
// shares its physical USB parent with a joystick is a gamepad's own keyboard
// interface, which chatters, so it is left out.
func planSAM(devices []InputDevice) samPlan {
	var plan samPlan
	joyParents := map[string]struct{}{}
	for i := range devices {
		dev := &devices[i]
		node, ok := dev.JoystickNode()
		if !ok || dev.Skipped() {
			continue
		}
		plan.joysticks = append(plan.joysticks, node)
		if parent := dev.UsbParent(); parent != "" {
			joyParents[parent] = struct{}{}
		}
	}
	for i := range devices {
		dev := &devices[i]
		// A keyboard is only watched through its hidraw node, which a uinput
		// keyboard such as MiSTer Main's own or the mrext remote never has, so
		// leaving them in would mean no keyboard could ever be fully covered.
		if dev.Skipped() || dev.Synthetic() || dev.Classify() != Keyboard {
			continue
		}
		if _, ok := dev.JoystickNode(); ok {
			continue
		}
		if _, shared := joyParents[dev.UsbParent()]; shared && dev.UsbParent() != "" {
			continue
		}
		plan.keyboards = append(plan.keyboards, *dev)
	}
	return plan
}

const (
	jsEventSize = 8
	jsButton    = 0x01
	jsAxis      = 0x02
	jsInit      = 0x80
)

// applyJSEvents feeds a buffer of struct js_event records into snap and
// reports whether a button changed or an axis moved beyond the deadzone.
func applyJSEvents(snap *Snapshot, data []byte) bool {
	active := false
	for off := 0; off+jsEventSize <= len(data); off += jsEventSize {
		value := int32(int16(binary.NativeEndian.Uint16(data[off+4:]))) //nolint:gosec // js value is a signed 16-bit
		typ := data[off+6] &^ jsInit
		number := data[off+7]
		key := uint32(typ)<<8 | uint32(number)
		switch typ {
		case jsButton:
			if snap.Button(key, value) {
				active = true
			}
		case jsAxis:
			if snap.Axis(key, value, joystickDeadzone) {
				active = true
			}
		}
	}
	return active
}
