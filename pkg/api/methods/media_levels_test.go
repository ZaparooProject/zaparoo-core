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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleMediaLevelsSubscribe(t *testing.T) {
	t.Parallel()
	var got []bool
	env := requests.RequestEnv{
		Params:          json.RawMessage(`{"enabled":true}`),
		LevelsSubscribe: func(enabled bool) { got = append(got, enabled) },
	}
	res, err := HandleMediaLevelsSubscribe(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, res)
	env.Params = json.RawMessage(`{"enabled":false}`)
	_, err = HandleMediaLevelsSubscribe(env)
	require.NoError(t, err)
	assert.Equal(t, []bool{true, false}, got)

	env.Params = json.RawMessage(`{}`)
	_, err = HandleMediaLevelsSubscribe(env)
	require.Error(t, err)
}

func TestHandleMediaLevelsSubscribeNeedsWebSocket(t *testing.T) {
	t.Parallel()
	_, err := HandleMediaLevelsSubscribe(requests.RequestEnv{Params: json.RawMessage(`{"enabled":true}`)})
	require.ErrorContains(t, err, "WebSocket")
}
