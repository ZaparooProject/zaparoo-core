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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAuthFromData_RootFormat(t *testing.T) {
	t.Parallel()

	data := []byte(`
["https://api.example.com"]
username = "user1"
password = "pass1"

["broker:1883"]
username = "mqtt-user"
password = "mqtt-pass"
`)

	result := LoadAuthFromData(data)

	require.Len(t, result, 2)
	assert.Equal(t, "user1", result["https://api.example.com"].Username)
	assert.Equal(t, "pass1", result["https://api.example.com"].Password)
	assert.Equal(t, "mqtt-user", result["broker:1883"].Username)
	assert.Equal(t, "mqtt-pass", result["broker:1883"].Password)
}

func TestLoadAuthFromData_CredsFormat(t *testing.T) {
	t.Parallel()

	data := []byte(`
[creds."https://api.example.com"]
username = "user1"
password = "pass1"

[creds."smb://fileserver"]
username = "smb-user"
password = "smb-pass"
`)

	result := LoadAuthFromData(data)

	require.Len(t, result, 2)
	assert.Equal(t, "user1", result["https://api.example.com"].Username)
	assert.Equal(t, "smb-user", result["smb://fileserver"].Username)
}

func TestLoadAuthFromData_AuthCredsFormat(t *testing.T) {
	t.Parallel()

	data := []byte(`
[auth.creds."https://api.example.com"]
username = "user1"
password = "pass1"

[auth.creds."http://legacy"]
username = "legacy-user"
password = "legacy-pass"
`)

	result := LoadAuthFromData(data)

	require.Len(t, result, 2)
	assert.Equal(t, "user1", result["https://api.example.com"].Username)
	assert.Equal(t, "legacy-user", result["http://legacy"].Username)
}

func TestLoadAuthFromData_MixedFormats(t *testing.T) {
	t.Parallel()

	data := []byte(`
# New root format
["https://new.example.com"]
username = "new-user"
password = "new-pass"

# Current creds format
[creds."https://current.example.com"]
username = "current-user"
password = "current-pass"

# Legacy auth.creds format
[auth.creds."https://legacy.example.com"]
username = "legacy-user"
password = "legacy-pass"
`)

	result := LoadAuthFromData(data)

	require.Len(t, result, 3)
	assert.Equal(t, "new-user", result["https://new.example.com"].Username)
	assert.Equal(t, "current-user", result["https://current.example.com"].Username)
	assert.Equal(t, "legacy-user", result["https://legacy.example.com"].Username)
}

func TestLoadAuthFromData_SimpleHostname(t *testing.T) {
	t.Parallel()

	data := []byte(`
["localhost"]
username = "local-user"
password = "local-pass"

["my-nas"]
username = "nas-user"
password = "nas-pass"
`)

	result := LoadAuthFromData(data)

	require.Len(t, result, 2)
	assert.Equal(t, "local-user", result["localhost"].Username)
	assert.Equal(t, "nas-user", result["my-nas"].Username)
}

func TestLoadAuthFromData_BearerToken(t *testing.T) {
	t.Parallel()

	data := []byte(`
["https://api.example.com"]
bearer = "token123"
`)

	result := LoadAuthFromData(data)

	require.Len(t, result, 1)
	assert.Equal(t, "token123", result["https://api.example.com"].Bearer)
}

func TestLoadAuthFromData_EmptyData(t *testing.T) {
	t.Parallel()

	result := LoadAuthFromData([]byte(""))
	assert.Empty(t, result)
}

func TestLoadAuthFromData_InvalidTOML(t *testing.T) {
	t.Parallel()

	// Invalid TOML should not panic, just return empty
	result := LoadAuthFromData([]byte("this is not valid toml [[["))
	assert.Empty(t, result)
}

func TestNormalizeScheme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"tcp", "mqtt"},
		{"TCP", "mqtt"},
		{"ssl", "mqtts"},
		{"SSL", "mqtts"},
		{"ws", "http"},
		{"WS", "http"},
		{"wss", "https"},
		{"WSS", "https"},
		{"mqtt", "mqtt"},
		{"mqtts", "mqtts"},
		{"http", "http"},
		{"https", "https"},
		{"smb", "smb"},
		{"ftp", "ftp"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, normalizeScheme(tt.input))
		})
	}
}

func TestIsSchemelessKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key      string
		expected bool
	}{
		{"https://example.com", false},
		{"http://example.com", false},
		{"mqtt://broker:1883", false},
		{"broker:1883", true},
		{"example.com", true},
		{"192.168.1.100:1883", true},
		{"localhost", true},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isSchemelessKey(tt.key))
		})
	}
}

