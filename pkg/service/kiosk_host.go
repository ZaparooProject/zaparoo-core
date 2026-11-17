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
// it. Core leaves the host's lock alone for as long as its own kiosk mode
// is off, apart from releasing it once when it is turned off.
type kioskHostSync struct {
	lock    *kiosk.Service
	pl      platforms.Platform
	clock   clockwork.Clock
	changed chan kiosk.Status
}

func newKioskHostSync(lock *kiosk.Service, pl platforms.Platform, clock clockwork.Clock) *kioskHostSync {
	h := &kioskHostSync{lock: lock, pl: pl, clock: clock, changed: make(chan kiosk.Status, 8)}
	lock.OnChange(func(status kiosk.Status) {
		select {
		case h.changed <- status:
		default:
			// A full buffer already holds a change that will make the
			// worker read the current state, which is all it acts on.
		}
	})
	return h
}

func (h *kioskHostSync) run(ctx context.Context, mediaStarted <-chan models.Notification) {
	if _, ok := h.pl.(platforms.KioskHost); !ok {
		return
	}
	var retry <-chan time.Time
	apply := func() {
		retry = nil
		status := h.lock.Status()
		if !status.Active() {
			return
		}
		if err := platforms.SetKioskLocked(h.pl, status.Locked()); err != nil {
			log.Debug().Err(err).Msg("kiosk: host did not take the lock, will offer it again")
			retry = h.clock.After(kioskHostRetry)
		}
	}
	apply()
	for {
		select {
		case <-ctx.Done():
			return
		case status := <-h.changed:
			if !status.Active() {
				retry = nil
				if err := platforms.SetKioskLocked(h.pl, false); err != nil {
					log.Warn().Err(err).Msg("kiosk: failed to release the host's lock")
				}
				continue
			}
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

// kioskIdleStop ends the running media the way a reached playtime limit
// does: the outgoing media's before_exit hook, then an unconditional stop.
func kioskIdleStop(svc *ServiceContext) {
	svc.State.RunBeforeExitHook()
	cancelStop := svc.State.RecordMediaStop(models.MediaStopReasonStopped)
	if err := svc.Platform.StopActiveLauncher(platforms.StopForMenu); err != nil {
		log.Error().Err(err).Msg("kiosk: failed to stop media on idle reset")
		cancelStop()
	}
}
