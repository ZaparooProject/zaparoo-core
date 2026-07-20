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

package profiles

import (
	"errors"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
)

// switchApplyTimeout bounds how long a profile switch waits inline for its
// data swap. Bind mounts are a few syscalls, so this only expires when
// storage misbehaves (e.g. a dead NAS); the apply keeps running in the
// worker and reports via notification when it finishes.
const switchApplyTimeout = time.Second

// Broker is the interface for subscribing to notifications.
type Broker interface {
	Subscribe(bufferSize int, methods ...string) (<-chan models.Notification, int)
	Unsubscribe(id int)
}

// swapRequest is one unit of work for the apply worker. emitSuccess
// controls whether a successful apply broadcasts a profiles.data
// notification: switch-originated applies do, quiet reconciles (boot,
// mount watcher) only report errors.
type swapRequest struct {
	done        chan struct{}
	ref         platforms.ProfileRef
	emitSuccess bool
}

type pendingSwap struct {
	ref      platforms.ProfileRef
	sequence uint64
}

// DataSwapCoordinator applies profile data swaps through a platform's
// ProfileDataSwapper. The profile switch itself is instant and never fails
// because of file operations: swaps run in a worker goroutine, switches
// wait for it only briefly, swaps while media is running are deferred
// until it stops, and queued targets coalesce to the most recent one.
type DataSwapCoordinator struct {
	cfg      *config.Instance
	st       *state.State
	swapper  platforms.ProfileDataSwapper
	notify   chan<- models.Notification
	targetCh chan swapRequest
	quit     chan struct{}
	done     chan struct{}
	pending  *pendingSwap
	mu       syncutil.Mutex
	sequence uint64
	subID    int
	started  bool
}

// NewDataSwapCoordinator creates a coordinator for the given platform
// swapper. A nil swapper is valid and makes every method a no-op, so
// callers never need to branch on platform capability.
func NewDataSwapCoordinator(
	cfg *config.Instance, st *state.State, swapper platforms.ProfileDataSwapper,
) *DataSwapCoordinator {
	return &DataSwapCoordinator{
		cfg:      cfg,
		st:       st,
		swapper:  swapper,
		targetCh: make(chan swapRequest, 1),
	}
}

// Start subscribes to media lifecycle events and starts the apply worker.
func (c *DataSwapCoordinator) Start(broker Broker, notificationsSend chan<- models.Notification) {
	if c.swapper == nil {
		return
	}

	c.mu.Lock()
	c.notify = notificationsSend
	c.quit = make(chan struct{})
	c.done = make(chan struct{})
	c.started = true
	c.pending = nil
	c.sequence = 0
	done := c.done
	c.mu.Unlock()

	notifChan, subID := broker.Subscribe(16, models.NotificationStopped)
	c.mu.Lock()
	c.subID = subID
	c.mu.Unlock()

	go func() {
		defer close(done)
		c.run(notifChan, broker)
	}()
}

// Stop shuts down the coordinator and waits for the worker to exit.
func (c *DataSwapCoordinator) Stop() {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return
	}
	c.started = false
	quit := c.quit
	done := c.done
	c.mu.Unlock()

	close(quit)
	<-done
}

func (c *DataSwapCoordinator) run(notifChan <-chan models.Notification, broker Broker) {
	defer broker.Unsubscribe(c.subID)
	for {
		select {
		case <-c.quit:
			return
		case req := <-c.targetCh:
			c.apply(&req)
		case n, ok := <-notifChan:
			if !ok {
				return
			}
			if n.Method == models.NotificationStopped {
				c.onMediaStopped()
			}
		}
	}
}

// RequestSwitch is called by the profiles service after the active profile
// changes. When no media is running it waits briefly for the swap so the
// common combo-card flow (switch then launch in one scan) launches with
// the new profile's data already mounted. While media runs, the swap is
// deferred until the media stops and the most recent target wins.
func (c *DataSwapCoordinator) RequestSwitch(ref platforms.ProfileRef) {
	if c.swapper == nil {
		return
	}
	ref = c.effectiveRef(ref)

	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return
	}
	c.sequence++
	sequence := c.sequence
	if c.st.ActiveMedia() != nil {
		c.pending = &pendingSwap{ref: ref, sequence: sequence}
		notify := c.notify
		c.mu.Unlock()
		log.Info().Str("profileId", ref.ID).
			Msg("profiles: media running, data swap deferred until it stops")
		notifications.ProfilesDataChanged(notify, models.ProfilesDataNotification{
			ProfileID: ref.ID,
			Status:    models.ProfilesDataDeferred,
		})
		return
	}
	c.pending = nil
	c.mu.Unlock()

	done := c.enqueueCurrent(
		swapRequest{ref: ref, emitSuccess: true, done: make(chan struct{})}, sequence)
	select {
	case <-done:
	case <-time.After(switchApplyTimeout):
		log.Warn().Str("profileId", ref.ID).
			Msg("profiles: data swap still running after switch, continuing in background")
	}
}

