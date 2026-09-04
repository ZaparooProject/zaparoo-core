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
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput/gamepadmap"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput/keyboardmap"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// ErrInputUnsupported is returned by InitDevices on platforms with no virtual
// input backend.
var ErrInputUnsupported = errors.New("virtual input is not supported on this platform")

// KeyboardDevice is a virtual keyboard backend. Codes are raw evdev keycodes
// from keyboardmap; backends translate them to whatever their own API expects.
type KeyboardDevice interface {
	KeyDown(code int) error
	KeyUp(code int) error
	Close() error
}

// GamepadDevice is a virtual gamepad backend. Codes are raw evdev button codes
// from gamepadmap.
type GamepadDevice interface {
	ButtonDown(code int) error
	ButtonUp(code int) error
	Close() error
}

// DefaultInputDelay is the pause held between the down and up of a single
// press, to avoid overloading the OS or user applications.
const DefaultInputDelay = 40 * time.Millisecond

// DefaultInterKeyDelay is the default pause between consecutive key presses in a
// sequence. Matches the inter-key delay used by the per-key fallback loop.
const DefaultInterKeyDelay = 100 * time.Millisecond

// InputManager owns the virtual keyboard and gamepad devices and the input
// held on them. Embed it in platform implementations that support input.
//
// The backend constructors default to the platform's native implementation and
// exist so tests can inject recording devices.
type InputManager struct {
	NewKeyboard   func() (KeyboardDevice, error)
	NewGamepad    func() (GamepadDevice, error)
	inputSessions map[*inputSession]*heldInputState
	keyboardRefs  map[int]int
	gamepadRefs   map[int]int
	kbd           KeyboardDevice
	gpd           GamepadDevice
	keyboardDelay time.Duration
	gamepadDelay  time.Duration
	sequenceMu    syncutil.Mutex
	inputMu       syncutil.Mutex
}

// InitDevices initializes keyboard and optionally gamepad based on config.
// gamepadEnabledByDefault controls the default when config doesn't specify.
func (l *InputManager) InitDevices(cfg *config.Instance, gamepadEnabledByDefault bool) error {
	newKbd := l.NewKeyboard
	if newKbd == nil {
		newKbd = newPlatformKeyboard
	}
	newGpd := l.NewGamepad
	if newGpd == nil {
		newGpd = newPlatformGamepad
	}

	kbd, err := newKbd()
	if err != nil {
		return fmt.Errorf("failed to create keyboard: %w", err)
	}
	l.kbd = kbd
	l.keyboardDelay = DefaultInputDelay

	if cfg.VirtualGamepadEnabled(gamepadEnabledByDefault) {
		gpd, err := newGpd()
		if err != nil {
			return fmt.Errorf("failed to create gamepad: %w", err)
		}
		l.gpd = gpd
		l.gamepadDelay = DefaultInputDelay
	}

	log.Debug().Msg("input devices initialized successfully")

	return nil
}

// CloseDevices releases all held input before closing keyboard and gamepad devices.
func (l *InputManager) CloseDevices() {
	l.closeInputDevices()
}

// KeyboardPress sends a keyboard key press.
func (l *InputManager) KeyboardPress(arg string) error {
	return l.pressKeyboardToken(arg)
}

// resolveHoldKeyCode converts a key name (as it appears inside a sigil or hold
// token, without braces) to a keycode. Shifted single chars (e.g. "M", "*")
// resolve to their base code. Multi-char names get braces added before looking
// them up (e.g. "shift" → "{shift}").
func resolveHoldKeyCode(name string) (int, error) {
	if baseCode, ok := keyboardmap.IsShiftedKey(name); ok {
		return baseCode, nil
	}
	// Choose the form ParseKeyCombo expects.
	arg := name
	if len([]rune(name)) > 1 {
		arg = "{" + name + "}"
	}
	codes, isCombo, err := keyboardmap.ParseKeyCombo(arg)
	if err != nil {
		return 0, fmt.Errorf("unknown key %q: %w", name, err)
	}
	if isCombo {
		return 0, fmt.Errorf("hold/press/release does not support combos: %q", name)
	}
	return codes[0], nil
}

// resolveGamepadHoldCode converts a button name (as it appears inside a sigil
// or hold token, without braces) to a button code.
func resolveGamepadHoldCode(name string) (int, error) {
	arg := name
	if len([]rune(name)) > 1 {
		arg = "{" + name + "}"
	}
	code, ok := gamepadmap.ToGamepadCode(arg)
	if !ok {
		return 0, fmt.Errorf("unknown button: %s", name)
	}
	return code, nil
}

// parseMacroDuration is the local copy of the duration parser used by the core
// zapscript package. It accepts plain integers (milliseconds) and Go durations
// ("1s", "500ms", "1m30s").
func parseMacroDuration(s string) (time.Duration, error) {
	if ms, err := strconv.Atoi(s); err == nil {
		return time.Duration(ms) * time.Millisecond, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	return d, nil
}

// KeyboardPressSequence sends a request-scoped key sequence. Explicit press
// tokens are always released before this method returns.
func (l *InputManager) KeyboardPressSequence(args []string, interKeyDelay time.Duration) error {
	return l.pressKeyboardSequence(args, interKeyDelay)
}

// GamepadPress sends a gamepad button press.
func (l *InputManager) GamepadPress(name string) error {
	return l.pressGamepadToken(name)
}

// GamepadPressSequence sends a request-scoped gamepad sequence. Explicit press
// tokens are always released before this method returns.
func (l *InputManager) GamepadPressSequence(args []string, interKeyDelay time.Duration) error {
	return l.pressGamepadSequence(args, interKeyDelay)
}
