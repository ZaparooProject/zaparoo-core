//go:build linux

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

package tags

import "testing"

func TestIsMifarePermissionBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		block    int
		expected bool
	}{
		{name: "sector zero trailer", block: 3, expected: true},
		{name: "first writable block after trailer", block: 4, expected: false},
		{name: "sector one trailer", block: 7, expected: true},
		{name: "block before final trailer", block: 59, expected: true},
		{name: "first block in final sector", block: 60, expected: false},
		{name: "final trailer", block: 63, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := isMifarePermissionBlock(tt.block); got != tt.expected {
				t.Fatalf("isMifarePermissionBlock(%d) = %v, want %v", tt.block, got, tt.expected)
			}
		})
	}
}
