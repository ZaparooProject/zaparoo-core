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

package pn532

import (
	"context"
	"testing"
	"time"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/ZaparooProject/go-pn532/polling"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/testutils"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransport is a mock implementation of pn532.Transport for testing.
type mockTransport struct{}

func (*mockTransport) Close() error {
	return nil
}

func (*mockTransport) IsConnected() bool {
	return true
}

func (*mockTransport) SendCommand(_ byte, _ []byte) ([]byte, error) {
	return []byte{}, nil
}

func (*mockTransport) SendCommandWithContext(_ context.Context, _ byte, _ []byte) ([]byte, error) {
	return []byte{}, nil
}

func (*mockTransport) SetTimeout(_ time.Duration) error {
	return nil
}

func (*mockTransport) Type() pn532.TransportType {
	return pn532.TransportUART
}

// mockPN532Device is a mock implementation of PN532Device for testing.
type mockPN532Device struct {
	initErr       error
	setTimeoutErr error
	closeErr      error
	timeoutSet    time.Duration
	initCalled    bool
	closeCalled   bool
}

func (m *mockPN532Device) Init() error {
	m.initCalled = true
	return m.initErr
}

func (m *mockPN532Device) SetTimeout(timeout time.Duration) error {
	m.timeoutSet = timeout
	return m.setTimeoutErr
}

func (m *mockPN532Device) Close() error {
	m.closeCalled = true
	return m.closeErr
}

// mockTag is a mock implementation of pn532.Tag for testing.
type mockTag struct {
	writeNDEFErr    error
	lastNDEFMessage *pn532.NDEFMessage
	uid             string
	tagType         pn532.TagType
	writeNDEFCalled bool
}

func (m *mockTag) UID() string {
	return m.uid
}

func (*mockTag) UIDBytes() []byte {
	return []byte{}
}

func (m *mockTag) Type() pn532.TagType {
	return m.tagType
}

func (*mockTag) ReadBlock(_ uint8) ([]byte, error) {
	return nil, nil
}

func (*mockTag) WriteBlock(_ uint8, _ []byte) error {
	return nil
}

func (*mockTag) ReadNDEF() (*pn532.NDEFMessage, error) {
	return nil, assert.AnError
}

func (m *mockTag) WriteNDEF(message *pn532.NDEFMessage) error {
	m.writeNDEFCalled = true
	m.lastNDEFMessage = message
	return m.writeNDEFErr
}

func (m *mockTag) WriteNDEFWithContext(_ context.Context, message *pn532.NDEFMessage) error {
	m.writeNDEFCalled = true
	m.lastNDEFMessage = message
	return m.writeNDEFErr
}

func (*mockTag) ReadText() (string, error) {
	return "", nil
}

func (*mockTag) WriteText(_ string) error {
	return nil
}

func (*mockTag) WriteTextWithContext(_ context.Context, _ string) error {
	return nil
}

func (*mockTag) DebugInfo() string {
	return "mockTag"
}

func (*mockTag) Summary() string {
	return "mockTag summary"
}

// mockPollingSession is a mock implementation of PollingSession for testing.
type mockPollingSession struct {
	startFunc          func(ctx context.Context) error
	closeFunc          func() error
	onCardDetected     func(*pn532.DetectedTag) error
	onCardRemoved      func()
	onCardChanged      func(*pn532.DetectedTag) error
	mockTag            pn532.Tag
	closeCalled        bool
	setCallbacksCalled bool
}

func (m *mockPollingSession) Start(ctx context.Context) error {
	if m.startFunc != nil {
		return m.startFunc(ctx)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (m *mockPollingSession) Close() error {
	m.closeCalled = true
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func (m *mockPollingSession) SetOnCardDetected(callback func(*pn532.DetectedTag) error) {
	m.onCardDetected = callback
	m.setCallbacksCalled = true
}

func (m *mockPollingSession) SetOnCardRemoved(callback func()) {
	m.onCardRemoved = callback
}

func (m *mockPollingSession) SetOnCardChanged(callback func(*pn532.DetectedTag) error) {
	m.onCardChanged = callback
}

func (m *mockPollingSession) WriteToNextTag(
	_ context.Context,
	writeCtx context.Context,
	_ time.Duration,
	writeFunc func(context.Context, pn532.Tag) error,
) error {
	// If a mock tag is provided, invoke the callback
	if m.mockTag != nil {
		return writeFunc(writeCtx, m.mockTag)
	}
	return nil
}

// TestOpen_SessionErrorWithActiveToken tests the fix for issue #326.
// When session.Start() returns an error (not context.Canceled) and there's an active token,
// ReaderError should be set to true to prevent triggering on_remove hooks.
func TestOpen_SessionErrorWithActiveToken(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockDevice := &mockPN532Device{}
	mockSession := &mockPollingSession{}

	// Track whether we've detected a tag
	tagDetected := false

	// Session start function:
	// 1. Trigger onCardDetected callback to set active token
	// 2. Return error to simulate session failure
	mockSession.startFunc = func(_ context.Context) error {
		if mockSession.onCardDetected != nil && !tagDetected {
			tagDetected = true
			// Simulate tag detection
			tag := &pn532.DetectedTag{
				Type:       pn532.TagTypeNTAG,
				UID:        "test-uid-session-error",
				TargetData: []byte{0x01, 0x02, 0x03},
			}
			_ = mockSession.onCardDetected(tag)
		}
		// Wait a bit then return error (not context.Canceled)
		time.Sleep(100 * time.Millisecond)
		return assert.AnError
	}

	// Transport factory returns mock transport
	reader.transportFactory = func(_ detection.DeviceInfo) (pn532.Transport, error) {
		return &mockTransport{}, nil
	}

	// Device factory returns mock device
	reader.deviceFactory = func(_ pn532.Transport) (PN532Device, error) {
		return mockDevice, nil
	}

	// Session factory returns mock session
	reader.sessionFactory = func(_ PN532Device, _ *polling.Config) PollingSession {
		return mockSession
	}

	device := config.ReadersConnect{
		Driver: "pn532",
		Path:   "/dev/test",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// First scan: token detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "test-uid-session-error", scan1.Token.UID)
	assert.False(t, scan1.ReaderError)

	// Second scan: session error with active token
	// This tests the fix for issue #326 - ReaderError should be true
	scan2 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.Nil(t, scan2.Token, "token should be nil on reader error")
	assert.True(t, scan2.ReaderError, "ReaderError should be true to prevent on_remove execution")

	// Verify device and session were initialized correctly
	assert.True(t, mockDevice.initCalled, "device Init should be called")
	assert.Equal(t, deviceTimeout, mockDevice.timeoutSet)
	assert.True(t, mockSession.setCallbacksCalled, "session callbacks should be set")
}

// TestOpen_SessionErrorWithoutActiveToken verifies that when session errors
// but there's NO active token, no ReaderError scan is sent.
func TestOpen_SessionErrorWithoutActiveToken(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockDevice := &mockPN532Device{}
	mockSession := &mockPollingSession{}

	// Session start function returns error immediately (no tag detected)
	mockSession.startFunc = func(_ context.Context) error {
		time.Sleep(100 * time.Millisecond)
		return assert.AnError
	}

	// Transport factory returns mock transport
	reader.transportFactory = func(_ detection.DeviceInfo) (pn532.Transport, error) {
		return &mockTransport{}, nil
	}

	// Device factory returns mock device
	reader.deviceFactory = func(_ pn532.Transport) (PN532Device, error) {
		return mockDevice, nil
	}

	// Session factory returns mock session
	reader.sessionFactory = func(_ PN532Device, _ *polling.Config) PollingSession {
		return mockSession
	}

	device := config.ReadersConnect{
		Driver: "pn532",
		Path:   "/dev/test",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// Wait for session error
	time.Sleep(500 * time.Millisecond)

	// No scan should be sent since there was no active token
	testutils.AssertNoScan(t, scanQueue, 500*time.Millisecond)
}

// TestOpen_SessionContextCanceled verifies that context.Canceled errors
// do NOT trigger ReaderError (normal shutdown).
func TestOpen_SessionContextCanceled(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockDevice := &mockPN532Device{}
	mockSession := &mockPollingSession{}

	// Session respects context cancellation
	mockSession.startFunc = func(ctx context.Context) error {
		<-ctx.Done()
		return context.Canceled
	}

	// Transport factory returns mock transport
	reader.transportFactory = func(_ detection.DeviceInfo) (pn532.Transport, error) {
		return &mockTransport{}, nil
	}

	// Device factory returns mock device
	reader.deviceFactory = func(_ pn532.Transport) (PN532Device, error) {
		return mockDevice, nil
	}

	// Session factory returns mock session
	reader.sessionFactory = func(_ PN532Device, _ *polling.Config) PollingSession {
		return mockSession
	}

	device := config.ReadersConnect{
		Driver: "pn532",
		Path:   "/dev/test",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// Close reader (triggers context cancellation)
	err = reader.Close()
	require.NoError(t, err)

	// No ReaderError scan should be sent (context.Canceled is expected)
	testutils.AssertNoScan(t, scanQueue, 500*time.Millisecond)

	// Verify session was closed
	assert.True(t, mockSession.closeCalled, "session Close should be called")
}

// TestOpen_TagDetectionAndRemoval tests normal tag detection and removal flow.
func TestOpen_TagDetectionAndRemoval(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockDevice := &mockPN532Device{}
	mockSession := &mockPollingSession{}

	// Session start function simulates: tag detected → tag removed → context canceled
	mockSession.startFunc = func(ctx context.Context) error {
		if mockSession.onCardDetected != nil {
			// Simulate tag detection
			tag := &pn532.DetectedTag{
				Type:       pn532.TagTypeNTAG,
				UID:        "ntag-uid-123",
				TargetData: []byte{0x01, 0x02, 0x03},
			}
			_ = mockSession.onCardDetected(tag)

			// Wait a bit then simulate tag removal
			time.Sleep(100 * time.Millisecond)
			if mockSession.onCardRemoved != nil {
				mockSession.onCardRemoved()
			}
		}

		// Wait for context cancellation
		<-ctx.Done()
		return ctx.Err()
	}

	// Transport factory returns mock transport
	reader.transportFactory = func(_ detection.DeviceInfo) (pn532.Transport, error) {
		return &mockTransport{}, nil
	}

	// Device factory returns mock device
	reader.deviceFactory = func(_ pn532.Transport) (PN532Device, error) {
		return mockDevice, nil
	}

	// Session factory returns mock session
	reader.sessionFactory = func(_ PN532Device, _ *polling.Config) PollingSession {
		return mockSession
	}

	device := config.ReadersConnect{
		Driver: "pn532",
		Path:   "/dev/test",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// First scan: token detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "ntag-uid-123", scan1.Token.UID)
	assert.Equal(t, tokens.TypeNTAG, scan1.Token.Type)
	assert.False(t, scan1.ReaderError)

	// Second scan: token removed
	scan2 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.Nil(t, scan2.Token, "token should be nil on removal")
	assert.False(t, scan2.ReaderError, "ReaderError should be false for normal removal")
}

// TestOpen_TagChanged tests the OnCardChanged callback flow.
func TestOpen_TagChanged(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockDevice := &mockPN532Device{}
	mockSession := &mockPollingSession{}

	// Session start function simulates: tag1 detected → tag1 changed to tag2
	mockSession.startFunc = func(ctx context.Context) error {
		if mockSession.onCardDetected != nil {
			// Simulate first tag detection
			tag1 := &pn532.DetectedTag{
				Type:       pn532.TagTypeNTAG,
				UID:        "ntag-uid-first",
				TargetData: []byte{0x01, 0x02, 0x03},
			}
			_ = mockSession.onCardDetected(tag1)

			// Wait a bit then simulate tag change
			time.Sleep(100 * time.Millisecond)
			if mockSession.onCardChanged != nil {
				tag2 := &pn532.DetectedTag{
					Type:       pn532.TagTypeMIFARE,
					UID:        "mifare-uid-second",
					TargetData: []byte{0x04, 0x05, 0x06},
				}
				_ = mockSession.onCardChanged(tag2)
			}
		}

		// Wait for context cancellation
		<-ctx.Done()
		return ctx.Err()
	}

	// Transport factory returns mock transport
	reader.transportFactory = func(_ detection.DeviceInfo) (pn532.Transport, error) {
		return &mockTransport{}, nil
	}

	// Device factory returns mock device
	reader.deviceFactory = func(_ pn532.Transport) (PN532Device, error) {
		return mockDevice, nil
	}

	// Session factory returns mock session
	reader.sessionFactory = func(_ PN532Device, _ *polling.Config) PollingSession {
		return mockSession
	}

	device := config.ReadersConnect{
		Driver: "pn532",
		Path:   "/dev/test",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// First scan: first tag detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "ntag-uid-first", scan1.Token.UID)
	assert.Equal(t, tokens.TypeNTAG, scan1.Token.Type)

	// Second scan: tag changed
	scan2 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan2.Token)
	assert.Equal(t, "mifare-uid-second", scan2.Token.UID)
	assert.Equal(t, tokens.TypeMifare, scan2.Token.Type)
}

// TestClose tests that Close() properly cancels context and closes session.
func TestClose(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockDevice := &mockPN532Device{}
	mockSession := &mockPollingSession{}

	// Session waits for context cancellation
	mockSession.startFunc = func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}

	// Transport factory returns mock transport
	reader.transportFactory = func(_ detection.DeviceInfo) (pn532.Transport, error) {
		return &mockTransport{}, nil
	}

	// Device factory returns mock device
	reader.deviceFactory = func(_ pn532.Transport) (PN532Device, error) {
		return mockDevice, nil
	}

	// Session factory returns mock session
	reader.sessionFactory = func(_ PN532Device, _ *polling.Config) PollingSession {
		return mockSession
	}

	device := config.ReadersConnect{
		Driver: "pn532",
		Path:   "/dev/test",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// Verify reader is connected
	assert.True(t, reader.Connected())

	// Close the reader
	err = reader.Close()
	require.NoError(t, err)

	// Verify session was closed
	assert.True(t, mockSession.closeCalled, "session Close should be called")

	// Verify reader is disconnected
	assert.False(t, reader.Connected())
}

// TestOpen_DeviceInitError verifies that device initialization errors are handled correctly.
func TestOpen_DeviceInitError(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	mockDevice := &mockPN532Device{
		initErr: assert.AnError,
	}

	// Transport factory returns mock transport
	reader.transportFactory = func(_ detection.DeviceInfo) (pn532.Transport, error) {
		return &mockTransport{}, nil
	}

	// Device factory returns mock device with init error
	reader.deviceFactory = func(_ pn532.Transport) (PN532Device, error) {
		return mockDevice, nil
	}

	device := config.ReadersConnect{
		Driver: "pn532",
		Path:   "/dev/test",
	}

	err := reader.Open(device, scanQueue)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize PN532 device")
	assert.True(t, mockDevice.initCalled)
	assert.True(t, mockDevice.closeCalled, "device should be closed on init error")
}

// TestWriteWithContext_Success tests successful write operation that creates result token
// with UID and Type from the tag.
func TestWriteWithContext_Success(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)

	// Create mock tag with specific UID and type
	mockNTAGTag := &mockTag{
		uid:     "test-uid-123",
		tagType: pn532.TagTypeNTAG,
	}

	// Create mock session that will invoke the write callback
	mockSession := &mockPollingSession{
		mockTag: mockNTAGTag,
	}

	// Set up reader with mock session
	reader.session = mockSession
	reader.deviceInfo = config.ReadersConnect{
		Driver: "pn532",
		Path:   "/dev/test",
	}

	// Perform write operation
	ctx := context.Background()
	text := "test-text-content"
	token, err := reader.WriteWithContext(ctx, text)

	// Verify write succeeded
	require.NoError(t, err)
	require.NotNil(t, token)

	// Verify token has UID from tag
	assert.Equal(t, "test-uid-123", token.UID, "token should have UID from tag.UID()")

	// Verify token has Type from convertTagType
	assert.Equal(t, tokens.TypeNTAG, token.Type, "token should have Type from convertTagType()")

	// Verify token has correct text
	assert.Equal(t, text, token.Text, "token should have the written text")

	// Verify token has correct source
	expectedSource := reader.deviceInfo.ConnectionString()
	assert.Equal(t, expectedSource, token.Source, "token should have correct source")

	// Verify NDEF was written to tag
	assert.True(t, mockNTAGTag.writeNDEFCalled, "WriteNDEFWithContext should be called")
	require.NotNil(t, mockNTAGTag.lastNDEFMessage, "NDEF message should be written")
	require.Len(t, mockNTAGTag.lastNDEFMessage.Records, 1, "should have one NDEF record")
	assert.Equal(t, pn532.NDEFTypeText, mockNTAGTag.lastNDEFMessage.Records[0].Type)
	assert.Equal(t, text, mockNTAGTag.lastNDEFMessage.Records[0].Text)
}

// TestWriteWithContext_DifferentTagTypes tests that different tag types are converted correctly
// via convertTagType() method.
func TestWriteWithContext_DifferentTagTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pn532TagType      pn532.TagType
		expectedTokenType string
		name              string
		description       string
	}{
		{
			name:              "NTAG tag type",
			pn532TagType:      pn532.TagTypeNTAG,
			expectedTokenType: tokens.TypeNTAG,
			description:       "NTAG should convert to TypeNTAG",
		},
		{
			name:              "MIFARE tag type",
			pn532TagType:      pn532.TagTypeMIFARE,
			expectedTokenType: tokens.TypeMifare,
			description:       "MIFARE should convert to TypeMifare",
		},
		{
			name:              "FeliCa tag type",
			pn532TagType:      pn532.TagTypeFeliCa,
			expectedTokenType: tokens.TypeFeliCa,
			description:       "FeliCa should convert to TypeFeliCa",
		},
		{
			name:              "Unknown tag type",
			pn532TagType:      pn532.TagTypeUnknown,
			expectedTokenType: tokens.TypeUnknown,
			description:       "Unknown should convert to TypeUnknown",
		},
		{
			name:              "Any tag type",
			pn532TagType:      pn532.TagTypeAny,
			expectedTokenType: tokens.TypeUnknown,
			description:       "Any should convert to TypeUnknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Instance{}
			reader := NewReader(cfg)

			// Create mock tag with specific type
			mockTestTag := &mockTag{
				uid:     "test-uid-" + string(tt.pn532TagType),
				tagType: tt.pn532TagType,
			}

			// Create mock session
			mockSession := &mockPollingSession{
				mockTag: mockTestTag,
			}

			// Set up reader
			reader.session = mockSession
			reader.deviceInfo = config.ReadersConnect{
				Driver: "pn532",
				Path:   "/dev/test",
			}

			// Perform write
			ctx := context.Background()
			token, err := reader.WriteWithContext(ctx, "test-text")

			// Verify
			require.NoError(t, err)
			require.NotNil(t, token)
			assert.Equal(t, tt.expectedTokenType, token.Type, tt.description)
		})
	}
}
