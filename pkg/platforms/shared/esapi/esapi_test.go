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

package esapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRunningGameResponse_NoGame(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		body string
	}{
		{name: "compact", body: `{"msg":"NO GAME RUNNING"}`},
		{name: "formatted", body: "{\n  \"msg\": \"NO GAME RUNNING\"\n}"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			game, running, err := parseRunningGameResponse([]byte(tc.body))

			require.NoError(t, err)
			assert.False(t, running)
			assert.Empty(t, game)
		})
	}
}

func TestParseRunningGameResponse_RunningGame(t *testing.T) {
	t.Parallel()

	body := `{"id":"42","path":"C:\\RetroBat\\roms\\snes\\game.sfc","name":"Game","systemName":"snes"}`

	game, running, err := parseRunningGameResponse([]byte(body))

	require.NoError(t, err)
	assert.True(t, running)
	assert.Equal(t, "42", game.ID)
	assert.Equal(t, `C:\RetroBat\roms\snes\game.sfc`, game.Path)
	assert.Equal(t, "Game", game.Name)
	assert.Equal(t, "snes", game.SystemName)
}

func TestParseRunningGameResponse_InvalidGameResponse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		body string
	}{
		{name: "empty object", body: `{}`},
		{name: "unknown message", body: `{"msg":"ERROR"}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			game, running, err := parseRunningGameResponse([]byte(tc.body))

			require.Error(t, err)
			assert.False(t, running)
			assert.Empty(t, game)
			assert.Contains(t, err.Error(), "did not include game identity")
		})
	}
}
