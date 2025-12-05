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

package pn532uart

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/testutils"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSerialPort is a mock implementation of SerialPort for testing.
type mockSerialPort struct {
	closed bool
	mu     syncutil.RWMutex // protects closed
}

func (m *mockSerialPort) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	return nil
}

func (m *mockSerialPort) IsClosed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.closed
}

// mockPN532Commander is a mock implementation of PN532Commander for testing.
type mockPN532Commander struct {
	listPassiveTargetFunc func(port SerialPort) (*Target, error)
	dataExchangeFunc      func(port SerialPort, data []byte) ([]byte, error)
}

func (m *mockPN532Commander) InListPassiveTarget(port SerialPort) (*Target, error) {
	if m.listPassiveTargetFunc != nil {
		return m.listPassiveTargetFunc(port)
	}
	return nil, nil //nolint:nilnil // nil target means no tag detected, which is valid behavior
}

func (m *mockPN532Commander) InDataExchange(port SerialPort, data []byte) ([]byte, error) {
	if m.dataExchangeFunc != nil {
		return m.dataExchangeFunc(port, data)
	}
	return []byte{}, nil
}

// TestOpen_ErrorCountExceedsMaxWithActiveToken tests the fix for issue #326.
// When error count exceeds maxErrors (5) and there's an active token,
// ReaderError should be set to true to prevent triggering on_remove hooks.
func TestOpen_ErrorCountExceedsMaxWithActiveToken(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockPort := &mockSerialPort{}
	callCount := 0

	// Connect function returns mock port
	reader.connectFn = func(_ string) (SerialPort, error) {
		return mockPort, nil
	}

	// First call succeeds with a token, subsequent calls fail to trigger error count
	reader.commander = &mockPN532Commander{
		listPassiveTargetFunc: func(_ SerialPort) (*Target, error) {
			callCount++
			if callCount == 1 {
				// First call: successful token detection
				return &Target{
					Type: tokens.TypeNTAG,
					UID:  "test-uid-123",
				}, nil
			}
			// Subsequent calls: errors to increment errCount
			return nil, assert.AnError
		},
		dataExchangeFunc: func(_ SerialPort, _ []byte) ([]byte, error) {
			// Return valid NDEF data for first token
			if callCount == 1 {
				return []byte{
					0x41, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				}, nil
			}
			return nil, assert.AnError
		},
	}

	devicePath := testutils.CreateTempDevicePath(t)
	device := config.ReadersConnect{
		Driver: "legacy_pn532_uart",
		Path:   devicePath,
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// First scan: token detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "test-uid-123", scan1.Token.UID)
	assert.False(t, scan1.ReaderError)

	// Wait for error count to exceed maxErrors (5) and ReaderError scan to be sent
	// This tests the fix for issue #326 - ReaderError should be true
	scan2 := testutils.AssertScanReceived(t, scanQueue, 5*time.Second)
	assert.Nil(t, scan2.Token, "token should be nil on reader error")
	assert.True(t, scan2.ReaderError, "ReaderError should be true to prevent on_remove execution")

	// Reader should have stopped polling
	time.Sleep(100 * time.Millisecond)
	assert.False(t, reader.Connected(), "reader should have stopped after max errors")
}

// TestOpen_ErrorCountExceedsMaxWithoutActiveToken verifies that when error count
// exceeds maxErrors but there's NO active token, no scan is sent (no ReaderError needed).
func TestOpen_ErrorCountExceedsMaxWithoutActiveToken(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockPort := &mockSerialPort{}

	// Connect function returns mock port
	reader.connectFn = func(_ string) (SerialPort, error) {
		return mockPort, nil
	}

	// All calls fail to trigger error count without any successful token detection
	reader.commander = &mockPN532Commander{
		listPassiveTargetFunc: func(_ SerialPort) (*Target, error) {
			return nil, assert.AnError
		},
	}

	devicePath := testutils.CreateTempDevicePath(t)
	device := config.ReadersConnect{
		Driver: "legacy_pn532_uart",
		Path:   devicePath,
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// Wait for error count to exceed maxErrors
	time.Sleep(3 * time.Second)

	// No scan should be sent since there was no active token
	testutils.AssertNoScan(t, scanQueue, 500*time.Millisecond)

	// Reader should have stopped polling
	assert.False(t, reader.Connected(), "reader should have stopped after max errors")
}

// TestOpen_TokenDetectionAndRemoval tests normal token detection and removal flow.
func TestOpen_TokenDetectionAndRemoval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockPort := &mockSerialPort{}
	callCount := 0
	maxZeroScans := 3

	// Connect function returns mock port
	reader.connectFn = func(_ string) (SerialPort, error) {
		return mockPort, nil
	}

	// Simulate: token detected → token present for a few scans → token removed
	reader.commander = &mockPN532Commander{
		listPassiveTargetFunc: func(_ SerialPort) (*Target, error) {
			callCount++
			// First 3 calls: token present
			if callCount <= 3 {
				return &Target{
					Type: tokens.TypeNTAG,
					UID:  "ntag-uid-456",
				}, nil
			}
			// Next calls: token removed (nil)
			return nil, nil //nolint:nilnil // nil target means no tag detected, which is valid behavior
		},
		dataExchangeFunc: func(_ SerialPort, _ []byte) ([]byte, error) {
			// Return empty NDEF data (will trigger empty block detection)
			return []byte{
				0x41, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			}, nil
		},
	}

	devicePath := testutils.CreateTempDevicePath(t)
	device := config.ReadersConnect{
		Driver: "legacy_pn532_uart",
		Path:   devicePath,
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// First scan: token detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "ntag-uid-456", scan1.Token.UID)
	assert.Equal(t, tokens.TypeNTAG, scan1.Token.Type)
	assert.False(t, scan1.ReaderError)

	// Wait for token removal (maxZeroScans = 3)
	// After 3 consecutive nil responses, token removal scan should be sent
	scan2 := testutils.AssertScanReceived(t, scanQueue, time.Duration(maxZeroScans+2)*300*time.Millisecond)
	assert.Nil(t, scan2.Token, "token should be nil on removal")
	assert.False(t, scan2.ReaderError, "ReaderError should be false for normal removal")
}

// TestOpen_MifareTokenRejected tests that Mifare tokens are rejected with a log message.
func TestOpen_MifareTokenRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockPort := &mockSerialPort{}
	callCount := 0

	// Connect function returns mock port
	reader.connectFn = func(_ string) (SerialPort, error) {
		return mockPort, nil
	}

	// Detect a Mifare token, then nil
	reader.commander = &mockPN532Commander{
		listPassiveTargetFunc: func(_ SerialPort) (*Target, error) {
			callCount++
			if callCount == 1 {
				// First call: Mifare token (should be rejected)
				return &Target{
					Type: tokens.TypeMifare,
					UID:  "mifare-uid-789",
				}, nil
			}
			// Subsequent calls: no token
			return nil, nil //nolint:nilnil // nil target means no tag detected, which is valid behavior
		},
	}

	devicePath := testutils.CreateTempDevicePath(t)
	device := config.ReadersConnect{
		Driver: "legacy_pn532_uart",
		Path:   devicePath,
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// No scan should be sent for Mifare token (it's rejected)
	testutils.AssertNoScan(t, scanQueue, 2*time.Second)
}

// TestOpen_DuplicateTokenIgnored tests that duplicate tokens are ignored.
func TestOpen_DuplicateTokenIgnored(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockPort := &mockSerialPort{}

	// Connect function returns mock port
	reader.connectFn = func(_ string) (SerialPort, error) {
		return mockPort, nil
	}

	// Always return the same token
	reader.commander = &mockPN532Commander{
		listPassiveTargetFunc: func(_ SerialPort) (*Target, error) {
			return &Target{
				Type: tokens.TypeNTAG,
				UID:  "same-uid-999",
			}, nil
		},
		dataExchangeFunc: func(_ SerialPort, _ []byte) ([]byte, error) {
			return []byte{
				0x41, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			}, nil
		},
	}

	devicePath := testutils.CreateTempDevicePath(t)
	device := config.ReadersConnect{
		Driver: "legacy_pn532_uart",
		Path:   devicePath,
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// First scan: token detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "same-uid-999", scan1.Token.UID)

	// No additional scans should be sent for the same token
	testutils.AssertNoScan(t, scanQueue, 2*time.Second)
}

// TestClose tests that Close() properly stops polling and closes the port.
func TestClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockPort := &mockSerialPort{}

	// Connect function returns mock port
	reader.connectFn = func(_ string) (SerialPort, error) {
		return mockPort, nil
	}

	reader.commander = &mockPN532Commander{
		listPassiveTargetFunc: func(_ SerialPort) (*Target, error) {
			return nil, nil //nolint:nilnil // nil target means no tag detected, which is valid behavior
		},
	}

	devicePath := testutils.CreateTempDevicePath(t)
	device := config.ReadersConnect{
		Driver: "legacy_pn532_uart",
		Path:   devicePath,
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// Verify reader is connected
	assert.True(t, reader.Connected())
	assert.True(t, reader.polling)

	// Close the reader
	err = reader.Close()
	require.NoError(t, err)

	// Verify reader is disconnected
	assert.False(t, reader.Connected())
	assert.True(t, mockPort.IsClosed(), "port should be closed")
}
