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
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestIsReaderEnabled(t *testing.T) {
	t.Parallel()

	falseValue := false
	cfg := &Instance{
		vals: Values{
			Readers: Readers{
				AutoDetect: true,
				Connect: []ReadersConnect{
					{Driver: "newdriver"},
					{Driver: "externaldrive"},
					{Driver: "disabledconn", Enabled: &falseValue},
				},
				Drivers: map[string]DriverConfig{
					"externaldrive": {
						Enabled: boolPtr(false),
					},
					"simple_serial": {
						Enabled: boolPtr(true),
					},
					"auto_off": {
						Enabled:    boolPtr(true),
						AutoDetect: boolPtr(false),
					},
				},
			},
		},
	}

	tests := []struct {
		name    string
		context ReaderEnableContext
		driver  DriverInfo
		want    bool
	}{
		{
			name: "candidate includes default enabled driver",
			driver: DriverInfo{
				ID:             "pn532",
				DefaultEnabled: true,
			},
			context: ReaderEnableContextCandidate,
			want:    true,
		},
		{
			name: "candidate normalizes driver config keys",
			driver: DriverInfo{
				ID:             "simpleserial",
				DefaultEnabled: false,
			},
			context: ReaderEnableContextCandidate,
			want:    true,
		},
		{
			name: "candidate skips default disabled driver without connect entry",
			driver: DriverInfo{
				ID:             "unuseddriver",
				DefaultEnabled: false,
			},
			context: ReaderEnableContextCandidate,
			want:    false,
		},
		{
			name: "candidate includes default disabled driver with connect entry",
			driver: DriverInfo{
				ID:             "newdriver",
				DefaultEnabled: false,
			},
			context: ReaderEnableContextCandidate,
			want:    true,
		},
		{
			name: "candidate skips disabled connect entry",
			driver: DriverInfo{
				ID:             "disabledconn",
				DefaultEnabled: false,
			},
			context: ReaderEnableContextCandidate,
			want:    false,
		},
		{
			name: "candidate respects explicit disable even with connect entry",
			driver: DriverInfo{
				ID:             "external_drive",
				DefaultEnabled: false,
			},
			context: ReaderEnableContextCandidate,
			want:    false,
		},
		{
			name: "manual connect allows default disabled driver",
			driver: DriverInfo{
				ID:             "newdriver",
				DefaultEnabled: false,
			},
			context: ReaderEnableContextManualConnect,
			want:    true,
		},
		{
			name: "manual connect respects explicit disable",
			driver: DriverInfo{
				ID:             "externaldrive",
				DefaultEnabled: false,
			},
			context: ReaderEnableContextManualConnect,
			want:    false,
		},
		{
			name: "auto-detect includes default enabled driver",
			driver: DriverInfo{
				ID:                "pn532",
				DefaultEnabled:    true,
				DefaultAutoDetect: true,
			},
			context: ReaderEnableContextAutoDetect,
			want:    true,
		},
		{
			name: "auto-detect ignores manual connect enablement",
			driver: DriverInfo{
				ID:                "newdriver",
				DefaultEnabled:    false,
				DefaultAutoDetect: true,
			},
			context: ReaderEnableContextAutoDetect,
			want:    false,
		},
		{
			name: "auto-detect respects explicit driver enable",
			driver: DriverInfo{
				ID:                "simpleserial",
				DefaultEnabled:    false,
				DefaultAutoDetect: true,
			},
			context: ReaderEnableContextAutoDetect,
			want:    true,
		},
		{
			name: "auto-detect respects explicit auto-detect disable",
			driver: DriverInfo{
				ID:                "autooff",
				DefaultEnabled:    true,
				DefaultAutoDetect: true,
			},
			context: ReaderEnableContextAutoDetect,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := cfg.IsReaderEnabled(tt.driver, tt.context)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestReadersConnect_IsEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rc       ReadersConnect
		expected bool
	}{
		{
			name:     "nil enabled means enabled (default)",
			rc:       ReadersConnect{Driver: "pn532"},
			expected: true,
		},
		{
			name:     "explicit true means enabled",
			rc:       ReadersConnect{Driver: "pn532", Enabled: boolPtr(true)},
			expected: true,
		},
		{
			name:     "explicit false means disabled",
			rc:       ReadersConnect{Driver: "pn532", Enabled: boolPtr(false)},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.rc.IsEnabled())
		})
	}
}

