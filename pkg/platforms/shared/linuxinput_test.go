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

package shared

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput"
	"github.com/bendahl/uinput"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGamepadPress_DisabledReturnsError tests that GamepadPress returns an error
// when the virtual gamepad is disabled (gpd.Device is nil).
func TestGamepadPress_DisabledReturnsError(t *testing.T) {
	t.Parallel()

	// Create LinuxInput with zero-value gamepad (Device will be nil)
	input := &LinuxInput{}

	// Attempt to press a button
	err := input.GamepadPress("a")

	// Should return error indicating gamepad is disabled
	require.Error(t, err)
	assert.Contains(t, err.Error(), "virtual gamepad is disabled")
}

// TestGamepadPress_AllButtonsWhenDisabled tests various button names all return the disabled error
func TestGamepadPress_AllButtonsWhenDisabled(t *testing.T) {
	t.Parallel()

	input := &LinuxInput{}

	buttons := []string{"a", "b", "x", "y", "start", "select", "up", "down", "left", "right"}
	for _, button := range buttons {
		t.Run(button, func(t *testing.T) {
			t.Parallel()
			err := input.GamepadPress(button)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "virtual gamepad is disabled")
		})
	}
}

// TestInitDevices_WithMockFactories tests InitDevices with mock keyboard/gamepad factories
func TestInitDevices_WithMockFactories(t *testing.T) {
	t.Parallel()

	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)

	// Track factory calls
	keyboardCreated := false
	gamepadCreated := false

	input := &LinuxInput{
		NewKeyboard: func(_ time.Duration) (linuxinput.Keyboard, error) {
			keyboardCreated = true
			return linuxinput.Keyboard{}, nil
		},
		NewGamepad: func(_ time.Duration) (linuxinput.Gamepad, error) {
			gamepadCreated = true
			return linuxinput.Gamepad{}, nil
		},
	}

	// Init with gamepad enabled by default
	err = input.InitDevices(cfg, true)
	require.NoError(t, err)

	assert.True(t, keyboardCreated, "keyboard factory should be called")
	assert.True(t, gamepadCreated, "gamepad factory should be called when enabled by default")
}

// TestInitDevices_GamepadDisabledByDefault tests that gamepad is not created when disabled by default
func TestInitDevices_GamepadDisabledByDefault(t *testing.T) {
	t.Parallel()

	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)

	gamepadCreated := false

	input := &LinuxInput{
		NewKeyboard: func(_ time.Duration) (linuxinput.Keyboard, error) {
			return linuxinput.Keyboard{}, nil
		},
		NewGamepad: func(_ time.Duration) (linuxinput.Gamepad, error) {
			gamepadCreated = true
			return linuxinput.Gamepad{}, nil
		},
	}

	// Init with gamepad disabled by default
	err = input.InitDevices(cfg, false)
	require.NoError(t, err)

	assert.False(t, gamepadCreated, "gamepad factory should not be called when disabled by default")
}

// TestInitDevices_KeyboardError tests error handling when keyboard creation fails
func TestInitDevices_KeyboardError(t *testing.T) {
	t.Parallel()

	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)

	expectedErr := errors.New("keyboard creation failed")

	input := &LinuxInput{
		NewKeyboard: func(_ time.Duration) (linuxinput.Keyboard, error) {
			return linuxinput.Keyboard{}, expectedErr
		},
	}

	err = input.InitDevices(cfg, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create keyboard")
}

// TestInitDevices_GamepadError tests error handling when gamepad creation fails
func TestInitDevices_GamepadError(t *testing.T) {
	t.Parallel()

	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)

	expectedErr := errors.New("gamepad creation failed")

	input := &LinuxInput{
		NewKeyboard: func(_ time.Duration) (linuxinput.Keyboard, error) {
			return linuxinput.Keyboard{}, nil
		},
		NewGamepad: func(_ time.Duration) (linuxinput.Gamepad, error) {
			return linuxinput.Gamepad{}, expectedErr
		},
	}

	err = input.InitDevices(cfg, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create gamepad")
}

// TestCloseDevices_NilGamepad tests that CloseDevices handles nil gamepad gracefully
func TestCloseDevices_NilGamepad(t *testing.T) {
	t.Parallel()

	// Create LinuxInput with zero-value devices
	input := &LinuxInput{}

	// Should not panic - gracefully handles nil devices
	// Note: This will log warnings but not panic
	assert.NotPanics(t, func() {
		input.CloseDevices()
	})
}

