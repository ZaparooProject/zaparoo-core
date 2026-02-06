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

package methods

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	corehelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestHandleLaunchersRefresh_ReloadsFromDisk tests that HandleLaunchersRefresh
// reloads config and custom launcher files from disk before refreshing the cache.
func TestHandleLaunchersRefresh_ReloadsFromDisk(t *testing.T) {
	t.Parallel()

	memFS := helpers.NewMemoryFS()
	dataDir := "/data"
	configDir := "/config"
	require.NoError(t, memFS.Fs.MkdirAll(configDir, 0o750))
	require.NoError(t, memFS.Fs.MkdirAll(dataDir+"/"+config.LaunchersDir, 0o750))

	cfg, err := helpers.NewTestConfig(memFS, configDir)
	require.NoError(t, err)

	expectedLaunchers := []platforms.Launcher{
		{ID: "custom-launcher", SystemID: "SNES"},
		{ID: "another-launcher", SystemID: "Genesis"},
	}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: dataDir}).Maybe()
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return(expectedLaunchers).Maybe()

	testCache := &corehelpers.LauncherCache{}
	assert.Empty(t, testCache.GetAllLaunchers())

	env := requests.RequestEnv{
		Platform:      mockPlatform,
		Config:        cfg,
		LauncherCache: testCache,
	}

	result, err := HandleLaunchersRefresh(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, result)

	cached := testCache.GetAllLaunchers()
	require.Len(t, cached, 2)
	assert.Equal(t, "custom-launcher", cached[0].ID)
	assert.Equal(t, "SNES", cached[0].SystemID)
	assert.Equal(t, "another-launcher", cached[1].ID)
	assert.Equal(t, "Genesis", cached[1].SystemID)

	// Verify Launchers was called (cache was refreshed)
	mockPlatform.AssertCalled(t, "Launchers", mock.AnythingOfType("*config.Instance"))
}

// TestHandleLaunchersRefresh_CacheUpdatesOnSecondCall tests that the cache
// reflects new data when the handler is called again with different launchers.
func TestHandleLaunchersRefresh_CacheUpdatesOnSecondCall(t *testing.T) {
	t.Parallel()

	memFS := helpers.NewMemoryFS()
	dataDir := "/data"
	configDir := "/config"
	require.NoError(t, memFS.Fs.MkdirAll(configDir, 0o750))
	require.NoError(t, memFS.Fs.MkdirAll(dataDir+"/"+config.LaunchersDir, 0o750))

	cfg, err := helpers.NewTestConfig(memFS, configDir)
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: dataDir}).Maybe()

	// First call returns one launcher
	firstLaunchers := []platforms.Launcher{
		{ID: "launcher-v1", SystemID: "NES"},
	}
	// Second call returns different launchers
	secondLaunchers := []platforms.Launcher{
		{ID: "launcher-v2", SystemID: "SNES"},
		{ID: "launcher-v3", SystemID: "Genesis"},
	}
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return(firstLaunchers).Once()
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return(secondLaunchers).Once()

	testCache := &corehelpers.LauncherCache{}

	env := requests.RequestEnv{
		Platform:      mockPlatform,
		Config:        cfg,
		LauncherCache: testCache,
	}

	// First refresh
	_, err = HandleLaunchersRefresh(env)
	require.NoError(t, err)
	cached := testCache.GetAllLaunchers()
	require.Len(t, cached, 1)
	assert.Equal(t, "launcher-v1", cached[0].ID)

	// Second refresh picks up updated launchers
	_, err = HandleLaunchersRefresh(env)
	require.NoError(t, err)
	cached = testCache.GetAllLaunchers()
	require.Len(t, cached, 2)
	assert.Equal(t, "launcher-v2", cached[0].ID)
	assert.Equal(t, "launcher-v3", cached[1].ID)
}
