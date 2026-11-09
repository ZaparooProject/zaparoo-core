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
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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
	dryReads  int
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

func (*fakeLive) Len() int       { return -1 }
func (*fakeLive) Position() int  { return 0 }
func (*fakeLive) Seek(int) error { return ErrLiveStream }
func (*fakeLive) Close() error   { return nil }
func (f *fakeLive) Buffered() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.frames == 0 {
		f.dryReads++
	}
	return f.frames
}
func (f *fakeLive) Suspend() { f.mu.Lock(); f.suspended++; f.mu.Unlock() }
func (f *fakeLive) Resume()  { f.mu.Lock(); f.resumed++; f.mu.Unlock() }

// dry reports how many times the player found nothing buffered.
func (f *fakeLive) dry() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dryReads
}

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
	require.ErrorIs(t, m.SkipTrack(mediaslot.Background, 1), ErrSingleTrack)
	assert.True(t, m.State(mediaslot.Background).Live)
	assert.False(t, m.State(mediaslot.Background).MultiTrack)

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

// A station's first title can arrive while its stream is still opening,
// before Play knows the source. It must still become the track's title.
func TestLongformPlaybackManager_PlayKeepsTitleFromOpening(t *testing.T) {
	oldDevice := globalDevice
	testDevice := newSharedDevice()
	testDevice.opening = true
	globalDevice = testDevice
	t.Cleanup(func() { globalDevice = oldDevice })

	tone, err := os.ReadFile(filepath.Join("nativedec", "testdata", "tone.mp3"))
	require.NoError(t, err)
	const metaint = 16
	// A metadata block is a length in 16-byte units, then the text padded
	// to it: this text takes two units.
	meta := "StreamTitle='Band - Opening';"
	block := append([]byte{2}, meta...)
	block = append(block, make([]byte, 2*16-len(meta))...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("icy-metaint", strconv.Itoa(metaint))
		var body bytes.Buffer
		for off := 0; off < len(tone); off += metaint {
			body.Write(tone[off:min(off+metaint, len(tone))])
			if off+metaint <= len(tone) {
				body.Write(block)
				block = []byte{0}
			}
		}
		_, _ = w.Write(body.Bytes())
	}))
	t.Cleanup(srv.Close)

	m := NewLongformPlaybackManager(false)
	require.NoError(t, m.Play(mediaslot.Background, srv.URL+"/stream", PlaybackOptions{Volume: 1}))
	t.Cleanup(func() { _ = m.Stop(mediaslot.Background) })

	got := m.State(mediaslot.Background).Meta
	require.NotNil(t, got)
	assert.Equal(t, "Opening", got.Title)
	assert.Equal(t, "Band", got.Artist)
}

// A station whose buffer runs dry while the normalizer is still measuring
// the start keeps holding that audio, so the gain is not settled on a
// fraction of a second.
func TestLiveSource_DryBufferKeepsNormalizerWarming(t *testing.T) {
	t.Parallel()

	live := &fakeLive{}
	live.add(normWarmupFrames / 4)
	s := newLiveSource(t, live, targetSampleRate)
	s.normalize = func() bool { return true }
	s.startPrefetch()
	t.Cleanup(s.stopAndDeregister)

	// Two dry reads mean the loop has handled the first one and come back.
	require.Eventually(t, func() bool { return live.dry() >= 2 }, 5*time.Second, time.Millisecond)
	assert.Zero(t, s.writePos.Load(), "audio still being measured is held")

	live.add(normWarmupFrames)
	require.Eventually(t, func() bool {
		return s.writePos.Load() >= normWarmupFrames
	}, 5*time.Second, time.Millisecond, "a full measurement is published")
}

// fakeTracks is a multi-track decoder: a silent timeline with tracks
// starting at fixed frames.
type fakeTracks struct {
	starts []int
	pos    int
	length int
}

func (f *fakeTracks) Stream(samples [][2]float64) (int, bool) {
	n := min(len(samples), f.length-f.pos)
	for i := range n {
		samples[i] = [2]float64{}
	}
	f.pos += n
	return n, n > 0
}
func (*fakeTracks) Err() error           { return nil }
func (f *fakeTracks) Len() int           { return f.length }
func (f *fakeTracks) Position() int      { return f.pos }
func (f *fakeTracks) Seek(p int) error   { f.pos = p; return nil }
func (*fakeTracks) Close() error         { return nil }
func (f *fakeTracks) TrackStarts() []int { return f.starts }

