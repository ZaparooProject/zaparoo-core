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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/bgpriority"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

const (
	// mediaUserReconcileRetryFirst and mediaUserReconcileRetryLongest bound the
	// wait before a failed reconcile is tried again. The wait doubles after
	// each failure.
	mediaUserReconcileRetryFirst   = time.Minute
	mediaUserReconcileRetryLongest = 30 * time.Minute
	// mediaUserReconcileBusyPoll is how often a waiting reconcile checks
	// whether the media database is free again.
	mediaUserReconcileBusyPoll = 5 * time.Second
	// mediaUserReconcilePending is the saved marker. Only its presence matters.
	mediaUserReconcilePending = "1"
)

// mediaUserReconciler rebuilds the media database's copy of media user data
// after the user database was replaced, as by a backup restore. A request is
// saved in the user database before it is acted on, so the restart that
// follows a restore still runs it, and so does a start after a failed or
// interrupted attempt.
type mediaUserReconciler struct {
	db         *database.Database
	wake       chan struct{}
	seq        uint64
	retryFirst time.Duration
	busyPoll   time.Duration
	mu         syncutil.Mutex
	pending    bool
}

var _ database.MediaUserDataReconciler = (*mediaUserReconciler)(nil)

// newMediaUserReconciler returns a reconciler holding any request a previous
// run left unfinished. A marker that cannot be read counts as a request: an
// unneeded reconcile writes nothing.
func newMediaUserReconciler(db *database.Database) *mediaUserReconciler {
	r := &mediaUserReconciler{
		db:         db,
		wake:       make(chan struct{}, 1),
		retryFirst: mediaUserReconcileRetryFirst,
		busyPoll:   mediaUserReconcileBusyPoll,
	}
	_, found, err := db.UserDB.GetDeviceState(database.DeviceStateKeyMediaUserDataReconcile)
	if err != nil {
		log.Warn().Err(err).Msg("failed to read the media user data reconcile marker; reconciling")
	}
	r.pending = found || err != nil
	return r
}

// QueueMediaUserDataReconcile saves the request, then wakes the worker.
func (r *mediaUserReconciler) QueueMediaUserDataReconcile() {
	r.mu.Lock()
	r.pending = true
	r.seq++
	if err := r.db.UserDB.SetDeviceState(
		database.DeviceStateKeyMediaUserDataReconcile, mediaUserReconcilePending,
	); err != nil {
		log.Warn().Err(err).Msg("failed to save the media user data reconcile marker")
	}
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Run reconciles whenever a request is pending, until ctx ends. It runs at
// background priority on its own thread.
func (r *mediaUserReconciler) Run(ctx context.Context) {
	bgpriority.Apply()
	retryDelay := r.retryFirst
	for {
		var timer *time.Timer
		var retry <-chan time.Time
		if r.reconcilePending(ctx) {
			retryDelay = r.retryFirst
		} else {
			timer = time.NewTimer(retryDelay)
			retry = timer.C
			retryDelay = min(retryDelay*2, mediaUserReconcileRetryLongest)
		}
		select {
		case <-ctx.Done():
		case <-r.wake:
		case <-retry:
		}
		if timer != nil {
			timer.Stop()
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// reconcilePending runs the reconcile if one is requested and reports false
// when it is still owed afterwards. The marker is only removed when no newer
// request arrived while the reconcile ran.
func (r *mediaUserReconciler) reconcilePending(ctx context.Context) bool {
	for ctx.Err() == nil {
		r.mu.Lock()
		pending, seq := r.pending, r.seq
		r.mu.Unlock()
		if !pending {
			return true
		}
		if !database.WaitForLongMediaWrites(ctx, r.db.MediaDB, r.busyPoll) {
			return true
		}
		if err := database.ReconcileMediaUserData(ctx, r.db); err != nil {
			if ctx.Err() == nil {
				log.Warn().Err(err).Msg("failed to reconcile media user data; will retry")
			}
			return false
		}
		if !r.finish(seq) {
			return false
		}
	}
	return true
}

// finish clears the request unless it was made again during the reconcile,
// and reports false when the saved marker could not be removed.
func (r *mediaUserReconciler) finish(seq uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seq != seq {
		return true
	}
	if err := r.db.UserDB.DeleteDeviceState(database.DeviceStateKeyMediaUserDataReconcile); err != nil {
		log.Warn().Err(err).Msg("failed to clear the media user data reconcile marker; will retry")
		return false
	}
	r.pending = false
	return true
}
