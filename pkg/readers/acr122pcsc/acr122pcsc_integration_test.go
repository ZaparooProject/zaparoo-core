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

package acr122pcsc

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/testutils"
	"github.com/ebfe/scard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockScardCard is a mock implementation of ScardCard for testing.
type mockScardCard struct {
	statusFunc     func() (*scard.CardStatus, error)
	transmitFunc   func([]byte) ([]byte, error)
	disconnectFunc func(scard.Disposition) error
	transmitCount  int
}

func (m *mockScardCard) Status() (*scard.CardStatus, error) {
	if m.statusFunc != nil {
		return m.statusFunc()
	}
	return &scard.CardStatus{
		Atr: []byte{0x3B, 0x8F, 0x80, 0x01, 0x80, 0x4F, 0x0C, 0xA0},
	}, nil
}

func (m *mockScardCard) Transmit(data []byte) ([]byte, error) {
	m.transmitCount++
	if m.transmitFunc != nil {
		return m.transmitFunc(data)
	}
	return []byte{}, nil
}

func (m *mockScardCard) Disconnect(d scard.Disposition) error {
	if m.disconnectFunc != nil {
		return m.disconnectFunc(d)
	}
	return nil
}

// mockScardContext is a mock implementation of ScardContext for testing.
type mockScardContext struct {
	listReadersFunc      func() ([]string, error)
	getStatusChangeFunc  func([]scard.ReaderState, time.Duration) error
	connectFunc          func(string, scard.ShareMode, scard.Protocol) (ScardCard, error)
	releaseFunc          func() error
	listReadersCallCount int
	releaseCalled        bool
}

func (m *mockScardContext) ListReaders() ([]string, error) {
	m.listReadersCallCount++
	if m.listReadersFunc != nil {
		return m.listReadersFunc()
	}
	return []string{"ACS ACR122U PICC Interface"}, nil
}

func (m *mockScardContext) GetStatusChange(rs []scard.ReaderState, timeout time.Duration) error {
	if m.getStatusChangeFunc != nil {
		return m.getStatusChangeFunc(rs, timeout)
	}
	return nil
}

func (m *mockScardContext) Connect(
	reader string,
	mode scard.ShareMode,
	proto scard.Protocol,
) (ScardCard, error) {
	if m.connectFunc != nil {
		return m.connectFunc(reader, mode, proto)
	}
	return &mockScardCard{}, nil
}

func (m *mockScardContext) Release() error {
	m.releaseCalled = true
	if m.releaseFunc != nil {
		return m.releaseFunc()
	}
	return nil
}

