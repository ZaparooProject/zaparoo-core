//go:build linux

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

package externaldrive

import (
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDeviceID_UUID_Priority(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	props := map[string]dbus.Variant{
		"IdUUID":   dbus.MakeVariant("abc-123-uuid"),
		"IdSerial": dbus.MakeVariant("SERIAL456"),
		"Device":   dbus.MakeVariant([]byte("/dev/sdb1")),
	}

	deviceID := detector.getDeviceID(props)
	assert.Equal(t, "abc-123-uuid", deviceID, "UUID should have highest priority")
}

func TestGetDeviceID_Serial_Fallback(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	props := map[string]dbus.Variant{
		"IdSerial": dbus.MakeVariant("SERIAL789"),
		"Device":   dbus.MakeVariant([]byte("/dev/sdc1")),
	}

	deviceID := detector.getDeviceID(props)
	assert.Equal(t, "SERIAL789", deviceID, "Serial should be used when UUID absent")
}

func TestGetDeviceID_Device_LastResort(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	props := map[string]dbus.Variant{
		"Device": dbus.MakeVariant([]byte("/dev/sdd1")),
	}

	deviceID := detector.getDeviceID(props)
	assert.Equal(t, "/dev/sdd1", deviceID, "Device name should be last resort")
}

func TestGetDeviceID_EmptyUUID_FallsBackToSerial(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	props := map[string]dbus.Variant{
		"IdUUID":   dbus.MakeVariant(""),
		"IdSerial": dbus.MakeVariant("SERIAL999"),
	}

	deviceID := detector.getDeviceID(props)
	assert.Equal(t, "SERIAL999", deviceID, "Empty UUID should trigger fallback to serial")
}

func TestGetDeviceID_EmptySerial_FallsBackToDevice(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	props := map[string]dbus.Variant{
		"IdUUID":   dbus.MakeVariant(""),
		"IdSerial": dbus.MakeVariant(""),
		"Device":   dbus.MakeVariant([]byte("/dev/sde1")),
	}

	deviceID := detector.getDeviceID(props)
	assert.Equal(t, "/dev/sde1", deviceID, "Empty serial should trigger device fallback")
}

func TestGetDeviceID_NoProperties(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	props := map[string]dbus.Variant{}

	deviceID := detector.getDeviceID(props)
	assert.Empty(t, deviceID, "No properties should return empty string")
}

func TestGetDeviceID_WrongTypes(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	// Properties with wrong types should be skipped
	props := map[string]dbus.Variant{
		"IdUUID":   dbus.MakeVariant(123), // int instead of string
		"IdSerial": dbus.MakeVariant(456), // int instead of string
		"Device":   dbus.MakeVariant("string instead of []byte"),
	}

	deviceID := detector.getDeviceID(props)
	assert.Empty(t, deviceID, "Wrong types should be skipped and return empty")
}

func TestGetVolumeLabel_Present(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	props := map[string]dbus.Variant{
		"IdLabel": dbus.MakeVariant("MY_USB_DRIVE"),
	}

	label := detector.getVolumeLabel(props)
	assert.Equal(t, "MY_USB_DRIVE", label)
}

func TestGetVolumeLabel_Missing(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	props := map[string]dbus.Variant{}

	label := detector.getVolumeLabel(props)
	assert.Empty(t, label, "Missing label should return empty string")
}

func TestGetVolumeLabel_WrongType(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	props := map[string]dbus.Variant{
		"IdLabel": dbus.MakeVariant(123), // int instead of string
	}

	label := detector.getVolumeLabel(props)
	assert.Empty(t, label, "Wrong type should return empty string")
}

func TestGetDeviceType_USB(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	props := map[string]dbus.Variant{
		"ConnectionBus": dbus.MakeVariant("usb"),
	}

	deviceType := detector.getDeviceType(props)
	assert.Equal(t, "USB", deviceType)
}

func TestGetDeviceType_SD(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	props := map[string]dbus.Variant{
		"ConnectionBus": dbus.MakeVariant("sdio"),
	}

	deviceType := detector.getDeviceType(props)
	assert.Equal(t, "SD", deviceType)
}

func TestGetDeviceType_Removable_Default(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	props := map[string]dbus.Variant{
		"ConnectionBus": dbus.MakeVariant("other"),
	}

	deviceType := detector.getDeviceType(props)
	assert.Equal(t, "removable", deviceType, "Unknown bus should default to removable")
}

func TestGetDeviceType_Removable_DriveTrue(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	props := map[string]dbus.Variant{
		"Removable": dbus.MakeVariant(true),
	}

	deviceType := detector.getDeviceType(props)
	assert.Equal(t, "removable", deviceType)
}

func TestGetDeviceType_NoProperties(t *testing.T) {
	t.Parallel()

	detector := &linuxMountDetector{}

	props := map[string]dbus.Variant{}

	deviceType := detector.getDeviceType(props)
	assert.Equal(t, "unknown", deviceType, "No properties should return unknown")
}

func TestFallbackRescanInterval_IsReasonable(t *testing.T) {
	t.Parallel()

	// Verify the rescan interval is within acceptable bounds
	// Too short = high CPU usage, too long = poor UX
	assert.GreaterOrEqual(t, fallbackRescanInterval.Seconds(), 1.0,
		"Rescan interval should be at least 1 second to avoid CPU overhead")
	assert.LessOrEqual(t, fallbackRescanInterval.Seconds(), 10.0,
		"Rescan interval should be at most 10 seconds for reasonable UX")
}

func TestFallbackDetector_HasLastScanField(t *testing.T) {
	t.Parallel()

	// Verify the detector struct has the lastScan field by creating one
	detector := &linuxMountDetectorFallback{}

	// The lastScan field should be zero-valued on creation
	assert.True(t, detector.lastScan.IsZero(),
		"lastScan should be zero-valued on new detector")
}

func TestNewLinuxMountDetectorFallback_InitializesCorrectly(t *testing.T) {
	t.Parallel()

	detector, err := newLinuxMountDetectorFallback()
	require.NoError(t, err)
	require.NotNil(t, detector)

	// Cast to access internal fields
	fallback, ok := detector.(*linuxMountDetectorFallback)
	assert.True(t, ok, "Should return linuxMountDetectorFallback")

	// Verify channels are initialized
	assert.NotNil(t, fallback.events)
	assert.NotNil(t, fallback.unmounts)
	assert.NotNil(t, fallback.stopChan)
	assert.NotNil(t, fallback.mountedDevs)

	// lastScan should be zero initially (set when Start() is called)
	assert.True(t, fallback.lastScan.IsZero())
}
