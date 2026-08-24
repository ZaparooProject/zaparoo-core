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

package syncutil

import (
	"context"
	"time"
)

type pauseState int

const (
	stateRunning pauseState = iota
	stateThrottled
	statePaused
)

// ThrottleLevel selects how aggressively the throttled state's duty cycle
// slows work down.
type ThrottleLevel int

const (
	// ThrottleLight is the default while ordinary foreground media is
	// active. 50ms/150ms yields ~25% duty, keeping storage and CPU mostly
	// free for the foreground consumer while still making steady progress.
	ThrottleLight ThrottleLevel = iota
	// ThrottleHeavy is for storage-streaming cores (CD-based, etc.) whose
	// continuous reads are sensitive to any competing I/O. 20ms/300ms
	// yields ~6% duty. On-device testing (MiSTer, CD-streaming arcade
	// core) showed the lighter duty cycle still let indexing's storage
	// bursts interfere with playback, so the work window is short and
	// infrequent enough to stay out of a foreground consumer's way.
	ThrottleHeavy
	// ThrottleBackground is a baseline for long-running jobs even when no
	// media is active. Equal work and sleep quanta reserve regular CPU time
	// for UI, API, reader, and audio work without changing process priority.
	ThrottleBackground
)

const (
	backgroundThrottleWork   = 40 * time.Millisecond
	backgroundThrottleSleep  = 40 * time.Millisecond
	lightThrottleWork        = 50 * time.Millisecond
	lightThrottleSleep       = 150 * time.Millisecond
	heavyThrottleWork        = 20 * time.Millisecond
	heavyThrottleSleep       = 300 * time.Millisecond
	maxBaselineThrottleSleep = time.Second
)

// quantaForLevel returns the work/sleep duty-cycle quanta for a throttle level.
func quantaForLevel(level ThrottleLevel) (work, sleep time.Duration) {
	switch level {
	case ThrottleHeavy:
		return heavyThrottleWork, heavyThrottleSleep
	case ThrottleBackground:
		return backgroundThrottleWork, backgroundThrottleSleep
	default:
		return lightThrottleWork, lightThrottleSleep
	}
}

func baselineSleepForElapsed(elapsed, workQuantum, sleepQuantum time.Duration) time.Duration {
	sleep := sleepQuantum
	if elapsed > workQuantum && workQuantum > 0 {
		sleep = time.Duration(float64(sleepQuantum) * (float64(elapsed) / float64(workQuantum)))
	}
	return min(sleep, maxBaselineThrottleSleep)
}

// Pauser is a thread-safe pause/throttle/resume primitive using the
// closed-channel pattern. When running, Wait returns immediately. When
// paused, Wait blocks until Resume is called or the context is cancelled.
// When throttled, Wait enforces a duty cycle: callers run unimpeded for a
// work quantum, then Wait sleeps for a sleep quantum before the next window.
//
// A nil *Pauser is safe to use: Wait always returns nil.
type Pauser struct {
	workStart           time.Time
	baselineWorkStart   time.Time
	ch                  chan struct{}
	workQuantum         time.Duration
	sleepQuantum        time.Duration
	baselineWorkQuantum time.Duration
	baselineSleep       time.Duration
	state               pauseState
	level               ThrottleLevel
	mu                  Mutex
	baselineEnabled     bool
}

// NewPauser returns a Pauser in the running state.
func NewPauser() *Pauser {
	ch := make(chan struct{})
	close(ch)
	work, sleep := quantaForLevel(ThrottleLight)
	return &Pauser{
		ch:           ch,
		workQuantum:  work,
		sleepQuantum: sleep,
	}
}

// SetBaselineThrottle enables a duty cycle while the pauser is otherwise in
// its running state. Explicit Pause and Throttle calls still override it, and
// Resume returns to this baseline. Baseline pacing does not make IsThrottled
// true, so status reporting and transaction sizing continue to describe only
// foreground-media restrictions.
func (p *Pauser) SetBaselineThrottle(level ThrottleLevel) {
	if p == nil {
		return
	}
	work, sleep := quantaForLevel(level)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.baselineEnabled = true
	p.baselineWorkQuantum = work
	p.baselineSleep = sleep
	p.baselineWorkStart = time.Now()
}

// Pause requests a full pause. Idempotent: calling Pause when already
// paused is a no-op. Pause overrides a throttled state.
func (p *Pauser) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == statePaused {
		return
	}
	if p.state == stateRunning {
		p.ch = make(chan struct{})
	}
	// From throttled the channel is already open; reuse it so throttled
	// sleepers wake only on Resume/Throttle, not on the state change.
	p.state = statePaused
}

