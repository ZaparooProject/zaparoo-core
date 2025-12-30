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

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckAllow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		allow    []string
		allowRe  []*regexp.Regexp
		expected bool
	}{
		{
			name:     "empty input returns false",
			allow:    []string{".*"},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(".*")},
			input:    "",
			expected: false,
		},
		{
			name:     "nil regex returns false",
			allow:    []string{"test"},
			allowRe:  []*regexp.Regexp{nil},
			input:    "test",
			expected: false,
		},
		{
			name:     "exact match",
			allow:    []string{"test"},
			allowRe:  []*regexp.Regexp{regexp.MustCompile("^test$")},
			input:    "test",
			expected: true,
		},
		{
			name:     "partial match with regex",
			allow:    []string{"test.*"},
			allowRe:  []*regexp.Regexp{regexp.MustCompile("test.*")},
			input:    "test123",
			expected: true,
		},
		{
			name:     "no match",
			allow:    []string{"test"},
			allowRe:  []*regexp.Regexp{regexp.MustCompile("^test$")},
			input:    "different",
			expected: false,
		},
		{
			name:     "multiple patterns first matches",
			allow:    []string{"test", "other"},
			allowRe:  []*regexp.Regexp{regexp.MustCompile("test"), regexp.MustCompile("other")},
			input:    "test123",
			expected: true,
		},
		{
			name:     "multiple patterns second matches",
			allow:    []string{"test", "other"},
			allowRe:  []*regexp.Regexp{regexp.MustCompile("test"), regexp.MustCompile("other")},
			input:    "other456",
			expected: true,
		},
		{
			name:     "multiple patterns none match",
			allow:    []string{"test", "other"},
			allowRe:  []*regexp.Regexp{regexp.MustCompile("^test$"), regexp.MustCompile("^other$")},
			input:    "nomatch",
			expected: false,
		},
		{
			name:     "case sensitive match",
			allow:    []string{"Test"},
			allowRe:  []*regexp.Regexp{regexp.MustCompile("Test")},
			input:    "test",
			expected: false,
		},
		{
			name:     "case insensitive match",
			allow:    []string{"(?i)test"},
			allowRe:  []*regexp.Regexp{regexp.MustCompile("(?i)test")},
			input:    "TEST",
			expected: true,
		},
		{
			name:     "wildcard pattern",
			allow:    []string{".*\\.exe"},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`.*\.exe`)},
			input:    "program.exe",
			expected: true,
		},
		{
			name:     "path pattern with forward slashes",
			allow:    []string{"/home/user/.*"},
			allowRe:  []*regexp.Regexp{regexp.MustCompile("/home/user/.*")},
			input:    "/home/user/file.txt",
			expected: true,
		},
		{
			name:     "path pattern with backslashes",
			allow:    []string{"C:\\\\Users\\\\.*"},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`C:\\Users\\.*`)},
			input:    "C:\\Users\\file.txt",
			expected: true,
		},
		{
			name:     "empty allow list",
			allow:    []string{},
			allowRe:  []*regexp.Regexp{},
			input:    "anything",
			expected: false,
		},
		{
			name:     "mixed nil and valid regexes",
			allow:    []string{"invalid", "valid"},
			allowRe:  []*regexp.Regexp{nil, regexp.MustCompile("valid")},
			input:    "valid",
			expected: true,
		},
		{
			name:     "non-windows style path - no normalization needed",
			allow:    []string{"/usr/local/.*"},
			allowRe:  []*regexp.Regexp{regexp.MustCompile("/usr/local/.*")},
			input:    "/usr/local/bin/app",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := checkAllow(tt.allow, tt.allowRe, tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCheckAllowWindowsPathNormalization(t *testing.T) {
	t.Parallel()

	// Test Windows path normalization behavior
	tests := []struct {
		osExpected map[string]bool
		name       string
		input      string
		allow      []string
		allowRe    []*regexp.Regexp
		expected   bool
	}{
		{
			name:    "windows forward slash should normalize to backslash on Windows",
			allow:   []string{"C:\\\\root\\\\.*\\.lnk"},
			allowRe: []*regexp.Regexp{regexp.MustCompile(`C:\\root\\.*\.lnk`)},
			input:   "C:/root/notepad.lnk",
			osExpected: map[string]bool{
				"windows": true,  // Should match after normalization
				"linux":   false, // No normalization on Linux
				"darwin":  false, // No normalization on macOS
			},
		},
		{
			name:    "windows mixed separators should normalize on Windows",
			allow:   []string{"C:\\\\Users\\\\.*\\\\Desktop\\\\.*"},
			allowRe: []*regexp.Regexp{regexp.MustCompile(`C:\\Users\\.*\\Desktop\\.*`)},
			input:   "C:/Users/test/Desktop/file.exe",
			osExpected: map[string]bool{
				"windows": true,  // Should match after normalization
				"linux":   false, // No normalization on Linux
				"darwin":  false, // No normalization on macOS
			},
		},
		{
			name:    "backslash input should always match backslash pattern",
			allow:   []string{"C:\\\\root\\\\.*\\.lnk"},
			allowRe: []*regexp.Regexp{regexp.MustCompile(`C:\\root\\.*\.lnk`)},
			input:   "C:\\root\\notepad.lnk",
			osExpected: map[string]bool{
				"windows": true, // Direct match
				"linux":   true, // Direct match (even though unusual for Linux)
				"darwin":  true, // Direct match (even though unusual for macOS)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := checkAllow(tt.allow, tt.allowRe, tt.input)

			// Get expected result for current OS
			expectedForCurrentOS, exists := tt.osExpected[runtime.GOOS]
			if !exists {
				// Default to false if OS not specified
				expectedForCurrentOS = false
			}

			assert.Equal(t, expectedForCurrentOS, result,
				"OS: %s, Input: %s, Pattern: %s",
				runtime.GOOS, tt.input, tt.allow[0])
		})
	}
}

func TestIsWindowsStylePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "empty path",
			path:     "",
			expected: false,
		},
		{
			name:     "unix absolute path",
			path:     "/usr/local/bin",
			expected: false,
		},
		{
			name:     "unix relative path",
			path:     "usr/local/bin",
			expected: false,
		},
		{
			name:     "windows drive letter C:",
			path:     "C:\\Users\\test",
			expected: true,
		},
		{
			name:     "windows drive letter with forward slash",
			path:     "C:/Users/test",
			expected: true,
		},
		{
			name:     "windows lowercase drive letter",
			path:     "d:\\temp",
			expected: true,
		},
		{
			name:     "UNC path with backslashes",
			path:     "\\\\server\\share",
			expected: true,
		},
		{
			name:     "UNC path with forward slashes",
			path:     "//server/share",
			expected: true,
		},
		{
			name:     "not a drive letter (too short)",
			path:     "C",
			expected: false,
		},
		{
			name:     "not a drive letter (no colon)",
			path:     "CD\\Users",
			expected: false,
		},
		{
			name:     "invalid drive letter (number)",
			path:     "1:\\Users",
			expected: false,
		},
		{
			name:     "relative path looks like drive",
			path:     "C:file.txt",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isWindowsStylePath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetMQTTPublishers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected []MQTTPublisher
		config   Values
	}{
		{
			name: "empty publishers",
			config: Values{
				Service: Service{},
			},
			expected: nil,
		},
		{
			name: "single publisher",
			config: Values{
				Service: Service{
					Publishers: Publishers{
						MQTT: []MQTTPublisher{
							{
								Broker: "localhost:1883",
								Topic:  "zaparoo/events",
								Filter: []string{"media.launched"},
							},
						},
					},
				},
			},
			expected: []MQTTPublisher{
				{
					Broker: "localhost:1883",
					Topic:  "zaparoo/events",
					Filter: []string{"media.launched"},
				},
			},
		},
		{
			name: "multiple publishers",
			config: Values{
				Service: Service{
					Publishers: Publishers{
						MQTT: []MQTTPublisher{
							{
								Broker: "localhost:1883",
								Topic:  "zaparoo/events",
								Filter: []string{"media.launched"},
							},
							{
								Broker: "remote:8883",
								Topic:  "remote/events",
								Filter: nil,
							},
						},
					},
				},
			},
			expected: []MQTTPublisher{
				{
					Broker: "localhost:1883",
					Topic:  "zaparoo/events",
					Filter: []string{"media.launched"},
				},
				{
					Broker: "remote:8883",
					Topic:  "remote/events",
					Filter: nil,
				},
			},
		},
		{
			name: "publisher with enabled flag",
			config: Values{
				Service: Service{
					Publishers: Publishers{
						MQTT: []MQTTPublisher{
							{
								Enabled: func() *bool { b := true; return &b }(),
								Broker:  "localhost:1883",
								Topic:   "zaparoo/events",
								Filter:  []string{},
							},
						},
					},
				},
			},
			expected: []MQTTPublisher{
				{
					Enabled: func() *bool { b := true; return &b }(),
					Broker:  "localhost:1883",
					Topic:   "zaparoo/events",
					Filter:  []string{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{vals: tt.config}
			result := cfg.GetMQTTPublishers()

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAPIPort(t *testing.T) {
	t.Parallel()

	port7497 := 7497
	port8080 := 8080

	tests := []struct {
		apiPort  *int
		name     string
		expected int
	}{
		{
			name:     "explicit port",
			apiPort:  &port7497,
			expected: 7497,
		},
		{
			name:     "custom port",
			apiPort:  &port8080,
			expected: 8080,
		},
		{
			name:     "nil port returns default",
			apiPort:  nil,
			expected: 7497,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{
				vals: Values{
					Service: Service{
						APIPort: tt.apiPort,
					},
				},
			}

			result := cfg.APIPort()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetAPIPort(t *testing.T) {
	t.Parallel()

	t.Run("sets port from nil", func(t *testing.T) {
		t.Parallel()

		cfg := &Instance{
			vals: Values{
				Service: Service{
					APIPort: nil, // Start with nil
				},
			},
		}

		assert.Nil(t, cfg.vals.Service.APIPort, "APIPort should start as nil")
		assert.Equal(t, DefaultAPIPort, cfg.APIPort(), "Getter should return default")

		cfg.SetAPIPort(8080)

		require.NotNil(t, cfg.vals.Service.APIPort, "APIPort should be set after SetAPIPort")
		assert.Equal(t, 8080, *cfg.vals.Service.APIPort, "APIPort value should be 8080")
		assert.Equal(t, 8080, cfg.APIPort(), "Getter should return new value")
	})

	t.Run("overwrites existing port", func(t *testing.T) {
		t.Parallel()

		initialPort := 9000
		cfg := &Instance{
			vals: Values{
				Service: Service{
					APIPort: &initialPort,
				},
			},
		}

		cfg.SetAPIPort(7777)

		assert.Equal(t, 7777, *cfg.vals.Service.APIPort, "APIPort should be overwritten")
		assert.Equal(t, 7777, cfg.APIPort(), "Getter should return new value")
	})
}

func TestAPIPort_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create config with defaults (APIPort is nil)
	cfg, err := NewConfig(tempDir, BaseDefaults)
	require.NoError(t, err)

	// Verify default is returned via getter
	assert.Equal(t, DefaultAPIPort, cfg.APIPort(), "Should return default port initially")

	// Set a custom port
	cfg.SetAPIPort(9999)
	assert.Equal(t, 9999, cfg.APIPort(), "Should return custom port after setting")

	// Save and reload
	err = cfg.Save()
	require.NoError(t, err)

	err = cfg.Load()
	require.NoError(t, err)

	// Verify custom port persists
	assert.Equal(t, 9999, cfg.APIPort(), "Custom port should persist after save/load")
}

func TestScanHistory(t *testing.T) {
	t.Parallel()

	thirtyDays := 30
	sevenDays := 7
	zero := 0

	tests := []struct {
		scanHistory *int
		name        string
		expected    int
	}{
		{
			name:        "nil returns default 30 days",
			scanHistory: nil,
			expected:    30,
		},
		{
			name:        "explicit 7 days",
			scanHistory: &sevenDays,
			expected:    7,
		},
		{
			name:        "explicit 30 days",
			scanHistory: &thirtyDays,
			expected:    30,
		},
		{
			name:        "zero (unlimited)",
			scanHistory: &zero,
			expected:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{
				vals: Values{
					Readers: Readers{
						ScanHistory: tt.scanHistory,
					},
				},
			}

			result := cfg.ScanHistory()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoad_PreservesDefaultsForMissingFields(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, CfgFile)

	// Define custom defaults that differ from zero values
	// Note: Service.APIPort and Groovy fields use pointer types with nil defaults
	// returned by getters, so they're not included in BaseDefaults
	defaults := Values{
		ConfigSchema: SchemaVersion,
		Audio: Audio{
			ScanFeedback: true, // This should persist after Load()
		},
		Readers: Readers{
			AutoDetect: true, // This should persist after Load()
			Scan: ReadersScan{
				Mode: ScanModeTap, // This should persist after Load()
			},
		},
	}

	// Create a minimal TOML file that only has ConfigSchema
	// (simulating a file that was saved without all default fields)
	minimalConfig := fmt.Sprintf("config_schema = %d\n", SchemaVersion)
	err := os.WriteFile(cfgPath, []byte(minimalConfig), 0o600)
	require.NoError(t, err)

	// Create config instance with our defaults
	cfg := &Instance{
		cfgPath:  cfgPath,
		vals:     defaults,
		defaults: defaults,
	}

	// Load the config file
	err = cfg.Load()
	require.NoError(t, err)

	// Verify that default values are preserved for fields not in the file
	assert.True(t, cfg.vals.Audio.ScanFeedback, "Audio.ScanFeedback should retain default true")
	assert.True(t, cfg.vals.Readers.AutoDetect, "Readers.AutoDetect should retain default true")
	assert.Equal(t, ScanModeTap, cfg.vals.Readers.Scan.Mode, "Readers.Scan.Mode should retain default")
	// Note: Service.APIPort is now a pointer type - nil value means use DefaultAPIPort via getter
	assert.Nil(t, cfg.vals.Service.APIPort, "Service.APIPort should be nil (getter returns default)")
}

func TestLoad_OverridesDefaults(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, CfgFile)

	// Defaults with specific values
	// Note: Service.APIPort uses pointer type with nil default returned by getter
	defaults := Values{
		ConfigSchema: SchemaVersion,
		Audio: Audio{
			ScanFeedback: true,
		},
		Readers: Readers{
			AutoDetect: true,
			Scan: ReadersScan{
				Mode: ScanModeTap,
			},
		},
	}

	// Config file that explicitly overrides some defaults
	configContent := fmt.Sprintf(`config_schema = %d
debug_logging = true

[audio]
scan_feedback = false

[readers]
auto_detect = false

[readers.scan]
mode = "hold"

[service]
api_port = 8080
`, SchemaVersion)

	err := os.WriteFile(cfgPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	cfg := &Instance{
		cfgPath:  cfgPath,
		vals:     defaults,
		defaults: defaults,
	}

	err = cfg.Load()
	require.NoError(t, err)

	// Verify that file values override defaults
	assert.True(t, cfg.vals.DebugLogging, "DebugLogging should be overridden to true")
	assert.False(t, cfg.vals.Audio.ScanFeedback, "Audio.ScanFeedback should be overridden to false")
	assert.False(t, cfg.vals.Readers.AutoDetect, "Readers.AutoDetect should be overridden to false")
	assert.Equal(t, ScanModeHold, cfg.vals.Readers.Scan.Mode, "Readers.Scan.Mode should be overridden")
	require.NotNil(t, cfg.vals.Service.APIPort, "Service.APIPort should be set from file")
	assert.Equal(t, 8080, *cfg.vals.Service.APIPort, "Service.APIPort should be overridden to 8080")
}

func TestLoad_ReloadCycle(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create config using NewConfig (the normal initialization path)
	cfg, err := NewConfig(tempDir, BaseDefaults)
	require.NoError(t, err)

	// Verify initial defaults are set
	assert.True(t, cfg.AudioFeedback(), "Initial AudioFeedback should be true")
	assert.True(t, cfg.Readers().AutoDetect, "Initial AutoDetect should be true")
	assert.Equal(t, ScanModeTap, cfg.ReadersScan().Mode, "Initial scan mode should be tap")

	// Modify a setting and save
	cfg.SetAudioFeedback(false)
	cfg.SetScanMode(ScanModeHold)
	err = cfg.Save()
	require.NoError(t, err)

	// Reload config
	err = cfg.Load()
	require.NoError(t, err)

	// Verify the explicitly saved values persist
	assert.False(t, cfg.AudioFeedback(), "AudioFeedback should be false after reload")
	assert.Equal(t, ScanModeHold, cfg.ReadersScan().Mode, "Scan mode should be hold after reload")

	// Verify other defaults are still intact
	assert.True(t, cfg.Readers().AutoDetect, "AutoDetect should retain default true after reload")
}

func TestGroovyDefaults_ReturnDefaultsWhenNil(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{
			Groovy: Groovy{}, // All fields nil
		},
	}

	// Verify getters return defaults when fields are nil
	assert.False(t, cfg.GmcProxyEnabled(), "GmcProxyEnabled should return false when nil")
	assert.Equal(t, DefaultGmcProxyPort, cfg.GmcProxyPort(), "GmcProxyPort should return default when nil")
	assert.Equal(t, DefaultGmcProxyBeaconInterval, cfg.GmcProxyBeaconInterval(),
		"GmcProxyBeaconInterval should return default when nil")
}

func TestGroovyDefaults_ReturnExplicitValues(t *testing.T) {
	t.Parallel()

	enabled := true
	port := 12345
	interval := "5s"

	cfg := &Instance{
		vals: Values{
			Groovy: Groovy{
				GmcProxyEnabled:        &enabled,
				GmcProxyPort:           &port,
				GmcProxyBeaconInterval: &interval,
			},
		},
	}

	// Verify getters return explicit values
	assert.True(t, cfg.GmcProxyEnabled(), "GmcProxyEnabled should return true when set")
	assert.Equal(t, 12345, cfg.GmcProxyPort(), "GmcProxyPort should return explicit value")
	assert.Equal(t, "5s", cfg.GmcProxyBeaconInterval(), "GmcProxyBeaconInterval should return explicit value")
}

func TestSave_OmitsNilPointerFields(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create config with default values (nil pointers)
	cfg, err := NewConfig(tempDir, BaseDefaults)
	require.NoError(t, err)

	// Save the config
	err = cfg.Save()
	require.NoError(t, err)

	// Read the saved file
	cfgPath := filepath.Join(tempDir, CfgFile)
	data, err := os.ReadFile(cfgPath) //nolint:gosec // test file path is controlled
	require.NoError(t, err)

	content := string(data)

	// Verify that pointer fields with nil values are not written
	assert.NotContains(t, content, "api_port", "api_port should not be in config when nil")
	assert.NotContains(t, content, "[groovy]", "groovy section should not be in config when all fields nil")
	assert.NotContains(t, content, "gmc_proxy_enabled", "gmc_proxy_enabled should not be in config when nil")
	assert.NotContains(t, content, "gmc_proxy_port", "gmc_proxy_port should not be in config when nil")
	assert.NotContains(t, content, "gmc_proxy_beacon_interval",
		"gmc_proxy_beacon_interval should not be in config when nil")
}

func TestVirtualGamepadEnabled(t *testing.T) {
	t.Parallel()

	trueVal := true
	falseVal := false

	tests := []struct {
		gamepadEnabled *bool
		name           string
		defaultEnabled bool
		expected       bool
	}{
		{
			name:           "nil returns default true",
			gamepadEnabled: nil,
			defaultEnabled: true,
			expected:       true,
		},
		{
			name:           "nil returns default false",
			gamepadEnabled: nil,
			defaultEnabled: false,
			expected:       false,
		},
		{
			name:           "explicit true overrides default false",
			gamepadEnabled: &trueVal,
			defaultEnabled: false,
			expected:       true,
		},
		{
			name:           "explicit false overrides default true",
			gamepadEnabled: &falseVal,
			defaultEnabled: true,
			expected:       false,
		},
		{
			name:           "explicit true with default true",
			gamepadEnabled: &trueVal,
			defaultEnabled: true,
			expected:       true,
		},
		{
			name:           "explicit false with default false",
			gamepadEnabled: &falseVal,
			defaultEnabled: false,
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{
				vals: Values{
					Input: Input{
						GamepadEnabled: tt.gamepadEnabled,
					},
				},
			}

			result := cfg.VirtualGamepadEnabled(tt.defaultEnabled)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetVirtualGamepadEnabled(t *testing.T) {
	t.Parallel()

	t.Run("sets enabled from nil", func(t *testing.T) {
		t.Parallel()

		cfg := &Instance{
			vals: Values{
				Input: Input{
					GamepadEnabled: nil,
				},
			},
		}

		assert.Nil(t, cfg.vals.Input.GamepadEnabled, "GamepadEnabled should start as nil")

		cfg.SetVirtualGamepadEnabled(true)

		require.NotNil(t, cfg.vals.Input.GamepadEnabled, "GamepadEnabled should be set")
		assert.True(t, *cfg.vals.Input.GamepadEnabled, "GamepadEnabled should be true")
	})

	t.Run("sets disabled from nil", func(t *testing.T) {
		t.Parallel()

		cfg := &Instance{
			vals: Values{
				Input: Input{
					GamepadEnabled: nil,
				},
			},
		}

		cfg.SetVirtualGamepadEnabled(false)

		require.NotNil(t, cfg.vals.Input.GamepadEnabled, "GamepadEnabled should be set")
		assert.False(t, *cfg.vals.Input.GamepadEnabled, "GamepadEnabled should be false")
	})

	t.Run("overwrites existing value", func(t *testing.T) {
		t.Parallel()

		initialVal := true
		cfg := &Instance{
			vals: Values{
				Input: Input{
					GamepadEnabled: &initialVal,
				},
			},
		}

		cfg.SetVirtualGamepadEnabled(false)

		assert.False(t, *cfg.vals.Input.GamepadEnabled, "GamepadEnabled should be overwritten to false")
	})
}

func TestVirtualGamepadEnabled_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create config with defaults (GamepadEnabled is nil)
	cfg, err := NewConfig(tempDir, BaseDefaults)
	require.NoError(t, err)

	// Verify default behavior - nil means use platform default
	assert.True(t, cfg.VirtualGamepadEnabled(true), "Should return true when nil and default is true")
	assert.False(t, cfg.VirtualGamepadEnabled(false), "Should return false when nil and default is false")

	// Set an explicit value
	cfg.SetVirtualGamepadEnabled(true)
	assert.True(t, cfg.VirtualGamepadEnabled(false), "Should return true regardless of default after setting")

	// Save and reload
	err = cfg.Save()
	require.NoError(t, err)

	err = cfg.Load()
	require.NoError(t, err)

	// Verify explicit value persists
	assert.True(t, cfg.VirtualGamepadEnabled(false), "Explicit true should persist after save/load")
}

func TestSave_OmitsGamepadEnabledWhenNil(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create config with default values (nil pointers)
	cfg, err := NewConfig(tempDir, BaseDefaults)
	require.NoError(t, err)

	// Save the config
	err = cfg.Save()
	require.NoError(t, err)

	// Read the saved file
	cfgPath := filepath.Join(tempDir, CfgFile)
	data, err := os.ReadFile(cfgPath) //nolint:gosec // test file path is controlled
	require.NoError(t, err)

	content := string(data)

	// Verify that gamepad_enabled is not written when nil
	assert.NotContains(t, content, "gamepad_enabled", "gamepad_enabled should not be in config when nil")
	assert.NotContains(t, content, "[input]", "input section should not be in config when all fields nil")
}
