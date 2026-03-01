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

	"github.com/stretchr/testify/assert"
)

func TestAutoUpdate(t *testing.T) {
	t.Parallel()

	trueVal := true
	falseVal := false

	tests := []struct {
		autoUpdate     *bool
		name           string
		defaultEnabled bool
		expected       bool
	}{
		{
			name:           "nil default enabled returns true",
			autoUpdate:     nil,
			defaultEnabled: true,
			expected:       true,
		},
		{
			name:           "nil default disabled returns false",
			autoUpdate:     nil,
			defaultEnabled: false,
			expected:       false,
		},
		{
			name:           "explicit true overrides default disabled",
			autoUpdate:     &trueVal,
			defaultEnabled: false,
			expected:       true,
		},
		{
			name:           "explicit false overrides default enabled",
			autoUpdate:     &falseVal,
			defaultEnabled: true,
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{
				vals: Values{
					AutoUpdate: tt.autoUpdate,
				},
			}

			result := cfg.AutoUpdate(tt.defaultEnabled)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetAutoUpdate(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{},
	}

	assert.Nil(t, cfg.vals.AutoUpdate)

	cfg.SetAutoUpdate(false)
	assert.NotNil(t, cfg.vals.AutoUpdate)
	assert.False(t, cfg.AutoUpdate(true))

	cfg.SetAutoUpdate(true)
	assert.True(t, cfg.AutoUpdate(true))
}

func TestIsDevelopmentVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected bool
	}{
		{"literal DEVELOPMENT", "DEVELOPMENT", true},
		{"hash-dev suffix", "abc1234-dev", true},
		{"release version", "2.9.1", false},
		{"prerelease version", "2.10.0-rc1", false},
		{"nightly version", "2.10.0-nightly.20260228", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := AppVersion
			AppVersion = tt.version
			t.Cleanup(func() { AppVersion = original })

			assert.Equal(t, tt.expected, IsDevelopmentVersion())
		})
	}
}
