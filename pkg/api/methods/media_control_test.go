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
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleMediaControl_NoActiveMedia(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	env := requests.RequestEnv{
		State:  st,
		Params: json.RawMessage(`{"action": "save_state"}`),
	}

	_, err := HandleMediaControl(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active media")
}

func TestHandleMediaControl_UnknownAction(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	st.SetActiveMedia(models.NewActiveMedia("NES", "NES", "/game.nes", "Game", "test-launcher"))

	cache := &helpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{
			ID:       "test-launcher",
			SystemID: "NES",
			Controls: map[string]platforms.Control{
				"save_state": {Func: func(_ *config.Instance, _ platforms.ControlParams) error {
					return nil
				}},
			},
		},
	})

	env := requests.RequestEnv{
		State:         st,
		LauncherCache: cache,
		Params:        json.RawMessage(`{"action": "unknown_action"}`),
	}

	_, err := HandleMediaControl(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestHandleMediaControl_NoControls(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	st.SetActiveMedia(models.NewActiveMedia("NES", "NES", "/game.nes", "Game", "test-launcher"))

	cache := &helpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{
			ID:       "test-launcher",
			SystemID: "NES",
		},
	})

	env := requests.RequestEnv{
		State:         st,
		LauncherCache: cache,
		Params:        json.RawMessage(`{"action": "save_state"}`),
	}

	_, err := HandleMediaControl(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no control capabilities")
}

func TestHandleMediaControl_Success(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	st.SetActiveMedia(models.NewActiveMedia("NES", "NES", "/game.nes", "Game", "test-launcher"))

	called := false
	cache := &helpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{
			ID:       "test-launcher",
			SystemID: "NES",
			Controls: map[string]platforms.Control{
				"save_state": {Func: func(_ *config.Instance, _ platforms.ControlParams) error {
					called = true
					return nil
				}},
			},
		},
	})

	env := requests.RequestEnv{
		State:         st,
		LauncherCache: cache,
		Params:        json.RawMessage(`{"action": "save_state"}`),
	}

	result, err := HandleMediaControl(env)
	require.NoError(t, err)
	assert.True(t, called)
	assert.NotNil(t, result)
}

func TestHandleMediaControl_MissingParams(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Params: json.RawMessage(`{}`),
	}

	_, err := HandleMediaControl(env)
	require.Error(t, err)
}

func TestHandleMediaControl_ArgsPassThrough(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	st.SetActiveMedia(models.NewActiveMedia("NES", "NES", "/game.nes", "Game", "test-launcher"))

	var receivedArgs map[string]string
	cache := &helpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{
			ID:       "test-launcher",
			SystemID: "NES",
			Controls: map[string]platforms.Control{
				"save_state": {Func: func(_ *config.Instance, cp platforms.ControlParams) error {
					receivedArgs = cp.Args
					return nil
				}},
			},
		},
	})

	env := requests.RequestEnv{
		State:         st,
		LauncherCache: cache,
		Params:        json.RawMessage(`{"action": "save_state", "args": {"slot": "3"}}`),
	}

	_, err := HandleMediaControl(env)
	require.NoError(t, err)
	require.NotNil(t, receivedArgs)
	assert.Equal(t, "3", receivedArgs["slot"])
}

func TestHandleMediaControl_ArgsNilWhenOmitted(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	st.SetActiveMedia(models.NewActiveMedia("NES", "NES", "/game.nes", "Game", "test-launcher"))

	var receivedArgs map[string]string
	cache := &helpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{
			ID:       "test-launcher",
			SystemID: "NES",
			Controls: map[string]platforms.Control{
				"save_state": {Func: func(_ *config.Instance, cp platforms.ControlParams) error {
					receivedArgs = cp.Args
					return nil
				}},
			},
		},
	})

	env := requests.RequestEnv{
		State:         st,
		LauncherCache: cache,
		Params:        json.RawMessage(`{"action": "save_state"}`),
	}

	_, err := HandleMediaControl(env)
	require.NoError(t, err)
	assert.Nil(t, receivedArgs)
}

func TestHandleMediaControl_ScriptExecution(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("ID").Return("test")
	pl.On("KeyboardPress", "{f2}").Return(nil)

	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	st.SetActiveMedia(models.NewActiveMedia("NES", "NES", "/game.nes", "Game", "test-launcher"))

	cache := &helpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{
			ID:       "test-launcher",
			SystemID: "NES",
			Controls: map[string]platforms.Control{
				"quick_save": {Script: "**input.keyboard:{f2}"},
			},
		},
	})

	env := requests.RequestEnv{
		State:         st,
		Platform:      pl,
		Config:        &config.Instance{},
		LauncherCache: cache,
		Params:        json.RawMessage(`{"action": "quick_save"}`),
	}

	result, err := HandleMediaControl(env)
	require.NoError(t, err)
	assert.NotNil(t, result)
	pl.AssertCalled(t, "KeyboardPress", "{f2}")
}

// drainNotifications prevents goroutine leaks by draining the notification channel.
func drainNotifications(t *testing.T, ns <-chan models.Notification) {
	t.Helper()
	t.Cleanup(func() {
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})
}
