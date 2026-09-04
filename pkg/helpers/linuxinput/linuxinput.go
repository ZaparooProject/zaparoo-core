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

// Package linuxinput creates the uinput virtual devices the input engine
// drives on Linux. The devices it returns satisfy the engine's keyboard and
// gamepad backend interfaces directly; key and button name resolution lives
// in the platform-neutral keyboardmap and gamepadmap packages.
package linuxinput

import (
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput/keyboardmap"
	"github.com/bendahl/uinput"
)

const (
	DeviceName = "Zaparoo"
	uinputDev  = "/dev/uinput"
)

// NewKeyboard returns a uinput virtual keyboard device. It must be closed when
// the service stops.
func NewKeyboard() (uinput.Keyboard, error) {
	keyboardmap.SetupLegacyKeyboardMap()
	kbd, err := uinput.CreateKeyboard(uinputDev, []byte(DeviceName))
	if err != nil {
		return nil, fmt.Errorf("failed to create keyboard device: %w", err)
	}
	return kbd, nil
}

// NewGamepad returns a uinput virtual gamepad device. It must be closed when
// the service stops.
func NewGamepad() (uinput.Gamepad, error) {
	gpd, err := uinput.CreateGamepad(
		uinputDev,
		[]byte(DeviceName),
		0x1234,
		0x5678,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gamepad device: %w", err)
	}
	return gpd, nil
}
