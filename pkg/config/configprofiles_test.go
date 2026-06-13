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

func TestProfilesRequireForLaunch(t *testing.T) {
	t.Parallel()

	cfg, err := NewConfig(t.TempDir(), BaseDefaults)
	require.NoError(t, err)

	// Default is false: profiles are purely additive.
	assert.False(t, cfg.ProfilesRequireForLaunch())

	cfg.SetProfilesRequireForLaunch(true)
	assert.True(t, cfg.ProfilesRequireForLaunch())

	cfg.SetProfilesRequireForLaunch(false)
	assert.False(t, cfg.ProfilesRequireForLaunch())
}

func TestProfilesRequireForLaunch_TOML(t *testing.T) {
	t.Parallel()

	cfg, err := NewConfig(t.TempDir(), BaseDefaults)
	require.NoError(t, err)

	require.NoError(t, cfg.LoadTOML(`[profiles]
require_for_launch = true`))
	assert.True(t, cfg.ProfilesRequireForLaunch())
}
