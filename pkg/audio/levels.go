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
	"math/cmplx"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/jonboulle/clockwork"
)

const (
	// LevelBands is how many frequency bands a level reading has.
	LevelBands = 16
	// levelInterval is how often levels are measured, about 30 times a
	// second.
	levelInterval = 33 * time.Millisecond
	// levelTapSize is how many recent samples a source keeps for level
	// measurement: one FFT window, about 43 ms at 48 kHz.
	levelTapSize = 2048
	// levelPeakWindow is how many of the newest samples the peak is taken
	// over, one interval at 48 kHz.
	levelPeakWindow = targetSampleRate * 33 / 1000
	levelLowHz      = 40.0
	levelHighHz     = 16000.0
	// levelFloorDB is the band level that reads as 0.
	levelFloorDB = -60.0
	// levelAttack and levelRelease are how far a smoothed level moves
	// toward a new reading each interval, rising and falling.
	levelAttack  = 0.7
	levelRelease = 0.2
)

// Levels is one measurement of what a slot is playing, for a visualizer.
// Every value is in 0..1.
type Levels struct {
	Bands [LevelBands]float64
	Peak  float64
}

// LevelSource is a playback manager that can report levels.
type LevelSource interface {
	// SetLevelListener starts measuring levels about 30 times a second and
	// hands them to fn for each slot that is playing. fn must not keep
	// levels after it returns. nil stops measuring.
	SetLevelListener(fn func(slot string, levels *Levels))
}

// levelTap holds the newest stereo frames a source played, before volume.
// The audio thread writes them; the analyzer reads them. Each frame is one
// atomic, left in the high half and right in the low, so a read racing a
// write sees either frame, never a torn one.
type levelTap struct {
	samples [levelTapSize]atomic.Uint64
	written atomic.Uint64
}

// add records n frames of ring starting at readPos. It is called from
// mixAdd on the audio thread and does not allocate.
func (t *levelTap) add(ring [][2]float64, readPos uint64, n int) {
	if n <= 0 {
		return
	}
	w := t.written.Load()
	size := uint64(len(ring))
	for i := range n {
		f := ring[(readPos+uint64(i))%size]
		frame := uint64(math.Float32bits(float32(f[0])))<<32 | uint64(math.Float32bits(float32(f[1])))
		t.samples[(w+uint64(i))%levelTapSize].Store(frame)
	}
	t.written.Store(w + uint64(n))
}

// snapshot copies the newest levelTapSize frames into dst, oldest first, as
// left + right·i.
func (t *levelTap) snapshot(dst []complex128) uint64 {
	w := t.written.Load()
	for i := range dst {
		idx := (w + uint64(i)) % levelTapSize
		frame := t.samples[idx].Load()
		l := float64(math.Float32frombits(uint32(frame >> 32)))
		r := float64(math.Float32frombits(uint32(frame))) //nolint:gosec // low half by design
		dst[i] = complex(l, r)
	}
	return w
}

// levelAnalyzer turns tapped samples into band levels. Its buffers are
// allocated once and reused for every measurement.
type levelAnalyzer struct {
	state   map[string]*slotLevels
	window  []float64
	samples []complex128
	spec    []complex128
	twiddle []complex128
	rev     []int
	// bandLo and bandHi are each band's FFT bins, hi exclusive.
	bandLo [LevelBands]int
	bandHi [LevelBands]int
}

// slotLevels is a slot's smoothed levels and the source they were for.
type slotLevels struct {
	src     *streamingSource
	levels  Levels
	written uint64
}

