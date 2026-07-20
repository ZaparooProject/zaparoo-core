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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	backupsvc "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/stretchr/testify/assert"
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
