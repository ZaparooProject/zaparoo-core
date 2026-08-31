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
	"github.com/stretchr/testify/require"
)

func TestUpdateCheck(t *testing.T) {
	t.Parallel()

	trueVal := true
	falseVal := false

	tests := []struct {
		check    *bool
		name     string
		expected bool
	}{
		{name: "unset checks", check: nil, expected: true},
		{name: "explicit true checks", check: &trueVal, expected: true},
		{name: "explicit false does not check", check: &falseVal, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{vals: Values{Updates: Updates{Check: tt.check}}}
			assert.Equal(t, tt.expected, cfg.UpdateCheck())
		})
	}
}

func TestSetUpdateCheck(t *testing.T) {
	t.Parallel()

	cfg := &Instance{vals: Values{}}
	assert.Nil(t, cfg.vals.Updates.Check)

	cfg.SetUpdateCheck(false)
	assert.NotNil(t, cfg.vals.Updates.Check)
	assert.False(t, cfg.UpdateCheck())

	cfg.SetUpdateCheck(true)
	assert.True(t, cfg.UpdateCheck())
}

func TestUpdateInstall(t *testing.T) {
	t.Parallel()

	trueVal := true
	falseVal := false

	tests := []struct {
		check    *bool
		install  *bool
		name     string
		expected bool
	}{
		{name: "unset does not install", check: nil, install: nil, expected: false},
		{name: "turned on installs", check: nil, install: &trueVal, expected: true},
		{name: "turned off does not install", check: nil, install: &falseVal, expected: false},
		{
			// Installing without checking is not a state the device can be in.
			name: "checking off wins", check: &falseVal, install: &trueVal, expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{vals: Values{Updates: Updates{Check: tt.check, Install: tt.install}}}
			assert.Equal(t, tt.expected, cfg.UpdateInstall())
		})
	}
}

func TestSetUpdateInstall(t *testing.T) {
	t.Parallel()

	cfg := &Instance{vals: Values{}}
	assert.Nil(t, cfg.vals.Updates.Install)

	cfg.SetUpdateInstall(true)
	assert.True(t, cfg.UpdateInstall())

	cfg.SetUpdateInstall(false)
	assert.False(t, cfg.UpdateInstall())
}

// A config written by an older release still has the keys that were replaced.
// They are ignored, and the defaults they used to carry are what the device
// falls back to.
func TestUpdates_LegacyKeysAreIgnored(t *testing.T) {
	t.Parallel()

	cfg := &Instance{vals: Values{}}
	legacy := "auto_update = false\nauto_update_install = true\nupdate_channel = 'beta'\n"
	require.NoError(t, cfg.applyTOML(legacy))

	assert.True(t, cfg.UpdateCheck())
	assert.False(t, cfg.UpdateInstall())
	assert.Equal(t, UpdateChannelStable, cfg.UpdateChannel())
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
