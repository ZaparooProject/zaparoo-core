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

package helpers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsUSBCDCDeviceName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		goos string
		path string
		want bool
	}{
		{name: "linux CDC ACM node", goos: "linux", path: "/dev/ttyACM0", want: true},
		{name: "linux USB-UART bridge", goos: "linux", path: "/dev/ttyUSB0", want: false},
		{name: "linux unrelated tty", goos: "linux", path: "/dev/ttyS0", want: false},
		// The ESP32-S3 has a native USB peripheral, so macOS names it
		// tty.usbmodem. Matching only tty.usbserial would never find it.
		{name: "darwin native USB device", goos: "darwin", path: "/dev/tty.usbmodem14201", want: true},
		{name: "darwin USB-UART bridge", goos: "darwin", path: "/dev/tty.usbserial-1420", want: false},
		{name: "windows COM port", goos: "windows", path: "COM3", want: true},
		{name: "windows non-COM", goos: "windows", path: "LPT1", want: false},
		{name: "unknown platform accepts anything", goos: "plan9", path: "/dev/eia0", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsUSBCDCDeviceName(tc.goos, tc.path))
		})
	}
}
