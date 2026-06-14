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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
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

func TestRegistryFiltersByPlatformAndSystem(t *testing.T) {
	misterID := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	linuxID := uuid.MustParse("11890f4a-33e8-4d44-d3a8-56824d352000")
	openID := uuid.MustParse("21890f4a-33e8-4d44-d3a8-56824d352000")
	linuxMediaID := uuid.MustParse("31890f4a-33e8-4d44-d3a8-56824d352000")
	registry := MustNewRegistry([]VirtualSystem{
		{
			ID:          misterID,
			Name:        "Chess Club",
			Category:    "Other",
			PlatformIDs: []string{"mister"},
			Launch:      testLaunch(),
		},
		{
			ID:          linuxID,
			Name:        "Linux Only",
			Category:    "Other",
			PlatformIDs: []string{"linux"},
			Launch:      testLaunch(),
		},
	}, []VirtualMedia{
		{
			ID:       openID,
			SystemID: systemdefs.SystemCPS3,
			Name:     "Street Fighter III: 3rd Strike",
			Launch:   testLaunch(),
		},
		{
			ID:          linuxMediaID,
			SystemID:    systemdefs.SystemNES,
			Name:        "Linux Media",
			PlatformIDs: []string{"linux"},
			Launch:      testLaunch(),
		},
	})
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("MiSTer")

	systems := registry.Systems(mockPlatform)
	media := registry.MediaForSystem(mockPlatform, systemdefs.SystemCPS3)

	require.Len(t, systems, 1)
	assert.Equal(t, "Chess Club", systems[0].Name)
	require.Len(t, media, 1)
	assert.Equal(t, "Street Fighter III: 3rd Strike", media[0].Name)
	assert.Equal(t, "zaparoo://"+EncodeID(openID)+"/Street%20Fighter%20III:%203rd%20Strike", media[0].ZapScript())
	mockPlatform.AssertExpectations(t)
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

func TestRegistryLaunchers_LaunchMatchingURI(t *testing.T) {
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	called := false
	registry := MustNewRegistry(nil, []VirtualMedia{
		{
			ID:       id,
			SystemID: systemdefs.SystemCPS3,
			Name:     "Street Fighter III: 3rd Strike",
			Launch: func(
				cfg *config.Instance,
				pl platforms.Platform,
				path string,
				opts *platforms.LaunchOptions,
			) (*os.Process, error) {
				called = true
				assert.NotNil(t, cfg)
				assert.Same(t, mockPlatform, pl)
				assert.Equal(t, "zaparoo://"+EncodeID(id)+"/Street%20Fighter%20III:%203rd%20Strike", path)
				assert.NotNil(t, opts)
				return &os.Process{}, nil
			},
		},
	})

	launchers := registry.Launchers(mockPlatform)
	require.Len(t, launchers, 1)
	process, err := launchers[0].Launch(&config.Instance{}, registry.media[0].ZapScript(), &platforms.LaunchOptions{})

	require.NoError(t, err)
	assert.NotNil(t, process)
	assert.True(t, called)
}

func TestRegistryLaunchers_RejectMismatchedURI(t *testing.T) {
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
	process, err := launchers[0].Launch(&config.Instance{}, "zaparoo://"+EncodeID(otherID)+"/Chess", nil)

	require.Error(t, err)
	assert.Nil(t, process)
	assert.Contains(t, err.Error(), "does not match launcher")
}

func TestParseURIRejectsInvalidVirtualPaths(t *testing.T) {
	_, ok, err := ParseURI("steam://123/Game")
	require.NoError(t, err)
	assert.False(t, ok)

	_, ok, err = ParseURI("zaparoo:///missing-host")
	require.Error(t, err)
	assert.True(t, ok)

	_, ok, err = ParseURI("zaparoo://bad/Game")
	require.Error(t, err)
	assert.True(t, ok)
}

func TestIsURI(t *testing.T) {
	assert.True(t, IsURI("ZAPAROO://"+EncodeID(uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000"))+"/Chess"))
	assert.False(t, IsURI("steam://123/Chess"))
}

func TestNewRegistryRejectsIncompleteDefinitions(t *testing.T) {
	_, err := NewRegistry([]VirtualSystem{{Name: "Missing ID", Category: "Other", Launch: testLaunch()}}, nil)
	require.ErrorContains(t, err, "id is required")

	_, err = NewRegistry([]VirtualSystem{
		{ID: uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000"), Category: "Other", Launch: testLaunch()},
	}, nil)
	require.ErrorContains(t, err, "name is required")

	_, err = NewRegistry([]VirtualSystem{
		{ID: uuid.MustParse("11890f4a-33e8-4d44-d3a8-56824d352000"), Name: "Missing Launch", Category: "Other"},
	}, nil)
	require.ErrorContains(t, err, "launch function is required")

	_, err = NewRegistry([]VirtualSystem{
		{ID: uuid.MustParse("21890f4a-33e8-4d44-d3a8-56824d352000"), Name: "Missing Category", Launch: testLaunch()},
	}, nil)
	require.ErrorContains(t, err, "category is required")
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
