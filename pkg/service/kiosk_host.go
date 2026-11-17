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
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/profiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// kioskHostRetry is how long to wait before offering the host its lock
	// again after it could not take it.
	kioskHostRetry = 15 * time.Second
	// kioskIdleTick is how often the idle reset looks at the clock.
	kioskIdleTick = 5 * time.Second
)

// setupKiosk loads the kiosk lock and attaches it to device state. It runs
// before the API starts, so no request is ever served without the lock it
// should have been held to.
func setupKiosk(
	st *state.State, db *database.Database, pl platforms.Platform, profilesSvc *profiles.Service,
) *kiosk.Service {
	lock := kiosk.NewService(db, clockwork.NewRealClock())
	st.SetKiosk(lock)

	if err := lock.InitRecoveryToken(afero.NewOsFs(), helpers.DataDir(pl)); err != nil {
		// Without the token a forgotten PIN can only be recovered with the
		// service stopped. The lock itself is unaffected.
		log.Error().Err(err).Msg("kiosk: failed to write recovery token")
	}

	dropAdminProfile := func() {
		if err := profilesSvc.DeactivateForKioskLock(); err != nil {
			log.Error().Err(err).Msg("kiosk: failed to leave the admin profile on lock")
		}
	}
	if lock.Locked() {
		log.Info().Msg("kiosk: device starts locked")
		dropAdminProfile()
	}

	lock.OnChange(func(status kiosk.Status) {
		if status.Locked() {
			dropAdminProfile()
		}
		methods.NotifyKioskState(st.Notifications, pl, &status)
	})
	return lock
}

// kioskHostSync keeps a platform's own lock, where it has one, in step with
// Core's. The host can lose the lock without saying so (MiSTer's Main is
// replaced on every core load, and a command sent mid-load is dropped), so
// the lock is offered again whenever media starts and until the host takes
// it.
//
// Core only ever releases a host lock it set. That ownership is stored, so
// a release that fails, or that Core is restarted in the middle of, is
// still made: without it a host could be left locked by a kiosk mode that
// is off, with nothing on the device able to open it. A host lock that was
// on before Core's kiosk mode was ever used is never touched.
type kioskHostSync struct {
	lock  *kiosk.Service
	pl    platforms.Platform
	clock clockwork.Clock
	db    *database.Database
	// changed only says that something changed. The worker reads the
	// lock's current state each time it wakes, so a wake-up that is dropped
	// because one is already waiting loses nothing.
	changed chan struct{}
}

func newKioskHostSync(
	lock *kiosk.Service, pl platforms.Platform, clock clockwork.Clock, db *database.Database,
) *kioskHostSync {
	h := &kioskHostSync{lock: lock, pl: pl, clock: clock, db: db, changed: make(chan struct{}, 1)}
	lock.OnChange(func(kiosk.Status) {
		select {
		case h.changed <- struct{}{}:
		default:
		}
	})
	return h
}

// owned reports whether Core has set the host's lock and not yet released
// it. A marker that cannot be read counts as owned: releasing a lock Core
// may have set is safer than leaving a host locked for good.
func (h *kioskHostSync) owned() bool {
	_, found, err := h.db.UserDB.GetDeviceState(database.DeviceStateKeyKioskHostLock)
	if err != nil {
		log.Warn().Err(err).Msg("kiosk: failed to read host lock ownership")
		return true
	}
	return found
}

func (h *kioskHostSync) setOwned(owned bool) {
	var err error
	if owned {
		err = h.db.UserDB.SetDeviceState(database.DeviceStateKeyKioskHostLock, "1")
	} else {
		err = h.db.UserDB.DeleteDeviceState(database.DeviceStateKeyKioskHostLock)
	}
	if err != nil {
		log.Warn().Err(err).Msg("kiosk: failed to store host lock ownership")
	}
}

// reconcile brings the host's lock in line with Core's current state and
// reports whether it has to be tried again.
func (h *kioskHostSync) reconcile() (retry bool) {
	status := h.lock.Status()
	if status.Active() {
		// Ownership is recorded before the host is told, so a restart
		// between the two still knows there may be a lock to release.
		if !h.owned() {
			h.setOwned(true)
		}
		if err := platforms.SetKioskLocked(h.pl, status.Locked()); err != nil {
			log.Debug().Err(err).Msg("kiosk: host did not take the lock, will offer it again")
			return true
		}
		return false
	}
	if !h.owned() {
		return false
	}
	if err := platforms.SetKioskLocked(h.pl, false); err != nil {
		log.Debug().Err(err).Msg("kiosk: host did not release the lock, will ask again")
		return true
	}
	h.setOwned(false)
	return false
}

