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

package service

import (
	"context"
	"errors"
	"path/filepath"
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
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
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

	// Validate test data is properly formed
	testhelpers.AssertValidActiveMedia(t, activeMedia)

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

func TestMediaHistoryTracker_Listen_StartedRequestsImmediatePlaySync(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	fakeClock := clockwork.NewFakeClock()
	syncRequests := 0
	tracker := &mediaHistoryTracker{
		st:              st,
		db:              &database.Database{UserDB: mockUserDB},
		clock:           fakeClock,
		requestPlaySync: func() { syncRequests++ },
	}
	activeMedia := &models.ActiveMedia{
		Started: fakeClock.Now(), SystemID: "nes", SystemName: "Nintendo Entertainment System",
		Path: filepath.Join("games", "mario.nes"), Name: "Super Mario Bros.", LauncherID: "retroarch",
	}
	st.SetActiveMedia(activeMedia)
	mockUserDB.On("AddMediaHistory", mock.Anything).Return(int64(42), nil).Once()

	notifChan := make(chan models.Notification, 1)
	notifChan <- models.Notification{Method: models.NotificationStarted}
	close(notifChan)
	tracker.listen(notifChan)

	mockUserDB.AssertExpectations(t)
	assert.Equal(t, 1, syncRequests)
	assert.Equal(t, fakeClock.Now(), tracker.lastPlaySyncRequestAt)
}

func TestMediaHistoryTracker_Listen_StartedResolvesIdentityAsynchronously(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := testhelpers.NewMockMediaDBI()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	syncRequests := make(chan struct{}, 2)
	tracker := &mediaHistoryTracker{
		st:              st,
		db:              &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		clock:           clockwork.NewFakeClock(),
		requestPlaySync: func() { syncRequests <- struct{}{} },
	}
	activeMedia := &models.ActiveMedia{
		Started: tracker.clock.Now(), SystemID: "NES", SystemName: "Nintendo Entertainment System",
		Path: filepath.Join("roms", "NES", "Game.nes"), Name: "Launch Name", LauncherID: "NES",
	}
	st.SetActiveMedia(activeMedia)

	lookupStarted := make(chan struct{})
	releaseLookup := make(chan struct{})
	identityUpdated := make(chan struct{})
	mockUserDB.On("AddMediaHistory", mock.MatchedBy(func(entry *database.MediaHistoryEntry) bool {
		return entry.SystemID == "NES" && entry.MediaPath == activeMedia.Path &&
			entry.MediaName == "Launch Name" && len(entry.Tags) == 0
	})).Return(int64(42), nil).Once()
	mockMediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, activeMedia.Path).
		Run(func(_ mock.Arguments) {
			close(lookupStarted)
			<-releaseLookup
		}).
		Return([]database.SearchResult{{
			SystemID: "NES", MediaID: 7, Name: "Indexed Name", Slug: "indexedname",
		}}, nil).Once()
	mockMediaDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(7)).
		Return([]database.TagInfo(nil), nil).Once()
	mockUserDB.On("UpdateMediaHistoryIdentity", int64(42), mock.MatchedBy(
		func(identity *database.MediaIdentity) bool {
			return identity != nil && identity.CanonicalSystemID == "NES" &&
				identity.DisplayName == "Indexed Name" &&
				identity.CoreSlug == "indexedname" &&
				identity.PolicyVersion == database.CurrentMediaIdentityPolicyVersion &&
				len(identity.Tags) == 0 && identity.ObservationFingerprint != ""
		},
	)).Run(func(_ mock.Arguments) { close(identityUpdated) }).Return(true, nil).Once()

	notifChan := make(chan models.Notification, 1)
	notifChan <- models.Notification{Method: models.NotificationStarted}
	close(notifChan)
	listenDone := make(chan struct{})
	go func() {
		tracker.listen(notifChan)
		close(listenDone)
	}()

	select {
	case <-lookupStarted:
	case <-time.After(time.Second):
		close(releaseLookup)
		t.Fatal("identity lookup did not start")
	}
	select {
	case <-listenDone:
		// Notification processing must not wait for MediaDB lookup.
	case <-time.After(time.Second):
		close(releaseLookup)
		<-listenDone
		t.Fatal("notification processing blocked on identity lookup")
	}
	select {
	case <-syncRequests:
		// Initial now-playing upload remains immediate.
	case <-time.After(time.Second):
		t.Fatal("initial play sync was not requested")
	}

	// Mutating current state cannot retarget the already captured launch identity.
	activeMedia.SystemID = "SNES"
	activeMedia.Path = filepath.Join("roms", "SNES", "Other.sfc")
	close(releaseLookup)
	select {
	case <-identityUpdated:
	case <-time.After(time.Second):
		t.Fatal("resolved identity was not persisted")
	}
	select {
	case <-syncRequests:
		// Enrichment must re-upload a session initially synced without identity.
	case <-time.After(time.Second):
		t.Fatal("identity enrichment did not request play sync")
	}
	mockUserDB.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
}

