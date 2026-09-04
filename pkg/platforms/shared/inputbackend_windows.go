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

package shared

import (
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/windowsinput"
)

func newPlatformKeyboard() (KeyboardDevice, error) {
	kbd, err := windowsinput.NewKeyboard()
	if err != nil {
		return nil, fmt.Errorf("create windows keyboard: %w", err)
	}
	return kbd, nil
}

// newPlatformGamepad plugs a virtual Xbox 360 pad into the ViGEmBus driver.
// The driver is an optional install, so a machine without it reports that
// rather than failing obscurely.
func newPlatformGamepad() (GamepadDevice, error) {
	gpd, err := windowsinput.NewGamepad()
	if err != nil {
		return nil, fmt.Errorf("create windows gamepad: %w", err)
	}
	return gpd, nil
}