func TestLookupAuth_EmptyCreds(t *testing.T) {
	t.Parallel()

	result := LookupAuth(nil, "https://example.com")
	assert.Nil(t, result)

	result = LookupAuth(map[string]CredentialEntry{}, "https://example.com")
	assert.Nil(t, result)
}

func TestLookupAuth_InvalidURL(t *testing.T) {
	t.Parallel()

	creds := map[string]CredentialEntry{
		"https://example.com": {Username: "user", Password: "pass"},
	}

	result := LookupAuth(creds, "://invalid-url")
	assert.Nil(t, result)
}

func TestLookupAuth_ExactSchemeMatch(t *testing.T) {
	t.Parallel()

	creds := map[string]CredentialEntry{
		"https://example.com": {Username: "user", Password: "pass"},
	}

	result := LookupAuth(creds, "https://example.com")
	require.NotNil(t, result)
	assert.Equal(t, "user", result.Username)

	// Different scheme should not match
	result = LookupAuth(creds, "http://example.com")
	assert.Nil(t, result)
}

func TestLookupAuth_PathPrefixMatch(t *testing.T) {
	t.Parallel()

	creds := map[string]CredentialEntry{
		"https://example.com/api": {Username: "user", Password: "pass"},
	}

	result := LookupAuth(creds, "https://example.com/api/v1/data")
	require.NotNil(t, result)
	assert.Equal(t, "user", result.Username)

	// Non-matching path should not match
	result = LookupAuth(creds, "https://example.com/other")
	assert.Nil(t, result)
}

func TestLookupAuth_CaseInsensitiveHostAndScheme(t *testing.T) {
	t.Parallel()

	creds := map[string]CredentialEntry{
		"HTTPS://EXAMPLE.COM": {Username: "user", Password: "pass"},
	}

	result := LookupAuth(creds, "https://example.com")
	require.NotNil(t, result)
	assert.Equal(t, "user", result.Username)
}

func TestLookupAuth_CanonicalSchemeMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		configURL   string
		requestURL  string
		shouldMatch bool
	}{
		{
			name:        "tcp matches mqtt config",
			configURL:   "mqtt://broker:1883",
			requestURL:  "tcp://broker:1883",
			shouldMatch: true,
		},
		{
			name:        "ssl matches mqtts config",
			configURL:   "mqtts://broker:8883",
			requestURL:  "ssl://broker:8883",
			shouldMatch: true,
		},
		{
			name:        "ws matches http config",
			configURL:   "http://api.example.com",
			requestURL:  "ws://api.example.com",
			shouldMatch: true,
		},
		{
			name:        "wss matches https config",
			configURL:   "https://api.example.com",
			requestURL:  "wss://api.example.com",
			shouldMatch: true,
		},
		{
			name:        "mqtt matches tcp config",
			configURL:   "tcp://broker:1883",
			requestURL:  "mqtt://broker:1883",
			shouldMatch: true,
		},
		{
			name:        "http does not match https config",
			configURL:   "https://api.example.com",
			requestURL:  "http://api.example.com",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			creds := map[string]CredentialEntry{
				tt.configURL: {Username: "user", Password: "pass"},
			}

			result := LookupAuth(creds, tt.requestURL)
			if tt.shouldMatch {
				require.NotNil(t, result, "expected match for %s -> %s", tt.configURL, tt.requestURL)
				assert.Equal(t, "user", result.Username)
			} else {
				assert.Nil(t, result, "expected no match for %s -> %s", tt.configURL, tt.requestURL)
			}
		})
	}
}

func TestLookupAuth_SchemelessHostPortMatch(t *testing.T) {
	t.Parallel()

	creds := map[string]CredentialEntry{
		"broker:1883": {Username: "mqtt-user", Password: "mqtt-pass"},
	}

	result := LookupAuth(creds, "mqtt://broker:1883")
	require.NotNil(t, result)
	assert.Equal(t, "mqtt-user", result.Username)

	result = LookupAuth(creds, "tcp://broker:1883")
	require.NotNil(t, result)
	assert.Equal(t, "mqtt-user", result.Username)

	result = LookupAuth(creds, "mqtts://broker:1883")
	require.NotNil(t, result)
	assert.Equal(t, "mqtt-user", result.Username)

	// Different port should not match
	result = LookupAuth(creds, "mqtt://broker:8883")
	assert.Nil(t, result)
}

func TestLookupAuth_SchemelessCaseInsensitive(t *testing.T) {
	t.Parallel()

	creds := map[string]CredentialEntry{
		"BROKER:1883": {Username: "user", Password: "pass"},
	}

	result := LookupAuth(creds, "mqtt://broker:1883")
	require.NotNil(t, result)
	assert.Equal(t, "user", result.Username)
}

