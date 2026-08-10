/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	backupsvc "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	stateservice "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestRemoteBackupDue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	unreliableNow := time.Date(1970, 1, 1, 12, 0, 0, 0, time.UTC)
	unreliableStored := time.Date(1970, 1, 1, 1, 0, 0, 0, time.UTC)

	tests := []struct {
		lastRun     *string
		lastSuccess *string
		now         time.Time
		schedule    string
		name        string
		want        bool
	}{
		{name: "unreliable now never due", now: unreliableNow, schedule: "daily", want: false},
		{name: "no run daily due", now: now, schedule: "daily", want: true},
		{name: "manual never due", now: now, schedule: "manual", want: false},
		{name: "invalid never due", now: now, schedule: "hourly", want: false},
		{
			name: "daily too recent", now: now, schedule: "daily",
			lastRun: backupTime(now.Add(-23 * time.Hour)), want: false,
		},
		{name: "daily due", now: now, schedule: "daily", lastRun: backupTime(now.Add(-24 * time.Hour)), want: true},
		{
			name: "weekly too recent", now: now, schedule: "weekly",
			lastRun: backupTime(now.AddDate(0, 0, -6)), want: false,
		},
		{name: "weekly due", now: now, schedule: "weekly", lastRun: backupTime(now.AddDate(0, 0, -7)), want: true},
		{name: "invalid timestamp due", now: now, schedule: "daily", lastRun: stringPtr("bad-time"), want: true},
		{
			name: "unreliable stored timestamp ignored", now: now, schedule: "daily",
			lastRun: backupTime(unreliableStored), want: true,
		},
		{
			name: "newer last success controls due check",
			now:  now, schedule: "daily",
			lastRun: backupTime(now.Add(-48 * time.Hour)), lastSuccess: backupTime(now.Add(-23 * time.Hour)),
			want: false,
		},
		{
			name: "newer last run controls due check",
			now:  now, schedule: "daily",
			lastRun: backupTime(now.Add(-23 * time.Hour)), lastSuccess: backupTime(now.Add(-48 * time.Hour)),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			status := models.BackupStatusEntry{LastRunAt: tt.lastRun, LastSuccessAt: tt.lastSuccess}
			assert.Equal(t, tt.want, remoteBackupDue(tt.now, &status, tt.schedule))
		})
	}
}

func TestRemoteBackupDueRetriesSoonerAfterFailure(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	failedRecently := models.BackupStatusEntry{
		LastStatus: backupsvc.StatusFailed,
		LastRunAt:  backupTime(now.Add(-30 * time.Minute)),
	}
	assert.False(t, remoteBackupDue(now, &failedRecently, "daily"),
		"a failed run still backs off for the retry interval")

	failedAWhileAgo := models.BackupStatusEntry{
		LastStatus: backupsvc.StatusFailed,
		LastRunAt:  backupTime(now.Add(-2 * time.Hour)),
	}
	assert.True(t, remoteBackupDue(now, &failedAWhileAgo, "daily"),
		"a failed run retries after the failure interval, not the full schedule")

	succeededRecently := models.BackupStatusEntry{
		LastStatus: backupsvc.StatusSuccess,
		LastRunAt:  backupTime(now.Add(-2 * time.Hour)),
	}
	assert.False(t, remoteBackupDue(now, &succeededRecently, "daily"),
		"success resets to the normal schedule interval")
}

func TestOnlineFailureRequiresWarning(t *testing.T) {
	t.Parallel()

	failure := errors.New("remote request failed")
	tests := []struct {
		err      error
		name     string
		expected bool
		want     bool
	}{
		{name: "nil", want: false},
		{name: "expected inactivity", err: failure, expected: true, want: false},
		{name: "shutdown cancellation", err: context.Canceled, want: false},
		{name: "wrapped shutdown cancellation", err: errors.Join(failure, context.Canceled), want: false},
		{name: "actionable failure", err: failure, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, onlineFailureRequiresWarning(tt.err, tt.expected))
		})
	}
}

