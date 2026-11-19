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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	stateservice "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoBackupActive(t *testing.T) {
	t.Parallel()
	off := false
	tests := []struct {
		auto     *bool
		name     string
		schedule string
		enabled  bool
		want     bool
	}{
		{name: "daily", schedule: config.BackupRemoteScheduleDaily, enabled: true, want: true},
		{name: "default schedule", schedule: "", enabled: true, want: true},
		{name: "weekly", schedule: config.BackupRemoteScheduleWeekly, enabled: true, want: true},
		{name: "manual", schedule: config.BackupRemoteScheduleManual, enabled: true},
		{name: "auto backup off", schedule: config.BackupRemoteScheduleDaily, enabled: true, auto: &off},
		{name: "cloud backup off", schedule: config.BackupRemoteScheduleDaily},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
			require.NoError(t, err)
			cfg.SetBackupRemoteEnabled(tt.enabled)
			cfg.SetBackupRemoteSchedule(tt.schedule)
			if tt.auto != nil {
				cfg.SetBackupRemoteAutoBackup(*tt.auto)
			}
			assert.Equal(t, tt.want, autoBackupActive(cfg))
		})
	}
}

func TestForwardMediaStopped(t *testing.T) {
	t.Parallel()
	stopped := func(slot string) models.Notification {
		params, err := json.Marshal(map[string]any{"slot": slot})
		require.NoError(t, err)
		return models.Notification{Method: models.NotificationStopped, Params: params}
	}
	notifChan := make(chan models.Notification, 4)
	requests := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		forwardMediaStopped(t.Context(), notifChan, requests)
		close(done)
	}()

	notifChan <- stopped("background")
	notifChan <- stopped("primary")
	notifChan <- stopped("primary")
	close(notifChan)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("forwarder did not stop when the subscription closed")
	}
	assert.Len(t, requests, 1, "primary stops coalesce into one request; background stops are ignored")
}

// autoBackupLoopEnv runs the scheduler loop against a server that reports the
// device as eligible to the startup heartbeat and not eligible after that, so
// each backup run ends after its eligibility check. Every run therefore shows
// up as one /v1/device/me call beyond the startup heartbeat's.
type autoBackupLoopEnv struct {
	mediaStopped chan struct{}
	pauser       *syncutil.Pauser
	deviceChecks *atomic.Int32
}

func startAutoBackupLoop(t *testing.T, enabled, auto bool, schedule string) *autoBackupLoopEnv {
	t.Helper()
	rootDir := t.TempDir()
	deviceChecks := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
			// The startup heartbeat's check keeps the device eligible; later
			// checks refuse, so a run stops before collecting files.
			first := deviceChecks.Add(1) == 1
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"id": "device-1", "name": "test", "backup_active": first,
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg, err := config.NewConfig(rootDir, config.BaseDefaults)
	require.NoError(t, err)
	require.NoError(t, cfg.SetOnlineBaseURL(server.URL))
	cfg.SetBackupRemoteEnabled(enabled)
	cfg.SetBackupRemoteSchedule(schedule)
	cfg.SetBackupRemoteAutoBackup(auto)
	cfg.SetPlaytimeSync(false)
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.RemoteAuthLookupURL(server.URL): {Bearer: "test-token"},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)

	// A backup that just succeeded keeps the schedule's own run from being
	// due, so any run seen here was queued by a stopped game.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	status, err := json.Marshal(map[string]any{
		"remote": map[string]any{
			"lastStatus":            backupsvc.StatusSuccess,
			"lastRunAt":             now,
			"lastSuccessAt":         now,
			"availability":          backupsvc.RemoteAvailabilityAvailable,
			"availabilityCheckedAt": now,
		},
	})
	require.NoError(t, err)
	statusPath := filepath.Join(rootDir, "backups", "status.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(statusPath), 0o750))
	require.NoError(t, os.WriteFile(statusPath, status, 0o600))

	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test-platform").Maybe()
	pl.On("Settings").Return(platforms.Settings{DataDir: rootDir, ConfigDir: rootDir}).Maybe()
	st, _ := stateservice.NewState(pl, "test-boot")
	t.Cleanup(st.StopService)
	db := &database.Database{UserDB: testhelpers.NewMockUserDBI()}

	env := &autoBackupLoopEnv{
		mediaStopped: make(chan struct{}, 1),
		pauser:       syncutil.NewPauser(),
		deviceChecks: deviceChecks,
	}
	// The scheduler counts its own start as API activity. A clock already
	// past the quiet window lets queued runs start at once.
	idleClock := clockwork.NewFakeClock()
	idleSched := idle.NewWithClock(idleClock)
	idleClock.Advance(remoteBackupIdleQuietWindow)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		remoteBackupSchedulerLoop(
			ctx, cfg, pl, db, st, idleSched, env.pauser, make(chan struct{}),
			autoBackupTiming{settle: 10 * time.Millisecond, retry: 10 * time.Millisecond},
			env.mediaStopped,
			func(time.Duration) *time.Ticker { return time.NewTicker(time.Hour) },
		)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("backup scheduler did not stop after cancellation")
		}
		idleSched.Wait()
	})

	require.Eventually(t, func() bool { return deviceChecks.Load() == 1 },
		5*time.Second, 5*time.Millisecond, "startup heartbeat checks the device once")
	return env
}

func TestRemoteBackupScheduler_AutoBackupQueuesRun(t *testing.T) {
	// No t.Parallel(): the auth config is global.
	for _, schedule := range []string{config.BackupRemoteScheduleDaily, config.BackupRemoteScheduleWeekly} {
		t.Run(schedule, func(t *testing.T) {
			env := startAutoBackupLoop(t, true, true, schedule)
			env.mediaStopped <- struct{}{}
			require.Eventually(t, func() bool { return env.deviceChecks.Load() == 2 },
				5*time.Second, 5*time.Millisecond, "a stopped game queues a backup")
		})
	}
}

func TestRemoteBackupScheduler_AutoBackupInactive(t *testing.T) {
	// No t.Parallel(): the auth config is global.
	tests := []struct {
		name     string
		schedule string
		enabled  bool
		auto     bool
	}{
		{name: "manual schedule", schedule: config.BackupRemoteScheduleManual, enabled: true, auto: true},
		{name: "auto backup off", schedule: config.BackupRemoteScheduleDaily, enabled: true},
		{name: "cloud backup off", schedule: config.BackupRemoteScheduleDaily, auto: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := startAutoBackupLoop(t, tt.enabled, tt.auto, tt.schedule)
			env.mediaStopped <- struct{}{}
			require.Never(t, func() bool { return env.deviceChecks.Load() > 1 },
				300*time.Millisecond, 10*time.Millisecond, "a stopped game queues nothing")
		})
	}
}

func TestRemoteBackupScheduler_AutoBackupWaitsOutPause(t *testing.T) {
	// No t.Parallel(): the auth config is global.
	env := startAutoBackupLoop(t, true, true, config.BackupRemoteScheduleDaily)
	env.pauser.Pause()
	env.mediaStopped <- struct{}{}
	require.Never(t, func() bool { return env.deviceChecks.Load() > 1 },
		300*time.Millisecond, 10*time.Millisecond, "no backup starts while a paused game runs")
	env.pauser.Resume()
	require.Eventually(t, func() bool { return env.deviceChecks.Load() == 2 },
		5*time.Second, 5*time.Millisecond, "the Auto-Backup run starts once the pause lifts")
}
