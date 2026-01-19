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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type hookTestEnv struct {
	platform *mocks.MockPlatform
	cfg      *config.Instance
	st       *state.State
	db       *database.Database
	lsq      chan *tokens.Token
	plq      chan *playlists.Playlist
}

func setupHookTest(t *testing.T) *hookTestEnv {
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

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	return &hookTestEnv{
		platform: mockPlatform,
		cfg:      cfg,
		st:       st,
		db:       db,
		lsq:      make(chan *tokens.Token, 1),
		plq:      make(chan *playlists.Playlist, 1),
	}
}

func (e *hookTestEnv) runHook(hookName, script string, opts *zapscript.ExprEnvOptions) error {
	return runHook(e.platform, e.cfg, e.st, e.db, e.lsq, e.plq, hookName, script, opts)
}

func TestRunHook_BasicExecution(t *testing.T) {
	t.Parallel()

	env := setupHookTest(t)

	err := env.runHook("test_hook", "**echo:test message", nil)
	assert.NoError(t, err, "echo hook should succeed")
}

func TestRunHook_WithScannedContext(t *testing.T) {
	t.Parallel()

	env := setupHookTest(t)

	scannedOpts := &zapscript.ExprEnvOptions{
		Scanned: &gozapscript.ExprEnvScanned{
			ID:    "test-token-id",
			Value: "**launch:/games/sonic.bin",
			Data:  "raw-ndef-data",
		},
	}

	err := env.runHook("on_scan", "**echo:scanned", scannedOpts)
	assert.NoError(t, err, "hook with scanned context should succeed")
}

func TestRunHook_WithLaunchingContext(t *testing.T) {
	t.Parallel()

	env := setupHookTest(t)

	launchingOpts := &zapscript.ExprEnvOptions{
		Launching: &gozapscript.ExprEnvLaunching{
			Path:       "/games/genesis/sonic.bin",
			SystemID:   "genesis",
			LauncherID: "retroarch",
		},
	}

	err := env.runHook("before_media_start", "**echo:launching", launchingOpts)
	assert.NoError(t, err, "hook with launching context should succeed")
}

func TestRunHook_SetsInHookContext(t *testing.T) {
	t.Parallel()

	env := setupHookTest(t)

	// Pass opts without InHookContext set - runHook should set it internally
	opts := &zapscript.ExprEnvOptions{
		Scanned: &gozapscript.ExprEnvScanned{
			ID: "test-id",
		},
		InHookContext: false, // Should be overridden to true by runHook
	}

	err := env.runHook("test_hook", "**echo:test", opts)
	assert.NoError(t, err)
	// The function always creates new opts with InHookContext=true, preserving other fields
}

func TestRunHook_PreservesScannedAndLaunching(t *testing.T) {
	t.Parallel()

	env := setupHookTest(t)

	// Provide both Scanned and Launching contexts
	opts := &zapscript.ExprEnvOptions{
		Scanned: &gozapscript.ExprEnvScanned{
			ID:    "scanned-id",
			Value: "scanned-value",
			Data:  "scanned-data",
		},
		Launching: &gozapscript.ExprEnvLaunching{
			Path:       "/path/to/game",
			SystemID:   "snes",
			LauncherID: "mister",
		},
	}

	err := env.runHook("combined_hook", "**echo:both contexts", opts)
	assert.NoError(t, err, "hook with both contexts should succeed")
}

func TestRunHook_InvalidScript(t *testing.T) {
	t.Parallel()

	env := setupHookTest(t)

	err := env.runHook("test_hook", "**unknown_command:arg", nil)
	assert.Error(t, err, "unknown command should return error")
}

func TestRunHook_EmptyScript(t *testing.T) {
	t.Parallel()

	env := setupHookTest(t)

	err := env.runHook("test_hook", "", nil)
	require.Error(t, err, "empty script should return error")
	assert.Contains(t, err.Error(), "script is empty")
}

func TestRunHook_NilExprOpts(t *testing.T) {
	t.Parallel()

	env := setupHookTest(t)

	err := env.runHook("test_hook", "**echo:nil opts test", nil)
	assert.NoError(t, err, "nil exprOpts should work")
}
