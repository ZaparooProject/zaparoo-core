//go:build linux

/*
Zaparoo Core
Copyright (C) 2023 Gareth Jones

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

package tags

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func TestCalculateNdefHeader(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		input []byte
		want  []byte
	}{
		"minimum": {input: bytes.Repeat([]byte{0x69}, 1), want: []byte{0x03, 0x01}},
		"255":     {input: bytes.Repeat([]byte{0x69}, 255), want: []byte{0x03, 0xFF, 0x00, 0xFF}},
		"256":     {input: bytes.Repeat([]byte{0x69}, 256), want: []byte{0x03, 0xFF, 0x01, 0x00}},
		"257":     {input: bytes.Repeat([]byte{0x69}, 257), want: []byte{0x03, 0xFF, 0x01, 0x01}},
		"258":     {input: bytes.Repeat([]byte{0x69}, 258), want: []byte{0x03, 0xFF, 0x01, 0x02}},
		"512":     {input: bytes.Repeat([]byte{0x69}, 512), want: []byte{0x03, 0xFF, 0x02, 0x00}},
		"maximum": {input: bytes.Repeat([]byte{0x69}, 865), want: []byte{0x03, 0xFF, 0x03, 0x61}},
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

func TestParseRecordText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		expected  string
		input     []byte
		shouldErr bool
	}{
		{
			name:      "valid NDEF with simple text",
			input:     []byte{0x00, 0x54, 0x02, 0x65, 0x6E, 0x48, 0x65, 0x6C, 0x6C, 0x6F, 0xFE},
			expected:  "Hello",
			shouldErr: false,
		},
		{
			name: "valid NDEF with token text",
			input: []byte{
				0x00, 0x54, 0x02, 0x65, 0x6E, 0x2A, 0x2A, 0x72, 0x61, 0x6E, 0x64, 0x6F, 0x6D,
				0x3A, 0x73, 0x6E, 0x65, 0x73, 0xFE,
			},
			expected:  "**random:snes",
			shouldErr: false,
		},
		{
			name:      "valid NDEF with single character",
			input:     []byte{0x00, 0x54, 0x02, 0x65, 0x6E, 0x41, 0xFE},
			expected:  "A",
			shouldErr: false,
		},
		{
			name:      "valid NDEF with special characters",
			input:     []byte{0x00, 0x54, 0x02, 0x65, 0x6E, 0x21, 0x40, 0x23, 0x24, 0x25, 0xFE},
			expected:  "!@#$%",
			shouldErr: false,
		},
		{
			name:      "valid NDEF with numbers",
			input:     []byte{0x00, 0x54, 0x02, 0x65, 0x6E, 0x31, 0x32, 0x33, 0x34, 0x35, 0xFE},
			expected:  "12345",
			shouldErr: false,
		},
		{
			name:      "valid NDEF with empty text",
			input:     []byte{0x00, 0x54, 0x02, 0x65, 0x6E, 0xFE},
			expected:  "",
			shouldErr: false,
		},
		{
			name:      "NDEF start at beginning",
			input:     []byte{0x54, 0x02, 0x65, 0x6E, 0x74, 0x65, 0x73, 0x74, 0xFE},
			expected:  "test",
			shouldErr: false,
		},
		{
			name:      "NDEF with padding data",
			input:     []byte{0x00, 0x00, 0x00, 0x54, 0x02, 0x65, 0x6E, 0x64, 0x61, 0x74, 0x61, 0xFE, 0x00, 0x00},
			expected:  "data",
			shouldErr: false,
		},
		{
			name:      "missing NDEF start",
			input:     []byte{0x00, 0x00, 0x65, 0x6E, 0x74, 0x65, 0x73, 0x74, 0xFE},
			expected:  "",
			shouldErr: true,
		},
		{
			name:      "missing NDEF end",
			input:     []byte{0x00, 0x54, 0x02, 0x65, 0x6E, 0x74, 0x65, 0x73, 0x74},
			expected:  "",
			shouldErr: true,
		},
		{
			name:      "NDEF end before start",
			input:     []byte{0xFE, 0x54, 0x02, 0x65, 0x6E, 0x74, 0x65, 0x73, 0x74},
			expected:  "",
			shouldErr: true,
		},
		{
			name:      "NDEF start at end of buffer",
			input:     []byte{0x00, 0x00, 0x54, 0x02, 0x65, 0x6E},
			expected:  "",
			shouldErr: true,
		},
		{
			name:      "empty input",
			input:     []byte{},
			expected:  "",
			shouldErr: true,
		},
		{
			name:      "only NDEF markers",
			input:     []byte{0x54, 0x02, 0x65, 0x6E, 0xFE},
			expected:  "",
			shouldErr: false,
		},
		{
			name:      "multiple NDEF starts (uses first)",
			input:     []byte{0x54, 0x02, 0x65, 0x6E, 0x41, 0x54, 0x02, 0x65, 0x6E, 0x42, 0xFE},
			expected:  "A\x54\x02en\x42",
			shouldErr: false,
		},
		{
			name:      "malformed start sequence",
			input:     []byte{0x54, 0x02, 0x65, 0x74, 0x65, 0x73, 0x74, 0xFE},
			expected:  "",
			shouldErr: true,
		},
		{
			name:      "binary data in text",
			input:     []byte{0x54, 0x02, 0x65, 0x6E, 0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE},
			expected:  "\x00\x01\x02\x03\xFF",
			shouldErr: false,
		},
		{
			name:      "very long text",
			input:     append(append([]byte{0x54, 0x02, 0x65, 0x6E}, bytes.Repeat([]byte{0x41}, 100)...), 0xFE),
			expected:  strings.Repeat("A", 100),
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := ParseRecordText(tt.input)

			if tt.shouldErr {
				if err == nil {
					t.Error("ParseRecordText() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseRecordText() unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("ParseRecordText() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestBuildMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{input: "**random:snes", want: "0314d101105402656e2a2a72616e646f6d3a736e6573fe"},
		{input: "A", want: "0308d101045402656e41fe"},
		{input: "AAAA", want: "030bd101075402656e41414141fe"},
		{
			input: strings.Repeat("A", 512),
			want:  "03ff020ac101000002035402656e4141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141fe", //nolint:revive // Long test data string
		},
	}

	for name, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := BuildMessage(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			want, err := hex.DecodeString(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("test %v, expected: %v, got: %v", name, hex.EncodeToString(want), hex.EncodeToString(got))
			}
		})
	}
}
