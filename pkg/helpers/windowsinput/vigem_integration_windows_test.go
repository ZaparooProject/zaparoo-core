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
	"testing"
	"time"
	"unsafe"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput/gamepadmap"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// XInput is how a game sees an Xbox 360 pad, so reading the virtual pad back
// through it proves the whole path: the bus accepted the device, Windows
// enumerated it, and the reports arrive as button state. Without it a test can
// only show that the IOCTLs returned success.
//
//nolint:gochecknoglobals // lazily resolved system DLL procedures
var (
	modxinput          = windows.NewLazySystemDLL("xinput1_4.dll")
	modxinputFallback  = windows.NewLazySystemDLL("xinput9_1_0.dll")
	procXInputGetState = modxinput.NewProc("XInputGetState")
)

const (
	xinputMaxUsers          = 4
	errorDeviceNotConnected = 1167
)

// xinputState mirrors XINPUT_STATE. Its gamepad half has the same layout as
// the report we submit, which is the point: what goes in comes back out.
type xinputState struct {
	packetNumber uint32
	gamepad      xusbReport
}

func xinputGetState(t *testing.T, index uint32) (xinputState, bool) {
	t.Helper()

	var state xinputState
	ret, _, _ := procXInputGetState.Call(
		uintptr(index),
		uintptr(unsafe.Pointer(&state)), //nolint:gosec // required for Windows API
	)
	switch ret {
	case 0:
		return state, true
	case errorDeviceNotConnected:
		return state, false
	default:
		t.Fatalf("XInputGetState(%d) failed: %d", index, ret)
		return state, false
	}
}

// connectedPads reports which XInput slots are currently filled.
func connectedPads(t *testing.T) map[uint32]bool {
	t.Helper()

	pads := make(map[uint32]bool, xinputMaxUsers)
	for i := range uint32(xinputMaxUsers) {
		if _, ok := xinputGetState(t, i); ok {
			pads[i] = true
		}
	}
	return pads
}

// waitFor polls until cond holds, because a virtual device takes a moment to
// enumerate and a report only lands on the next poll of the pad.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// This test needs the ViGEmBus driver, so it skips on a machine without it,
// which includes CI.
func TestGamepadIsVisibleToXInput(t *testing.T) {
	if err := procXInputGetState.Find(); err != nil {
		procXInputGetState = modxinputFallback.NewProc("XInputGetState")
		if err := procXInputGetState.Find(); err != nil {
			t.Skip("XInput is unavailable:", err)
		}
	}

	before := connectedPads(t)

	pad, err := NewGamepad()
	if errors.Is(err, ErrDriverMissing) {
		t.Skip("the ViGEmBus driver is not installed")
	}
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pad.Close())
	})

	// The new slot is whichever one was not filled a moment ago.
	var index uint32
	found := waitFor(t, 5*time.Second, func() bool {
		for i := range uint32(xinputMaxUsers) {
			if before[i] {
				continue
			}
			if _, ok := xinputGetState(t, i); ok {
				index = i
				return true
			}
		}
		return false
	})
	require.True(t, found, "the virtual pad never appeared to XInput")

	require.NoError(t, pad.ButtonDown(gamepadmap.ButtonSouth))
	require.True(t, waitFor(t, 2*time.Second, func() bool {
		state, ok := xinputGetState(t, index)
		return ok && state.gamepad.buttons&xusbA != 0
	}), "A never registered as pressed")

	require.NoError(t, pad.ButtonUp(gamepadmap.ButtonSouth))
	require.True(t, waitFor(t, 2*time.Second, func() bool {
		state, ok := xinputGetState(t, index)
		return ok && state.gamepad.buttons&xusbA == 0
	}), "A never registered as released")

	// A trigger is analogue, so it proves the report's byte fields as well as
	// its button bits.
	require.NoError(t, pad.ButtonDown(gamepadmap.ButtonTriggerLeft))
	require.True(t, waitFor(t, 2*time.Second, func() bool {
		state, ok := xinputGetState(t, index)
		return ok && state.gamepad.leftTrigger == triggerFull
	}), "the left trigger never reached its limit")
	require.NoError(t, pad.ButtonUp(gamepadmap.ButtonTriggerLeft))

	// Two buttons at once must not clear each other.
	require.NoError(t, pad.ButtonDown(gamepadmap.ButtonStart))
	require.NoError(t, pad.ButtonDown(gamepadmap.ButtonDpadUp))
	require.True(t, waitFor(t, 2*time.Second, func() bool {
		state, ok := xinputGetState(t, index)
		return ok && state.gamepad.buttons&(xusbStart|xusbDpadUp) == xusbStart|xusbDpadUp
	}), "start and dpad-up were not held together")
	require.NoError(t, pad.ButtonUp(gamepadmap.ButtonStart))
	require.NoError(t, pad.ButtonUp(gamepadmap.ButtonDpadUp))
}
