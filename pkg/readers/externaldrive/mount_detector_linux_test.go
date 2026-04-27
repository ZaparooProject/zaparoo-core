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
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testMountEventTimeout   = time.Second
	testNoMountEventTimeout = 300 * time.Millisecond
)

func newLinuxMountDetectorForTest() *linuxMountDetector {
	return &linuxMountDetector{
		events:       make(chan MountEvent, 10),
		unmounts:     make(chan string, 10),
		stopChan:     make(chan struct{}),
		mountedDevs:  make(map[string]MountEvent),
		pathMappings: make(map[dbus.ObjectPath]string),
		blockProps:   make(map[dbus.ObjectPath]map[string]dbus.Variant),
	}
}

func absTestPath(parts ...string) string {
	return filepath.Join(append([]string{string(filepath.Separator)}, parts...)...)
}

func testBlockProps(deviceID string) map[string]dbus.Variant {
	devicePath := absTestPath("dev", "sdb1")
	return map[string]dbus.Variant{
		"IdUUID":        dbus.MakeVariant(deviceID),
		"Device":        dbus.MakeVariant([]byte(devicePath + "\x00")),
		"IdLabel":       dbus.MakeVariant("CARD"),
		"ConnectionBus": dbus.MakeVariant("usb"),
	}
}

func testFSProps(mountPath string) map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"MountPoints": dbus.MakeVariant([][]byte{[]byte(mountPath + "\x00")}),
	}
}

func requireMountEvent(t *testing.T, detector *linuxMountDetector) MountEvent {
	t.Helper()

	select {
	case event := <-detector.events:
		return event
	case <-time.After(testMountEventTimeout):
		require.Fail(t, "timed out waiting for mount event")
		return MountEvent{}
	}
}

func requireNoMountEvent(t *testing.T, detector *linuxMountDetector) {
	t.Helper()

	select {
	case event := <-detector.events:
		require.Failf(t, "unexpected mount event", "%+v", event)
	case <-time.After(testNoMountEventTimeout):
	}
}

func requireUnmountEvent(t *testing.T, detector *linuxMountDetector) string {
	t.Helper()

	select {
	case deviceID := <-detector.unmounts:
		return deviceID
	case <-time.After(testMountEventTimeout):
		require.Fail(t, "timed out waiting for unmount event")
		return ""
	}
}

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
	devicePath := absTestPath("dev", "sdd1")

	props := map[string]dbus.Variant{
		"Device": dbus.MakeVariant([]byte(devicePath + "\x00")),
	}

	deviceID := detector.getDeviceID(props)
	assert.Equal(t, devicePath, deviceID, "Device name should be last resort")
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
		"Device":   dbus.MakeVariant([]byte(absTestPath("dev", "sde1"))),
	}

	deviceID := detector.getDeviceID(props)
	assert.Equal(t, absTestPath("dev", "sde1"), deviceID, "Empty serial should trigger device fallback")
}

func TestMountPointsFromVariant_TrimsTrailingNulls(t *testing.T) {
	t.Parallel()

	mountPath := absTestPath("run", "media", "user", "CARD")
	mountPoints := mountPointsFromVariant(dbus.MakeVariant([][]byte{
		[]byte(mountPath + "\x00"),
		[]byte("\x00"),
	}))

	assert.Equal(t, []string{mountPath}, mountPoints)
}

func TestProcessManagedObjects_EmitsExistingMountedFilesystem(t *testing.T) {
	detector := newLinuxMountDetectorForTest()
	mountPath := absTestPath("run", "media", "user", "CARD")
	objectPath := dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/sdb1")

	detector.processManagedObjects(map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
		objectPath: {
			udisks2BlockInterface: testBlockProps("uuid-1"),
			udisks2FSInterface:    testFSProps(mountPath),
		},
	})

	event := requireMountEvent(t, detector)
	assert.Equal(t, "uuid-1", event.DeviceID)
	assert.Equal(t, absTestPath("dev", "sdb1"), event.DeviceNode)
	assert.Equal(t, mountPath, event.MountPath)
	assert.Equal(t, "CARD", event.VolumeLabel)
	assert.Equal(t, "USB", event.DeviceType)
}

func TestProcessManagedObjects_SkipsSystemAndIgnoredFilesystems(t *testing.T) {
	detector := newLinuxMountDetectorForTest()
	mountPath := absTestPath("run", "media", "user", "CARD")
	systemProps := testBlockProps("system")
	systemProps["HintSystem"] = dbus.MakeVariant(true)
	ignoredProps := testBlockProps("ignored")
	ignoredProps["HintIgnore"] = dbus.MakeVariant(true)

	detector.processManagedObjects(map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
		dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/sda1"): {
			udisks2BlockInterface: systemProps,
			udisks2FSInterface:    testFSProps(mountPath),
		},
		dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/sdb1"): {
			udisks2BlockInterface: ignoredProps,
			udisks2FSInterface:    testFSProps(mountPath),
		},
	})

	requireNoMountEvent(t, detector)
}

func TestProcessObjectMount_DoesNotDuplicateTrackedMount(t *testing.T) {
	detector := newLinuxMountDetectorForTest()
	mountPath := absTestPath("run", "media", "user", "CARD")
	objectPath := dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/sdb1")

	detector.processObjectMount(objectPath, testBlockProps("uuid-1"), testFSProps(mountPath), "test")
	_ = requireMountEvent(t, detector)
	detector.processObjectMount(objectPath, testBlockProps("uuid-1"), testFSProps(mountPath), "test")

	requireNoMountEvent(t, detector)
}

