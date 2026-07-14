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

package config

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/assert"
)

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

func TestIsStreamingSystem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		systemID string
		expected bool
	}{
		{name: "Saturn (pause-tier) is streaming", systemID: systemdefs.SystemSaturn, expected: true},
		{name: "3DO (pause-tier) is streaming", systemID: systemdefs.System3DO, expected: true},
		{name: "CDI (pause-tier) is streaming", systemID: systemdefs.SystemCDI, expected: true},
		{name: "JaguarCD (pause-tier) is streaming", systemID: systemdefs.SystemJaguarCD, expected: true},
		{name: "PSX (heavy-tier) is streaming", systemID: systemdefs.SystemPSX, expected: true},
		{name: "MegaCD (heavy-tier) is streaming", systemID: systemdefs.SystemMegaCD, expected: true},
		{name: "NES is not streaming", systemID: "NES", expected: false},
		{name: "SNES is not streaming", systemID: "SNES", expected: false},
		{name: "empty systemID is not streaming", systemID: "", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := IsStreamingSystem(tt.systemID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveMediaPausePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		systemID string
		expected MediaPausePolicy
	}{
		{
			name:     "pause-tier system (Saturn) defaults to pause",
			systemID: systemdefs.SystemSaturn,
			expected: MediaPausePolicy{Mode: IndexDuringMediaPause, Level: syncutil.ThrottleHeavy},
		},
		{
			name:     "pause-tier system (3DO) defaults to pause",
			systemID: systemdefs.System3DO,
			expected: MediaPausePolicy{Mode: IndexDuringMediaPause, Level: syncutil.ThrottleHeavy},
		},
		{
			name:     "pause-tier system (CDI) defaults to pause",
			systemID: systemdefs.SystemCDI,
			expected: MediaPausePolicy{Mode: IndexDuringMediaPause, Level: syncutil.ThrottleHeavy},
		},
		{
			name:     "pause-tier system (JaguarCD) defaults to pause",
			systemID: systemdefs.SystemJaguarCD,
			expected: MediaPausePolicy{Mode: IndexDuringMediaPause, Level: syncutil.ThrottleHeavy},
		},
		{
			name:     "heavy-tier system (PSX) defaults to heavy throttle",
			systemID: systemdefs.SystemPSX,
			expected: MediaPausePolicy{Mode: IndexDuringMediaThrottle, Level: syncutil.ThrottleHeavy},
		},
		{
			name:     "heavy-tier system (MegaCD) defaults to heavy throttle",
			systemID: systemdefs.SystemMegaCD,
			expected: MediaPausePolicy{Mode: IndexDuringMediaThrottle, Level: syncutil.ThrottleHeavy},
		},
		{
			name:     "non-streaming system defaults to light throttle",
			systemID: "NES",
			expected: MediaPausePolicy{Mode: IndexDuringMediaThrottle, Level: syncutil.ThrottleLight},
		},
		{
			name:     "empty systemID defaults to light throttle",
			systemID: "",
			expected: MediaPausePolicy{Mode: IndexDuringMediaThrottle, Level: syncutil.ThrottleLight},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{}

			result := cfg.ResolveMediaPausePolicy(tt.systemID)
			assert.Equal(t, tt.expected, result)
		})
	}
}
