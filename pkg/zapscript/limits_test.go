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

package zapscript

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateScriptLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		wantErr bool
	}{
		{name: "empty", text: "", wantErr: false},
		{name: "ordinary script", text: "**launch:/games/snes/mario.sfc", wantErr: false},
		{
			name:    "the largest tag in common use",
			text:    strings.Repeat("A", 888),
			wantErr: false,
		},
		{name: "exactly at the limit", text: strings.Repeat("A", MaxScriptLength), wantErr: false},
		{name: "one byte over", text: strings.Repeat("A", MaxScriptLength+1), wantErr: true},
		{name: "far over", text: strings.Repeat("A", 32000), wantErr: true},
		{
			name: "multibyte counted as bytes, not runes",
			// Each rune is 3 bytes, so this is under the rune count but
			// over the byte limit.
			text:    strings.Repeat("あ", MaxScriptLength/2),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateScriptLength(tt.text)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrScriptTooLong)
		})
	}
}
