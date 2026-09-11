/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

// Package boolutil interprets the loosely-typed booleans that reach Core from
// user input: ZapScript arguments and traits, configuration values, and API
// payloads. It sits below pkg/helpers so packages that pkg/helpers itself
// depends on can share one definition of what counts as true.
package boolutil

import "strings"

// IsTruthy reports whether s is an affirmative written by a user.
func IsTruthy(s string) bool {
	return strings.EqualFold(s, "true") || strings.EqualFold(s, "yes")
}

// IsFalsey reports whether s is a negative written by a user.
//
// This is not the negation of IsTruthy: a value that is neither affirmative
// nor negative, such as "maybe", is neither truthy nor falsey, so callers can
// tell "the user said no" apart from "the user said something unreadable".
func IsFalsey(s string) bool {
	return strings.EqualFold(s, "false") || strings.EqualFold(s, "no")
}
