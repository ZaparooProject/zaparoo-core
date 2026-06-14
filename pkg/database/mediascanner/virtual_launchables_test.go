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

package mediascanner

import (
	"context"
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/launchables"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func withMediascannerLaunchablesRegistry(t *testing.T, registry *launchables.Registry) {
	t.Helper()
	oldRegistry := launchables.DefaultRegistry
	launchables.DefaultRegistry = registry
	t.Cleanup(func() {
		launchables.DefaultRegistry = oldRegistry
	})
}

func testVirtualLaunch() launchables.LaunchFunc {
	return func(*config.Instance, platforms.Platform, string, *platforms.LaunchOptions) (*os.Process, error) {
		return &os.Process{}, nil
	}
}

func TestNewIndexLauncherCacheIncludesRegistryLaunchers(t *testing.T) {
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	item := launchables.VirtualMedia{
		ID:          id,
		SystemID:    systemdefs.SystemCPS3,
		Name:        "Street Fighter III: 3rd Strike",
		PlatformIDs: []string{"test-platform"},
		Launch:      testVirtualLaunch(),
	}
	withMediascannerLaunchablesRegistry(t, launchables.MustNewRegistry(nil, []launchables.VirtualMedia{item}))

	platform := mocks.NewMockPlatform()
	platform.On("ID").Return("test-platform")
	platform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
		{ID: "NativeLauncher", SystemID: systemdefs.SystemCPS3},
	})
	cfg := &config.Instance{}

	launcherCache, allLaunchers := newIndexLauncherCache(cfg, platform)

	assert.Len(t, allLaunchers, 2)
	systemLaunchers := launcherCache.GetLaunchersBySystem(systemdefs.SystemCPS3)
	require.Len(t, systemLaunchers, 2)
	assert.True(t, helpers.PathIsLauncher(cfg, platform, &systemLaunchers[1], item.ZapScript()))
	wrongURI := "zaparoo://aaaaaaaaaaaaaaaaaaaaaaaaaa/Wrong"
	assert.False(t, helpers.PathIsLauncher(cfg, platform, &systemLaunchers[1], wrongURI))
	platform.AssertExpectations(t)
}

func TestNewNamesIndexIndexesVirtualMediaAndPreservesVirtualSystems(t *testing.T) {
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	item := launchables.VirtualMedia{
		ID:          id,
		SystemID:    systemdefs.SystemCPS3,
		Name:        "Street Fighter III: 3rd Strike",
		PlatformIDs: []string{"test-platform"},
		Launch:      testVirtualLaunch(),
	}
	withMediascannerLaunchablesRegistry(t, launchables.MustNewRegistry(nil, []launchables.VirtualMedia{item}))

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	platform := mocks.NewMockPlatform()
	platform.On("ID").Return("test-platform")
	platform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})
	platform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{})

	count, err := NewNamesIndex(
		context.Background(),
		platform,
		cfg,
		[]systemdefs.System{{ID: systemdefs.SystemCPS3}},
		db,
		func(IndexStatus) {},
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, count)
	media, err := db.MediaDB.GetMediaBySystemID(systemdefs.SystemCPS3)
	require.NoError(t, err)
	require.Len(t, media, 1)
	assert.Equal(t, item.ZapScript(), media[0].Path)
	assert.Equal(t, systemdefs.SystemCPS3, media[0].SystemID)
	platform.AssertExpectations(t)
}
