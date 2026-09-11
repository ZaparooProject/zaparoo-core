//go:build linux

/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package helpers

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

// usbTopologyPattern matches USB topology paths like "1-2", "1-2.3", "1-2.3.1"
var usbTopologyPattern = regexp.MustCompile(`^\d+-[\d.]+$`)

// GetUSBTopologyPath resolves a device path (e.g., /dev/ttyUSB0) to its
// USB port topology path (e.g., "1-2.3.1"). This path is stable across
// reboots as long as the device stays in the same physical USB port.
//
// Returns empty string if topology cannot be determined (e.g., in Docker,
// non-USB device, or missing /sys filesystem).
func GetUSBTopologyPath(devicePath string) string {
	sysfsPath := deviceNodeSysfsPath(devicePath)
	if sysfsPath == "" {
		return ""
	}

	// Walk up the path looking for a USB topology directory
	// The path looks like: /sys/devices/pci.../usb1/1-2/1-2.3/1-2.3:1.0/tty/ttyUSB0
	// We want to extract "1-2.3" (the USB topology)
	return extractUSBTopology(sysfsPath)
}

// deviceNodeSysfsPath resolves a character device node to the sysfs directory
// describing it, via /sys/dev/char/{major}:{minor}. Returns "" when the node
// cannot be stat'd or has no sysfs entry.
func deviceNodeSysfsPath(devicePath string) string {
	if devicePath == "" {
		return ""
	}

	// unix.Stat, not os.Stat: os.Stat's FileInfo.Sys() carries a
	// *syscall.Stat_t, so asserting it to *unix.Stat_t never succeeded and
	// every caller silently got "" on every Linux device.
	var stat unix.Stat_t
	if err := unix.Stat(devicePath, &stat); err != nil {
		log.Debug().Str("path", devicePath).Err(err).Msg("cannot stat device")
		return ""
	}

	// unix.Major/unix.Minor, not hand-rolled shifts: Linux dev_t is not the
	// old 16-bit packing, and masking the low byte truncates a minor over 255.
	sysPath := fmt.Sprintf("/sys/dev/char/%d:%d", unix.Major(stat.Rdev), unix.Minor(stat.Rdev))
	resolved, err := filepath.EvalSymlinks(sysPath)
	if err != nil {
		log.Debug().
			Str("path", devicePath).
			Str("sysPath", sysPath).
			Err(err).
			Msg("cannot resolve sysfs symlink")
		return ""
	}
	return resolved
}

// extractUSBTopology walks up a sysfs device path to find the USB topology.
// Returns the topology string (e.g., "1-2.3.1") or empty string if not found.
func extractUSBTopology(sysfsPath string) string {
	current := sysfsPath

	for current != "/" && current != "." && current != "" {
		base := filepath.Base(current)

		// Check if this looks like a USB topology path
		if usbTopologyPattern.MatchString(base) {
			return base
		}

		current = filepath.Dir(current)
	}

	return ""
}
