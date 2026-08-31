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

package mediascanner

import (
	"errors"
	"fmt"
	"testing"

	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

func TestIsSQLiteDatabaseCorrupt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		name string
		want bool
	}{
		{
			name: "corrupt sqlite code",
			err:  sqlite3.Error{Code: sqlite3.ErrCorrupt},
			want: true,
		},
		{
			name: "not a database sqlite code",
			err:  fmt.Errorf("wrapped: %w", sqlite3.Error{Code: sqlite3.ErrNotADB}),
			want: true,
		},
		{
			name: "corrupt message fallback",
			err:  errors.New("database disk image is malformed"),
			want: true,
		},
		{
			name: "ordinary error",
			err:  errors.New("temporary query failure"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, isSQLiteDatabaseCorrupt(tt.err))
		})
	}
}
