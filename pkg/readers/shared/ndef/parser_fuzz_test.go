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

package ndef

import (
	"testing"
)

// FuzzParseToText tests the main NDEF parsing function with random binary inputs
// to discover edge cases in TLV parsing and NDEF record extraction.
func FuzzParseToText(f *testing.F) {
	// Valid minimal NDEF structures
	f.Add([]byte{0x03, 0x00, 0xFE})                               // Empty TLV payload
	f.Add([]byte{0x03, 0x01, 0x00, 0xFE})                         // Minimal TLV with one zero byte
	f.Add([]byte{0x03, 0x05, 0x00, 0x00, 0x00, 0x00, 0x00, 0xFE}) // Short format TLV

	// Long format TLV header (0xFF marker)
	f.Add([]byte{0x03, 0xFF, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0xFE})

	// Truncated/malformed inputs
	f.Add([]byte{})                 // Empty
	f.Add([]byte{0x03})             // Just TLV marker
	f.Add([]byte{0x03, 0xFF})       // Long format marker, no length
	f.Add([]byte{0x03, 0xFF, 0x00}) // Truncated long format length
	f.Add([]byte{0x01, 0x02, 0x03}) // No NDEF TLV marker
	f.Add([]byte{0x00, 0x00, 0x00, 0x00})

	// Length field edge cases
	f.Add([]byte{0x03, 0xFE, 0xFE})       // Max short format length (254)
	f.Add([]byte{0x03, 0xFF, 0xFF, 0xFF}) // Max long format length (65535)
	f.Add([]byte{0x03, 0x10})             // Claims 16 bytes, has none
	f.Add([]byte{0x03, 0xFF, 0x00, 0x10}) // Long format claims 16 bytes, has none

	// Multiple TLV markers
	f.Add([]byte{0x03, 0x01, 0x00, 0x03, 0x01, 0x00, 0xFE})

	// NDEF TLV at different offsets
	f.Add([]byte{0x00, 0x00, 0x03, 0x01, 0x00, 0xFE}) // TLV after some null bytes

	f.Fuzz(func(t *testing.T, data []byte) {
		result, err := ParseToText(data)
		_ = result

		// URI prefixes add up to 28 chars, so allow input length + 30 overhead
		if err == nil && len(result) > len(data)+30 {
			t.Errorf("Result unexpectedly long: input=%d bytes, result=%d chars",
				len(data), len(result))
		}

		if len(data) == 0 && err == nil {
			t.Error("Empty input should produce an error")
		}

		if len(data) < 4 && err == nil {
			t.Errorf("Short input (%d bytes) should produce an error", len(data))
		}

		result2, err2 := ParseToText(data)
		if err == nil && err2 == nil && result != result2 {
			t.Errorf("Non-deterministic result for input: %x", data)
		}
		if (err == nil) != (err2 == nil) {
			t.Errorf("Non-deterministic error for input: %x", data)
		}
	})
}

// FuzzValidateNDEFMessage tests NDEF message structure validation
// with random binary inputs to discover edge cases in TLV detection.
func FuzzValidateNDEFMessage(f *testing.F) {
	// Valid structures
	f.Add([]byte{0x03, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05, 0xFE})
	f.Add([]byte{0x00, 0x00, 0x03, 0x01, 0x00, 0xFE}) // TLV after padding

	// Invalid structures
	f.Add([]byte{})
	f.Add([]byte{0x01, 0x02})       // Too short
	f.Add([]byte{0x01, 0x02, 0x04}) // No TLV marker (0x03)
	f.Add([]byte{0x00, 0x00, 0x00, 0x00})
	f.Add([]byte{0x03}) // Just the marker

	// Edge cases
	f.Add([]byte{0x03, 0x03, 0x03, 0x03}) // Multiple 0x03 bytes
	f.Add([]byte{0x02, 0x03, 0x04, 0x05}) // 0x03 in second position

	f.Fuzz(func(t *testing.T, data []byte) {
		// Call the function - should never panic
		err := ValidateNDEFMessage(data)

		if len(data) == 0 && err == nil {
			t.Error("Empty data should produce an error")
		}

		if len(data) < 4 && err == nil {
			t.Errorf("Short data (%d bytes) should produce an error", len(data))
		}

		has03 := false
		for _, b := range data {
			if b == 0x03 {
				has03 = true
				break
			}
		}
		if !has03 && len(data) >= 4 && err == nil {
			t.Errorf("Data without 0x03 TLV marker should produce an error: %x", data)
		}

		err2 := ValidateNDEFMessage(data)
		if (err == nil) != (err2 == nil) {
			t.Errorf("Non-deterministic error for input: %x", data)
		}
	})
}

