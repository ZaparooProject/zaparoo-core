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

package rs232barcode

import (
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

// FuzzParseLine tests barcode line parsing with random inputs
// to discover edge cases in STX/ETX framing and string manipulation.
func FuzzParseLine(f *testing.F) {
	// Simple barcodes
	f.Add("1234567890")
	f.Add("ABC-123-XYZ")
	f.Add("https://example.com/product/123")

	// STX/ETX framing (control characters \x02 and \x03)
	f.Add("\x02BARCODE\x03")
	f.Add("\x02BARCODE")         // STX only
	f.Add("BARCODE\x03")         // ETX only
	f.Add("  \x02BARCODE\x03  ") // With surrounding whitespace

	// Multiple/nested control characters
	f.Add("\x02\x02DATA\x03\x03")
	f.Add("\x02\x02\x02CODE\x03\x03\x03")

	// STX/ETX in data
	f.Add("CODE\x02MID\x03END")
	f.Add("START\x02CODE\x03END\x02\x03")

	// Only control characters
	f.Add("\x02\x03")
	f.Add("\x02")
	f.Add("\x03")

	// Empty and whitespace
	f.Add("")
	f.Add("   ")
	f.Add("\r")
	f.Add("\n")
	f.Add("\r\n")
	f.Add("   \r\n")
	f.Add("\t\t\t")

	// Mixed line endings
	f.Add("CODE\r")
	f.Add("CODE\n")
	f.Add("CODE\r\n")
	f.Add("\r\nCODE\r\n")

	// Large barcodes (approaching 8KB limit)
	f.Add(strings.Repeat("A", 1000))
	f.Add(strings.Repeat("0123456789", 700)) // 7KB

	// Unicode content
	f.Add("barcode-日本") //nolint:gosmopolitan // testing Unicode
	f.Add("code-Россия")
	f.Add("🎮-game-code")

	// Control characters other than STX/ETX
	f.Add("CODE\x00MID")
	f.Add("CODE\x01\x04\x05")
	f.Add("\x1FCODE\x7F")

	// Special characters in barcodes
	f.Add("!@#$%^&*()")
	f.Add("<>{}[]|\\")
	f.Add("path/to/file.txt")

	f.Fuzz(func(t *testing.T, line string) {
		// Create a reader instance for testing
		r := &Reader{
			device: config.ReadersConnect{
				Driver: "rs232barcode",
				Path:   "/dev/ttyUSB0",
			},
		}

		// Call the function - should never panic
		token, err := r.parseLine(line)

		// Property 1: Function should never return both nil token and error
		// (current implementation returns nil,nil for empty lines)
		if token == nil && err != nil {
			t.Errorf("Unexpected error with nil token: %v for input: %q", err, line)
		}

		// Property 2: If token is returned, it should have valid fields
		if token != nil {
			// Token type should be TypeBarcode
			if token.Type != tokens.TypeBarcode {
				t.Errorf("Token type should be TypeBarcode, got: %v", token.Type)
			}

			// UID, Text, and Data should be identical
			if token.UID != token.Text || token.Text != token.Data {
				t.Errorf("UID, Text, and Data should be identical: UID=%q, Text=%q, Data=%q",
					token.UID, token.Text, token.Data)
			}

			if token.Text == "" {
				t.Error("Token with empty text should not be returned")
			}

			// Token text length should not exceed input length
			if len(token.Text) > len(line) {
				t.Errorf("Token text longer than input: %d > %d", len(token.Text), len(line))
			}
		}

		// Property 3: Purely whitespace input should return nil token
		trimmed := strings.TrimSpace(strings.Trim(line, "\r"))
		trimmed = strings.TrimPrefix(trimmed, "\x02")
		trimmed = strings.TrimSuffix(trimmed, "\x03")
		if trimmed == "" && token != nil {
			t.Errorf("Empty/whitespace input should return nil token, got: %q", token.Text)
		}

		// Property 4: Deterministic - same input always produces same result
		token2, err2 := r.parseLine(line)
		if (token == nil) != (token2 == nil) {
			t.Errorf("Non-deterministic nil result for input: %q", line)
		}
		if (err == nil) != (err2 == nil) {
			t.Errorf("Non-deterministic error for input: %q", line)
		}
		if token != nil && token2 != nil && token.Text != token2.Text {
			t.Errorf("Non-deterministic token text: %q vs %q for input: %q",
				token.Text, token2.Text, line)
		}
	})
}
