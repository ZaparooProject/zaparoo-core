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

package inputmacro

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyControl(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  Control
	}{
		{name: "delay", input: "{delay:500ms}", want: Control{Action: ActionDelay, Value: "500ms"}},
		{name: "press", input: "{press:up}", want: Control{Action: ActionPress, Value: "up"}},
		{name: "release", input: "{release:up}", want: Control{Action: ActionRelease, Value: "up"}},
		{name: "hold", input: "{hold:up:1s}", want: Control{Action: ActionHold, Value: "up:1s"}},
		{name: "press sigil", input: "{_up}", want: Control{Action: ActionPress, Value: "up"}},
		{name: "release sigil", input: "{^up}", want: Control{Action: ActionRelease, Value: "up"}},
		{name: "hold sigil", input: "{~up:1s}", want: Control{Action: ActionHold, Value: "up:1s"}},
		{name: "standard key", input: "{up}"},
		{name: "unbraced", input: "press:up"},
		{name: "empty sigil", input: "{_}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ClassifyControl(tt.input))
		})
	}
}

func TestKeyForToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  string
		want string
		ok   bool
	}{
		{raw: "a", want: "a", ok: true},
		{raw: "{enter}", want: "{enter}", ok: true},
		{raw: "{ctrl+q}", want: "{ctrl+q}", ok: true},
		{raw: "{press:super}", want: "{super}", ok: true},
		{raw: "{release:super}", want: "{super}", ok: true},
		{raw: "{hold:super}", want: "{super}", ok: true},
		{raw: "{hold:super:500}", want: "{super}", ok: true},
		{raw: "{_super}", want: "{super}", ok: true},
		{raw: "{^super}", want: "{super}", ok: true},
		{raw: "{~super:1s}", want: "{super}", ok: true},
		{raw: "{press:a}", want: "a", ok: true},
		{raw: "{press:ctrl+alt+delete}", want: "{ctrl+alt+delete}", ok: true},
		{raw: "{delay:500}", want: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()
			got, ok := KeyForToken(tt.raw)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSplitHold(t *testing.T) {
	t.Parallel()

	name, duration := SplitHold("esc:500")
	assert.Equal(t, "esc", name)
	assert.Equal(t, "500", duration)

	name, duration = SplitHold("esc")
	assert.Equal(t, "esc", name)
	assert.Empty(t, duration)
}
