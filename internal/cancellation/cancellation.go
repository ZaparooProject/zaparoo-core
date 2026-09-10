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

package cancellation

import (
	"context"

	"github.com/rs/zerolog/log"
)

// Only reports whether every leaf in an error chain is context.Canceled.
// A concurrent cancellation must not hide a storage or cleanup failure.
func Only(err error) bool {
	if err == nil {
		return false
	}
	switch e := err.(type) { //nolint:errorlint // Walk every immediate child; errors.As can skip joined failures.
	case interface{ Unwrap() []error }:
		found := false
		for _, child := range e.Unwrap() {
			if child == nil {
				continue
			}
			if !Only(child) {
				return false
			}
			found = true
		}
		return found
	case interface{ Unwrap() error }:
		return Only(e.Unwrap())
	default:
		return err == context.Canceled //nolint:errorlint // Only reached after all wrappers have been traversed.
	}
}

// LogFailure keeps cancellation-only failures out of error telemetry.
func LogFailure(err error, message string) {
	if Only(err) {
		log.Debug().Err(err).Msg(message)
	} else {
		log.Error().Err(err).Msg(message)
	}
}