func TestLookupMediaIdentityWithRetry_BoundsTransientFailures(t *testing.T) {
	t.Parallel()

	identity := database.MediaIdentity{
		MediaType: "Game", CanonicalSystemID: "NES", DisplayName: "Game", CoreSlug: "game",
		ObservationFingerprint: "sha256:test", PolicyVersion: database.CurrentMediaIdentityPolicyVersion,
	}
	t.Run("transient then success", func(t *testing.T) {
		t.Parallel()
		calls := 0
		lookup := func(
			context.Context, database.MediaDBI, string, string,
		) (database.MediaIdentity, bool, error) {
			calls++
			if calls == 1 {
				return database.MediaIdentity{}, false, errors.New("database busy")
			}
			return identity, true, nil
		}

		got, found, err := lookupMediaIdentityWithRetry(
			context.Background(), nil, "NES", filepath.Join("games", "Game.nes"),
			time.Second, []time.Duration{0, 0}, lookup,
		)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, identity, got)
		assert.Equal(t, 2, calls)
	})

	t.Run("exhausted", func(t *testing.T) {
		t.Parallel()
		calls := 0
		lookup := func(
			context.Context, database.MediaDBI, string, string,
		) (database.MediaIdentity, bool, error) {
			calls++
			return database.MediaIdentity{}, false, errors.New("database busy")
		}

		got, found, err := lookupMediaIdentityWithRetry(
			context.Background(), nil, "NES", filepath.Join("games", "Game.nes"),
			time.Second, []time.Duration{0, 0}, lookup,
		)
		require.Error(t, err)
		assert.False(t, found)
		assert.Empty(t, got)
		assert.Equal(t, 3, calls)
	})

	// Shutdown must stop the retry loop where it stands: neither a fresh
	// attempt nor a pending backoff may outlive the context.
	t.Run("cancelled before first attempt", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		calls := 0
		lookup := func(
			context.Context, database.MediaDBI, string, string,
		) (database.MediaIdentity, bool, error) {
			calls++
			return identity, true, nil
		}

		_, found, err := lookupMediaIdentityWithRetry(
			ctx, nil, "NES", filepath.Join("games", "Game.nes"),
			time.Second, []time.Duration{0, 0}, lookup,
		)
		require.Error(t, err)
		assert.False(t, found)
		assert.Equal(t, 0, calls, "a cancelled context must not start a lookup")
	})

	t.Run("cancelled during retry delay", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		calls := 0
		lookup := func(
			context.Context, database.MediaDBI, string, string,
		) (database.MediaIdentity, bool, error) {
			calls++
			cancel()
			return database.MediaIdentity{}, false, errors.New("database busy")
		}

		// An hour of backoff the call must not wait out.
		_, found, err := lookupMediaIdentityWithRetry(
			ctx, nil, "NES", filepath.Join("games", "Game.nes"),
			time.Second, []time.Duration{time.Hour}, lookup,
		)
		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, found)
		assert.Equal(t, 1, calls)
	})

	t.Run("nil lookup uses the production resolver", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join("games", "Game.nes")
		mediaDB := testhelpers.NewMockMediaDBI()
		mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, path).
			Return([]database.SearchResult{}, nil).Once()

		_, found, err := lookupMediaIdentityWithRetry(
			context.Background(), mediaDB, "NES", path, time.Second, nil, nil,
		)
		require.NoError(t, err)
		assert.False(t, found)
		mediaDB.AssertExpectations(t)
	})

	t.Run("unindexed is definitive", func(t *testing.T) {
		t.Parallel()
		calls := 0
		lookup := func(
			context.Context, database.MediaDBI, string, string,
		) (database.MediaIdentity, bool, error) {
			calls++
			return database.MediaIdentity{}, false, nil
		}

		_, found, err := lookupMediaIdentityWithRetry(
			context.Background(), nil, "NES", filepath.Join("games", "Missing.nes"),
			time.Second, []time.Duration{0, 0}, lookup,
		)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Equal(t, 1, calls)
	})
}

