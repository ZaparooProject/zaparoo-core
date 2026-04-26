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

package pn532

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	pn533 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	assert.Equal(t, "pn532", metadata.ID)
	assert.Equal(t, "PN532 NFC reader (UART/I2C/SPI)", metadata.Description)
	assert.True(t, metadata.DefaultEnabled)
	assert.True(t, metadata.DefaultAutoDetect)
}

func TestIDs(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	ids := reader.IDs()

	expectedIDs := []string{
		"pn532",
		"pn532uart",
		"pn532_uart",
		"pn532i2c",
		"pn532_i2c",
		"pn532spi",
		"pn532_spi",
	}

	assert.Equal(t, expectedIDs, ids)
}

func TestIsExpectedDetectionMiss(t *testing.T) {
	t.Parallel()

	assert.True(t, isExpectedDetectionMiss(detection.ErrNoDevicesFound))
	assert.True(t, isExpectedDetectionMiss(detection.ErrDetectionTimeout))
	assert.False(t, isExpectedDetectionMiss(errors.New("serial permission denied")))
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	capabilities := reader.Capabilities()

	require.Len(t, capabilities, 2)
	assert.Contains(t, capabilities, readers.CapabilityWrite)
	assert.Contains(t, capabilities, readers.CapabilityRemovable)
}

func TestOnMediaChange(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	err := reader.OnMediaChange(&models.ActiveMedia{})

	assert.NoError(t, err, "OnMediaChange should return nil")
}

func TestWrite_EmptyText(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})
	token, err := reader.Write("")

	assert.Nil(t, token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text cannot be empty")
}

func TestWrite_NoSession(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})
	token, err := reader.Write("test")

	assert.Nil(t, token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session not initialized")
}

func TestWriteWithContext_EmptyText(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})
	ctx := context.Background()
	token, err := reader.WriteWithContext(ctx, "")

	assert.Nil(t, token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text cannot be empty")
}

func TestCancelWrite_NoActiveWrite(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})

	// Should not panic when there's no active write
	reader.CancelWrite()
}

func TestConnected_NoDevice(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		device: nil,
		ctx:    nil,
	}

	assert.False(t, reader.Connected(), "should not be connected without device")
}

func TestConnected_NoContext(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		device: nil, // Would need mock device
		ctx:    nil,
	}

	assert.False(t, reader.Connected(), "should not be connected without context")
}

func TestCreateTransport_InvalidType(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	deviceInfo := detection.DeviceInfo{
		Transport: "invalid",
		Path:      "/dev/test",
	}

	transport, err := reader.transportFactory(deviceInfo)

	assert.Nil(t, transport)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported transport type")
}

func TestOpen_TransportTypeParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		driver        string
		wantTransport string
	}{
		{"pn532", "uart"},
		{"pn532uart", "uart"},
		{"pn532_uart", "uart"},
		{"pn532i2c", "i2c"},
		{"pn532_i2c", "i2c"},
		{"pn532spi", "spi"},
		{"pn532_spi", "spi"},
	}

	for _, tt := range tests {
		t.Run(tt.driver, func(t *testing.T) {
			t.Parallel()

			var gotTransport string
			reader := NewReader(&config.Instance{})
			reader.transportFactory = func(di detection.DeviceInfo) (pn533.Transport, error) {
				gotTransport = di.Transport
				return nil, errors.New("stop here")
			}

			_ = reader.Open(config.ReadersConnect{
				Driver: tt.driver,
				Path:   "/dev/test",
			}, nil, readers.OpenOpts{})

			assert.Equal(t, tt.wantTransport, gotTransport)
		})
	}
}

func TestDevice(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		deviceInfo: config.ReadersConnect{
			Driver: "pn532uart",
			Path:   "/dev/ttyUSB0",
		},
	}

	result := reader.Path()

	assert.Equal(t, "/dev/ttyUSB0", result)
}

func TestInfo(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		name: "uart:/dev/ttyUSB0",
	}

	result := reader.Info()

	assert.Equal(t, "PN532 (uart:/dev/ttyUSB0)", result)
}

