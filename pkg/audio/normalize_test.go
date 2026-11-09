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
	"sync/atomic"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sine fills frames at targetSampleRate with a 1 kHz tone of amplitude amp
// in both channels, continuing from frame offset.
func sine(frames [][2]float64, offset int, amp float64) {
	for i := range frames {
		v := amp * math.Sin(2*math.Pi*1000*float64(offset+i)/float64(targetSampleRate))
		frames[i] = [2]float64{v, v}
	}
}

func peakOf(frames [][2]float64) float64 {
	peak := 0.0
	for _, f := range frames {
		peak = max(peak, math.Abs(f[0]), math.Abs(f[1]))
	}
	return peak
}

// A 1 kHz tone in both channels measures at its own level in LUFS, so a
// tone at 0.1 (-20 LUFS) is raised by 4 dB to the target.
func TestNormalizer_SettlesOnMeasuredLevel(t *testing.T) {
	t.Parallel()

	n := newNormalizer()
	frames := make([][2]float64, normWarmupFrames)
	sine(frames, 0, 0.1)
	n.observe(frames)
	n.settle()
	assert.InDelta(t, normTargetLUFS+20, n.gainDB, 0.3)
}

func TestNormalizer_GainIsBounded(t *testing.T) {
	t.Parallel()

	loud := newNormalizer()
	frames := make([][2]float64, normWarmupFrames)
	sine(frames, 0, 0.9)
	loud.observe(frames)
	loud.settle()
	assert.InDelta(t, -normMaxGainDB, loud.gainDB, 1e-9)

	quiet := newNormalizer()
	sine(frames, 0, 0.005)
	quiet.observe(frames)
	quiet.settle()
	assert.InDelta(t, normMaxGainDB, quiet.gainDB, 1e-9)
}

// Silence is gated out: a track that starts silent plays at unity until
// there is something to measure.
func TestNormalizer_SilenceLeavesGain(t *testing.T) {
	t.Parallel()

	n := newNormalizer()
	n.observe(make([][2]float64, normWarmupFrames))
	n.settle()
	assert.Zero(t, n.gainDB)
	assert.Zero(t, n.gatedBlocks)
}

// A quiet passage after loud music does not drag the estimate down.
func TestNormalizer_QuietPassageGated(t *testing.T) {
	t.Parallel()

	n := newNormalizer()
	frames := make([][2]float64, normWarmupFrames)
	sine(frames, 0, 0.3)
	n.observe(frames)
	n.settle()
	before := n.gainDB
	sine(frames, 0, 0.01) // 30 dB down
	n.observe(frames)
	assert.InDelta(t, before, n.targetDB(), 1e-9)
}

func TestNormalizer_ReplayGainReplacesMeasurement(t *testing.T) {
	t.Parallel()

	n := newNormalizer()
	frames := make([][2]float64, normWarmupFrames)
	sine(frames, 0, 0.1)
	n.observe(frames)
	n.setReplayGain(-6)
	n.settle()
	assert.InDelta(t, -6+normTargetLUFS-replayGainRefLUFS, n.gainDB, 1e-9)
}

func TestNormalizer_LimiterHoldsCeiling(t *testing.T) {
	t.Parallel()

	n := newNormalizer()
	n.setReplayGain(10) // +12 dB, clamped: a 0.9 tone would peak near 3.6
	n.settle()
	frames := make([][2]float64, targetSampleRate)
	sine(frames, 0, 0.9)
	n.apply(frames, true)
	assert.LessOrEqual(t, peakOf(frames), limiterCeiling+1e-12)
	assert.Greater(t, peakOf(frames), 0.8)
}

// Once settled, the gain follows a change in level at normSlewDBPerSec.
func TestNormalizer_SlewsSlowlyOnceSettled(t *testing.T) {
	t.Parallel()

	n := newNormalizer()
	block := make([][2]float64, normBlockFrames)
	for i := range normSettleBlocks + 1 {
		sine(block, i*normBlockFrames, 0.1)
		n.observe(block)
	}
	n.settle()
	start := n.gainDB
	n.estimate /= 100 // the music turns 20 dB quieter
	sine(block, 0, 0.02)
	for range 10 {
		n.apply(block, true)
	}
	assert.InDelta(t, normSlewDBPerSec, n.gainDB-start, 1e-6, "one second moves one dB")
}

