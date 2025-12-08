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

package mistex

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGamepadPress_DisabledReturnsError tests that GamepadPress returns an error
// when the virtual gamepad is disabled (gpd.Device is nil).
func TestGamepadPress_DisabledReturnsError(t *testing.T) {
	t.Parallel()

	// Create platform with zero-value gamepad (Device will be nil)
	platform := &Platform{}

	// Attempt to press a button
	err := platform.GamepadPress("a")

	// Should return error indicating gamepad is disabled
	require.Error(t, err)
	assert.Contains(t, err.Error(), "virtual gamepad is disabled")
}

// TestGamepadPress_ValidButtonsWhenDisabled tests various button names return the same disabled error
func TestGamepadPress_ValidButtonsWhenDisabled(t *testing.T) {
	t.Parallel()

	platform := &Platform{}

	buttons := []string{"a", "b", "x", "y", "start", "select", "up", "down", "left", "right"}
	for _, button := range buttons {
		t.Run(button, func(t *testing.T) {
			t.Parallel()
			err := platform.GamepadPress(button)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "virtual gamepad is disabled")
		})
	}
}