// mockGamepad implements uinput.Gamepad for testing
type mockGamepad struct {
	buttonDownErr error
}

func (*mockGamepad) ButtonPress(_ int) error                { return nil }
func (m *mockGamepad) ButtonDown(_ int) error               { return m.buttonDownErr }
func (*mockGamepad) ButtonUp(_ int) error                   { return nil }
func (*mockGamepad) LeftStickMoveX(_ float32) error         { return nil }
func (*mockGamepad) LeftStickMoveY(_ float32) error         { return nil }
func (*mockGamepad) RightStickMoveX(_ float32) error        { return nil }
func (*mockGamepad) RightStickMoveY(_ float32) error        { return nil }
func (*mockGamepad) LeftStickMove(_, _ float32) error       { return nil }
func (*mockGamepad) RightStickMove(_, _ float32) error      { return nil }
func (*mockGamepad) HatPress(_ uinput.HatDirection) error   { return nil }
func (*mockGamepad) HatRelease(_ uinput.HatDirection) error { return nil }
func (*mockGamepad) Close() error                           { return nil }

// TestGamepadPress_UnknownButton tests that GamepadPress returns error for unknown button names
func TestGamepadPress_UnknownButton(t *testing.T) {
	t.Parallel()

	// Create LinuxInput with a mock gamepad that has a non-nil Device
	input := &LinuxInput{
		gpd: linuxinput.Gamepad{
			Device: &mockGamepad{},
		},
	}

	// Test with an invalid button name - should get unknown button error
	err := input.GamepadPress("invalidbutton123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown button")
}

// TestKeyboardPress_UnknownKey tests that KeyboardPress returns error for unknown key names
func TestKeyboardPress_UnknownKey(t *testing.T) {
	t.Parallel()

	input := &LinuxInput{}

	// Test with an invalid key name
	err := input.KeyboardPress("invalidkey123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse key combo")
	assert.Contains(t, err.Error(), "unknown keyboard key")
}

// TestKeyboardPress_UnknownKeyInCombo tests that KeyboardPress returns error for unknown key in combo
func TestKeyboardPress_UnknownKeyInCombo(t *testing.T) {
	t.Parallel()

	input := &LinuxInput{}

	// Test with an invalid key in a combo
	err := input.KeyboardPress("{ctrl+invalidkey}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse key combo")
	assert.Contains(t, err.Error(), "unknown keyboard key")
}

// TestGamepadPress_Success tests successful gamepad button press
func TestGamepadPress_Success(t *testing.T) {
	t.Parallel()

	input := &LinuxInput{
		gpd: linuxinput.Gamepad{
			Device: &mockGamepad{},
		},
	}

	// Test with a valid button name
	err := input.GamepadPress("a")
	require.NoError(t, err)
}

// TestGamepadPress_DeviceError tests gamepad press when device returns error
func TestGamepadPress_DeviceError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("device error")
	input := &LinuxInput{
		gpd: linuxinput.Gamepad{
			Device: &mockGamepad{buttonDownErr: expectedErr},
		},
	}

	err := input.GamepadPress("a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to press gamepad button")
}

// mockKeyboard implements uinput.Keyboard for testing
type mockKeyboard struct {
	keyDownErr error
}

func (*mockKeyboard) KeyPress(_ int) error          { return nil }
func (m *mockKeyboard) KeyDown(_ int) error         { return m.keyDownErr }
func (*mockKeyboard) KeyUp(_ int) error             { return nil }
func (*mockKeyboard) FetchSyspath() (string, error) { return "", nil }
func (*mockKeyboard) Close() error                  { return nil }

// keyEvent records a single key event for sequence assertion in tests.
type keyEvent struct {
	kind string // "down" or "up"
	code int
}

// recordingKeyboard records all key events in order for assertion.
// If failOnCode is set, KeyDown returns an error when that code is pressed.
type recordingKeyboard struct {
	events     []keyEvent
	failOnCode int  // if non-zero, KeyDown returns error for this code
	failOnce   bool // trigger failOnCode only once
	failed     bool
}

func (r *recordingKeyboard) KeyPress(code int) error {
	r.events = append(r.events, keyEvent{"down", code}, keyEvent{"up", code})
	return nil
}

func (r *recordingKeyboard) KeyDown(code int) error {
	if r.failOnCode != 0 && code == r.failOnCode && (!r.failOnce || !r.failed) {
		r.failed = true
		return fmt.Errorf("injected KeyDown error for code %d", code)
	}
	r.events = append(r.events, keyEvent{"down", code})
	return nil
}

