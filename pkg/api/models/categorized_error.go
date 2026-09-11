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

// Error categories emitted in ErrorData.Category. Clients branch on these
// values, so add new ones rather than renaming existing ones.
const (
	ErrorCategoryBusy            = "busy"
	ErrorCategoryMediaNotFound   = "media_not_found"
	ErrorCategoryDisabled        = "disabled"
	ErrorCategoryInvalidScript   = "invalid_script"
	ErrorCategoryBlocked         = "blocked"
	ErrorCategoryPlaytimeLimit   = "playtime_limit"
	ErrorCategoryCancelled       = "cancelled"
	ErrorCategoryTimeout         = "timeout"
	ErrorCategoryUnavailable     = "unavailable"
	ErrorCategoryExecutionFailed = "execution_failed"
)

// ErrorData is the structured payload placed in ErrorObject.Data for
// categorized errors.
type ErrorData struct {
	Category string `json:"category"`
}

// CategorizedError is a client-facing failure with a stable category and a
// message that is safe to send over the API. Error() returns only the safe
// message; the underlying cause is reachable through Unwrap for logging and
// errors.Is, but never reaches the wire because it may contain filesystem
// paths or token contents.
type CategorizedError struct {
	Err      error
	Category string
	Message  string
}

func (e *CategorizedError) Error() string {
	return e.Message
}

func (e *CategorizedError) Unwrap() error {
	return e.Err
}

// CategorizedErr wraps err with a stable category and a safe message.
func CategorizedErr(category, message string, err error) error {
	return &CategorizedError{Err: err, Category: category, Message: message}
}
