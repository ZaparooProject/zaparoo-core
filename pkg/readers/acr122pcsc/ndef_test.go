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

package acr122pcsc

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestCalculateNdefHeader(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		input []byte
		want  []byte
	}{
		"empty":   {input: []byte{}, want: []byte{0x03, 0x00}},
		"minimum": {input: bytes.Repeat([]byte{0x69}, 1), want: []byte{0x03, 0x01}},
		"small":   {input: bytes.Repeat([]byte{0x69}, 10), want: []byte{0x03, 0x0A}},
		"254":     {input: bytes.Repeat([]byte{0x69}, 254), want: []byte{0x03, 0xFE}},
		"255":     {input: bytes.Repeat([]byte{0x69}, 255), want: []byte{0x03, 0xFF, 0x00, 0xFF}},
		"256":     {input: bytes.Repeat([]byte{0x69}, 256), want: []byte{0x03, 0xFF, 0x01, 0x00}},
		"257":     {input: bytes.Repeat([]byte{0x69}, 257), want: []byte{0x03, 0xFF, 0x01, 0x01}},
		"258":     {input: bytes.Repeat([]byte{0x69}, 258), want: []byte{0x03, 0xFF, 0x01, 0x02}},
		"512":     {input: bytes.Repeat([]byte{0x69}, 512), want: []byte{0x03, 0xFF, 0x02, 0x00}},
		"1024":    {input: bytes.Repeat([]byte{0x69}, 1024), want: []byte{0x03, 0xFF, 0x04, 0x00}},
		"maximum": {input: bytes.Repeat([]byte{0x69}, 865), want: []byte{0x03, 0xFF, 0x03, 0x61}},
		"65535":   {input: bytes.Repeat([]byte{0x69}, 65535), want: []byte{0x03, 0xFF, 0xFF, 0xFF}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := CalculateNdefHeader(tc.input)
			if err != nil {
				t.Fatalf("Got error: %v", err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("test %v, expected: %v, got: %v", name, hex.EncodeToString(tc.want), hex.EncodeToString(got))
			}
		})
	}
}

func TestCalculateNdefHeader_TooLarge(t *testing.T) {
	t.Parallel()

	// Test with a record that's too large (> 65535)
	largeInput := make([]byte, 65536)
	_, err := CalculateNdefHeader(largeInput)
	if err == nil {
		t.Fatal("Expected error for input larger than 65535 bytes, but got nil")
	}

	expectedError := "NDEF record too large for Type 2 tag format"
	if err.Error() != expectedError {
		t.Fatalf("Expected error message %q, got %q", expectedError, err.Error())
	}
}

func TestCalculateNdefHeader_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []byte
		want  []byte
	}{
		{
			name:  "boundary at 254",
			input: bytes.Repeat([]byte{0xAB}, 254),
			want:  []byte{0x03, 0xFE},
		},
		{
			name:  "boundary at 255",
			input: bytes.Repeat([]byte{0xAB}, 255),
			want:  []byte{0x03, 0xFF, 0x00, 0xFF},
		},
		{
			name:  "single byte",
			input: []byte{0xFF},
			want:  []byte{0x03, 0x01},
		},
		{
			name:  "two bytes",
			input: []byte{0xFF, 0x00},
			want:  []byte{0x03, 0x02},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := CalculateNdefHeader(tt.input)
			if err != nil {
				t.Fatalf("Got unexpected error: %v", err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("test %v, expected: %v, got: %v", tt.name, hex.EncodeToString(tt.want),
					hex.EncodeToString(got))
			}
		})
	}
}

func TestCalculateNdefHeader_Consistency(t *testing.T) {
	t.Parallel()

	// Test that repeated calls with the same input produce the same output
	input := bytes.Repeat([]byte{0x42}, 100)

	first, err := CalculateNdefHeader(input)
	if err != nil {
		t.Fatalf("Got error on first call: %v", err)
	}

	second, err := CalculateNdefHeader(input)
	if err != nil {
		t.Fatalf("Got error on second call: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("CalculateNdefHeader is not deterministic: first=%v, second=%v",
			hex.EncodeToString(first), hex.EncodeToString(second))
	}
}
