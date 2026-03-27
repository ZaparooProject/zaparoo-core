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

package mediadb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildLetterFilterSQL(t *testing.T) {
	t.Parallel()

	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name          string
		letter        *string
		column        string
		expectClauses []string
		expectArgs    []any
	}{
		{
			name:          "nil letter returns nothing",
			letter:        nil,
			column:        "mt.Name",
			expectClauses: nil,
			expectArgs:    nil,
		},
		{
			name:          "empty string returns nothing",
			letter:        strPtr(""),
			column:        "mt.Name",
			expectClauses: nil,
			expectArgs:    nil,
		},
		{
			name:          "single uppercase letter",
			letter:        strPtr("A"),
			column:        "mt.Name",
			expectClauses: []string{"UPPER(SUBSTR(mt.Name, 1, 1)) = ?"},
			expectArgs:    []any{"A"},
		},
		{
			name:          "single lowercase letter is uppercased",
			letter:        strPtr("z"),
			column:        "mt.Name",
			expectClauses: []string{"UPPER(SUBSTR(mt.Name, 1, 1)) = ?"},
			expectArgs:    []any{"Z"},
		},
		{
			name:   "0-9 range filter",
			letter: strPtr("0-9"),
			column: "mt.Name",
			expectClauses: []string{
				"UPPER(SUBSTR(mt.Name, 1, 1)) BETWEEN '0' AND '9'",
			},
			expectArgs: nil,
		},
		{
			name:   "hash symbol filter",
			letter: strPtr("#"),
			column: "mt.Name",
			expectClauses: []string{
				"UPPER(SUBSTR(mt.Name, 1, 1)) NOT BETWEEN 'A' AND 'Z'",
				"UPPER(SUBSTR(mt.Name, 1, 1)) NOT BETWEEN '0' AND '9'",
			},
			expectArgs: nil,
		},
		{
			name:          "multi-char string is ignored",
			letter:        strPtr("AB"),
			column:        "mt.Name",
			expectClauses: nil,
			expectArgs:    nil,
		},
		{
			name:          "non-letter single char is ignored",
			letter:        strPtr("!"),
			column:        "mt.Name",
			expectClauses: nil,
			expectArgs:    nil,
		},
		{
			name:          "custom column name",
			letter:        strPtr("M"),
			column:        "MediaTitles.Name",
			expectClauses: []string{"UPPER(SUBSTR(MediaTitles.Name, 1, 1)) = ?"},
			expectArgs:    []any{"M"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			clauses, args := BuildLetterFilterSQL(tt.letter, tt.column)
			assert.Equal(t, tt.expectClauses, clauses)
			assert.Equal(t, tt.expectArgs, args)
		})
	}
}