func (r *recordingKeyboard) KeyUp(code int) error {
	r.events = append(r.events, keyEvent{"up", code})
	return nil
}

func (*recordingKeyboard) FetchSyspath() (string, error) { return "", nil }
func (*recordingKeyboard) Close() error                  { return nil }

// TestKeyboardPress_SingleKey tests successful single key press
func TestKeyboardPress_SingleKey(t *testing.T) {
	t.Parallel()

	input := &LinuxInput{
		kbd: linuxinput.Keyboard{
			Device: &mockKeyboard{},
		},
	}

	err := input.KeyboardPress("a")
	require.NoError(t, err)
}

// TestKeyboardPress_SingleKeyError tests single key press device error
func TestKeyboardPress_SingleKeyError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("key down error")
	input := &LinuxInput{
		kbd: linuxinput.Keyboard{
			Device: &mockKeyboard{keyDownErr: expectedErr},
		},
	}

	err := input.KeyboardPress("a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to press keyboard key")
}

// TestKeyboardPress_Combo tests successful key combo press
func TestKeyboardPress_Combo(t *testing.T) {
	t.Parallel()

	input := &LinuxInput{
		kbd: linuxinput.Keyboard{
			Device: &mockKeyboard{},
		},
	}

	err := input.KeyboardPress("{ctrl+a}")
	require.NoError(t, err)
}

// TestKeyboardPress_ComboError tests key combo press device error
func TestKeyboardPress_ComboError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("key down error")
	input := &LinuxInput{
		kbd: linuxinput.Keyboard{
			Device: &mockKeyboard{keyDownErr: expectedErr},
		},
	}

	err := input.KeyboardPress("{ctrl+a}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to press keyboard combo")
}

// newRecordingLinuxInput returns a LinuxInput with a recording keyboard and zero delays.
func newRecordingLinuxInput(rec *recordingKeyboard) *LinuxInput {
	return &LinuxInput{
		kbd: linuxinput.Keyboard{
			Device: rec,
			Delay:  0,
		},
	}
}

// TestKeyboardPressSequence_ShiftBatching_AsteriskMENU is the core regression test
// for issue #939: *MENU should hold Shift once for the whole run, not toggle it per char.
func TestKeyboardPressSequence_ShiftBatching_AsteriskMENU(t *testing.T) {
	t.Parallel()

	rec := &recordingKeyboard{}
	input := newRecordingLinuxInput(rec)

	// *MENU: *, M, E, N, U are all shifted chars. Space before is intentional.
	args := []string{" ", "*", "M", "E", "N", "U"}
	require.NoError(t, input.KeyboardPressSequence(args, 0))

	events := rec.events

	// Space (key 57) should be emitted normally with no shift involvement.
	assert.Equal(t, keyEvent{"down", 57}, events[0], "space KeyDown")
	assert.Equal(t, keyEvent{"up", 57}, events[1], "space KeyUp")

	// Find where Shift goes down and up.
	const shiftCode = 42
	var shiftDownIdx, shiftUpIdx int
	shiftDownCount, shiftUpCount := 0, 0
	for i, ev := range events {
		if ev.code == shiftCode {
			if ev.kind == "down" {
				shiftDownIdx = i
				shiftDownCount++
			} else {
				shiftUpIdx = i
				shiftUpCount++
			}
		}
	}

	assert.Equal(t, 1, shiftDownCount, "Shift should be pressed exactly once for the whole run")
	assert.Equal(t, 1, shiftUpCount, "Shift should be released exactly once")
	assert.Less(t, shiftDownIdx, shiftUpIdx, "Shift down must come before Shift up")

	// All five base keys (*=9, M=50, E=18, N=49, U=22) must appear between
	// ShiftDown and ShiftUp.
	baseCodes := map[int]bool{9: true, 50: true, 18: true, 49: true, 22: true}
	for _, ev := range events[shiftDownIdx+1 : shiftUpIdx] {
		delete(baseCodes, ev.code)
	}
	assert.Empty(t, baseCodes, "all base key events should be between ShiftDown and ShiftUp")
}

