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
	"errors"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newControlExprEnv(launcherID string) *zapscript.ArgExprEnv {
	return &zapscript.ArgExprEnv{
		MediaPlaying: true,
		ActiveMedia: zapscript.ExprEnvActiveMedia{
			LauncherID: launcherID,
		},
	}
}

func TestCmdControl_Success(t *testing.T) {
	t.Parallel()

	var calledWith platforms.ControlParams
	controlFunc := func(_ context.Context, _ *config.Instance, params platforms.ControlParams) error {
		calledWith = params
		return nil
	}

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", (*config.Instance)(nil)).Return([]platforms.Launcher{
		{
			ID: "test-launcher",
			Controls: map[string]platforms.Control{
				"toggle_pause": {Func: controlFunc},
			},
		},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"toggle_pause"},
		},
		ExprEnv: newControlExprEnv("test-launcher"),
	}

	result, err := cmdControl(mockPlatform, env)
	require.NoError(t, err)
	assert.Equal(t, platforms.CmdResult{}, result)
	assert.Nil(t, calledWith.Args)
}

func TestCmdControl_SuccessWithScript(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")
	mockPlatform.On("KeyboardPress", "{f2}").Return(nil)
	mockPlatform.On("Launchers", (*config.Instance)(nil)).Return([]platforms.Launcher{
		{
			ID: "test-launcher",
			Controls: map[string]platforms.Control{
				"save_state": {Script: "**input.keyboard:{f2}"},
			},
		},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"save_state"},
		},
		ExprEnv: newControlExprEnv("test-launcher"),
	}

	result, err := cmdControl(mockPlatform, env)
	require.NoError(t, err)
	assert.Equal(t, platforms.CmdResult{}, result)
	mockPlatform.AssertCalled(t, "KeyboardPress", "{f2}")
}

func TestCmdControl_ScriptUsesServiceContext(t *testing.T) {
	t.Parallel()

	serviceCtx, cancelService := context.WithCancel(context.Background())
	cancelService()
	launcherCtx := context.Background()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")
	mockPlatform.On("Launchers", (*config.Instance)(nil)).Return([]platforms.Launcher{
		{
			ID: "test-launcher",
			Controls: map[string]platforms.Control{
				"save_state": {Script: "**input.keyboard:{f2}"},
			},
		},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"save_state"},
		},
		ExprEnv:     newControlExprEnv("test-launcher"),
		ServiceCtx:  serviceCtx,
		LauncherCtx: launcherCtx,
	}

	_, err := cmdControl(mockPlatform, env)
	require.ErrorIs(t, err, context.Canceled)
	mockPlatform.AssertNotCalled(t, "KeyboardPress", "{f2}")
}

func TestCmdControl_ScriptExprEnvPropagation(t *testing.T) {
	// Skipped: input macro commands (input.keyboard, input.gamepad) don't support
	// expression evaluation in go-zapscript's parseInputMacroArg.
	// See: https://github.com/ZaparooProject/go-zapscript/issues/2
	t.Skip("blocked by go-zapscript#2: input macro commands don't support expressions")
}

func TestCmdControl_NoArgs(t *testing.T) {
	t.Parallel()

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{},
		},
	}

	_, err := cmdControl(nil, env)
	require.ErrorIs(t, err, ErrArgCount)
}

func TestCmdControl_EmptyAction(t *testing.T) {
	t.Parallel()

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{""},
		},
	}

	_, err := cmdControl(nil, env)
	require.ErrorIs(t, err, ErrRequiredArgs)
}

func TestCmdControl_NoActiveMedia(t *testing.T) {
	t.Parallel()

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"toggle_pause"},
		},
		ExprEnv: &zapscript.ArgExprEnv{
			MediaPlaying: false,
		},
	}

	_, err := cmdControl(nil, env)
	require.ErrorIs(t, err, ErrNoActiveMedia)
}

func TestCmdControl_NilExprEnv(t *testing.T) {
	t.Parallel()

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"toggle_pause"},
		},
	}

	_, err := cmdControl(nil, env)
	require.ErrorIs(t, err, ErrNoActiveMedia)
}

func TestCmdControl_EmptyLauncherID(t *testing.T) {
	t.Parallel()

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"toggle_pause"},
		},
		ExprEnv: newControlExprEnv(""),
	}

	_, err := cmdControl(nil, env)
	require.ErrorIs(t, err, ErrNoLauncher)
}

