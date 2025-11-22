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

package testutils

import (
	"fmt"
	"time"

	"go.bug.st/serial"
)

// SerialPort defines the interface for serial port operations.
// This interface is used by serial-based readers for dependency injection and testing.
type SerialPort interface {
	Read(p []byte) (n int, err error)
	Close() error
	SetReadTimeout(t time.Duration) error
}

// SerialPortFactory creates a serial port connection.
// This factory pattern allows readers to be testable by injecting mock implementations.
type SerialPortFactory func(path string, mode *serial.Mode) (SerialPort, error)

// DefaultSerialPortFactory is the default factory that opens real serial ports.
// It wraps the go.bug.st/serial library for production use.
func DefaultSerialPortFactory(path string, mode *serial.Mode) (SerialPort, error) {
	port, err := serial.Open(path, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open serial port: %w", err)
	}
	return port, nil
}