// TestKeyboardPressSequence_MixedShiftRuns verifies that a mixed sequence (unshifted,
// shifted run, unshifted) batches the shifted portion and leaves others as-is.
func TestKeyboardPressSequence_MixedShiftRuns(t *testing.T) {
	t.Parallel()

	rec := &recordingKeyboard{}
	input := newRecordingLinuxInput(rec)

	// "aBC d" — a (unshifted), BC (shifted run), space (unshifted), d (unshifted)
	args := []string{"a", "B", "C", " ", "d"}
	require.NoError(t, input.KeyboardPressSequence(args, 0))

	events := rec.events

	const shiftCode = 42
	shiftDownCount := 0
	for _, ev := range events {
		if ev.code == shiftCode && ev.kind == "down" {
			shiftDownCount++
		}
	}
	assert.Equal(t, 1, shiftDownCount, "only one shifted run so Shift pressed exactly once")

	// a (code 30) must appear before shiftCode first down
	firstShiftIdx := -1
	for i, ev := range events {
		if ev.code == shiftCode && ev.kind == "down" {
			firstShiftIdx = i
			break
		}
	}
	require.NotEqual(t, -1, firstShiftIdx)

	// Find index of a's KeyDown
	aIdx := -1
	for i, ev := range events {
		if ev.code == 30 && ev.kind == "down" {
			aIdx = i
			break
		}
	}
	require.NotEqual(t, -1, aIdx)
	assert.Less(t, aIdx, firstShiftIdx, "unshifted 'a' must be emitted before the shifted run")
}

// TestKeyboardPressSequence_NonShifted verifies that no Shift events appear when
// typing all-lowercase text.
func TestKeyboardPressSequence_NonShifted(t *testing.T) {
	t.Parallel()

	rec := &recordingKeyboard{}
	input := newRecordingLinuxInput(rec)

	require.NoError(t, input.KeyboardPressSequence([]string{"h", "e", "l", "l", "o"}, 0))

	for _, ev := range rec.events {
		assert.NotEqual(t, 42, ev.code, "no Shift events expected for lowercase text")
	}
}

// TestKeyboardPressSequence_DisabledKeyboard returns an error when Device is nil.
func TestKeyboardPressSequence_DisabledKeyboard(t *testing.T) {
	t.Parallel()

	input := &LinuxInput{}
	err := input.KeyboardPressSequence([]string{"a"}, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "virtual keyboard is disabled")
}

// TestKeyboardPressSequence_ReleaseAllOnError verifies that Shift is released by the
// sequence-scoped defer even if an error aborts the run mid-way.
func TestKeyboardPressSequence_ReleaseAllOnError(t *testing.T) {
	t.Parallel()

	// Fail when the second shifted base key (M=50) goes down.
	rec := &recordingKeyboard{failOnCode: 50, failOnce: true}
	input := newRecordingLinuxInput(rec)

	err := input.KeyboardPressSequence([]string{"*", "M", "E"}, 0)
	require.Error(t, err)

	// Despite the error, Shift must have been released by the defer.
	const shiftCode = 42
	shiftDownCount, shiftUpCount := 0, 0
	for _, ev := range rec.events {
		if ev.code == shiftCode {
			if ev.kind == "down" {
				shiftDownCount++
			} else {
				shiftUpCount++
			}
		}
	}
	assert.Equal(t, 1, shiftDownCount, "Shift pressed once before the run")
	assert.Equal(t, 1, shiftUpCount, "Shift released by sequence-scoped defer even on error")
}

// TestKeyboardPressSequence_BracedSpecial verifies that a braced special key
// ({enter}) is emitted without any Shift involvement.
func TestKeyboardPressSequence_BracedSpecial(t *testing.T) {
	t.Parallel()

	rec := &recordingKeyboard{}
	input := newRecordingLinuxInput(rec)

	require.NoError(t, input.KeyboardPressSequence([]string{"{enter}"}, 0))

	for _, ev := range rec.events {
		assert.NotEqual(t, 42, ev.code, "no Shift events for {enter}")
	}
	// {enter} = code 28
	assert.Contains(t, rec.events, keyEvent{"down", 28})
	assert.Contains(t, rec.events, keyEvent{"up", 28})
}

// TestKeyboardPressSequence_EmptyArgs returns nil without emitting anything.
func TestKeyboardPressSequence_EmptyArgs(t *testing.T) {
	t.Parallel()

	rec := &recordingKeyboard{}
	input := newRecordingLinuxInput(rec)

	require.NoError(t, input.KeyboardPressSequence([]string{}, 0))
	assert.Empty(t, rec.events)
}

