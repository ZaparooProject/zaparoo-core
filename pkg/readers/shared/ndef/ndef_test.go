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

package ndef

import (
	"bytes"
	"testing"

	"github.com/hsanjuan/go-ndef"
)

func TestParseToText_TextRecord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "Valid text record",
			input:   "test",
			wantErr: false,
		},
		{
			name:    "Valid text record with longer text",
			input:   "hello world",
			wantErr: false,
		},
		{
			name:    "Empty text",
			input:   "",
			wantErr: false,
		},
		{
			name:    "Unicode text",
			input:   "test 测试", //nolint:gosmopolitan // testing Unicode support
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Build message first
			built, err := BuildTextMessage(tt.input)
			if err != nil {
				t.Fatalf("BuildTextMessage() failed: %v", err)
			}

			// Now parse it
			result, err := ParseToText(built)

			if tt.wantErr {
				if err == nil {
					t.Error("ParseToText() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseToText() unexpected error: %v", err)
				return
			}

			if result != tt.input {
				t.Errorf("ParseToText() = %q, expected %q", result, tt.input)
			}
		})
	}
}

func TestParseToText_URIRecord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		uri  string
	}{
		{
			name: "HTTP URI",
			uri:  "http://example.com",
		},
		{
			name: "HTTPS URI",
			uri:  "https://example.com",
		},
		{
			name: "Tel URI",
			uri:  "tel:+1234567890",
		},
		{
			name: "Mailto URI",
			uri:  "mailto:test@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create URI record manually
			msg := ndef.NewURIMessage(tt.uri)
			payload, err := msg.Marshal()
			if err != nil {
				t.Fatalf("Failed to marshal URI message: %v", err)
			}

			// Add TLV wrapper
			header, err := calculateNDEFHeader(payload)
			if err != nil {
				t.Fatalf("Failed to calculate NDEF header: %v", err)
			}

			// Build complete message
			built := make([]byte, 0, len(header)+len(payload)+1)
			built = append(built, header...)
			built = append(built, payload...)
			built = append(built, NdefEnd...)

			// Parse it
			result, err := ParseToText(built)
			if err != nil {
				t.Errorf("ParseToText() unexpected error: %v", err)
				return
			}

			if result != tt.uri {
				t.Errorf("ParseToText() = %q, expected %q", result, tt.uri)
			}
		})
	}
}

func TestParseToText_ErrorCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []byte
	}{
		{
			name:  "Empty data",
			input: []byte{},
		},
		{
			name:  "Invalid TLV",
			input: []byte{0x01, 0x02, 0x03},
		},
		{
			name:  "No NDEF record",
			input: []byte{0x03, 0x05, 0x00, 0x00, 0x00, 0x00, 0x00, 0xFE},
		},
		{
			name:  "Too short message",
			input: []byte{0x03},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseToText(tt.input)

			if err == nil {
				t.Error("ParseToText() expected error but got none")
			}
		})
	}
}

func TestBuildTextMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		checkLen bool
		minLen   int
	}{
		{
			name:     "Simple text",
			input:    "test",
			checkLen: true,
			minLen:   10,
		},
		{
			name:     "Longer text",
			input:    "hello world",
			checkLen: true,
			minLen:   15,
		},
		{
			name:     "Empty text",
			input:    "",
			checkLen: true,
			minLen:   8,
		},
		{
			name:     "Unicode text",
			input:    "test 测试", //nolint:gosmopolitan // testing Unicode support
			checkLen: true,
			minLen:   12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := BuildTextMessage(tt.input)
			if err != nil {
				t.Errorf("BuildTextMessage() unexpected error: %v", err)
				return
			}

			if tt.checkLen && len(result) < tt.minLen {
				t.Errorf("BuildTextMessage() result too short: got %d, want >= %d", len(result), tt.minLen)
			}

			// Check TLV header
			if len(result) < 2 || result[0] != 0x03 {
				t.Error("BuildTextMessage() missing or invalid TLV header")
			}

			// Check NDEF terminator
			if len(result) < 1 || result[len(result)-1] != 0xFE {
				t.Error("BuildTextMessage() missing NDEF terminator")
			}
		})
	}
}

func TestBuildTextMessage_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []string{
		"test",
		"hello world",
		"",
		"test 测试", //nolint:gosmopolitan // testing Unicode support
		"special chars: !@#$%^&*()",
	}

	for _, text := range tests {
		t.Run(text, func(t *testing.T) {
			t.Parallel()

			// Build the message
			built, err := BuildTextMessage(text)
			if err != nil {
				t.Fatalf("BuildTextMessage() failed: %v", err)
			}

			// Parse it back
			parsed, err := ParseToText(built)
			if err != nil {
				t.Fatalf("ParseToText() failed: %v", err)
			}

			// Should match original
			if parsed != text {
				t.Errorf("Round trip failed: built %q, parsed %q", text, parsed)
			}
		})
	}
}

func TestCalculateNDEFHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "Short format - 1 byte",
			input:    []byte{0x42},
			expected: []byte{0x03, 0x01},
		},
		{
			name:     "Short format - 254 bytes",
			input:    bytes.Repeat([]byte{0x42}, 254),
			expected: []byte{0x03, 0xFE},
		},
		{
			name:     "Long format - 255 bytes",
			input:    bytes.Repeat([]byte{0x42}, 255),
			expected: []byte{0x03, 0xFF, 0x00, 0xFF},
		},
		{
			name:     "Long format - 256 bytes",
			input:    bytes.Repeat([]byte{0x42}, 256),
			expected: []byte{0x03, 0xFF, 0x01, 0x00},
		},
		{
			name:     "Long format - 1000 bytes",
			input:    bytes.Repeat([]byte{0x42}, 1000),
			expected: []byte{0x03, 0xFF, 0x03, 0xE8},
		},
		{
			name:     "Empty payload",
			input:    []byte{},
			expected: []byte{0x03, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := calculateNDEFHeader(tt.input)
			if err != nil {
				t.Fatalf("calculateNDEFHeader() error = %v", err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("calculateNDEFHeader() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCalculateNDEFHeader_TooLarge(t *testing.T) {
	t.Parallel()

	// Test with a payload that's too large (> 65535)
	largeInput := make([]byte, 65536)
	_, err := calculateNDEFHeader(largeInput)
	if err == nil {
		t.Fatal("Expected error for input larger than 65535 bytes, but got nil")
	}

	expectedMsg := "NDEF payload too large"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message %q, got %q", expectedMsg, err.Error())
	}
}

func TestValidateNDEFMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []byte
		wantErr bool
	}{
		{
			name:    "Valid message",
			input:   []byte{0x03, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05, 0xFE},
			wantErr: false,
		},
		{
			name:    "Too short",
			input:   []byte{0x01, 0x02},
			wantErr: true,
		},
		{
			name:    "No NDEF TLV",
			input:   []byte{0x01, 0x02, 0x04, 0x05, 0x06},
			wantErr: true,
		},
		{
			name:    "Empty",
			input:   []byte{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateNDEFMessage(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Error("ValidateNDEFMessage() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("ValidateNDEFMessage() unexpected error: %v", err)
				}
			}
		})
	}
}
