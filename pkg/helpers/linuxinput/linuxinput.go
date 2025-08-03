// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-only
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

package linuxinput

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/helpers/linuxinput/keyboardmap"
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
		return Keyboard{}, err
	}
	return Keyboard{
		Device: kbd,
		Delay:  delay,
	}, nil
}

func (k *Keyboard) Close() error {
	return k.Device.Close()
}

func (k *Keyboard) Press(key int) error {
	if key < 0 {
		return k.Combo(42, -key)
	}

	err := k.Device.KeyDown(key)
	if err != nil {
		return err
	}

	time.Sleep(k.Delay)

	return k.Device.KeyUp(key)
}

func (k *Keyboard) Combo(keys ...int) error {
	for _, key := range keys {
		err := k.Device.KeyDown(key)
		if err != nil {
			return err
		}
	}
	time.Sleep(k.Delay)
	for _, key := range keys {
		err := k.Device.KeyUp(key)
		if err != nil {
			return err
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
		return Gamepad{}, err
	}
	return Gamepad{
		Device: gpd,
		Delay:  delay,
	}, nil
}

func (k *Gamepad) Close() error {
	return k.Device.Close()
}

func (k *Gamepad) Press(key int) error {
	err := k.Device.ButtonDown(key)
	if err != nil {
		return err
	}
	time.Sleep(k.Delay)
	return k.Device.ButtonUp(key)
}

func (k *Gamepad) Combo(keys ...int) error {
	for _, key := range keys {
		err := k.Device.ButtonDown(key)
		if err != nil {
			return err
		}
	}
	time.Sleep(k.Delay)
	for _, key := range keys {
		err := k.Device.ButtonUp(key)
		if err != nil {
			return err
		}
	}
	return nil
}
