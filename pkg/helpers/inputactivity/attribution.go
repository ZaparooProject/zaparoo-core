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

package inputactivity

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
)

// Core's own synthetic input moves the same system counters a person does, so
// a source that reads them must discount input that Core itself caused.
//
// Residual limitation: a real keypress within keyboardInjectionTolerance of a
// Core key injection, or a real pad change inside padInjectionWindow of a Core
// pad injection, cannot be told apart from the injection and is missed. The
// next real input after that window counts as usual.
const (
	// keyboardInjectionTolerance bounds how far the system's last-input time
	// may sit from Core's recorded injection time and still be Core's.
	keyboardInjectionTolerance = 300 * time.Millisecond
	// padInjectionWindow is how long after a Core pad injection a polled pad
	// change is still attributed to it; the virtual pad's state reaches the
	// XInput and winmm APIs asynchronously.
	padInjectionWindow = time.Second
)

// lastInputInjected reports whether the system's last keyboard or mouse input,
// whose own timestamp is inputAt, is Core's injection at injectedAt. Input
// later than the tolerance is a person's even if an injection came first.
func lastInputInjected(inputAt, injectedAt time.Time) bool {
	if injectedAt.IsZero() {
		return false
	}
	diff := inputAt.Sub(injectedAt)
	return diff >= -keyboardInjectionTolerance && diff <= keyboardInjectionTolerance
}

// lastInputFromCore reports whether the system's last input, stamped inputAt,
// was caused by Core. A key injection moves it within a few ticks. A virtual
// gamepad report can move it too (the system counts game controller input as
// activity), and that effect arrives late: the driver holds a report until the
// next poll of the pad, so it lands up to a poll interval after Core's stamp.
// Gamepad injections are therefore matched with the longer pad window.
func lastInputFromCore(inputAt, keyboardInjectedAt, gamepadInjectedAt time.Time) bool {
	return lastInputInjected(inputAt, keyboardInjectedAt) || padChangeInjected(inputAt, gamepadInjectedAt)
}

// padChangeInjected reports whether a pad change first seen by a poll at
// seenAt falls inside the window after Core's last pad injection.
func padChangeInjected(seenAt, injectedAt time.Time) bool {
	if injectedAt.IsZero() {
		return false
	}
	diff := seenAt.Sub(injectedAt)
	return diff >= -keyboardInjectionTolerance && diff <= padInjectionWindow
}

// deviceDiscovery decides when to look for present joysticks again. Probing a
// winmm joystick id that has no device leaks a handle inside winmm, about one
// per 15 probes, and the handle is only released when the process exits. So
// absent ids are probed only when the machine's set of input devices has
// changed (a fingerprint supplied by the caller) and at most once per
// minInterval, never on a timer.
//
// Worst case that is one 16-id round, roughly one leaked handle, per
// minInterval and only while devices are being plugged and unplugged.
type deviceDiscovery struct {
	lastProbe   time.Time
	ids         []uint32
	minInterval time.Duration
	signature   uint64
	probed      bool
	stale       bool
	mu          syncutil.Mutex
}

func newDeviceDiscovery(minInterval time.Duration) *deviceDiscovery {
	return &deviceDiscovery{minInterval: minInterval}
}

// due reports whether a probe round should run now for the given device
// fingerprint.
func (d *deviceDiscovery) due(signature uint64, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.probed {
		return true
	}
	if signature == d.signature && !d.stale {
		return false
	}
	return now.Sub(d.lastProbe) >= d.minInterval
}

// record stores the result of a probe round.
func (d *deviceDiscovery) record(signature uint64, ids []uint32, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.probed = true
	d.stale = false
	d.signature = signature
	d.lastProbe = now
	d.ids = append(d.ids[:0], ids...)
}

// present returns the ids confirmed present by the last probe round.
func (d *deviceDiscovery) present() []uint32 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]uint32(nil), d.ids...)
}

// drop forgets an id that stopped answering and asks for a fresh round.
func (d *deviceDiscovery) drop(id uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, have := range d.ids {
		if have == id {
			d.ids = append(d.ids[:i], d.ids[i+1:]...)
			break
		}
	}
	d.stale = true
}

// fingerprint folds a list of device handles into one value that changes when
// any device is added or removed.
func fingerprint(handles []uintptr) uint64 {
	h := uint64(fnvOffset)
	for _, v := range handles {
		h = (h ^ uint64(v)) * fnvPrime
	}
	return h
}

// diagGap is the least time between two diagnostic lines for one source.
const diagGap = 500 * time.Millisecond

// diagLimiter keeps per-source diagnostic logging from flooding the log: a
// source that changes on every poll logs at most once per diagGap.
type diagLimiter struct {
	clock clockwork.Clock
	last  map[string]time.Time
}

func newDiagLimiter(clock clockwork.Clock) *diagLimiter {
	return &diagLimiter{clock: clock, last: make(map[string]time.Time)}
}

// allow reports whether a line for key may be written now.
func (l *diagLimiter) allow(key string) bool {
	now := l.clock.Now()
	if prev, ok := l.last[key]; ok && now.Sub(prev) < diagGap {
		return false
	}
	l.last[key] = now
	return true
}
