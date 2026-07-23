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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	backupsvc "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
)

type remoteHeartbeatState struct {
	lastSuccess time.Time
	nextAttempt time.Time
	backoff     time.Duration
}

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
	remoteHeartbeatInterval       = 24 * time.Hour
	remoteHeartbeatInitialBackoff = 1 * time.Minute
	remoteHeartbeatMaxBackoff     = 1 * time.Hour
	// remoteBackupStaleAfter is how long scheduling may go without a
	// successful run before the user is told via the inbox. The inbox
	// message wording assumes roughly a week.
	remoteBackupStaleAfter = 7 * 24 * time.Hour
	// remoteBackupStaleNoticeInterval paces re-posting the (category-
	// deduplicated) stale notice while the condition persists.
	remoteBackupStaleNoticeInterval = 24 * time.Hour
)

type staleNoticeState struct {
	notifiedAt time.Time
}

// shouldNotify reports whether the stale notice should be posted now. It
// re-arms as soon as staleness clears, so a later stale episode notifies
// again.
func (s *staleNoticeState) shouldNotify(now time.Time, stale bool) bool {
	if !stale {
		s.notifiedAt = time.Time{}
		return false
	}
	if !s.notifiedAt.IsZero() && now.Sub(s.notifiedAt) < remoteBackupStaleNoticeInterval {
		return false
	}
	s.notifiedAt = now
	return true
}

func (s *remoteHeartbeatState) due(now time.Time) bool {
	if !s.lastSuccess.IsZero() && now.Sub(s.lastSuccess) < remoteHeartbeatInterval {
		return false
	}
	return s.nextAttempt.IsZero() || !now.Before(s.nextAttempt)
}

func (s *remoteHeartbeatState) recordFailure(now time.Time) {
	if s.backoff <= 0 {
		s.backoff = remoteHeartbeatInitialBackoff
	}
	s.nextAttempt = now.Add(s.backoff)
	s.backoff = min(s.backoff*2, remoteHeartbeatMaxBackoff)
}

func (s *remoteHeartbeatState) recordSuccess(now time.Time) {
	s.lastSuccess = now
	s.nextAttempt = time.Time{}
	s.backoff = remoteHeartbeatInitialBackoff
}

func startRemoteBackupScheduler(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	st *state.State,
	idleSched *idle.Scheduler,
	pauser *syncutil.Pauser,
	wg *sync.WaitGroup,
) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		remoteBackupSchedulerLoop(ctx, cfg, pl, db, st, idleSched, pauser, time.NewTicker)
	}()
}

func remoteBackupSchedulerLoop(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	st *state.State,
	idleSched *idle.Scheduler,
	pauser *syncutil.Pauser,
	newTicker func(time.Duration) *time.Ticker,
) {
	// A run interrupted by power loss or a hard shutdown left "running" in
	// the status file; record it as failed so it retries on the short
	// failure interval instead of waiting out the full schedule cadence.
	// The shared coordinator lets recovery skip a run that started between
	// the API coming up and this pass.
	recoveryMgr := backupsvc.NewManager(cfg, pl, db)
	if st != nil {
		recoveryMgr.WithCoordinator(st.BackupCoordinator())
	}
	recoveryMgr.RecoverInterruptedRuns()

	var scheduled atomic.Bool
	trySchedule := func() {
		// While a pause-tier core (CD-based) is running, don't start at all:
		// a run would immediately block on the pauser while holding the
		// backup coordinator lease. The next tick retries; throttled states
		// still run and make slow progress.
		if scheduled.Load() || pauser.IsPaused() || !scheduledRemoteBackupDue(time.Now(), cfg, pl, db) {
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
				runScheduledRemoteBackup(taskCtx, cfg, pl, db, st, pauser)
			},
		)
	}

	heartbeatState := remoteHeartbeatState{backoff: remoteHeartbeatInitialBackoff}
	tryHeartbeat := func() {
		now := time.Now()
		if !heartbeatState.due(now) {
			return
		}
		mgr := backupsvc.NewManager(cfg, pl, db).WithCoordinator(st.BackupCoordinator())
		if err := mgr.SendHeartbeat(ctx); err != nil {
			// Not linked or unreachable: fine, heartbeats are best-effort.
			heartbeatState.recordFailure(now)
			log.Debug().Err(err).Msg("remote heartbeat not sent")
			return
		}
		heartbeatState.recordSuccess(now)
	}

	staleState := staleNoticeState{}
	tryStaleNotice := func() {
		now := time.Now()
		active := cfg.BackupRemoteEnabled() && cfg.BackupRemoteSchedule() != "manual"
		mgr := backupsvc.NewManager(cfg, pl, db).
			WithCoordinator(st.BackupCoordinator()).WithInbox(st.Inbox())
		if staleState.shouldNotify(now, mgr.TrackScheduleStale(now, active, remoteBackupStaleAfter)) {
			mgr.NotifyScheduleStale()
		}
	}

	tryHeartbeat()
	trySchedule()
	tryStaleNotice()
	ticker := newTicker(remoteBackupSchedulerCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			tryHeartbeat()
			trySchedule()
			tryStaleNotice()
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
	if status.Remote.Availability != backupsvc.RemoteAvailabilityAvailable &&
		!backupsvc.RemoteAvailabilityNeedsRefresh(now, &status.Remote) {
		return false
	}
	return remoteBackupDue(now, &status.Remote, schedule)
}

func runScheduledRemoteBackup(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	st *state.State,
	pauser *syncutil.Pauser,
) {
	if pauser.IsPaused() || !scheduledRemoteBackupDue(time.Now(), cfg, pl, db) {
		return
	}
	log.Info().Str("schedule", cfg.BackupRemoteSchedule()).Msg("running scheduled remote backup")
	mgr := backupsvc.NewManager(cfg, pl, db).WithPauser(pauser)
	if st != nil {
		if _, _, active := st.BackupCoordinator().Active(); active {
			log.Debug().Msg("skipping scheduled remote backup while backup service is busy")
			return
		}
		mgr.WithCoordinator(st.BackupCoordinator()).WithInbox(st.Inbox())
	}
	availability, refreshErr := mgr.RefreshRemoteAvailability(ctx)
	if refreshErr != nil {
		log.Debug().Err(refreshErr).Msg("scheduled remote backup eligibility refresh failed")
		return
	}
	if availability != backupsvc.RemoteAvailabilityAvailable {
		log.Info().Str("availability", availability).Msg("skipping scheduled remote backup")
		return
	}
	info, err := mgr.RunRemote(ctx, backupsvc.RemoteBackupTypeScheduled)
	if err != nil {
		log.Warn().Err(err).Msg("scheduled remote backup failed")
		return
	}
	log.Info().
		Int("uploadedFiles", info.UploadedFiles).
		Int("dedupedFiles", info.DedupedFiles).
		Int64("uploadedBytes", info.UploadedBytes).
		Bool("noChanges", info.NoChanges).
		Str("backupID", info.Backup.ID).
		Time("snapshotCreatedAt", info.Backup.CreatedAt).
		Msg("scheduled remote backup completed")
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
