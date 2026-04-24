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
	assert.Equal(t, modeACR122Only, reader.mode)
}

func TestNewLegacyUARTReader(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewLegacyUARTReader(cfg)

	assert.NotNil(t, reader)
	assert.Equal(t, cfg, reader.cfg)
	assert.Equal(t, modeLegacyUART, reader.mode)
}

func TestNewLegacyI2CReader(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewLegacyI2CReader(cfg)

	assert.NotNil(t, reader)
	assert.Equal(t, cfg, reader.cfg)
	assert.Equal(t, modeLegacyI2C, reader.mode)
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

		assert.Equal(t, "libnfcacr122", metadata.ID)
		assert.Equal(t, "LibNFC ACR122 USB NFC reader", metadata.Description)
		assert.True(t, metadata.DefaultEnabled)
		assert.True(t, metadata.DefaultAutoDetect)
	})

	t.Run("legacy uart mode", func(t *testing.T) {
		t.Parallel()

		reader := NewLegacyUARTReader(&config.Instance{})
		metadata := reader.Metadata()

		assert.Equal(t, "legacypn532uart", metadata.ID)
		assert.Equal(t, "Legacy PN532 UART reader via LibNFC", metadata.Description)
		assert.True(t, metadata.DefaultEnabled)
		assert.False(t, metadata.DefaultAutoDetect)
	})

	t.Run("legacy i2c mode", func(t *testing.T) {
		t.Parallel()

		reader := NewLegacyI2CReader(&config.Instance{})
		metadata := reader.Metadata()

		assert.Equal(t, "legacypn532i2c", metadata.ID)
		assert.Equal(t, "Legacy PN532 I2C reader via LibNFC", metadata.Description)
		assert.True(t, metadata.DefaultEnabled)
		assert.False(t, metadata.DefaultAutoDetect)
	})
}

func TestIDs(t *testing.T) {
	t.Parallel()

	t.Run("normal mode", func(t *testing.T) {
		t.Parallel()

		reader := NewReader(&config.Instance{})
		ids := reader.IDs()

		expectedIDs := []string{
			"pn532uart",
			"pn532_uart",
			"pn532i2c",
			"pn532_i2c",
			"acr122usb",
			"acr122_usb",
		}

		assert.Equal(t, expectedIDs, ids)
	})

	t.Run("acr122 only mode", func(t *testing.T) {
		t.Parallel()

		reader := NewACR122Reader(&config.Instance{})
		ids := reader.IDs()

		expectedIDs := []string{
			"acr122usb",
			"acr122_usb",
		}

		assert.Equal(t, expectedIDs, ids)
	})

	t.Run("legacy uart mode", func(t *testing.T) {
		t.Parallel()

		reader := NewLegacyUARTReader(&config.Instance{})
		ids := reader.IDs()

		expectedIDs := []string{
			"legacypn532uart",
			"legacy_pn532_uart",
		}

		assert.Equal(t, expectedIDs, ids)
	})

	t.Run("legacy i2c mode", func(t *testing.T) {
		t.Parallel()

		reader := NewLegacyI2CReader(&config.Instance{})
		ids := reader.IDs()

		expectedIDs := []string{
			"legacypn532i2c",
			"legacy_pn532_i2c",
		}

		assert.Equal(t, expectedIDs, ids)
	})
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})
	capabilities := reader.Capabilities()

	require.Len(t, capabilities, 2)
	assert.Contains(t, capabilities, readers.CapabilityWrite)
	assert.Contains(t, capabilities, readers.CapabilityRemovable)
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

func TestConnected(t *testing.T) {
	t.Parallel()

	t.Run("not connected by default", func(t *testing.T) {
		t.Parallel()
		r := NewReader(&config.Instance{})
		assert.False(t, r.Connected())
	})

	t.Run("not connected when only polling is true", func(t *testing.T) {
		t.Parallel()
		r := NewReader(&config.Instance{})
		r.polling = true
		assert.False(t, r.Connected())
	})
}

