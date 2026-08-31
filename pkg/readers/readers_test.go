/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
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

package readers

import "testing"

func TestNormalizeDriverID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple_serial to simpleserial",
			input:    "simple_serial",
			expected: "simpleserial",
		},
		{
			name:     "acr122_pcsc to acr122pcsc",
			input:    "acr122_pcsc",
			expected: "acr122pcsc",
		},
		{
			name:     "pn532_uart to pn532uart",
			input:    "pn532_uart",
			expected: "pn532uart",
		},
		{
			name:     "already normalized stays same",
			input:    "simpleserial",
			expected: "simpleserial",
		},
		{
			name:     "multiple underscores",
			input:    "legacy_pn532_uart",
			expected: "legacypn532uart",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only underscores",
			input:    "___",
			expected: "",
		},
		{
			name:     "single character",
			input:    "a",
			expected: "a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := NormalizeDriverID(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeDriverID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// A driver ID is user-supplied config, so its case is not part of the name.
func TestNormalizeDriverID_FoldsCase(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"PN532":         "pn532",
		"Simple_Serial": "simpleserial",
		"ACR122_PCSC":   "acr122pcsc",
		"OpticalDrive":  "opticaldrive",
	}
	for input, want := range tests {
		if got := NormalizeDriverID(input); got != want {
			t.Errorf("NormalizeDriverID(%q) = %q, want %q", input, got, want)
		}
	}
}

// Drivers check the ID a config entry asked for against the IDs they answer
// to. An exact comparison meant a reader configured as "PN532" was skipped
// with nothing logged, because connectReaders had already matched it on the
// normalized ID and the driver then refused to open it.
func TestMatchesDriverID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		driver string
		ids    []string
		want   bool
	}{
		{name: "exact", ids: []string{"pn532"}, driver: "pn532", want: true},
		{name: "config uses upper case", ids: []string{"pn532"}, driver: "PN532", want: true},
		{name: "config uses mixed case", ids: []string{"pn532"}, driver: "Pn532", want: true},
		{name: "config uses an underscore", ids: []string{"simpleserial"}, driver: "simple_serial", want: true},
		{
			name: "driver advertises the underscored form",
			ids:  []string{"simple_serial"}, driver: "simpleserial", want: true,
		},
		{name: "matches a later ID", ids: []string{"a", "PN532"}, driver: "pn532", want: true},
		{name: "different driver", ids: []string{"pn532"}, driver: "libnfc", want: false},
		{name: "no ids", ids: nil, driver: "pn532", want: false},
		{name: "empty driver", ids: []string{"pn532"}, driver: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := MatchesDriverID(tt.ids, tt.driver); got != tt.want {
				t.Errorf("MatchesDriverID(%v, %q) = %v, want %v", tt.ids, tt.driver, got, tt.want)
			}
		})
	}
}