// Turning normalization off brings the gain back to unity, then leaves the
// audio alone.
func TestNormalizer_DisableReturnsToUnity(t *testing.T) {
	t.Parallel()

	n := newNormalizer()
	n.setReplayGain(-3) // -1 dB
	n.settle()
	require.False(t, n.bypassed())
	frames := make([][2]float64, normBlockFrames)
	for range 10 {
		sine(frames, 0, 0.1)
		n.apply(frames, false)
	}
	assert.True(t, n.bypassed())
}

// toneDecoder is a file decoder of a 1 kHz tone at a fixed amplitude.
type toneDecoder struct {
	amp    float64
	pos    int
	length int
}

func (d *toneDecoder) Stream(samples [][2]float64) (int, bool) {
	n := min(len(samples), d.length-d.pos)
	sine(samples[:n], d.pos, d.amp)
	d.pos += n
	return n, n > 0
}
func (*toneDecoder) Err() error         { return nil }
func (d *toneDecoder) Len() int         { return d.length }
func (d *toneDecoder) Position() int    { return d.pos }
func (d *toneDecoder) Seek(p int) error { d.pos = p; return nil }
func (*toneDecoder) Close() error       { return nil }

// A levelled source publishes nothing until it has measured the start of
// the track, and its first frames already play at the levelled gain.
func TestStreamingSource_NormalizesFromFirstFrame(t *testing.T) {
	t.Parallel()

	dec := &toneDecoder{amp: 0.1, length: 3 * targetSampleRate}
	s := sourceFromDecoder("/music/quiet.mp3", &openedDecoder{decoder: dec, rate: beep.SampleRate(targetSampleRate)},
		1.0, lowPowerResampleQuality)
	s.normalize = func() bool { return true }
	s.startPrefetch()
	t.Cleanup(s.stopAndDeregister)

	require.Eventually(t, func() bool { return s.writePos.Load() > 0 }, 5*time.Second, time.Millisecond)
	assert.GreaterOrEqual(t, s.writePos.Load(), uint64(normWarmupFrames))
	want := 0.1 * dbToGain(normTargetLUFS+20)
	assert.InDelta(t, want, peakOf(s.ring[:normBlockFrames]), 0.01)
}

// With normalization off the source plays exactly what was decoded.
func TestStreamingSource_NormalizeOff(t *testing.T) {
	t.Parallel()

	dec := &toneDecoder{amp: 0.1, length: 3 * targetSampleRate}
	s := sourceFromDecoder("/music/quiet.mp3", &openedDecoder{decoder: dec, rate: beep.SampleRate(targetSampleRate)},
		1.0, lowPowerResampleQuality)
	var enabled atomic.Bool
	s.normalize = enabled.Load
	s.startPrefetch()
	t.Cleanup(s.stopAndDeregister)

	require.Eventually(t, func() bool { return s.writePos.Load() > 0 }, 5*time.Second, time.Millisecond)
	assert.InDelta(t, 0.1, peakOf(s.ring[:normBlockFrames]), 1e-3)
}

// A track shorter than the measuring window still plays, levelled.
func TestStreamingSource_NormalizesShortTrack(t *testing.T) {
	t.Parallel()

	dec := &toneDecoder{amp: 0.1, length: targetSampleRate / 4}
	s := sourceFromDecoder("/music/blip.mp3", &openedDecoder{decoder: dec, rate: beep.SampleRate(targetSampleRate)},
		1.0, lowPowerResampleQuality)
	s.normalize = func() bool { return true }
	s.startPrefetch()
	t.Cleanup(s.stopAndDeregister)

	require.Eventually(t, s.eof.Load, 5*time.Second, time.Millisecond)
	assert.Equal(t, uint64(targetSampleRate/4), s.writePos.Load())
	assert.Greater(t, peakOf(s.ring[:normBlockFrames]), 0.12)
}
