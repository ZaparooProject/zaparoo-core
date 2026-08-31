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

package models

import "fmt"

// ClientError wraps an error to indicate it is an expected client-facing
// error (bad input, validation failure, expected operational state) rather
// than an internal server error. The API server uses this to log at Warn
// level instead of Error, keeping expected failures out of Sentry.
type ClientError struct {
	Err error
}

// QuietClientError is an expected client-facing error that should not be logged
// as a warning for every occurrence. Use for high-volume misses where the JSON-RPC
// error response is still correct, but warning logs would create noise.
type QuietClientError struct {
	Err error
}

func (e *ClientError) Error() string {
	return e.Err.Error()
}

func (e *ClientError) Unwrap() error {
	return e.Err
}

func (e *QuietClientError) Error() string {
	return e.Err.Error()
}

func (e *QuietClientError) Unwrap() error {
	return e.Err
}

func (e *QuietClientError) As(target any) bool {
	clientErr, ok := target.(**ClientError)
	if !ok {
		return false
	}
	*clientErr = &ClientError{Err: e.Err}
	return true
}

// ClientErr wraps an error as a ClientError.
func ClientErr(err error) error {
	return &ClientError{Err: err}
}

// QuietClientErr wraps an error as a QuietClientError.
func QuietClientErr(err error) error {
	return &QuietClientError{Err: err}
}

// ClientErrf creates a new formatted ClientError.
func ClientErrf(format string, a ...any) error {
	return &ClientError{Err: fmt.Errorf(format, a...)}
}

// QuietClientErrf creates a formatted QuietClientError that the API server logs
// at debug level instead of warning level.
func QuietClientErrf(format string, a ...any) error {
	return &QuietClientError{Err: fmt.Errorf(format, a...)}
}
