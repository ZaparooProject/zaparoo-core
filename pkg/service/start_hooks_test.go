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

package service

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestIsFirstServiceStartForBootPersistsBootID(t *testing.T) {
	oldDetectSystemBootID := detectSystemBootID
	defer func() { detectSystemBootID = oldDetectSystemBootID }()

	bootID := "boot-1"
	detectSystemBootID = func() (string, error) {
		return bootID, nil
	}

	testRoot := t.TempDir()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: testRoot})

	first, err := isFirstServiceStartForBoot(mockPlatform)
	require.NoError(t, err)
	assert.True(t, first)

	first, err = isFirstServiceStartForBoot(mockPlatform)
	require.NoError(t, err)
	assert.False(t, first)

	bootID = "boot-2"
	first, err = isFirstServiceStartForBoot(mockPlatform)
	require.NoError(t, err)
	assert.True(t, first)
}

func TestRunConfiguredStartupHooksRunsOnServiceStart(t *testing.T) {
	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML(`[launchers]
on_service_start = "**input.keyboard:{f2}"
`))

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("mock-platform")
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false)
	mockPlatform.On("KeyboardPress", "{f2}").Return(nil).Once()

	mockUserDB := &testhelpers.MockUserDBI{}
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil)
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	svc := &ServiceContext{
		Platform:      mockPlatform,
		Config:        cfg,
		State:         st,
		DB:            &database.Database{UserDB: mockUserDB},
		PlaylistQueue: make(chan *playlists.Playlist, 1),
	}

	runConfiguredStartupHooks(svc)

	mockPlatform.AssertExpectations(t)
	mockUserDB.AssertExpectations(t)
}

func TestRunConfiguredStartupHooksRunsOnBootStartOnlyOncePerBoot(t *testing.T) {
	oldDetectSystemBootID := detectSystemBootID
	defer func() { detectSystemBootID = oldDetectSystemBootID }()
	detectSystemBootID = func() (string, error) {
		return "test-boot", nil
	}

	testRoot := t.TempDir()
	cfg, err := testhelpers.NewTestConfig(nil, filepath.Join(testRoot, "config"))
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML(`[launchers]
on_boot_start = "**input.keyboard:{f2}"
`))

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("mock-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: filepath.Join(testRoot, "data")})
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false)
	mockPlatform.On("KeyboardPress", "{f2}").Return(nil).Once()

	mockUserDB := &testhelpers.MockUserDBI{}
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil).Once()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	svc := &ServiceContext{
		Platform:      mockPlatform,
		Config:        cfg,
		State:         st,
		DB:            &database.Database{UserDB: mockUserDB},
		PlaylistQueue: make(chan *playlists.Playlist, 1),
	}

	runConfiguredStartupHooks(svc)
	runConfiguredStartupHooks(svc)

	mockPlatform.AssertExpectations(t)
	mockUserDB.AssertExpectations(t)
}
