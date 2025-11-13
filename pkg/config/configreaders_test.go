/*
Zaparoo Core
Copyright (c) 2025 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConnectionStringNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rc       ReadersConnect
		expected string
	}{
		{
			name: "simple_serial normalizes to simpleserial",
			rc: ReadersConnect{
				Driver: "simple_serial",
				Path:   "/dev/ttyUSB0",
			},
			expected: "simpleserial:/dev/ttyUSB0",
		},
		{
			name: "acr122_pcsc normalizes to acr122pcsc",
			rc: ReadersConnect{
				Driver: "acr122_pcsc",
				Path:   "/dev/usb1",
			},
			expected: "acr122pcsc:/dev/usb1",
		},
		{
			name: "already normalized stays the same",
			rc: ReadersConnect{
				Driver: "simpleserial",
				Path:   "/dev/ttyUSB0",
			},
			expected: "simpleserial:/dev/ttyUSB0",
		},
		{
			name: "legacy_pn532_uart normalizes",
			rc: ReadersConnect{
				Driver: "legacy_pn532_uart",
				Path:   "/dev/ttyACM0",
			},
			expected: "legacypn532uart:/dev/ttyACM0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tt.rc.ConnectionString()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsDriverEnabledNormalization(t *testing.T) {
	t.Parallel()

	// Create instance with old underscore format in config
	cfg := &Instance{
		vals: Values{
			Readers: Readers{
				Drivers: map[string]DriverConfig{
					"simple_serial": {
						Enabled: boolPtr(true),
					},
					"acr122_pcsc": {
						Enabled: boolPtr(false),
					},
				},
			},
		},
	}

	tests := []struct {
		name           string
		driverID       string
		defaultEnabled bool
		expected       bool
	}{
		{
			name:           "lookup with new format finds old config",
			driverID:       "simpleserial",
			defaultEnabled: false,
			expected:       true,
		},
		{
			name:           "lookup with old format finds old config",
			driverID:       "simple_serial",
			defaultEnabled: false,
			expected:       true,
		},
		{
			name:           "disabled driver returns false",
			driverID:       "acr122pcsc",
			defaultEnabled: true,
			expected:       false,
		},
		{
			name:           "missing driver returns default",
			driverID:       "pn532uart",
			defaultEnabled: true,
			expected:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := cfg.IsDriverEnabled(tt.driverID, tt.defaultEnabled)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsDriverAutoDetectEnabledNormalization(t *testing.T) {
	t.Parallel()

	// Create instance with old underscore format in config
	cfg := &Instance{
		vals: Values{
			Readers: Readers{
				AutoDetect: true,
				Drivers: map[string]DriverConfig{
					"simple_serial": {
						AutoDetect: boolPtr(false),
					},
					"acr122_pcsc": {
						AutoDetect: boolPtr(true),
					},
				},
			},
		},
	}

	tests := []struct {
		name              string
		driverID          string
		defaultAutoDetect bool
		expected          bool
	}{
		{
			name:              "lookup with new format finds old config",
			driverID:          "simpleserial",
			defaultAutoDetect: true,
			expected:          false,
		},
		{
			name:              "lookup with old format finds old config",
			driverID:          "simple_serial",
			defaultAutoDetect: true,
			expected:          false,
		},
		{
			name:              "enabled driver returns true",
			driverID:          "acr122pcsc",
			defaultAutoDetect: false,
			expected:          true,
		},
		{
			name:              "missing driver returns global auto detect",
			driverID:          "pn532uart",
			defaultAutoDetect: true,
			expected:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := cfg.IsDriverAutoDetectEnabled(tt.driverID, tt.defaultAutoDetect)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}
