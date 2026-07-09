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
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	backupsvc "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
)

const (
	remoteBackupSchedulerCheckInterval = 1 * time.Minute
	remoteBackupIdleQuietWindow        = 5 * time.Second
	remoteBackupIdleMaxWait            = 300 * time.Second
	// remoteBackupFailureRetryInterval is how soon a failed scheduled run is
	// retried, instead of waiting out the full daily/weekly interval.
	remoteBackupFailureRetryInterval = 1 * time.Hour
	// remoteHeartbeatInterval paces liveness reports to the remote API. A
	// heartbeat is also sent when the service starts. Heartbeats run for any
	// linked device, independent of whether remote backup is enabled.
	remoteHeartbeatInterval = 24 * time.Hour
)

func startRemoteBackupScheduler(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	st *state.State,
	idleSched *idle.Scheduler,
	wg *sync.WaitGroup,
) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		remoteBackupSchedulerLoop(ctx, cfg, pl, db, st, idleSched, time.NewTicker)
	}()
}

func remoteBackupSchedulerLoop(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	st *state.State,
	idleSched *idle.Scheduler,
	newTicker func(time.Duration) *time.Ticker,
) {
	var scheduled atomic.Bool
	trySchedule := func() {
		if scheduled.Load() || !scheduledRemoteBackupDue(time.Now(), cfg, pl, db) {
			return
		}
		scheduled.Store(true)
		idleSched.Schedule(
			ctx,
			"remote-backup",
			remoteBackupIdleQuietWindow,
			remoteBackupIdleMaxWait,
			func(taskCtx context.Context) {
				defer scheduled.Store(false)
				runScheduledRemoteBackup(taskCtx, cfg, pl, db, st)
			},
		)
	}

	var lastHeartbeat time.Time
	tryHeartbeat := func() {
		if !lastHeartbeat.IsZero() && time.Since(lastHeartbeat) < remoteHeartbeatInterval {
			return
		}
		lastHeartbeat = time.Now()
		if err := backupsvc.NewManager(cfg, pl, db).SendHeartbeat(ctx); err != nil {
			// Not linked or unreachable: fine, heartbeats are best-effort.
			log.Debug().Err(err).Msg("remote heartbeat not sent")
		}
	}

	tryHeartbeat()
	trySchedule()
	ticker := newTicker(remoteBackupSchedulerCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			tryHeartbeat()
			trySchedule()
		case <-ctx.Done():
			return
		}
	}
}

func scheduledRemoteBackupDue(
	now time.Time,
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
) bool {
	if !cfg.BackupRemoteEnabled() {
		return false
	}
	schedule := cfg.BackupRemoteSchedule()
	if schedule == "manual" {
		return false
	}
	status := backupsvc.NewManager(cfg, pl, db).Status()
	return remoteBackupDue(now, &status.Remote, schedule)
}

func runScheduledRemoteBackup(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	st *state.State,
) {
	if !scheduledRemoteBackupDue(time.Now(), cfg, pl, db) {
		return
	}
	log.Info().Str("schedule", cfg.BackupRemoteSchedule()).Msg("running scheduled remote backup")
	mgr := backupsvc.NewManager(cfg, pl, db)
	if st != nil {
		mgr.WithInbox(st.Inbox())
	}
	if _, err := mgr.RunRemote(ctx, backupsvc.RemoteBackupTypeScheduled); err != nil {
		log.Warn().Err(err).Msg("scheduled remote backup failed")
		return
	}
	log.Info().Msg("scheduled remote backup completed")
}

func remoteBackupDue(now time.Time, status *models.BackupStatusEntry, schedule string) bool {
	if !helpers.IsClockReliable(now) {
		return false
	}
	interval, ok := remoteBackupScheduleInterval(schedule)
	if !ok {
		return false
	}
	// A failed run retries sooner than the normal cadence; success resets
	// to the full interval.
	if status != nil && status.LastStatus == backupsvc.StatusFailed &&
		remoteBackupFailureRetryInterval < interval {
		interval = remoteBackupFailureRetryInterval
	}
	lastRun, lastRunOK := reliableStatusTime(statusLastRunAt(status))
	lastSuccess, lastSuccessOK := reliableStatusTime(statusLastSuccessAt(status))
	switch {
	case lastRunOK && lastSuccessOK:
		if lastSuccess.After(lastRun) {
			return !now.Before(lastSuccess.Add(interval))
		}
		return !now.Before(lastRun.Add(interval))
	case lastRunOK:
		return !now.Before(lastRun.Add(interval))
	case lastSuccessOK:
		return !now.Before(lastSuccess.Add(interval))
	default:
		return true
	}
}

func statusLastRunAt(status *models.BackupStatusEntry) *string {
	if status == nil {
		return nil
	}
	return status.LastRunAt
}

func statusLastSuccessAt(status *models.BackupStatusEntry) *string {
	if status == nil {
		return nil
	}
	return status.LastSuccessAt
}

func reliableStatusTime(raw *string) (time.Time, bool) {
	if raw == nil || *raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, *raw)
	if err != nil || !helpers.IsClockReliable(parsed) {
		return time.Time{}, false
	}
	return parsed, true
}

func remoteBackupScheduleInterval(schedule string) (time.Duration, bool) {
	switch schedule {
	case "daily":
		return 24 * time.Hour, true
	case "weekly":
		return 7 * 24 * time.Hour, true
	default:
		return 0, false
	}
}
