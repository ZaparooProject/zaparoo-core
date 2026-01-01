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

package tty2oled

import (
	"strings"
	"testing"
)

func TestMapSystemToPicture(t *testing.T) {
	t.Parallel()
	tests := []struct {
		systemID string
		expected string
	}{
		{"Genesis", "Genesis"},
		{"NES", "NES"},
		{"SNES", "SNES"},
		{"PSX", "PSX"},
		{"Dreamcast", "Dreamcast"},
		{"DOS", "AO486"},
		{"PC", "AO486"},
		{"AdventureVision", "AVision"},
		{"Apogee", "APOGEE"},
		{"AppleI", "APPLE-I"},
		{"UnknownSystem", ""}, // Should return empty string for unmapped systems
	}

	for _, tt := range tests {
		t.Run(tt.systemID, func(t *testing.T) {
			t.Parallel()
			result := mapSystemToPicture(tt.systemID)
			if result != tt.expected {
				t.Errorf("mapSystemToPicture(%q) = %q, want %q", tt.systemID, result, tt.expected)
			}
		})
	}
}

func TestSelectPictureVariant(t *testing.T) {
	t.Parallel()
	tests := []struct {
		baseName    string
		description string
		expectAlts  bool
	}{
		{baseName: "Genesis", description: "Genesis should have alternatives", expectAlts: true},
		{baseName: "PSX", description: "PSX should have alternatives", expectAlts: true},
		{baseName: "NES", description: "NES should not have alternatives", expectAlts: false},
		{baseName: "SNES", description: "SNES should not have alternatives", expectAlts: false},
		{
			baseName:    "NonExistentPicture",
			description: "Unknown pictures should not have alternatives",
			expectAlts:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()
			result := selectPictureVariant(tt.baseName)

			// Should never return empty string
			if result == "" {
				t.Errorf("selectPictureVariant(%q) returned empty string", tt.baseName)
			}

			// Should start with base name
			if !strings.HasPrefix(result, tt.baseName) {
				t.Errorf("selectPictureVariant(%q) = %q, should start with base name", tt.baseName, result)
			}

			if tt.expectAlts {
				// For pictures with alternatives, the result should be either the base name
				// or the base name with an _altN suffix
				if result != tt.baseName && !strings.Contains(result, "_alt") {
					t.Errorf("selectPictureVariant(%q) = %q, expected either base name or _alt variant",
						tt.baseName, result)
				}
			} else {
				// For pictures without alternatives, should return exactly the base name
				if result != tt.baseName {
					t.Errorf("selectPictureVariant(%q) = %q, expected %q", tt.baseName, result, tt.baseName)
				}
			}
		})
	}
}

func TestSelectPictureVariantConsistency(t *testing.T) {
	t.Parallel()
	// Test that the same input always returns the same output (deterministic)
	baseName := "Genesis"

	first := selectPictureVariant(baseName)
	for i := range 10 {
		result := selectPictureVariant(baseName)
		if result != first {
			t.Errorf("selectPictureVariant(%q) is not consistent: first=%q, iteration %d=%q",
				baseName, first, i, result)
		}
	}
}

func TestDOSToAO486Mapping(t *testing.T) {
	t.Parallel()
	// Specific test for the DOS -> AO486 mapping that was causing crashes
	dosMapping := mapSystemToPicture("DOS")
	pcMapping := mapSystemToPicture("PC")

	if dosMapping != "AO486" {
		t.Errorf("DOS should map to AO486, got: %s", dosMapping)
	}

	if pcMapping != "AO486" {
		t.Errorf("PC should map to AO486, got: %s", pcMapping)
	}

	// Test that AO486 variant selection works
	variant := selectPictureVariant("AO486")
	// AO486 is in our picturesWithAlts map with 1 alt, so it might return AO486_alt1
	if variant != "AO486" && variant != "AO486_alt1" {
		t.Errorf("AO486 variant selection returned unexpected result: %s", variant)
	}
}
