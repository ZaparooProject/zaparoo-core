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

package testutils

import (
	"errors"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// MockSerialPort is a mock implementation of serial port for testing.
// It implements the common serial port interface used by readers.
type MockSerialPort struct {
	ReadError  error
	CloseError error
	TimeoutErr error
	ReadFunc   func(p []byte) (n int, err error)
	ReadData   []byte
	ReadIndex  int
	Closed     bool
	mu         syncutil.RWMutex // protects Closed, CloseError
}

// NewMockSerialPort creates a new mock serial port for testing.
func NewMockSerialPort() *MockSerialPort {
	return &MockSerialPort{}
}

// Read implements the Read method for serial ports.
// It supports custom read functions, error injection, and buffered data reading.
func (m *MockSerialPort) Read(p []byte) (n int, err error) {
	m.mu.RLock()
	closed := m.Closed
	m.mu.RUnlock()

	if closed {
		return 0, errors.New("port closed")
	}

	// Use custom read function if provided
	if m.ReadFunc != nil {
		return m.ReadFunc(p)
	}

	// Return error if set
	if m.ReadError != nil {
		return 0, m.ReadError
	}

	// Return data from buffer
	if m.ReadIndex >= len(m.ReadData) {
		// Simulate blocking read with small delay
		time.Sleep(10 * time.Millisecond)
		return 0, nil
	}

	// Copy available data
	n = copy(p, m.ReadData[m.ReadIndex:])
	m.ReadIndex += n
	return n, nil
}

// Close implements the Close method for serial ports.
func (m *MockSerialPort) Close() error {
	m.mu.Lock()
	m.Closed = true
	closeError := m.CloseError
	m.mu.Unlock()
	return closeError
}

// SetReadTimeout implements the SetReadTimeout method for serial ports.
func (m *MockSerialPort) SetReadTimeout(_ time.Duration) error {
	return m.TimeoutErr
}

// IsClosed returns true if the port has been closed (thread-safe).
func (m *MockSerialPort) IsClosed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Closed
}
