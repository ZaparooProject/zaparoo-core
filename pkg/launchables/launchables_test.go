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

package launchables

import (
	"os"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLaunch() LaunchFunc {
	return func(*config.Instance, platforms.Platform, string, *platforms.LaunchOptions) (*os.Process, error) {
		return nil, nil
	}
}

func TestEncodeID_Base32UUID(t *testing.T) {
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")

	encoded := EncodeID(id)

	assert.Len(t, encoded, 26)
	assert.Equal(t, strings.ToLower(encoded), encoded)
	assert.Regexp(t, `^[a-z2-7]{26}$`, encoded)

	decoded, err := DecodeID(strings.ToUpper(encoded))
	require.NoError(t, err)
	assert.Equal(t, id, decoded)
}

func TestParseURI_UsesHostID(t *testing.T) {
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	rawURI := "zaparoo://" + EncodeID(id) + "/Street%20Fighter%20III"

	decoded, ok, err := ParseURI(rawURI)

	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, id, decoded)
}

func TestRegistryLaunchers_FilterByURIID(t *testing.T) {
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	otherID := uuid.MustParse("11890f4a-33e8-4d44-d3a8-56824d352000")
	registry := MustNewRegistry([]VirtualSystem{
		{
			ID:       id,
			Name:     "Chess",
			Category: "Other",
			Launch:   testLaunch(),
		},
	}, nil)

	launchers := registry.Launchers(nil)

	require.Len(t, launchers, 1)
	assert.Equal(t, []string{Scheme}, launchers[0].Schemes)
	assert.True(t, launchers[0].Test(nil, "zaparoo://"+EncodeID(id)+"/Chess"))
	assert.False(t, launchers[0].Test(nil, "zaparoo://"+EncodeID(otherID)+"/Other"))
}

func TestNewRegistryRejectsDuplicateIDs(t *testing.T) {
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")

	_, err := NewRegistry([]VirtualSystem{
		{
			ID:       id,
			Name:     "Chess",
			Category: "Other",
			Launch:   testLaunch(),
		},
	}, []VirtualMedia{
		{
			ID:       id,
			SystemID: systemdefs.SystemCPS3,
			Name:     "Street Fighter III: 3rd Strike",
			Launch:   testLaunch(),
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate launchable id")
}

func TestNewRegistryRejectsInvalidMediaSystem(t *testing.T) {
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")

	_, err := NewRegistry(nil, []VirtualMedia{
		{
			ID:       id,
			SystemID: "Chess",
			Name:     "Chess",
			Launch:   testLaunch(),
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid system")
}
