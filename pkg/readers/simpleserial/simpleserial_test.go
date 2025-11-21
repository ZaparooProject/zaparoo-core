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

package simpleserial

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.bug.st/serial"
)

func TestNewReader(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)

	assert.NotNil(t, reader)
	assert.Equal(t, cfg, reader.cfg)
}

func TestMetadata(t *testing.T) {
	t.Parallel()

	reader := &SimpleSerialReader{}
	metadata := reader.Metadata()

	assert.Equal(t, "simpleserial", metadata.ID)
	assert.Equal(t, "Simple serial protocol reader", metadata.Description)
	assert.True(t, metadata.DefaultEnabled)
	assert.True(t, metadata.DefaultAutoDetect)
}

func TestIDs(t *testing.T) {
	t.Parallel()

	reader := &SimpleSerialReader{}
	ids := reader.IDs()

	require.Len(t, ids, 2)
	assert.Equal(t, "simpleserial", ids[0])
	assert.Equal(t, "simple_serial", ids[1])
}

func TestDetect(t *testing.T) {
	t.Parallel()

	reader := &SimpleSerialReader{}
	result := reader.Detect([]string{"any", "input"})

	assert.Empty(t, result, "simpleserial does not support auto-detection")
}

func TestWrite_NotSupported(t *testing.T) {
	t.Parallel()

	reader := &SimpleSerialReader{}
	token, err := reader.Write("test-data")

	assert.Nil(t, token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing not supported")
}

func TestCancelWrite(t *testing.T) {
	t.Parallel()

	reader := &SimpleSerialReader{}

	// Should not panic
	reader.CancelWrite()
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	reader := &SimpleSerialReader{}
	capabilities := reader.Capabilities()

	assert.Empty(t, capabilities, "simpleserial reader has no special capabilities")
}

func TestOnMediaChange(t *testing.T) {
	t.Parallel()

	reader := &SimpleSerialReader{}
	err := reader.OnMediaChange(&models.ActiveMedia{})

	assert.NoError(t, err, "OnMediaChange should return nil")
}

func TestConnected_NoPort(t *testing.T) {
	t.Parallel()

	reader := &SimpleSerialReader{
		polling: true,
		port:    nil,
	}

	assert.False(t, reader.Connected(), "should not be connected without port")
}

func TestConnected_NotPolling(t *testing.T) {
	t.Parallel()

	reader := &SimpleSerialReader{
		polling: false,
		port:    nil,
	}

	assert.False(t, reader.Connected(), "should not be connected when not polling")
}

func TestParseLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		line            string
		expectedUID     string
		expectedText    string
		expectToken     bool
		expectedFromAPI bool
	}{
		{
			name:        "empty line",
			line:        "",
			expectToken: false,
		},
		{
			name:        "whitespace only",
			line:        "   \r\n",
			expectToken: false,
		},
		{
			name:        "no SCAN prefix",
			line:        "invalid format",
			expectToken: false,
		},
		{
			name:        "SCAN with no args",
			line:        "SCAN\t",
			expectToken: false,
		},
		{
			name:         "SCAN with text only (no named args)",
			line:         "SCAN\t**launch.system:nes",
			expectToken:  true,
			expectedText: "**launch.system:nes",
		},
		{
			name:        "SCAN with uid",
			line:        "SCAN\tuid=abc123",
			expectToken: true,
			expectedUID: "abc123",
		},
		{
			name:         "SCAN with text",
			line:         "SCAN\ttext=hello",
			expectToken:  true,
			expectedText: "hello",
		},
		{
			name:         "SCAN with uid and text",
			line:         "SCAN\tuid=abc123\ttext=hello world",
			expectToken:  true,
			expectedUID:  "abc123",
			expectedText: "hello world",
		},
		{
			name:            "SCAN with removable=no",
			line:            "SCAN\tuid=abc123\tremovable=no",
			expectToken:     true,
			expectedUID:     "abc123",
			expectedFromAPI: true,
		},
		{
			name:            "SCAN with removable=yes",
			line:            "SCAN\tuid=abc123\tremovable=yes",
			expectToken:     true,
			expectedUID:     "abc123",
			expectedFromAPI: false,
		},
		{
			name:            "SCAN with all args",
			line:            "SCAN\tuid=xyz789\ttext=test message\tremovable=no",
			expectToken:     true,
			expectedUID:     "xyz789",
			expectedText:    "test message",
			expectedFromAPI: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create new reader for each test to ensure independence
			r := &SimpleSerialReader{
				device: config.ReadersConnect{
					Driver: "simple_serial",
					Path:   "/dev/ttyUSB0",
				},
			}

			token, err := r.parseLine(tt.line)

			require.NoError(t, err)

			if !tt.expectToken {
				assert.Nil(t, token)
				return
			}

			require.NotNil(t, token)
			assert.Equal(t, tt.expectedUID, token.UID)
			assert.Equal(t, tt.expectedText, token.Text)
			assert.Equal(t, tt.expectedFromAPI, token.FromAPI)
			assert.Equal(t, "simpleserial:/dev/ttyUSB0", token.Source)
		})
	}
}

