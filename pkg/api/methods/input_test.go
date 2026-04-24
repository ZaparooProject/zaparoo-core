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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

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
	}

	_, err := HandleInputGamepad(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "virtual gamepad is disabled")
}
