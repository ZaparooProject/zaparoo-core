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

	"github.com/stretchr/testify/assert"
)

func TestMediaHistory(t *testing.T) {
	t.Parallel()

	threeSixtyFive := 365
	ninety := 90
	zero := 0

	tests := []struct {
		mediaHistory *int
		name         string
		expected     int
	}{
		{
			name:         "nil returns default 365 days",
			mediaHistory: nil,
			expected:     365,
		},
		{
			name:         "explicit 90 days",
			mediaHistory: &ninety,
			expected:     90,
		},
		{
			name:         "explicit 365 days",
			mediaHistory: &threeSixtyFive,
			expected:     365,
		},
		{
			name:         "zero (unlimited)",
			mediaHistory: &zero,
			expected:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{
				vals: Values{
					Media: Media{
						MediaHistory: tt.mediaHistory,
					},
				},
			}

			result := cfg.MediaHistory()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFilenameTags(t *testing.T) {
	t.Parallel()

	trueVal := true
	falseVal := false

	tests := []struct {
		filenameTags *bool
		name         string
		expected     bool
	}{
		{
			name:         "nil returns default true",
			filenameTags: nil,
			expected:     true,
		},
		{
			name:         "explicit true",
			filenameTags: &trueVal,
			expected:     true,
		},
		{
			name:         "explicit false",
			filenameTags: &falseVal,
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{
				vals: Values{
					Media: Media{
						FilenameTags: tt.filenameTags,
					},
				},
			}

			result := cfg.FilenameTags()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetFilenameTags(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{
			Media: Media{},
		},
	}

	// Initially should be default (true)
	assert.True(t, cfg.FilenameTags())

	// Set to false
	cfg.SetFilenameTags(false)
	assert.False(t, cfg.FilenameTags())

	// Set back to true
	cfg.SetFilenameTags(true)
	assert.True(t, cfg.FilenameTags())
}

func TestDefaultRegions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		regions  []string
		name     string
		expected []string
	}{
		{
			name:     "nil returns default [us, world]",
			regions:  nil,
			expected: []string{"us", "world"},
		},
		{
			name:     "empty slice returns default [us, world]",
			regions:  []string{},
			expected: []string{"us", "world"},
		},
		{
			name:     "single region",
			regions:  []string{"USA"},
			expected: []string{"USA"},
		},
		{
			name:     "multiple regions",
			regions:  []string{"USA", "Europe", "Japan"},
			expected: []string{"USA", "Europe", "Japan"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{
				vals: Values{
					Media: Media{
						DefaultRegions: tt.regions,
					},
				},
			}

			result := cfg.DefaultRegions()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDefaultLangs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		langs    []string
		name     string
		expected []string
	}{
		{
			name:     "nil returns default [en]",
			langs:    nil,
			expected: []string{"en"},
		},
		{
			name:     "empty slice returns default [en]",
			langs:    []string{},
			expected: []string{"en"},
		},
		{
			name:     "single language",
			langs:    []string{"en"},
			expected: []string{"en"},
		},
		{
			name:     "multiple languages",
			langs:    []string{"en", "es", "fr"},
			expected: []string{"en", "es", "fr"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{
				vals: Values{
					Media: Media{
						DefaultLangs: tt.langs,
					},
				},
			}

			result := cfg.DefaultLangs()
			assert.Equal(t, tt.expected, result)
		})
	}
}
