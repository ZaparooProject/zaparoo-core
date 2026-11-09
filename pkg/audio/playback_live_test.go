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
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/gopxl/beep/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLive is a live decoder fed by the test. It never blocks: with nothing
// ready it returns (0, true), and a read of more than it holds is short.
type fakeLive struct {
	err       error
	frames    int
	ended     bool
	suspended int
	resumed   int
	mu        syncutil.Mutex
}

func (f *fakeLive) add(n int) {
	f.mu.Lock()
	f.frames += n
	f.mu.Unlock()
}

func (f *fakeLive) end() {
	f.mu.Lock()
	f.ended = true
	f.err = errors.New("stream ended")
	f.mu.Unlock()
}

func (f *fakeLive) Stream(samples [][2]float64) (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := min(len(samples), f.frames)
	for i := range n {
		samples[i] = [2]float64{0.25, 0.25}
	}
	f.frames -= n
	if n == 0 && f.ended {
		return 0, false
	}
	return n, true
}

func (f *fakeLive) Err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

func (*fakeLive) Len() int        { return -1 }
func (*fakeLive) Position() int   { return 0 }
func (*fakeLive) Seek(int) error  { return ErrLiveStream }
func (*fakeLive) Close() error    { return nil }
func (f *fakeLive) Buffered() int { f.mu.Lock(); defer f.mu.Unlock(); return f.frames }
func (f *fakeLive) Suspend()      { f.mu.Lock(); f.suspended++; f.mu.Unlock() }
func (f *fakeLive) Resume()       { f.mu.Lock(); f.resumed++; f.mu.Unlock() }
func (f *fakeLive) counts() (suspended, resumed int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.suspended, f.resumed
}

func newLiveSource(t *testing.T, live *fakeLive, rate int) *streamingSource {
	t.Helper()
	s := sourceFromDecoder("http://radio.example/stream", &openedDecoder{
		decoder: live, live: live, rate: beep.SampleRate(rate),
	}, 1.0, lowPowerResampleQuality)
	s.tickInterval = 5 * time.Millisecond
	return s
}

func TestLiveSource_ShortReadsAreNotTheEnd(t *testing.T) {
	t.Parallel()

	for _, rate := range []int{targetSampleRate, 44100} {
		live := &fakeLive{}
		s := newLiveSource(t, live, rate)
		assert.Equal(t, int64(-1), s.totalFrames, "a live stream has no length")
		s.startPrefetch()
		t.Cleanup(s.stopAndDeregister)

		// Audio arrives a little at a time, as it does from a station. None
		// of the gaps between arrivals may read as the stream ending.
		for range 20 {
			live.add(rate / 20)
			time.Sleep(2 * time.Millisecond)
		}
		require.Eventually(t, func() bool {
			return s.bufferedFrames() >= targetSampleRate/2
		}, 5*time.Second, time.Millisecond, "rate %d", rate)
		assert.False(t, s.eof.Load(), "rate %d: gaps in a live stream are not its end", rate)

		live.end()
		require.Eventually(t, s.eof.Load, 5*time.Second, time.Millisecond, "an ended stream drains")
	}
}

func TestLiveSource_ManagerControls(t *testing.T) {
	t.Parallel()

	live := &fakeLive{}
	live.add(targetSampleRate)
	s := newLiveSource(t, live, targetSampleRate)
	s.startPrefetch()
	t.Cleanup(s.stopAndDeregister)

	m := NewLongformPlaybackManager(true)
	m.mu.Lock()
	m.setSourceLocked(mediaslot.Background, s)
	m.mu.Unlock()

	require.ErrorIs(t, m.Seek(mediaslot.Background, 10*time.Second), ErrLiveStream)
	assert.True(t, m.State(mediaslot.Background).Live)

	require.NoError(t, m.Pause(mediaslot.Background))
	suspended, resumed := live.counts()
	assert.Equal(t, 1, suspended, "pausing a stream drops its connection")
	assert.Zero(t, resumed)

	require.NoError(t, m.Resume(mediaslot.Background))
	suspended, resumed = live.counts()
	assert.Equal(t, 1, suspended)
	assert.Equal(t, 1, resumed, "resuming reconnects at the live point")

	require.NoError(t, m.TogglePause(mediaslot.Background))
	require.NoError(t, m.TogglePause(mediaslot.Background))
	suspended, resumed = live.counts()
	assert.Equal(t, 2, suspended)
	assert.Equal(t, 2, resumed)
}

func TestLiveSource_StartAtIgnored(t *testing.T) {
	t.Parallel()

	s := newLiveSource(t, &fakeLive{}, targetSampleRate)
	s.startAt(time.Minute, 0)
	assert.False(t, s.seekPending.Load(), "a live stream never starts part way in")
}