func TestOpen_InvalidDriver(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "invalid-driver",
		Path:   "/dev/ttyUSB0",
	}

	err := reader.Open(device, scanQueue)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid reader id")
}

func TestOpen_SetReadTimeoutError(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()
	mockPort.TimeoutErr = assert.AnError

	reader := NewReader(&config.Instance{})
	reader.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return mockPort, nil
	}

	scanQueue := testutils.CreateTestScanChannel(t)
	device := config.ReadersConnect{
		Driver: "simple_serial",
		Path:   testutils.CreateTempDevicePath(t),
	}

	err := reader.Open(device, scanQueue)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to set read timeout")
}

func TestOpen_SuccessfulConnection(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()
	mockPort.ReadData = []byte("SCAN\tuid=test123\ttext=hello\n")

	reader := NewReader(&config.Instance{})
	reader.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return mockPort, nil
	}

	scanQueue := testutils.CreateTestScanChannel(t)
	devicePath := testutils.CreateTempDevicePath(t)
	device := config.ReadersConnect{
		Driver: "simple_serial",
		Path:   devicePath,
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	assert.True(t, reader.Connected())
	assert.Equal(t, devicePath, reader.Info())

	// Wait for scan to be processed
	scan := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)

	assert.NotNil(t, scan.Token)
	assert.Equal(t, "test123", scan.Token.UID)
	assert.Equal(t, "hello", scan.Token.Text)
	assert.False(t, scan.ReaderError)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
	assert.False(t, reader.Connected())
}

func TestOpen_ReaderErrorWithActiveToken(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()

	// Custom read function: first read succeeds with token, second read fails
	callCount := 0
	mockPort.ReadFunc = func(p []byte) (int, error) {
		callCount++
		if callCount == 1 {
			// First call: return valid token
			data := []byte("SCAN\tuid=active-token\n")
			return copy(p, data), nil
		}
		// Second call: simulate reader error (issue #326 scenario)
		return 0, assert.AnError
	}

	reader := NewReader(&config.Instance{})
	reader.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return mockPort, nil
	}

	scanQueue := testutils.CreateTestScanChannel(t)
	devicePath := testutils.CreateTempDevicePath(t)
	device := config.ReadersConnect{
		Driver: "simple_serial",
		Path:   devicePath,
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// First scan: token detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "active-token", scan1.Token.UID)
	assert.False(t, scan1.ReaderError)

	// Second scan: reader error with active token
	// This tests the bug fix from issue #326 - ReaderError should be set to prevent
	// triggering on_remove hooks and exit timer
	scan2 := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.Nil(t, scan2.Token, "token should be nil on reader error")
	assert.True(t, scan2.ReaderError, "ReaderError should be true to prevent on_remove execution")
	assert.Equal(t, "simpleserial:"+devicePath, scan2.Source)

	// Verify reader auto-closed after error
	time.Sleep(50 * time.Millisecond)
	assert.True(t, mockPort.IsClosed())
}

func TestOpen_ReaderErrorWithoutActiveToken(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()
	mockPort.ReadError = assert.AnError

	reader := NewReader(&config.Instance{})
	reader.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return mockPort, nil
	}

	scanQueue := testutils.CreateTestScanChannel(t)
	device := config.ReadersConnect{
		Driver: "simple_serial",
		Path:   testutils.CreateTempDevicePath(t),
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// Wait briefly for error to be processed
	time.Sleep(100 * time.Millisecond)

	// No scan should be sent when there's no active token
	testutils.AssertNoScan(t, scanQueue, 100*time.Millisecond)

	// Verify reader auto-closed after error
	assert.True(t, mockPort.IsClosed())
}