func TestMultiTrackSource_SkipTrack(t *testing.T) {
	t.Parallel()

	rate := targetSampleRate
	dec := &fakeTracks{starts: []int{0, 10 * rate, 20 * rate}, length: 30 * rate}
	s := sourceFromDecoder("/Audio/game.nsf", &openedDecoder{decoder: dec, tracks: dec, rate: beep.SampleRate(rate)},
		1.0, lowPowerResampleQuality)

	assert.True(t, s.state().MultiTrack)
	assert.False(t, s.state().Live)

	meta := s.trackMeta(s.played.Load())
	require.NotNil(t, meta)
	assert.Equal(t, 1, meta.TrackNumber)
	assert.Equal(t, 3, meta.TrackTotal)

	require.NoError(t, s.skipTrack(1))
	assert.Equal(t, int64(10*rate), s.played.Load())
	assert.Equal(t, 2, s.trackMeta(s.played.Load()).TrackNumber)

	require.NoError(t, s.skipTrack(1))
	assert.Equal(t, int64(20*rate), s.played.Load())
	require.Error(t, s.skipTrack(1), "there is no track after the last")

	// Five seconds into a track, going back restarts it.
	s.played.Store(int64(25 * rate))
	require.NoError(t, s.skipTrack(-1))
	assert.Equal(t, int64(20*rate), s.played.Load())

	// At its start, going back goes to the track before.
	require.NoError(t, s.skipTrack(-1))
	assert.Equal(t, int64(10*rate), s.played.Load())

	require.NoError(t, s.skipTrack(-1))
	require.NoError(t, s.skipTrack(-1))
	assert.Zero(t, s.played.Load(), "going back from the first track stays on it")
}

// A track boundary that falls between output frames, as one does when the
// source rate is not the output rate, is reached exactly and not one frame
// short of it.
func TestMultiTrackSource_SkipTrackLandsOnTheBoundary(t *testing.T) {
	t.Parallel()

	const rate = 44100
	dec := &fakeTracks{starts: []int{0, 1000, 158 * rate}, length: 200 * rate}
	s := sourceFromDecoder("/Audio/game.nsf", &openedDecoder{decoder: dec, tracks: dec, rate: beep.SampleRate(rate)},
		1.0, lowPowerResampleQuality)

	s.played.Store(960)
	require.NoError(t, s.skipTrack(1))
	assert.Equal(t, 2, s.trackMeta(s.played.Load()).TrackNumber)
	assert.Equal(t, int64(1000), s.seekSrcFrame.Load())

	require.NoError(t, s.skipTrack(1))
	assert.Equal(t, 3, s.trackMeta(s.played.Load()).TrackNumber)
	assert.Equal(t, int64(158*rate), s.seekSrcFrame.Load())
}

func TestSplitTrackPath(t *testing.T) {
	t.Parallel()

	file, track, ok := SplitTrackPath("/Audio/game.nsf/5")
	require.True(t, ok)
	assert.Equal(t, "/Audio/game.nsf", file)
	assert.Equal(t, 4, track)

	for _, path := range []string{
		"/Audio/game.nsf", "/Audio/game.nsf/0", "/Audio/game.nsf/05", "/Audio/game.nsf/x",
		"/Audio/game.spc/1", "/Audio/song.mp3/2", "/Audio/game.nsf/-1",
	} {
		_, _, ok := SplitTrackPath(path)
		assert.False(t, ok, path)
	}
}

// A file of one track, such as an NSF picked by its track number, has no
// other track to move to.
func TestMultiTrackSource_OneTrackIsNotMultiTrack(t *testing.T) {
	t.Parallel()

	rate := targetSampleRate
	dec := &fakeTracks{starts: []int{0}, length: 10 * rate}
	s := sourceFromDecoder("/Audio/game.nsf/5", &openedDecoder{decoder: dec, tracks: dec, rate: beep.SampleRate(rate)},
		1.0, lowPowerResampleQuality)
	assert.False(t, s.state().MultiTrack)
}
