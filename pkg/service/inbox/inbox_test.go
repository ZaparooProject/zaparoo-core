// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package inbox

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestService_Add_Success(t *testing.T) {
	t.Parallel()

	mockDB := helpers.NewMockUserDBI()
	ns := make(chan models.Notification, 10)

	now := time.Now()
	insertedEntry := &database.InboxEntry{
		DBID:      42,
		Title:     "Test Title",
		Body:      "Test Body",
		CreatedAt: now,
	}

	mockDB.On("AddInboxEntry", mock.MatchedBy(func(e *database.InboxEntry) bool {
		return e.Title == "Test Title" && e.Body == "Test Body"
	})).Return(insertedEntry, nil)

	svc := NewService(mockDB, ns)

	err := svc.Add("Test Title", "Test Body")

	require.NoError(t, err)
	mockDB.AssertExpectations(t)

	// Verify notification was sent with full entry
	select {
	case notif := <-ns:
		assert.Equal(t, models.NotificationInboxAdded, notif.Method)

		// Verify payload contains the full entry
		var payload models.InboxEntry
		err := json.Unmarshal(notif.Params, &payload)
		require.NoError(t, err)
		assert.Equal(t, int64(42), payload.ID)
		assert.Equal(t, "Test Title", payload.Title)
		assert.Equal(t, "Test Body", payload.Body)
		assert.False(t, payload.CreatedAt.IsZero())
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected notification was not sent")
	}
}

func TestService_Add_EmptyBody(t *testing.T) {
	t.Parallel()

	mockDB := helpers.NewMockUserDBI()
	ns := make(chan models.Notification, 10)

	now := time.Now()
	insertedEntry := &database.InboxEntry{
		DBID:      1,
		Title:     "Title Only",
		Body:      "",
		CreatedAt: now,
	}

	mockDB.On("AddInboxEntry", mock.MatchedBy(func(e *database.InboxEntry) bool {
		return e.Title == "Title Only" && e.Body == ""
	})).Return(insertedEntry, nil)

	svc := NewService(mockDB, ns)

	err := svc.Add("Title Only", "")

	require.NoError(t, err)
	mockDB.AssertExpectations(t)

	// Verify notification was sent
	select {
	case notif := <-ns:
		assert.Equal(t, models.NotificationInboxAdded, notif.Method)

		var payload models.InboxEntry
		err := json.Unmarshal(notif.Params, &payload)
		require.NoError(t, err)
		assert.Equal(t, "Title Only", payload.Title)
		assert.Empty(t, payload.Body)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected notification was not sent")
	}
}

func TestService_Add_EmptyTitle(t *testing.T) {
	t.Parallel()

	mockDB := helpers.NewMockUserDBI()
	ns := make(chan models.Notification, 10)

	svc := NewService(mockDB, ns)

	err := svc.Add("", "Body without title")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "title cannot be empty")
	mockDB.AssertNotCalled(t, "AddInboxEntry")

	// Verify no notification was sent
	select {
	case <-ns:
		t.Fatal("notification should not be sent on error")
	case <-time.After(50 * time.Millisecond):
		// Expected - no notification
	}
}

func TestService_Add_DatabaseError(t *testing.T) {
	t.Parallel()

	mockDB := helpers.NewMockUserDBI()
	ns := make(chan models.Notification, 10)

	mockDB.On("AddInboxEntry", mock.Anything).Return((*database.InboxEntry)(nil), errors.New("db error"))

	svc := NewService(mockDB, ns)

	err := svc.Add("Test Title", "Test Body")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add inbox entry")
	mockDB.AssertExpectations(t)

	// Verify no notification was sent on error
	select {
	case <-ns:
		t.Fatal("notification should not be sent on database error")
	case <-time.After(50 * time.Millisecond):
		// Expected - no notification
	}
}

func TestNewService(t *testing.T) {
	t.Parallel()

	mockDB := helpers.NewMockUserDBI()
	ns := make(chan models.Notification, 10)

	svc := NewService(mockDB, ns)

	assert.NotNil(t, svc)
	assert.Equal(t, mockDB, svc.db)
	assert.Equal(t, chan<- models.Notification(ns), svc.notifications)
}