func TestRegisterMountEventReplacesStaleObjectPathForDevice(t *testing.T) {
	detector := newLinuxMountDetectorForTest()
	oldPath := dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/sdb1")
	newPath := dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/sdc1")
	oldMountPath := absTestPath("run", "media", "user", "OLD")
	newMountPath := absTestPath("run", "media", "user", "NEW")

	detector.processObjectMount(oldPath, testBlockProps("uuid-1"), testFSProps(oldMountPath), "test")
	_ = requireMountEvent(t, detector)
	detector.processObjectMount(newPath, testBlockProps("uuid-1"), testFSProps(newMountPath), "test")
	_ = requireMountEvent(t, detector)

	detector.mu.RLock()
	defer detector.mu.RUnlock()
	assert.Equal(t, "uuid-1", detector.pathMappings[newPath])
	assert.NotContains(t, detector.pathMappings, oldPath)
	assert.NotContains(t, detector.blockProps, oldPath)
	assert.Equal(t, newMountPath, detector.mountedDevs["uuid-1"].MountPath)
}

func TestPropertiesChanged_EmitsMountAfterEmptyInterfacesAdded(t *testing.T) {
	detector := newLinuxMountDetectorForTest()
	objectPath := dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/sdb1")
	mountPath := absTestPath("run", "media", "user", "CARD")

	detector.handleInterfacesAdded(&dbus.Signal{
		Name: dbusObjectManager + ".InterfacesAdded",
		Body: []any{
			objectPath,
			map[string]map[string]dbus.Variant{
				udisks2BlockInterface: testBlockProps("uuid-1"),
				udisks2FSInterface: {
					"MountPoints": dbus.MakeVariant([][]byte{}),
				},
			},
		},
	})
	requireNoMountEvent(t, detector)

	detector.handlePropertiesChanged(&dbus.Signal{
		Name: dbusProperties + ".PropertiesChanged",
		Path: objectPath,
		Body: []any{
			udisks2FSInterface,
			map[string]dbus.Variant{"MountPoints": dbus.MakeVariant([][]byte{[]byte(mountPath + "\x00")})},
			[]string{},
		},
	})

	event := requireMountEvent(t, detector)
	assert.Equal(t, "uuid-1", event.DeviceID)
	assert.Equal(t, mountPath, event.MountPath)
}

func TestPropertiesChanged_EmptyMountPointsEmitsUnmount(t *testing.T) {
	detector := newLinuxMountDetectorForTest()
	objectPath := dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/sdb1")
	mountPath := absTestPath("run", "media", "user", "CARD")

	detector.processObjectMount(objectPath, testBlockProps("uuid-1"), testFSProps(mountPath), "test")
	_ = requireMountEvent(t, detector)
	detector.handlePropertiesChanged(&dbus.Signal{
		Name: dbusProperties + ".PropertiesChanged",
		Path: objectPath,
		Body: []any{
			udisks2FSInterface,
			map[string]dbus.Variant{"MountPoints": dbus.MakeVariant([][]byte{})},
			[]string{},
		},
	})

	deviceID := requireUnmountEvent(t, detector)
	assert.Equal(t, "uuid-1", deviceID)
}

func TestPropertiesChanged_InvalidatedMountPointsUsesCurrentValue(t *testing.T) {
	detector := newLinuxMountDetectorForTest()
	objectPath := dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/sdb1")
	mountPath := absTestPath("run", "media", "user", "CARD")
	detector.mountReader = func(path dbus.ObjectPath) []string {
		require.Equal(t, objectPath, path)
		return []string{mountPath}
	}
	detector.blockReader = func(path dbus.ObjectPath) (map[string]dbus.Variant, error) {
		require.Equal(t, objectPath, path)
		return testBlockProps("uuid-1"), nil
	}

	detector.handlePropertiesChanged(&dbus.Signal{
		Name: dbusProperties + ".PropertiesChanged",
		Path: objectPath,
		Body: []any{
			udisks2FSInterface,
			map[string]dbus.Variant{},
			[]string{"MountPoints"},
		},
	})

	event := requireMountEvent(t, detector)
	assert.Equal(t, "uuid-1", event.DeviceID)
	assert.Equal(t, mountPath, event.MountPath)
}

func TestPropertiesChanged_FetchesBlockPropsWhenMissingFromCache(t *testing.T) {
	detector := newLinuxMountDetectorForTest()
	objectPath := dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/sdb1")
	mountPath := absTestPath("run", "media", "user", "CARD")
	blockReaderCalls := 0
	detector.blockReader = func(path dbus.ObjectPath) (map[string]dbus.Variant, error) {
		blockReaderCalls++
		require.Equal(t, objectPath, path)
		return testBlockProps("uuid-1"), nil
	}

	detector.handlePropertiesChanged(&dbus.Signal{
		Name: dbusProperties + ".PropertiesChanged",
		Path: objectPath,
		Body: []any{
			udisks2FSInterface,
			map[string]dbus.Variant{"MountPoints": dbus.MakeVariant([][]byte{[]byte(mountPath + "\x00")})},
			[]string{},
		},
	})

	event := requireMountEvent(t, detector)
	assert.Equal(t, "uuid-1", event.DeviceID)
	assert.Equal(t, mountPath, event.MountPath)
	assert.Equal(t, 1, blockReaderCalls)
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
