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

package hoststatus

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsBluetoothGamepad(t *testing.T) {
	t.Parallel()

	hid := "00001124-0000-1000-8000-00805f9b34fb"
	hogp := "00001812-0000-1000-8000-00805F9B34FB"
	tests := []struct {
		name   string
		device BluetoothDevice
		want   bool
	}{
		{name: "DualShock 4 class", device: BluetoothDevice{Class: 0x002508}, want: true},
		{name: "joystick class", device: BluetoothDevice{Class: 0x000504}, want: true},
		{name: "keyboard class", device: BluetoothDevice{Class: 0x002540}},
		{name: "remote control class", device: BluetoothDevice{Class: 0x00050C}},
		{name: "headset class", device: BluetoothDevice{Class: 0x240404}},
		{name: "Xbox low energy appearance", device: BluetoothDevice{Appearance: 0x03C4}, want: true},
		{name: "joystick appearance", device: BluetoothDevice{Appearance: 0x03C3}, want: true},
		{name: "keyboard appearance", device: BluetoothDevice{Appearance: 0x03C1}},
		{name: "gaming icon", device: BluetoothDevice{Icon: "input-gaming"}, want: true},
		{
			name:   "HID with a controller name",
			device: BluetoothDevice{UUIDs: []string{hid}, Name: "Pro Controller"},
			want:   true,
		},
		{
			name:   "HID over GATT with a controller name",
			device: BluetoothDevice{UUIDs: []string{hogp}, Name: "8BitDo Lite 2"},
			want:   true,
		},
		{name: "HID mouse", device: BluetoothDevice{UUIDs: []string{hid}, Name: "MX Master 3"}},
		{name: "controller name without HID", device: BluetoothDevice{Name: "Wireless Controller"}},
		{name: "nothing known", device: BluetoothDevice{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsBluetoothGamepad(&tt.device))
		})
	}
}

func TestNormalizeBluetoothAddress(t *testing.T) {
	t.Parallel()

	got, ok := NormalizeBluetoothAddress("aa:bb:cc:dd:ee:0f")
	assert.True(t, ok)
	assert.Equal(t, "AA:BB:CC:DD:EE:0F", got)

	for _, bad := range []string{"", "AA:BB:CC:DD:EE", "AA-BB-CC-DD-EE-FF", "AA:BB:CC:DD:EE:GG", "AAA:BB:CC:DD:EE:F"} {
		_, ok := NormalizeBluetoothAddress(bad)
		assert.False(t, ok, bad)
	}
}