func TestCmdControl_LauncherNotFound(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", (*config.Instance)(nil)).Return([]platforms.Launcher{
		{ID: "other-launcher"},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"toggle_pause"},
		},
		ExprEnv: newControlExprEnv("missing-launcher"),
	}

	_, err := cmdControl(mockPlatform, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "launcher not found")
}

func TestCmdControl_NoControls(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", (*config.Instance)(nil)).Return([]platforms.Launcher{
		{ID: "test-launcher"},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"toggle_pause"},
		},
		ExprEnv: newControlExprEnv("test-launcher"),
	}

	_, err := cmdControl(mockPlatform, env)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoControlCapabilities)
	assert.Contains(t, err.Error(), "no control capabilities")
}

func TestCmdControl_UnknownAction(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", (*config.Instance)(nil)).Return([]platforms.Launcher{
		{
			ID: "test-launcher",
			Controls: map[string]platforms.Control{
				"toggle_pause": {Func: func(context.Context, *config.Instance, platforms.ControlParams) error {
					return nil
				}},
			},
		},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"nonexistent_action"},
		},
		ExprEnv: newControlExprEnv("test-launcher"),
	}

	_, err := cmdControl(mockPlatform, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported by launcher")
}

func TestCmdControl_NoImplementation(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", (*config.Instance)(nil)).Return([]platforms.Launcher{
		{
			ID: "test-launcher",
			Controls: map[string]platforms.Control{
				"toggle_pause": {}, // neither Func nor Script
			},
		},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"toggle_pause"},
		},
		ExprEnv: newControlExprEnv("test-launcher"),
	}

	_, err := cmdControl(mockPlatform, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no implementation")
}

func TestCmdControl_AdvArgsPassThrough(t *testing.T) {
	t.Parallel()

	var calledWith platforms.ControlParams
	controlFunc := func(_ context.Context, _ *config.Instance, params platforms.ControlParams) error {
		calledWith = params
		return nil
	}

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", (*config.Instance)(nil)).Return([]platforms.Launcher{
		{
			ID: "test-launcher",
			Controls: map[string]platforms.Control{
				"save_state": {Func: controlFunc},
			},
		},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"save_state"},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"slot": "3",
				"name": "quicksave",
			}),
		},
		ExprEnv: newControlExprEnv("test-launcher"),
	}

	_, err := cmdControl(mockPlatform, env)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"slot": "3", "name": "quicksave"}, calledWith.Args)
}

func TestCmdControl_WhenKeyStripped(t *testing.T) {
	t.Parallel()

	var calledWith platforms.ControlParams
	controlFunc := func(_ context.Context, _ *config.Instance, params platforms.ControlParams) error {
		calledWith = params
		return nil
	}

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", (*config.Instance)(nil)).Return([]platforms.Launcher{
		{
			ID: "test-launcher",
			Controls: map[string]platforms.Control{
				"save_state": {Func: controlFunc},
			},
		},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"save_state"},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"when": "true",
				"slot": "1",
			}),
		},
		ExprEnv: newControlExprEnv("test-launcher"),
	}

	_, err := cmdControl(mockPlatform, env)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"slot": "1"}, calledWith.Args)
	assert.NotContains(t, calledWith.Args, "when")
}

func TestCmdControl_WhenOnlyAdvArg(t *testing.T) {
	t.Parallel()

	var calledWith platforms.ControlParams
	controlFunc := func(_ context.Context, _ *config.Instance, params platforms.ControlParams) error {
		calledWith = params
		return nil
	}

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", (*config.Instance)(nil)).Return([]platforms.Launcher{
		{
			ID: "test-launcher",
			Controls: map[string]platforms.Control{
				"toggle_pause": {Func: controlFunc},
			},
		},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"toggle_pause"},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"when": "true",
			}),
		},
		ExprEnv: newControlExprEnv("test-launcher"),
	}

	_, err := cmdControl(mockPlatform, env)
	require.NoError(t, err)
	assert.Nil(t, calledWith.Args, "args should be nil when only 'when' is present")
}

func TestCmdControl_FuncError(t *testing.T) {
	t.Parallel()

	controlFunc := func(_ context.Context, _ *config.Instance, _ platforms.ControlParams) error {
		return errors.New("kodi connection refused")
	}

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", (*config.Instance)(nil)).Return([]platforms.Launcher{
		{
			ID: "test-launcher",
			Controls: map[string]platforms.Control{
				"toggle_pause": {Func: controlFunc},
			},
		},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"toggle_pause"},
		},
		ExprEnv: newControlExprEnv("test-launcher"),
	}

	_, err := cmdControl(mockPlatform, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kodi connection refused")
	assert.Contains(t, err.Error(), "control action")
}