func TestLookupAuth_Priority(t *testing.T) {
	t.Parallel()

	t.Run("exact scheme beats canonical", func(t *testing.T) {
		t.Parallel()

		creds := map[string]CredentialEntry{
			"mqtt://broker:1883": {Username: "exact-user", Password: "exact-pass"},
			"tcp://broker:1883":  {Username: "canonical-user", Password: "canonical-pass"},
		}

		// Request mqtt:// should match mqtt:// exactly, not tcp:// canonically
		result := LookupAuth(creds, "mqtt://broker:1883")
		require.NotNil(t, result)
		assert.Equal(t, "exact-user", result.Username)
	})

	t.Run("scheme with match beats schemeless", func(t *testing.T) {
		t.Parallel()

		creds := map[string]CredentialEntry{
			"mqtt://broker:1883": {Username: "scheme-user", Password: "scheme-pass"},
			"broker:1883":        {Username: "schemeless-user", Password: "schemeless-pass"},
		}

		result := LookupAuth(creds, "mqtt://broker:1883")
		require.NotNil(t, result)
		assert.Equal(t, "scheme-user", result.Username)
	})

	t.Run("canonical beats schemeless", func(t *testing.T) {
		t.Parallel()

		creds := map[string]CredentialEntry{
			"mqtt://broker:1883": {Username: "canonical-user", Password: "canonical-pass"},
			"broker:1883":        {Username: "schemeless-user", Password: "schemeless-pass"},
		}

		// Request tcp:// should match mqtt:// canonically, not schemeless
		result := LookupAuth(creds, "tcp://broker:1883")
		require.NotNil(t, result)
		assert.Equal(t, "canonical-user", result.Username)
	})

	t.Run("schemeless fallback when no scheme match", func(t *testing.T) {
		t.Parallel()

		creds := map[string]CredentialEntry{
			"https://other.com": {Username: "other-user", Password: "other-pass"},
			"broker:1883":       {Username: "schemeless-user", Password: "schemeless-pass"},
		}

		result := LookupAuth(creds, "mqtt://broker:1883")
		require.NotNil(t, result)
		assert.Equal(t, "schemeless-user", result.Username)
	})
}

func TestLookupAuth_PortMatching(t *testing.T) {
	t.Parallel()

	creds := map[string]CredentialEntry{
		"https://example.com:8080": {Username: "port-user", Password: "port-pass"},
	}

	result := LookupAuth(creds, "https://example.com:8080/api")
	require.NotNil(t, result)
	assert.Equal(t, "port-user", result.Username)

	// Missing port should not match
	result = LookupAuth(creds, "https://example.com/api")
	assert.Nil(t, result)

	// Different port should not match
	result = LookupAuth(creds, "https://example.com:9090/api")
	assert.Nil(t, result)
}

func TestLookupAuth_HostMismatch(t *testing.T) {
	t.Parallel()

	creds := map[string]CredentialEntry{
		"https://example.com": {Username: "user", Password: "pass"},
	}

	result := LookupAuth(creds, "https://other.com")
	assert.Nil(t, result)
}

func TestLookupAuth_InvalidConfigURL(t *testing.T) {
	t.Parallel()

	creds := map[string]CredentialEntry{
		"://invalid":          {Username: "invalid", Password: "invalid"},
		"https://example.com": {Username: "valid", Password: "valid"},
	}

	result := LookupAuth(creds, "https://example.com")
	require.NotNil(t, result)
	assert.Equal(t, "valid", result.Username)
}

func TestLookupAuth_RealWorldMQTTScenarios(t *testing.T) {
	t.Parallel()

	t.Run("schemeless IP:port for MQTT", func(t *testing.T) {
		t.Parallel()

		creds := map[string]CredentialEntry{
			"192.168.1.100:1883": {Username: "mqtt-user", Password: "mqtt-pass"},
		}

		result := LookupAuth(creds, "mqtt://192.168.1.100:1883")
		require.NotNil(t, result)
		assert.Equal(t, "mqtt-user", result.Username)

		result = LookupAuth(creds, "mqtts://192.168.1.100:1883")
		require.NotNil(t, result)
		assert.Equal(t, "mqtt-user", result.Username)
	})

	t.Run("mqtt:// scheme config", func(t *testing.T) {
		t.Parallel()

		creds := map[string]CredentialEntry{
			"mqtt://192.168.1.100:1883": {Username: "mqtt-user", Password: "mqtt-pass"},
		}

		result := LookupAuth(creds, "mqtt://192.168.1.100:1883")
		require.NotNil(t, result)
		assert.Equal(t, "mqtt-user", result.Username)

		// tcp canonicalizes to mqtt
		result = LookupAuth(creds, "tcp://192.168.1.100:1883")
		require.NotNil(t, result)
		assert.Equal(t, "mqtt-user", result.Username)
	})
}
