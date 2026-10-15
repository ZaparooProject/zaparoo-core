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

package platforms

import "context"

// InputClass is a set of kinds of user input.
type InputClass uint8

const (
	// InputKeyboard is key presses on a keyboard.
	InputKeyboard InputClass = 1 << iota
	// InputMouse is mouse or pointer movement and buttons.
	InputMouse
	// InputController is button and stick activity on a game controller.
	InputController

	// InputAll is every kind of input Core waits for before it treats
	// someone as away.
	InputAll = InputKeyboard | InputMouse | InputController
)

// Has reports whether every class in want is in c.
func (c InputClass) Has(want InputClass) bool {
	return c&want == want
}

// InputActivitySource is implemented by a platform that can tell when a
// person is using the device. Idle rotation is only as safe as this signal:
// it would switch games under someone whose keyboard, mouse or controller it
// cannot see, so a platform reports exactly what it covers and the feature
// requires InputAll.
//
// A platform that does not implement this has no input source, and anything
// that needs one refuses.
type InputActivitySource interface {
	// InputCoverage reports which classes of input WatchInput can observe.
	InputCoverage() InputClass
	// WatchInput watches input until ctx is cancelled and calls onActivity
	// when the user does something. onActivity must be cheap and may be
	// called from any goroutine; implementations call it at most about every
	// 200ms however busy the input is. Input from Zaparoo's own virtual
	// devices, and from devices that report constantly without a person
	// behind them, must not count.
	//
	// responsive asks for a low-latency watch, used for the few seconds a
	// countdown is on screen, where one tap has to register. Otherwise the
	// implementation is free to check less often and keep CPU use near zero.
	//
	// WatchInput blocks until ctx is cancelled or it cannot continue.
	WatchInput(ctx context.Context, responsive bool, onActivity func()) error
}

// ResolveInputActivity returns the input source for pl, or nil when the
// platform has none.
func ResolveInputActivity(pl Platform) InputActivitySource {
	source, ok := pl.(InputActivitySource)
	if !ok {
		return nil
	}
	return source
}
