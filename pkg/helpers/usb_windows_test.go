//go:build windows

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

func TestExtractWindowsUSBTopology(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "typical USB path with single port",
			path:     "PCIROOT(0)#PCI(1400)#USBROOT(0)#USB(1)",
			expected: "0-1",
		},
		{
			name:     "USB path with two ports",
			path:     "PCIROOT(0)#PCI(1400)#USBROOT(0)#USB(1)#USB(2)",
			expected: "0-1.2",
		},
		{
			name:     "USB path with three ports (through hub)",
			path:     "PCIROOT(0)#PCI(1400)#USBROOT(0)#USB(1)#USB(2)#USB(3)",
			expected: "0-1.2.3",
		},
		{
			name:     "USB path with different USBROOT",
			path:     "PCIROOT(0)#PCI(1D00)#USBROOT(1)#USB(4)#USB(2)",
			expected: "1-4.2",
		},
		{
			name:     "USB path with high port numbers",
			path:     "PCIROOT(0)#PCI(1400)#USBROOT(0)#USB(10)#USB(5)#USB(3)#USB(1)",
			expected: "0-10.5.3.1",
		},
		{
			name:     "no USB in path (non-USB device)",
			path:     "PCIROOT(0)#PCI(1F00)#COM(0)",
			expected: "",
		},
		{
			name:     "empty path",
			path:     "",
			expected: "",
		},
		{
			name:     "only USBROOT no ports",
			path:     "PCIROOT(0)#PCI(1400)#USBROOT(0)",
			expected: "",
		},
		{
			name:     "ACPI path (no USB)",
			path:     "ACPI(_SB_)#ACPI(PCI0)#ACPI(SERI)",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := extractWindowsUSBTopology(tt.path)
			if result != tt.expected {
				t.Errorf("extractWindowsUSBTopology(%q) = %q, want %q", tt.path, result, tt.expected)
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

	result := GetUSBTopologyPath("COM999")
	if result != "" {
		t.Errorf("GetUSBTopologyPath for nonexistent device = %q, want empty string", result)
	}
}

func TestUSBPortPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input       string
		captured    string
		shouldMatch bool
	}{
		{"USB(1)", "1", true},
		{"USB(10)", "10", true},
		{"USB(123)", "123", true},
		{"USBROOT(0)", "", false},
		{"PCI(1400)", "", false},
		{"USB()", "", false},
		{"USB(a)", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			matches := usbPortPattern.FindStringSubmatch(tt.input)
			if tt.shouldMatch {
				if len(matches) < 2 {
					t.Errorf("usbPortPattern should match %q", tt.input)
				} else if matches[1] != tt.captured {
					t.Errorf("usbPortPattern captured %q from %q, want %q", matches[1], tt.input, tt.captured)
				}
			} else {
				if len(matches) > 0 {
					t.Errorf("usbPortPattern should not match %q", tt.input)
				}
			}
		})
	}
}