func newLevelAnalyzer() *levelAnalyzer {
	const n = levelTapSize
	a := &levelAnalyzer{
		window:  make([]float64, n),
		samples: make([]complex128, n),
		spec:    make([]complex128, n),
		twiddle: make([]complex128, n/2),
		rev:     make([]int, n),
		state:   make(map[string]*slotLevels, 2),
	}
	for i := range n {
		a.window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	for i := range n / 2 {
		a.twiddle[i] = cmplx.Exp(complex(0, -2*math.Pi*float64(i)/float64(n)))
	}
	bits := 0
	for 1<<bits < n {
		bits++
	}
	for i := range n {
		r := 0
		for b := range bits {
			if i&(1<<b) != 0 {
				r |= 1 << (bits - 1 - b)
			}
		}
		a.rev[i] = r
	}
	binHz := float64(targetSampleRate) / n
	ratio := levelHighHz / levelLowHz
	for b := range LevelBands {
		lo := levelLowHz * math.Pow(ratio, float64(b)/LevelBands)
		hi := levelLowHz * math.Pow(ratio, float64(b+1)/LevelBands)
		loBin := int(math.Ceil(lo / binHz))
		hiBin := int(math.Ceil(hi / binHz))
		if hiBin <= loBin {
			// A band narrower than a bin reads the bin nearest its centre.
			loBin = int(math.Round(math.Sqrt(lo*hi) / binHz))
			hiBin = loBin + 1
		}
		a.bandLo[b], a.bandHi[b] = loBin, min(hiBin, n/2)
	}
	return a
}

// fft transforms a.spec in place.
func (a *levelAnalyzer) fft() {
	n := len(a.spec)
	for i, r := range a.rev {
		if i < r {
			a.spec[i], a.spec[r] = a.spec[r], a.spec[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		half, step := size/2, n/size
		for start := 0; start < n; start += size {
			for k := range half {
				t := a.twiddle[k*step] * a.spec[start+k+half]
				u := a.spec[start+k]
				a.spec[start+k] = u + t
				a.spec[start+k+half] = u - t
			}
		}
	}
}

// measure reads a source's newest frames into unsmoothed levels. Each
// channel is measured on its own, so a signal that is out of phase between
// them does not cancel as it would in a mono mix.
func (a *levelAnalyzer) measure(tap *levelTap) Levels {
	var out Levels
	tap.snapshot(a.samples)
	n := len(a.samples)
	for i, v := range a.samples {
		if i >= n-levelPeakWindow {
			out.Peak = max(out.Peak, math.Abs(real(v)), math.Abs(imag(v)))
		}
		a.spec[i] = v * complex(a.window[i], 0)
	}
	out.Peak = min(out.Peak, 1)
	// Both channels go through one transform, left as the real part and
	// right as the imaginary. Bins k and n-k then hold their combined
	// power: |L[k]|² + |R[k]|² = (|Z[k]|² + |Z[n-k]|²) / 2.
	a.fft()
	// A full-scale sine under a Hann window peaks at n/4.
	scale := 4.0 / float64(n)
	for b := range LevelBands {
		peak := 0.0
		for k := a.bandLo[b]; k < a.bandHi[b]; k++ {
			z, mirror := a.spec[k], a.spec[n-k]
			power := real(z)*real(z) + imag(z)*imag(z) + real(mirror)*real(mirror) + imag(mirror)*imag(mirror)
			// The mean of the two channels' powers, as an amplitude.
			peak = max(peak, math.Sqrt(power/4))
		}
		out.Bands[b] = dbLevel(peak * scale)
	}
	return out
}

// dbLevel maps an amplitude to 0..1 across levelFloorDB..0 dB.
func dbLevel(amp float64) float64 {
	if amp <= 0 {
		return 0
	}
	db := 20 * math.Log10(amp)
	return math.Max(0, math.Min(1, (db-levelFloorDB)/-levelFloorDB))
}

func smooth(old, v float64) float64 {
	if v > old {
		return old + (v-old)*levelAttack
	}
	return old + (v-old)*levelRelease
}

// update measures src for slot and returns its smoothed levels. ok is false
// when the source has played nothing since the last measurement.
func (a *levelAnalyzer) update(slot string, src *streamingSource) (Levels, bool) {
	st := a.state[slot]
	if st == nil || st.src != src {
		st = &slotLevels{src: src}
		a.state[slot] = st
	}
	written := src.levelTap.written.Load()
	if written == st.written {
		return st.levels, false
	}
	st.written = written
	raw := a.measure(&src.levelTap)
	st.levels.Peak = smooth(st.levels.Peak, raw.Peak)
	for i := range raw.Bands {
		st.levels.Bands[i] = smooth(st.levels.Bands[i], raw.Bands[i])
	}
	return st.levels, true
}

// SetLevelListener implements LevelSource.
func (m *LongformPlaybackManager) SetLevelListener(fn func(slot string, levels *Levels)) {
	m.levelMu.Lock()
	defer m.levelMu.Unlock()
	if m.levelStop != nil {
		close(m.levelStop)
		<-m.levelDone
		m.levelStop, m.levelDone = nil, nil
	}
	m.levelsOn.Store(fn != nil)
	if fn == nil {
		return
	}
	clock := m.levelClock
	if clock == nil {
		clock = clockwork.NewRealClock()
	}
	stop, done := make(chan struct{}), make(chan struct{})
	m.levelStop, m.levelDone = stop, done
	go m.runLevels(clock, fn, stop, done)
}

func (m *LongformPlaybackManager) runLevels(
	clock clockwork.Clock,
	fn func(string, *Levels),
	stop, done chan struct{},
) {
	defer close(done)
	ticker := clock.NewTicker(levelInterval)
	defer ticker.Stop()
	a := newLevelAnalyzer()
	slots := [2]string{mediaslot.Primary, mediaslot.Background}
	for {
		select {
		case <-stop:
			return
		case <-ticker.Chan():
		}
		for _, slot := range slots {
			m.mu.Lock()
			src := m.getSourceLocked(slot)
			m.mu.Unlock()
			if src == nil || !src.isActive() {
				delete(a.state, slot)
				continue
			}
			if levels, ok := a.update(slot, src); ok {
				m.levelRuns.Add(1)
				fn(slot, &levels)
			}
		}
	}
}