// Reconcile re-applies the current active profile's data state. Used at
// boot (after profile restore), when the swap_data setting changes, and by
// platform storage watchers when the mount table changes underneath us.
// Successful no-op reconciles are quiet; only errors notify.
func (c *DataSwapCoordinator) Reconcile() {
	if c.swapper == nil {
		return
	}

	ref := platforms.ProfileRef{}
	if active := c.st.ActiveProfile(); active != nil {
		ref = platforms.ProfileRef{ID: active.ProfileID, Name: active.Name}
	}
	ref = c.effectiveRef(ref)

	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return
	}
	c.sequence++
	sequence := c.sequence
	if c.st.ActiveMedia() != nil {
		c.pending = &pendingSwap{ref: ref, sequence: sequence}
		c.mu.Unlock()
		return
	}
	c.pending = nil
	c.enqueue(swapRequest{ref: ref, emitSuccess: false, done: make(chan struct{})})
	c.mu.Unlock()
}

func (c *DataSwapCoordinator) takePending() *pendingSwap {
	c.mu.Lock()
	defer c.mu.Unlock()
	pending := c.pending
	c.pending = nil
	return pending
}

func (c *DataSwapCoordinator) enqueuePending(pending *pendingSwap) {
	if pending == nil {
		return
	}
	// Re-resolve against config in case swap_data flipped while deferred.
	ref := c.effectiveRef(pending.ref)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.started || pending.sequence != c.sequence {
		return
	}
	c.enqueue(swapRequest{ref: ref, emitSuccess: true, done: make(chan struct{})})
}

func (c *DataSwapCoordinator) onMediaStopped() {
	c.enqueuePending(c.takePending())
}

func (c *DataSwapCoordinator) enqueueCurrent(req swapRequest, sequence uint64) <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.started || sequence != c.sequence {
		close(req.done)
		return req.done
	}
	return c.enqueue(req)
}

// effectiveRef maps the requested profile to the shared profile when data
// swapping is disabled, so turning the setting off converges all mounts
// back to the default state.
func (c *DataSwapCoordinator) effectiveRef(ref platforms.ProfileRef) platforms.ProfileRef {
	if !c.cfg.ProfilesSwapData() {
		return platforms.ProfileRef{}
	}
	return ref
}

// enqueue hands a request to the worker, displacing any not-yet-started
// request so targets coalesce to the most recent. The displaced request's
// done channel is closed so nothing waits on superseded work.
func (c *DataSwapCoordinator) enqueue(req swapRequest) <-chan struct{} {
	for {
		select {
		case c.targetCh <- req:
			return req.done
		default:
			select {
			case stale := <-c.targetCh:
				close(stale.done)
			default:
			}
		}
	}
}

func (c *DataSwapCoordinator) apply(req *swapRequest) {
	defer close(req.done)

	items := make([]string, 0, 2)
	for _, item := range c.swapper.ProfileItems() {
		if item.Owner == platforms.ProfileItemOwnerProfile {
			items = append(items, item.ID)
		}
	}

	release, err := c.st.AcquireRestoreAccess()
	if err == nil {
		err = c.swapper.ApplyProfile(req.ref, items)
		release()
	}

	c.mu.Lock()
	notify := c.notify
	c.mu.Unlock()

	switch {
	case err == nil:
		if req.emitSuccess {
			log.Info().Str("profileId", req.ref.ID).Msg("profiles: data swap applied")
			notifications.ProfilesDataChanged(notify, models.ProfilesDataNotification{
				ProfileID: req.ref.ID,
				Status:    models.ProfilesDataApplied,
			})
		}
	case errors.Is(err, platforms.ErrProfileDataUnavailable):
		log.Warn().Err(err).Str("profileId", req.ref.ID).
			Msg("profiles: data swap unavailable")
		notifications.ProfilesDataChanged(notify, models.ProfilesDataNotification{
			ProfileID: req.ref.ID,
			Status:    models.ProfilesDataUnavailable,
			Reason:    err.Error(),
		})
	default:
		log.Error().Err(err).Str("profileId", req.ref.ID).
			Msg("profiles: data swap failed, existing data untouched")
		notifications.ProfilesDataChanged(notify, models.ProfilesDataNotification{
			ProfileID: req.ref.ID,
			Status:    models.ProfilesDataFailed,
			Reason:    err.Error(),
		})
	}
}
