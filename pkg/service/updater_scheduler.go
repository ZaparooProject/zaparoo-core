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
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	updaterSchedulerTickInterval  = time.Minute
	updaterCheckInterval          = 12 * time.Hour
	updaterFailureInitialBackoff  = 5 * time.Minute
	updaterFailureMaxBackoff      = 6 * time.Hour
	updaterCheckIdleQuietWindow   = 5 * time.Second
	updaterCheckIdleMaxWait       = 300 * time.Second
	updaterInstallIdleQuietWindow = 60 * time.Second
	updaterInstallIdleMaxWait     = 900 * time.Second
)

//nolint:govet // Field groups separate dependencies, state, and concurrency controls.
type updaterScheduler struct {
	cfg          *config.Instance
	platform     platforms.Platform
	db           *database.Database
	state        *state.State
	idle         *idle.Scheduler
	clock        clockwork.Clock
	waitInternet func(context.Context, int) bool
	check        updater.CheckFn
	apply        func(context.Context, updater.Options) (string, error)

	pending          atomic.Pointer[updater.Result]
	checkScheduled   atomic.Bool
	installScheduled atomic.Bool
	checkState       intervalState
	installState     intervalState
	mu               syncutil.Mutex
}

func startUpdaterScheduler(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	st *state.State,
	idleSched *idle.Scheduler,
	wg *sync.WaitGroup,
) {
	scheduler := newUpdaterScheduler(cfg, pl, db, st, idleSched)
	wg.Add(1)
	go func() {
		defer wg.Done()
		scheduler.loop(ctx)
	}()
}

func newUpdaterScheduler(
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	st *state.State,
	idleSched *idle.Scheduler,
) *updaterScheduler {
	return &updaterScheduler{
		cfg:          cfg,
		platform:     pl,
		db:           db,
		state:        st,
		idle:         idleSched,
		clock:        clockwork.NewRealClock(),
		waitInternet: helpers.WaitForInternetContext,
		check:        updater.Check,
		apply:        updater.Apply,
	}
}

func (s *updaterScheduler) loop(ctx context.Context) {
	s.tryCheck(ctx)
	ticker := s.clock.NewTicker(updaterSchedulerTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.Chan():
			s.tryCheck(ctx)
			s.tryInstall(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func updaterInterval(deviceID string) time.Duration {
	if deviceID == "" {
		return updaterCheckInterval
	}
	sum := sha256.Sum256([]byte(deviceID))
	// Map uniformly onto [-10%, +10%], inclusive. The result is stable for a
	// device and does not require wall-clock time.
	spread := int64(updaterCheckInterval / 5)
	offset := int64(binary.BigEndian.Uint64(sum[:8])%uint64(spread+1)) - spread/2 //nolint:gosec // bounded modulo
	return updaterCheckInterval + time.Duration(offset)
}

func (s *updaterScheduler) checkDue(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkState.due(now, updaterInterval(s.cfg.DeviceID()))
}

func (s *updaterScheduler) recordCheck(now time.Time, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.checkState.recordSuccess(now, updaterFailureInitialBackoff)
		return
	}
	s.checkState.recordFailure(now, updaterFailureInitialBackoff, updaterFailureMaxBackoff)
}

func (s *updaterScheduler) installDue(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.installState.due(now, 0)
}

func (s *updaterScheduler) recordInstall(now time.Time, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.installState.recordSuccess(now, updaterFailureInitialBackoff)
		return
	}
	s.installState.recordFailure(now, updaterFailureInitialBackoff, updaterFailureMaxBackoff)
}

func (s *updaterScheduler) tryCheck(ctx context.Context) {
	if s.cfg == nil || !s.cfg.UpdateCheck() || s.installScheduled.Load() ||
		s.checkScheduled.Load() || !s.checkDue(s.clock.Now()) {
		return
	}
	if !s.checkScheduled.CompareAndSwap(false, true) {
		return
	}
	s.idle.Schedule(ctx, "updater-check", updaterCheckIdleQuietWindow, updaterCheckIdleMaxWait,
		func(taskCtx context.Context) {
			defer s.checkScheduled.Store(false)
			if !s.cfg.UpdateCheck() {
				return
			}
			online := false
			var result *updater.Result
			var checkErr error
			opts := serviceUpdaterOptions(s.cfg, s.platform, s.db, updater.ModeAuto)
			opts.Gate = s.gateDeps(nil)
			capture := func(checkCtx context.Context, opts updater.Options) (*updater.Result, error) {
				result, checkErr = s.check(checkCtx, opts)
				return result, checkErr
			}
			updater.CheckAndNotify(
				taskCtx,
				s.cfg,
				opts,
				s.state.Inbox(),
				func(waitCtx context.Context, _ int) bool {
					online = s.waitInternet(waitCtx, 3)
					return online
				},
				capture,
				s.platform.ManagedByPackageManager(),
			)
			if taskCtx.Err() != nil {
				return
			}
			if !online {
				checkErr = errors.New("internet unavailable")
			}
			if errors.Is(checkErr, updater.ErrDevelopmentVersion) {
				checkErr = nil
			}
			if checkErr != nil || result == nil {
				s.pending.Store(nil)
				s.recordCheck(s.clock.Now(), checkErr)
				return
			}
			s.pending.Store(result)
			s.recordCheck(s.clock.Now(), nil)
			s.tryInstall(taskCtx)
		})
}

func autoInstallable(result *updater.Result) bool {
	return result != nil && result.UpdateAvailable && !result.RolloutHeld &&
		result.Eligibility == updater.EligibilityEligible &&
		!updater.PreviouslyRolledBack(result)
}

func (s *updaterScheduler) tryInstall(ctx context.Context) {
	result := s.pending.Load()
	if s.cfg == nil || !s.cfg.UpdateCheck() || !s.cfg.UpdateInstall() ||
		s.installScheduled.Load() || !s.installDue(s.clock.Now()) || !autoInstallable(result) {
		return
	}

	reporting := s.gateDeps(result)
	decision, err := updater.CanApplyUpdate(ctx, reporting, updater.ModeAuto, false)
	if err != nil {
		log.Warn().Err(err).Msg("could not evaluate automatic update gate")
		return
	}
	decision.Release()
	if !decision.OK {
		return
	}
	if !s.installScheduled.CompareAndSwap(false, true) {
		return
	}
	s.idle.Schedule(ctx, "updater-install", updaterInstallIdleQuietWindow, updaterInstallIdleMaxWait,
		func(taskCtx context.Context) {
			defer s.installScheduled.Store(false)
			s.runInstall(taskCtx)
		})
}

func (s *updaterScheduler) runInstall(ctx context.Context) {
	if !s.cfg.UpdateCheck() || !s.cfg.UpdateInstall() {
		return
	}
	opts := serviceUpdaterOptions(s.cfg, s.platform, s.db, updater.ModeAuto)
	opts.Gate = s.gateDeps(s.pending.Load())
	fresh, err := s.check(ctx, opts)
	if err != nil || !autoInstallable(fresh) {
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Warn().Err(err).Msg("automatic update recheck failed")
			s.recordInstall(s.clock.Now(), err)
		}
		if fresh != nil {
			s.pending.Store(fresh)
		} else {
			s.pending.Store(nil)
		}
		return
	}
	s.pending.Store(fresh)

	deps := s.gateDeps(fresh)
	deps.AcquireRestore = s.state.TryAcquireRestoreAccess
	deps.AcquireMediaGate = s.state.AcquireUpdateMediaGate
	decision, err := updater.CanApplyUpdate(ctx, deps, updater.ModeAuto, false)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Warn().Err(err).Msg("automatic update gate failed")
		}
		s.recordInstall(s.clock.Now(), err)
		return
	}
	if !decision.OK {
		decision.Release()
		return
	}
	defer decision.Release()

	opts.Progress = serviceUpdaterProgress(s.state)
	opts.PreQuiesce = func(context.Context) error {
		powered := updater.PowerReady(deps, updater.ModeAuto, false)
		return powered.Err()
	}
	previousVersion := config.AppVersion
	newVersion, err := s.apply(ctx, opts)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Warn().Err(err).Msg("automatic update install failed")
		}
		s.recordInstall(s.clock.Now(), err)
		return
	}
	s.recordInstall(s.clock.Now(), nil)
	log.Info().Str("previous", previousVersion).Str("new", newVersion).
		Msg("automatic update applied, restarting service")
	s.state.RestartService()
}

