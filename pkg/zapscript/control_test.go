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

package zapscript

import (
	"context"
	"testing"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunControlScript_SingleCommand(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")
	mockPlatform.On("KeyboardPress", "{f2}").Return(nil)

	cfg := &config.Instance{}
	exprEnv := gozapscript.ArgExprEnv{Platform: "test"}

	err := RunControlScript(context.Background(), mockPlatform, cfg, nil, "**input.keyboard:{f2}", &exprEnv)
	require.NoError(t, err)
	mockPlatform.AssertCalled(t, "KeyboardPress", "{f2}")
}

func TestRunControlScript_MultiCommand(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")
	mockPlatform.On("KeyboardPress", "{f2}").Return(nil)
	mockPlatform.On("KeyboardPress", "{f5}").Return(nil)

	cfg := &config.Instance{}
	exprEnv := gozapscript.ArgExprEnv{Platform: "test"}

	err := RunControlScript(context.Background(), mockPlatform, cfg, nil, "**input.keyboard:{f2}||**input.keyboard:{f5}", &exprEnv)
	require.NoError(t, err)
	mockPlatform.AssertCalled(t, "KeyboardPress", "{f2}")
	mockPlatform.AssertCalled(t, "KeyboardPress", "{f5}")
}

func TestRunControlScript_RejectsLaunchCommands(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	err := RunControlScript(context.Background(), mockPlatform, &config.Instance{}, nil, "**launch:/path/to/game", nil)
	require.ErrorIs(t, err, ErrControlCommandNotAllowed)
	assert.Contains(t, err.Error(), "launch")
}

func TestRunControlScript_RejectsPlaylistCommands(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	err := RunControlScript(context.Background(), mockPlatform, &config.Instance{}, nil, "**playlist.play", nil)
	require.ErrorIs(t, err, ErrControlCommandNotAllowed)
	assert.Contains(t, err.Error(), "playlist.play")
}

func TestRunControlScript_EmptyScript(t *testing.T) {
	t.Parallel()

	err := RunControlScript(context.Background(), nil, &config.Instance{}, nil, "", nil)
	require.Error(t, err)
}

func TestRunControlScript_InvalidSyntax(t *testing.T) {
	t.Parallel()

	err := RunControlScript(context.Background(), nil, &config.Instance{}, nil, "**", nil)
	require.Error(t, err)
}

func TestRunControlScript_RejectsBeforeExecuting(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	// Valid command first, then a forbidden launch command.
	// The valid command must NOT execute.
	err := RunControlScript(context.Background(), mockPlatform, &config.Instance{}, nil, "**input.keyboard:{f2}||**launch:/path/to/game", nil)
	require.ErrorIs(t, err, ErrControlCommandNotAllowed)
	mockPlatform.AssertNotCalled(t, "KeyboardPress", "{f2}")
}

func TestRunControlScript_RejectsPlaylistInMultiCommand(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	err := RunControlScript(context.Background(), mockPlatform, &config.Instance{}, nil, "**input.keyboard:{f2}||**playlist.play", nil)
	require.ErrorIs(t, err, ErrControlCommandNotAllowed)
	mockPlatform.AssertNotCalled(t, "KeyboardPress", "{f2}")
}

func TestRunControlScript_RejectsControlCommand(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	err := RunControlScript(context.Background(), mockPlatform, &config.Instance{}, nil, "**control:toggle_pause", nil)
	require.ErrorIs(t, err, ErrControlCommandNotAllowed)
	assert.Contains(t, err.Error(), "control")
}

func TestRunControlScript_StopsWhenContextCanceled(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := RunControlScript(ctx, mockPlatform, &config.Instance{}, nil, "**input.keyboard:{f2}", nil)
	require.ErrorIs(t, err, context.Canceled)
	mockPlatform.AssertNotCalled(t, "KeyboardPress", "{f2}")
}

func TestIsControlCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmdName string
		want    bool
	}{
		{name: "control", cmdName: gozapscript.ZapScriptCmdControl, want: true},
		{name: "launch is not control", cmdName: "launch", want: false},
		{name: "stop is not control", cmdName: "stop", want: false},
		{name: "empty string", cmdName: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsControlCommand(tt.cmdName))
		})
	}
}

func TestIsPlaylistCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmdName string
		want    bool
	}{
		{name: "playlist.play", cmdName: gozapscript.ZapScriptCmdPlaylistPlay, want: true},
		{name: "playlist.stop", cmdName: gozapscript.ZapScriptCmdPlaylistStop, want: true},
		{name: "playlist.next", cmdName: gozapscript.ZapScriptCmdPlaylistNext, want: true},
		{name: "playlist.previous", cmdName: gozapscript.ZapScriptCmdPlaylistPrevious, want: true},
		{name: "playlist.goto", cmdName: gozapscript.ZapScriptCmdPlaylistGoto, want: true},
		{name: "playlist.pause", cmdName: gozapscript.ZapScriptCmdPlaylistPause, want: true},
		{name: "playlist.load", cmdName: gozapscript.ZapScriptCmdPlaylistLoad, want: true},
		{name: "playlist.open", cmdName: gozapscript.ZapScriptCmdPlaylistOpen, want: true},
		{name: "launch is not playlist", cmdName: "launch", want: false},
		{name: "control is not playlist", cmdName: gozapscript.ZapScriptCmdControl, want: false},
		{name: "input.keyboard is not playlist", cmdName: "input.keyboard", want: false},
		{name: "empty string", cmdName: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsPlaylistCommand(tt.cmdName))
		})
	}
}
