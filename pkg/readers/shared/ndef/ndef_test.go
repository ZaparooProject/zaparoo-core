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

func TestValidateNDEFRecordHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		wantErr string
		payload []byte
	}{
		{
			name: "Valid short text record",
			// flags=0xD1 (MB=1,ME=1,CF=0,SR=1,IL=0,TNF=1), typeLen=1, payloadLen=5, type='T', payload
			payload: []byte{0xD1, 0x01, 0x05, 'T', 0x02, 'e', 'n', 'H', 'i'},
			wantErr: "",
		},
		{
			name: "Valid short URI record",
			// flags=0xD1 (MB=1,ME=1,CF=0,SR=1,IL=0,TNF=1), typeLen=1, payloadLen=4, type='U', payload
			payload: []byte{0xD1, 0x01, 0x04, 'U', 0x03, 'a', '.', 'b'},
			wantErr: "",
		},
		{
			name:    "Too short",
			payload: []byte{0xD1, 0x01},
			wantErr: "record too short",
		},
		{
			name: "Invalid TNF (7 is reserved)",
			// flags=0xD7 (TNF=7)
			payload: []byte{0xD7, 0x01, 0x01, 'T', 'x'},
			wantErr: "invalid TNF value 7",
		},
		{
			name: "MB flag not set",
			// flags=0x51 (MB=0,ME=1,CF=0,SR=1,IL=0,TNF=1)
			payload: []byte{0x51, 0x01, 0x01, 'T', 'x'},
			wantErr: "MB flag not set",
		},
		{
			name: "ME flag not set (multi-record)",
			// flags=0x91 (MB=1,ME=0,CF=0,SR=1,IL=0,TNF=1)
			payload: []byte{0x91, 0x01, 0x01, 'T', 'x'},
			wantErr: "ME flag not set",
		},
		{
			name: "Chunked record (CF set)",
			// flags=0xF1 (MB=1,ME=1,CF=1,SR=1,IL=0,TNF=1)
			payload: []byte{0xF1, 0x01, 0x01, 'T', 'x'},
			wantErr: "chunked records not supported",
		},
		{
			name: "Long record (SR not set)",
			// flags=0xC1 (MB=1,ME=1,CF=0,SR=0,IL=0,TNF=1)
			payload: []byte{0xC1, 0x01, 0x00, 0x00, 0x00, 0x01, 'T', 'x'},
			wantErr: "long records not supported",
		},
		{
			name: "Record with ID (IL set)",
			// flags=0xD9 (MB=1,ME=1,CF=0,SR=1,IL=1,TNF=1)
			payload: []byte{0xD9, 0x01, 0x01, 0x02, 'T', 'x', 'i', 'd'},
			wantErr: "records with ID not supported",
		},
		{
			name: "Truncated header (typeLen exceeds)",
			// flags=0xD1, typeLen=10, but only 2 more bytes
			payload: []byte{0xD1, 0x0A, 0x01, 'T', 'x'},
			wantErr: "truncated record header",
		},
		{
			name: "Truncated payload",
			// flags=0xD1, typeLen=1, payloadLen=10, but only 1 payload byte
			payload: []byte{0xD1, 0x01, 0x0A, 'T', 'x'},
			wantErr: "truncated payload",
		},
		{
			name: "Empty record with non-zero type length",
			// flags=0xD0 (TNF=0 empty), typeLen=1 (invalid for empty)
			payload: []byte{0xD0, 0x01, 0x00, 'T'},
			wantErr: "empty record must have zero lengths",
		},
		{
			name: "Empty record with non-zero payload length",
			// flags=0xD0 (TNF=0 empty), typeLen=0, payloadLen=1 (invalid)
			payload: []byte{0xD0, 0x00, 0x01, 'x'},
			wantErr: "empty record must have zero lengths",
		},
		{
			name: "Well-known record missing type",
			// flags=0xD1 (TNF=1), typeLen=0 (invalid for well-known)
			payload: []byte{0xD1, 0x00, 0x01, 'x'},
			wantErr: "well-known record must have type",
		},
		{
			name: "Valid empty record (TNF=0)",
			// flags=0xD0 (MB=1,ME=1,CF=0,SR=1,IL=0,TNF=0), typeLen=0, payloadLen=0
			payload: []byte{0xD0, 0x00, 0x00},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateNDEFRecordHeader(tt.payload)

			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("validateNDEFRecordHeader() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("validateNDEFRecordHeader() expected error containing %q but got none", tt.wantErr)
				} else if !bytes.Contains([]byte(err.Error()), []byte(tt.wantErr)) {
					t.Errorf("validateNDEFRecordHeader() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}
