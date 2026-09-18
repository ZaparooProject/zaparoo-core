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
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	backupsvc "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/librarysync"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
)

type librarySyncTimings struct {
	// check paces how often the loop looks at its triggers.
	check time.Duration
	// idleSlice is how long the pass waits for the device to fall quiet before
	// looking at the Library sync setting again.
	idleSlice time.Duration
	// startup delays the first pass past the boot burst.
	startup time.Duration
	// recheck is how long a quiet pass is trusted before the next one.
	recheck        time.Duration
	initialBackoff time.Duration
	maxBackoff     time.Duration
	idleQuiet      time.Duration
	idleMaxWait    time.Duration
}

var defaultLibrarySyncTimings = librarySyncTimings{
	check:          time.Minute,
	idleSlice:      5 * time.Second,
	startup:        2 * time.Minute,
	recheck:        15 * time.Minute,
	initialBackoff: time.Minute,
	maxBackoff:     time.Hour,
	idleQuiet:      5 * time.Second,
	idleMaxWait:    300 * time.Second,
}

// librarySyncRunner is the part of the Library sync service the scheduler
// drives.
type librarySyncRunner interface {
	Enabled() bool
	ApplySetting(ctx context.Context) (bool, error)
	DeleteInventory(ctx context.Context) (bool, error)
	SyncInventory(ctx context.Context, force bool) (librarysync.InventoryResult, error)
}

// systemLaunchers returns a lookup of the launchers that serve one system,
// the same set a title launch ranks file types with.
func systemLaunchers(cfg *config.Instance, pl platforms.Platform) func(systemID string) []platforms.Launcher {
	return func(systemID string) []platforms.Launcher {
		all := pl.Launchers(cfg)
		out := make([]platforms.Launcher, 0, len(all))
		for i := range all {
			if all[i].SystemID == systemID {
				out = append(out, all[i])
			}
		}
		return out
	}
}

func startLibrarySyncScheduler(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	st *state.State,
	idleSched *idle.Scheduler,
	pauser *syncutil.Pauser,
	notifBroker *broker.Broker,
	wg *sync.WaitGroup,
) {
	manager := backupsvc.NewManager(cfg, pl, db).WithCoordinator(st.BackupCoordinator())
	svc := librarysync.New(&librarysync.Options{
		Config:        cfg,
		DB:            db,
		NewClient:     manager.NewOnlineClient,
		Inbox:         st.Inbox(),
		Pauser:        pauser,
		SendHeartbeat: manager.SendCapabilityHeartbeat,
		Launchers:     systemLaunchers(cfg, pl),
	})
	requests := make(chan struct{}, 1)
	stateRequests := make(chan struct{}, 1)
	st.SetLibrarySyncSignals(state.LibrarySyncSignals{
		SettingChanged: func() {
			signalLibrarySync(requests)
			signalLibrarySync(stateRequests)
		},
		StateChanged: func() { signalLibrarySync(stateRequests) },
	})
	indexing, subID := notifBroker.Subscribe(32, models.NotificationMediaIndexing)
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer notifBroker.Unsubscribe(subID)
		librarySyncLoop(ctx, svc, idleSched, requests, indexing, &defaultLibrarySyncTimings)
	}()
	go func() {
		defer wg.Done()
		libraryStateLoop(ctx, svc, stateRequests, &defaultLibraryStateTimings)
	}()
}

func signalLibrarySync(requests chan<- struct{}) {
	select {
	case requests <- struct{}{}:
	default:
	}
}

type libraryStateTimings struct {
	check          time.Duration
	startup        time.Duration
	debounce       time.Duration
	interval       time.Duration
	initialBackoff time.Duration
	maxBackoff     time.Duration
}

var defaultLibraryStateTimings = libraryStateTimings{
	check:          time.Minute,
	startup:        2 * time.Minute,
	debounce:       2 * time.Second,
	interval:       time.Hour,
	initialBackoff: time.Minute,
	maxBackoff:     time.Hour,
}

// libraryStateRunner is the part of the Library sync service that keeps
// personal state converged.
type libraryStateRunner interface {
	SyncState(ctx context.Context) (librarysync.StateResult, error)
}

// libraryStateLoop pulls and pushes personal state apart from the inventory,
// so an edit is pushed within seconds even while a large first inventory is
// still being resolved. Edits are coalesced over a short debounce; otherwise
// a pass runs after startup and hourly.
func libraryStateLoop(
	ctx context.Context,
	runner libraryStateRunner,
	requests <-chan struct{},
	timings *libraryStateTimings,
) {
	startup := time.NewTimer(timings.startup)
	defer startup.Stop()
	ticker := time.NewTicker(timings.check)
	defer ticker.Stop()
	debounce := time.NewTimer(timings.debounce)
	debounce.Stop()
	defer debounce.Stop()

	started := false
	pending := false
	retry := intervalState{backoff: timings.initialBackoff}
	run := func() {
		now := time.Now()
		_, err := runner.SyncState(ctx)
		switch {
		case ctx.Err() != nil:
		case errors.Is(err, librarysync.ErrNotSettled):
			// The media database is being written, so the pass deferred.
			// Nothing synced, so recording a success would hold the device
			// back for the whole interval once the index finishes, edit or
			// no edit. Nothing failed either, so the failure backoff is left
			// as it was: just ask again at the check interval.
			pending = true
			retry.nextAttempt = now.Add(timings.check)
		case err == nil || librarysync.IsIdleError(err):
			pending = false
			retry.recordSuccess(now, timings.initialBackoff)
		default:
			retry.recordFailure(now, timings.initialBackoff, timings.maxBackoff)
			log.Warn().Err(err).Dur("retry_in", retry.nextAttempt.Sub(now)).Msg("library state sync failed")
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-startup.C:
			started = true
			run()
		case <-requests:
			pending = true
			debounce.Reset(timings.debounce)
		case <-debounce.C:
			if retry.nextAttempt.IsZero() || !time.Now().Before(retry.nextAttempt) {
				run()
			}
		case <-ticker.C:
			now := time.Now()
			intervalDue := (pending || started) && retry.due(now, timings.interval)
			retryDue := pending && !retry.nextAttempt.IsZero() && !now.Before(retry.nextAttempt)
			if intervalDue || retryDue {
				run()
			}
		}
	}
}