func TestInfo(t *testing.T) {
	t.Parallel()

	t.Run("empty when not connected", func(t *testing.T) {
		t.Parallel()
		r := NewReader(&config.Instance{})
		assert.Empty(t, r.Info())
	})

	t.Run("empty when polling but no device", func(t *testing.T) {
		t.Parallel()
		r := NewReader(&config.Instance{})
		r.polling = true
		assert.Empty(t, r.Info())
	})
}

func TestPath(t *testing.T) {
	t.Parallel()

	r := NewReader(&config.Instance{})
	r.conn = config.ReadersConnect{
		Driver: "pn532_i2c",
		Path:   "/dev/i2c-2",
	}
	assert.Equal(t, "/dev/i2c-2", r.Path())
}

func TestClose_NilDevice(t *testing.T) {
	t.Parallel()

	r := NewReader(&config.Instance{})
	err := r.Close()
	require.NoError(t, err)
	assert.False(t, r.polling)
}

func TestReaderID(t *testing.T) {
	t.Parallel()

	t.Run("uses connection string from conn", func(t *testing.T) {
		t.Parallel()
		r := NewReader(&config.Instance{})
		r.conn = config.ReadersConnect{
			Driver: "pn532_i2c",
			Path:   "/dev/i2c-2",
		}
		id := r.ReaderID()
		assert.NotEmpty(t, id)
	})

	t.Run("empty conn yields deterministic id", func(t *testing.T) {
		t.Parallel()
		r := NewReader(&config.Instance{})
		id := r.ReaderID()
		assert.NotEmpty(t, id)
	})

	t.Run("auto conn with nil pnd uses auto string", func(t *testing.T) {
		t.Parallel()
		r := NewReader(&config.Instance{})
		r.conn = config.ReadersConnect{
			Driver: "libnfcauto",
			Path:   "",
		}
		id := r.ReaderID()
		assert.NotEmpty(t, id)
	})
}

func TestValidateWriteParameters(t *testing.T) {
	t.Parallel()

	t.Run("nil reader", func(t *testing.T) {
		t.Parallel()
		err := validateWriteParameters(nil, "test")
		assert.EqualError(t, err, "reader cannot be nil")
	})

	t.Run("not connected", func(t *testing.T) {
		t.Parallel()
		r := NewReader(&config.Instance{})
		err := validateWriteParameters(r, "test")
		assert.EqualError(t, err, "reader not connected")
	})

	t.Run("empty text", func(t *testing.T) {
		t.Parallel()
		r := NewReader(&config.Instance{})
		r.polling = true
		// Can't set pnd to non-nil without real device, so this hits
		// "reader not connected" first. Test the empty text path via
		// the nil reader and not-connected paths above.
		err := validateWriteParameters(r, "")
		assert.Error(t, err)
	})
}

// TODO: Add mock-based tests for error scenarios:
// - IO errors with active token → sends ReaderError: true
// - Device disconnect scenarios
// - Write operations with context cancellation
// These require mocking libnfc C library
// Consider refactoring to use dependency injection for nfc.Device

// TestOpenConnectionStringTranslation verifies that legacy connection strings
// are correctly translated to libnfc format by validating the reader mode
func TestOpenConnectionStringTranslation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reader       *Reader
		name         string
		expectedMode readerMode
	}{
		{
			name:         "legacy uart mode",
			reader:       NewLegacyUARTReader(&config.Instance{}),
			expectedMode: modeLegacyUART,
		},
		{
			name:         "legacy i2c mode",
			reader:       NewLegacyI2CReader(&config.Instance{}),
			expectedMode: modeLegacyI2C,
		},
		{
			name:         "normal mode",
			reader:       NewReader(&config.Instance{}),
			expectedMode: modeAll,
		},
		{
			name:         "acr122 mode",
			reader:       NewACR122Reader(&config.Instance{}),
			expectedMode: modeACR122Only,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Verify the reader was created with the correct mode
			// which determines how connection strings are translated
			assert.Equal(t, tt.expectedMode, tt.reader.mode)
		})
	}
}

