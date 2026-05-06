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

// Package idle provides a small scheduler for "run eventually" background
// work. Tasks block on Scheduler.WaitForIdle until the API has been quiet
// for a configurable window or a hard cap elapses, so cold-boot work like
// arcade DB checks or update polling doesn't compete with the launcher's
// first request for the single ARM core or the SQLite file lock.
package idle

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

// ErrMaxWaitElapsed is returned by WaitForIdle when the maxWait hard cap
// expired before the quiet window was satisfied. Callers that want to run
// the task anyway (the typical case for Schedule) should treat this as a
// success-equivalent signal and proceed; callers that want to bail can
// check errors.Is(err, ErrMaxWaitElapsed).
var ErrMaxWaitElapsed = errors.New("idle scheduler: max wait elapsed before idle")

// Scheduler tracks in-flight API requests and lets background work wait
// until the system has been idle for a configurable quiet window. Safe
// for concurrent use; the zero value is not usable, construct with New.
//
// Wakeups use a single channel that RequestEnded closes and replaces under
// mu. WaitForIdle snapshots the channel under mu before reading the in-
// flight predicate, so a RequestEnded that races the read still wakes us
// (the snapshotted channel is the closed one, and the next select returns
// immediately rather than blocking on stale state).
type Scheduler struct {
	clock              clockwork.Clock
	wakeCh             chan struct{}
	lastRequestEndedAt atomic.Int64 // unix nanos
	inFlight           atomic.Int64
	mu                 syncutil.Mutex
	tasks              sync.WaitGroup
}

// New constructs a Scheduler with no in-flight requests using the real
// system clock.
func New() *Scheduler {
	return NewWithClock(clockwork.NewRealClock())
}

// NewWithClock constructs a Scheduler using the supplied clock. Tests use
// this with clockwork.NewFakeClock to drive time deterministically.
func NewWithClock(clock clockwork.Clock) *Scheduler {
	s := &Scheduler{
		clock:  clock,
		wakeCh: make(chan struct{}),
	}
	s.lastRequestEndedAt.Store(clock.Now().UnixNano())
	return s
}

// RequestStarted increments the in-flight counter. Pair with
// RequestEnded; the API server middleware should call this on request
// entry.
func (s *Scheduler) RequestStarted() {
	s.inFlight.Add(1)
}

// RequestEnded decrements the in-flight counter and wakes any waiters.
// Pair with RequestStarted; the API server middleware should call this
// on request completion. The wake is done by closing the current wakeCh
// and replacing it with a fresh one under mu, which fans out to every
// goroutine currently in WaitForIdle's select.
//
// Unmatched calls (double RequestEnded, or RequestEnded without a prior
// RequestStarted) clamp the counter at zero rather than letting it go
// negative — a negative inFlight would prevent WaitForIdle's
// `inFlight == 0` predicate from ever holding, wedging waiters until the
// maxWait cap.
func (s *Scheduler) RequestEnded() {
	for {
		cur := s.inFlight.Load()
		if cur <= 0 {
			log.Warn().
				Int64("in_flight", cur).
				Msg("idle scheduler: RequestEnded without matching RequestStarted, clamping to zero")
			s.inFlight.Store(0)
			break
		}
		if s.inFlight.CompareAndSwap(cur, cur-1) {
			break
		}
	}
	s.lastRequestEndedAt.Store(s.clock.Now().UnixNano())
	s.mu.Lock()
	close(s.wakeCh)
	s.wakeCh = make(chan struct{})
	s.mu.Unlock()
}

// InFlight returns the current number of in-flight requests. Mostly for
// tests and diagnostics.
func (s *Scheduler) InFlight() int64 {
	return s.inFlight.Load()
}

// WaitForIdle blocks until the in-flight counter is zero AND the time
// since the last request ended exceeds quietWindow, OR maxWait elapses,
// OR ctx is cancelled. Returns ctx.Err() on context cancellation,
// ErrMaxWaitElapsed when the hard cap expires before the quiet window is
// satisfied, and nil when the idle window is achieved.
func (s *Scheduler) WaitForIdle(ctx context.Context, quietWindow, maxWait time.Duration) error {
	deadline := s.clock.Now().Add(maxWait)
	for {
		if err := ctx.Err(); err != nil {
			return err //nolint:wrapcheck // bare ctx.Err is the contract
		}
		now := s.clock.Now()
		if !now.Before(deadline) {
			return ErrMaxWaitElapsed
		}

		// Snapshot wakeCh BEFORE reading the predicate. If RequestEnded
		// closes wakeCh between this snapshot and the select below, the
		// select returns immediately on the closed channel — preventing
		// a lost-wakeup if the predicate read sees stale state.
		s.mu.Lock()
		wake := s.wakeCh
		s.mu.Unlock()

		inFlight := s.inFlight.Load()
		lastEndedNanos := s.lastRequestEndedAt.Load()
		// idleFor can be briefly negative if RequestEnded stored a fresher
		// timestamp between our `now` capture above and this load. That's
		// harmless — the predicate below evaluates to false and we re-check
		// on the next iteration with a fresh `now`.
		idleFor := now.Sub(time.Unix(0, lastEndedNanos))
		if inFlight == 0 && idleFor >= quietWindow {
			return nil
		}

		var nextWake time.Time
		if inFlight == 0 {
			candidate := time.Unix(0, lastEndedNanos).Add(quietWindow)
			if candidate.After(now) {
				nextWake = candidate
			} else {
				nextWake = now.Add(50 * time.Millisecond)
			}
		} else {
			nextWake = now.Add(250 * time.Millisecond)
		}
		if nextWake.After(deadline) {
			nextWake = deadline
		}
		d := nextWake.Sub(now)
		if d <= 0 {
			continue
		}
		timer := s.clock.NewTimer(d)
		select {
		case <-wake:
		case <-timer.Chan():
		case <-ctx.Done():
		}
		timer.Stop()
	}
}

// Schedule spawns a goroutine that waits for an idle window then runs
// task. Logs the wait result; task errors are the task's responsibility.
// Returns immediately. The spawned goroutine is tracked internally so
// callers can block on Wait during shutdown to avoid racing tasks
// against resource teardown (e.g. database close).
func (s *Scheduler) Schedule(
	ctx context.Context,
	name string,
	quietWindow, maxWait time.Duration,
	task func(context.Context),
) {
	s.tasks.Add(1)
	go func() {
		defer s.tasks.Done()
		start := s.clock.Now()
		err := s.WaitForIdle(ctx, quietWindow, maxWait)
		switch {
		case errors.Is(err, ErrMaxWaitElapsed):
			log.Debug().
				Str("task", name).
				Dur("waited", s.clock.Since(start)).
				Msg("idle scheduler: max wait elapsed, running task anyway")
		case err != nil:
			log.Debug().
				Err(err).
				Str("task", name).
				Dur("waited", s.clock.Since(start)).
				Msg("idle scheduler: context cancelled before task ran")
			return
		default:
			log.Debug().
				Str("task", name).
				Dur("waited", s.clock.Since(start)).
				Msg("idle scheduler: running task")
		}
		task(ctx)
	}()
}

// Wait blocks until every goroutine started by Schedule has returned.
// Intended for shutdown: callers should cancel the scheduler's context
// first so any task still in WaitForIdle exits promptly, then call Wait
// before tearing down resources the tasks may still be using.
func (s *Scheduler) Wait() {
	s.tasks.Wait()
}
