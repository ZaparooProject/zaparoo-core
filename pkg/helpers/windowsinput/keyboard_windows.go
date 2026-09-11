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
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

//nolint:gochecknoglobals // lazily resolved system DLL procedures
var (
	moduser32 = windows.NewLazySystemDLL("user32.dll")

	procSendInput = moduser32.NewProc("SendInput")
)

const (
	inputKeyboard = 1

	keyEventExtendedKey = 0x0001
	keyEventKeyUp       = 0x0002
	keyEventScancode    = 0x0008
)

// ErrUnknownKey is returned for a keycode with no Windows scancode.
var ErrUnknownKey = errors.New("key is not supported on windows")

// keybdInput mirrors the Win32 KEYBDINPUT structure.
type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// input mirrors the Win32 INPUT structure. The trailing padding makes the
// struct match the size of INPUT's largest union member, MOUSEINPUT, on both
// 32-bit and 64-bit builds; SendInput rejects a mismatched cbSize.
type input struct {
	typ uint32
	ki  keybdInput
	_   [8]byte
}

// Keyboard injects keystrokes into the session Core is running in.
//
// It has no device to open or close: SendInput posts to the calling session's
// input queue, so a Keyboard is only meaningful in an interactive session.
// Windows filters injected input in two cases neither this type nor its caller
// can detect or defeat, both of which need an elevated process to fix: UIPI
// drops input aimed at a window owned by a higher integrity level, and nothing
// reaches the secure desktop (UAC prompts, the lock screen, Ctrl+Alt+Del).
type Keyboard struct{}

// NewKeyboard returns a virtual keyboard for the current session.
func NewKeyboard() (*Keyboard, error) {
	if err := procSendInput.Find(); err != nil {
		return nil, fmt.Errorf("user32!SendInput unavailable: %w", err)
	}
	return &Keyboard{}, nil
}

// KeyDown presses a key, identified by its evdev keycode.
func (*Keyboard) KeyDown(code int) error {
	return sendKey(code, false)
}

// KeyUp releases a key, identified by its evdev keycode.
func (*Keyboard) KeyUp(code int) error {
	return sendKey(code, true)
}

// Close releases the keyboard. Nothing is held open, so this always succeeds.
func (*Keyboard) Close() error {
	return nil
}

func sendKey(code int, up bool) error {
	scancode, extended, ok := Scancode(code)
	if !ok {
		return fmt.Errorf("%w: keycode %d", ErrUnknownKey, code)
	}

	// Scancodes address physical keys, so what a key produces still depends on
	// the active keyboard layout. That matches how the uinput backend behaves
	// on Linux, and it is what games reading raw input expect.
	flags := uint32(keyEventScancode)
	if extended {
		flags |= keyEventExtendedKey
	}
	if up {
		flags |= keyEventKeyUp
	}

	in := input{
		typ: inputKeyboard,
		ki: keybdInput{
			wScan:   scancode,
			dwFlags: flags,
		},
	}

	sent, _, err := procSendInput.Call(
		1,
		uintptr(unsafe.Pointer(&in)), //nolint:gosec // required for Windows API
		unsafe.Sizeof(in),
	)
	if sent != 1 {
		// SendInput reports a short count when another process has the input
		// desktop locked, or when input is blocked outright.
		return fmt.Errorf("SendInput rejected keycode %d: %w", code, err)
	}
	return nil
}
