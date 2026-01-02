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
	"os"
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
	if devicePath == "" {
		return ""
	}

	info, err := os.Stat(devicePath)
	if err != nil {
		log.Debug().Str("path", devicePath).Err(err).Msg("cannot stat device")
		return ""
	}

	stat, ok := info.Sys().(*unix.Stat_t)
	if !ok {
		log.Debug().Str("path", devicePath).Msg("cannot get unix.Stat_t")
		return ""
	}

	major := (stat.Rdev >> 8) & 0xff
	minor := stat.Rdev & 0xff

	// Look up via /sys/dev/char/{major}:{minor} which symlinks to the
	// full device path in sysfs
	sysPath := fmt.Sprintf("/sys/dev/char/%d:%d", major, minor)
	resolved, err := filepath.EvalSymlinks(sysPath)
	if err != nil {
		log.Debug().
			Str("path", devicePath).
			Str("sysPath", sysPath).
			Err(err).
			Msg("cannot resolve sysfs symlink")
		return ""
	}

	// Walk up the path looking for a USB topology directory
	// The path looks like: /sys/devices/pci.../usb1/1-2/1-2.3/1-2.3:1.0/tty/ttyUSB0
	// We want to extract "1-2.3" (the USB topology)
	return extractUSBTopology(resolved)
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
