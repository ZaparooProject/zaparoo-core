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

//go:build linux

package libnfc

import (
	"testing"

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

func TestNewACR122Reader(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewACR122Reader(cfg)

	assert.NotNil(t, reader)
	assert.Equal(t, cfg, reader.cfg)
}

func TestMetadata(t *testing.T) {
	t.Parallel()

	t.Run("normal mode", func(t *testing.T) {
		t.Parallel()

		reader := NewReader(&config.Instance{})
		metadata := reader.Metadata()

		assert.Equal(t, "libnfc", metadata.ID)
		assert.Equal(t, "LibNFC NFC reader (PN532/ACR122)", metadata.Description)
		assert.True(t, metadata.DefaultEnabled)
		assert.True(t, metadata.DefaultAutoDetect)
	})

	t.Run("acr122 only mode", func(t *testing.T) {
		t.Parallel()

		reader := NewACR122Reader(&config.Instance{})
		metadata := reader.Metadata()

		assert.Equal(t, "libnfc_acr122", metadata.ID)
		assert.Equal(t, "LibNFC ACR122 USB NFC reader", metadata.Description)
		assert.True(t, metadata.DefaultEnabled)
		assert.True(t, metadata.DefaultAutoDetect)
	})
}

func TestIDs(t *testing.T) {
	t.Parallel()

	t.Run("normal mode", func(t *testing.T) {
		t.Parallel()

		reader := NewReader(&config.Instance{})
		ids := reader.IDs()

		expectedIDs := []string{
			"pn532_uart",
			"pn532_i2c",
			"acr122_usb",
		}

		assert.Equal(t, expectedIDs, ids)
	})

	t.Run("acr122 only mode", func(t *testing.T) {
		t.Parallel()

		reader := NewACR122Reader(&config.Instance{})
		ids := reader.IDs()

		expectedIDs := []string{
			"acr122_usb",
		}

		assert.Equal(t, expectedIDs, ids)
	})
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})
	capabilities := reader.Capabilities()

	require.Len(t, capabilities, 1)
	assert.Equal(t, readers.CapabilityWrite, capabilities[0])
}

func TestOnMediaChange(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})
	err := reader.OnMediaChange(&models.ActiveMedia{})

	assert.NoError(t, err, "OnMediaChange should always return nil")
}

func TestCancelWrite_NoActiveWrite(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})

	// Should not panic when there's no active write
	reader.CancelWrite()
}

func TestIsRetryableError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err       error
		name      string
		retryable bool
	}{
		{
			name:      "transport timeout error",
			err:       &TransportTimeoutError{Device: "test"},
			retryable: true,
		},
		{
			name:      "tag not found error",
			err:       &TagNotFoundError{Device: "test"},
			retryable: true,
		},
		{
			name:      "data corrupted error",
			err:       &DataCorruptedError{Device: "test"},
			retryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := IsRetryableError(tt.err)
			assert.Equal(t, tt.retryable, result)
		})
	}
}

func TestTransportTimeoutError(t *testing.T) {
	t.Parallel()

	err := &TransportTimeoutError{
		Device: "test-device",
		Err:    assert.AnError,
	}

	assert.Contains(t, err.Error(), "test-device")
	assert.Contains(t, err.Error(), "transport timeout")
	assert.True(t, err.IsRetryable())
	assert.Equal(t, assert.AnError, err.Unwrap())
}

func TestTagNotFoundError(t *testing.T) {
	t.Parallel()

	err := &TagNotFoundError{
		Device: "test-device",
		Err:    assert.AnError,
	}

	assert.Contains(t, err.Error(), "test-device")
	assert.Contains(t, err.Error(), "tag not found")
	assert.True(t, err.IsRetryable())
	assert.Equal(t, assert.AnError, err.Unwrap())
}

func TestDataCorruptedError(t *testing.T) {
	t.Parallel()

	err := &DataCorruptedError{
		Device: "test-device",
		Err:    assert.AnError,
	}

	assert.Contains(t, err.Error(), "test-device")
	assert.Contains(t, err.Error(), "data corrupted")
	assert.False(t, err.IsRetryable())
	assert.Equal(t, assert.AnError, err.Unwrap())
}

// TODO: Add mock-based tests for error scenarios:
// - IO errors with active token → sends ReaderError: true
// - Device disconnect scenarios
// - Write operations with context cancellation
// These require mocking libnfc C library
// Consider refactoring to use dependency injection for nfc.Device
