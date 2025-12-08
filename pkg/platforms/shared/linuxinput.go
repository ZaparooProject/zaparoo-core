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
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput"
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