// librarySyncLoop runs Library sync passes: after the startup delay, when the
// setting changes, after an index finishes, and otherwise every recheck
// interval. Failures back off; being off, unlinked or mid-index does not.
func librarySyncLoop(
	ctx context.Context,
	runner librarySyncRunner,
	idleSched *idle.Scheduler,
	requests <-chan struct{},
	indexing <-chan models.Notification,
	timings *librarySyncTimings,
) {
	startup := time.NewTimer(timings.startup)
	defer startup.Stop()
	ticker := time.NewTicker(timings.check)
	defer ticker.Stop()

	started := false
	requested := false
	indexChanged := false
	retry := intervalState{backoff: timings.initialBackoff}
	for {
		select {
		case <-ctx.Done():
			return
		case <-startup.C:
			started = true
		case <-requests:
			requested = true
		case _, ok := <-indexing:
			if !ok {
				indexing = nil
				continue
			}
			indexChanged = true
			continue
		case <-ticker.C:
		}
		if !started && !requested {
			continue
		}
		now := time.Now()
		indexDue := indexChanged && retry.nextAttempt.IsZero()
		if !requested && !indexDue && !retry.due(now, timings.recheck) {
			continue
		}
		requested = false

		err := runLibrarySyncPass(ctx, runner, idleSched, timings)
		indexChanged = false
		// A first sync of a large library runs for far longer than the first
		// backoff, so the interval is measured from when the pass finished. From
		// when it started, a failure's next attempt would already be in the past
		// and the backoff would not hold at all.
		finished := time.Now()
		switch {
		case ctx.Err() != nil:
			return
		case err == nil || librarysync.IsIdleError(err):
			// An index still being written notifies again when it finishes,
			// and the recheck covers the runs that do not.
			retry.recordSuccess(finished, timings.initialBackoff)
		default:
			retry.recordFailure(finished, timings.initialBackoff, timings.maxBackoff)
			log.Warn().Err(err).Dur("retry_in", retry.nextAttempt.Sub(finished)).Msg("library sync pass failed")
		}
	}
}

// waitForQuietDevice waits for the device to stop serving requests before a
// pass reads the whole library, giving up after idleMaxWait.
//
// The wait is taken in slices so that turning Library sync off is acted on
// without waiting out a device that is busy: the loop cannot deliver the
// setting request while a pass is running, and on a device with an app polling
// it the wait runs to its limit. Between slices the setting is read again, and
// a pass that is no longer wanted returns to the loop, which has the request
// buffered and starts the pass that removes the inventory.
func waitForQuietDevice(
	ctx context.Context,
	runner librarySyncRunner,
	idleSched *idle.Scheduler,
	timings *librarySyncTimings,
) error {
	if idleSched == nil {
		return nil
	}
	slice := timings.idleSlice
	if slice <= 0 {
		slice = timings.idleMaxWait
	}
	deadline := time.Now().Add(timings.idleMaxWait)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil
		}
		err := idleSched.WaitForIdle(ctx, timings.idleQuiet, min(slice, remaining))
		if err == nil {
			return nil
		}
		if !errors.Is(err, idle.ErrMaxWaitElapsed) {
			return err //nolint:wrapcheck // only context cancellation reaches here
		}
		if !runner.Enabled() {
			return nil
		}
	}
}

func runLibrarySyncPass(
	ctx context.Context,
	runner librarySyncRunner,
	idleSched *idle.Scheduler,
	timings *librarySyncTimings,
) error {
	if _, err := runner.ApplySetting(ctx); err != nil {
		return fmt.Errorf("apply library sync setting: %w", err)
	}
	if _, err := runner.DeleteInventory(ctx); err != nil {
		return fmt.Errorf("remove library inventory: %w", err)
	}
	// Applying the setting and removing a dropped inventory are the whole
	// pass while sync is off. Everything below serves an upload, so waiting
	// for the device to fall idle first would be a wait for nothing.
	if !runner.Enabled() {
		return nil
	}
	if err := waitForQuietDevice(ctx, runner, idleSched, timings); err != nil {
		return err
	}
	if !runner.Enabled() {
		return nil
	}
	result, err := runner.SyncInventory(ctx, false)
	if err != nil {
		if librarysync.IsIdleError(err) {
			log.Debug().Err(err).Msg("library inventory not synced")
		}
		return err //nolint:wrapcheck // the service already names the failing step
	}
	if result.Outcome != librarysync.InventorySkipped {
		log.Debug().Str("outcome", result.Outcome).Int("items", result.ItemCount).
			Msg("library inventory pass finished")
	}
	return nil
}
