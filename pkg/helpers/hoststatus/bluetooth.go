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
	"context"
	"errors"
	"strings"
)

var (
	// ErrBluetoothNoAdapter means the Bluetooth stack is running but has no
	// adapter to use.
	ErrBluetoothNoAdapter = errors.New("no bluetooth adapter")
	// ErrBluetoothNotPowered means the adapter is off. Core never turns it on
	// by itself.
	ErrBluetoothNotPowered = errors.New("bluetooth adapter is powered off")
	// ErrBluetoothNotFound means the stack does not know the device.
	ErrBluetoothNotFound = errors.New("bluetooth device not found")
	// ErrBluetoothPinRequired means the device asked for a PIN or passkey,
	// which Core has no way to show or enter.
	ErrBluetoothPinRequired = errors.New("bluetooth device requires a PIN or passkey")
	// ErrBluetoothRejected means the device or the stack refused the pairing.
	ErrBluetoothRejected = errors.New("bluetooth pairing was rejected")
	// ErrBluetoothTimeout means the device stopped answering.
	ErrBluetoothTimeout = errors.New("bluetooth device did not answer")
)

// BluetoothDevice is one remote device the Bluetooth stack knows about.
type BluetoothDevice struct {
	// Address is the device's address in upper case, colon separated.
	Address string
	// Name is empty when the device has not said what it is called.
	Name string
	// VendorID and ProductID come from the device's modalias: four lowercase
	// hex digits, or empty.
	VendorID  string
	ProductID string
	Icon      string
	UUIDs     []string
	// Class is the Class of Device of a classic device, or zero.
	Class uint32
	// Appearance is a low energy device's appearance value, or zero.
	Appearance uint16
	Paired     bool
	Connected  bool
	Trusted    bool
	// InRange means the current discovery has heard from the device.
	InRange bool
}

// BluetoothReading is the adapter and the devices it knows.
type BluetoothReading struct {
	Devices     []BluetoothDevice
	Powered     bool
	Discovering bool
}

// BluetoothController manages Bluetooth devices. Every method returns
// ErrUnsupported when the stack is not running and ErrBluetoothNoAdapter when
// it has no adapter.
type BluetoothController interface {
	Read(ctx context.Context) (BluetoothReading, error)
	// StartDiscovery and StopDiscovery start and end this process's own
	// discovery session. Other programs' sessions are left alone.
	StartDiscovery(ctx context.Context) error
	StopDiscovery(ctx context.Context) error
	// Pair bonds with the device, answering the stack's questions without
	// user input while the attempt lasts. It blocks until the device answers
	// or ctx ends.
	Pair(ctx context.Context, address string) error
	Trust(ctx context.Context, address string) error
	Connect(ctx context.Context, address string) error
	// Remove unpairs the device and forgets it.
	Remove(ctx context.Context, address string) error
	// Watch calls onChange whenever the stack reports a change, until ctx
	// ends. onChange must not block.
	Watch(ctx context.Context, onChange func())
}

// NormalizeBluetoothAddress returns address in upper case when it is a
// colon-separated six-octet address, and false otherwise.
func NormalizeBluetoothAddress(address string) (string, bool) {
	const octets = 6
	parts := strings.Split(address, ":")
	if len(parts) != octets {
		return "", false
	}
	for _, part := range parts {
		if len(part) != 2 || !isHex(part[0]) || !isHex(part[1]) {
			return "", false
		}
	}
	return strings.ToUpper(address), true
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

const (
	codMajorPeripheral = 0x05
	codMinorJoystick   = 0x01
	codMinorGamepad    = 0x02

	appearanceJoystick = 0x03C3
	appearanceGamepad  = 0x03C4

	iconGaming = "input-gaming"
)

// hidUUIDs are the classic HID profile and HID over GATT.
var hidUUIDs = map[string]bool{ //nolint:gochecknoglobals // Static lookup table.
	"00001124-0000-1000-8000-00805f9b34fb": true,
	"00001812-0000-1000-8000-00805f9b34fb": true,
}

// gamepadNames are fragments of the names controllers advertise, for the ones
// that describe themselves only as generic HID devices.
var gamepadNames = []string{ //nolint:gochecknoglobals // Static lookup table.
	"controller", "gamepad", "joystick", "joy-con", "dualshock", "dualsense",
	"8bitdo", "xbox", "wireless controller", "nintendo rvl", "stadia", "nimbus",
}

// IsBluetoothGamepad reports whether a device identifies itself as a gamepad
// or joystick: by its Class of Device, its low energy appearance, the icon the
// stack derived from either, or failing those a HID profile and a name that
// says controller.
func IsBluetoothGamepad(device *BluetoothDevice) bool {
	major := (device.Class >> 8) & 0x1F
	minor := (device.Class >> 2) & 0x0F
	if major == codMajorPeripheral && (minor == codMinorJoystick || minor == codMinorGamepad) {
		return true
	}
	if device.Appearance == appearanceGamepad || device.Appearance == appearanceJoystick {
		return true
	}
	if device.Icon == iconGaming {
		return true
	}
	hid := false
	for _, uuid := range device.UUIDs {
		if hidUUIDs[strings.ToLower(uuid)] {
			hid = true
			break
		}
	}
	if !hid {
		return false
	}
	name := strings.ToLower(device.Name)
	for _, fragment := range gamepadNames {
		if strings.Contains(name, fragment) {
			return true
		}
	}
	return false
}
