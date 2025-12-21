// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package validation

import (
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

// Error wraps validation errors with formatted messages.
type Error struct {
	Fields []FieldError
}

// FieldError represents a single field validation error.
type FieldError struct {
	Field   string
	Tag     string
	Value   any
	Message string
}

func (e *Error) Error() string {
	if len(e.Fields) == 0 {
		return "validation failed"
	}
	msgs := make([]string, len(e.Fields))
	for i, fe := range e.Fields {
		msgs[i] = fe.Message
	}
	return strings.Join(msgs, "; ")
}

// NewError creates an Error from validator.ValidationErrors.
func NewError(errs validator.ValidationErrors) *Error {
	ve := &Error{
		Fields: make([]FieldError, len(errs)),
	}
	for i, fe := range errs {
		ve.Fields[i] = FieldError{
			Field:   fe.Field(),
			Tag:     fe.Tag(),
			Value:   fe.Value(),
			Message: formatValidationError(fe),
		}
	}
	return ve
}

// formatValidationError creates a human-readable error message.
func formatValidationError(fe validator.FieldError) string {
	field := strings.ToLower(fe.Field())
	switch fe.Tag() {
	case "required":
		return field + " is required"
	case "letter":
		return field + " must be A-Z, 0-9, or #"
	case "duration":
		return field + " must be a valid duration (e.g., 1h30m)"
	case "regex":
		return field + " must be a valid regex pattern"
	case "system":
		return fmt.Sprintf("system %q not found", fe.Value())
	case "launcher":
		return fmt.Sprintf("launcher %q not found", fe.Value())
	case "hexadecimal":
		return field + " must be a valid hexadecimal string"
	case "hexdata":
		return field + " must be valid hex data (e.g., \"AABBCC\" or \"AA BB CC\")"
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, fe.Param())
	case "min":
		return fmt.Sprintf("%s must be at least %s", field, fe.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s", field, fe.Param())
	case "gt":
		return fmt.Sprintf("%s must be greater than %s", field, fe.Param())
	case "gte":
		return fmt.Sprintf("%s must be greater than or equal to %s", field, fe.Param())
	case "lt":
		return fmt.Sprintf("%s must be less than %s", field, fe.Param())
	case "lte":
		return fmt.Sprintf("%s must be less than or equal to %s", field, fe.Param())
	default:
		return fmt.Sprintf("%s failed %s validation", field, fe.Tag())
	}
}