func TestOpen_TokenTimeout(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()

	// Custom read function: return token once, then wait for timeout
	callCount := 0
	mockPort.ReadFunc = func(p []byte) (int, error) {
		callCount++
		if callCount == 1 {
			// Return token
			data := []byte("SCAN\tuid=timeout-test\n")
			return copy(p, data), nil
		}
		// Subsequent calls: simulate blocking read with delay
		time.Sleep(50 * time.Millisecond)
		return 0, nil
	}

	reader := NewReader(&config.Instance{})
	reader.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return mockPort, nil
	}

	scanQueue := testutils.CreateTestScanChannel(t)
	device := config.ReadersConnect{
		Driver: "simple_serial",
		Path:   testutils.CreateTempDevicePath(t),
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// First scan: token detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "timeout-test", scan1.Token.UID)

	// Wait for token timeout (> 1 second)
	scan2 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.Nil(t, scan2.Token, "token should be nil after timeout")
	assert.False(t, scan2.ReaderError, "should not be a reader error, just normal timeout")

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_MultipleTokens(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()

	// Use custom read function to control timing and avoid token timeout
	tokenIndex := 0
	tokens := []string{
		"SCAN\tuid=token1\n",
		"SCAN\tuid=token2\ttext=second\n",
		"SCAN\tuid=token1\n", // Same as first, should be deduplicated
	}

	mockPort.ReadFunc = func(p []byte) (int, error) {
		if tokenIndex < len(tokens) {
			data := []byte(tokens[tokenIndex])
			tokenIndex++
			// Small delay to allow processing between tokens
			time.Sleep(10 * time.Millisecond)
			return copy(p, data), nil
		}
		// After all tokens sent, simulate blocking read
		time.Sleep(50 * time.Millisecond)
		return 0, nil
	}

	reader := NewReader(&config.Instance{})
	reader.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return mockPort, nil
	}

	scanQueue := testutils.CreateTestScanChannel(t)
	device := config.ReadersConnect{
		Driver: "simple_serial",
		Path:   testutils.CreateTempDevicePath(t),
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// First token
	scan1 := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "token1", scan1.Token.UID)

	// Second token (different from first)
	scan2 := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan2.Token)
	assert.Equal(t, "token2", scan2.Token.UID)
	assert.Equal(t, "second", scan2.Token.Text)

	// Third token (same UID as first, but different from second which is lastToken)
	// This SHOULD generate a scan because it's different from the current lastToken
	scan3 := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan3.Token)
	assert.Equal(t, "token1", scan3.Token.UID)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_DuplicateTokenIgnored(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()

	// Use custom read function to send the same token twice in a row
	tokenIndex := 0
	tokens := []string{
		"SCAN\tuid=token1\ttext=first\n",
		"SCAN\tuid=token1\ttext=first\n", // Exact duplicate, should be ignored
	}

	mockPort.ReadFunc = func(p []byte) (int, error) {
		if tokenIndex < len(tokens) {
			data := []byte(tokens[tokenIndex])
			tokenIndex++
			// Small delay to allow processing between tokens
			time.Sleep(10 * time.Millisecond)
			return copy(p, data), nil
		}
		// After all tokens sent, simulate blocking read
		time.Sleep(50 * time.Millisecond)
		return 0, nil
	}

	reader := NewReader(&config.Instance{})
	reader.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return mockPort, nil
	}

	scanQueue := testutils.CreateTestScanChannel(t)
	device := config.ReadersConnect{
		Driver: "simple_serial",
		Path:   testutils.CreateTempDevicePath(t),
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// First scan: token received
	scan1 := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "token1", scan1.Token.UID)
	assert.Equal(t, "first", scan1.Token.Text)

	// Second scan: duplicate should be ignored, no scan sent
	time.Sleep(100 * time.Millisecond)
	testutils.AssertNoScan(t, scanQueue, 100*time.Millisecond)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestClose_WithoutPort(t *testing.T) {
	t.Parallel()

	reader := &SimpleSerialReader{
		port:    nil,
		polling: true,
	}

	err := reader.Close()
	require.NoError(t, err)
	assert.False(t, reader.polling)
}

func TestClose_PortCloseError(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()
	mockPort.CloseError = assert.AnError

	reader := &SimpleSerialReader{
		port:    mockPort,
		polling: true,
	}

	err := reader.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to close serial port")
	assert.False(t, reader.polling)
}
