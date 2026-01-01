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

//go:build linux

package linuxinput

import (
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput/keyboardmap"
	"github.com/bendahl/uinput"
)

const (
	DeviceName     = "Zaparoo"
	DefaultTimeout = 40 * time.Millisecond
	uinputDev      = "/dev/uinput"
)

type Keyboard struct {
	Device uinput.Keyboard
	Delay  time.Duration
}

// NewKeyboard returns a uinput virtual keyboard device. It takes a delay
// duration which is used between presses to avoid overloading the OS or user
// applications. This device must be closed when the service stops.
func NewKeyboard(delay time.Duration) (Keyboard, error) {
	keyboardmap.SetupLegacyKeyboardMap()
	kbd, err := uinput.CreateKeyboard(uinputDev, []byte(DeviceName))
	if err != nil {
		return Keyboard{}, fmt.Errorf("failed to create keyboard device: %w", err)
	}
	return Keyboard{
		Device: kbd,
		Delay:  delay,
	}, nil
}

func (k *Keyboard) Close() error {
	if err := k.Device.Close(); err != nil {
		return fmt.Errorf("failed to close keyboard device: %w", err)
	}
	return nil
}

func (k *Keyboard) Press(key int) error {
	if key < 0 {
		return k.Combo(42, -key)
	}

	err := k.Device.KeyDown(key)
	if err != nil {
		return fmt.Errorf("failed to press key down: %w", err)
	}

	time.Sleep(k.Delay)

	if err := k.Device.KeyUp(key); err != nil {
		return fmt.Errorf("failed to release key: %w", err)
	}
	return nil
}

func (k *Keyboard) Combo(keys ...int) error {
	for _, key := range keys {
		err := k.Device.KeyDown(key)
		if err != nil {
			return fmt.Errorf("failed to press combo key down: %w", err)
		}
	}
	time.Sleep(k.Delay)
	for _, key := range keys {
		err := k.Device.KeyUp(key)
		if err != nil {
			return fmt.Errorf("failed to release combo key: %w", err)
		}
	}
	return nil
}

type Gamepad struct {
	Device uinput.Gamepad
	Delay  time.Duration
}

// NewGamepad returns a uinput virtual gamepad device. It takes a delay
// duration which is used between presses to avoid overloading the OS or user
// applications. This device must be closed when the service stops.
func NewGamepad(delay time.Duration) (Gamepad, error) {
	gpd, err := uinput.CreateGamepad(
		uinputDev,
		[]byte(DeviceName),
		0x1234,
		0x5678,
	)
	if err != nil {
		return Gamepad{}, fmt.Errorf("failed to create gamepad device: %w", err)
	}
	return Gamepad{
		Device: gpd,
		Delay:  delay,
	}, nil
}

func (k *Gamepad) Close() error {
	if err := k.Device.Close(); err != nil {
		return fmt.Errorf("failed to close gamepad device: %w", err)
	}
	return nil
}

func (k *Gamepad) Press(key int) error {
	err := k.Device.ButtonDown(key)
	if err != nil {
		return fmt.Errorf("failed to press gamepad button down: %w", err)
	}
	time.Sleep(k.Delay)
	if err := k.Device.ButtonUp(key); err != nil {
		return fmt.Errorf("failed to release gamepad button: %w", err)
	}
	return nil
}

func (k *Gamepad) Combo(keys ...int) error {
	for _, key := range keys {
		err := k.Device.ButtonDown(key)
		if err != nil {
			return fmt.Errorf("failed to press gamepad combo button down: %w", err)
		}
	}
	time.Sleep(k.Delay)
	for _, key := range keys {
		err := k.Device.ButtonUp(key)
		if err != nil {
			return fmt.Errorf("failed to release gamepad combo button: %w", err)
		}
	}
	return nil
}

// ParseKeyCombo parses a keyboard key argument string and returns the corresponding
// key codes. It supports both single keys (e.g., "a", "{f9}") and combo keys with
// the "+" delimiter (e.g., "{ctrl+q}", "{shift+a}").
//
// The function returns:
//   - codes: slice of keyboard codes ready to be used with Press() or Combo()
//   - isCombo: true if multiple keys detected (use Combo()), false for single key (use Press())
//   - error: if any key name is unknown
//
// Examples:
//   - ParseKeyCombo("a") -> [30], false, nil
//   - ParseKeyCombo("{f9}") -> [67], false, nil
//   - ParseKeyCombo("{ctrl+q}") -> [29, 16], true, nil
//   - ParseKeyCombo("{shift+a}") -> [42, 30], true, nil
func ParseKeyCombo(arg string) (codes []int, isCombo bool, err error) {
	var names []string

	// Parse combo syntax: {key1+key2+...} or single key
	if len(arg) > 1 && arg[0] == '{' && arg[len(arg)-1] == '}' {
		// Strip outer braces and split by +
		inner := arg[1 : len(arg)-1]
		parts := strings.Split(inner, "+")

		if len(parts) > 1 {
			// Combo detected - re-add braces to multi-char parts
			names = make([]string, len(parts))
			for i, part := range parts {
				if len(part) > 1 {
					names[i] = "{" + part + "}"
				} else {
					names[i] = part
				}
			}
			isCombo = true
		} else {
			// Single key with braces
			names = []string{arg}
			isCombo = false
		}
	} else {
		// Single key without braces
		names = []string{arg}
		isCombo = false
	}

	// Convert all names to codes
	codes = make([]int, 0, len(names))
	for _, name := range names {
		code, ok := ToKeyboardCode(name)
		if !ok {
			return nil, false, fmt.Errorf("unknown keyboard key: %s", name)
		}
		codes = append(codes, code)
	}

	return codes, isCombo, nil
}
