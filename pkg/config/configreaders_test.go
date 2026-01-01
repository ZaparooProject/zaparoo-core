/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
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

// TestIsDriverEnabledForConnect tests the behavior when a driver has a
// [[readers.connect]] entry. The driver is implicitly enabled unless
// explicitly disabled in config.
func TestIsDriverEnabledForConnect(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{
			Readers: Readers{
				Drivers: map[string]DriverConfig{
					"externaldrive": {
						Enabled: boolPtr(false), // explicitly disabled
					},
					"simpleserial": {
						Enabled: boolPtr(true), // explicitly enabled
					},
					// pn532 has no config - not explicitly set (nil)
				},
			},
		},
	}

	tests := []struct {
		name     string
		driver   DriverInfo
		expected bool
	}{
		{
			name: "explicitly disabled driver is blocked",
			driver: DriverInfo{
				ID:             "externaldrive",
				DefaultEnabled: false, // matches real externaldrive
			},
			expected: false,
		},
		{
			name: "explicitly enabled driver is allowed",
			driver: DriverInfo{
				ID:             "simpleserial",
				DefaultEnabled: true,
			},
			expected: true,
		},
		{
			name: "driver with no config is implicitly enabled",
			driver: DriverInfo{
				ID:             "pn532",
				DefaultEnabled: true,
			},
			expected: true,
		},
		{
			name: "driver with DefaultEnabled=false but no config is still enabled for connect",
			driver: DriverInfo{
				ID:             "newdriver",
				DefaultEnabled: false, // would be disabled for auto-detect
			},
			expected: true, // but enabled for connect entries
		},
		{
			name: "normalized ID matches explicit disable",
			driver: DriverInfo{
				ID:             "external_drive",
				DefaultEnabled: false,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := cfg.IsDriverEnabledForConnect(tt.driver)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsDriverEnabledForAutoDetect tests the behavior for auto-detection.
// The driver's DefaultEnabled is used when not explicitly configured.
func TestIsDriverEnabledForAutoDetect(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{
			Readers: Readers{
				Drivers: map[string]DriverConfig{
					"externaldrive": {
						Enabled: boolPtr(true), // explicitly enabled (overrides default)
					},
					"simpleserial": {
						Enabled: boolPtr(false), // explicitly disabled
					},
					// pn532 has no config - uses DefaultEnabled
				},
			},
		},
	}

	tests := []struct {
		name     string
		driver   DriverInfo
		expected bool
	}{
		{
			name: "explicitly enabled overrides DefaultEnabled=false",
			driver: DriverInfo{
				ID:             "externaldrive",
				DefaultEnabled: false,
			},
			expected: true,
		},
		{
			name: "explicitly disabled overrides DefaultEnabled=true",
			driver: DriverInfo{
				ID:             "simpleserial",
				DefaultEnabled: true,
			},
			expected: false,
		},
		{
			name: "no config with DefaultEnabled=true is enabled",
			driver: DriverInfo{
				ID:             "pn532",
				DefaultEnabled: true,
			},
			expected: true,
		},
		{
			name: "no config with DefaultEnabled=false is disabled",
			driver: DriverInfo{
				ID:             "newdriver",
				DefaultEnabled: false,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := cfg.IsDriverEnabledForAutoDetect(tt.driver)
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

func TestIsHoldModeIgnoredSystemFuzzyMatching(t *testing.T) {
	t.Parallel()

	// Create instance with system IDs using aliases in the ignore list
	cfg := &Instance{
		vals: Values{
			Readers: Readers{
				Scan: ReadersScan{
					// Use aliases and different case variations
					IgnoreSystem: []string{
						"megadrive",     // Alias for Genesis (lowercase)
						"N64",           // Alias for Nintendo64
						"playstation",   // Alias for PSX
						"SuperNintendo", // Alias for SNES
					},
				},
			},
		},
	}

	tests := []struct {
		name     string
		systemID string
		expected bool
	}{
		{
			name:     "canonical ID matches alias in config (Genesis via megadrive)",
			systemID: "Genesis",
			expected: true,
		},
		{
			name:     "canonical ID matches alias in config (Nintendo64 via N64)",
			systemID: "Nintendo64",
			expected: true,
		},
		{
			name:     "canonical ID matches alias in config (PSX via playstation)",
			systemID: "PSX",
			expected: true,
		},
		{
			name:     "canonical ID matches alias in config (SNES via SuperNintendo)",
			systemID: "SNES",
			expected: true,
		},
		{
			name:     "system not in ignore list returns false",
			systemID: "NES",
			expected: false,
		},
		{
			name:     "unknown system ID returns false",
			systemID: "UnknownSystem",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := cfg.IsHoldModeIgnoredSystem(tt.systemID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsHoldModeIgnoredSystemWithInvalidConfig(t *testing.T) {
	t.Parallel()

	// Create instance with invalid system IDs in the ignore list
	cfg := &Instance{
		vals: Values{
			Readers: Readers{
				Scan: ReadersScan{
					IgnoreSystem: []string{
						"InvalidSystemID",
						"AnotherBadID",
						"Genesis", // One valid entry
					},
				},
			},
		},
	}

	// Should still match Genesis despite invalid entries
	assert.True(t, cfg.IsHoldModeIgnoredSystem("Genesis"))

	// Invalid entries should not cause matches
	assert.False(t, cfg.IsHoldModeIgnoredSystem("InvalidSystemID"))
}
