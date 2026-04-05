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

func TestUpdateChannel(t *testing.T) {
	t.Parallel()

	stable := "stable"
	beta := "beta"

	tests := []struct {
		channel  *string
		name     string
		expected string
	}{
		{
			name:     "nil defaults to stable",
			channel:  nil,
			expected: "stable",
		},
		{
			name:     "explicit stable",
			channel:  &stable,
			expected: "stable",
		},
		{
			name:     "explicit beta",
			channel:  &beta,
			expected: "beta",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{
				vals: Values{
					UpdateChannel: tt.channel,
				},
			}

			result := cfg.UpdateChannel()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetUpdateChannel(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{},
	}

	assert.Nil(t, cfg.vals.UpdateChannel)

	cfg.SetUpdateChannel("beta")
	assert.NotNil(t, cfg.vals.UpdateChannel)
	assert.Equal(t, "beta", cfg.UpdateChannel())

	cfg.SetUpdateChannel("stable")
	assert.Equal(t, "stable", cfg.UpdateChannel())
}
