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
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func eventBytes(events ...inputEvent) []byte {
	out := make([]byte, 0, len(events)*evSize)
	tv := evSize - evHeaderSize
	for _, ev := range events {
		buf := make([]byte, evSize)
		binary.NativeEndian.PutUint16(buf[tv:], ev.typ)
		binary.NativeEndian.PutUint16(buf[tv+2:], ev.code)
		binary.NativeEndian.PutUint32(buf[tv+4:], uint32(ev.value)) //nolint:gosec // test data
		out = append(out, buf...)
	}
	return out
}

func TestParseEvents(t *testing.T) {
	t.Parallel()
	want := []inputEvent{{typ: evKey, code: 30, value: 1}, {typ: evAbs, code: 1, value: -20000}}
	data := eventBytes(want...)

	assert.Equal(t, want, parseEvents(data))
	assert.Equal(t, want[:1], parseEvents(data[:evSize+5]), "partial trailing event is dropped")
	assert.Empty(t, parseEvents(nil))
}

func TestEventsActive(t *testing.T) {
	t.Parallel()
	newAxes := func() *axisTracker {
		tr := newAxisTracker(nil)
		tr.seed(0, axisRange{min: -32768, max: 32767, value: 0})
		tr.seed(2, axisRange{min: 0, max: 255, value: 0})
		tr.seed(16, axisRange{min: -1, max: 1, value: 0})
		return tr
	}
	tests := []struct {
		name   string
		events []inputEvent
		class  Class
		want   bool
	}{
		{"key press", []inputEvent{{typ: evKey, code: 30, value: 1}}, Keyboard, true},
		{"key repeat", []inputEvent{{typ: evKey, code: 30, value: 2}}, Keyboard, true},
		{"key release only", []inputEvent{{typ: evKey, code: 30, value: 0}}, Keyboard, false},
		{"keyboard ignores relative", []inputEvent{{typ: evRel, code: 0, value: 5}}, Keyboard, false},
		{"mouse move", []inputEvent{{typ: evRel, code: 0, value: 3}}, Mouse, true},
		{"mouse zero move", []inputEvent{{typ: evRel, code: 0, value: 0}}, Mouse, false},
		{"mouse button", []inputEvent{{typ: evKey, code: 0x110, value: 1}}, Mouse, true},
		{"pad button", []inputEvent{{typ: evKey, code: 0x130, value: 1}}, Controller, true},
		{"pad repeat is not a press", []inputEvent{{typ: evKey, code: 0x130, value: 2}}, Controller, false},
		{"stick inside deadzone", []inputEvent{{typ: evAbs, code: 0, value: 3000}}, Controller, false},
		{"stick past deadzone", []inputEvent{{typ: evAbs, code: 0, value: 9000}}, Controller, true},
		{"trigger past deadzone", []inputEvent{{typ: evAbs, code: 2, value: 40}}, Controller, true},
		{"hat press", []inputEvent{{typ: evAbs, code: 16, value: 1}}, Controller, true},
		{"abs ignored on a keyboard", []inputEvent{{typ: evAbs, code: 0, value: 30000}}, Keyboard, false},
		{"sync only", []inputEvent{{typ: 0, code: 0, value: 0}}, Controller, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := eventsActive(tt.class, tt.events, newAxes())
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAxisTrackerUnseededUsesLookup(t *testing.T) {
	t.Parallel()
	tr := newAxisTracker(func(uint16) (axisRange, bool) { return axisRange{min: 0, max: 100}, true })
	assert.False(t, tr.moved(3, 50), "first value is a baseline")
	assert.False(t, tr.moved(3, 55))
	assert.True(t, tr.moved(3, 70), "12 percent of a 0..100 axis is 12")
}

func TestAxisTrackerFallbackRange(t *testing.T) {
	t.Parallel()
	tr := newAxisTracker(nil)
	require.False(t, tr.moved(1, 0))
	assert.False(t, tr.moved(1, 7000))
	assert.True(t, tr.moved(1, 9000))
}

func TestApplyJSEvents(t *testing.T) {
	t.Parallel()
	ev := func(value int16, typ, number byte) []byte {
		b := make([]byte, jsEventSize)
		binary.NativeEndian.PutUint16(b[4:], uint16(value)) //nolint:gosec // test data
		b[6], b[7] = typ, number
		return b
	}
	cat := func(parts ...[]byte) []byte {
		var out []byte
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}

	snap := NewSnapshot()
	init := cat(ev(0, jsButton|jsInit, 0), ev(0, jsAxis|jsInit, 0), ev(-32767, jsAxis|jsInit, 2))
	assert.False(t, applyJSEvents(snap, init), "init replay is the baseline")
	assert.False(t, applyJSEvents(snap, init), "unchanged replay")
	assert.False(t, applyJSEvents(snap, nil))
	assert.False(t, applyJSEvents(snap, ev(1500, jsAxis|jsInit, 0)), "inside deadzone")
	assert.True(t, applyJSEvents(snap, ev(2500, jsAxis|jsInit, 0)), "axis beyond deadzone")
	assert.True(t, applyJSEvents(snap, ev(1, jsButton|jsInit, 0)), "button down")
	assert.True(t, applyJSEvents(snap, ev(0, jsButton, 0)), "button up as a live event")
	assert.False(t, applyJSEvents(snap, init[:5]), "truncated event ignored")
}

func TestEventsActiveSignature(t *testing.T) {
	t.Parallel()
	sig := func(events ...inputEvent) uint64 {
		_, s := eventsActive(Mouse, events, nil)
		return s
	}
	a := sig(inputEvent{typ: evRel, code: 0, value: 3})
	assert.Equal(t, a, sig(inputEvent{typ: evRel, code: 0, value: 3}))
	assert.NotEqual(t, a, sig(inputEvent{typ: evRel, code: 0, value: 4}))
	assert.NotEqual(t, a, sig(inputEvent{typ: evRel, code: 1, value: 3}))
}