func TestMediaHistoryTracker_SnapshotIdentity_UnindexedLaunchKeepsFallback(t *testing.T) {
	t.Parallel()

	path := filepath.Join("outside-index", "Game.nes")
	mockUserDB := testhelpers.NewMockUserDBI()
	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, path).
		Return([]database.SearchResult{}, nil).Once()
	syncRequests := 0
	tracker := &mediaHistoryTracker{
		db:              &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		requestPlaySync: func() { syncRequests++ },
	}

	tracker.snapshotMediaHistoryIdentity(42, "NES", path)

	mockUserDB.AssertNotCalled(t, "UpdateMediaHistoryIdentity", mock.Anything, mock.Anything)
	mockMediaDB.AssertExpectations(t)
	assert.Zero(t, syncRequests)
}

// A snapshot only earns an upload when it actually changed the stored row:
// relaunching unchanged media must not spend a sync, and a failed write must
// not claim one either.
func TestMediaHistoryTracker_SnapshotIdentity_SyncsOnlyOnStoredChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		updateErr    error
		name         string
		updated      bool
		wantRequests int
	}{
		{name: "stored change requests sync", updated: true, wantRequests: 1},
		{name: "unchanged identity is not resynced", updated: false, wantRequests: 0},
		{name: "write failure requests nothing", updateErr: errors.New("disk full"), wantRequests: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join("games", "NES", "Super Game.nes")
			mockMediaDB := testhelpers.NewMockMediaDBI()
			mockMediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, path).
				Return([]database.SearchResult{{
					SystemID: "NES", Name: "Super Game", Path: path,
					Slug: "supergame", MediaID: 7,
				}}, nil).Once()
			mockMediaDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(7)).
				Return([]database.TagInfo{{Tag: "us", Type: "region"}}, nil).Once()

			mockUserDB := testhelpers.NewMockUserDBI()
			mockUserDB.On("UpdateMediaHistoryIdentity", int64(42), mock.MatchedBy(
				func(identity *database.MediaIdentity) bool {
					return identity != nil && identity.CoreSlug == "supergame" &&
						identity.CanonicalSystemID == "NES"
				},
			)).Return(tt.updated, tt.updateErr).Once()

			syncRequests := 0
			tracker := &mediaHistoryTracker{
				db:              &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
				requestPlaySync: func() { syncRequests++ },
			}

			tracker.snapshotMediaHistoryIdentity(42, "NES", path)

			assert.Equal(t, tt.wantRequests, syncRequests)
			mockMediaDB.AssertExpectations(t)
			mockUserDB.AssertExpectations(t)
		})
	}
}

