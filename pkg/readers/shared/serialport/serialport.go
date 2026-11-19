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

// Package serialport provides the production serial boundary shared by readers.
package serialport

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/usbserial"
	"go.bug.st/serial"
)

// SerialPort is the subset of serial operations used by scan readers.
type SerialPort interface {
	Read(p []byte) (n int, err error)
	Close() error
	SetReadTimeout(t time.Duration) error
}

// SerialPortFactory allows readers to inject a serial connection.
type SerialPortFactory func(path string, mode *serial.Mode) (SerialPort, error)

// DefaultSerialPortFactory opens a hardware serial connection.
func DefaultSerialPortFactory(path string, mode *serial.Mode) (SerialPort, error) {
	port, err := OpenPort(path, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open serial port: %w", err)
	}
	return port, nil
}

// usbSerialOpen and osSerialOpen are the two backends OpenPort routes
// between, indirected for tests.
var (
	usbSerialOpen = func(path string, mode *serial.Mode) (serial.Port, error) {
		return usbserial.Open(path, mode)
	}
	osSerialOpen = serial.Open
)

// OpenPort opens a serial port. A usbserial path ("usbserial:1-1.2") opens a
// CH34x or PL2303 adapter that has no kernel driver through the userspace
// USB driver; any other path is an OS serial device.
func OpenPort(path string, mode *serial.Mode) (serial.Port, error) {
	if usbserial.IsPath(path) {
		port, err := usbSerialOpen(path, mode)
		if err != nil {
			return nil, fmt.Errorf("open USB serial adapter: %w", err)
		}
		return port, nil
	}
	port, err := osSerialOpen(path, mode)
	if err != nil {
		return nil, fmt.Errorf("open serial device: %w", err)
	}
	return port, nil
}

// CheckExists reports whether the port at path is present. Windows COM port
// names are not files, so they are left for the open to check.
func CheckExists(path string) error {
	if usbserial.IsPath(path) {
		if err := usbserial.Exists(path); err != nil {
			return fmt.Errorf("USB serial adapter %s: %w", path, err)
		}
		return nil
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("failed to stat device path %s: %w", path, err)
	}
	return nil
}

// TopologyPath returns the USB port a serial port is plugged into, such as
// "1-2.3", or an empty string when it is not a USB device.
func TopologyPath(path string) string {
	if usbserial.IsPath(path) {
		return usbserial.Topology(path)
	}
	return helpers.GetUSBTopologyPath(path)
}
