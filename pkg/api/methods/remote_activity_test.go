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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleRemoteActivity_RejectsRemoteClients(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{IsLocal: false, ClientRole: "member"}
	_, err := HandleRemoteActivity(env)
	require.Error(t, err)
}

func TestHandleRemoteActivity_ReturnsRecentEntriesNewestFirst(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	mockUserDB := &helpers.MockUserDBI{}
	mockUserDB.On("ListRecentRemoteCommands", defaultRemoteActivityLimit).Return([]database.RemoteCommand{
		{
			CommandID: "cmd_1", OperationType: "launch", State: "terminal",
			ResultStatus: "succeeded", CreatedAt: created,
			Origin: json.RawMessage(`{"kind":"first_party"}`),
		},
		{
			CommandID: "cmd_2", OperationType: "media.search", State: "terminal",
			ResultStatus: "failed", ErrorCode: "bad_params", CreatedAt: created.Add(-time.Minute),
			Origin: json.RawMessage(`{"kind":"api_key","key_name":"companion-app"}`),
		},
	}, nil)

	env := requests.RequestEnv{
		IsLocal:  true,
		Database: &database.Database{UserDB: mockUserDB},
	}
	result, err := HandleRemoteActivity(env)
	require.NoError(t, err)
	resp, ok := result.(models.RemoteActivityResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 2)

	assert.Equal(t, "launch", resp.Entries[0].OperationType)
	assert.Equal(t, "first_party", resp.Entries[0].OriginKind)
	assert.Empty(t, resp.Entries[0].OriginKeyName)
	assert.Equal(t, "succeeded", resp.Entries[0].Status)

	assert.Equal(t, "media.search", resp.Entries[1].OperationType)
	assert.Equal(t, "api_key", resp.Entries[1].OriginKind)
	assert.Equal(t, "companion-app", resp.Entries[1].OriginKeyName)
	assert.Equal(t, "bad_params", resp.Entries[1].ErrorCode)

	mockUserDB.AssertExpectations(t)
}

// TestHandleRemoteActivity_ReportsPollerStatus pins that the response
// carries the remote poller's last observation (state, last contact, last
// error code) alongside the ledger, and reports unknown when no service
// state is available at all.
func TestHandleRemoteActivity_ReportsPollerStatus(t *testing.T) {
	t.Parallel()

	st, notifications := state.NewState(nil, "")
	t.Cleanup(func() {
		for len(notifications) > 0 {
			<-notifications
		}
	})
	st.SetRemoteStatus(state.RemoteStateWaiting, "")
	st.SetRemoteStatus(state.RemoteStateNotRemoteDevice, "remote_slot_required")

	mockUserDB := &helpers.MockUserDBI{}
	mockUserDB.On("ListRecentRemoteCommands", defaultRemoteActivityLimit).Return([]database.RemoteCommand{}, nil)
	result, err := HandleRemoteActivity(requests.RequestEnv{
		IsLocal: true, State: st, Database: &database.Database{UserDB: mockUserDB},
	})
	require.NoError(t, err)
	resp, ok := result.(models.RemoteActivityResponse)
	require.True(t, ok)
	assert.Equal(t, state.RemoteStateNotRemoteDevice, resp.Status.State)
	assert.Equal(t, "remote_slot_required", resp.Status.LastErrorCode)
	assert.NotEmpty(t, resp.Status.LastContactAt, "the earlier waiting state is remembered as last contact")
	_, parseErr := time.Parse(time.RFC3339, resp.Status.LastContactAt)
	require.NoError(t, parseErr)

	result, err = HandleRemoteActivity(requests.RequestEnv{IsLocal: true})
	require.NoError(t, err)
	resp, ok = result.(models.RemoteActivityResponse)
	require.True(t, ok)
	assert.Equal(t, state.RemoteStateUnknown, resp.Status.State)
	assert.Empty(t, resp.Entries)
}

func TestHandleRemoteActivity_RejectsOutOfRangeLimit(t *testing.T) {
	t.Parallel()

	requested := 1000
	paramsJSON, err := json.Marshal(models.RemoteActivityParams{Limit: &requested})
	require.NoError(t, err)

	mockUserDB := &helpers.MockUserDBI{}
	env := requests.RequestEnv{
		IsLocal:  true,
		Database: &database.Database{UserDB: mockUserDB},
		Params:   paramsJSON,
	}
	_, err = HandleRemoteActivity(env)
	require.Error(t, err)
	mockUserDB.AssertNotCalled(t, "ListRecentRemoteCommands", mock.Anything)
}

// TestHandleRemoteActivity_RejectsZeroLimit pins that limit: 0 is rejected,
// not silently reinterpreted as "no results" or as the default: the
// validate tag is "omitempty,gt=0,max=100" on a *int, and go-playground's
// omitempty for a pointer field checks nilness, not the dereferenced value,
// so an explicit 0 still hits gt=0 and fails rather than skipping it.
func TestHandleRemoteActivity_RejectsZeroLimit(t *testing.T) {
	t.Parallel()

	zero := 0
	paramsJSON, err := json.Marshal(models.RemoteActivityParams{Limit: &zero})
	require.NoError(t, err)

	mockUserDB := &helpers.MockUserDBI{}
	env := requests.RequestEnv{
		IsLocal:  true,
		Database: &database.Database{UserDB: mockUserDB},
		Params:   paramsJSON,
	}
	_, err = HandleRemoteActivity(env)
	require.Error(t, err)
	mockUserDB.AssertNotCalled(t, "ListRecentRemoteCommands", mock.Anything)
}

func TestHandleRemoteActivity_HonoursRequestedLimit(t *testing.T) {
	t.Parallel()

	requested := 5
	paramsJSON, err := json.Marshal(models.RemoteActivityParams{Limit: &requested})
	require.NoError(t, err)

	mockUserDB := &helpers.MockUserDBI{}
	mockUserDB.On("ListRecentRemoteCommands", requested).Return([]database.RemoteCommand{}, nil)

	env := requests.RequestEnv{
		IsLocal:  true,
		Database: &database.Database{UserDB: mockUserDB},
		Params:   paramsJSON,
	}
	_, err = HandleRemoteActivity(env)
	require.NoError(t, err)
	mockUserDB.AssertExpectations(t)
}
