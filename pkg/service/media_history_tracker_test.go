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

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMediaHistoryTracker_Listen_Started(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform)
	fakeClock := clockwork.NewFakeClock()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	tracker := &mediaHistoryTracker{
		st:    st,
		db:    db,
		clock: fakeClock,
	}

	// Create active media
	startTime := fakeClock.Now()
	activeMedia := &models.ActiveMedia{
		Started:    startTime,
		SystemID:   "nes",
		SystemName: "Nintendo Entertainment System",
		Path:       "/games/mario.nes",
		Name:       "Super Mario Bros.",
		LauncherID: "retroarch",
	}

	// Set active media in state
	st.SetActiveMedia(activeMedia)

	// Setup mock expectations
	expectedDBID := int64(42)
	mockUserDB.On("AddMediaHistory", mock.MatchedBy(func(entry *database.MediaHistoryEntry) bool {
		return entry.SystemID == "nes" &&
			entry.SystemName == "Nintendo Entertainment System" &&
			entry.MediaPath == "/games/mario.nes" &&
			entry.MediaName == "Super Mario Bros." &&
			entry.LauncherID == "retroarch" &&
			entry.PlayTime == 0
	})).Return(expectedDBID, nil)

	// Create notification channel
	notifChan := make(chan models.Notification, 1)
	notifChan <- models.Notification{Method: models.NotificationStarted}
	close(notifChan)

	// Execute
	tracker.listen(notifChan)

	// Verify
	mockUserDB.AssertExpectations(t)
	assert.Equal(t, expectedDBID, tracker.currentHistoryDBID)
	assert.Equal(t, startTime, tracker.currentMediaStartTime)
}

func TestMediaHistoryTracker_Listen_Started_NoActiveMedia(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform)
	fakeClock := clockwork.NewFakeClock()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	tracker := &mediaHistoryTracker{
		st:    st,
		db:    db,
		clock: fakeClock,
	}

	// No active media set in state

	// Create notification channel
	notifChan := make(chan models.Notification, 1)
	notifChan <- models.Notification{Method: models.NotificationStarted}
	close(notifChan)

	// Execute
	tracker.listen(notifChan)

	// Verify - AddMediaHistory should not be called
	mockUserDB.AssertNotCalled(t, "AddMediaHistory", mock.Anything)
	assert.Equal(t, int64(0), tracker.currentHistoryDBID)
}

func TestMediaHistoryTracker_Listen_Started_DatabaseError(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform)
	fakeClock := clockwork.NewFakeClock()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	tracker := &mediaHistoryTracker{
		st:    st,
		db:    db,
		clock: fakeClock,
	}

	// Create active media
	activeMedia := &models.ActiveMedia{
		Started:    fakeClock.Now(),
		SystemID:   "nes",
		SystemName: "Nintendo Entertainment System",
		Path:       "/games/mario.nes",
		Name:       "Super Mario Bros.",
		LauncherID: "retroarch",
	}

	st.SetActiveMedia(activeMedia)

	// Setup mock to return error
	mockUserDB.On("AddMediaHistory", mock.Anything).Return(int64(0), errors.New("database error"))

	// Create notification channel
	notifChan := make(chan models.Notification, 1)
	notifChan <- models.Notification{Method: models.NotificationStarted}
	close(notifChan)

	// Execute - should not panic on error
	tracker.listen(notifChan)

	// Verify
	mockUserDB.AssertExpectations(t)
	// DBID should remain 0 when database fails
	assert.Equal(t, int64(0), tracker.currentHistoryDBID)
}

func TestMediaHistoryTracker_Listen_Stopped(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform)
	fakeClock := clockwork.NewFakeClock()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	startTime := fakeClock.Now()
	tracker := &mediaHistoryTracker{
		st:                    st,
		db:                    db,
		clock:                 fakeClock,
		currentHistoryDBID:    42,
		currentMediaStartTime: startTime,
	}

	// Advance clock by 5 minutes
	fakeClock.Advance(5 * time.Minute)

	// Setup mock expectations - playTime should be exactly 300 seconds (5 minutes)
	mockUserDB.On(
		"CloseMediaHistory",
		int64(42),
		mock.AnythingOfType("time.Time"),
		300, // Exactly 5 minutes
	).Return(nil)

	// Create notification channel
	notifChan := make(chan models.Notification, 1)
	notifChan <- models.Notification{Method: models.NotificationStopped}
	close(notifChan)

	// Execute
	tracker.listen(notifChan)

	// Verify
	mockUserDB.AssertExpectations(t)
	assert.Equal(t, int64(0), tracker.currentHistoryDBID)
	assert.True(t, tracker.currentMediaStartTime.IsZero())
}

