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
	"fmt"
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput/keyboardmap"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// LinuxInput manages virtual keyboard and gamepad devices for Linux platforms.
// Embed this struct in platform implementations that need input device support.
type LinuxInput struct {
	NewKeyboard   func(time.Duration) (linuxinput.Keyboard, error)
	NewGamepad    func(time.Duration) (linuxinput.Gamepad, error)
	inputSessions map[*linuxInputSession]*heldInputState
	keyboardRefs  map[int]int
	gamepadRefs   map[int]int
	kbd           linuxinput.Keyboard
	gpd           linuxinput.Gamepad
	sequenceMu    syncutil.Mutex
	inputMu       syncutil.Mutex
}

// InitDevices initializes keyboard and optionally gamepad based on config.
// gamepadEnabledByDefault controls the default when config doesn't specify.
func (l *LinuxInput) InitDevices(cfg *config.Instance, gamepadEnabledByDefault bool) error {
	// Use real implementations if factories not set (production)
	newKbd := l.NewKeyboard
	if newKbd == nil {
		newKbd = linuxinput.NewKeyboard
	}
	newGpd := l.NewGamepad
	if newGpd == nil {
		newGpd = linuxinput.NewGamepad
	}

	kbd, err := newKbd(linuxinput.DefaultTimeout)
	if err != nil {
		return fmt.Errorf("failed to create keyboard: %w", err)
	}
	l.kbd = kbd

	if cfg.VirtualGamepadEnabled(gamepadEnabledByDefault) {
		gpd, err := newGpd(linuxinput.DefaultTimeout)
		if err != nil {
			return fmt.Errorf("failed to create gamepad: %w", err)
		}
		l.gpd = gpd
	}

	log.Debug().Msg("input devices initialized successfully")

	return nil
}

// CloseDevices releases all held input before closing keyboard and gamepad devices.
func (l *LinuxInput) CloseDevices() {
	l.closeInputDevices()
}

// KeyboardPress sends a keyboard key press.
func (l *LinuxInput) KeyboardPress(arg string) error {
	return l.pressKeyboardToken(arg)
}

// DefaultInterKeyDelay is the default pause between consecutive key presses in a
// sequence. Matches the inter-key delay used by the per-key fallback loop.
const DefaultInterKeyDelay = 100 * time.Millisecond

// resolveHoldKeyCode converts a key name (as it appears inside a sigil or hold
// token, without braces) to a uinput keycode. Shifted single chars (e.g. "M",
// "*") resolve to their base code. Multi-char names get braces added before
// looking them up (e.g. "shift" → "{shift}").
func resolveHoldKeyCode(name string) (int, error) {
	if baseCode, ok := keyboardmap.IsShiftedKey(name); ok {
		return baseCode, nil
	}
	// Choose the form ParseKeyCombo expects.
	arg := name
	if len([]rune(name)) > 1 {
		arg = "{" + name + "}"
	}
	codes, isCombo, err := linuxinput.ParseKeyCombo(arg)
	if err != nil {
		return 0, fmt.Errorf("unknown key %q: %w", name, err)
	}
	if isCombo {
		return 0, fmt.Errorf("hold/press/release does not support combos: %q", name)
	}
	return codes[0], nil
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
func (l *LinuxInput) KeyboardPressSequence(args []string, interKeyDelay time.Duration) error {
	return l.pressKeyboardSequence(args, interKeyDelay)
}

// GamepadPress sends a gamepad button press.
func (l *LinuxInput) GamepadPress(name string) error {
	return l.pressGamepadToken(name)
}

// GamepadPressSequence sends a request-scoped gamepad sequence. Explicit press
// tokens are always released before this method returns.
func (l *LinuxInput) GamepadPressSequence(args []string, interKeyDelay time.Duration) error {
	return l.pressGamepadSequence(args, interKeyDelay)
}
