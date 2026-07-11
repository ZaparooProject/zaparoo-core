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
	"fmt"
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

type testProviderPlatform struct {
	*mocks.MockPlatform
	defs []Launchable
}

func (p *testProviderPlatform) Launchables(*config.Instance) []Launchable {
	return p.defs
}

func testLaunch() LaunchFunc {
	return func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
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

func TestLaunchablesReturnsPlatformDefinitions(t *testing.T) {
	systemID := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	mediaID := uuid.MustParse("11890f4a-33e8-4d44-d3a8-56824d352000")
	platform := &testProviderPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		defs: []Launchable{
			VirtualSystem{ID: systemID, Name: "Chess Club", Category: "Other", Launch: testLaunch()},
			VirtualMedia{
				ID:       mediaID,
				SystemID: systemdefs.SystemCPS3,
				Name:     "Street Fighter III",
				Launch:   testLaunch(),
			},
		},
	}

	defs := Launchables(&config.Instance{}, platform)
	systems := Systems(&config.Instance{}, platform)
	media := MediaForSystem(&config.Instance{}, platform, systemdefs.SystemCPS3)

	require.Len(t, defs, 2)
	require.Len(t, systems, 1)
	assert.Equal(t, "Chess Club", systems[0].Name)
	require.Len(t, media, 1)
	assert.Equal(t, "Street Fighter III", media[0].Name)
	assert.Equal(t, "zaparoo://"+EncodeID(mediaID)+"/Street%20Fighter%20III", media[0].ZapScript())
}

func TestLaunchablesReturnsCommandVirtualSystemWithoutPlatformProvider(t *testing.T) {
	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[launchers.custom]]