func TestCancelWrite_WithActiveWrite(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})

	// Simulate an active write by setting up a cancel function
	ctx, cancel := context.WithCancel(context.Background())
	reader.writeCtx = ctx
	reader.writeCancel = cancel

	// Should cancel without panic
	reader.CancelWrite()

	// Verify the context was cancelled
	assert.Error(t, ctx.Err(), "context should be cancelled")
}

// TestClose_ClearsFailedProbe verifies that closing a reader removes its
// path from the failed probe cache, allowing re-probing on next detection
// cycle. Not parallel because it modifies package-level state.
func TestClose_ClearsFailedProbe(t *testing.T) {
	// Save and restore package state
	probeStateMu.Lock()
	origFailed := failedProbePaths
	defer func() {
		probeStateMu.Lock()
		failedProbePaths = origFailed
		probeStateMu.Unlock()
	}()
	probeStateMu.Unlock()

	// Pre-populate failed probes for two paths
	probeStateMu.Lock()
	failedProbePaths = map[string]failedProbeEntry{
		"/dev/ttyUSB0": {deviceModTime: time.Now()},
		"/dev/ttyUSB1": {deviceModTime: time.Now()},
	}
	probeStateMu.Unlock()

	reader := &Reader{
		session: &mockPollingSession{},
		device:  &mockPN532Device{},
		deviceInfo: config.ReadersConnect{
			Driver: "pn532_uart",
			Path:   "/dev/ttyUSB0",
		},
	}

	err := reader.Close()
	require.NoError(t, err)

	probeStateMu.RLock()
	assert.NotContains(t, failedProbePaths, "/dev/ttyUSB0",
		"closed reader's path should be removed from failed probes")
	assert.Contains(t, failedProbePaths, "/dev/ttyUSB1",
		"other failed probe paths should be preserved")
	probeStateMu.RUnlock()
}

// TestClose_SessionError verifies that Close returns an error when the
// session fails to close, but still clears the failed probe cache.
// Not parallel because it modifies package-level state.
func TestClose_SessionError(t *testing.T) {
	// Save and restore package state
	probeStateMu.Lock()
	origFailed := failedProbePaths
	defer func() {
		probeStateMu.Lock()
		failedProbePaths = origFailed
		probeStateMu.Unlock()
	}()
	failedProbePaths = map[string]failedProbeEntry{
		"/dev/ttyUSB0": {deviceModTime: time.Now()},
	}
	probeStateMu.Unlock()

	reader := &Reader{
		session: &mockPollingSession{
			closeFunc: func() error {
				return errors.New("session close failed")
			},
		},
		device: &mockPN532Device{},
		deviceInfo: config.ReadersConnect{
			Driver: "pn532_uart",
			Path:   "/dev/ttyUSB0",
		},
	}

	err := reader.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to close PN532 session")

	// Probe cache should still be cleared despite the error
	probeStateMu.RLock()
	assert.NotContains(t, failedProbePaths, "/dev/ttyUSB0",
		"failed probe should be cleared even when session close fails")
	probeStateMu.RUnlock()
}

// TestClose_DeviceError verifies that Close returns an error when the
// device fails to close, but still clears the failed probe cache.
// Not parallel because it modifies package-level state.
func TestClose_DeviceError(t *testing.T) {
	// Save and restore package state
	probeStateMu.Lock()
	origFailed := failedProbePaths
	defer func() {
		probeStateMu.Lock()
		failedProbePaths = origFailed
		probeStateMu.Unlock()
	}()
	failedProbePaths = map[string]failedProbeEntry{
		"/dev/ttyUSB0": {deviceModTime: time.Now()},
	}
	probeStateMu.Unlock()

	reader := &Reader{
		session: &mockPollingSession{},
		device:  &mockPN532Device{closeErr: errors.New("device close failed")},
		deviceInfo: config.ReadersConnect{
			Driver: "pn532_uart",
			Path:   "/dev/ttyUSB0",
		},
	}

	err := reader.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to close PN532 device")

	// Probe cache should still be cleared despite the error
	probeStateMu.RLock()
	assert.NotContains(t, failedProbePaths, "/dev/ttyUSB0",
		"failed probe should be cleared even when device close fails")
	probeStateMu.RUnlock()
}