func TestMediaHistoryTracker_Listen_Stopped_NoActiveHistory(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform)
	fakeClock := clockwork.NewFakeClock()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	tracker := &mediaHistoryTracker{
		st:                 st,
		db:                 db,
		clock:              fakeClock,
		currentHistoryDBID: 0, // No active history
	}

	// Create notification channel
	notifChan := make(chan models.Notification, 1)
	notifChan <- models.Notification{Method: models.NotificationStopped}
	close(notifChan)

	// Execute
	tracker.listen(notifChan)

	// Verify - CloseMediaHistory should not be called
	mockUserDB.AssertNotCalled(t, "CloseMediaHistory", mock.Anything, mock.Anything, mock.Anything)
}

func TestMediaHistoryTracker_Listen_Stopped_DatabaseError(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform)
	fakeClock := clockwork.NewFakeClock()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	startTime := fakeClock.Now()
	tracker := &mediaHistoryTracker{
		st:                    st,
		db:                    db,
		clock:                 fakeClock,
		currentHistoryDBID:    42,
		currentMediaStartTime: startTime,
	}

	// Advance clock by 5 minutes
	fakeClock.Advance(5 * time.Minute)

	// Setup mock to return error
	mockUserDB.On("CloseMediaHistory", int64(42), mock.AnythingOfType("time.Time"), 300).
		Return(errors.New("database error"))

	// Create notification channel
	notifChan := make(chan models.Notification, 1)
	notifChan <- models.Notification{Method: models.NotificationStopped}
	close(notifChan)

	// Execute - should not panic on error
	tracker.listen(notifChan)

	// Verify
	mockUserDB.AssertExpectations(t)
	// State should still be reset even on error
	assert.Equal(t, int64(0), tracker.currentHistoryDBID)
	assert.True(t, tracker.currentMediaStartTime.IsZero())
}

func TestMediaHistoryTracker_Listen_MultipleNotifications(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform)
	fakeClock := clockwork.NewFakeClock()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	// Record the initial time
	initialTime := fakeClock.Now()

	tracker := &mediaHistoryTracker{
		st:    st,
		db:    db,
		clock: fakeClock,
		// Manually set state as if media started 10 seconds ago
		currentHistoryDBID:    1,
		currentMediaStartTime: initialTime,
	}

	// Advance clock by 10 seconds to simulate media playing
	fakeClock.Advance(10 * time.Second)

	// Setup mock expectations for stop -> start sequence
	mockUserDB.On(
		"CloseMediaHistory",
		int64(1),
		mock.AnythingOfType("time.Time"),
		10, // 10 seconds of play time
	).Return(nil).Once()
	mockUserDB.On("AddMediaHistory", mock.Anything).Return(int64(2), nil).Once()

	// Process notifications
	notifChan := make(chan models.Notification, 2)

	// Stop - tracker calculates playTime as clock.Now() - initialTime = 10 seconds
	notifChan <- models.Notification{Method: models.NotificationStopped}

	// Re-launch - tracker records new start time
	activeMedia := &models.ActiveMedia{
		Started:    fakeClock.Now(),
		SystemID:   "nes",
		SystemName: "Nintendo Entertainment System",
		Path:       "/games/mario.nes",
		Name:       "Super Mario Bros.",
		LauncherID: "retroarch",
	}
	st.SetActiveMedia(activeMedia)
	notifChan <- models.Notification{Method: models.NotificationStarted}

	close(notifChan)

	tracker.listen(notifChan)

	// Verify
	mockUserDB.AssertExpectations(t)
	// After sequence, should have a new active history entry
	assert.Equal(t, int64(2), tracker.currentHistoryDBID)
}