func TestRemoteHeartbeatStateBacksOffFailuresAndRecordsOnlySuccess(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	state := remoteHeartbeatState{backoff: remoteHeartbeatInitialBackoff}
	assert.True(t, state.due(now))

	state.recordFailure(now)
	assert.True(t, state.lastSuccess.IsZero(), "failure must not advance heartbeat success time")
	assert.False(t, state.due(now.Add(30*time.Second)))
	assert.True(t, state.due(now.Add(remoteHeartbeatInitialBackoff)))
	assert.Equal(t, 2*remoteHeartbeatInitialBackoff, state.backoff)

	succeededAt := now.Add(remoteHeartbeatInitialBackoff)
	state.recordSuccess(succeededAt)
	assert.Equal(t, succeededAt, state.lastSuccess)
	assert.False(t, state.due(succeededAt.Add(time.Hour)))
	assert.True(t, state.due(succeededAt.Add(remoteHeartbeatInterval)))
	assert.Equal(t, remoteHeartbeatInitialBackoff, state.backoff)
}

func TestPlaySyncDuePendingBypassesIntervalButHonorsBackoff(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	s := remoteHeartbeatState{lastSuccess: now.Add(-time.Minute)}

	assert.False(t, playSyncDue(&s, now, false), "periodic sync must honor the success interval")
	assert.True(t, playSyncDue(&s, now, true), "completed session must request an immediate sync")

	s.nextAttempt = now.Add(time.Minute)
	assert.False(t, playSyncDue(&s, now, true), "completed session must not bypass failure backoff")
	assert.True(t, playSyncDue(&s, now.Add(time.Minute), true))

	s.idle = true
	s.nextAttempt = time.Time{}
	assert.False(t, playSyncDue(&s, now.Add(time.Minute), true), "pending sync must remain idle while ineligible")
}

func TestRecordPlaySyncError_DoesNotBackoffExpectedInactivity(t *testing.T) {
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{})
	t.Cleanup(config.ClearAuthCfgForTesting)
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	manager := backupsvc.NewManager(cfg, nil, nil)
	_, disabledErr := manager.SyncPlayHistory(t.Context())
	require.Error(t, disabledErr)
	require.True(t, backupsvc.IsPlaySyncDisabledError(disabledErr))
	cfg.SetPlaytimeSync(true)
	_, unlinkedErr := manager.SyncPlayHistory(t.Context())
	require.Error(t, unlinkedErr)
	require.True(t, backupsvc.IsRemoteUnlinkedError(unlinkedErr))

	now := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		err  error
		name string
	}{
		{name: "disabled", err: disabledErr},
		{name: "unlinked", err: unlinkedErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state := remoteHeartbeatState{
				nextAttempt: now.Add(time.Hour),
				backoff:     remoteHeartbeatMaxBackoff,
			}

			assert.True(t, recordPlaySyncError(&state, now, tt.err))
			assert.True(t, state.idle)
			assert.True(t, state.nextAttempt.IsZero(), "eligibility change must allow the next request immediately")
			assert.Equal(t, remoteHeartbeatInitialBackoff, state.backoff)
		})
	}

	state := remoteHeartbeatState{backoff: remoteHeartbeatInitialBackoff}
	networkErr := errors.New("network unavailable")
	assert.False(t, recordPlaySyncError(&state, now, networkErr))
	assert.False(t, state.idle)
	assert.Equal(t, now.Add(remoteHeartbeatInitialBackoff), state.nextAttempt)
	assert.Equal(t, 2*remoteHeartbeatInitialBackoff, state.backoff)
}

