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

package windowsinput

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInjectionTimes(t *testing.T) {
	// Not parallel: the timestamps are package state.
	lastKeyboardInjection.Store(0)
	lastGamepadInjection.Store(0)
	assert.True(t, LastKeyboardInjection().IsZero())
	assert.True(t, LastGamepadInjection().IsZero())

	before := time.Now()
	markKeyboardInjection()
	assert.False(t, LastKeyboardInjection().Before(before))
	assert.True(t, LastGamepadInjection().IsZero(), "pad and keyboard are tracked separately")

	markGamepadInjection()
	assert.False(t, LastGamepadInjection().Before(before))
}