func TestReadersConnect_EnabledRoundTrip(t *testing.T) {
	t.Parallel()

	memFs := afero.NewMemMapFs()
	configDir := "/config"
	cfg, err := NewConfigWithFs(configDir, BaseDefaults, memFs)
	require.NoError(t, err)

	// Set connections with mixed enabled states
	cfg.SetReaderConnections([]ReadersConnect{
		{Driver: "pn532", Path: "/dev/ttyUSB0"},
		{Driver: "externaldrive", Enabled: boolPtr(true)},
		{Driver: "libnfc", Enabled: boolPtr(false)},
	})

	err = cfg.Save()
	require.NoError(t, err)

	err = cfg.Load()
	require.NoError(t, err)

	readers := cfg.Readers().Connect
	require.Len(t, readers, 3)

	assert.Nil(t, readers[0].Enabled, "nil should survive round-trip")
	assert.Equal(t, "pn532", readers[0].Driver)

	require.NotNil(t, readers[1].Enabled)
	assert.True(t, *readers[1].Enabled, "explicit true should survive round-trip")
	assert.Equal(t, "externaldrive", readers[1].Driver)

	require.NotNil(t, readers[2].Enabled)
	assert.False(t, *readers[2].Enabled, "explicit false should survive round-trip")
	assert.Equal(t, "libnfc", readers[2].Driver)
}

func TestReadersConnect_EnabledFromTOML(t *testing.T) {
	t.Parallel()

	memFs := afero.NewMemMapFs()
	configDir := "/config"
	cfg, err := NewConfigWithFs(configDir, BaseDefaults, memFs)
	require.NoError(t, err)

	// Write config with enabled = true in [[readers.connect]] (matches docs)
	cfgPath := filepath.Join(configDir, CfgFile)
	content := `config_schema = 1

[readers]
auto_detect = true

[[readers.connect]]
driver = "externaldrive"
enabled = true

[readers.scan]
mode = 'tap'
`
	err = afero.WriteFile(memFs, cfgPath, []byte(content), 0o600)
	require.NoError(t, err)

	err = cfg.Load()
	require.NoError(t, err)

	readers := cfg.Readers().Connect
	require.Len(t, readers, 1)
	assert.Equal(t, "externaldrive", readers[0].Driver)
	require.NotNil(t, readers[0].Enabled, "enabled should be parsed from TOML")
	assert.True(t, *readers[0].Enabled)

	// Save and verify enabled survives
	err = cfg.Save()
	require.NoError(t, err)

	saved, err := afero.ReadFile(memFs, cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(saved), "enabled = true",
		"enabled = true should survive Load/Save round-trip")
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

func TestLaunchGuardTimeout_DefaultWhenZero(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	assert.InDelta(t, DefaultLaunchGuardTimeout, cfg.LaunchGuardTimeout(), 0)
}

func TestLaunchGuardTimeout_CustomValue(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{
			Readers: Readers{
				Scan: ReadersScan{
					LaunchGuard: ScanLaunchGuard{Timeout: 30},
				},
			},
		},
	}
	assert.InDelta(t, 30, cfg.LaunchGuardTimeout(), 0)
}

func TestLaunchGuardTimeout_NegativeDisablesTimeout(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	cfg.SetLaunchGuardTimeout(-1)
	assert.InDelta(t, -1, cfg.LaunchGuardTimeout(), 0)
}

func TestLaunchGuardSettersAndGetters(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}

	assert.False(t, cfg.LaunchGuardEnabled())
	assert.False(t, cfg.LaunchGuardRequireConfirm())

	cfg.SetLaunchGuard(true)
	cfg.SetLaunchGuardRequireConfirm(true)

	assert.True(t, cfg.LaunchGuardEnabled())
	assert.True(t, cfg.LaunchGuardRequireConfirm())

	cfg.SetLaunchGuard(false)
	assert.False(t, cfg.LaunchGuardEnabled())
}

func TestLaunchGuardDelay_DefaultZero(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	assert.InDelta(t, 0, cfg.LaunchGuardDelay(), 0)
}

