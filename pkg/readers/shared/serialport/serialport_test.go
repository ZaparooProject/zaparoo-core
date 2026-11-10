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

//nolint:paralleltest // these tests swap the package's open functions
package serialport

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/usbserial"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.bug.st/serial"
)

func swapOpeners(t *testing.T, usb, osOpen func(string, *serial.Mode) (serial.Port, error)) {
	t.Helper()
	origUSB, origOS := usbSerialOpen, osSerialOpen
	t.Cleanup(func() { usbSerialOpen, osSerialOpen = origUSB, origOS })
	usbSerialOpen, osSerialOpen = usb, osOpen
}

func TestOpenPortRouting(t *testing.T) {
	var usbPaths, osPaths []string
	errUSB := errors.New("usb open")
	errOS := errors.New("os open")
	swapOpeners(t,
		func(path string, _ *serial.Mode) (serial.Port, error) {
			usbPaths = append(usbPaths, path)
			return nil, errUSB
		},
		func(path string, _ *serial.Mode) (serial.Port, error) {
			osPaths = append(osPaths, path)
			return nil, errOS
		},
	)

	_, err := OpenPort("usbserial:1-2", &serial.Mode{})
	require.ErrorIs(t, err, errUSB)
	_, err = DefaultSerialPortFactory("usbserial:1-3.1", &serial.Mode{})
	require.ErrorIs(t, err, errUSB)
	_, err = OpenPort("/dev/ttyUSB0", &serial.Mode{})
	require.ErrorIs(t, err, errOS)
	_, err = DefaultSerialPortFactory("COM3", &serial.Mode{})
	require.ErrorIs(t, err, errOS)

	assert.Equal(t, []string{"usbserial:1-2", "usbserial:1-3.1"}, usbPaths)
	assert.Equal(t, []string{"/dev/ttyUSB0", "COM3"}, osPaths)
}

func TestDefaultUSBSerialOpen(t *testing.T) {
	// The real opener refuses a path outside the sysfs device names before
	// touching any device.
	_, err := OpenPort("usbserial:../../etc", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, usbserial.ErrBadPath) || errors.Is(err, usbserial.ErrUnsupported))
}

func TestCheckExists(t *testing.T) {
	err := CheckExists("usbserial:../../etc")
	require.Error(t, err)

	if runtime.GOOS == "windows" {
		require.NoError(t, CheckExists("COM3"))
		return
	}
	dev := filepath.Join(t.TempDir(), "ttyUSB0")
	require.NoError(t, os.WriteFile(dev, nil, 0o600))
	require.NoError(t, CheckExists(dev))
	require.ErrorContains(t, CheckExists(filepath.Join(t.TempDir(), "missing")), "failed to stat device path")
}

func TestTopologyPath(t *testing.T) {
	assert.Equal(t, "1-2.3", TopologyPath("usbserial:1-2.3"))
	assert.Empty(t, TopologyPath("usbserial:bad"))
}