// TestKeyboardPressSequence_DelayToken verifies that {delay:N} sleeps without
// emitting any key events.
func TestKeyboardPressSequence_DelayToken(t *testing.T) {
	t.Parallel()

	rec := &recordingKeyboard{}
	input := newRecordingLinuxInput(rec)

	// "a", delay 0ms, "b" — zero delay so the test is instant.
	require.NoError(t, input.KeyboardPressSequence([]string{"a", "{delay:0}", "b"}, 0))

	// Only a (30) and b (48) events; no spurious key events from the delay token.
	codes := make([]int, 0, len(rec.events))
	for _, ev := range rec.events {
		codes = append(codes, ev.code)
	}
	assert.Equal(t, []int{30, 30, 48, 48}, codes, "only 'a' and 'b' events, each down then up")
}

// TestKeyboardPressSequence_PressReleaseSigils verifies {_a}/{^a} emit only
// down/up events respectively without the paired counterpart.
func TestKeyboardPressSequence_PressReleaseSigils(t *testing.T) {
	t.Parallel()

	rec := &recordingKeyboard{}
	input := newRecordingLinuxInput(rec)

	// Press 'a' down, type 'b', release 'a'.
	require.NoError(t, input.KeyboardPressSequence([]string{"{_a}", "b", "{^a}"}, 0))

	// Expected: a-down, b-down, b-up, a-up (sequence-scoped defer cleans up any
	// still-held keys, but {^a} should remove it from held before the defer).
	aDownIdx, aUpIdx, bDownIdx := -1, -1, -1
	for i, ev := range rec.events {
		switch {
		case ev.code == 30 && ev.kind == "down":
			aDownIdx = i
		case ev.code == 30 && ev.kind == "up":
			aUpIdx = i
		case ev.code == 48 && ev.kind == "down":
			bDownIdx = i
		}
	}
	require.NotEqual(t, -1, aDownIdx, "a-down must be present")
	require.NotEqual(t, -1, aUpIdx, "a-up must be present")
	require.NotEqual(t, -1, bDownIdx, "b-down must be present")
	assert.Less(t, aDownIdx, bDownIdx, "a pressed before b")
	assert.Less(t, bDownIdx, aUpIdx, "b typed while a is held")
}

// TestKeyboardPressSequence_HoldToken verifies {hold:a:0} emits a down+up pair.
func TestKeyboardPressSequence_HoldToken(t *testing.T) {
	t.Parallel()

	rec := &recordingKeyboard{}
	input := newRecordingLinuxInput(rec)

	// Hold 'a' for 0ms.
	require.NoError(t, input.KeyboardPressSequence([]string{"{hold:a:0}"}, 0))

	assert.Contains(t, rec.events, keyEvent{"down", 30}, "hold emits key-down")
	assert.Contains(t, rec.events, keyEvent{"up", 30}, "hold emits key-up")
}

// TestKeyboardPressSequence_HoldSigil verifies {~a:0} (sigil form) matches {hold:a:0}.
func TestKeyboardPressSequence_HoldSigil(t *testing.T) {
	t.Parallel()

	rec := &recordingKeyboard{}
	input := newRecordingLinuxInput(rec)

	require.NoError(t, input.KeyboardPressSequence([]string{"{~a:0}"}, 0))

	assert.Contains(t, rec.events, keyEvent{"down", 30})
	assert.Contains(t, rec.events, keyEvent{"up", 30})
}

// TestKeyboardPressSequence_DelayToken_GoDuration verifies that Go duration strings
// ("0ms") are accepted by the {delay:…} token.
func TestKeyboardPressSequence_DelayToken_GoDuration(t *testing.T) {
	t.Parallel()

	rec := &recordingKeyboard{}
	input := newRecordingLinuxInput(rec)

	require.NoError(t, input.KeyboardPressSequence([]string{"{delay:0ms}", "a"}, 0))
	assert.Contains(t, rec.events, keyEvent{"down", 30})
}

// TestKeyboardPressSequence_InterKeyDelay verifies the custom interKeyDelay param
// is accepted (non-zero; just smoke-test the zero-default path).
func TestKeyboardPressSequence_InterKeyDelay(t *testing.T) {
	t.Parallel()

	rec := &recordingKeyboard{}
	input := newRecordingLinuxInput(rec)

	// 1ms inter-key delay — fast enough for tests; proves the param is wired.
	require.NoError(t, input.KeyboardPressSequence([]string{"a", "b"}, 1*time.Millisecond))
	assert.Contains(t, rec.events, keyEvent{"down", 30})
	assert.Contains(t, rec.events, keyEvent{"down", 48})
}
