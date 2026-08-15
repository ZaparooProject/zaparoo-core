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

// Package inputmacro classifies control tokens shared by input transports and emitters.
package inputmacro

import "strings"

// Action identifies an inline input macro control operation.
type Action uint8

const (
	ActionNone Action = iota
	ActionDelay
	ActionPress
	ActionRelease
	ActionHold
)

// Control describes a recognized inline input macro token.
type Control struct {
	Value  string
	Action Action
}

// ClassifyControl recognizes braced delay, press, release, and hold tokens,
// including their sigil forms. Non-control tokens return ActionNone.
func ClassifyControl(token string) Control {
	if len(token) <= 2 || token[0] != '{' || token[len(token)-1] != '}' {
		return Control{}
	}

	inner := token[1 : len(token)-1]
	switch {
	case strings.HasPrefix(inner, "delay:"):
		return Control{Action: ActionDelay, Value: inner[len("delay:"):]}
	case strings.HasPrefix(inner, "press:"):
		return Control{Action: ActionPress, Value: inner[len("press:"):]}
	case strings.HasPrefix(inner, "release:"):
		return Control{Action: ActionRelease, Value: inner[len("release:"):]}
	case strings.HasPrefix(inner, "hold:"):
		return Control{Action: ActionHold, Value: inner[len("hold:"):]}
	case len(inner) > 1 && inner[0] == '_':
		return Control{Action: ActionPress, Value: inner[1:]}
	case len(inner) > 1 && inner[0] == '^':
		return Control{Action: ActionRelease, Value: inner[1:]}
	case len(inner) > 1 && inner[0] == '~':
		return Control{Action: ActionHold, Value: inner[1:]}
	default:
		return Control{}
	}
}
