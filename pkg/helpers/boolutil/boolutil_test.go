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

package boolutil_test

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/boolutil"
	"github.com/stretchr/testify/assert"
)

func TestIsTruthy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "true_lowercase",
			input:    "true",
			expected: true,
		},
		{
			name:     "true_uppercase",
			input:    "TRUE",
			expected: true,
		},
		{
			name:     "true_mixed_case",
			input:    "TrUe",
			expected: true,
		},
		{
			name:     "yes_lowercase",
			input:    "yes",
			expected: true,
		},
		{
			name:     "yes_uppercase",
			input:    "YES",
			expected: true,
		},
		{
			name:     "yes_mixed_case",
			input:    "YeS",
			expected: true,
		},
		{
			name:     "false_string",
			input:    "false",
			expected: false,
		},
		{
			name:     "no_string",
			input:    "no",
			expected: false,
		},
		{
			name:     "empty_string",
			input:    "",
			expected: false,
		},
		{
			name:     "random_string",
			input:    "maybe",
			expected: false,
		},
		{
			name:     "numeric_one",
			input:    "1",
			expected: false,
		},
		{
			name:     "numeric_zero",
			input:    "0",
			expected: false,
		},
		{
			name:     "whitespace_around_true",
			input:    " true ",
			expected: false, // No trimming in function
		},
		{
			name:     "partial_match",
			input:    "truthy",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := boolutil.IsTruthy(tt.input)
			assert.Equal(t, tt.expected, result, "IsTruthy result mismatch")
		})
	}
}

func TestIsFalsey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "false_lowercase",
			input:    "false",
			expected: true,
		},
		{
			name:     "false_uppercase",
			input:    "FALSE",
			expected: true,
		},
		{
			name:     "false_mixed_case",
			input:    "FaLsE",
			expected: true,
		},
		{
			name:     "no_lowercase",
			input:    "no",
			expected: true,
		},
		{
			name:     "no_uppercase",
			input:    "NO",
			expected: true,
		},
		{
			name:     "no_mixed_case",
			input:    "No",
			expected: true,
		},
		{
			name:     "true_string",
			input:    "true",
			expected: false,
		},
		{
			name:     "yes_string",
			input:    "yes",
			expected: false,
		},
		{
			name:     "empty_string",
			input:    "",
			expected: false,
		},
		{
			name:     "random_string",
			input:    "maybe",
			expected: false,
		},
		{
			name:     "numeric_zero",
			input:    "0",
			expected: false,
		},
		{
			name:     "numeric_one",
			input:    "1",
			expected: false,
		},
		{
			name:     "whitespace_around_false",
			input:    " false ",
			expected: false, // No trimming in function
		},
		{
			name:     "partial_match",
			input:    "falsey",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := boolutil.IsFalsey(tt.input)
			assert.Equal(t, tt.expected, result, "IsFalsey result mismatch")
		})
	}
}
