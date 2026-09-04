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

//go:build windows

package windowsinput

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInputStructSize pins the INPUT layout. SendInput takes the struct size as
// an argument and silently does nothing when it disagrees, so a layout mistake
// would look like input that is accepted and then vanishes.
func TestInputStructSize(t *testing.T) {
	t.Parallel()

	want := uintptr(40) // 64-bit: 4 type + 4 padding + 32 union
	if unsafe.Sizeof(uintptr(0)) == 4 {
		want = 28 // 32-bit: 4 type + 24 union
	}
	assert.Equal(t, want, unsafe.Sizeof(input{}), "INPUT size must match the Win32 definition")

	// The keyboard arm must sit at the union offset, not immediately after the
	// type field.
	kiOffset := unsafe.Offsetof(input{}.ki)
	wantOffset := uintptr(8)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		wantOffset = 4
	}
	assert.Equal(t, wantOffset, kiOffset, "KEYBDINPUT must start at the union offset")
}

func TestNewKeyboard(t *testing.T) {
	t.Parallel()

	kbd, err := NewKeyboard()
	require.NoError(t, err)
	require.NotNil(t, kbd)
	assert.NoError(t, kbd.Close())
}

func TestKeyDownRejectsUnknownKey(t *testing.T) {
	t.Parallel()

	kbd, err := NewKeyboard()
	require.NoError(t, err)

	require.ErrorIs(t, kbd.KeyDown(200), ErrUnknownKey)
	require.ErrorIs(t, kbd.KeyUp(200), ErrUnknownKey)
}
