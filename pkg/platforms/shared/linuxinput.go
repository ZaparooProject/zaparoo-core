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
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput/keyboardmap"
	"github.com/rs/zerolog/log"
)

// LinuxInput manages virtual keyboard and gamepad devices for Linux platforms.
// Embed this struct in platform implementations that need input device support.
type LinuxInput struct {
	NewKeyboard func(time.Duration) (linuxinput.Keyboard, error)
	NewGamepad  func(time.Duration) (linuxinput.Gamepad, error)
	kbd         linuxinput.Keyboard
	gpd         linuxinput.Gamepad
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

// CloseDevices closes keyboard and gamepad devices.
func (l *LinuxInput) CloseDevices() {
	if l.kbd.Device != nil {
		if err := l.kbd.Close(); err != nil {
			log.Warn().Err(err).Msg("error closing keyboard")
		}
	}
	if l.gpd.Device != nil {
		if err := l.gpd.Close(); err != nil {
			log.Warn().Err(err).Msg("error closing gamepad")
		}
	}
}

// KeyboardPress sends a keyboard key press.
func (l *LinuxInput) KeyboardPress(arg string) error {
	codes, isCombo, err := linuxinput.ParseKeyCombo(arg)
	if err != nil {
		return fmt.Errorf("failed to parse key combo: %w", err)
	}
	if isCombo {
		if err := l.kbd.Combo(codes...); err != nil {
			return fmt.Errorf("failed to press keyboard combo: %w", err)
		}
		return nil
	}
	if err := l.kbd.Press(codes[0]); err != nil {
		return fmt.Errorf("failed to press keyboard key: %w", err)
	}
	return nil
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

// KeyboardPressSequence sends a key sequence with shift-batching and inline
// macro token support. interKeyDelay is the gap between consecutive keys; if
// zero, DefaultInterKeyDelay is used.
//
// Shift-batching: maximal runs of shift-modified single characters are grouped
// under one held LeftShift, matching how a person types *MENU.
//
// Inline tokens (passed through from the parser):
//
//	{delay:N}         — sleep N (integer ms or Go duration)
//	{press:k}/{_k}    — key down, no release
//	{release:k}/{^k}  — key up
//	{hold:k:dur}/{~k:dur} — key down, sleep dur, key up
//
// A sequence-scoped defer ensures any key left held is released on return.
func (l *LinuxInput) KeyboardPressSequence(args []string, interKeyDelay time.Duration) error {
	if l.kbd.Device == nil {
		return errors.New("virtual keyboard is disabled")
	}
	if interKeyDelay == 0 {
		interKeyDelay = DefaultInterKeyDelay
	}

	const shiftCode = 42 // KEY_LEFTSHIFT

	// held tracks keys currently pressed for sequence-scoped release-all.
	held := make(map[int]bool)

	keyDown := func(code int) error {
		if err := l.kbd.Device.KeyDown(code); err != nil {
			return fmt.Errorf("key down %d: %w", code, err)
		}
		held[code] = true
		return nil
	}

	keyUp := func(code int) error {
		if err := l.kbd.Device.KeyUp(code); err != nil {
			return fmt.Errorf("key up %d: %w", code, err)
		}
		delete(held, code)
		return nil
	}

	// Release any key left held on error or normal completion.
	defer func() {
		for code := range held {
			_ = l.kbd.Device.KeyUp(code)
		}
	}()

	i := 0
	for i < len(args) {
		token := args[i]

		// Dispatch inline control tokens: {delay:…}, {press:…}, {release:…},
		// {hold:…:…}, {_…}, {^…}, {~…:…}. These must be checked before the
		// shifted-run detection because their content starts with '{'.
		if len(token) > 2 && token[0] == '{' && token[len(token)-1] == '}' {
			inner := token[1 : len(token)-1]
			switch {
			case strings.HasPrefix(inner, "delay:"):
				d, err := parseMacroDuration(inner[len("delay:"):])
				if err != nil {
					return fmt.Errorf("invalid delay token %q: %w", token, err)
				}
				time.Sleep(d)
				i++
				continue

			case strings.HasPrefix(inner, "press:") || (len(inner) > 1 && inner[0] == '_'):
				var name string
				if inner[0] == '_' {
					name = inner[1:]
				} else {
					name = inner[len("press:"):]
				}
				code, err := resolveHoldKeyCode(name)
				if err != nil {
					return fmt.Errorf("token %q: %w", token, err)
				}
				if err := keyDown(code); err != nil {
					return fmt.Errorf("token %q: %w", token, err)
				}
				i++
				continue

			case strings.HasPrefix(inner, "release:") || (len(inner) > 1 && inner[0] == '^'):
				var name string
				if inner[0] == '^' {
					name = inner[1:]
				} else {
					name = inner[len("release:"):]
				}
				code, err := resolveHoldKeyCode(name)
				if err != nil {
					return fmt.Errorf("token %q: %w", token, err)
				}
				if err := keyUp(code); err != nil {
					return fmt.Errorf("token %q: %w", token, err)
				}
				i++
				continue

			case strings.HasPrefix(inner, "hold:") || (len(inner) > 1 && inner[0] == '~'):
				var rest string
				if inner[0] == '~' {
					rest = inner[1:]
				} else {
					rest = inner[len("hold:"):]
				}
				// Split "key:dur" or "key" at the LAST ':'.
				var keyName, durStr string
				if idx := strings.LastIndex(rest, ":"); idx != -1 {
					keyName = rest[:idx]
					durStr = rest[idx+1:]
				} else {
					keyName = rest
				}
				code, err := resolveHoldKeyCode(keyName)
				if err != nil {
					return fmt.Errorf("token %q: %w", token, err)
				}
				holdDur := l.kbd.Delay
				if durStr != "" {
					holdDur, err = parseMacroDuration(durStr)
					if err != nil {
						return fmt.Errorf("invalid hold duration in %q: %w", token, err)
					}
				}
				if err := keyDown(code); err != nil {
					return fmt.Errorf("token %q key down: %w", token, err)
				}
				time.Sleep(holdDur)
				if err := keyUp(code); err != nil {
					return fmt.Errorf("token %q key up: %w", token, err)
				}
				i++
				continue
			}
			// Fall through for standard braced keys like {enter}, {ctrl+c}.
		}

		// Check if this starts a shifted run (token maps to a negative keymap code).
		if baseCode, ok := keyboardmap.IsShiftedKey(token); ok {
			// Extend to the maximal contiguous run of shifted single chars.
			end := i + 1
			for end < len(args) {
				if _, ok2 := keyboardmap.IsShiftedKey(args[end]); !ok2 {
					break
				}
				end++
			}

			// Hold LeftShift across the whole run.
			if err := keyDown(shiftCode); err != nil {
				return fmt.Errorf("failed to press shift down: %w", err)
			}

			// Emit the first key (baseCode already resolved above).
			if err := keyDown(baseCode); err != nil {
				return fmt.Errorf("failed to press shifted key %q down: %w", token, err)
			}
			time.Sleep(l.kbd.Delay)
			if err := keyUp(baseCode); err != nil {
				return fmt.Errorf("failed to release shifted key %q: %w", token, err)
			}
			time.Sleep(interKeyDelay)

			// Emit remaining keys in the run.
			for j := i + 1; j < end; j++ {
				bc, _ := keyboardmap.IsShiftedKey(args[j])
				if err := keyDown(bc); err != nil {
					return fmt.Errorf("failed to press shifted key %q down: %w", args[j], err)
				}
				time.Sleep(l.kbd.Delay)
				if err := keyUp(bc); err != nil {
					return fmt.Errorf("failed to release shifted key %q: %w", args[j], err)
				}
				time.Sleep(interKeyDelay)
			}

			// Release Shift after the run.
			if err := keyUp(shiftCode); err != nil {
				return fmt.Errorf("failed to release shift: %w", err)
			}

			i = end
			continue
		}

		// Non-shifted, non-control token: parse and emit via combo/press.
		codes, isCombo, err := linuxinput.ParseKeyCombo(token)
		if err != nil {
			return fmt.Errorf("failed to parse key %q: %w", token, err)
		}

		if isCombo {
			if err := l.kbd.Combo(codes...); err != nil {
				return fmt.Errorf("failed to press combo %q: %w", token, err)
			}
		} else {
			if err := l.kbd.Press(codes[0]); err != nil {
				return fmt.Errorf("failed to press key %q: %w", token, err)
			}
		}
		time.Sleep(interKeyDelay)

		i++
	}

	return nil
}

// GamepadPress sends a gamepad button press.
func (l *LinuxInput) GamepadPress(name string) error {
	if l.gpd.Device == nil {
		return errors.New("virtual gamepad is disabled")
	}
	code, ok := linuxinput.ToGamepadCode(name)
	if !ok {
		return fmt.Errorf("unknown button: %s", name)
	}
	if err := l.gpd.Press(code); err != nil {
		return fmt.Errorf("failed to press gamepad button %s: %w", name, err)
	}
	return nil
}
