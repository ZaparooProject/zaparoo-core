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
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleInputKeyboard_LegacyPolicy(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("KeyboardPress", "a").Return(nil)
	params := json.RawMessage(`{"keys":"a"}`)

	_, err := HandleInputKeyboard(requests.RequestEnv{
		Platform: pl, PlatformID: platformids.Linux, Params: params,
	})
	require.ErrorIs(t, err, ErrForbidden)
	pl.AssertNotCalled(t, "KeyboardPress", "a")

	_, err = HandleInputKeyboard(requests.RequestEnv{
		Platform: pl, PlatformID: platformids.Batocera, Params: params,
	})
	require.NoError(t, err)
	pl.AssertCalled(t, "KeyboardPress", "a")
}

func TestHandleInputKeyboard_SingleKey(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("KeyboardPress", "a").Return(nil)

	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		Params:   json.RawMessage(`{"keys": "a"}`),
		IsLocal:  true,
	}

	result, err := HandleInputKeyboard(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, result)
	pl.AssertCalled(t, "KeyboardPress", "a")
}

func TestHandleInputKeyboard_MultiCharMacro(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("KeyboardPress", mock.AnythingOfType("string")).Return(nil)

	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		Params:   json.RawMessage(`{"keys": "abc{enter}"}`),
		IsLocal:  true,
	}

	result, err := HandleInputKeyboard(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, result)

	presses := pl.GetKeyboardPresses()
	assert.Equal(t, []string{"a", "b", "c", "{enter}"}, presses)
}

func TestHandleInputKeyboard_SpecialKey(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("KeyboardPress", "{f9}").Return(nil)

	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		Params:   json.RawMessage(`{"keys": "{f9}"}`),
		IsLocal:  true,
	}

	result, err := HandleInputKeyboard(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, result)
	pl.AssertCalled(t, "KeyboardPress", "{f9}")
}

func TestHandleInputKeyboard_MissingParams(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context: context.Background(),
		Params:  json.RawMessage(`{}`),
		IsLocal: true,
	}

	_, err := HandleInputKeyboard(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid params")

	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
}

func TestHandleInputKeyboard_PlatformError(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("KeyboardPress", "a").Return(errors.New("device not available"))

	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		Params:   json.RawMessage(`{"keys": "a"}`),
		IsLocal:  true,
	}

	_, err := HandleInputKeyboard(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device not available")
}

func TestHandleInputGamepad_SingleButton(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("GamepadPress", "A").Return(nil)

	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		Params:   json.RawMessage(`{"buttons": "A"}`),
		IsLocal:  true,
	}

	result, err := HandleInputGamepad(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, result)
	pl.AssertCalled(t, "GamepadPress", "A")
}

func TestHandleInputGamepad_MultiButton(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("GamepadPress", mock.AnythingOfType("string")).Return(nil)

	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		Params:   json.RawMessage(`{"buttons": "{up}{down}A"}`),
		IsLocal:  true,
	}

	result, err := HandleInputGamepad(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, result)

	presses := pl.GetGamepadPresses()
	assert.Equal(t, []string{"{up}", "{down}", "A"}, presses)
}

func TestHandleInputGamepad_MissingParams(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context: context.Background(),
		Params:  json.RawMessage(`{}`),
		IsLocal: true,
	}

	_, err := HandleInputGamepad(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid params")

	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
}

func TestHandleInputGamepad_PlatformError(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("GamepadPress", "A").Return(errors.New("virtual gamepad is disabled"))

	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		Params:   json.RawMessage(`{"buttons": "A"}`),
		IsLocal:  true,
	}

	_, err := HandleInputGamepad(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "virtual gamepad is disabled")
}

// memberEnv builds a request env for a paired member on a remote connection,
// which is the case the input lists are meant to constrain.
func memberEnv(pl *mocks.MockPlatform, cfg *config.Instance, params json.RawMessage) requests.RequestEnv {
	return requests.RequestEnv{
		Platform:   pl,
		PlatformID: platformids.Windows,
		ClientRole: "member",
		Config:     cfg,
		Params:     params,
	}
}

func TestHandleInputKeyboard_BlockListAppliesToMembers(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("KeyboardPress", mock.Anything).Return(nil)

	// These all resolve to real keys, so a pass here cannot be an unknown-key
	// error wearing a block list's clothes.
	blocked := []string{
		`{"keys":"{alt+f4}"}`,
		`{"keys":"{ctrl+alt+delete}"}`,
		`{"keys":"{ctrl+alt+t}"}`,
		`{"keys":"{press:ctrl+alt+delete}"}`,
	}
	for _, params := range blocked {
		_, err := HandleInputKeyboard(memberEnv(pl, nil, json.RawMessage(params)))
		require.Error(t, err, "params %s should be blocked", params)
		require.ErrorIs(t, err, zapscript.ErrInputBlocked, "params %s", params)
	}
	pl.AssertNotCalled(t, "KeyboardPress", "{alt+f4}")
	pl.AssertNotCalled(t, "KeyboardPress", "{ctrl+alt+delete}")
}

// The app's remote keyboard sends plain characters, so the mode check must not
// reach the API even though Windows defaults to combos mode.
func TestHandleInputKeyboard_MembersMayStillType(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("KeyboardPress", mock.Anything).Return(nil)

	_, err := HandleInputKeyboard(memberEnv(pl, nil, json.RawMessage(`{"keys":"hello"}`)))
	require.NoError(t, err)
	pl.AssertCalled(t, "KeyboardPress", "h")
}

func TestHandleInputKeyboard_BlockListExemptsAdminAndLocal(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("KeyboardPress", mock.Anything).Return(nil)
	params := json.RawMessage(`{"keys":"{alt+f4}"}`)

	_, err := HandleInputKeyboard(requests.RequestEnv{
		Platform: pl, PlatformID: platformids.Windows, ClientRole: "admin", Params: params,
	})
	require.NoError(t, err)

	_, err = HandleInputKeyboard(requests.RequestEnv{
		Platform: pl, IsLocal: true, Params: params,
	})
	require.NoError(t, err)
}

func TestHandleInputKeyboard_ClearedBlockListLetsMembersThrough(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("KeyboardPress", mock.Anything).Return(nil)

	values := config.BaseDefaults
	values.ZapScript.Input.Block = []string{}
	cfg, err := config.NewConfig(t.TempDir(), values)
	require.NoError(t, err)

	_, err = HandleInputKeyboard(memberEnv(pl, cfg, json.RawMessage(`{"keys":"{alt+f4}"}`)))
	require.NoError(t, err)
	pl.AssertCalled(t, "KeyboardPress", "{alt+f4}")
}

func TestHandleInputGamepad_BlockListDoesNotBlockButtons(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("GamepadPress", mock.Anything).Return(nil)

	_, err := HandleInputGamepad(memberEnv(pl, nil, json.RawMessage(`{"buttons":"{start}"}`)))
	require.NoError(t, err)
	pl.AssertCalled(t, "GamepadPress", "{start}")
}