// TestToLibnfcConnStr verifies that internal connection strings are translated
// to the libnfc format with underscored driver names (e.g. "pn532_i2c").
// Regression test: libnfc requires underscores in driver names but the internal
// ConnectionString() normalization strips them, causing nfc.Open() to fail with
// "cannot open NFC device".
func TestToLibnfcConnStr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
		mode     readerMode
	}{
		// Legacy I2C mode — the primary bug that was reported
		{
			name:     "legacy i2c from config",
			mode:     modeLegacyI2C,
			input:    "legacypn532i2c:/dev/i2c-2",
			expected: "pn532_i2c:/dev/i2c-2",
		},
		{
			name:     "legacy i2c bus 1",
			mode:     modeLegacyI2C,
			input:    "legacypn532i2c:/dev/i2c-1",
			expected: "pn532_i2c:/dev/i2c-1",
		},

		// Legacy UART mode
		{
			name:     "legacy uart from config",
			mode:     modeLegacyUART,
			input:    "legacypn532uart:/dev/ttyUSB0",
			expected: "pn532_uart:/dev/ttyUSB0",
		},
		{
			name:     "legacy uart ttyACM device",
			mode:     modeLegacyUART,
			input:    "legacypn532uart:/dev/ttyACM0",
			expected: "pn532_uart:/dev/ttyACM0",
		},

		// Default mode (non-legacy libnfc reader)
		{
			name:     "default mode i2c",
			mode:     modeAll,
			input:    "pn532i2c:/dev/i2c-2",
			expected: "pn532_i2c:/dev/i2c-2",
		},
		{
			name:     "default mode uart",
			mode:     modeAll,
			input:    "pn532uart:/dev/ttyUSB0",
			expected: "pn532_uart:/dev/ttyUSB0",
		},

		// Passthrough cases — strings that shouldn't be changed
		{
			name:     "acr122 mode passthrough",
			mode:     modeACR122Only,
			input:    "libnfcauto:",
			expected: "libnfcauto:",
		},
		{
			name:     "empty string passthrough",
			mode:     modeAll,
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := toLibnfcConnStr(tt.mode, tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestToLibnfcConnStrEndToEnd verifies the full pipeline from user config
// (ReadersConnect) through ConnectionString() normalization to libnfc format.
func TestToLibnfcConnStrEndToEnd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		device   config.ReadersConnect
		name     string
		expected string
		mode     readerMode
	}{
		{
			name: "legacy_pn532_i2c config to libnfc",
			mode: modeLegacyI2C,
			device: config.ReadersConnect{
				Driver: "legacy_pn532_i2c",
				Path:   "/dev/i2c-2",
			},
			expected: "pn532_i2c:/dev/i2c-2",
		},
		{
			name: "legacypn532i2c config to libnfc",
			mode: modeLegacyI2C,
			device: config.ReadersConnect{
				Driver: "legacypn532i2c",
				Path:   "/dev/i2c-2",
			},
			expected: "pn532_i2c:/dev/i2c-2",
		},
		{
			name: "legacy_pn532_uart config to libnfc",
			mode: modeLegacyUART,
			device: config.ReadersConnect{
				Driver: "legacy_pn532_uart",
				Path:   "/dev/ttyUSB0",
			},
			expected: "pn532_uart:/dev/ttyUSB0",
		},
		{
			name: "legacypn532uart config to libnfc",
			mode: modeLegacyUART,
			device: config.ReadersConnect{
				Driver: "legacypn532uart",
				Path:   "/dev/ttyUSB0",
			},
			expected: "pn532_uart:/dev/ttyUSB0",
		},
		{
			name: "pn532_i2c config to libnfc (default mode)",
			mode: modeAll,
			device: config.ReadersConnect{
				Driver: "pn532_i2c",
				Path:   "/dev/i2c-1",
			},
			expected: "pn532_i2c:/dev/i2c-1",
		},
		{
			name: "pn532_uart config to libnfc (default mode)",
			mode: modeAll,
			device: config.ReadersConnect{
				Driver: "pn532_uart",
				Path:   "/dev/ttyUSB0",
			},
			expected: "pn532_uart:/dev/ttyUSB0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			connStr := tt.device.ConnectionString()
			result := toLibnfcConnStr(tt.mode, connStr)
			assert.Equal(t, tt.expected, result)
		})
	}
}