func (h *kioskHostSync) run(ctx context.Context, mediaStarted <-chan models.Notification) {
	if _, ok := h.pl.(platforms.KioskHost); !ok {
		return
	}
	var retry <-chan time.Time
	apply := func() {
		retry = nil
		if h.reconcile() {
			retry = h.clock.After(kioskHostRetry)
		}
	}
	apply()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.changed:
			apply()
		case <-mediaStarted:
			apply()
		case <-retry:
			apply()
		}
	}
}

// kioskIdleReset stops the running media on a locked device that nobody has
// touched for the configured time, so the next person starts from the home
// screen instead of in the middle of someone else's game.
type kioskIdleReset struct {
	lock     *kiosk.Service
	source   platforms.InputActivitySource
	clock    clockwork.Clock
	hasMedia func() bool
	stop     func()
	changed  chan struct{}
	lastUsed atomic.Int64
}

func newKioskIdleReset(
	lock *kiosk.Service, pl platforms.Platform, clock clockwork.Clock, hasMedia func() bool, stop func(),
) *kioskIdleReset {
	k := &kioskIdleReset{
		lock: lock, clock: clock, hasMedia: hasMedia, stop: stop,
		changed: make(chan struct{}, 1),
	}
	if kiosk.IdleResetSupported(pl) {
		k.source = platforms.ResolveInputActivity(pl)
	}
	lock.OnChange(func(kiosk.Status) {
		select {
		case k.changed <- struct{}{}:
		default:
		}
	})
	return k
}

func (k *kioskIdleReset) timeout() (time.Duration, bool) {
	status := k.lock.Status()
	if !status.Locked() || !status.Settings.IdleReset {
		return 0, false
	}
	return time.Duration(status.Settings.IdleTimeout) * time.Second, true
}

func (k *kioskIdleReset) run(ctx context.Context) {
	if k.source == nil {
		return
	}
	for {
		if _, on := k.timeout(); !on {
			select {
			case <-ctx.Done():
				return
			case <-k.changed:
				continue
			}
		}
		k.watch(ctx)
		if ctx.Err() != nil {
			return
		}
	}
}

// watch runs for as long as idle reset stays on.
func (k *kioskIdleReset) watch(ctx context.Context) {
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	k.lastUsed.Store(k.clock.Now().UnixNano())
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		err := k.source.WatchInput(watchCtx, false, func() {
			k.lastUsed.Store(k.clock.Now().UnixNano())
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Warn().Err(err).Msg("kiosk: input watch for idle reset stopped")
		}
	}()
	defer func() {
		cancel()
		<-watchDone
	}()

	ticker := k.clock.NewTicker(kioskIdleTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-watchDone:
			// Without the watch nothing says the device is in use, and
			// stopping a game on a guess is worse than not resetting.
			select {
			case <-ctx.Done():
			case <-k.changed:
			}
			return
		case <-k.changed:
			if _, on := k.timeout(); !on {
				return
			}
		case <-ticker.Chan():
			timeout, on := k.timeout()
			if !on {
				return
			}
			now := k.clock.Now()
			if !k.hasMedia() {
				// Nothing to reset. The wait starts when media does.
				k.lastUsed.Store(now.UnixNano())
				continue
			}
			if now.Sub(time.Unix(0, k.lastUsed.Load())) < timeout {
				continue
			}
			log.Info().Dur("idle", timeout).Msg("kiosk: device idle, stopping media")
			k.stop()
			k.lastUsed.Store(now.UnixNano())
		}
	}
}

// kioskIdleStop ends the running media the way the stop API does. It calls
// off a launch that is still settling, runs the outgoing media's
// before_exit hook, and then waits for the media stop gate, so it cannot
// land on a game a launch in flight has only just started and leave that
// launch to publish media that is no longer running.
func kioskIdleStop(svc *ServiceContext) {
	ctx := svc.State.GetContext()
	svc.State.LauncherManager().NewContext()

	// Before the gate: a before_exit script may launch media, which takes
	// the read side of the gate held below.
	outgoingGen, hadMedia := svc.State.ActiveMediaReadyGeneration()
	svc.State.RunBeforeExitHook()

	release, err := svc.State.AcquireMediaStop(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("kiosk: idle reset could not wait for an in-flight launch")
		return
	}
	defer release()

	if svc.State.ActiveMediaReplacedSince(outgoingGen, hadMedia) {
		log.Info().Msg("kiosk: before_exit replaced the outgoing media, leaving it running")
		return
	}
	cancelStop := svc.State.RecordMediaStop(models.MediaStopReasonStopped)
	if err := svc.Platform.StopActiveLauncher(platforms.StopForMenu); err != nil {
		log.Error().Err(err).Msg("kiosk: failed to stop media on idle reset")
		cancelStop()
	}
}
