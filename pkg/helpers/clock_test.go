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

package helpers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsClockReliable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		time time.Time
		name string
		want bool
	}{
		{
			name: "year 2024 is reliable",
			time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "year 2025 is reliable",
			time: time.Date(2025, 11, 22, 12, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "year 2030 is reliable",
			time: time.Date(2030, 6, 15, 9, 30, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "year 2023 is unreliable",
			time: time.Date(2023, 12, 31, 23, 59, 59, 0, time.UTC),
			want: false,
		},
		{
			name: "year 2000 is unreliable",
			time: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "epoch time (1970) is unreliable",
			time: time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "unix zero is unreliable",
			time: time.Unix(0, 0),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := IsClockReliable(tt.time)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClockSourceConstants(t *testing.T) {
	t.Parallel()

	// Verify constants have expected values
	assert.Equal(t, "system", ClockSourceSystem)
	assert.Equal(t, "epoch", ClockSourceEpoch)
	assert.Equal(t, "healed", ClockSourceHealed)

	// Verify all constants are unique
	sources := []string{ClockSourceSystem, ClockSourceEpoch, ClockSourceHealed}
	uniqueMap := make(map[string]bool)
	for _, source := range sources {
		assert.False(t, uniqueMap[source], "clock source %q should be unique", source)
		uniqueMap[source] = true
	}
}

func TestMinReliableYear(t *testing.T) {
	t.Parallel()

	// Verify the constant has the expected value
	assert.Equal(t, 2024, MinReliableYear)

	// Verify boundary conditions
	assert.True(t, IsClockReliable(time.Date(MinReliableYear, 1, 1, 0, 0, 0, 0, time.UTC)))
	assert.False(t, IsClockReliable(time.Date(MinReliableYear-1, 12, 31, 23, 59, 59, 0, time.UTC)))
}
