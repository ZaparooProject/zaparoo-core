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
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleInbox_Success(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()

	messages := []database.InboxMessage{
		{DBID: 1, Title: "First Message", Body: "Body 1", Severity: 1, Category: "cat1", CreatedAt: now},
		{DBID: 2, Title: "Second Message", Body: "", Severity: 0, CreatedAt: now.Add(-time.Hour)},
	}

	mockUserDB.On("GetInboxMessages").Return(messages, nil)

	env := requests.RequestEnv{
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := HandleInbox(env)

	require.NoError(t, err)
	resp, ok := result.(models.InboxResponse)
	require.True(t, ok)
	assert.Len(t, resp.Messages, 2)
	assert.Equal(t, int64(1), resp.Messages[0].ID)
	assert.Equal(t, "First Message", resp.Messages[0].Title)
	assert.Equal(t, "Body 1", resp.Messages[0].Body)
	assert.Equal(t, 1, resp.Messages[0].Severity)
	assert.Equal(t, "cat1", resp.Messages[0].Category)
	assert.Equal(t, int64(2), resp.Messages[1].ID)
	assert.Equal(t, "Second Message", resp.Messages[1].Title)
	assert.Empty(t, resp.Messages[1].Body)

	mockUserDB.AssertExpectations(t)
}

func TestHandleInbox_Empty(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetInboxMessages").Return([]database.InboxMessage{}, nil)

	env := requests.RequestEnv{
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := HandleInbox(env)

	require.NoError(t, err)
	resp, ok := result.(models.InboxResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Messages)

	mockUserDB.AssertExpectations(t)
}

func TestHandleInbox_DatabaseError(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetInboxMessages").Return([]database.InboxMessage{}, errors.New("db error"))

	env := requests.RequestEnv{
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := HandleInbox(env)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "error getting inbox messages")

	mockUserDB.AssertExpectations(t)
}

func TestHandleInboxDelete_Success(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("DeleteInboxMessage", int64(42)).Return(nil)

	params := models.DeleteInboxParams{ID: 42}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Database: &database.Database{UserDB: mockUserDB},
		Params:   paramsJSON,
	}

	result, err := HandleInboxDelete(env)

	require.NoError(t, err)
	_, ok := result.(NoContent)
	assert.True(t, ok)

	mockUserDB.AssertExpectations(t)
}

func TestHandleInboxDelete_InvalidParams(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()

	// ID must be > 0 per validation
	params := models.DeleteInboxParams{ID: 0}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Database: &database.Database{UserDB: mockUserDB},
		Params:   paramsJSON,
	}

	result, err := HandleInboxDelete(env)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid params")

	mockUserDB.AssertNotCalled(t, "DeleteInboxMessage")
}

func TestHandleInboxDelete_DatabaseError(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("DeleteInboxMessage", int64(1)).Return(errors.New("db error"))

	params := models.DeleteInboxParams{ID: 1}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Database: &database.Database{UserDB: mockUserDB},
		Params:   paramsJSON,
	}

	result, err := HandleInboxDelete(env)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to delete inbox message")

	mockUserDB.AssertExpectations(t)
}

func TestHandleInboxClear_Success(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("DeleteAllInboxMessages").Return(int64(5), nil)

	env := requests.RequestEnv{
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := HandleInboxClear(env)

	require.NoError(t, err)
	_, ok := result.(NoContent)
	assert.True(t, ok)

	mockUserDB.AssertExpectations(t)
}

func TestHandleInboxClear_EmptyInbox(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("DeleteAllInboxMessages").Return(int64(0), nil)

	env := requests.RequestEnv{
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := HandleInboxClear(env)

	require.NoError(t, err)
	_, ok := result.(NoContent)
	assert.True(t, ok)

	mockUserDB.AssertExpectations(t)
}

func TestHandleInboxClear_DatabaseError(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("DeleteAllInboxMessages").Return(int64(0), errors.New("db error"))

	env := requests.RequestEnv{
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := HandleInboxClear(env)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to clear inbox")

	mockUserDB.AssertExpectations(t)
}
