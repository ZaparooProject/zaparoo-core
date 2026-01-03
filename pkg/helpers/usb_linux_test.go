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
	"testing"
)

func TestExtractUSBTopology(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "typical USB serial path",
			path:     "/sys/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2.3/1-2.3:1.0/tty/ttyUSB0",
			expected: "1-2.3",
		},
		{
			name:     "single port USB",
			path:     "/sys/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/tty/ttyUSB0",
			expected: "1-2",
		},
		{
			name:     "deep USB hub path",
			path:     "/sys/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2.3/1-2.3.1/1-2.3.1.4/1-2.3.1.4:1.0/tty/ttyUSB0",
			expected: "1-2.3.1.4",
		},
		{
			name:     "USB2 bus path",
			path:     "/sys/devices/pci0000:00/0000:00:14.0/usb2/2-1/2-1.2/2-1.2:1.0/tty/ttyUSB0",
			expected: "2-1.2",
		},
		{
			name:     "no USB topology in path",
			path:     "/sys/devices/platform/serial8250/tty/ttyS0",
			expected: "",
		},
		{
			name:     "empty path",
			path:     "",
			expected: "",
		},
		{
			name:     "root path",
			path:     "/",
			expected: "",
		},
		{
			name:     "ACPI serial device",
			path:     "/sys/devices/LNXSYSTM:00/LNXSYBUS:00/PNP0A08:00/serial0/tty/ttyS0",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := extractUSBTopology(tt.path)
			if result != tt.expected {
				t.Errorf("extractUSBTopology(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestGetUSBTopologyPath_EmptyInput(t *testing.T) {
	t.Parallel()

	result := GetUSBTopologyPath("")
	if result != "" {
		t.Errorf("GetUSBTopologyPath(\"\") = %q, want empty string", result)
	}
}

func TestGetUSBTopologyPath_NonexistentDevice(t *testing.T) {
	t.Parallel()

	result := GetUSBTopologyPath("/dev/nonexistent_device_xyz123")
	if result != "" {
		t.Errorf("GetUSBTopologyPath for nonexistent device = %q, want empty string", result)
	}
}

func TestUSBTopologyPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		matches bool
	}{
		{"1-2", true},
		{"1-2.3", true},
		{"1-2.3.1", true},
		{"1-2.3.1.4", true},
		{"2-1.2", true},
		{"10-5.3.2.1", true},
		{"usb1", false},
		{"ttyUSB0", false},
		{"pci0000:00", false},
		{"1-2:1.0", false}, // interface, not topology
		{"", false},
		{"abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			result := usbTopologyPattern.MatchString(tt.input)
			if result != tt.matches {
				t.Errorf("usbTopologyPattern.MatchString(%q) = %v, want %v", tt.input, result, tt.matches)
			}
		})
	}
}
