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
	// ThrottleLight is the default for most cores: work for one quantum,
	// then sleep. 50ms/150ms yields ~25% duty, keeping storage and CPU
	// mostly free for a foreground consumer while still making steady
	// progress.
	ThrottleLight ThrottleLevel = iota
	// ThrottleHeavy is for storage-streaming cores (CD-based, etc.) whose
	// continuous reads are sensitive to any competing I/O. 20ms/300ms
	// yields ~6% duty. On-device testing (MiSTer, CD-streaming arcade
	// core) showed the lighter duty cycle still let indexing's storage
	// bursts interfere with playback, so the work window is short and
	// infrequent enough to stay out of a foreground consumer's way.
	ThrottleHeavy
)

const (
	lightThrottleWork  = 50 * time.Millisecond
	lightThrottleSleep = 150 * time.Millisecond
	heavyThrottleWork  = 20 * time.Millisecond
	heavyThrottleSleep = 300 * time.Millisecond
)

// quantaForLevel returns the work/sleep duty-cycle quanta for a throttle level.
func quantaForLevel(level ThrottleLevel) (work, sleep time.Duration) {
	if level == ThrottleHeavy {
		return heavyThrottleWork, heavyThrottleSleep
	}
	return lightThrottleWork, lightThrottleSleep
}

// Pauser is a thread-safe pause/throttle/resume primitive using the
// closed-channel pattern. When running, Wait returns immediately. When
// paused, Wait blocks until Resume is called or the context is cancelled.
// When throttled, Wait enforces a duty cycle: callers run unimpeded for a
// work quantum, then Wait sleeps for a sleep quantum before the next window.
//
// A nil *Pauser is safe to use: Wait always returns nil.
type Pauser struct {
	workStart    time.Time
	ch           chan struct{}
	workQuantum  time.Duration
	sleepQuantum time.Duration
	state        pauseState
	level        ThrottleLevel
	mu           Mutex
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

// SetThrottleQuanta overrides the throttle duty cycle. Non-positive values
// are ignored. Intended for configuration and tests.
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

// Wait applies the current state to the caller. It returns nil immediately
// when running, blocks until Resume while paused, and enforces the duty
// cycle while throttled. It returns the context error if the context is
// cancelled while blocked or sleeping. A nil receiver returns nil.
func (p *Pauser) Wait(ctx context.Context) error {
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
		p.mu.Unlock()

		switch state {
		case stateRunning:
			return nil
		case statePaused:
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
