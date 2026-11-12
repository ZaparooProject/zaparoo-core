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

// Package inputactivity detects whether a person is using a device's
// keyboard, mouse or controllers. The decision logic lives in portable files
// so it can be tested without hardware; the OS-specific files only gather raw
// samples.
package inputactivity

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
)

// Class is a set of kinds of input. The bit values match platforms.InputClass
// so a platform can convert with a plain cast.
type Class uint8

const (
	// Keyboard is key presses on a keyboard.
	Keyboard Class = 1 << iota
	// Mouse is mouse or pointer movement and buttons.
	Mouse
	// Controller is button and stick activity on a game controller.
	Controller
)

// Has reports whether every class in want is in c.
func (c Class) Has(want Class) bool {
	return c&want == want
}

const (
	// NotifyInterval is the shortest gap between two callbacks.
	NotifyInterval = 200 * time.Millisecond

	// PollInterval is how often polled sources sample while idle rotation is
	// only waiting for someone to go away.
	PollInterval = 250 * time.Millisecond
	// ResponsivePollInterval is the sampling gap while a countdown is on
	// screen and one tap has to register.
	ResponsivePollInterval = 20 * time.Millisecond
)

// pollEvery returns the sampling gap for the responsive flag.
func pollEvery(responsive bool) time.Duration {
	if responsive {
		return ResponsivePollInterval
	}
	return PollInterval
}

// Throttle forwards at most one call per interval.
type Throttle struct {
	clock    clockwork.Clock
	fn       func()
	last     time.Time
	interval time.Duration
	mu       syncutil.Mutex
	fired    bool
}

// NewThrottle returns a Throttle that calls fn no more often than interval.
func NewThrottle(clock clockwork.Clock, interval time.Duration, fn func()) *Throttle {
	return &Throttle{clock: clock, interval: interval, fn: fn}
}

// Trigger records activity and calls fn unless it already did within the
// interval. fn runs outside the throttle's lock.
func (t *Throttle) Trigger() {
	now := t.clock.Now()
	t.mu.Lock()
	if t.fired && now.Sub(t.last) < t.interval {
		t.mu.Unlock()
		return
	}
	t.fired = true
	t.last = now
	t.mu.Unlock()
	t.fn()
}

// Snapshot remembers the last value of each button and axis of a polled
// device so a change can be told from a held position.
type Snapshot struct {
	vals map[uint32]int32
}

// NewSnapshot returns an empty snapshot.
func NewSnapshot() *Snapshot {
	return &Snapshot{vals: make(map[uint32]int32)}
}

// Button records a button value and reports whether it changed. The first
// value seen for a key is a baseline, not activity.
func (s *Snapshot) Button(key uint32, value int32) bool {
	prev, ok := s.vals[key]
	s.vals[key] = value
	return ok && prev != value
}

// Axis records an axis value and reports whether it moved more than deadzone
// from the last value that counted. Small drift never accumulates into
// activity because the reference only advances when it is exceeded.
func (s *Snapshot) Axis(key uint32, value, deadzone int32) bool {
	prev, ok := s.vals[key]
	if !ok {
		s.vals[key] = value
		return false
	}
	diff := value - prev
	if diff < 0 {
		diff = -diff
	}
	if diff <= deadzone {
		return false
	}
	s.vals[key] = value
	return true
}

// IdleTracker turns a stream of "time since last input" readings into
// activity. Idle time only ever grows by the time between readings unless
// someone touched an input in between.
type IdleTracker struct {
	clock   clockwork.Clock
	at      time.Time
	idle    time.Duration
	started bool
}

// idleTolerance absorbs scheduling jitter between the reading and the clock.
const idleTolerance = 50 * time.Millisecond

// NewIdleTracker returns a tracker reading time from clock.
func NewIdleTracker(clock clockwork.Clock) *IdleTracker {
	return &IdleTracker{clock: clock}
}

// Observe records an idle reading and reports whether input happened since
// the previous one. The first reading is a baseline.
func (t *IdleTracker) Observe(idle time.Duration) bool {
	now := t.clock.Now()
	defer func() {
		t.at = now
		t.idle = idle
		t.started = true
	}()
	if !t.started {
		return false
	}
	expected := t.idle + now.Sub(t.at)
	return idle+idleTolerance < expected
}

// Stuck-device thresholds: a device is treated as broken, not a person, once
// it reports the same thing more than chatterRate times per second for
// chatterWindow without ever pausing for chatterGap. Rate alone is never
// enough: a person moving a mouse or stick reports just as fast, but what
// they report keeps changing.
const (
	chatterRate     = 20.0
	chatterWindow   = 5 * time.Second
	chatterGap      = time.Second
	chatterDistinct = 2
)

// ChatterFilter suppresses a device that is stuck. A person pauses or varies
// what they do; a gamepad keyboard interface, a pegged axis or a flapping
// switch repeats the same event nonstop.
type ChatterFilter struct {
	streakStart time.Time
	last        time.Time
	seen        [chatterDistinct]uint64
	count       int
	distinct    int
	started     bool
	latched     bool
}

// Allow records one activity event at now, identified by sig (a digest of
// what it reported), and reports whether it should count as activity.
func (f *ChatterFilter) Allow(now time.Time, sig uint64) bool {
	if !f.started || now.Sub(f.last) >= chatterGap {
		*f = ChatterFilter{started: true, streakStart: now}
	}
	f.last = now
	f.count++
	f.observe(sig)
	if f.distinct > chatterDistinct {
		// Something new came in, so the device is not stuck.
		f.latched = false
		return true
	}
	if f.latched {
		return false
	}
	elapsed := now.Sub(f.streakStart)
	if elapsed >= chatterWindow && float64(f.count)/elapsed.Seconds() > chatterRate {
		f.latched = true
		return false
	}
	return true
}

func (f *ChatterFilter) observe(sig uint64) {
	if f.distinct > chatterDistinct {
		return
	}
	for i := range f.distinct {
		if f.seen[i] == sig {
			return
		}
	}
	if f.distinct < chatterDistinct {
		f.seen[f.distinct] = sig
	}
	f.distinct++
}
