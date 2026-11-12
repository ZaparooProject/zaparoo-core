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
	"sync/atomic"
	"time"
)

// Core's own synthetic input is indistinguishable from a person's to the
// system-wide idle counters, so the times of Core's injections are recorded
// and idle detection can discount input that lines up with one.
var (
	lastKeyboardInjection atomic.Int64
	lastGamepadInjection  atomic.Int64
)

func markKeyboardInjection() {
	lastKeyboardInjection.Store(time.Now().UnixNano())
}

func markGamepadInjection() {
	lastGamepadInjection.Store(time.Now().UnixNano())
}

// LastKeyboardInjection returns when Core last sent a synthetic key event, or
// the zero time if it never has.
func LastKeyboardInjection() time.Time {
	return injectionTime(&lastKeyboardInjection)
}

// LastGamepadInjection returns when Core last changed its virtual gamepad
// (plug, button report or unplug), or the zero time if it never has.
func LastGamepadInjection() time.Time {
	return injectionTime(&lastGamepadInjection)
}

func injectionTime(v *atomic.Int64) time.Time {
	ns := v.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}