func TestMediaHistoryTracker_UpdatePlayTime(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform)
	fakeClock := clockwork.NewFakeClock()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	startTime := fakeClock.Now()
	tracker := &mediaHistoryTracker{
		st:                    st,
		db:                    db,
		clock:                 fakeClock,
		currentHistoryDBID:    42,
		currentMediaStartTime: startTime,
	}

	// Setup mock expectations - should be called when ticker fires
	mockUserDB.On("UpdateMediaHistoryTime", int64(42), 120). // 2 minutes = 120 seconds
									Return(nil)

	// Create context with cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Execute in goroutine
	done := make(chan bool)
	go func() {
		tracker.updatePlayTime(ctx)
		done <- true
	}()

	// Wait for goroutine to reach the select statement
	err := fakeClock.BlockUntilContext(ctx, 1)
	require.NoError(t, err)

	// Advance clock by 2 minutes to trigger the ticker
	fakeClock.Advance(2 * time.Minute)

	// Give time for the update to process
	time.Sleep(10 * time.Millisecond)

	// Cancel context to stop the goroutine
	cancel()

	// Wait for goroutine to exit
	<-done

	// Verify - should have been called once
	mockUserDB.AssertExpectations(t)
}

func TestMediaHistoryTracker_UpdatePlayTime_NoActiveMedia(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform)
	fakeClock := clockwork.NewFakeClock()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	tracker := &mediaHistoryTracker{
		st:                 st,
		db:                 db,
		clock:              fakeClock,
		currentHistoryDBID: 0, // No active media
	}

	// Create context with cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Execute in goroutine
	done := make(chan bool)
	go func() {
		tracker.updatePlayTime(ctx)
		done <- true
	}()

	// Wait for goroutine to reach the select
	err := fakeClock.BlockUntilContext(ctx, 1)
	require.NoError(t, err)

	// Advance clock by 1 minute
	fakeClock.Advance(1 * time.Minute)

	// Give time for the ticker to fire
	time.Sleep(10 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for goroutine to exit
	<-done

	// Verify - UpdateMediaHistoryTime should not be called
	mockUserDB.AssertNotCalled(t, "UpdateMediaHistoryTime", mock.Anything, mock.Anything)
}

func TestMediaHistoryTracker_UpdatePlayTime_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform)
	fakeClock := clockwork.NewFakeClock()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	tracker := &mediaHistoryTracker{
		st:                    st,
		db:                    db,
		clock:                 fakeClock,
		currentHistoryDBID:    42,
		currentMediaStartTime: fakeClock.Now(),
	}

	// Create context that we'll cancel immediately
	ctx, cancel := context.WithCancel(context.Background())

	// Start updatePlayTime in goroutine
	done := make(chan bool)
	go func() {
		tracker.updatePlayTime(ctx)
		done <- true
	}()

	// Cancel context immediately
	cancel()

	// Wait for goroutine to exit
	select {
	case <-done:
		// Success - goroutine exited
	case <-time.After(1 * time.Second):
		t.Fatal("updatePlayTime did not exit after context cancellation")
	}
}

func TestMediaHistoryTracker_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform)
	fakeClock := clockwork.NewFakeClock()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	tracker := &mediaHistoryTracker{
		st:    st,
		db:    db,
		clock: fakeClock,
	}

	// Create active media
	activeMedia := &models.ActiveMedia{
		Started:    fakeClock.Now(),
		SystemID:   "nes",
		SystemName: "Nintendo Entertainment System",
		Path:       "/games/mario.nes",
		Name:       "Super Mario Bros.",
		LauncherID: "retroarch",
	}

	st.SetActiveMedia(activeMedia)

	// Setup mocks
	mockUserDB.On("AddMediaHistory", mock.Anything).Return(int64(1), nil)
	mockUserDB.On("UpdateMediaHistoryTime", mock.Anything, mock.Anything).Return(nil)

	// Create notification channel
	notifChan := make(chan models.Notification, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start both goroutines concurrently
	listenerDone := make(chan bool)
	updaterDone := make(chan bool)

	// Start listener
	go func() {
		tracker.listen(notifChan)
		listenerDone <- true
	}()

	// Start updater
	go func() {
		tracker.updatePlayTime(ctx)
		updaterDone <- true
	}()

	// Send started notification
	notifChan <- models.Notification{Method: models.NotificationStarted}

	// Wait for updater to be waiting on ticker
	err := fakeClock.BlockUntilContext(ctx, 1)
	require.NoError(t, err)

	// Advance clock to trigger update
	fakeClock.Advance(1 * time.Minute)

	// Give time for the update to process
	time.Sleep(10 * time.Millisecond)

	// Close notification channel and cancel context
	close(notifChan)
	cancel()

	// Wait for goroutines to finish
	<-listenerDone
	<-updaterDone

	// Verify no panics occurred (test for race conditions)
	// The fact that we got here without panicking means mutex is working correctly
	require.NotNil(t, tracker)
}
