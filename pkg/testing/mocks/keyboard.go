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

package mocks

// MockKeyboard implements uinput.Keyboard for testing.
// It records all key presses for verification in tests.
type MockKeyboard struct {
	KeyDownCalls []int
	KeyUpCalls   []int
}

// NewMockKeyboard creates a new MockKeyboard instance.
func NewMockKeyboard() *MockKeyboard {
	return &MockKeyboard{}
}

// KeyPress records a key press (down + up).
func (m *MockKeyboard) KeyPress(key int) error {
	m.KeyDownCalls = append(m.KeyDownCalls, key)
	m.KeyUpCalls = append(m.KeyUpCalls, key)
	return nil
}

// KeyDown records a key down event.
func (m *MockKeyboard) KeyDown(key int) error {
	m.KeyDownCalls = append(m.KeyDownCalls, key)
	return nil
}

// KeyUp records a key up event.
func (m *MockKeyboard) KeyUp(key int) error {
	m.KeyUpCalls = append(m.KeyUpCalls, key)
	return nil
}

// FetchSyspath returns a mock syspath.
func (*MockKeyboard) FetchSyspath() (string, error) {
	return "/sys/devices/virtual/input/mock", nil
}

// Close is a no-op for the mock.
func (*MockKeyboard) Close() error {
	return nil
}

// Reset clears all recorded calls.
func (m *MockKeyboard) Reset() {
	m.KeyDownCalls = nil
	m.KeyUpCalls = nil
}
