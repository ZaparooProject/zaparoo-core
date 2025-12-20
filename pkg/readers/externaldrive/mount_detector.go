// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

// MountEvent represents a filesystem mount event for a removable storage device.
type MountEvent struct {
	// DeviceID is a unique and stable identifier for the device, such as a volume UUID
	// or serial number. This is used to track the device across mount/unmount cycles.
	DeviceID string

	// DeviceNode is the block device path (e.g., "/dev/sda1", "/dev/mmcblk0p1").
	// Used for safety checks when detecting stale mounts. May be empty if unavailable.
	DeviceNode string

	// MountPath is the filesystem path where the volume is mounted.
	// Examples: "E:\", "/media/user/USB_DRIVE", "/Volumes/MyUSB"
	MountPath string

	// VolumeLabel is the user-facing volume label for the device.
	// Examples: "MyUSB", "SD_CARD"
	VolumeLabel string

	// DeviceType indicates the type of removable device.
	// Examples: "USB", "SD", "removable"
	DeviceType string
}

// MountDetector provides platform-specific mount event detection for removable storage devices.
// Implementations must be event-driven (not polling-based) and should filter for removable
// devices only, excluding internal hard drives and system partitions.
type MountDetector interface {
	// Events returns a channel that emits MountEvent when a removable device is mounted.
	// The channel is closed when Stop() is called.
	Events() <-chan MountEvent

	// Unmounts returns a channel that emits the DeviceID when a removable device is unmounted.
	// The channel is closed when Stop() is called.
	Unmounts() <-chan string

	// Start begins monitoring for mount/unmount events.
	// Returns an error if the platform-specific monitoring service cannot be initialized.
	Start() error

	// Stop terminates the mount detector and releases all resources.
	// After Stop() is called, the Events() and Unmounts() channels are closed.
	Stop()

	// Forget removes a device from internal tracking, allowing it to be
	// re-detected on the next scan. Used when a mount is detected as stale
	// (e.g., block device no longer exists after USB was yanked).
	Forget(deviceID string)
}
