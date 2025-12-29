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

package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateZapScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		input           string
		expectMsgSubstr string
		expectValid     bool
	}{
		{
			name:            "empty string",
			input:           "",
			expectValid:     false,
			expectMsgSubstr: "",
		},
		{
			name:            "whitespace only",
			input:           "   \t\n  ",
			expectValid:     false,
			expectMsgSubstr: "",
		},
		{
			name:            "valid launch command",
			input:           "**launch.system:nes",
			expectValid:     true,
			expectMsgSubstr: "Valid: launch.system",
		},
		{
			name:            "valid launch with path",
			input:           "/path/to/game.nes",
			expectValid:     true,
			expectMsgSubstr: "Valid: launch",
		},
		{
			name:            "valid http command",
			input:           "**http.get:http://example.com",
			expectValid:     true,
			expectMsgSubstr: "Valid: http.get",
		},
		{
			name:            "multiple valid commands",
			input:           "**launch.system:nes\n**input.key:enter",
			expectValid:     true,
			expectMsgSubstr: "Valid:",
		},
		{
			name:            "unknown command",
			input:           "**unknown.command:value",
			expectValid:     false,
			expectMsgSubstr: "Unknown command: unknown.command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			valid, message := validateZapScript(tt.input)
			assert.Equal(t, tt.expectValid, valid)
			if tt.expectMsgSubstr != "" {
				assert.Contains(t, message, tt.expectMsgSubstr)
			}
		})
	}
}
