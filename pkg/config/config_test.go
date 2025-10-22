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
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestLookupAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		authCfg  Auth
		expected *CredentialEntry
		name     string
		reqURL   string
	}{
		{
			name:     "empty auth config returns nil",
			authCfg:  Auth{},
			reqURL:   "https://example.com/api",
			expected: nil,
		},
		{
			name: "invalid request URL returns nil",
			authCfg: Auth{
				Creds: map[string]CredentialEntry{
					"https://example.com": {Username: "user", Password: "pass"},
				},
			},
			reqURL:   "://invalid-url",
			expected: nil,
		},
		{
			name: "exact match returns credentials",
			authCfg: Auth{
				Creds: map[string]CredentialEntry{
					"https://example.com": {Username: "user", Password: "pass"},
				},
			},
			reqURL:   "https://example.com",
			expected: &CredentialEntry{Username: "user", Password: "pass"},
		},
		{
			name: "path prefix match returns credentials",
			authCfg: Auth{
				Creds: map[string]CredentialEntry{
					"https://example.com/api": {Username: "user", Password: "pass"},
				},
			},
			reqURL:   "https://example.com/api/v1/data",
			expected: &CredentialEntry{Username: "user", Password: "pass"},
		},
		{
			name: "scheme mismatch returns nil",
			authCfg: Auth{
				Creds: map[string]CredentialEntry{
					"https://example.com": {Username: "user", Password: "pass"},
				},
			},
			reqURL:   "http://example.com",
			expected: nil,
		},
		{
			name: "host mismatch returns nil",
			authCfg: Auth{
				Creds: map[string]CredentialEntry{
					"https://example.com": {Username: "user", Password: "pass"},
				},
			},
			reqURL:   "https://other.com",
			expected: nil,
		},
		{
			name: "path not a prefix returns nil",
			authCfg: Auth{
				Creds: map[string]CredentialEntry{
					"https://example.com/api": {Username: "user", Password: "pass"},
				},
			},
			reqURL:   "https://example.com/other",
			expected: nil,
		},
		{
			name: "case insensitive scheme match",
			authCfg: Auth{
				Creds: map[string]CredentialEntry{
					"HTTPS://example.com": {Username: "user", Password: "pass"},
				},
			},
			reqURL:   "https://example.com",
			expected: &CredentialEntry{Username: "user", Password: "pass"},
		},
		{
			name: "case insensitive host match",
			authCfg: Auth{
				Creds: map[string]CredentialEntry{
					"https://EXAMPLE.COM": {Username: "user", Password: "pass"},
				},
			},
			reqURL:   "https://example.com",
			expected: &CredentialEntry{Username: "user", Password: "pass"},
		},
		{
			name: "bearer token credentials",
			authCfg: Auth{
				Creds: map[string]CredentialEntry{
					"https://api.example.com": {Bearer: "token123"},
				},
			},
			reqURL:   "https://api.example.com/v1",
			expected: &CredentialEntry{Bearer: "token123"},
		},
		{
			name: "mixed credential types",
			authCfg: Auth{
				Creds: map[string]CredentialEntry{
					"https://api.example.com": {Username: "user", Password: "pass", Bearer: "token"},
				},
			},
			reqURL:   "https://api.example.com",
			expected: &CredentialEntry{Username: "user", Password: "pass", Bearer: "token"},
		},
		{
			name: "invalid config URL continues to next",
			authCfg: Auth{
				Creds: map[string]CredentialEntry{
					"://invalid":          {Username: "invalid", Password: "invalid"},
					"https://example.com": {Username: "user", Password: "pass"},
				},
			},
			reqURL:   "https://example.com",
			expected: &CredentialEntry{Username: "user", Password: "pass"},
		},
		{
			name: "port matching",
			authCfg: Auth{
				Creds: map[string]CredentialEntry{
					"https://example.com:8080": {Username: "user", Password: "pass"},
				},
			},
			reqURL:   "https://example.com:8080/api",
			expected: &CredentialEntry{Username: "user", Password: "pass"},
		},
		{
			name: "port mismatch returns nil",
			authCfg: Auth{
				Creds: map[string]CredentialEntry{
					"https://example.com:8080": {Username: "user", Password: "pass"},
				},
			},
			reqURL:   "https://example.com:9090/api",
			expected: nil,
		},
		{
			name: "root path matching",
			authCfg: Auth{
				Creds: map[string]CredentialEntry{
					"https://example.com/": {Username: "user", Password: "pass"},
				},
			},
			reqURL:   "https://example.com/api",
			expected: &CredentialEntry{Username: "user", Password: "pass"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := LookupAuth(tt.authCfg, tt.reqURL)
			assert.Equal(t, tt.expected, result)
		})
	}

	// Test multiple path matches - map iteration order is non-deterministic
	t.Run("multiple path matches returns valid result", func(t *testing.T) {
		t.Parallel()
		authCfg := Auth{
			Creds: map[string]CredentialEntry{
				"https://example.com":     {Username: "user1", Password: "pass1"},
				"https://example.com/api": {Username: "user2", Password: "pass2"},
			},
		}
		result := LookupAuth(authCfg, "https://example.com/api/data")
		// Both credentials are valid matches, so result should not be nil
		assert.NotNil(t, result)
		// Since map iteration order is non-deterministic, accept either match
		validMatch := (result.Username == "user1" && result.Password == "pass1") ||
			(result.Username == "user2" && result.Password == "pass2")
		assert.True(t, validMatch, "Result should match one of the valid credentials")
	})
}

func TestLaunchersDefaultServerURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		launcherCfg LaunchersDefault
		expected    string
	}{
		{
			name: "ServerURL field is properly set and retrieved",
			launcherCfg: LaunchersDefault{
				Launcher:   "Kodi",
				InstallDir: "/opt/kodi",
				ServerURL:  "http://kodi-server:8080/jsonrpc",
			},
			expected: "http://kodi-server:8080/jsonrpc",
		},
		{
			name: "ServerURL field can be empty",
			launcherCfg: LaunchersDefault{
				Launcher:   "KodiLocal",
				InstallDir: "/usr/bin/kodi",
				ServerURL:  "",
			},
			expected: "",
		},
		{
			name: "ServerURL field supports localhost URLs",
			launcherCfg: LaunchersDefault{
				Launcher:  "KodiTest",
				ServerURL: "http://localhost:8080/jsonrpc",
			},
			expected: "http://localhost:8080/jsonrpc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.launcherCfg.ServerURL)
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

	tests := []struct {
		name     string
		apiPort  int
		expected int
	}{
		{
			name:     "default port",
			apiPort:  7497,
			expected: 7497,
		},
		{
			name:     "custom port",
			apiPort:  8080,
			expected: 8080,
		},
		{
			name:     "zero port",
			apiPort:  0,
			expected: 0,
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

func TestAllowedOrigins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		origins  []string
		expected []string
	}{
		{
			name:     "nil origins",
			origins:  nil,
			expected: nil,
		},
		{
			name:     "empty origins",
			origins:  []string{},
			expected: []string{},
		},
		{
			name:     "single origin",
			origins:  []string{"http://localhost:3000"},
			expected: []string{"http://localhost:3000"},
		},
		{
			name:     "multiple origins",
			origins:  []string{"http://localhost:3000", "https://example.com"},
			expected: []string{"http://localhost:3000", "https://example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{
				vals: Values{
					Service: Service{
						AllowedOrigins: tt.origins,
					},
				},
			}

			result := cfg.AllowedOrigins()
			assert.Equal(t, tt.expected, result)
		})
	}
}