// TODO: Detect() integration with failed probe tracking is untested because
// detection.DetectAll and helpers.GetSerialDeviceList aren't injectable.
// Making those dependencies injectable would allow testing the full flow.

// TestRefreshFailedProbes verifies device file fingerprinting used by the
// failed probe tracking system. Not parallel because it modifies package-level state.
func TestRefreshFailedProbes(t *testing.T) {
	// Save and restore package state
	probeStateMu.Lock()
	origFailed := failedProbePaths
	defer func() {
		probeStateMu.Lock()
		failedProbePaths = origFailed
		probeStateMu.Unlock()
	}()
	probeStateMu.Unlock()

	t.Run("device gone", func(t *testing.T) {
		probeStateMu.Lock()
		defer probeStateMu.Unlock()
		failedProbePaths = map[string]failedProbeEntry{
			"/dev/nonexistent_device_xyz": {deviceModTime: time.Now()},
		}
		refreshFailedProbes()
		assert.Empty(t, failedProbePaths, "entry should be removed when device file is gone")
	})

	t.Run("device unchanged", func(t *testing.T) {
		// Create a temp file to simulate a device file
		tmpFile, err := os.CreateTemp("", "pn532test")
		require.NoError(t, err)
		defer func() { _ = os.Remove(tmpFile.Name()) }()
		_ = tmpFile.Close()

		info, err := os.Stat(tmpFile.Name())
		require.NoError(t, err)

		probeStateMu.Lock()
		defer probeStateMu.Unlock()
		failedProbePaths = map[string]failedProbeEntry{
			tmpFile.Name(): {deviceModTime: info.ModTime()},
		}
		refreshFailedProbes()
		assert.Contains(t, failedProbePaths, tmpFile.Name(),
			"entry should persist when ModTime unchanged")
	})

	t.Run("device changed", func(t *testing.T) {
		// Create a temp file and record a stale ModTime
		tmpFile, err := os.CreateTemp("", "pn532test")
		require.NoError(t, err)
		defer func() { _ = os.Remove(tmpFile.Name()) }()
		_ = tmpFile.Close()

		staleTime := time.Now().Add(-time.Hour)

		probeStateMu.Lock()
		defer probeStateMu.Unlock()
		failedProbePaths = map[string]failedProbeEntry{
			tmpFile.Name(): {deviceModTime: staleTime},
		}
		refreshFailedProbes()
		assert.Empty(t, failedProbePaths,
			"entry should be removed when ModTime differs (device was swapped)")
	})
}

// TestClearFailedProbe verifies that ClearFailedProbe removes only the
// specified path. Not parallel because it modifies package-level state.
func TestClearFailedProbe(t *testing.T) {
	probeStateMu.Lock()
	origFailed := failedProbePaths
	defer func() {
		probeStateMu.Lock()
		failedProbePaths = origFailed
		probeStateMu.Unlock()
	}()
	probeStateMu.Unlock()

	probeStateMu.Lock()
	failedProbePaths = map[string]failedProbeEntry{
		"/dev/ttyUSB0": {deviceModTime: time.Now()},
		"/dev/ttyUSB1": {deviceModTime: time.Now()},
	}
	probeStateMu.Unlock()

	ClearFailedProbe("/dev/ttyUSB0")

	probeStateMu.RLock()
	assert.NotContains(t, failedProbePaths, "/dev/ttyUSB0",
		"cleared path should be removed")
	assert.Contains(t, failedProbePaths, "/dev/ttyUSB1",
		"other paths should be preserved")
	probeStateMu.RUnlock()
}