func TestLaunchGuardDelay_CustomValue(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{
			Readers: Readers{
				Scan: ReadersScan{
					LaunchGuard: ScanLaunchGuard{
						Timeout: 15,
						Delay:   5,
					},
				},
			},
		},
	}
	assert.InDelta(t, 5, cfg.LaunchGuardDelay(), 0)
}

func TestLaunchGuardDelay_ClampedWhenExceedsTimeout(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{
			Readers: Readers{
				Scan: ReadersScan{
					LaunchGuard: ScanLaunchGuard{
						Timeout: 10,
						Delay:   10,
					},
				},
			},
		},
	}
	assert.InDelta(t, 5, cfg.LaunchGuardDelay(), 0)
}

func TestLaunchGuardDelay_ClampedWithDefaultTimeout(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{
			Readers: Readers{
				Scan: ReadersScan{
					LaunchGuard: ScanLaunchGuard{
						Delay: 20,
					},
				},
			},
		},
	}
	// timeout defaults to 15, delay 20 >= 15, so clamped to 15/2 = 7.5
	assert.InDelta(t, 7.5, cfg.LaunchGuardDelay(), 0)
}

func TestLaunchGuardDelay_NegativeTimeoutReturnsZero(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{
			Readers: Readers{
				Scan: ReadersScan{
					LaunchGuard: ScanLaunchGuard{
						Timeout: -1,
						Delay:   5,
					},
				},
			},
		},
	}
	assert.InDelta(t, 0, cfg.LaunchGuardDelay(), 0)
}

func TestLaunchGuardDelay_SetterAndGetter(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	cfg.SetLaunchGuardTimeout(20)
	cfg.SetLaunchGuardDelay(8)
	assert.InDelta(t, 8, cfg.LaunchGuardDelay(), 0)
}

func TestScanModeForReader(t *testing.T) {
	t.Parallel()

	newCfg := func(globalMode string) *Instance {
		return &Instance{
			vals: Values{
				Readers: Readers{
					Scan: ReadersScan{Mode: globalMode},
					Connect: []ReadersConnect{
						{Driver: "pn532", Path: "/dev/ttyUSB0"},
						{Driver: "pn532", Path: "/dev/ttyUSB1", ScanMode: ScanModeHold},
						{Driver: "simple_serial", Path: "/dev/ttyACM0", ScanMode: ScanModeTap},
						{Driver: "opticaldrive", Path: "/dev/sr0", ScanMode: "sideways"},
						{Driver: "pn532_uart", Path: "/dev/ttyUSB2", ScanMode: ScanModeHold},
					},
					Drivers: map[string]DriverConfig{
						"pn532":         {ScanMode: ScanModeTap},
						"opticaldrive":  {ScanMode: ScanModeHold},
						"simpleserial":  {ScanMode: ScanModeHold},
						"acr122pcsc":    {ScanMode: ""},
						"externaldrive": {ScanMode: "HOLD"},
						"libnfcacr122":  {ScanMode: ScanModeHold},
					},
				},
			},
		}
	}

	tests := []struct {
		name       string
		globalMode string
		path       string
		want       string
		driverIDs  []string
	}{
		{
			name:       "connect entry overrides driver and global",
			globalMode: ScanModeTap,
			driverIDs:  []string{"pn532"},
			path:       "/dev/ttyUSB1",
			want:       ScanModeHold,
		},
		{
			name:       "driver applies when connect entry sets nothing",
			globalMode: ScanModeHold,
			driverIDs:  []string{"pn532"},
			path:       "/dev/ttyUSB0",
			want:       ScanModeTap,
		},
		{
			name:       "connect entry matches underscored driver key",
			globalMode: ScanModeHold,
			driverIDs:  []string{"simpleserial"},
			path:       "/dev/ttyACM0",
			want:       ScanModeTap,
		},
		{
			name:       "driver key normalizes against underscored config key",
			globalMode: ScanModeTap,
			driverIDs:  []string{"simple_serial"},
			path:       "/dev/ttyACM9",
			want:       ScanModeHold,
		},
		{
			name:       "unmatched path falls through to driver",
			globalMode: ScanModeTap,
			driverIDs:  []string{"opticaldrive"},
			path:       "/dev/sr9",
			want:       ScanModeHold,
		},
		{
			name:       "unrecognised connect value falls through to driver",
			globalMode: ScanModeTap,
			driverIDs:  []string{"opticaldrive"},
			path:       "/dev/sr0",
			want:       ScanModeHold,
		},
		{
			name:       "empty driver value falls through to global",
			globalMode: ScanModeHold,
			driverIDs:  []string{"acr122pcsc"},
			path:       "/dev/acr",
			want:       ScanModeHold,
		},
		{
			name:       "driver value is case insensitive",
			globalMode: ScanModeTap,
			driverIDs:  []string{"externaldrive"},
			path:       "/mnt/usb",
			want:       ScanModeHold,
		},
		{
			name:       "unknown driver falls through to global",
			globalMode: ScanModeHold,
			driverIDs:  []string{"mqtt"},
			path:       "broker:1883",
			want:       ScanModeHold,
		},
		{
			name:       "empty global mode defaults to tap",
			globalMode: "",
			driverIDs:  []string{"mqtt"},
			path:       "broker:1883",
			want:       ScanModeTap,
		},
		{
			name:       "unrecognised global mode defaults to tap",
			globalMode: "sideways",
			driverIDs:  []string{"mqtt"},
			path:       "broker:1883",
			want:       ScanModeTap,
		},
		{
			// A reader opens under whichever spelling the config used, so an
			// entry written with an alias has to reach it too.
			name:       "connect entry written with a driver alias",
			globalMode: ScanModeTap,
			driverIDs:  []string{"pn532", "pn532uart", "pn532_uart", "pn532i2c"},
			path:       "/dev/ttyUSB2",
			want:       ScanModeHold,
		},
		{
			// libnfc in ACR122 mode reports a canonical ID its own alias list
			// does not contain, so matching on the alias list alone loses it.
			name:       "driver entry written with the canonical ID",
			globalMode: ScanModeTap,
			driverIDs:  []string{"libnfcacr122", "acr122usb", "acr122_usb"},
			path:       "/dev/acr122",
			want:       ScanModeHold,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := newCfg(tt.globalMode)
			assert.Equal(t, tt.want, cfg.ScanModeForReader(tt.driverIDs, tt.path))
			assert.Equal(t, tt.want == ScanModeHold,
				cfg.HoldModeEnabledForReader(tt.driverIDs, tt.path))
		})
	}
}

