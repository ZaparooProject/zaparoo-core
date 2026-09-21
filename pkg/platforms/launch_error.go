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

import "strings"

// LaunchRepairError explicitly marks fixed, application-authored repair text
// safe for API clients. Wrapping error details remain private to Core logs.
type LaunchRepairError struct{ message string }

func (e *LaunchRepairError) Error() string {
	if e == nil || e.message == "" {
		return "player request could not be completed"
	}
	return e.message
}

// NewLaunchRepairError must never receive paths, scripts, credentials, provider
// errors or other runtime input. Use fixed messages selected by a bounded code.
func NewLaunchRepairError(message string) error {
	if message == "" || len(message) > 1024 || strings.ContainsAny(message, "\x00\r\n") {
		message = "player request could not be completed"
	}
	return &LaunchRepairError{message: message}
}
