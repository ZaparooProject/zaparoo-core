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
	"time"

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
	port, err := serial.Open(path, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open serial port: %w", err)
	}
	return port, nil
}
