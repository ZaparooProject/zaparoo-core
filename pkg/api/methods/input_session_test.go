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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingInputSession struct {
	keyboardErr   error
	gamepadErr    error
	keyboardArgs  []string
	gamepadArgs   []string
	keyboardDelay time.Duration
	gamepadDelay  time.Duration
	released      bool
}

func (s *recordingInputSession) KeyboardPressSequence(
	_ context.Context,
	args []string,
	delay time.Duration,
) error {
	s.keyboardArgs = append([]string(nil), args...)
	s.keyboardDelay = delay
	return s.keyboardErr
}

func (s *recordingInputSession) GamepadPressSequence(
	_ context.Context,
	args []string,
	delay time.Duration,
) error {
	s.gamepadArgs = append([]string(nil), args...)
	s.gamepadDelay = delay
	return s.gamepadErr
}

func (s *recordingInputSession) ReleaseAll() error {
	s.released = true
	return nil
}

func TestHandleInputKeyboard_UsesDurableInputSession(t *testing.T) {
	t.Parallel()

	session := &recordingInputSession{}
	env := requests.RequestEnv{
		Context:      t.Context(),
		InputSession: session,
		Params:       json.RawMessage(`{"keys":"{press:up}"}`),
	}

	result, err := HandleInputKeyboard(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, result)
	assert.Equal(t, []string{"{press:up}"}, session.keyboardArgs)
	assert.Zero(t, session.keyboardDelay)
	assert.False(t, session.released)
}

func TestHandleInputGamepad_UsesDurableInputSession(t *testing.T) {
	t.Parallel()

	session := &recordingInputSession{}
	env := requests.RequestEnv{
		Context:      t.Context(),
		InputSession: session,
		Params:       json.RawMessage(`{"buttons":"{press:start}"}`),
	}

	result, err := HandleInputGamepad(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, result)
	assert.Equal(t, []string{"{press:start}"}, session.gamepadArgs)
	assert.Zero(t, session.gamepadDelay)
}

func TestHandleInputKeyboard_RejectsPersistentInputWithoutSession(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context: t.Context(),
		Params:  json.RawMessage(`{"keys":"{press:up}"}`),
	}

	_, err := HandleInputKeyboard(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a supported WebSocket session")
	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
}

func TestHandleInputGamepad_RejectsPersistentInputWithoutSession(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context: t.Context(),
		Params:  json.RawMessage(`{"buttons":"{release:start}"}`),
	}

	_, err := HandleInputGamepad(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a supported WebSocket session")
	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
}
