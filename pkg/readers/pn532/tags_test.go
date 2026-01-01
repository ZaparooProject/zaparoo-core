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

package pn532

import (
	"encoding/json"
	"testing"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertNDEFToTokenText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		message  *pn532.NDEFMessage
		expected string
	}{
		{
			name:     "nil message returns empty string",
			message:  nil,
			expected: "",
		},
		{
			name: "empty records returns empty string",
			message: &pn532.NDEFMessage{
				Records: []pn532.NDEFRecord{},
			},
			expected: "",
		},
		{
			name: "text record passes through directly",
			message: &pn532.NDEFMessage{
				Records: []pn532.NDEFRecord{
					{Text: "Hello World"},
				},
			},
			expected: "Hello World",
		},
		{
			name: "URI record passes through directly",
			message: &pn532.NDEFMessage{
				Records: []pn532.NDEFRecord{
					{URI: "https://example.com"},
				},
			},
			expected: "https://example.com",
		},
		{
			name: "text takes priority over URI",
			message: &pn532.NDEFMessage{
				Records: []pn532.NDEFRecord{
					{Text: "Priority Text", URI: "https://example.com"},
				},
			},
			expected: "Priority Text",
		},
		{
			name: "WiFi credentials convert to JSON",
			message: &pn532.NDEFMessage{
				Records: []pn532.NDEFRecord{
					{
						WiFi: &pn532.WiFiCredential{
							SSID:       "TestNetwork",
							NetworkKey: "password123",
						},
					},
				},
			},
			expected: `{"networkKey":"password123","ssid":"TestNetwork","type":"wifi"}`,
		},
		{
			name: "VCard converts to JSON",
			message: &pn532.NDEFMessage{
				Records: []pn532.NDEFRecord{
					{
						VCard: &pn532.VCardContact{
							FormattedName: "John Doe",
						},
					},
				},
			},
			expected: `{"contact":{"name":"John Doe"},"type":"vcard"}`,
		},
		{
			name: "Smart Poster converts to JSON with hex payload",
			message: &pn532.NDEFMessage{
				Records: []pn532.NDEFRecord{
					{
						Type:    pn532.NDEFTypeSmartPoster,
						Payload: []byte{0xDE, 0xAD, 0xBE, 0xEF},
					},
				},
			},
			expected: `{"raw":"deadbeef","type":"smartposter"}`,
		},
		{
			name: "generic payload converts to JSON",
			message: &pn532.NDEFMessage{
				Records: []pn532.NDEFRecord{
					{
						Type:    "application/custom",
						Payload: []byte{0x01, 0x02, 0x03},
					},
				},
			},
			expected: `{"payload":"010203","type":"unknown","typeField":"application/custom"}`,
		},
		{
			name: "record with no content returns empty string",
			message: &pn532.NDEFMessage{
				Records: []pn532.NDEFRecord{
					{},
				},
			},
			expected: "",
		},
		{
			name: "only first record is processed",
			message: &pn532.NDEFMessage{
				Records: []pn532.NDEFRecord{
					{Text: "First Record"},
					{Text: "Second Record"},
				},
			},
			expected: "First Record",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertNDEFToTokenText(tt.message)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertWiFiToJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wifi     *pn532.WiFiCredential
		validate func(t *testing.T, result string)
		name     string
	}{
		{
			name: "basic WiFi with SSID only",
			wifi: &pn532.WiFiCredential{
				SSID: "MyNetwork",
			},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "wifi", data["type"])
				assert.Equal(t, "MyNetwork", data["ssid"])
				assert.NotContains(t, data, "networkKey")
			},
		},
		{
			name: "WiFi with network key",
			wifi: &pn532.WiFiCredential{
				SSID:       "SecureNetwork",
				NetworkKey: "secret123",
			},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "wifi", data["type"])
				assert.Equal(t, "SecureNetwork", data["ssid"])
				assert.Equal(t, "secret123", data["networkKey"])
			},
		},
		{
			name: "WiFi with auth type",
			wifi: &pn532.WiFiCredential{
				SSID:     "AuthNetwork",
				AuthType: 2,
			},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "wifi", data["type"])
				authType, ok := data["authType"].(float64)
				require.True(t, ok, "authType should be a number")
				assert.Equal(t, 2, int(authType))
			},
		},
		{
			name: "WiFi with encryption type",
			wifi: &pn532.WiFiCredential{
				SSID:           "EncryptedNetwork",
				EncryptionType: 4,
			},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "wifi", data["type"])
				encType, ok := data["encryptionType"].(float64)
				require.True(t, ok, "encryptionType should be a number")
				assert.Equal(t, 4, int(encType))
			},
		},
		{
			name: "WiFi with MAC address",
			wifi: &pn532.WiFiCredential{
				SSID:       "MACNetwork",
				MACAddress: "AA:BB:CC:DD:EE:FF",
			},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "wifi", data["type"])
				assert.Equal(t, "AA:BB:CC:DD:EE:FF", data["macAddress"])
			},
		},
		{
			name: "WiFi with all fields",
			wifi: &pn532.WiFiCredential{
				SSID:           "FullNetwork",
				NetworkKey:     "fullpass",
				AuthType:       1,
				EncryptionType: 2,
				MACAddress:     "11:22:33:44:55:66",
			},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "wifi", data["type"])
				assert.Equal(t, "FullNetwork", data["ssid"])
				assert.Equal(t, "fullpass", data["networkKey"])
				authType, ok := data["authType"].(float64)
				require.True(t, ok, "authType should be a number")
				assert.Equal(t, 1, int(authType))
				encType, ok := data["encryptionType"].(float64)
				require.True(t, ok, "encryptionType should be a number")
				assert.Equal(t, 2, int(encType))
				assert.Equal(t, "11:22:33:44:55:66", data["macAddress"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertWiFiToJSON(tt.wifi)
			tt.validate(t, result)
		})
	}
}

func TestConvertVCardToJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		vcard    *pn532.VCardContact
		validate func(t *testing.T, result string)
		name     string
	}{
		{
			name: "VCard with name only",
			vcard: &pn532.VCardContact{
				FormattedName: "Jane Smith",
			},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "vcard", data["type"])
				contact, ok := data["contact"].(map[string]any)
				require.True(t, ok, "contact should be a map")
				assert.Equal(t, "Jane Smith", contact["name"])
			},
		},
		{
			name: "VCard with phone numbers",
			vcard: &pn532.VCardContact{
				FormattedName: "Contact",
				PhoneNumbers:  map[string]string{"mobile": "+1234567890", "work": "+0987654321"},
			},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				contact, ok := data["contact"].(map[string]any)
				require.True(t, ok, "contact should be a map")
				phones, ok := contact["phones"].(map[string]any)
				require.True(t, ok, "phones should be a map")
				assert.Len(t, phones, 2)
				assert.Equal(t, "+1234567890", phones["mobile"])
			},
		},
		{
			name: "VCard with email addresses",
			vcard: &pn532.VCardContact{
				FormattedName:  "Email User",
				EmailAddresses: map[string]string{"home": "user@example.com", "work": "work@company.com"},
			},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				contact, ok := data["contact"].(map[string]any)
				require.True(t, ok, "contact should be a map")
				emails, ok := contact["emails"].(map[string]any)
				require.True(t, ok, "emails should be a map")
				assert.Len(t, emails, 2)
				assert.Equal(t, "user@example.com", emails["home"])
			},
		},
		{
			name: "VCard with organization",
			vcard: &pn532.VCardContact{
				FormattedName: "Employee",
				Organization:  "Acme Corp",
			},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				contact, ok := data["contact"].(map[string]any)
				require.True(t, ok, "contact should be a map")
				assert.Equal(t, "Acme Corp", contact["organization"])
			},
		},
		{
			name: "VCard with title",
			vcard: &pn532.VCardContact{
				FormattedName: "Manager",
				Title:         "Software Engineer",
			},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				contact, ok := data["contact"].(map[string]any)
				require.True(t, ok, "contact should be a map")
				assert.Equal(t, "Software Engineer", contact["title"])
			},
		},
		{
			name: "VCard with URL",
			vcard: &pn532.VCardContact{
				FormattedName: "Website User",
				URL:           "https://example.com",
			},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				contact, ok := data["contact"].(map[string]any)
				require.True(t, ok, "contact should be a map")
				assert.Equal(t, "https://example.com", contact["url"])
			},
		},
		{
			name: "VCard with all fields",
			vcard: &pn532.VCardContact{
				FormattedName:  "Full Contact",
				PhoneNumbers:   map[string]string{"mobile": "+1111111111"},
				EmailAddresses: map[string]string{"home": "full@example.com"},
				Organization:   "Full Org",
				Title:          "Full Title",
				URL:            "https://full.example.com",
			},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "vcard", data["type"])
				contact, ok := data["contact"].(map[string]any)
				require.True(t, ok, "contact should be a map")
				assert.Equal(t, "Full Contact", contact["name"])
				assert.NotNil(t, contact["phones"])
				assert.NotNil(t, contact["emails"])
				assert.Equal(t, "Full Org", contact["organization"])
				assert.Equal(t, "Full Title", contact["title"])
				assert.Equal(t, "https://full.example.com", contact["url"])
			},
		},
		{
			name:  "VCard with empty fields",
			vcard: &pn532.VCardContact{},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "vcard", data["type"])
				assert.NotContains(t, data, "contact")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertVCardToJSON(tt.vcard)
			tt.validate(t, result)
		})
	}
}

func TestConvertSmartPosterToJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		validate func(t *testing.T, result string)
		name     string
		payload  []byte
	}{
		{
			name:    "basic payload",
			payload: []byte{0x01, 0x02, 0x03, 0x04},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "smartposter", data["type"])
				assert.Equal(t, "01020304", data["raw"])
			},
		},
		{
			name:    "empty payload",
			payload: []byte{},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "smartposter", data["type"])
				assert.Empty(t, data["raw"])
			},
		},
		{
			name:    "payload with special bytes",
			payload: []byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "smartposter", data["type"])
				assert.Equal(t, "deadbeefcafe", data["raw"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertSmartPosterToJSON(tt.payload)
			tt.validate(t, result)
		})
	}
}

func TestConvertGenericRecordToJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		validate   func(t *testing.T, result string)
		name       string
		recordType string
		payload    []byte
	}{
		{
			name:       "basic record",
			recordType: "application/octet-stream",
			payload:    []byte{0xAA, 0xBB, 0xCC},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "unknown", data["type"])
				assert.Equal(t, "application/octet-stream", data["typeField"])
				assert.Equal(t, "aabbcc", data["payload"])
			},
		},
		{
			name:       "custom MIME type",
			recordType: "application/vnd.custom",
			payload:    []byte{0x01, 0x02},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "unknown", data["type"])
				assert.Equal(t, "application/vnd.custom", data["typeField"])
				assert.Equal(t, "0102", data["payload"])
			},
		},
		{
			name:       "empty payload",
			recordType: "empty",
			payload:    []byte{},
			validate: func(t *testing.T, result string) {
				var data map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &data))
				assert.Equal(t, "unknown", data["type"])
				assert.Equal(t, "empty", data["typeField"])
				assert.Empty(t, data["payload"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertGenericRecordToJSON(tt.recordType, tt.payload)
			tt.validate(t, result)
		})
	}
}

func TestReadNDEFData_NoTagOps(t *testing.T) {
	t.Parallel()

	// Test the early return path when tagOps is nil (no real device available)
	reader := &Reader{
		tagOps: nil,
	}

	detectedTag := &pn532.DetectedTag{
		UID:        "test-uid",
		TargetData: []byte{0x01, 0x02, 0x03},
	}

	uid, data := reader.readNDEFData(detectedTag)

	assert.Empty(t, uid, "uid should be empty when tagOps is nil")
	assert.Equal(t, detectedTag.TargetData, data, "should return original target data")
}
