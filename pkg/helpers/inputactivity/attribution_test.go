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
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
)

func TestLastInputInjected(t *testing.T) {
	t.Parallel()
	inj := time.Unix(5000, 0)
	tests := []struct {
		inputAt time.Time
		injAt   time.Time
		name    string
		want    bool
	}{
		{inj, time.Time{}, "never injected", false},
		{inj, inj, "same instant", true},
		{inj.Add(20 * time.Millisecond), inj, "tick rounding after", true},
		{inj.Add(keyboardInjectionTolerance), inj, "just inside tolerance", true},
		{inj.Add(keyboardInjectionTolerance + time.Millisecond), inj, "real keypress after the window", false},
		{inj.Add(-5 * time.Second), inj, "older real input", false},
		{inj.Add(-100 * time.Millisecond), inj, "slightly early", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, lastInputInjected(tt.inputAt, tt.injAt))
		})
	}
}

func TestPadChangeInjected(t *testing.T) {
	t.Parallel()
	inj := time.Unix(5000, 0)
	assert.False(t, padChangeInjected(inj, time.Time{}))
	assert.True(t, padChangeInjected(inj.Add(250*time.Millisecond), inj))
	assert.True(t, padChangeInjected(inj.Add(padInjectionWindow), inj))
	assert.False(t, padChangeInjected(inj.Add(padInjectionWindow+time.Millisecond), inj), "later input is a person's")
	assert.False(t, padChangeInjected(inj.Add(-time.Second), inj), "a change well before the injection is a person's")
}

// A series of injections every 7s must keep being discounted while a real
// press between them is not.
func TestAttributionOverRepeatedInjections(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	start := clock.Now()
	for i := range 8 {
		inj := start.Add(time.Duration(i) * 7 * time.Second)
		assert.True(t, padChangeInjected(inj.Add(250*time.Millisecond), inj), "poll right after injection %d", i)
	}
	lastInj := start.Add(7 * 7 * time.Second)
	assert.False(t, padChangeInjected(lastInj.Add(3*time.Second), lastInj), "a press between injections counts")
}

func TestDeviceDiscovery(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	d := newDeviceDiscovery(30 * time.Second)

	assert.True(t, d.due(1, clock.Now()), "first look always runs")
	d.record(1, []uint32{0, 2}, clock.Now())
	assert.Equal(t, []uint32{0, 2}, d.present())

	clock.Advance(time.Hour)
	assert.False(t, d.due(1, clock.Now()), "no change in devices, no probe however long it has been")

	assert.True(t, d.due(2, clock.Now()), "devices changed and the interval has passed")
	d.record(2, []uint32{0}, clock.Now())

	clock.Advance(10 * time.Second)
	assert.False(t, d.due(3, clock.Now()), "devices changed again too soon")
	clock.Advance(20 * time.Second)
	assert.True(t, d.due(3, clock.Now()))
	d.record(3, nil, clock.Now())
	assert.Empty(t, d.present())
}

func TestDeviceDiscoveryDropRequestsRefresh(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	d := newDeviceDiscovery(30 * time.Second)
	d.record(1, []uint32{0, 1}, clock.Now())

	d.drop(0)
	assert.Equal(t, []uint32{1}, d.present())
	assert.False(t, d.due(1, clock.Now()), "still rate limited")
	clock.Advance(31 * time.Second)
	assert.True(t, d.due(1, clock.Now()), "a vanished device triggers a fresh round")
	d.record(1, []uint32{1}, clock.Now())
	assert.False(t, d.due(1, clock.Now()))
}

// Counts the worst case: probes only on change, never faster than the interval.
func TestDeviceDiscoveryBoundsProbes(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	d := newDeviceDiscovery(30 * time.Second)
	probes := 0
	// One hour of polling every 2s while a device flaps on every poll.
	for i := range 1800 {
		sig := uint64(i) //nolint:gosec // loop index
		if d.due(sig, clock.Now()) {
			probes++
			d.record(sig, nil, clock.Now())
		}
		clock.Advance(2 * time.Second)
	}
	assert.LessOrEqual(t, probes, 121)
}

func TestFingerprint(t *testing.T) {
	t.Parallel()
	a := fingerprint([]uintptr{1, 2, 3})
	assert.Equal(t, a, fingerprint([]uintptr{1, 2, 3}))
	assert.NotEqual(t, a, fingerprint([]uintptr{1, 2}))
	assert.NotEqual(t, a, fingerprint([]uintptr{1, 2, 4}))
}

func TestLastInputFromCore(t *testing.T) {
	t.Parallel()
	kb := time.Unix(5000, 0)
	pad := time.Unix(6000, 0)
	assert.True(t, lastInputFromCore(kb.Add(10*time.Millisecond), kb, pad))
	assert.True(t, lastInputFromCore(pad.Add(10*time.Millisecond), kb, pad))
	assert.True(t, lastInputFromCore(pad.Add(600*time.Millisecond), kb, pad),
		"a pad report delivered a poll later is still Core's")
	assert.False(t, lastInputFromCore(kb.Add(600*time.Millisecond), kb, pad),
		"a key 600ms after a key injection is a person's")
	assert.False(t, lastInputFromCore(pad.Add(2*time.Second), kb, pad))
	assert.False(t, lastInputFromCore(kb, time.Time{}, time.Time{}))
}

func TestDiagLimiter(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	l := newDiagLimiter(clock)
	assert.True(t, l.allow("xinput"))
	assert.False(t, l.allow("xinput"))
	assert.True(t, l.allow("winmm"), "sources are limited independently")
	clock.Advance(diagGap - time.Millisecond)
	assert.False(t, l.allow("xinput"))
	clock.Advance(time.Millisecond)
	assert.True(t, l.allow("xinput"))
}
