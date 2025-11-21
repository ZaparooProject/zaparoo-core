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

package rs232barcode

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/testutils"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
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

	reader := &Reader{}
	metadata := reader.Metadata()

	assert.Equal(t, "rs232barcode", metadata.ID)
	assert.Equal(t, "RS232 barcode/QR code reader", metadata.Description)
	assert.True(t, metadata.DefaultEnabled)
	assert.False(t, metadata.DefaultAutoDetect)
}

func TestIDs(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	ids := reader.IDs()

	require.Len(t, ids, 2)
	assert.Equal(t, "rs232barcode", ids[0])
	assert.Equal(t, "rs232_barcode", ids[1])
}

func TestDetect(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	result := reader.Detect([]string{"any", "input"})

	assert.Empty(t, result, "rs232barcode does not support auto-detection")
}

func TestWrite_NotSupported(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	token, err := reader.Write("test-data")

	assert.Nil(t, token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing not supported")
}

func TestCancelWrite(t *testing.T) {
	t.Parallel()

	reader := &Reader{}

	// Should not panic
	reader.CancelWrite()
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	capabilities := reader.Capabilities()

	assert.Empty(t, capabilities, "rs232barcode reader has no special capabilities")
}

func TestOnMediaChange(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	err := reader.OnMediaChange(&models.ActiveMedia{})

	assert.NoError(t, err, "OnMediaChange should return nil")
}

func TestConnected_NoPort(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		polling: true,
		port:    nil,
	}

	assert.False(t, reader.Connected(), "should not be connected without port")
}

func TestConnected_NotPolling(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		polling: false,
		port:    nil,
	}

	assert.False(t, reader.Connected(), "should not be connected when not polling")
}

func TestParseLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		line         string
		expectedUID  string
		expectedText string
		expectToken  bool
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
			name:         "simple barcode",
			line:         "1234567890",
			expectToken:  true,
			expectedUID:  "1234567890",
			expectedText: "1234567890",
		},
		{
			name:         "QR code with URL",
			line:         "https://example.com/product/123",
			expectToken:  true,
			expectedUID:  "https://example.com/product/123",
			expectedText: "https://example.com/product/123",
		},
		{
			name:         "barcode with spaces",
			line:         "  BARCODE-WITH-SPACES  ",
			expectToken:  true,
			expectedUID:  "BARCODE-WITH-SPACES",
			expectedText: "BARCODE-WITH-SPACES",
		},
		{
			name:         "barcode with carriage return",
			line:         "CODE123\r",
			expectToken:  true,
			expectedUID:  "CODE123",
			expectedText: "CODE123",
		},
		{
			name:         "UPC-A barcode",
			line:         "012345678905",
			expectToken:  true,
			expectedUID:  "012345678905",
			expectedText: "012345678905",
		},
		{
			name:         "EAN-13 barcode",
			line:         "5901234123457",
			expectToken:  true,
			expectedUID:  "5901234123457",
			expectedText: "5901234123457",
		},
		{
			name:         "Code 128 with alphanumeric",
			line:         "ABC-123-XYZ",
			expectToken:  true,
			expectedUID:  "ABC-123-XYZ",
			expectedText: "ABC-123-XYZ",
		},
		{
			name:         "barcode with STX/ETX framing",
			line:         "\x02BARCODE-DATA\x03",
			expectToken:  true,
			expectedUID:  "BARCODE-DATA",
			expectedText: "BARCODE-DATA",
		},
		{
			name:         "barcode with STX only",
			line:         "\x02BARCODE-123",
			expectToken:  true,
			expectedUID:  "BARCODE-123",
			expectedText: "BARCODE-123",
		},
		{
			name:         "barcode with ETX only",
			line:         "BARCODE-456\x03",
			expectToken:  true,
			expectedUID:  "BARCODE-456",
			expectedText: "BARCODE-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create new reader for each test to ensure independence
			r := &Reader{
				device: config.ReadersConnect{
					Driver: "rs232barcode",
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
			assert.Equal(t, tokens.TypeBarcode, token.Type)
			assert.Equal(t, tt.expectedUID, token.UID)
			assert.Equal(t, tt.expectedText, token.Text)
			assert.Equal(t, tt.expectedUID, token.Data)
			assert.Equal(t, "rs232barcode:/dev/ttyUSB0", token.Source)
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
		Driver: "rs232barcode",
		Path:   testutils.CreateTempDevicePath(t),
	}

	err := reader.Open(device, scanQueue)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to set read timeout")
}

func TestOpen_SuccessfulConnection(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()
	mockPort.ReadData = []byte("1234567890\n")

	reader := NewReader(&config.Instance{})
	reader.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return mockPort, nil
	}

	scanQueue := testutils.CreateTestScanChannel(t)
	devicePath := testutils.CreateTempDevicePath(t)
	device := config.ReadersConnect{
		Driver: "rs232barcode",
		Path:   devicePath,
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	assert.True(t, reader.Connected())
	assert.Equal(t, devicePath, reader.Info())

	// Wait for scan to be processed
	scan := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)

	assert.NotNil(t, scan.Token)
	assert.Equal(t, tokens.TypeBarcode, scan.Token.Type)
	assert.Equal(t, "1234567890", scan.Token.UID)
	assert.Equal(t, "1234567890", scan.Token.Text)
	assert.False(t, scan.ReaderError)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
	assert.False(t, reader.Connected())
}

