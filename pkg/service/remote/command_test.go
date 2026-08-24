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

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandRejectsMisterScriptOnOtherPlatforms(t *testing.T) {
	linux := mocks.NewMockPlatform()
	linux.On("ID").Return("linux")
	m := &manager{deps: Deps{Platform: linux}}
	unsupported := m.executeCommand(
		context.Background(), "mister.script", json.RawMessage(`{"value":"update_all.sh"}`))
	assert.Equal(t, "unsupported", unsupported.ErrorCode)
}

// TestCommandMapsDisabledKillSwitchToDisabledCode verifies remote launches
// respect settings.runZapScript like every other source: the kill switch
// has no bypass, and its sentinel error must not be reported as a generic
// execution_failed.
func TestCommandMapsDisabledKillSwitchToDisabledCode(t *testing.T) {
	linux := mocks.NewMockPlatform()
	linux.On("ID").Return("linux")
	m := &manager{deps: Deps{
		Platform: linux,
		RunZapScript: func(
			context.Context, tokens.Token, playlists.PlaylistController,
			*gozapscript.ArgExprEnv, bool,
		) error {
			return state.ErrRunZapScriptDisabled
		},
	}}

	result := m.executeCommand(context.Background(), "launch", json.RawMessage(`{"value":"Genesis/Sonic.md"}`))
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, "disabled", result.ErrorCode)
}

func TestCommandExecuteSucceeds(t *testing.T) {
	m := &manager{deps: Deps{
		RunZapScript: func(
			context.Context, tokens.Token, playlists.PlaylistController,
			*gozapscript.ArgExprEnv, bool,
		) error {
			return nil
		},
	}}

	result := m.executeCommand(context.Background(), "launch", json.RawMessage(`{"value":"Genesis/Sonic.md"}`))
	assert.Equal(t, "succeeded", result.Status)
}

// TestCommandClassifiesRunZapScriptErrors pins how executeCommand maps every
// RunZapScript error it recognizes to a stable remote result code; anything
// unrecognized falls back to a generic execution_failed rather than leaking
// internal error text over the wire (classifyHandlerError in dispatch.go
// documents the same "never return handler text" rule for method-backed
// operations).
func TestCommandClassifiesRunZapScriptErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err           error
		name          string
		wantStatus    string
		wantErrorCode string
	}{
		{name: "launch in progress is busy", err: state.ErrLaunchInProgress, wantStatus: "busy"},
		{
			name: "file not found", err: zapscript.ErrFileNotFound,
			wantStatus: "failed", wantErrorCode: "media_not_found",
		},
		{
			name: "unrecognized error is execution_failed", err: errors.New("launcher crashed"),
			wantStatus: "failed", wantErrorCode: "execution_failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &manager{deps: Deps{
				RunZapScript: func(
					context.Context, tokens.Token, playlists.PlaylistController,
					*gozapscript.ArgExprEnv, bool,
				) error {
					return tt.err
				},
			}}

			result := m.executeCommand(context.Background(), "launch", json.RawMessage(`{"value":"Genesis/Sonic.md"}`))
			assert.Equal(t, tt.wantStatus, result.Status)
			assert.Equal(t, tt.wantErrorCode, result.ErrorCode)
		})
	}
}

func TestCommandExecuteRejectsMalformedParams(t *testing.T) {
	t.Parallel()
	m := &manager{}
	result := m.executeCommand(context.Background(), "launch", json.RawMessage(`not json`))
	assert.Equal(t, "bad_params", result.ErrorCode)
}

// TestBuildStructuralCommandRejectsEmptyArgument pins that a value which is
// only a query string (e.g. "?launcher=x") is rejected rather than building
// a command with an empty launch target.
func TestBuildStructuralCommandRejectsEmptyArgument(t *testing.T) {
	t.Parallel()
	_, err := buildStructuralCommand("launch", "?launcher=x")
	require.Error(t, err)
}

// TestBuildStructuralCommandRejectsMalformedAdvancedArgs pins that a
// duplicated or empty advanced-argument key is rejected outright rather than
// silently taking the first or last value.
func TestBuildStructuralCommandRejectsMalformedAdvancedArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{name: "duplicated key", value: "Genesis/Sonic.md?launcher=a&launcher=b"},
		{name: "empty key", value: "Genesis/Sonic.md?=x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := buildStructuralCommand("launch", tt.value)
			require.Error(t, err)
		})
	}
}

// TestCommandRejectsURLValueForAllStructuralVerbs pins that none of the
// three remote structural verbs accept a URL-shaped value: a system ID,
// media path, or script name is never legitimately one. This is
// defense-in-depth alongside RunCommand skipping ZapLink resolution
// entirely for remote-sourced tokens (pkg/zapscript/commands_test.go); it
// gives a clean bad_params error instead of a downstream failure, and used
// to only apply to "launch".
func TestCommandRejectsURLValueForAllStructuralVerbs(t *testing.T) {
	t.Parallel()
	for _, operationType := range []string{"launch", "launch.system", "mister.script"} {
		m := &manager{}
		result := m.executeCommand(
			context.Background(), operationType, json.RawMessage(`{"value":"https://example.com/game.zip"}`))
		assert.Equal(t, "bad_params", result.ErrorCode, operationType)
	}
}

func TestBuildStructuralCommandSeparatesAdvancedArgs(t *testing.T) {
	command, err := buildStructuralCommand("launch", "Genesis/Sonic.md?launcher=genesis-alt")
	require.NoError(t, err)
	assert.Equal(t, []string{"Genesis/Sonic.md"}, command.Args)
	assert.Equal(t, "genesis-alt", command.AdvArgs.Get(gozapscript.KeyLauncher))
}

// TestBuildStructuralCommandLaunchSystemAcceptsLauncherArg pins that a
// remote launch.system operation can request a specific launcher (e.g. a
// MiSTer alt core) the same way launch does: the query-string parsing in
// buildStructuralCommand is generic across operation names, and cmdSystem
// reads the same launcher adv arg to pick among a system's launchers.
func TestBuildStructuralCommandLaunchSystemAcceptsLauncherArg(t *testing.T) {
	command, err := buildStructuralCommand("launch.system", "SNES?launcher=SuperNT")
	require.NoError(t, err)
	assert.Equal(t, []string{"SNES"}, command.Args)
	assert.Equal(t, "SuperNT", command.AdvArgs.Get(gozapscript.KeyLauncher))
}

func TestValidCommandValueRejectsZapScriptInjection(t *testing.T) {
	for _, value := range []string{"game||**stop", "**launch:game", "game\n**stop", ""} {
		assert.False(t, validCommandValue(value), value)
	}
	assert.True(t, validCommandValue("Genesis/Sonic.md?launcher=default"))
	assert.True(t, isURLLaunch("https://example.com/game.zip?launcher=x"))
	assert.False(t, isURLLaunch("Genesis/Sonic.md?launcher=x"))
}

func FuzzBuildStructuralCommand(f *testing.F) {
	f.Add("Genesis/Sonic.md?launcher=default")
	f.Add("game||**stop")
	f.Add("")
	f.Fuzz(func(t *testing.T, value string) {
		if !validCommandValue(value) {
			return
		}
		command, err := buildStructuralCommand("launch", value)
		if err != nil {
			return
		}
		require.Equal(t, "launch", command.Name)
		require.Len(t, command.Args, 1)
		assert.NotContains(t, command.Args[0], "**")
		assert.NotContains(t, command.Args[0], "||")
	})
}