// Resolving a per-reader override must never write back to the global setting,
// which stays the single value the settings API reads and writes.
func TestScanModeForReaderLeavesGlobalUntouched(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{
			Readers: Readers{
				Scan:    ReadersScan{Mode: ScanModeTap},
				Connect: []ReadersConnect{{Driver: "pn532", Path: "/dev/ttyUSB0", ScanMode: ScanModeHold}},
				Drivers: map[string]DriverConfig{"opticaldrive": {ScanMode: ScanModeHold}},
			},
		},
	}

	for range 5 {
		cfg.ScanModeForReader([]string{"pn532"}, "/dev/ttyUSB0")
		cfg.ScanModeForReader([]string{"opticaldrive"}, "/dev/sr0")
		cfg.HoldModeEnabledForReader([]string{"pn532"}, "/dev/ttyUSB0")
	}

	assert.Equal(t, ScanModeTap, cfg.ReadersScan().Mode)
	assert.False(t, cfg.HoldModeEnabled())
	assert.True(t, cfg.TapModeEnabled())
}

func TestScanModeTOMLRoundTrip(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	configDir := filepath.Join(t.TempDir(), "zaparoo")
	cfg, err := NewConfigWithFs(configDir, BaseDefaults, fs)
	require.NoError(t, err)

	require.NoError(t, cfg.LoadTOML(`
[readers.scan]
mode = "tap"

[readers.drivers.opticaldrive]
scan_mode = "hold"

[[readers.connect]]
driver = "pn532"
path = "/dev/ttyUSB1"
scan_mode = "hold"
`))

	require.NoError(t, cfg.Save())
	require.NoError(t, cfg.Load())

	assert.Equal(t, ScanModeTap, cfg.ReadersScan().Mode)
	assert.Equal(t, ScanModeHold, cfg.ScanModeForReader([]string{"opticaldrive"}, "/dev/sr0"))
	assert.Equal(t, ScanModeHold, cfg.ScanModeForReader([]string{"pn532"}, "/dev/ttyUSB1"))
	assert.Equal(t, ScanModeTap, cfg.ScanModeForReader([]string{"pn532"}, "/dev/ttyUSB0"))
}

