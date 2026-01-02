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
	"context"
	"testing"

	pn533 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
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