// FuzzExtractTLVPayload tests the internal TLV payload extraction logic.
func FuzzExtractTLVPayload(f *testing.F) {
	// Short format valid
	f.Add([]byte{0x03, 0x02, 0xAA, 0xBB}, 0)
	f.Add([]byte{0x03, 0x00}, 0) // Zero-length payload

	// Long format valid
	f.Add([]byte{0x03, 0xFF, 0x00, 0x02, 0xAA, 0xBB}, 0)

	// Edge cases
	f.Add([]byte{0x03, 0x02}, 0)             // Truncated short format
	f.Add([]byte{0x03, 0xFF, 0x00}, 0)       // Truncated long format
	f.Add([]byte{0x03, 0xFF, 0x00, 0x02}, 0) // Long format, no payload

	// Offset edge cases
	f.Add([]byte{0x00, 0x03, 0x02, 0xAA, 0xBB}, 1)
	f.Add([]byte{0x03, 0x02, 0xAA, 0xBB}, 10) // Offset beyond data

	f.Fuzz(func(t *testing.T, data []byte, offset int) {
		// Bound offset to prevent obvious panics from test harness
		if offset < 0 {
			offset = 0
		}

		result := extractTLVPayload(data, offset)

		if offset >= len(data) && result != nil {
			t.Errorf("Should return nil for offset %d beyond data length %d", offset, len(data))
		}

		if result != nil && len(result) > len(data) {
			t.Errorf("Result length %d exceeds input length %d", len(result), len(data))
		}

		result2 := extractTLVPayload(data, offset)
		if (result == nil) != (result2 == nil) {
			t.Errorf("Non-deterministic nil result for input: %x, offset: %d", data, offset)
		}
		if result != nil && result2 != nil && len(result) != len(result2) {
			t.Errorf("Non-deterministic result length for input: %x, offset: %d", data, offset)
		}
	})
}

// FuzzParseTextPayload tests text record payload parsing with various byte sequences.
func FuzzParseTextPayload(f *testing.F) {
	// Valid text payloads: [status byte][language][text]
	// Status byte: bit 7 = UTF-16 (0=UTF-8), bits 5-0 = language code length
	f.Add([]byte{0x02, 'e', 'n', 'H', 'e', 'l', 'l', 'o'}) // 2-char lang, "Hello"
	f.Add([]byte{0x02, 'e', 'n'})                          // Empty text
	f.Add([]byte{0x00})                                    // Zero-length language, empty text

	// Edge cases
	f.Add([]byte{})          // Empty
	f.Add([]byte{0x3F})      // langLen = 63 (max), no language bytes
	f.Add([]byte{0x3F, 'x'}) // langLen = 63, only 1 language byte
	f.Add([]byte{0x02})      // langLen = 2, no language bytes

	// Unicode text
	f.Add([]byte{0x02, 'e', 'n', 0xE4, 0xB8, 0xAD}) // UTF-8 Chinese chars

	// Invalid UTF-8 in text portion
	f.Add([]byte{0x02, 'e', 'n', 0xFF, 0xFE})

	f.Fuzz(func(t *testing.T, payload []byte) {
		result, err := parseTextPayload(payload)

		if len(payload) == 0 && err == nil {
			t.Error("Empty payload should produce an error")
		}

		if len(payload) > 0 {
			langLen := int(payload[0] & 0x3F)
			if len(payload) < 1+langLen && err == nil {
				t.Errorf("Should error when langLen (%d) exceeds available bytes (%d)",
					langLen, len(payload)-1)
			}
		}

		if err == nil && len(result) > len(payload) {
			t.Errorf("Result length %d exceeds payload length %d", len(result), len(payload))
		}

		result2, err2 := parseTextPayload(payload)
		if (err == nil) != (err2 == nil) {
			t.Errorf("Non-deterministic error for payload: %x", payload)
		}
		if err == nil && result != result2 {
			t.Errorf("Non-deterministic result for payload: %x", payload)
		}
	})
}

// FuzzParseURIPayload tests URI record payload parsing with various byte sequences.
func FuzzParseURIPayload(f *testing.F) {
	// Valid URI payloads: [prefix code][URI suffix]
	f.Add([]byte{0x00, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'}) // No prefix
	f.Add([]byte{0x01, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'}) // http://www.
	f.Add([]byte{0x02, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'}) // https://www.
	f.Add([]byte{0x03, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'}) // http://
	f.Add([]byte{0x04, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'}) // https://
	f.Add([]byte{0x00})                                                        // Empty URI suffix
	f.Add([]byte{0x23})                                                        // Max valid prefix code (35)

	// Invalid prefix codes
	f.Add([]byte{0x24}) // Just above max (36)
	f.Add([]byte{0xFF}) // Max byte value

	// Edge cases
	f.Add([]byte{})

	// Unicode in URI suffix
	f.Add([]byte{0x00, 0xE4, 0xB8, 0xAD}) // Chinese chars

	f.Fuzz(func(t *testing.T, payload []byte) {
		result, err := parseURIPayload(payload)

		if len(payload) == 0 && err == nil {
			t.Error("Empty payload should produce an error")
		}

		if len(payload) > 0 && payload[0] >= 36 && err == nil {
			t.Errorf("Invalid prefix code %d should produce an error", payload[0])
		}

		// Prefix codes 1-35 add a prefix, so result should be at least as long as suffix
		if err == nil && len(payload) > 0 {
			prefixCode := int(payload[0])
			if prefixCode > 0 && prefixCode < 36 {
				suffixLen := len(payload) - 1
				if len(result) < suffixLen {
					t.Errorf("Result %d shorter than suffix %d for prefix code %d",
						len(result), suffixLen, prefixCode)
				}
			}
		}

		result2, err2 := parseURIPayload(payload)
		if (err == nil) != (err2 == nil) {
			t.Errorf("Non-deterministic error for payload: %x", payload)
		}
		if err == nil && result != result2 {
			t.Errorf("Non-deterministic result for payload: %x", payload)
		}
	})
}