func (s *updaterScheduler) gateDeps(result *updater.Result) *updater.GateDeps {
	deps := serviceUpdaterGateDeps(s.platform, s.db, s.state)
	deps.Now = s.clock.Now
	deps.DeferredSince = func() time.Time {
		if result == nil {
			return time.Time{}
		}
		return result.DeferredSince
	}
	return deps
}

func serviceUpdaterOptions(
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	mode updater.Mode,
) updater.Options {
	opts := updater.Options{
		PlatformID: pl.ID(),
		Channel:    cfg.UpdateChannel(),
		DataDir:    helpers.DataDir(pl),
		DeviceID:   cfg.DeviceID(),
		Managed:    pl.ManagedByPackageManager(),
		Mode:       mode,
	}
	if provider, ok := pl.(platforms.UpdatePayloadProvider); ok {
		opts.Payload = provider.UpdatePayload()
	}
	if db != nil {
		opts.UserDB = db.UserDB
	}
	return opts
}

func serviceUpdaterGateDeps(
	pl platforms.Platform,
	db *database.Database,
	st *state.State,
) *updater.GateDeps {
	deps := &updater.GateDeps{Power: func() power.Status { return platforms.PowerStatus(pl) }}
	if db != nil && db.MediaDB != nil {
		deps.IndexingStatus = db.MediaDB.GetIndexingStatus
		deps.OptimizationStatus = db.MediaDB.GetOptimizationStatus
		deps.ScrapingStatus = db.MediaDB.GetScrapingStatus
	}
	if st == nil {
		return deps
	}
	if coordinator := st.BackupCoordinator(); coordinator != nil {
		deps.BackupActive = func() bool {
			_, _, active := coordinator.Active()
			return active
		}
	}
	deps.ReaderWriteActive = st.AnyReaderWriteActive
	deps.ActiveMedia = func() bool { return st.ActiveMedia() != nil }
	deps.BackgroundMedia = func() bool { return st.BackgroundMedia() != nil }
	deps.ActivePlaylist = func() bool { return st.GetActivePlaylist() != nil }
	return deps
}

func serviceUpdaterProgress(st *state.State) updater.ProgressFn {
	if st == nil || st.Notifications == nil {
		return nil
	}
	return func(progress updater.Progress) {
		notifications.UpdateState(st.Notifications, models.UpdateStateNotification{
			Stage:           string(progress.Stage),
			Version:         progress.Version,
			Trigger:         progress.Trigger,
			Error:           progress.Error,
			BytesDownloaded: progress.BytesDownloaded,
			BytesTotal:      progress.BytesTotal,
		})
	}
}
