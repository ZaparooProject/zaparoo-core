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
		"pn532_uart",
		"pn532_i2c",
		"pn532_spi",
	}

	assert.Equal(t, expectedIDs, ids)
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	capabilities := reader.Capabilities()

	require.Len(t, capabilities, 1)
	assert.Equal(t, readers.CapabilityWrite, capabilities[0])
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
