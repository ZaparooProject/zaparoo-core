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

func TestThrottle(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	calls := 0
	th := NewThrottle(clock, NotifyInterval, func() { calls++ })

	th.Trigger()
	th.Trigger()
	clock.Advance(NotifyInterval - time.Millisecond)
	th.Trigger()
	assert.Equal(t, 1, calls)

	clock.Advance(time.Millisecond)
	th.Trigger()
	assert.Equal(t, 2, calls)
}

func TestSnapshotButton(t *testing.T) {
	t.Parallel()
	s := NewSnapshot()
	assert.False(t, s.Button(1, 0), "first value is a baseline")
	assert.False(t, s.Button(1, 0))
	assert.True(t, s.Button(1, 1))
	assert.False(t, s.Button(1, 1))
	assert.True(t, s.Button(1, 0))
	assert.False(t, s.Button(2, 1), "keys are independent")
}

func TestSnapshotAxis(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		moves []int32
		want  []bool
	}{
		{
			name: "jitter inside deadzone", moves: []int32{0, 500, -500, 2000, -2000},
			want: []bool{false, false, false, false, false},
		},
		{name: "past deadzone", moves: []int32{0, 2001, -2001}, want: []bool{false, true, true}},
		{
			name: "slow drift never accumulates", moves: []int32{0, 1500, 1900, 1999},
			want: []bool{false, false, false, false},
		},
		{
			name: "reference moves only when counted", moves: []int32{0, 3000, 4000, 5100},
			want: []bool{false, true, false, true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewSnapshot()
			for i, v := range tt.moves {
				assert.Equal(t, tt.want[i], s.Axis(7, v, 2000), "step %d", i)
			}
		})
	}
}

func TestIdleTracker(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	tr := NewIdleTracker(clock)

	assert.False(t, tr.Observe(10*time.Second), "baseline")
	clock.Advance(250 * time.Millisecond)
	assert.False(t, tr.Observe(10250*time.Millisecond), "idle grew with the clock")
	clock.Advance(250 * time.Millisecond)
	assert.False(t, tr.Observe(10500*time.Millisecond+30*time.Millisecond), "jitter within tolerance")
	clock.Advance(250 * time.Millisecond)
	assert.True(t, tr.Observe(100*time.Millisecond), "input between readings")
	clock.Advance(250 * time.Millisecond)
	assert.False(t, tr.Observe(350*time.Millisecond))
	clock.Advance(250 * time.Millisecond)
	assert.True(t, tr.Observe(0), "input right at the reading")
}

func TestChatterFilter(t *testing.T) {
	t.Parallel()
	start := time.Unix(1000, 0)
	at := start.Add

	t.Run("person pressing keys is allowed", func(t *testing.T) {
		t.Parallel()
		var f ChatterFilter
		for i := range 40 {
			assert.True(t, f.Allow(at(time.Duration(i)*500*time.Millisecond), 1))
		}
	})

	t.Run("continuously moving mouse is never filtered", func(t *testing.T) {
		t.Parallel()
		var f ChatterFilter
		for i := range 3000 { // 30 seconds at 100 events/s
			assert.True(t, f.Allow(at(time.Duration(i)*10*time.Millisecond), uint64(i%37)))
		}
	})

	t.Run("wiggling stick is never filtered", func(t *testing.T) {
		t.Parallel()
		var f ChatterFilter
		for i := range 2000 {
			assert.True(t, f.Allow(at(time.Duration(i)*10*time.Millisecond), uint64(1000+i%11)))
		}
	})

	t.Run("pegged axis repeating one value is suppressed after the window", func(t *testing.T) {
		t.Parallel()
		var f ChatterFilter
		allowed := 0
		for i := range 1000 {
			if f.Allow(at(time.Duration(i)*10*time.Millisecond), 42) {
				allowed++
			}
		}
		assert.Less(t, allowed, 600, "events after the first five seconds are dropped")
		assert.False(t, f.Allow(at(10*time.Second+10*time.Millisecond), 42))
	})

	t.Run("two alternating values still count as stuck", func(t *testing.T) {
		t.Parallel()
		var f ChatterFilter
		for i := range 1000 {
			f.Allow(at(time.Duration(i)*10*time.Millisecond), uint64(i%2))
		}
		assert.False(t, f.Allow(at(10*time.Second+10*time.Millisecond), 1))
	})

	t.Run("varied input frees a suppressed device", func(t *testing.T) {
		t.Parallel()
		var f ChatterFilter
		for i := range 1000 {
			f.Allow(at(time.Duration(i)*10*time.Millisecond), 42)
		}
		assert.False(t, f.Allow(at(10*time.Second+10*time.Millisecond), 42))
		assert.False(t, f.Allow(at(10*time.Second+20*time.Millisecond), 7), "two values is still stuck")
		assert.True(t, f.Allow(at(10*time.Second+30*time.Millisecond), 8), "a third value shows a person")
	})

	t.Run("a pause clears the suppression", func(t *testing.T) {
		t.Parallel()
		var f ChatterFilter
		for i := range 1000 {
			f.Allow(at(time.Duration(i)*10*time.Millisecond), 42)
		}
		assert.True(t, f.Allow(at(12*time.Second), 42))
	})

	t.Run("slow identical events are not chatter", func(t *testing.T) {
		t.Parallel()
		var f ChatterFilter
		for i := range 100 {
			assert.True(t, f.Allow(at(time.Duration(i)*200*time.Millisecond), 5))
		}
	})
}

func TestApplyJoySample(t *testing.T) {
	t.Parallel()
	centre := joySample{axes: [joyAxisCount]uint32{32767, 32767, 32767, 32767, 32767, 32767}, pov: 65535}
	snap := NewSnapshot()
	assert.False(t, applyJoySample(snap, 0, centre))
	assert.False(t, applyJoySample(snap, 0, centre))

	small := centre
	small.axes[0] = 32767 + 3000
	assert.False(t, applyJoySample(snap, 0, small), "stick drift inside the deadzone")

	big := centre
	big.axes[1] = 32767 + 20000
	assert.True(t, applyJoySample(snap, 0, big))

	pressed := big
	pressed.buttons = 1
	assert.True(t, applyJoySample(snap, 0, pressed))

	hat := pressed
	hat.pov = 9000
	assert.True(t, applyJoySample(snap, 0, hat))

	assert.False(t, applyJoySample(snap, 1, centre), "another joystick has its own baseline")
}
