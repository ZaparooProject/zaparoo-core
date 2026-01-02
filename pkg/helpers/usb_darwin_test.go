//go:build darwin

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

func TestFormatLocationID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		locationID uint32
		expected   string
	}{
		{
			name:       "single port on bus 20",
			locationID: 0x14200000, // Bus 0x14 (20), Port 2
			expected:   "20-2",
		},
		{
			name:       "two ports on bus 20",
			locationID: 0x14230000, // Bus 0x14 (20), Port 2, Port 3
			expected:   "20-2.3",
		},
		{
			name:       "three ports deep",
			locationID: 0x14234000, // Bus 0x14 (20), Port 2, Port 3, Port 4
			expected:   "20-2.3.4",
		},
		{
			name:       "bus 1 single port",
			locationID: 0x01100000, // Bus 1, Port 1
			expected:   "1-1",
		},
		{
			name:       "high port numbers",
			locationID: 0x02ABC000, // Bus 2, Port 10, Port 11, Port 12
			expected:   "2-10.11.12",
		},
		{
			name:       "zero locationID",
			locationID: 0,
			expected:   "",
		},
		{
			name:       "bus only no ports (edge case)",
			locationID: 0x14000000, // Bus 0x14 (20), no ports
			expected:   "",
		},
		{
			name:       "full depth path (6 ports)",
			locationID: 0x01123456, // Bus 1, ports 1,2,3,4,5,6
			expected:   "1-1.2.3.4.5.6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := FormatLocationID(tt.locationID)
			if result != tt.expected {
				t.Errorf("FormatLocationID(0x%08X) = %q, want %q", tt.locationID, result, tt.expected)
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
