//go:build linux

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

package shared

import (
	"errors"
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