func TestCreateVIDPIDBlocklist(t *testing.T) {
	t.Parallel()

	blocklist := createVIDPIDBlocklist()

	// Should contain Sinden Lightgun VID:PIDs
	assert.NotEmpty(t, blocklist)
	assert.Contains(t, blocklist, "16C0:0F38")
	assert.Contains(t, blocklist, "16D0:1094")

	// All entries should be in VID:PID format
	for _, entry := range blocklist {
		assert.Regexp(t, `^[0-9A-F]{4}:[0-9A-F]{4}$`, entry,
			"blocklist entry should be in VID:PID format")
	}
}

func TestConvertTagType(t *testing.T) {
	t.Parallel()

	reader := &Reader{}

	tests := []struct {
		inputTagType      pn533.TagType
		expectedTokenType string
		name              string
	}{
		{
			name:              "NTAG converts to TypeNTAG",
			inputTagType:      pn533.TagTypeNTAG,
			expectedTokenType: "NTAG",
		},
		{
			name:              "MIFARE converts to TypeMifare",
			inputTagType:      pn533.TagTypeMIFARE,
			expectedTokenType: "MIFARE",
		},
		{
			name:              "FeliCa converts to TypeFeliCa",
			inputTagType:      pn533.TagTypeFeliCa,
			expectedTokenType: "FeliCa",
		},
		{
			name:              "Unknown converts to TypeUnknown",
			inputTagType:      pn533.TagTypeUnknown,
			expectedTokenType: "Unknown",
		},
		{
			name:              "Any converts to TypeUnknown",
			inputTagType:      pn533.TagTypeAny,
			expectedTokenType: "Unknown",
		},
		{
			name:              "Invalid/default converts to TypeUnknown",
			inputTagType:      pn533.TagType("INVALID"),
			expectedTokenType: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := reader.convertTagType(tt.inputTagType)
			assert.Equal(t, tt.expectedTokenType, result)
		})
	}
}

func TestLogTraceableError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		expectLevel   string
		unexpectLevel string
	}{
		{
			name:          "context canceled logs at debug level",
			err:           context.Canceled,
			expectLevel:   `"level":"debug"`,
			unexpectLevel: `"level":"error"`,
		},
		{
			name:          "context deadline exceeded logs at debug level",
			err:           context.DeadlineExceeded,
			expectLevel:   `"level":"debug"`,
			unexpectLevel: `"level":"error"`,
		},
		{
			name:          "fatal hardware error logs at warn level",
			err:           pn533.ErrDeviceNotFound,
			expectLevel:   `"level":"warn"`,
			unexpectLevel: `"level":"error"`,
		},
		{
			name:          "transport closed logs at warn level",
			err:           pn533.ErrTransportClosed,
			expectLevel:   `"level":"warn"`,
			unexpectLevel: `"level":"error"`,
		},
		{
			name:          "other error logs at error level",
			err:           errors.New("unexpected communication failure"),
			expectLevel:   `"level":"error"`,
			unexpectLevel: `"level":"warn"`,
		},
		{
			name: "error with wire trace includes trace data",
			err: &pn533.TraceableError{
				Err:       errors.New("communication failed"),
				Transport: "UART",
				Port:      "/dev/ttyUSB0",
			},
			expectLevel:   `"level":"error"`,
			unexpectLevel: `"level":"warn"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			originalLogger := log.Logger
			log.Logger = zerolog.New(&buf)

			logTraceableError(tt.err, "test operation")

			log.Logger = originalLogger

			logOutput := buf.String()
			assert.Contains(t, logOutput, tt.expectLevel)
			assert.NotContains(t, logOutput, tt.unexpectLevel)
			assert.Contains(t, logOutput, "PN532 error")
			assert.Contains(t, logOutput, "test operation")

			// Verify wire trace data is included when present
			var te *pn533.TraceableError
			if errors.As(tt.err, &te) {
				assert.Contains(t, logOutput, te.Transport)
				assert.Contains(t, logOutput, te.Port)
				assert.Contains(t, logOutput, "wire_trace")
			}
		})
	}
}
