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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleClients_Success(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("ListClients").Return([]database.Client{
		{ClientID: "id-1", ClientName: "App One", CreatedAt: 1700000000, LastSeenAt: 1700001000},
		{ClientID: "id-2", ClientName: "App Two", CreatedAt: 1700100000, LastSeenAt: 1700101000},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		IsLocal:  true,
	}

	result, err := HandleClients(env)
	require.NoError(t, err)

	resp, ok := result.(models.ClientsResponse)
	require.True(t, ok)
	require.Len(t, resp.Clients, 2)
	assert.Equal(t, "id-1", resp.Clients[0].ClientID)
	assert.Equal(t, "App One", resp.Clients[0].ClientName)
	assert.Equal(t, int64(1700000000), resp.Clients[0].CreatedAt)
	assert.Equal(t, int64(1700001000), resp.Clients[0].LastSeenAt)
	assert.Equal(t, "id-2", resp.Clients[1].ClientID)
	mockUserDB.AssertExpectations(t)
}

func TestHandleClients_Empty(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("ListClients").Return([]database.Client{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		IsLocal:  true,
	}

	result, err := HandleClients(env)
	require.NoError(t, err)
	resp, ok := result.(models.ClientsResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Clients)
}

func TestHandleClients_DatabaseError(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("ListClients").Return([]database.Client{}, errors.New("db error"))

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		IsLocal:  true,
	}

	_, err := HandleClients(env)
	require.Error(t, err)
}

func TestHandleClients_RemoteRejected(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	// Expect no DB call since we should be rejected before reaching it.
	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		IsLocal:  false,
	}

	_, err := HandleClients(env)
	require.ErrorIs(t, err, ErrLocalhostOnly)
	mockUserDB.AssertNotCalled(t, "ListClients")
}

// Note on revocation semantics: HandleClientsDelete is forward-only —
// it deletes the row but does not actively close in-flight WebSocket
// sessions. The "new connections fail after revoke" property is
// covered end-to-end by `TestIntegration_RevokedClientCannotConnect` in
// `pkg/api/integration_encryption_test.go`, which mocks
// `GetClientByToken` to return an error post-revoke and asserts
// `EstablishSession` rejects with `ErrUnknownAuthToken`. The tests
// below cover only the HTTP/RPC handler shape.

func TestHandleClientsDelete_Success(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("DeleteClient", "client-uuid").Return(nil)

	params, err := json.Marshal(models.ClientsDeleteParams{ClientID: "client-uuid"})
	require.NoError(t, err)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		IsLocal:  true,
		Params:   params,
	}

	result, err := HandleClientsDelete(env)
	require.NoError(t, err)
	_, ok := result.(NoContent)
	assert.True(t, ok)
	mockUserDB.AssertExpectations(t)
}

func TestHandleClientsDelete_MissingClientID(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	params, err := json.Marshal(models.ClientsDeleteParams{ClientID: ""})
	require.NoError(t, err)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		IsLocal:  true,
		Params:   params,
	}

	_, err = HandleClientsDelete(env)
	require.Error(t, err)
	mockUserDB.AssertNotCalled(t, "DeleteClient")
}

func TestHandleClientsDelete_DatabaseError(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("DeleteClient", "client-uuid").Return(errors.New("not found"))

	params, err := json.Marshal(models.ClientsDeleteParams{ClientID: "client-uuid"})
	require.NoError(t, err)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		IsLocal:  true,
		Params:   params,
	}

	_, err = HandleClientsDelete(env)
	require.Error(t, err)
}

func TestHandleClientsDelete_RemoteRejected(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	params, err := json.Marshal(models.ClientsDeleteParams{ClientID: "client-uuid"})
	require.NoError(t, err)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		IsLocal:  false,
		Params:   params,
	}

	_, err = HandleClientsDelete(env)
	require.ErrorIs(t, err, ErrLocalhostOnly)
	mockUserDB.AssertNotCalled(t, "DeleteClient")
}
