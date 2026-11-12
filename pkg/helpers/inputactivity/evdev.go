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
	"strconv"
)

// Event types and axis geometry for evdev devices.
const (
	// axisDeadzoneFraction is how far an axis must travel, as a fraction of
	// its full range, before it counts as someone using the stick.
	axisDeadzoneFraction = 0.12

	// defaultAxisRange is the range assumed when the kernel will not say.
	defaultAxisMin = -32768
	defaultAxisMax = 32767

	evHeaderSize = 8

	fnvOffset = 14695981039346656037
	fnvPrime  = 1099511628211
)

// evSize is the size of one struct input_event: a timeval as wide as the
// kernel's long, then type, code and value.
var evSize = 2*(strconv.IntSize/8) + evHeaderSize

type inputEvent struct {
	value int32
	typ   uint16
	code  uint16
}

// parseEvents splits a read into events. A trailing partial event is dropped.
func parseEvents(p []byte) []inputEvent {
	events := make([]inputEvent, 0, len(p)/evSize)
	tv := evSize - evHeaderSize
	for off := 0; off+evSize <= len(p); off += evSize {
		events = append(events, inputEvent{
			typ:   binary.NativeEndian.Uint16(p[off+tv:]),
			code:  binary.NativeEndian.Uint16(p[off+tv+2:]),
			value: int32(binary.NativeEndian.Uint32(p[off+tv+4:])), //nolint:gosec // kernel value is signed
		})
	}
	return events
}

// axisRange is the reported span of an absolute axis.
type axisRange struct {
	min, max, value int32
}

// axisTracker decides whether absolute axis movement is a person. It keeps the
// last position that counted per axis, so a stick that creeps a little never
// adds up to activity, and a trigger or hat is judged against its own range.
type axisTracker struct {
	ranges  map[uint16]axisRange
	anchors map[uint16]int32
	lookup  func(code uint16) (axisRange, bool)
}

func newAxisTracker(lookup func(code uint16) (axisRange, bool)) *axisTracker {
	return &axisTracker{
		ranges:  make(map[uint16]axisRange),
		anchors: make(map[uint16]int32),
		lookup:  lookup,
	}
}

// seed records the starting position of an axis.
func (t *axisTracker) seed(code uint16, r axisRange) {
	t.ranges[code] = r
	t.anchors[code] = r.value
}

// moved reports whether the axis left its deadzone around the last counted
// position.
func (t *axisTracker) moved(code uint16, value int32) bool {
	anchor, seeded := t.anchors[code]
	if !seeded {
		r := axisRange{min: defaultAxisMin, max: defaultAxisMax, value: value}
		if t.lookup != nil {
			if got, ok := t.lookup(code); ok && got.max > got.min {
				r = got
			}
		}
		t.ranges[code] = r
		t.anchors[code] = value
		return false
	}
	r := t.ranges[code]
	span := float64(r.max) - float64(r.min)
	diff := float64(value) - float64(anchor)
	if diff < 0 {
		diff = -diff
	}
	if diff <= span*axisDeadzoneFraction {
		return false
	}
	t.anchors[code] = value
	return true
}

// eventsActive reports whether any event in a read is a person using a device
// of the given class, and a digest of the events that counted. axes may be nil
// for classes without absolute axes.
func eventsActive(class Class, events []inputEvent, axes *axisTracker) (active bool, sig uint64) {
	sig = fnvOffset
	count := func(ev inputEvent) {
		active = true
		//nolint:gosec // digest only
		fields := [...]uint64{uint64(ev.typ), uint64(ev.code), uint64(uint32(ev.value))}
		for _, v := range fields {
			sig = (sig ^ v) * fnvPrime
		}
	}
	for _, ev := range events {
		switch ev.typ {
		case evKey:
			// A release alone is not intent; press and autorepeat are.
			if ev.value != 0 && (class != Controller || ev.value == 1) {
				count(ev)
			}
		case evRel:
			if class == Mouse && ev.value != 0 {
				count(ev)
			}
		case evAbs:
			// Every axis event must reach the tracker so anchors stay current
			// even when an earlier event in the same read already counted.
			if class == Controller && axes != nil && axes.moved(ev.code, ev.value) {
				count(ev)
			}
		}
	}
	return active, sig
}