func TestOpen_ReaderError(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()

	// Custom read function: first read succeeds with token, second read fails
	callCount := 0
	mockPort.ReadFunc = func(p []byte) (int, error) {
		callCount++
		if callCount == 1 {
			// First call: return valid barcode
			data := []byte("TEST-BARCODE-123\n")
			return copy(p, data), nil
		}
		// Second call: simulate reader error
		return 0, assert.AnError
	}

	reader := NewReader(&config.Instance{})
	reader.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return mockPort, nil
	}

	scanQueue := testutils.CreateTestScanChannel(t)
	devicePath := testutils.CreateTempDevicePath(t)
	device := config.ReadersConnect{
		Driver: "rs232barcode",
		Path:   devicePath,
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// First scan: barcode detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "TEST-BARCODE-123", scan1.Token.UID)
	assert.Equal(t, tokens.TypeBarcode, scan1.Token.Type)
	assert.False(t, scan1.ReaderError)

	// Reader error should close the reader but not send any additional scans
	// (barcode readers don't have "active token" concept like NFC readers)
	time.Sleep(100 * time.Millisecond)
	testutils.AssertNoScan(t, scanQueue, 100*time.Millisecond)

	// Verify reader auto-closed after error
	assert.True(t, mockPort.IsClosed())
}

func TestOpen_MultipleBarcodes(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()

	// Use custom read function to send multiple barcodes
	tokenIndex := 0
	barcodes := []string{
		"BARCODE-001\n",
		"BARCODE-002\n",
		"BARCODE-001\n", // Same as first - should still be sent (no deduplication)
	}

	mockPort.ReadFunc = func(p []byte) (int, error) {
		if tokenIndex < len(barcodes) {
			data := []byte(barcodes[tokenIndex])
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
		Driver: "rs232barcode",
		Path:   testutils.CreateTempDevicePath(t),
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// First barcode
	scan1 := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "BARCODE-001", scan1.Token.UID)
	assert.Equal(t, tokens.TypeBarcode, scan1.Token.Type)

	// Second barcode (different from first)
	scan2 := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan2.Token)
	assert.Equal(t, "BARCODE-002", scan2.Token.UID)
	assert.Equal(t, tokens.TypeBarcode, scan2.Token.Type)

	// Third barcode (duplicate of first) - should still be sent
	// Each barcode scan is independent; no deduplication
	scan3 := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan3.Token)
	assert.Equal(t, "BARCODE-001", scan3.Token.UID)
	assert.Equal(t, tokens.TypeBarcode, scan3.Token.Type)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_SplitReads(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()

	// Simulate a barcode arriving in multiple chunks
	readCount := 0
	chunks := [][]byte{
		[]byte("SPLIT"),
		[]byte("-BAR"),
		[]byte("CODE"),
		[]byte("-123\n"),
	}

	mockPort.ReadFunc = func(p []byte) (int, error) {
		if readCount < len(chunks) {
			chunk := chunks[readCount]
			readCount++
			time.Sleep(10 * time.Millisecond)
			return copy(p, chunk), nil
		}
		// After all chunks sent, simulate blocking read
		time.Sleep(50 * time.Millisecond)
		return 0, nil
	}

	reader := NewReader(&config.Instance{})
	reader.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return mockPort, nil
	}

	scanQueue := testutils.CreateTestScanChannel(t)
	device := config.ReadersConnect{
		Driver: "rs232barcode",
		Path:   testutils.CreateTempDevicePath(t),
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// Should receive complete barcode after all chunks arrive
	scan := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan.Token)
	assert.Equal(t, "SPLIT-BARCODE-123", scan.Token.UID)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_BufferOverflow(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()

	// Build overflow data: 9KB of X's followed by newline, then valid barcode
	// (exceeds 8KB limit to test overflow handling)
	overflowData := make([]byte, 0, 9100)
	for range 9000 {
		overflowData = append(overflowData, 'X')
	}
	// Append newline to terminate the overflow data, then valid barcode
	overflowData = append(overflowData, []byte("\nVALID-BARCODE\n")...)
	mockPort.ReadData = overflowData

	reader := NewReader(&config.Instance{})
	reader.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return mockPort, nil
	}

	scanQueue := testutils.CreateTestScanChannel(t)
	device := config.ReadersConnect{
		Driver: "rs232barcode",
		Path:   testutils.CreateTempDevicePath(t),
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// Should receive the valid barcode after buffer overflow recovery
	// The overflow data (9000 X's) should be discarded when 8KB buffer limit is hit
	scan := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan.Token)
	assert.Equal(t, "VALID-BARCODE", scan.Token.UID)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_LargeQRCode(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()

	// Test with a large QR code (close to 7KB - max numeric QR code)
	largeQRData := make([]byte, 0, 7100)
	for range 7000 {
		largeQRData = append(largeQRData, '1')
	}
	largeQRData = append(largeQRData, '\n')
	mockPort.ReadData = largeQRData

	reader := NewReader(&config.Instance{})
	reader.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return mockPort, nil
	}

	scanQueue := testutils.CreateTestScanChannel(t)
	device := config.ReadersConnect{
		Driver: "rs232barcode",
		Path:   testutils.CreateTempDevicePath(t),
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// Should successfully receive large QR code (7000 characters)
	scan := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan.Token)
	assert.Len(t, scan.Token.UID, 7000)
	assert.Equal(t, tokens.TypeBarcode, scan.Token.Type)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestParseLine_STXETXFraming(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		line         string
		expectedUID  string
		expectedText string
	}{
		{
			name:         "STX and ETX framing",
			line:         "\x02BARCODE-DATA\x03",
			expectedUID:  "BARCODE-DATA",
			expectedText: "BARCODE-DATA",
		},
		{
			name:         "STX only",
			line:         "\x02BARCODE-123",
			expectedUID:  "BARCODE-123",
			expectedText: "BARCODE-123",
		},
		{
			name:         "ETX only",
			line:         "BARCODE-456\x03",
			expectedUID:  "BARCODE-456",
			expectedText: "BARCODE-456",
		},
		{
			name:         "STX/ETX with spaces",
			line:         "  \x02BARCODE-CLEAN\x03  ",
			expectedUID:  "BARCODE-CLEAN",
			expectedText: "BARCODE-CLEAN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &Reader{
				device: config.ReadersConnect{
					Driver: "rs232barcode",
					Path:   "/dev/ttyUSB0",
				},
			}

			token, err := r.parseLine(tt.line)

			require.NoError(t, err)
			require.NotNil(t, token)
			assert.Equal(t, tokens.TypeBarcode, token.Type)
			assert.Equal(t, tt.expectedUID, token.UID)
			assert.Equal(t, tt.expectedText, token.Text)
		})
	}
}

