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
	"fmt"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	apimodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newControlLauncherCache seeds a launcher cache the way the service does at
// startup. cmdControl resolves through the cache rather than the platform's own
// launcher list, so tests must not rely on Platform.Launchers.
func newControlLauncherCache(launchers []platforms.Launcher) *helpers.LauncherCache {
	cache := &helpers.LauncherCache{}
	cache.InitializeFromSlice(launchers)
	return cache
}

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
	launcherCache := newControlLauncherCache([]platforms.Launcher{
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
		ExprEnv:       newControlExprEnv("test-launcher"),
		LauncherCache: launcherCache,
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
	launcherCache := newControlLauncherCache([]platforms.Launcher{
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
		ExprEnv:       newControlExprEnv("test-launcher"),
		LauncherCache: launcherCache,
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
	launcherCache := newControlLauncherCache([]platforms.Launcher{
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
		ExprEnv:       newControlExprEnv("test-launcher"),
		LauncherCache: launcherCache,
		ServiceCtx:    serviceCtx,
		LauncherCtx:   launcherCtx,
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
	launcherCache := newControlLauncherCache([]platforms.Launcher{
		{ID: "other-launcher"},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"toggle_pause"},
		},
		ExprEnv:       newControlExprEnv("missing-launcher"),
		LauncherCache: launcherCache,
	}

	_, err := cmdControl(mockPlatform, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "launcher not found")
}

func TestCmdControl_NoControls(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	launcherCache := newControlLauncherCache([]platforms.Launcher{
		{ID: "test-launcher"},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"toggle_pause"},
		},
		ExprEnv:       newControlExprEnv("test-launcher"),
		LauncherCache: launcherCache,
	}

	_, err := cmdControl(mockPlatform, env)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoControlCapabilities)
	assert.Contains(t, err.Error(), "no control capabilities")
}

func TestCmdControl_UnknownAction(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	launcherCache := newControlLauncherCache([]platforms.Launcher{
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
		ExprEnv:       newControlExprEnv("test-launcher"),
		LauncherCache: launcherCache,
	}

	_, err := cmdControl(mockPlatform, env)
	require.ErrorIs(t, err, ErrUnsupportedControlAction)
	require.EqualError(t, err, `action "nonexistent_action" not supported by launcher test-launcher`)
}

func TestCmdControl_NoImplementation(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	launcherCache := newControlLauncherCache([]platforms.Launcher{
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
		ExprEnv:       newControlExprEnv("test-launcher"),
		LauncherCache: launcherCache,
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
	launcherCache := newControlLauncherCache([]platforms.Launcher{
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
		ExprEnv:       newControlExprEnv("test-launcher"),
		LauncherCache: launcherCache,
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
	launcherCache := newControlLauncherCache([]platforms.Launcher{
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
		ExprEnv:       newControlExprEnv("test-launcher"),
		LauncherCache: launcherCache,
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
	launcherCache := newControlLauncherCache([]platforms.Launcher{
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
		ExprEnv:       newControlExprEnv("test-launcher"),
		LauncherCache: launcherCache,
	}

	_, err := cmdControl(mockPlatform, env)
	require.NoError(t, err)
	assert.Nil(t, calledWith.Args, "args should be nil when only 'when' is present")
}

func TestCmdControl_WaitsForMediaReadyBeforeControl(t *testing.T) {
	t.Parallel()

	waited := false
	controlFunc := func(_ context.Context, _ *config.Instance, _ platforms.ControlParams) error {
		require.True(t, waited)
		return nil
	}

	mockPlatform := mocks.NewMockPlatform()
	launcherCache := newControlLauncherCache([]platforms.Launcher{
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
		ExprEnv:       newControlExprEnv("test-launcher"),
		LauncherCache: launcherCache,
		LauncherCtx:   context.Background(),
		WaitForMediaReady: func(ctx context.Context) error {
			require.NotNil(t, ctx)
			waited = true
			return nil
		},
	}

	_, err := cmdControl(mockPlatform, env)
	require.NoError(t, err)
	assert.True(t, waited)
}

func TestCmdControl_WaitForMediaReadyErrorSkipsControl(t *testing.T) {
	t.Parallel()

	controlCalled := false
	controlFunc := func(context.Context, *config.Instance, platforms.ControlParams) error {
		controlCalled = true
		return nil
	}

	mockPlatform := mocks.NewMockPlatform()
	launcherCache := newControlLauncherCache([]platforms.Launcher{
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
		ExprEnv:       newControlExprEnv("test-launcher"),
		LauncherCache: launcherCache,
		WaitForMediaReady: func(context.Context) error {
			return errors.New("not ready")
		},
	}

	_, err := cmdControl(mockPlatform, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wait for media ready")
	assert.False(t, controlCalled)
}

func TestCmdControl_FuncError(t *testing.T) {
	t.Parallel()

	controlFunc := func(_ context.Context, _ *config.Instance, _ platforms.ControlParams) error {
		return errors.New("kodi connection refused")
	}

	mockPlatform := mocks.NewMockPlatform()
	launcherCache := newControlLauncherCache([]platforms.Launcher{
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
		ExprEnv:       newControlExprEnv("test-launcher"),
		LauncherCache: launcherCache,
	}

	_, err := cmdControl(mockPlatform, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kodi connection refused")
	assert.Contains(t, err.Error(), "control action")
}

// TestCmdControl_LauncherOnlyInCache pins issue #1386: the native-audio launcher is
// built at service startup and only ever lives in the launcher cache, never in
// Platform.Launchers. Resolving from the platform list fails every control action
// for it with "launcher not found".
func TestCmdControl_LauncherOnlyInCache(t *testing.T) {
	t.Parallel()

	called := false
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", (*config.Instance)(nil)).Return([]platforms.Launcher{
		{ID: "some-platform-launcher"},
	})

	launcherCache := newControlLauncherCache([]platforms.Launcher{
		{ID: "some-platform-launcher"},
		{
			ID: platforms.NativeAudioLauncherID,
			Controls: map[string]platforms.Control{
				platforms.ControlPause: {
					Func: func(context.Context, *config.Instance, platforms.ControlParams) error {
						called = true
						return nil
					},
				},
			},
		},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{platforms.ControlPause},
		},
		ExprEnv:       newControlExprEnv(platforms.NativeAudioLauncherID),
		LauncherCache: launcherCache,
	}

	_, err := cmdControl(mockPlatform, env)
	require.NoError(t, err)
	assert.True(t, called, "control must run for a launcher only present in the cache")
}

// TestCmdControl_LauncherIDCaseInsensitive matches the cache's own lookup rules and
// the API control path, which both compare launcher IDs case-insensitively.
func TestCmdControl_LauncherIDCaseInsensitive(t *testing.T) {
	t.Parallel()

	called := false
	mockPlatform := mocks.NewMockPlatform()
	launcherCache := newControlLauncherCache([]platforms.Launcher{
		{
			ID: "Generic",
			Controls: map[string]platforms.Control{
				platforms.ControlPause: {
					Func: func(context.Context, *config.Instance, platforms.ControlParams) error {
						called = true
						return nil
					},
				},
			},
		},
	})

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{platforms.ControlPause},
		},
		ExprEnv:       newControlExprEnv("generic"),
		LauncherCache: launcherCache,
	}

	_, err := cmdControl(mockPlatform, env)
	require.NoError(t, err)
	assert.True(t, called)
}

func TestCmdControl_NilLauncherCache(t *testing.T) {
	t.Parallel()

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "control",
			Args: []string{"toggle_pause"},
		},
		ExprEnv: newControlExprEnv("test-launcher"),
	}

	_, err := cmdControl(mocks.NewMockPlatform(), env)
	require.ErrorIs(t, err, ErrNoLauncherCache)
}

// slotControlRig records which launcher a control reached and with what
// params: a game launcher in the primary slot and native audio in the
// background slot.
type slotControlRig struct {
	game      *platforms.ControlParams
	audio     *platforms.ControlParams
	cache     *helpers.LauncherCache
	primary   *zapscript.ArgExprEnv
	bgMedia   *apimodels.ActiveMedia
	gameCalls int
}

func newSlotControlRig(gameRunning bool) *slotControlRig {
	rig := &slotControlRig{
		bgMedia: &apimodels.ActiveMedia{LauncherID: platforms.NativeAudioLauncherID},
		primary: &zapscript.ArgExprEnv{},
	}
	if gameRunning {
		rig.primary = newControlExprEnv("game-launcher")
	}
	gameFunc := func(_ context.Context, _ *config.Instance, params platforms.ControlParams) error {
		rig.gameCalls++
		rig.game = &params
		// The save state slot check the MiSTer and RePlayOS launchers make.
		if _, err := platforms.SlotArg(params, 0, 9); err != nil {
			return fmt.Errorf("save state slot: %w", err)
		}
		return nil
	}
	audioFunc := func(_ context.Context, _ *config.Instance, params platforms.ControlParams) error {
		rig.audio = &params
		return nil
	}
	rig.cache = newControlLauncherCache([]platforms.Launcher{
		{
			ID: "game-launcher",
			Controls: map[string]platforms.Control{
				platforms.ControlPause:     {Func: gameFunc},
				platforms.ControlStop:      {Func: gameFunc},
				platforms.ControlSaveState: {Func: gameFunc},
			},
		},
		{
			ID: platforms.NativeAudioLauncherID,
			Controls: map[string]platforms.Control{
				platforms.ControlPause: {Func: audioFunc},
				platforms.ControlStop:  {Func: audioFunc},
			},
		},
	})
	return rig
}

func (r *slotControlRig) env(action string, advArgs map[string]string) platforms.CmdEnv {
	return platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name:    "control",
			Args:    []string{action},
			AdvArgs: zapscript.NewAdvArgs(advArgs),
		},
		ExprEnv:         r.primary,
		LauncherCache:   r.cache,
		BackgroundMedia: func() *apimodels.ActiveMedia { return r.bgMedia },
	}
}

func TestCmdControl_BackgroundSlotWithoutPrimaryMedia(t *testing.T) {
	t.Parallel()

	rig := newSlotControlRig(false)
	_, err := cmdControl(mocks.NewMockPlatform(), rig.env(platforms.ControlPause, map[string]string{
		"slot": "background",
	}))
	require.NoError(t, err)
	require.NotNil(t, rig.audio)
	assert.Equal(t, map[string]string{"slot": mediaslot.Background}, rig.audio.Args)
}

func TestCmdControl_BackgroundSlotLeavesGameAlone(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"background", "Background", " bg "} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			rig := newSlotControlRig(true)
			_, err := cmdControl(mocks.NewMockPlatform(), rig.env(platforms.ControlPause, map[string]string{
				"slot": raw,
			}))
			require.NoError(t, err)
			require.NotNil(t, rig.audio)
			assert.Equal(t, map[string]string{"slot": mediaslot.Background}, rig.audio.Args)
			assert.Zero(t, rig.gameCalls)
		})
	}
}

func TestCmdControl_BackgroundSlotNoBackgroundMedia(t *testing.T) {
	t.Parallel()

	rig := newSlotControlRig(true)
	rig.bgMedia = nil
	_, err := cmdControl(mocks.NewMockPlatform(), rig.env(platforms.ControlPause, map[string]string{
		"slot": "background",
	}))
	require.ErrorIs(t, err, ErrNoActiveMedia)
	assert.Zero(t, rig.gameCalls)
}

func TestCmdControl_BackgroundSlotWithoutStateHook(t *testing.T) {
	t.Parallel()

	rig := newSlotControlRig(true)
	env := rig.env(platforms.ControlPause, map[string]string{"slot": "background"})
	env.BackgroundMedia = nil
	_, err := cmdControl(mocks.NewMockPlatform(), env)
	require.ErrorIs(t, err, ErrNoActiveMedia)
	assert.Zero(t, rig.gameCalls)
}

func TestCmdControl_SaveStateSlotNumberGoesToPrimary(t *testing.T) {
	t.Parallel()

	rig := newSlotControlRig(true)
	_, err := cmdControl(mocks.NewMockPlatform(), rig.env(platforms.ControlSaveState, map[string]string{
		"slot": "3",
	}))
	require.NoError(t, err)
	require.NotNil(t, rig.game)
	assert.Equal(t, map[string]string{"slot": "3"}, rig.game.Args)
	assert.Nil(t, rig.audio)
}

func TestCmdControl_PrimarySlotNotPassedToGameLauncher(t *testing.T) {
	t.Parallel()

	rig := newSlotControlRig(true)
	_, err := cmdControl(mocks.NewMockPlatform(), rig.env(platforms.ControlSaveState, map[string]string{
		"slot": "primary",
	}))
	require.NoError(t, err)
	require.NotNil(t, rig.game)
	assert.Nil(t, rig.game.Args)
}

func TestCmdControl_PrimarySlotPassedToNativeAudio(t *testing.T) {
	t.Parallel()

	rig := newSlotControlRig(false)
	rig.primary = newControlExprEnv(platforms.NativeAudioLauncherID)
	_, err := cmdControl(mocks.NewMockPlatform(), rig.env(platforms.ControlPause, map[string]string{
		"slot": "PRIMARY",
	}))
	require.NoError(t, err)
	require.NotNil(t, rig.audio)
	assert.Equal(t, map[string]string{"slot": mediaslot.Primary}, rig.audio.Args)
}

func TestCmdControl_BackgroundStopClosesBackgroundPlaylist(t *testing.T) {
	t.Parallel()

	rig := newSlotControlRig(true)
	queue := make(chan *playlists.Playlist, 1)
	env := rig.env(platforms.ControlStop, map[string]string{"slot": "background"})
	env.Playlist = playlists.PlaylistController{Queue: queue}
	env.RecordMediaStop = func(string) func() {
		t.Error("a background stop must not record a primary stop")
		return func() {}
	}
	env.WaitForMediaReady = func(context.Context) error {
		t.Error("a background control must not wait for the primary media")
		return nil
	}

	result, err := cmdControl(mocks.NewMockPlatform(), env)
	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	require.NotNil(t, result.Playlist)
	assert.Equal(t, mediaslot.Background, result.Playlist.Slot)
	require.NotNil(t, rig.audio)
	assert.Zero(t, rig.gameCalls)
	require.Len(t, queue, 1)
	update := <-queue
	assert.Equal(t, mediaslot.Background, update.Slot)
	assert.True(t, update.Clear)
}

func TestCmdControl_PrimaryStopRecordsStop(t *testing.T) {
	t.Parallel()

	rig := newSlotControlRig(true)
	env := rig.env(platforms.ControlStop, nil)
	recorded := false
	env.RecordMediaStop = func(reason string) func() {
		recorded = true
		assert.Equal(t, apimodels.MediaStopReasonStopped, reason)
		return func() {}
	}

	result, err := cmdControl(mocks.NewMockPlatform(), env)
	require.NoError(t, err)
	assert.False(t, result.PlaylistChanged)
	assert.True(t, recorded)
	assert.Equal(t, 1, rig.gameCalls)
}