// TapModeEnabled and GlobalScanMode read the same setting and must agree.
// Exact matching made "TAP" report neither tap nor hold, and an unrecognised
// value report hold while GlobalScanMode fell back to tap.
func TestTapModeEnabled_MatchesGlobalScanMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"", "tap", "TAP", " tap ", "hold", "HOLD", "sideways"} {
		cfg, err := NewConfig(t.TempDir(), BaseDefaults)
		require.NoError(t, err)
		cfg.SetScanMode(mode)

		assert.Equal(t, cfg.GlobalScanMode() == ScanModeTap, cfg.TapModeEnabled(),
			"TapModeEnabled disagrees with GlobalScanMode for %q", mode)
		assert.Equal(t, cfg.GlobalScanMode() == ScanModeHold, cfg.HoldModeEnabled(),
			"HoldModeEnabled disagrees with GlobalScanMode for %q", mode)
	}
}

// A driver ID is something a user types into a config file, so it is matched
// the way the rest of the file's identifiers are: neither case nor underscores
// are part of the name. Only underscores were folded before, so a
// [readers.drivers.PN532] section, or a connect entry naming "PN532", silently
// matched nothing at all.
func TestDriverIDMatchingIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	newCfg := func(t *testing.T, toml string) *Instance {
		t.Helper()
		fs := afero.NewMemMapFs()
		cfg, err := NewConfigWithFs(filepath.Join(t.TempDir(), "zaparoo"), BaseDefaults, fs)
		require.NoError(t, err)
		require.NoError(t, cfg.LoadTOML(toml))
		return cfg
	}

	t.Run("drivers section key", func(t *testing.T) {
		t.Parallel()
		cfg := newCfg(t, `
[readers.scan]
mode = "tap"

[readers.drivers.PN532]
scan_mode = "hold"
`)
		assert.Equal(t, ScanModeHold, cfg.ScanModeForReader([]string{"pn532"}, "/dev/ttyUSB0"))
	})

	t.Run("drivers section key with case and an underscore", func(t *testing.T) {
		t.Parallel()
		cfg := newCfg(t, `
[readers.scan]
mode = "tap"

[readers.drivers.Simple_Serial]
scan_mode = "hold"
`)
		assert.Equal(t, ScanModeHold, cfg.ScanModeForReader([]string{"simpleserial"}, "/dev/ttyACM0"))
	})

	t.Run("connect entry driver", func(t *testing.T) {
		t.Parallel()
		cfg := newCfg(t, `
[readers.scan]
mode = "tap"

[[readers.connect]]
driver = "PN532"
path = "/dev/ttyUSB0"
scan_mode = "hold"
`)
		assert.Equal(t, ScanModeHold, cfg.ScanModeForReader([]string{"pn532"}, "/dev/ttyUSB0"))
	})

	// The enabled/auto-detect lookups read the same key, so they have to agree
	// with the scan-mode lookup about which section names which driver.
	t.Run("enabled lookup", func(t *testing.T) {
		t.Parallel()
		cfg := newCfg(t, `
[readers.drivers.PN532]
enabled = false
`)
		driver := DriverInfo{ID: "pn532", DefaultEnabled: true}
		assert.False(t, cfg.IsReaderEnabled(driver, ReaderEnableContextCandidate),
			"a mixed-case drivers key must disable the driver it names")
	})

	t.Run("auto-detect lookup", func(t *testing.T) {
		t.Parallel()
		cfg := newCfg(t, `
[readers.drivers.PN532]
auto_detect = false
`)
		driver := DriverInfo{ID: "pn532", DefaultEnabled: true, DefaultAutoDetect: true}
		assert.False(t, cfg.IsReaderEnabled(driver, ReaderEnableContextAutoDetect),
			"a mixed-case drivers key must stop the driver it names auto-detecting")
	})
}

func TestConnectionStringFoldsCase(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "pn532:/dev/ttyUSB0",
		ReadersConnect{Driver: "PN532", Path: "/dev/ttyUSB0"}.ConnectionString())
	assert.Equal(t, "simpleserial:/dev/ttyACM0",
		ReadersConnect{Driver: "Simple_Serial", Path: "/dev/ttyACM0"}.ConnectionString())
}