func TestMediaHistoryTracker_Listen_Started_NoActiveMedia(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
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
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
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
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	fakeClock := clockwork.NewFakeClock()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	startTime := fakeClock.Now()
	syncRequests := 0
	tracker := &mediaHistoryTracker{
		st:                    st,
		db:                    db,
		clock:                 fakeClock,
		requestPlaySync:       func() { syncRequests++ },
		currentHistoryDBID:    42,
		currentMediaStartTime: startTime,
		lastPlaySyncRequestAt: startTime,
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
	assert.True(t, tracker.lastPlaySyncRequestAt.IsZero())
	assert.Equal(t, 1, syncRequests)
}

func TestMediaHistoryTracker_Listen_Stopped_NoActiveHistory(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
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
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	fakeClock := clockwork.NewFakeClock()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	startTime := fakeClock.Now()
	syncRequests := 0
	tracker := &mediaHistoryTracker{
		st:                    st,
		db:                    db,
		clock:                 fakeClock,
		requestPlaySync:       func() { syncRequests++ },
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
	// State remains active on close failure so a later stop can retry.
	assert.Equal(t, int64(42), tracker.currentHistoryDBID)
	assert.Equal(t, startTime, tracker.currentMediaStartTime)
	assert.Equal(t, int64(0), tracker.closingHistoryDBID)
	assert.Equal(t, 0, syncRequests)
}

func TestMediaHistoryTracker_Listen_MultipleNotifications(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
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
	testhelpers.AssertValidActiveMedia(t, activeMedia)
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
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
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

func TestMediaHistoryTracker_UpdatePlayTimeRequestsActiveSyncEveryFiveMinutes(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	fakeClock := clockwork.NewFakeClock()
	startTime := fakeClock.Now()
	syncRequested := make(chan struct{}, 1)
	tracker := &mediaHistoryTracker{
		st:                    st,
		db:                    &database.Database{UserDB: mockUserDB},
		clock:                 fakeClock,
		requestPlaySync:       func() { syncRequested <- struct{}{} },
		currentHistoryDBID:    42,
		currentMediaStartTime: startTime,
		lastPlaySyncRequestAt: startTime,
	}
	mockUserDB.On("UpdateMediaHistoryTime", int64(42), 300).Return(nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		tracker.updatePlayTime(ctx)
		close(done)
	}()

	require.NoError(t, fakeClock.BlockUntilContext(ctx, 1))
	fakeClock.Advance(activePlaySyncInterval)
	select {
	case <-syncRequested:
	case <-time.After(time.Second):
		t.Fatal("active play sync was not requested after five minutes")
	}
	cancel()
	<-done

	mockUserDB.AssertExpectations(t)
	assert.Equal(t, fakeClock.Now(), tracker.lastPlaySyncRequestAt)
}

func TestMediaHistoryTracker_RequestActivePlaySyncHonorsIntervalAndDBID(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	syncRequests := 0
	tracker := &mediaHistoryTracker{
		requestPlaySync:       func() { syncRequests++ },
		currentHistoryDBID:    42,
		lastPlaySyncRequestAt: startTime,
	}

	tracker.requestActivePlaySyncIfDue(42, startTime.Add(activePlaySyncInterval-time.Second), false)
	assert.Zero(t, syncRequests)
	assert.Equal(t, startTime, tracker.lastPlaySyncRequestAt)

	tracker.requestActivePlaySyncIfDue(42, startTime.Add(-time.Minute), false)
	assert.Zero(t, syncRequests, "a backward clock jump must remain throttled")
	assert.Equal(t, startTime, tracker.lastPlaySyncRequestAt)

	nextSync := startTime.Add(activePlaySyncInterval)
	tracker.requestActivePlaySyncIfDue(42, nextSync, false)
	assert.Equal(t, 1, syncRequests)
	assert.Equal(t, nextSync, tracker.lastPlaySyncRequestAt)

	tracker.requestActivePlaySyncIfDue(7, nextSync.Add(activePlaySyncInterval), true)
	assert.Equal(t, 1, syncRequests, "a stale DBID must not request sync")
	assert.Equal(t, nextSync, tracker.lastPlaySyncRequestAt)

	tracker.closingHistoryDBID = 42
	tracker.requestActivePlaySyncIfDue(42, nextSync.Add(activePlaySyncInterval), true)
	assert.Equal(t, 1, syncRequests, "a closing history row must not request sync")
	assert.Equal(t, nextSync, tracker.lastPlaySyncRequestAt)
}

func TestMediaHistoryTracker_UpdatePlayTime_NoActiveMedia(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
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

func TestMediaHistoryTracker_UpdatePlayTime_SkipsHistoryBeingClosed(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	fakeClock := clockwork.NewFakeClock()
	db := &database.Database{UserDB: mockUserDB}
	tracker := &mediaHistoryTracker{
		st:                    st,
		db:                    db,
		clock:                 fakeClock,
		currentHistoryDBID:    42,
		closingHistoryDBID:    42,
		currentMediaStartTime: fakeClock.Now(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan bool)

	go func() {
		tracker.updatePlayTime(ctx)
		done <- true
	}()

	err := fakeClock.BlockUntilContext(ctx, 1)
	require.NoError(t, err)
	fakeClock.Advance(time.Minute)
	time.Sleep(10 * time.Millisecond)
	cancel()
	<-done

	mockUserDB.AssertNotCalled(t, "UpdateMediaHistoryTime", mock.Anything, mock.Anything)
}

func TestMediaHistoryTracker_UpdatePlayTime_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Setup
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
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
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
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

// TestMediaHistoryTracker_Listen_OrphanedEntry verifies that receiving media.started
// while a previous DBID is still open closes the orphaned entry before creating a new one.
// This covers the close-before-open path added to prevent orphaned history rows when a
// media.stopped event was dropped (e.g., during an indexing storm).
func TestMediaHistoryTracker_Listen_OrphanedEntry(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	fakeClock := clockwork.NewFakeClock()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	orphanedDBID := int64(10)
	orphanStart := fakeClock.Now()

	// Pre-set tracker state as if a game is currently tracked (orphaned open entry).
	syncRequests := 0
	tracker := &mediaHistoryTracker{
		st:                    st,
		db:                    db,
		clock:                 fakeClock,
		requestPlaySync:       func() { syncRequests++ },
		currentHistoryDBID:    orphanedDBID,
		currentMediaStartTime: orphanStart,
		lastPlaySyncRequestAt: orphanStart,
	}

	// Advance clock so the orphaned entry gets a non-zero play time.
	fakeClock.Advance(30 * time.Second)

	newDBID := int64(11)
	activeMedia := &models.ActiveMedia{
		Started:    fakeClock.Now(),
		SystemID:   "snes",
		SystemName: "Super Nintendo Entertainment System",
		Path:       filepath.Join("games", "zelda.sfc"),
		Name:       "The Legend of Zelda: A Link to the Past",
		LauncherID: "retroarch",
	}
	testhelpers.AssertValidActiveMedia(t, activeMedia)
	st.SetActiveMedia(activeMedia)

	// Expect orphaned entry to be closed (play time = 30s from wall-clock fallback).
	mockUserDB.On(
		"CloseMediaHistory",
		orphanedDBID,
		mock.AnythingOfType("time.Time"),
		30,
	).Return(nil).Once()

	// Expect a new entry to be created for the next game.
	mockUserDB.On("AddMediaHistory", mock.MatchedBy(func(entry *database.MediaHistoryEntry) bool {
		return entry.SystemID == "snes" && entry.MediaName == "The Legend of Zelda: A Link to the Past"
	})).Return(newDBID, nil).Once()

	notifChan := make(chan models.Notification, 1)
	notifChan <- models.Notification{Method: models.NotificationStarted}
	close(notifChan)

	tracker.listen(notifChan)

	mockUserDB.AssertExpectations(t)
	assert.Equal(t, newDBID, tracker.currentHistoryDBID, "new DBID should be set after orphan close")
	assert.Equal(t, fakeClock.Now(), tracker.lastPlaySyncRequestAt)
	assert.Equal(t, 2, syncRequests, "orphan close and new now-playing session both request sync")
}

func TestMediaHistoryTracker_Listen_OrphanedEntryWithoutReplacementResetsSyncState(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	fakeClock := clockwork.NewFakeClock()
	orphanStart := fakeClock.Now()
	syncRequests := 0
	tracker := &mediaHistoryTracker{
		st:                    st,
		db:                    &database.Database{UserDB: mockUserDB},
		clock:                 fakeClock,
		requestPlaySync:       func() { syncRequests++ },
		currentHistoryDBID:    10,
		currentMediaStartTime: orphanStart,
		lastPlaySyncRequestAt: orphanStart,
	}
	fakeClock.Advance(30 * time.Second)
	mockUserDB.On(
		"CloseMediaHistory",
		int64(10),
		mock.AnythingOfType("time.Time"),
		30,
	).Return(nil).Once()

	notifChan := make(chan models.Notification, 1)
	notifChan <- models.Notification{Method: models.NotificationStarted}
	close(notifChan)
	tracker.listen(notifChan)

	mockUserDB.AssertExpectations(t)
	mockUserDB.AssertNotCalled(t, "AddMediaHistory", mock.Anything)
	assert.Zero(t, tracker.currentHistoryDBID)
	assert.True(t, tracker.currentMediaStartTime.IsZero())
	assert.True(t, tracker.currentMediaStartTimeMono.IsZero())
	assert.True(t, tracker.lastPlaySyncRequestAt.IsZero())
	assert.Equal(t, 1, syncRequests)
}