func TestOpen_CarriageReturnDelimiter(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()

	// Test both \r and \n as delimiters
	mockPort.ReadData = []byte("BARCODE-WITH-CR\rBARCODE-WITH-LF\nBARCODE-WITH-CRLF\r\n")

	reader := NewReader(&config.Instance{})
	reader.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return mockPort, nil
	}

	scanQueue := testutils.CreateTestScanChannel(t)
	device := config.ReadersConnect{
		Driver: "rs232barcode",
		Path:   testutils.CreateTempDevicePath(t),
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// First barcode with \r
	scan1 := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "BARCODE-WITH-CR", scan1.Token.UID)

	// Second barcode with \n
	scan2 := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan2.Token)
	assert.Equal(t, "BARCODE-WITH-LF", scan2.Token.UID)

	// Third barcode with \r\n (should only trigger once, not twice)
	scan3 := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan3.Token)
	assert.Equal(t, "BARCODE-WITH-CRLF", scan3.Token.UID)

	// No additional scans from the extra line delimiter
	time.Sleep(100 * time.Millisecond)
	testutils.AssertNoScan(t, scanQueue, 100*time.Millisecond)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_EmptyLinesIgnored(t *testing.T) {
	t.Parallel()

	mockPort := testutils.NewMockSerialPort()

	// Send multiple delimiters and empty lines
	mockPort.ReadData = []byte("\n\r\r\nVALID-BARCODE\n\r\n")

	reader := NewReader(&config.Instance{})
	reader.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return mockPort, nil
	}

	scanQueue := testutils.CreateTestScanChannel(t)
	device := config.ReadersConnect{
		Driver: "rs232barcode",
		Path:   testutils.CreateTempDevicePath(t),
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// Should only receive one scan for the valid barcode
	scan := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
	assert.NotNil(t, scan.Token)
	assert.Equal(t, "VALID-BARCODE", scan.Token.UID)

	// No additional scans from empty lines
	time.Sleep(100 * time.Millisecond)
	testutils.AssertNoScan(t, scanQueue, 100*time.Millisecond)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestClose_WithoutPort(t *testing.T) {
	t.Parallel()

	reader := &Reader{
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

	reader := &Reader{
		port:    mockPort,
		polling: true,
	}

	err := reader.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to close serial port")
	assert.False(t, reader.polling)
}
