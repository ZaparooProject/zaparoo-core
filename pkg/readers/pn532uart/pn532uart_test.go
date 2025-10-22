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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
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

	reader := &PN532UARTReader{}
	metadata := reader.Metadata()

	assert.Equal(t, "pn532uart", metadata.ID)
	assert.Equal(t, "PN532 NFC reader via UART (legacy)", metadata.Description)
	assert.True(t, metadata.DefaultEnabled)
	assert.True(t, metadata.DefaultAutoDetect)
}

func TestIDs(t *testing.T) {
	t.Parallel()

	reader := &PN532UARTReader{}
	ids := reader.IDs()

	require.Len(t, ids, 1)
	assert.Equal(t, "pn532_uart", ids[0])
}

func TestDetect(t *testing.T) {
	t.Parallel()

	reader := &PN532UARTReader{}
	result := reader.Detect([]string{"any", "input"})

	// Returns empty string as detection requires actual hardware
	assert.Empty(t, result)
}

func TestWrite_NotSupported(t *testing.T) {
	t.Parallel()

	reader := &PN532UARTReader{}
	token, err := reader.Write("test-data")

	assert.Nil(t, token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing not supported")
}

func TestCancelWrite(t *testing.T) {
	t.Parallel()

	reader := &PN532UARTReader{}

	// Should not panic
	reader.CancelWrite()
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	reader := &PN532UARTReader{}
	capabilities := reader.Capabilities()

	assert.Empty(t, capabilities, "PN532 UART reader has no special capabilities")
}

func TestOnMediaChange(t *testing.T) {
	t.Parallel()

	reader := &PN532UARTReader{}
	err := reader.OnMediaChange(&models.ActiveMedia{})

	assert.NoError(t, err, "OnMediaChange should return nil")
}

func TestConnected_NoPort(t *testing.T) {
	t.Parallel()

	reader := &PN532UARTReader{
		polling: true,
		port:    nil,
	}

	assert.False(t, reader.Connected(), "should not be connected without port")
}

func TestConnected_NotPolling(t *testing.T) {
	t.Parallel()

	reader := &PN532UARTReader{
		polling: false,
		port:    nil, // Would need mock port
	}

	assert.False(t, reader.Connected(), "should not be connected when not polling")
}