// TestOpen_ReaderListingErrorAfterTagRemoval tests error handling after normal tag removal.
// This test verifies that the reader stops gracefully when ListReaders fails.
func TestOpen_ReaderListingErrorAfterTagRemoval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewAcr122Pcsc(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockCtx := &mockScardContext{}
	cardRemoved := false

	// ListReaders sequence:
	// Call 1: Open() initial check - success
	// Call 2: First iteration of polling loop - success (for tag detection)
	// Call 3: After card removed, next iteration - ERROR
	mockCtx.listReadersFunc = func() ([]string, error) {
		if mockCtx.listReadersCallCount <= 2 {
			return []string{"ACS ACR122U PICC Interface"}, nil
		}
		// After card removed - return error
		return nil, assert.AnError
	}

	// GetStatusChange: card present initially, then removed quickly
	mockCtx.getStatusChangeFunc = func(rs []scard.ReaderState, _ time.Duration) error {
		// First call: detect card
		// Subsequent calls in card-removal loop: indicate removed immediately
		if rs[0].CurrentState == scard.StatePresent && !cardRemoved {
			// In the card-removal waiting loop
			cardRemoved = true
			rs[0].EventState = 0 // Card removed
			return nil
		}

		rs[0].EventState = scard.StatePresent
		return nil
	}

	// Connect: Return mock card with UID data
	mockCtx.connectFunc = func(_ string, _ scard.ShareMode, _ scard.Protocol) (ScardCard, error) {
		mockCard := &mockScardCard{
			transmitFunc: func(data []byte) ([]byte, error) {
				// Get UID command
				if len(data) == 5 && data[0] == 0xFF && data[1] == 0xCA {
					return []byte{0x04, 0xAA, 0xBB, 0xCC, 0x90, 0x00}, nil
				}
				// NDEF reads: Return empty data (triggers stop)
				return []byte{0x00, 0x00, 0x00, 0x00, 0x90, 0x00}, nil
			},
		}
		return mockCard, nil
	}

	// Inject mock context
	reader.contextFactory = func() (ScardContext, error) {
		return mockCtx, nil
	}

	device := config.ReadersConnect{
		Driver: "acr122_pcsc",
		Path:   "ACS ACR122U PICC Interface",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// First scan: token detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "04aabbcc", scan1.Token.UID)
	assert.False(t, scan1.ReaderError)

	// Second scan: Normal card removal
	scan2 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.Nil(t, scan2.Token)
	assert.False(t, scan2.ReaderError, "normal card removal should not have ReaderError")

	// Verify polling stopped after ListReaders error
	time.Sleep(500 * time.Millisecond)
	assert.False(t, reader.Connected(), "reader should have stopped after ListReaders error")
}

// TestOpen_ReaderDisconnectedAfterTagRemoval tests reader disconnect after normal tag removal.
// This test verifies that the reader stops gracefully when it's no longer in the reader list.
func TestOpen_ReaderDisconnectedAfterTagRemoval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewAcr122Pcsc(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockCtx := &mockScardContext{}
	cardRemoved := false

	// ListReaders sequence:
	// Call 1: Open() initial check - success
	// Call 2: First iteration of polling loop - success (for tag detection)
	// Call 3+: After card removed - reader disconnected (empty list)
	mockCtx.listReadersFunc = func() ([]string, error) {
		if mockCtx.listReadersCallCount <= 2 {
			return []string{"ACS ACR122U PICC Interface"}, nil
		}
		// Reader disconnected (empty list, no error)
		return []string{}, nil
	}

	// GetStatusChange: card present initially, then removed quickly
	mockCtx.getStatusChangeFunc = func(rs []scard.ReaderState, _ time.Duration) error {
		// First call: detect card
		// Subsequent calls in card-removal loop: indicate removed immediately
		if rs[0].CurrentState == scard.StatePresent && !cardRemoved {
			// In the card-removal waiting loop
			cardRemoved = true
			rs[0].EventState = 0 // Card removed
			return nil
		}

		rs[0].EventState = scard.StatePresent
		return nil
	}

	// Connect: Return mock card with UID
	mockCtx.connectFunc = func(_ string, _ scard.ShareMode, _ scard.Protocol) (ScardCard, error) {
		mockCard := &mockScardCard{
			transmitFunc: func(data []byte) ([]byte, error) {
				if len(data) == 5 && data[0] == 0xFF && data[1] == 0xCA {
					return []byte{0x05, 0xDD, 0xEE, 0xFF, 0x90, 0x00}, nil
				}
				return []byte{0x00, 0x00, 0x00, 0x00, 0x90, 0x00}, nil
			},
		}
		return mockCard, nil
	}

	reader.contextFactory = func() (ScardContext, error) {
		return mockCtx, nil
	}

	device := config.ReadersConnect{
		Driver: "acr122_pcsc",
		Path:   "ACS ACR122U PICC Interface",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// First scan: token detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "05ddeeff", scan1.Token.UID)
	assert.False(t, scan1.ReaderError)

	// Second scan: Normal tag removal
	scan2 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.Nil(t, scan2.Token)
	assert.False(t, scan2.ReaderError, "normal tag removal should not have ReaderError")

	// Verify reader stopped after disconnect was detected
	time.Sleep(500 * time.Millisecond)
	assert.False(t, reader.Connected())
}

// TestOpen_NormalTagDetectionAndRemoval tests normal tag lifecycle without errors.
func TestOpen_NormalTagDetectionAndRemoval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewAcr122Pcsc(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockCtx := &mockScardContext{}
	tagPresent := true
	statusChangeCount := 0

	mockCtx.getStatusChangeFunc = func(rs []scard.ReaderState, _ time.Duration) error {
		statusChangeCount++

		// First call: detect tag
		if statusChangeCount == 1 {
			rs[0].EventState = scard.StatePresent
			return nil
		}

		// Subsequent calls: tag removed after a few checks
		if statusChangeCount > 4 {
			tagPresent = false
		}

		if tagPresent {
			rs[0].EventState = scard.StatePresent
		} else {
			rs[0].EventState = 0
		}
		return nil
	}

	mockCtx.connectFunc = func(_ string, _ scard.ShareMode, _ scard.Protocol) (ScardCard, error) {
		mockCard := &mockScardCard{
			transmitFunc: func(data []byte) ([]byte, error) {
				if len(data) == 5 && data[0] == 0xFF && data[1] == 0xCA {
					return []byte{0x01, 0x23, 0x45, 0x67, 0x90, 0x00}, nil
				}
				return []byte{0x00, 0x00, 0x00, 0x00, 0x90, 0x00}, nil
			},
		}
		return mockCard, nil
	}

	reader.contextFactory = func() (ScardContext, error) {
		return mockCtx, nil
	}

	device := config.ReadersConnect{
		Driver: "acr122_pcsc",
		Path:   "ACS ACR122U PICC Interface",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// First scan: tag detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "01234567", scan1.Token.UID)
	assert.False(t, scan1.ReaderError)

	// Second scan: tag removed (normal removal)
	scan2 := testutils.AssertScanReceived(t, scanQueue, 3*time.Second)
	assert.Nil(t, scan2.Token)
	assert.False(t, scan2.ReaderError, "ReaderError should be false for normal tag removal")
}

// TestOpen_ConnectFails tests when Connect() fails (no scan should be sent).
func TestOpen_ConnectFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewAcr122Pcsc(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockCtx := &mockScardContext{}
	var connectAttempts atomic.Int32

	mockCtx.getStatusChangeFunc = func(rs []scard.ReaderState, _ time.Duration) error {
		// Always indicate card present to trigger Connect attempts
		rs[0].EventState = scard.StatePresent
		return nil
	}

	mockCtx.connectFunc = func(_ string, _ scard.ShareMode, _ scard.Protocol) (ScardCard, error) {
		attempts := connectAttempts.Add(1)
		if attempts >= 3 {
			// Stop polling after a few failed attempts
			_ = reader.Close()
		}
		return nil, assert.AnError
	}

	reader.contextFactory = func() (ScardContext, error) {
		return mockCtx, nil
	}

	device := config.ReadersConnect{
		Driver: "acr122_pcsc",
		Path:   "ACS ACR122U PICC Interface",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// No scan should be sent since Connect fails
	testutils.AssertNoScan(t, scanQueue, 2*time.Second)
}

// TestOpen_NDEFDataReading tests NDEF data reading from tag.
func TestOpen_NDEFDataReading(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewAcr122Pcsc(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockCtx := &mockScardContext{}
	tagDetected := false

	mockCtx.getStatusChangeFunc = func(rs []scard.ReaderState, _ time.Duration) error {
		if !tagDetected {
			rs[0].EventState = scard.StatePresent
		} else {
			rs[0].EventState = 0
		}
		return nil
	}

	mockCtx.connectFunc = func(_ string, _ scard.ShareMode, _ scard.Protocol) (ScardCard, error) {
		tagDetected = true
		mockCard := &mockScardCard{
			transmitFunc: func(data []byte) ([]byte, error) {
				// Get UID
				if len(data) == 5 && data[0] == 0xFF && data[1] == 0xCA {
					return []byte{0xAA, 0xBB, 0xCC, 0xDD, 0x90, 0x00}, nil
				}

				// Read NDEF blocks - return NDEF text record "test"
				// NDEF format: TLV(03) + Length + Record Header + Type Length + Payload Length + Type + Payload
				// Payload = Status(1) + Language(2) + Text(4) = 7 bytes
				// NDEF Record = Header(1) + TypeLen(1) + PayloadLen(1) + Type(1) + Payload(7) = 11 bytes
				if len(data) == 5 && data[0] == 0xFF && data[1] == 0xB0 {
					blockNum := int(data[3])
					if blockNum == 0 {
						// TLV start (03), TLV length (0B=11), record header (D1), type length (01)
						return []byte{0x03, 0x0B, 0xD1, 0x01, 0x90, 0x00}, nil
					}
					if blockNum == 1 {
						// Payload length (07), type (54='T'), status (02), lang code start (65='e')
						return []byte{0x07, 0x54, 0x02, 0x65, 0x90, 0x00}, nil
					}
					if blockNum == 2 {
						// Language code end (6E='n') + text: 'tes'
						return []byte{0x6E, 0x74, 0x65, 0x73, 0x90, 0x00}, nil
					}
					if blockNum == 3 {
						// Text end: 't' + TLV terminator (FE)
						return []byte{0x74, 0xFE, 0x00, 0x00, 0x90, 0x00}, nil
					}
					// End of data
					return []byte{0x00, 0x00, 0x00, 0x00, 0x90, 0x00}, nil
				}

				return []byte{0x90, 0x00}, nil
			},
		}
		return mockCard, nil
	}

	reader.contextFactory = func() (ScardContext, error) {
		return mockCtx, nil
	}

	device := config.ReadersConnect{
		Driver: "acr122_pcsc",
		Path:   "ACS ACR122U PICC Interface",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// Scan: token with NDEF text
	scan := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan.Token)
	assert.Equal(t, "aabbccdd", scan.Token.UID)
	assert.Equal(t, "test", scan.Token.Text, "NDEF text should be parsed correctly")
}

// TestClose tests that Close() properly stops polling and releases context.
func TestClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewAcr122Pcsc(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockCtx := &mockScardContext{}
	mockCtx.getStatusChangeFunc = func(rs []scard.ReaderState, _ time.Duration) error {
		rs[0].EventState = 0 // No card
		return nil
	}

	reader.contextFactory = func() (ScardContext, error) {
		return mockCtx, nil
	}

	device := config.ReadersConnect{
		Driver: "acr122_pcsc",
		Path:   "ACS ACR122U PICC Interface",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// Verify reader is connected
	assert.True(t, reader.Connected())

	// Close the reader
	err = reader.Close()
	require.NoError(t, err)

	// Verify reader is disconnected and context released
	assert.False(t, reader.polling)
	assert.False(t, reader.Connected())
	assert.True(t, mockCtx.releaseCalled, "context should be released")
}
