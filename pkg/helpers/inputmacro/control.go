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

// SplitHold separates a hold token's value into the key name and its optional
// duration ("esc:500" -> "esc", "500").
func SplitHold(value string) (name, duration string) {
	if idx := strings.LastIndex(value, ":"); idx != -1 {
		return value[:idx], value[idx+1:]
	}
	return value, ""
}

// KeyForToken returns the key or button name a token acts on, in the braced
// form the key tables and config allow/block lists use. Control tokens are
// unwrapped so the key they act on is what gets validated — otherwise
// "{press:super}" would slip past a "{super}" block list entry. Delay tokens
// carry a duration rather than a key and report false.
func KeyForToken(token string) (key string, ok bool) {
	control := ClassifyControl(token)
	switch control.Action {
	case ActionNone:
		return token, true
	case ActionDelay:
		return "", false
	case ActionPress, ActionRelease:
		return braceMultiRune(control.Value), true
	case ActionHold:
		name, _ := SplitHold(control.Value)
		return braceMultiRune(name), true
	default:
		return token, true
	}
}

// braceMultiRune adds braces to multi-character key names, which is how they
// appear in the key tables and config lists. Single characters stay bare.
func braceMultiRune(name string) string {
	if len([]rune(name)) > 1 {
		return "{" + name + "}"
	}
	return name
}
