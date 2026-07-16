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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleUIReturnsState(t *testing.T) {
	t.Parallel()

	service := events.New(clockwork.NewFakeClock(), nil, nil)
	handle, err := service.Open(t.Context(), &events.Request{Kind: models.UIEventKindConfirm})
	require.NoError(t, err)

	response, err := HandleUI(requests.RequestEnv{UI: service})
	require.NoError(t, err)
	state, ok := response.(models.UIStateResponse)
	require.True(t, ok)
	assert.Equal(t, uint64(1), state.Revision)
	require.Len(t, state.Events, 1)
	assert.Equal(t, handle.ID, state.Events[0].ID)
	assert.Empty(t, state.Resolved)
}

func TestHandleUIRespondResolvesEvent(t *testing.T) {
	t.Parallel()

	service := events.New(clockwork.NewFakeClock(), nil, nil)
	handle, err := service.Open(t.Context(), &events.Request{Kind: models.UIEventKindConfirm})
	require.NoError(t, err)

	params, err := json.Marshal(models.UIRespondParams{
		ID:     handle.ID,
		Action: models.UIResponseActionConfirm,
	})
	require.NoError(t, err)

	response, err := HandleUIRespond(requests.RequestEnv{
		UI:         service,
		ClientRole: "member",
		Params:     params,
	})
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, response)

	result := <-handle.Results
	assert.Equal(t, models.UIOutcomeConfirmed, result.Resolution.Outcome)
}

func TestHandleUIRespondRejectsInvalidParams(t *testing.T) {
	t.Parallel()

	service := events.New(clockwork.NewFakeClock(), nil, nil)
	_, err := HandleUIRespond(requests.RequestEnv{
		UI:     service,
		Params: json.RawMessage(`{"action":"confirm"}`),
	})
	require.Error(t, err)

	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
}

func TestHandleUIRespondRejectsStaleEvent(t *testing.T) {
	t.Parallel()

	service := events.New(clockwork.NewFakeClock(), nil, nil)
	params := json.RawMessage(`{"id":"stale","action":"confirm"}`)

	_, err := HandleUIRespond(requests.RequestEnv{UI: service, Params: params})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active UI event")
}

func TestHandleUIRequiresService(t *testing.T) {
	t.Parallel()

	_, err := HandleUI(requests.RequestEnv{})
	require.ErrorIs(t, err, errUIServiceUnavailable)

	_, err = HandleUIRespond(requests.RequestEnv{})
	require.ErrorIs(t, err, errUIServiceUnavailable)
}