func TestRemoteBackupScheduler_PlaySyncRequestBypassesSuccessInterval(t *testing.T) {
	rootDir := t.TempDir()
	var watermarkRequests atomic.Int32
	watermarkSeen := make(chan int32, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "device-1", "name": "test", "backup_active": false,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/play-sessions/watermark":
			count := watermarkRequests.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"watermark": nil})
			watermarkSeen <- count
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg, err := config.NewConfig(rootDir, config.BaseDefaults)
	require.NoError(t, err)
	require.NoError(t, cfg.SetBackupRemoteBaseURL(server.URL))
	require.NoError(t, cfg.SetPlaytimeBaseURL(server.URL))
	cfg.SetBackupRemoteEnabled(false)
	cfg.SetPlaytimeSync(true)
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.RemoteAuthLookupURL(server.URL): {Bearer: "test-token"},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: rootDir, ConfigDir: rootDir,
	}).Maybe()
	st, _ := stateservice.NewState(mockPlatform, "test-boot")
	t.Cleanup(st.StopService)

	mockUserDB := testhelpers.NewMockUserDBI()
	syncFinished := make(chan struct{}, 2)
	mockUserDB.On("ResetMediaHistorySyncAfter", (*time.Time)(nil)).Return(nil).Twice()
	mockUserDB.On("GetMediaHistorySyncBatch", time.Time{}, int64(0), mock.Anything).
		Run(func(mock.Arguments) { syncFinished <- struct{}{} }).
		Return([]database.MediaHistoryEntry{}, nil).Twice()
	db := &database.Database{UserDB: mockUserDB}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	playSyncRequests := make(chan struct{}, 1)
	done := make(chan struct{})
	// Remote backup is disabled, so this loop exercises heartbeat and play
	// sync only; the idle scheduler must never be reached.
	go func() {
		remoteBackupSchedulerLoop(
			ctx, cfg, mockPlatform, db, st, nil, syncutil.NewPauser(), playSyncRequests,
			func(time.Duration) *time.Ticker { return time.NewTicker(time.Hour) },
		)
		close(done)
	}()

	waitForWatermark := func(want int32) {
		t.Helper()
		select {
		case got := <-watermarkSeen:
			assert.Equal(t, want, got)
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for play sync request %d", want)
		}
	}
	waitForSync := func() {
		t.Helper()
		select {
		case <-syncFinished:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for play sync to finish")
		}
	}
	waitForWatermark(1)
	waitForSync()
	playSyncRequests <- struct{}{}
	waitForWatermark(2)
	waitForSync()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("backup scheduler did not stop after cancellation")
	}
	assert.Equal(t, int32(2), watermarkRequests.Load())
	mockUserDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestRemoteBackupScheduler_PlaySyncExpectedInactivityResumes(t *testing.T) {
	tests := []struct {
		name             string
		initiallyLinked  bool
		initiallyEnabled bool
	}{
		{name: "disabled", initiallyLinked: true, initiallyEnabled: false},
		{name: "unlinked", initiallyLinked: false, initiallyEnabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.SetAuthCfgForTesting(map[string]config.CredentialEntry{})
			t.Cleanup(config.ClearAuthCfgForTesting)
			rootDir := t.TempDir()
			watermarkSeen := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"id": "device-1", "name": "test", "backup_active": false,
					})
				case r.Method == http.MethodGet && r.URL.Path == "/v1/device/play-sessions/watermark":
					_ = json.NewEncoder(w).Encode(map[string]any{"watermark": nil})
					watermarkSeen <- struct{}{}
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			cfg, err := config.NewConfig(rootDir, config.BaseDefaults)
			require.NoError(t, err)
			require.NoError(t, cfg.SetBackupRemoteBaseURL(server.URL))
			require.NoError(t, cfg.SetPlaytimeBaseURL(server.URL))
			cfg.SetBackupRemoteEnabled(false)
			cfg.SetPlaytimeSync(tt.initiallyEnabled)
			credentials := map[string]config.CredentialEntry{
				config.RemoteAuthLookupURL(server.URL): {Bearer: "test-token"},
			}
			if tt.initiallyLinked {
				config.SetAuthCfgForTesting(credentials)
			}

			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.On("ID").Return("test-platform").Maybe()
			mockPlatform.On("Settings").Return(platforms.Settings{
				DataDir: rootDir, ConfigDir: rootDir,
			}).Maybe()
			st, _ := stateservice.NewState(mockPlatform, "test-boot")
			t.Cleanup(st.StopService)

			mockUserDB := testhelpers.NewMockUserDBI()
			syncFinished := make(chan struct{}, 1)
			mockUserDB.On("ResetMediaHistorySyncAfter", (*time.Time)(nil)).Return(nil).Once()
			mockUserDB.On("GetMediaHistorySyncBatch", time.Time{}, int64(0), mock.Anything).
				Run(func(mock.Arguments) { syncFinished <- struct{}{} }).
				Return([]database.MediaHistoryEntry{}, nil).Once()
			db := &database.Database{UserDB: mockUserDB}

			ctx, cancel := context.WithCancel(t.Context())
			playSyncRequests := make(chan struct{}, 1)
			ready := make(chan struct{})
			done := make(chan struct{})
			go func() {
				remoteBackupSchedulerLoop(
					ctx, cfg, mockPlatform, db, st, nil, syncutil.NewPauser(), playSyncRequests,
					func(time.Duration) *time.Ticker {
						close(ready)
						return time.NewTicker(time.Hour)
					},
				)
				close(done)
			}()
			<-ready
			select {
			case <-watermarkSeen:
				t.Fatal("play sync ran while expected to remain idle")
			default:
			}

			cfg.SetPlaytimeSync(true)
			config.SetAuthCfgForTesting(credentials)
			playSyncRequests <- struct{}{}
			select {
			case <-watermarkSeen:
			case <-time.After(5 * time.Second):
				t.Fatal("play sync did not resume after becoming eligible")
			}
			select {
			case <-syncFinished:
			case <-time.After(5 * time.Second):
				t.Fatal("resumed play sync did not finish")
			}

			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("backup scheduler did not stop after cancellation")
			}
			mockUserDB.AssertExpectations(t)
			mockPlatform.AssertExpectations(t)
		})
	}
}

