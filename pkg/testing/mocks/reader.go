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

package mocks

import (
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/mock"
)

// MockReader is a mock implementation of the Reader interface using testify/mock
type MockReader struct {
	mock.Mock
}

// Metadata returns static configuration for this driver
func (m *MockReader) Metadata() readers.DriverMetadata {
	args := m.Called()
	if metadata, ok := args.Get(0).(readers.DriverMetadata); ok {
		return metadata
	}
	return readers.DriverMetadata{}
}

// IDs returns the device string prefixes supported by this reader
func (m *MockReader) IDs() []string {
	args := m.Called()
	if ids, ok := args.Get(0).([]string); ok {
		return ids
	}
	return []string{}
}

// Open any necessary connections to the device and start polling
func (m *MockReader) Open(readerConfig config.ReadersConnect, scanChan chan<- readers.Scan) error {
	args := m.Called(readerConfig, scanChan)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// Close any open connections to the device and stop polling
func (m *MockReader) Close() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// Detect attempts to search for a connected device and returns the device connection string
func (m *MockReader) Detect(devices []string) string {
	args := m.Called(devices)
	return args.String(0)
}

// Device returns the device connection string
func (m *MockReader) Device() string {
	args := m.Called()
	return args.String(0)
}

// Connected returns true if the device is connected and active
func (m *MockReader) Connected() bool {
	args := m.Called()
	return args.Bool(0)
}

// Info returns a string with information about the connected device
func (m *MockReader) Info() string {
	args := m.Called()
	return args.String(0)
}

// Write sends a string to the device to be written to a token
func (m *MockReader) Write(data string) (*tokens.Token, error) {
	args := m.Called(data)
	if token, ok := args.Get(0).(*tokens.Token); ok {
		if err := args.Error(1); err != nil {
			return token, fmt.Errorf("mock operation failed: %w", err)
		}
		return token, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, errors.New("mock operation failed: no token provided")
}

// CancelWrite sends a request to cancel an active write request
func (m *MockReader) CancelWrite() {
	m.Called()
}

// Capabilities returns the list of capabilities supported by this reader
func (m *MockReader) Capabilities() []readers.Capability {
	args := m.Called()
	if capabilities, ok := args.Get(0).([]readers.Capability); ok {
		return capabilities
	}
	return []readers.Capability{}
}

// OnMediaChange is called when the active media changes
func (m *MockReader) OnMediaChange(media *models.ActiveMedia) error {
	args := m.Called(media)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// Helper methods for testing

// SimulateTokenScan sends a token scan to the provided channel (for testing Open method)
func (*MockReader) SimulateTokenScan(scanChan chan<- readers.Scan, token *tokens.Token, source string) {
	scanChan <- readers.Scan{
		Error:  nil,
		Token:  token,
		Source: source,
	}
}

// SimulateError sends an error scan to the provided channel (for testing Open method)
func (*MockReader) SimulateError(scanChan chan<- readers.Scan, err error, source string) {
	scanChan <- readers.Scan{
		Error:  err,
		Token:  nil,
		Source: source,
	}
}

// NewMockReader creates a new MockReader instance
func NewMockReader() *MockReader {
	m := &MockReader{}
	// Provide a safe optional default for Close() since it may or may not be called
	// depending on error conditions, defer statements, or cleanup patterns.
	m.On("Close").Return(nil).Maybe()
	return m
}

// SetupBasicMock configures the mock with typical default values for basic operations
func (m *MockReader) SetupBasicMock() {
	m.On("Metadata").Return(readers.DriverMetadata{
		ID:                "mock-reader",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
		Description:       "Mock Reader for Testing",
	})
	m.On("IDs").Return([]string{"mock:", "test:"})
	m.On("Connected").Return(true)
	m.On("Device").Return("mock://test-device")
	m.On("Info").Return("Mock Reader Test Device")
	m.On("Capabilities").Return([]readers.Capability{readers.CapabilityWrite})
}