// Throttle requests the duty-cycled throttled state at the given level.
// Idempotent: calling Throttle with the level already active is a no-op and
// does not reset the current work window. Calling Throttle with a different
// level while already throttled switches the duty cycle immediately.
// Calling Throttle when paused releases blocked waiters into the throttled
// state.
func (p *Pauser) Throttle(level ThrottleLevel) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == stateThrottled && p.level == level {
		return
	}
	if p.state == statePaused {
		// Wake anyone blocked in the paused state; they re-enter Wait and
		// observe the throttled state.
		close(p.ch)
	}
	if p.state != stateThrottled {
		p.ch = make(chan struct{})
	}
	p.level = level
	p.workQuantum, p.sleepQuantum = quantaForLevel(level)
	p.state = stateThrottled
	p.workStart = time.Now()
}

// Resume returns to the running state, unblocking all goroutines waiting in
// Wait. Idempotent: calling Resume when running is a no-op.
func (p *Pauser) Resume() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.baselineWorkStart = time.Now()
	if p.state == stateRunning {
		return
	}
	p.state = stateRunning
	close(p.ch)
}

// IsPaused reports whether the Pauser is currently in the paused state. A nil
// receiver reports false.
func (p *Pauser) IsPaused() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state == statePaused
}

// IsThrottled reports whether the Pauser is currently in the throttled state.
// A nil receiver reports false.
func (p *Pauser) IsThrottled() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state == stateThrottled
}

// HasBaselineThrottle reports whether running work uses baseline pacing.
func (p *Pauser) HasBaselineThrottle() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.baselineEnabled
}

// Level returns the throttle level applied by the most recent Throttle call.
// Only meaningful while IsThrottled is true. A nil receiver returns ThrottleLight.
func (p *Pauser) Level() ThrottleLevel {
	if p == nil {
		return ThrottleLight
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.level
}

// SetThrottleQuanta overrides the explicit throttle duty cycle. Non-positive
// values are ignored. Intended for configuration and tests.
func (p *Pauser) SetThrottleQuanta(work, sleep time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if work > 0 {
		p.workQuantum = work
	}
	if sleep > 0 {
		p.sleepQuantum = sleep
	}
}

// SetBaselineThrottleQuanta overrides the running-state baseline duty cycle.
// Non-positive values are ignored. Intended for configuration and tests.
func (p *Pauser) SetBaselineThrottleQuanta(work, sleep time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if work > 0 {
		p.baselineWorkQuantum = work
	}
	if sleep > 0 {
		p.baselineSleep = sleep
	}
}

// Wait applies the current state to the caller. Running work returns
// immediately unless a baseline throttle is enabled, paused work blocks until
// Resume, and explicitly throttled work follows its stronger duty cycle. It
// returns the context error if the context is cancelled while blocked or
// sleeping. A nil receiver returns nil.
func (p *Pauser) Wait(ctx context.Context) error {
	return p.applyState(ctx, true)
}

// WaitForPacing applies baseline and explicit throttle pacing without blocking
// for a full pause. Use it while a transaction or other resource must be
// released before the caller can safely honor Pause with Wait.
func (p *Pauser) WaitForPacing(ctx context.Context) error {
	return p.applyState(ctx, false)
}

func (p *Pauser) applyState(ctx context.Context, blockWhilePaused bool) error {
	if p == nil {
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		p.mu.Lock()
		state := p.state
		ch := p.ch
		sleepQuantum := p.sleepQuantum
		workExpired := state == stateThrottled && time.Since(p.workStart) >= p.workQuantum
		baselineEnabled := p.baselineEnabled
		baselineElapsed := time.Since(p.baselineWorkStart)
		baselineSleep := baselineSleepForElapsed(
			baselineElapsed, p.baselineWorkQuantum, p.baselineSleep,
		)
		pacingSleep := sleepQuantum
		if baselineEnabled {
			pacingSleep = max(pacingSleep, baselineSleep)
		}
		baselineExpired := state == stateRunning && baselineEnabled &&
			baselineElapsed >= p.baselineWorkQuantum
		p.mu.Unlock()

		switch state {
		case stateRunning:
			if !baselineExpired {
				return nil
			}
			timer := time.NewTimer(baselineSleep)
			select {
			case <-timer.C:
				p.mu.Lock()
				stillRunning := p.state == stateRunning
				if stillRunning && p.baselineEnabled {
					p.baselineWorkStart = time.Now()
				}
				p.mu.Unlock()
				if stillRunning {
					return nil
				}
				continue
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		case statePaused:
			if !blockWhilePaused {
				timer := time.NewTimer(pacingSleep)
				select {
				case <-timer.C:
					return nil
				case <-ch:
					timer.Stop()
					continue
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				}
			}
			select {
			case <-ch:
				// State changed; loop to observe the new state.
			case <-ctx.Done():
				return ctx.Err()
			}
		case stateThrottled:
			if !workExpired {
				return nil
			}
			timer := time.NewTimer(sleepQuantum)
			select {
			case <-timer.C:
				p.mu.Lock()
				// Only reset the window if still throttled; a state change
				// during the sleep is handled on the next Wait call.
				if p.state == stateThrottled {
					p.workStart = time.Now()
				}
				p.mu.Unlock()
				return nil
			case <-ch:
				timer.Stop()
				// State changed; loop to observe the new state.
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
	}
}
