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
	"testing"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func setupHookTest(t *testing.T) *ServiceContext {
	t.Helper()

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false)

	mockUserDB := &testhelpers.MockUserDBI{}
	mockUserDB.On("GetEnabledMappings").Return(nil, nil)

	mockMediaDB := &testhelpers.MockMediaDBI{}

	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	return &ServiceContext{
		Platform: mockPlatform,
		Config:   cfg,
		State:    st,
		DB: &database.Database{
			UserDB:  mockUserDB,
			MediaDB: mockMediaDB,
		},
		LaunchSoftwareQueue: make(chan *tokens.Token, 1),
		PlaylistQueue:       make(chan *playlists.Playlist, 1),
	}
}

func TestRunHook_BasicExecution(t *testing.T) {
	t.Parallel()

	svc := setupHookTest(t)

	err := runHook(svc, "test_hook", "**echo:test message", nil, nil)
	assert.NoError(t, err, "echo hook should succeed")
}

func TestRunHook_WithScannedContext(t *testing.T) {
	t.Parallel()

	svc := setupHookTest(t)

	scanned := &gozapscript.ExprEnvScanned{
		ID:    "test-token-id",
		Value: "**launch:/games/sonic.bin",
		Data:  "raw-ndef-data",
	}

	err := runHook(svc, "on_scan", "**echo:scanned", scanned, nil)
	assert.NoError(t, err, "hook with scanned context should succeed")
}

func TestRunHook_WithLaunchingContext(t *testing.T) {
	t.Parallel()

	svc := setupHookTest(t)

	launching := &gozapscript.ExprEnvLaunching{
		Path:       "/games/genesis/sonic.bin",
		SystemID:   "genesis",
		LauncherID: "retroarch",
	}

	err := runHook(svc, "before_media_start", "**echo:launching", nil, launching)
	assert.NoError(t, err, "hook with launching context should succeed")
}

func TestRunHook_AlwaysInHookContext(t *testing.T) {
	t.Parallel()

	svc := setupHookTest(t)

	// Hooks always run in hook context (inHookContext=true), which means
	// before_media_start hooks inside hooks are blocked (no recursion).
	scanned := &gozapscript.ExprEnvScanned{
		ID: "test-id",
	}

	err := runHook(svc, "test_hook", "**echo:test", scanned, nil)
	assert.NoError(t, err)
}

func TestRunHook_PreservesScannedAndLaunching(t *testing.T) {
	t.Parallel()

	svc := setupHookTest(t)

	scanned := &gozapscript.ExprEnvScanned{
		ID:    "scanned-id",
		Value: "scanned-value",
		Data:  "scanned-data",
	}
	launching := &gozapscript.ExprEnvLaunching{
		Path:       "/path/to/game",
		SystemID:   "snes",
		LauncherID: "mister",
	}

	err := runHook(svc, "combined_hook", "**echo:both contexts", scanned, launching)
	assert.NoError(t, err, "hook with both contexts should succeed")
}

func TestRunHook_InvalidScript(t *testing.T) {
	t.Parallel()

	svc := setupHookTest(t)

	err := runHook(svc, "test_hook", "**unknown_command:arg", nil, nil)
	assert.Error(t, err, "unknown command should return error")
}

func TestRunHook_EmptyScript(t *testing.T) {
	t.Parallel()

	svc := setupHookTest(t)

	err := runHook(svc, "test_hook", "", nil, nil)
	require.Error(t, err, "empty script should return error")
	assert.Contains(t, err.Error(), "script is empty")
}

func TestRunHook_NilContextParams(t *testing.T) {
	t.Parallel()

	svc := setupHookTest(t)

	err := runHook(svc, "test_hook", "**echo:nil opts test", nil, nil)
	assert.NoError(t, err, "nil scanned/launching should work")
}
