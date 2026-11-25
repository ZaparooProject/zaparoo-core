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

package audio

import (
	"math"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sineRing(hz, amp float64, frames int) [][2]float64 {
	ring := make([][2]float64, frames)
	for i := range ring {
		v := amp * math.Sin(2*math.Pi*hz*float64(i)/targetSampleRate)
		ring[i] = [2]float64{v, v}
	}
	return ring
}

// expectedBand is the log band a frequency falls in.
func expectedBand(hz float64) int {
	return int(LevelBands * math.Log(hz/levelLowHz) / math.Log(levelHighHz/levelLowHz))
}

func loudestBand(l *Levels) int {
	best := 0
	for i, v := range l.Bands {
		if v > l.Bands[best] {
			best = i
		}
	}
	return best
}

func TestLevelAnalyzerPutsSineInItsBand(t *testing.T) {
	t.Parallel()
	for _, hz := range []float64{60, 440, 1000, 5000, 12000} {
		a := newLevelAnalyzer()
		var tap levelTap
		ring := sineRing(hz, 0.5, levelTapSize)
		tap.add(ring, 0, len(ring))
		l := a.measure(&tap)
		want := expectedBand(hz)
		assert.Equal(t, want, loudestBand(&l), "%v Hz", hz)
		// -6 dB on a -60..0 dB scale.
		assert.InDelta(t, 0.9, l.Bands[want], 0.03, "%v Hz", hz)
		assert.InDelta(t, 0.5, l.Peak, 0.01, "%v Hz", hz)
		for b, v := range l.Bands {
			if b < want-2 || b > want+2 {
				assert.Less(t, v, 0.5, "%v Hz band %d", hz, b)
			}
		}
	}
}

func TestLevelAnalyzerMeasuresEachChannel(t *testing.T) {
	t.Parallel()
	const hz = 1000.0
	want := expectedBand(hz)
	inverted := sineRing(hz, 0.5, levelTapSize)
	for i := range inverted {
		inverted[i][1] = -inverted[i][0]
	}
	leftOnly := sineRing(hz, 0.5, levelTapSize)
	for i := range leftOnly {
		leftOnly[i][1] = 0
	}

	a := newLevelAnalyzer()
	var tap levelTap
	tap.add(inverted, 0, len(inverted))
	l := a.measure(&tap)
	assert.InDelta(t, 0.5, l.Peak, 0.01, "out-of-phase channels do not cancel")
	assert.InDelta(t, 0.9, l.Bands[want], 0.03)

	tap.add(leftOnly, 0, len(leftOnly))
	l = a.measure(&tap)
	assert.InDelta(t, 0.5, l.Peak, 0.01)
	// One channel at -6 dB averages to -9 dB across two.
	assert.InDelta(t, 0.85, l.Bands[want], 0.03)
}

func TestLevelAnalyzerSilenceIsZero(t *testing.T) {
	t.Parallel()
	a := newLevelAnalyzer()
	var tap levelTap
	tap.add(make([][2]float64, levelTapSize), 0, levelTapSize)
	l := a.measure(&tap)
	assert.Zero(t, l.Peak)
	for _, v := range l.Bands {
		assert.Zero(t, v)
	}
}

// Not parallel: AllocsPerRun refuses to run in a parallel test.
func TestLevelTapAddDoesNotAllocate(t *testing.T) {
	var tap levelTap
	ring := sineRing(1000, 0.5, 4096)
	allocs := testing.AllocsPerRun(100, func() { tap.add(ring, 4000, 512) })
	assert.Zero(t, allocs)
}

// playingSource is a source with a ring full of a sine, ready to mix.
func playingSource(m *LongformPlaybackManager) *streamingSource {
	s := newTestSource()
	s.ring = sineRing(1000, 0.5, 8192)
	s.writePos.Store(uint64(len(s.ring)))
	s.levelsOn = &m.levelsOn
	return s
}

func TestLevelsEmitOnlyWithListenerAndWhilePlaying(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	m := NewLongformPlaybackManager(false)
	m.levelClock = clock
	s := playingSource(m)
	m.mu.Lock()
	m.primary = s
	m.mu.Unlock()
	buf := make([][2]float64, levelTapSize)

	// No listener: the audio thread records nothing and nothing is measured.
	s.mixAdd(buf, len(buf))
	assert.Zero(t, s.levelTap.written.Load())
	assert.Zero(t, m.levelRuns.Load())

	got := make(chan Levels, 4)
	m.SetLevelListener(func(slot string, l *Levels) {
		assert.Equal(t, mediaslot.Primary, slot)
		got <- *l
	})
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))

	s.mixAdd(buf, len(buf))
	clock.Advance(levelInterval)
	select {
	case l := <-got:
		assert.Equal(t, expectedBand(1000), loudestBand(&l))
		assert.Positive(t, l.Peak)
	case <-time.After(5 * time.Second):
		require.Fail(t, "no levels emitted")
	}

	// Nothing new played: no emission. Paused: no emission.
	clock.Advance(levelInterval)
	s.paused.Store(true)
	clock.Advance(levelInterval)
	m.SetLevelListener(nil)
	assert.Equal(t, uint64(1), m.levelRuns.Load())
	assert.Empty(t, got)

	// Stopping the listener stops recording.
	assert.False(t, m.levelsOn.Load())
	s.paused.Store(false)
	before := s.levelTap.written.Load()
	s.writePos.Store(s.readPos.Load() + uint64(len(buf)))
	s.mixAdd(buf, len(buf))
	assert.Equal(t, before, s.levelTap.written.Load())
}

func TestSetLevelListenerReplacesGoroutine(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	m := NewLongformPlaybackManager(false)
	m.levelClock = clock
	m.SetLevelListener(func(string, *Levels) {})
	m.SetLevelListener(func(string, *Levels) {})
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	m.SetLevelListener(nil)
	m.SetLevelListener(nil)
	assert.Nil(t, m.levelStop)
}