id = "Tools"
kind = "virtual_system"
backend = "command"
name = "Tools"
category = "Computer"
execute = "echo tools"
`))
	platform := mocks.NewMockPlatform()
	platform.On("Launchers", cfg).Return([]platforms.Launcher{{ID: "Tools", Launch: testLaunch()}})

	defs := Launchables(cfg, platform)

	require.Len(t, defs, 1)
	entry, ok := defs[0].(VirtualSystem)
	require.True(t, ok)
	assert.Equal(t, "Tools", entry.Name)
	assert.Equal(t, "Computer", entry.Category)
	assert.Equal(t, uuid.NewSHA1(ZaparooLaunchableNamespace, []byte("command:tools")), entry.ID)
}

func TestLaunchablesReturnsNilForPlatformsWithoutDefinitions(t *testing.T) {
	platform := mocks.NewMockPlatform()

	assert.Nil(t, Launchables(&config.Instance{}, platform))
	assert.Nil(t, Systems(&config.Instance{}, platform))
	assert.Nil(t, Media(&config.Instance{}, platform))
	assert.Nil(t, Launchers(&config.Instance{}, platform))
}

func TestLaunchablesFiltersUnavailableDefinitions(t *testing.T) {
	availableID := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	unavailableID := uuid.MustParse("11890f4a-33e8-4d44-d3a8-56824d352000")
	platform := &testProviderPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		defs: []Launchable{
			VirtualSystem{
				ID:       availableID,
				Name:     "Available",
				Category: "Other",
				Launch:   testLaunch(),
				Test:     func(*config.Instance) bool { return true },
			},
			VirtualSystem{
				ID:       unavailableID,
				Name:     "Unavailable",
				Category: "Other",
				Launch:   testLaunch(),
				Test:     func(*config.Instance) bool { return false },
			},
		},
	}

	defs := Launchables(&config.Instance{}, platform)
	systems := Systems(&config.Instance{}, platform)
	launchers := Launchers(&config.Instance{}, platform)

	require.Len(t, defs, 1)
	require.Len(t, systems, 1)
	require.Len(t, launchers, 1)
	assert.Equal(t, "Available", systems[0].Name)
}

func TestPlatformLaunchers_FilterByURIID(t *testing.T) {
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	otherID := uuid.MustParse("11890f4a-33e8-4d44-d3a8-56824d352000")
	platform := &testProviderPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		defs: []Launchable{
			VirtualSystem{ID: id, Name: "Chess", Category: "Other", Launch: testLaunch()},
		},
	}

	launchers := Launchers(&config.Instance{}, platform)

	require.Len(t, launchers, 1)
	assert.Equal(t, []string{Scheme}, launchers[0].Schemes)
	assert.True(t, launchers[0].Test(nil, "zaparoo://"+EncodeID(id)+"/Chess"))
	assert.False(t, launchers[0].Test(nil, "zaparoo://"+EncodeID(otherID)+"/Other"))
}

func TestPlatformLaunchers_LaunchMatchingURI(t *testing.T) {
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	called := false
	platform := &testProviderPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		defs: []Launchable{
			VirtualMedia{
				ID:       id,
				SystemID: systemdefs.SystemCPS3,
				Name:     "Street Fighter III: 3rd Strike",
				Launch: func(
					cfg *config.Instance,
					path string,
					opts *platforms.LaunchOptions,
				) (*os.Process, error) {
					called = true
					assert.NotNil(t, cfg)
					assert.Equal(t, "zaparoo://"+EncodeID(id)+"/Street%20Fighter%20III:%203rd%20Strike", path)
					assert.NotNil(t, opts)
					return &os.Process{}, nil
				},
			},
		},
	}

	launchers := Launchers(&config.Instance{}, platform)
	media := Media(&config.Instance{}, platform)
	require.Len(t, launchers, 1)
	require.Len(t, media, 1)
	process, err := launchers[0].Launch(&config.Instance{}, media[0].ZapScript(), &platforms.LaunchOptions{})

	require.NoError(t, err)
	assert.NotNil(t, process)
	assert.True(t, called)
}

func TestPlatformLaunchers_RejectMismatchedURI(t *testing.T) {
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	otherID := uuid.MustParse("11890f4a-33e8-4d44-d3a8-56824d352000")
	platform := &testProviderPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		defs: []Launchable{
			VirtualSystem{ID: id, Name: "Chess", Category: "Other", Launch: testLaunch()},
		},
	}

	launchers := Launchers(&config.Instance{}, platform)
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
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")

	assert.True(t, IsURI("ZAPAROO://"+EncodeID(id)+"/Chess"))
	assert.False(t, IsURI("steam://123/Chess"))
}

func requirePanicContains(t *testing.T, expected string, f func()) {
	t.Helper()
	defer func() {
		recovered := recover()
		require.NotNil(t, recovered)
		assert.Contains(t, fmt.Sprint(recovered), expected)
	}()
	f()
}

func TestLaunchablesRejectsIncompleteDefinitions(t *testing.T) {
	assert.PanicsWithError(t, "virtual system \"Missing ID\": id is required", func() {
		Launchables(&config.Instance{}, &testProviderPlatform{
			MockPlatform: mocks.NewMockPlatform(),
			defs: []Launchable{
				VirtualSystem{Name: "Missing ID", Category: "Other", Launch: testLaunch()},
			},
		})
	})
	assert.PanicsWithError(t, "virtual system \"\": name is required", func() {
		Launchables(&config.Instance{}, &testProviderPlatform{
			MockPlatform: mocks.NewMockPlatform(),
			defs: []Launchable{
				VirtualSystem{
					ID:       uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000"),
					Category: "Other",
					Launch:   testLaunch(),
				},
			},
		})
	})
	assert.PanicsWithError(t, "virtual system \"Missing Launch\": launch function is required", func() {
		Launchables(&config.Instance{}, &testProviderPlatform{
			MockPlatform: mocks.NewMockPlatform(),
			defs: []Launchable{
				VirtualSystem{
					ID:       uuid.MustParse("11890f4a-33e8-4d44-d3a8-56824d352000"),
					Name:     "Missing Launch",
					Category: "Other",
				},
			},
		})
	})
	assert.PanicsWithError(t, "virtual system \"Missing Category\": category is required", func() {
		Launchables(&config.Instance{}, &testProviderPlatform{
			MockPlatform: mocks.NewMockPlatform(),
			defs: []Launchable{
				VirtualSystem{
					ID:     uuid.MustParse("21890f4a-33e8-4d44-d3a8-56824d352000"),
					Name:   "Missing Category",
					Launch: testLaunch(),
				},
			},
		})
	})
}

func TestLaunchablesRejectsDuplicateIDs(t *testing.T) {
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	platform := &testProviderPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		defs: []Launchable{
			VirtualSystem{ID: id, Name: "Chess", Category: "Other", Launch: testLaunch()},
			VirtualMedia{
				ID:       id,
				SystemID: systemdefs.SystemCPS3,
				Name:     "Street Fighter III",
				Launch:   testLaunch(),
			},
		},
	}

	requirePanicContains(t, "duplicate launchable id", func() {
		Launchables(&config.Instance{}, platform)
	})
}

func TestLaunchablesRejectsInvalidMediaSystem(t *testing.T) {
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	platform := &testProviderPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		defs: []Launchable{
			VirtualMedia{ID: id, SystemID: "Chess", Name: "Chess", Launch: testLaunch()},
		},
	}

	requirePanicContains(t, "invalid system \"Chess\"", func() {
		Launchables(&config.Instance{}, platform)
	})
}
