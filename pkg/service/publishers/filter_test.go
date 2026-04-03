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

package publishers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchesFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		method  string
		wantMsg string
		filter  []string
		want    bool
	}{
		{
			name:    "empty filter matches all",
			filter:  []string{},
			method:  "media.started",
			want:    true,
			wantMsg: "empty filter should match all notifications",
		},
		{
			name:    "nil filter matches all",
			filter:  nil,
			method:  "tokens.added",
			want:    true,
			wantMsg: "nil filter should match all notifications",
		},
		{
			name:    "method in filter",
			filter:  []string{"media.started", "media.stopped"},
			method:  "media.started",
			want:    true,
			wantMsg: "should match when method is in filter",
		},
		{
			name:    "method not in filter",
			filter:  []string{"media.started", "media.stopped"},
			method:  "readers.added",
			want:    false,
			wantMsg: "should not match when method not in filter",
		},
		{
			name:    "single item filter match",
			filter:  []string{"tokens.added"},
			method:  "tokens.added",
			want:    true,
			wantMsg: "should match single item in filter",
		},
		{
			name:    "single item filter no match",
			filter:  []string{"tokens.added"},
			method:  "tokens.removed",
			want:    false,
			wantMsg: "should not match when not in single-item filter",
		},
		{
			name:    "case sensitive",
			filter:  []string{"media.started"},
			method:  "Media.Started",
			want:    false,
			wantMsg: "filter matching should be case-sensitive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := MatchesFilter(tt.filter, tt.method)

			assert.Equal(t, tt.want, result, tt.wantMsg)
		})
	}
}