func TestRemoteBackupScheduleInterval(t *testing.T) {
	t.Parallel()

	daily, ok := remoteBackupScheduleInterval("daily")
	require.True(t, ok)
	assert.Equal(t, 24*time.Hour, daily)

	weekly, ok := remoteBackupScheduleInterval("weekly")
	require.True(t, ok)
	assert.Equal(t, 7*24*time.Hour, weekly)

	_, ok = remoteBackupScheduleInterval("manual")
	assert.False(t, ok)
}

func TestStaleNoticeStateShouldNotify(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	var state staleNoticeState

	assert.False(t, state.shouldNotify(now, false))
	assert.True(t, state.shouldNotify(now, true))
	// Paced: not re-posted within the notice interval.
	assert.False(t, state.shouldNotify(now.Add(time.Hour), true))
	assert.True(t, state.shouldNotify(now.Add(remoteBackupStaleNoticeInterval), true))
	// Clearing staleness re-arms for the next episode.
	assert.False(t, state.shouldNotify(now.Add(25*time.Hour), false))
	assert.True(t, state.shouldNotify(now.Add(26*time.Hour), true))
}

func backupTime(t time.Time) *string {
	return stringPtr(t.Format(time.RFC3339Nano))
}

func stringPtr(s string) *string { return &s }

func TestScheduledRemoteBackupDueUsesAvailabilityFreshness(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	rootDir := t.TempDir()
	cfg, err := config.NewConfig(rootDir, config.BaseDefaults)
	require.NoError(t, err)
	cfg.SetBackupRemoteEnabled(true)
	cfg.SetBackupRemoteSchedule("daily")
	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: rootDir})

	statusPath := filepath.Join(rootDir, "backups", "status.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(statusPath), 0o750))
	writeUnavailableStatus := func(checkedAt time.Time) {
		t.Helper()
		data, marshalErr := json.Marshal(map[string]any{
			"remote": map[string]any{
				"lastStatus":            backupsvc.StatusNever,
				"availability":          backupsvc.RemoteAvailabilityUnavailable,
				"availabilityCheckedAt": checkedAt.Format(time.RFC3339Nano),
			},
		})
		require.NoError(t, marshalErr)
		require.NoError(t, os.WriteFile(statusPath, data, 0o600))
	}

	writeUnavailableStatus(now.Add(-time.Minute))
	assert.False(t, scheduledRemoteBackupDue(now, cfg, pl, nil),
		"fresh unavailable state must suppress scheduled work")

	writeUnavailableStatus(now.Add(-10 * time.Minute))
	assert.True(t, scheduledRemoteBackupDue(now, cfg, pl, nil),
		"stale unavailable state must schedule work so eligibility can refresh")
	pl.AssertExpectations(t)
}

func TestRunScheduledRemoteBackupSkipsWhilePaused(t *testing.T) {
	t.Parallel()
	pauser := syncutil.NewPauser()
	pauser.Pause()
	// The pause gate must be checked before anything else: the nil platform
	// and database would panic if the run proceeded past it.
	runScheduledRemoteBackup(context.Background(), nil, nil, nil, nil, pauser)
}
