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

package windowsinput

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput/keyboardmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryMappedKeyHasScancode fails when a key is added to KeyboardMap
// without a Windows scancode, which would otherwise only show up as an
// unsupported key at runtime on Windows.
func TestEveryMappedKeyHasScancode(t *testing.T) {
	t.Parallel()

	for name, code := range keyboardmap.KeyboardMap {
		// Negative codes are shift-modified aliases; the engine presses shift
		// itself and then the positive base code.
		base := code
		if base < 0 {
			base = -base
		}
		_, _, ok := Scancode(base)
		_, isVirtual := VirtualKey(base)
		assert.True(t, ok || isVirtual, "key %q (keycode %d) has no windows scancode", name, base)
	}
}

func TestScancode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		code     int
		want     uint16
		extended bool
		ok       bool
	}{
		{name: "escape", code: 1, want: 0x01, ok: true},
		{name: "a", code: 30, want: 0x1E, ok: true},
		{name: "enter", code: 28, want: 0x1C, ok: true},
		{name: "left shift", code: 42, want: 0x2A, ok: true},
		{name: "right shift", code: 54, want: 0x36, ok: true},
		{name: "left alt", code: 56, want: 0x38, ok: true},
		{name: "f1", code: 59, want: 0x3B, ok: true},
		{name: "f11 past the gap", code: 87, want: 0x57, ok: true},
		{name: "f12 past the gap", code: 88, want: 0x58, ok: true},
		{name: "up is extended", code: 103, want: 0x48, extended: true, ok: true},
		{name: "delete is extended", code: 111, want: 0x53, extended: true, ok: true},
		{name: "right ctrl is extended", code: 97, want: 0x1D, extended: true, ok: true},
		{name: "left meta is extended", code: 125, want: 0x5B, extended: true, ok: true},
		{name: "mute is extended", code: 113, want: 0x20, extended: true, ok: true},
		{name: "play pause is extended", code: 164, want: 0x22, extended: true, ok: true},
		{name: "102nd is plain", code: 86, want: 0x56, ok: true},
		{name: "f13 is plain", code: 183, want: 0x64, ok: true},
		{name: "f24 is plain", code: 194, want: 0x76, ok: true},
		{name: "84 has no scancode", code: 84, ok: false},
		{name: "85 has no scancode", code: 85, ok: false},
		{name: "pause has no scancode", code: 119, ok: false},
		{name: "unmapped", code: 200, ok: false},
		{name: "zero", code: 0, ok: false},
		{name: "negative", code: -30, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sc, extended, ok := Scancode(tt.code)
			require.Equal(t, tt.ok, ok)
			if !tt.ok {
				return
			}
			assert.Equal(t, tt.want, sc)
			assert.Equal(t, tt.extended, extended)
		})
	}
}

// TestExtendedAndIdentityDoNotOverlap guards the assumption that the identity
// range, the extended and plain tables, and the virtual-key table describe
// disjoint sets of keycodes.
func TestExtendedAndIdentityDoNotOverlap(t *testing.T) {
	t.Parallel()

	inIdentityRange := func(code int) bool {
		return (code >= 1 && code <= 83) || code == 87 || code == 88
	}
	for code := range extendedScancodes {
		assert.False(t, inIdentityRange(code), "keycode %d is in both the identity range and the extended table", code)
		_, plain := plainScancodes[code]
		assert.False(t, plain, "keycode %d is in both the extended and plain tables", code)
	}
	for code := range plainScancodes {
		assert.False(t, inIdentityRange(code), "keycode %d is in both the identity range and the plain table", code)
	}
	for code := range virtualKeys {
		_, _, ok := Scancode(code)
		assert.False(t, ok, "keycode %d has both a scancode and a virtual key", code)
	}
}

func TestVirtualKey(t *testing.T) {
	t.Parallel()

	vk, ok := VirtualKey(119)
	require.True(t, ok)
	assert.Equal(t, uint16(0x13), vk, "KEY_PAUSE sends VK_PAUSE")

	_, ok = VirtualKey(30)
	assert.False(t, ok, "ordinary keys are sent by scancode")
}
