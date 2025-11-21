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

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPlaytimeLimitsEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		enabled *bool
		name    string
		want    bool
	}{
		{
			name:    "nil returns false",
			enabled: nil,
			want:    false,
		},
		{
			name:    "false returns false",
			enabled: boolPtr(false),
			want:    false,
		},
		{
			name:    "true returns true",
			enabled: boolPtr(true),
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inst := &Instance{
				vals: Values{
					Playtime: Playtime{
						Limits: PlaytimeLimits{
							Enabled: tt.enabled,
						},
					},
				},
			}

			got := inst.PlaytimeLimitsEnabled()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDailyLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		limit string
		name  string
		want  time.Duration
	}{
		{
			name:  "empty string returns 0",
			limit: "",
			want:  0,
		},
		{
			name:  "valid duration in hours",
			limit: "2h",
			want:  2 * time.Hour,
		},
		{
			name:  "valid duration in minutes",
			limit: "90m",
			want:  90 * time.Minute,
		},
		{
			name:  "invalid duration returns 0",
			limit: "invalid",
			want:  0,
		},
		{
			name:  "complex duration",
			limit: "1h30m",
			want:  90 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inst := &Instance{
				vals: Values{
					Playtime: Playtime{
						Limits: PlaytimeLimits{
							Daily: tt.limit,
						},
					},
				},
			}

			got := inst.DailyLimit()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSessionLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		limit string
		name  string
		want  time.Duration
	}{
		{
			name:  "empty string returns 0",
			limit: "",
			want:  0,
		},
		{
			name:  "valid duration in minutes",
			limit: "45m",
			want:  45 * time.Minute,
		},
		{
			name:  "valid duration in hours",
			limit: "1h",
			want:  1 * time.Hour,
		},
		{
			name:  "invalid duration returns 0",
			limit: "not-a-duration",
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inst := &Instance{
				vals: Values{
					Playtime: Playtime{
						Limits: PlaytimeLimits{
							Session: tt.limit,
						},
					},
				},
			}

			got := inst.SessionLimit()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWarningIntervals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		intervals []string
		name      string
		want      []time.Duration
	}{
		{
			name:      "nil returns defaults",
			intervals: nil,
			want:      []time.Duration{5 * time.Minute, 2 * time.Minute, 1 * time.Minute},
		},
		{
			name:      "empty slice returns defaults",
			intervals: []string{},
			want:      []time.Duration{5 * time.Minute, 2 * time.Minute, 1 * time.Minute},
		},
		{
			name:      "valid intervals",
			intervals: []string{"10m", "5m", "1m"},
			want:      []time.Duration{10 * time.Minute, 5 * time.Minute, 1 * time.Minute},
		},
		{
			name:      "skips invalid intervals",
			intervals: []string{"10m", "invalid", "1m"},
			want:      []time.Duration{10 * time.Minute, 1 * time.Minute},
		},
		{
			name:      "skips zero and negative intervals",
			intervals: []string{"10m", "0m", "-5m", "1m"},
			want:      []time.Duration{10 * time.Minute, 1 * time.Minute},
		},
		{
			name:      "custom intervals",
			intervals: []string{"15m", "7m", "3m", "30s"},
			want: []time.Duration{
				15 * time.Minute,
				7 * time.Minute,
				3 * time.Minute,
				30 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inst := &Instance{
				vals: Values{
					Playtime: Playtime{
						Limits: PlaytimeLimits{
							Warnings: tt.intervals,
						},
					},
				},
			}

			got := inst.WarningIntervals()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPlaytimeRetention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		retention *int
		name      string
		want      int
	}{
		{
			name:      "nil returns 365 days default",
			retention: nil,
			want:      365,
		},
		{
			name:      "zero returns zero",
			retention: intPtr(0),
			want:      0,
		},
		{
			name:      "custom value",
			retention: intPtr(90),
			want:      90,
		},
		{
			name:      "one year",
			retention: intPtr(365),
			want:      365,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inst := &Instance{
				vals: Values{
					Playtime: Playtime{
						Retention: tt.retention,
					},
				},
			}

			got := inst.PlaytimeRetention()
			assert.Equal(t, tt.want, got)
		})
	}
}

func intPtr(i int) *int {
	return &i
}
