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
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSource() *streamingSource {
	return &streamingSource{
		ring:        make([][2]float64, 100),
		volume:      1.0,
		totalFrames: int64(targetSampleRate), // 1 second
		wakeCh:      make(chan struct{}, 1),
	}
}

func TestBoundedFrameCount(t *testing.T) {
	t.Parallel()

	assert.Zero(t, boundedFrameCount(10, 0))
	assert.Zero(t, boundedFrameCount(0, 10))
	assert.Equal(t, 5, boundedFrameCount(5, 10))
	assert.Equal(t, 10, boundedFrameCount(10, 10))
	assert.Equal(t, 10, boundedFrameCount(^uint64(0), 10))
}

func TestSampleDuration(t *testing.T) {
	t.Parallel()
	assert.Equal(t, time.Duration(0), sampleDuration(0))
	assert.Equal(t, time.Duration(0), sampleDuration(-1))
	assert.Equal(t, time.Second, sampleDuration(targetSampleRate))
	assert.Equal(t, 500*time.Millisecond, sampleDuration(targetSampleRate/2))
}

func TestStreamingSource_State_PlayingLogic(t *testing.T) {
	t.Parallel()

	s := newTestSource()
	s.played.Store(targetSampleRate / 2) // 0.5 s in

	// Default: not paused, not stopped, not eof → Playing
	ps := s.state()
	assert.True(t, ps.Playing)
	assert.False(t, ps.Paused)
	assert.Equal(t, 500*time.Millisecond, ps.Position)
	assert.Equal(t, time.Second, ps.Duration)

	// Paused → Playing=false
	s.paused.Store(true)
	assert.False(t, s.state().Playing)
	assert.True(t, s.state().Paused)
	s.paused.Store(false)

	// Stopped → Playing=false
	s.stopped.Store(true)
	assert.False(t, s.state().Playing)
	s.stopped.Store(false)

	// EOF with ring drained → Playing=false
	s.eof.Store(true)
	assert.False(t, s.state().Playing)

	// EOF but ring still has frames → Playing=true (tail draining)
	s.writePos.Store(10)
	assert.True(t, s.state().Playing)
}

func TestStreamingSource_IsActive(t *testing.T) {
	t.Parallel()
	s := newTestSource()

	assert.True(t, s.isActive())
	s.paused.Store(true)
	assert.False(t, s.isActive())
	s.paused.Store(false)
	s.stopped.Store(true)
	assert.False(t, s.isActive())
}

func TestStreamingSource_OnDrained(t *testing.T) {
	t.Parallel()
	s := newTestSource()

	// No callback: no panic.
	s.onDrained()

	var gotNatural *bool
	s.onDrain = func(natural bool) { gotNatural = &natural }

	// stopped=false → natural drain
	s.stopped.Store(false)
	s.onDrained()
	require.NotNil(t, gotNatural)
	assert.True(t, *gotNatural)

	// stopped=true → explicit stop
	gotNatural = nil
	s.stopped.Store(true)
	s.onDrained()
	require.NotNil(t, gotNatural)
	assert.False(t, *gotNatural)
}

func TestStreamingSource_SetPaused(t *testing.T) {
	t.Parallel()
	s := newTestSource()

	s.setPaused(true)
	assert.True(t, s.paused.Load())

	// Resume writes to wakeCh.
	s.setPaused(false)
	assert.False(t, s.paused.Load())

	select {
	case <-s.wakeCh:
	default:
		t.Fatal("expected wake signal after resume")
	}
}

func TestStreamingSource_TogglePause(t *testing.T) {
	t.Parallel()
	s := newTestSource()

	nowPaused := s.togglePause()
	assert.True(t, nowPaused)
	assert.True(t, s.paused.Load())

	nowPaused = s.togglePause()
	assert.False(t, nowPaused)
	assert.False(t, s.paused.Load())

	// Resume writes to wakeCh.
	select {
	case <-s.wakeCh:
	default:
		t.Fatal("expected wake signal after toggle-to-unpaused")
	}
}

func TestStreamingSource_Seek(t *testing.T) {
	t.Parallel()
	s := newTestSource()
	// Pre-fill the ring to verify it is hidden from the callback during seek.
	s.writePos.Store(50)
	s.played.Store(targetSampleRate) // 1 s

	s.seek(0) // seek to current position (offset=0)

	assert.True(t, s.seekPending.Load(), "seekPending must be set")
	assert.Equal(t, 0, s.bufferedFrames(), "pending seek must hide stale ring frames")

	select {
	case <-s.wakeCh:
	default:
		t.Fatal("expected wake signal after seek")
	}
}

func TestStreamingSource_SeekClampsToTrackBounds(t *testing.T) {
	t.Parallel()

	s := newTestSource()
	s.sourceRate = targetSampleRate
	s.played.Store(targetSampleRate / 2)
	s.seek(-time.Second)
	assert.Equal(t, int64(0), s.played.Load())
	assert.Equal(t, int64(0), s.seekSrcFrame.Load())
	<-s.wakeCh

	s.seekPending.Store(false)
	s.played.Store(targetSampleRate)
	s.seek(time.Second)
	assert.Equal(t, int64(targetSampleRate), s.played.Load())
	assert.Equal(t, int64(targetSampleRate), s.seekSrcFrame.Load())
}

func TestStreamingSource_MixAdd(t *testing.T) {
	t.Parallel()
	s := newTestSource()

	// Fill ring with known samples.
	const nFrames = 10
	for i := range nFrames {
		s.ring[i] = [2]float64{float64(i+1) * 0.1, float64(i+1) * 0.1}
	}
	s.writePos.Store(nFrames)

	buf := make([][2]float64, 20)
	n, drained := s.mixAdd(buf, nFrames)

	assert.Equal(t, nFrames, n)
	assert.False(t, drained, "not drained — eof is false")
	// Volume=1 so buf values should equal the ring values.
	for i := range nFrames {
		assert.InDelta(t, float64(i+1)*0.1, buf[i][0], 1e-9)
	}

	// Now drained: eof=true, ring empty after mix.
	s.eof.Store(true)
	buf2 := make([][2]float64, 5)
	n2, drained2 := s.mixAdd(buf2, 5)
	assert.Equal(t, 0, n2)
	assert.True(t, drained2)

	// Stopped: drains immediately.
	s2 := newTestSource()
	s2.stopped.Store(true)
	_, stopped := s2.mixAdd(buf, 5)
	assert.True(t, stopped)
}

func TestStreamingSource_MixAddDoesNotWaitForLifecycleLock(t *testing.T) {
	t.Parallel()

	s := newTestSource()
	s.ring[0] = [2]float64{0.5, 0.5}
	s.writePos.Store(1)
	buf := make([][2]float64, 1)

	s.mu.Lock()
	defer s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		s.mixAdd(buf, 1)
		close(done)
	}()

	select {
	case <-done:
		assert.InDelta(t, 0.5, buf[0][0], 1e-9)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("realtime source mix blocked on lifecycle lock")
	}
}

func TestStreamingSource_UnderrunEpisodes(t *testing.T) {
	t.Parallel()

	s := newTestSource()
	buf := make([][2]float64, 4)

	written, drained := s.mixAdd(buf, len(buf))
	assert.Zero(t, written)
	assert.False(t, drained)
	assert.Equal(t, uint64(1), s.underruns.Load())

	// Repeated empty callbacks belong to the same underrun episode.
	s.mixAdd(buf, len(buf))
	assert.Equal(t, uint64(1), s.underruns.Load())

	// Consuming newly published audio ends the episode; another empty callback
	// starts a new one.
	s.ring[0] = [2]float64{0.25, 0.25}
	s.writePos.Store(1)
	s.mixAdd(buf, 1)
	s.mixAdd(buf, len(buf))
	assert.Equal(t, uint64(2), s.underruns.Load())
}

func TestNewStreamingSource_UnsupportedExtension(t *testing.T) {
	t.Parallel()
	// The file must exist — the extension check happens after os.Open succeeds.
	path := filepath.Join(t.TempDir(), "audio.xyz")
	require.NoError(t, os.WriteFile(path, []byte("fake"), 0o600))
	_, err := newStreamingSource(path, 1.0, resampleQuality)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported audio format")
}

func TestNewStreamingSource_FileNotFound(t *testing.T) {
	t.Parallel()
	_, err := newStreamingSource(filepath.Join(t.TempDir(), "missing.mp3"), 1.0, resampleQuality)
	require.Error(t, err)
}

func TestNewStreamingSource_WAVInitializesStreamingFields(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "audio.wav")
	require.NoError(t, os.WriteFile(path, validWAVHeader(), 0o600))

	s, err := newStreamingSource(path, 0.75, resampleQuality)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, s.decoder.Close())
	}()

	assert.Equal(t, path, s.path)
	assert.InDelta(t, 0.75, s.volume, 1e-9)
	assert.Equal(t, 44100, s.sourceRate)
	assert.Len(t, s.ring, ringBufferFrames)
	assert.Len(t, s.chunk, decodeChunkFrames)
	assert.NotNil(t, s.wakeCh)
	assert.NotNil(t, s.resampler)
}

// wavWithSamples builds a minimal mono 16-bit 44.1 kHz WAV containing n silent samples.
func wavWithSamples(n uint32) []byte {
	dataSize := n * 2
	header := validWAVHeader()
	// Patch RIFF size and data chunk size for the sample payload.
	binary.LittleEndian.PutUint32(header[4:8], 36+dataSize)
	binary.LittleEndian.PutUint32(header[40:44], dataSize)
	return append(header, make([]byte, dataSize)...)
}

func TestStreamingSource_SeekAfterEOFRefillsRing(t *testing.T) {
	t.Parallel()

	// Both qualities: the seek path rebuilds the resampler from the source's
	// stored quality, which must work for low-power platforms too.
	for _, tc := range []struct {
		name    string
		quality int
	}{
		{name: "default quality", quality: resampleQuality},
		{name: "low power quality", quality: lowPowerResampleQuality},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "audio.wav")
			require.NoError(t, os.WriteFile(path, wavWithSamples(4410), 0o600)) // 0.1 s of audio

			s, err := newStreamingSource(path, 1.0, tc.quality)
			require.NoError(t, err)
			s.startPrefetch()
			t.Cleanup(s.stopAndDeregister)

			// Wait for the prefetch goroutine to fully decode the file.
			require.Eventually(t, func() bool {
				return s.eof.Load()
			}, 5*time.Second, 5*time.Millisecond, "prefetch should reach EOF")

			// Simulate the tail playing out, then seek back to the start. Before the
			// parked-prefetch fix the goroutine had already exited and the flushed ring
			// stayed empty forever, wedging the slot in silence.
			buf := make([][2]float64, 64)
			s.mixAdd(buf, len(buf))
			s.seek(-time.Second)

			require.Eventually(t, func() bool {
				return s.bufferedFrames() > 0
			}, 5*time.Second, 5*time.Millisecond, "seek after EOF should refill the ring")

			assert.Equal(t, tc.quality, s.quality, "seek must preserve the source's resample quality")
		})
	}
}

// TestStreamingSource_PrefetchFillsRingWithoutTickerPacing verifies the prefetch
// goroutine fills the entire ring in a burst rather than one chunk per 100 ms
// tick. Per-tick pacing capped decode at 1x realtime, so the ring never built a
// cushion and CPU contention produced period-sized dropouts (crackle) on slow
// ARM devices. Filling the 4 s ring takes 40 chunks: burst-filling completes in
// well under 2 s, while per-tick pacing needs at least 4 s.
func TestStreamingSource_PrefetchFillsRingWithoutTickerPacing(t *testing.T) {
	t.Parallel()

	// 6 s of source audio at 44.1 kHz resamples to more than the 4 s ring holds.
	path := filepath.Join(t.TempDir(), "audio.wav")
	require.NoError(t, os.WriteFile(path, wavWithSamples(6*44100), 0o600))

	s, err := newStreamingSource(path, 1.0, resampleQuality)
	require.NoError(t, err)
	s.startPrefetch()
	t.Cleanup(s.stopAndDeregister)

	require.Eventually(t, func() bool {
		return s.bufferedFrames() == len(s.ring)
	}, 2*time.Second, 5*time.Millisecond, "prefetch should burst-fill the full ring")
}

func TestStreamingSource_ConcurrentPrefetchAndMix(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "audio.wav")
	require.NoError(t, os.WriteFile(path, wavWithSamples(10*44100), 0o600))

	s, err := newStreamingSource(path, 1.0, lowPowerResampleQuality)
	require.NoError(t, err)
	s.startPrefetch()
	t.Cleanup(s.stopAndDeregister)
	require.Eventually(t, func() bool {
		return s.bufferedFrames() == len(s.ring)
	}, 2*time.Second, 5*time.Millisecond)

	buf := make([][2]float64, 2048)
	deadline := time.Now().Add(5 * time.Second)
	total := 0
	for time.Now().Before(deadline) {
		for i := range buf {
			buf[i] = [2]float64{}
		}
		written, drained := s.mixAdd(buf, len(buf))
		total += written
		if drained {
			break
		}
		if written == 0 {
			time.Sleep(time.Millisecond)
		}
	}

	assert.Greater(t, total, ringBufferFrames)
	assert.Equal(t, int64(total), s.played.Load())
	assert.True(t, s.eof.Load())
	assert.Zero(t, s.bufferedFrames())
}

func TestNewLongformPlaybackManager(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager(false)
	require.NotNil(t, m)
	assert.NotNil(t, m.drainCallbacks)
	assert.Nil(t, m.primary)
	assert.Nil(t, m.background)
}

func TestNewLongformPlaybackManager_ResampleQuality(t *testing.T) {
	t.Parallel()
	assert.Equal(t, resampleQuality, NewLongformPlaybackManager(false).resampleQuality)
	assert.Equal(t, lowPowerResampleQuality, NewLongformPlaybackManager(true).resampleQuality)
}

func TestLongformPlaybackManager_PlayReplacesSourceAndUsesDrainCallback(t *testing.T) {
	oldDevice := globalDevice
	testDevice := newSharedDevice()
	testDevice.opening = true
	globalDevice = testDevice
	t.Cleanup(func() { globalDevice = oldDevice })

	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.wav")
	secondPath := filepath.Join(dir, "second.wav")
	require.NoError(t, os.WriteFile(firstPath, validWAVHeader(), 0o600))
	require.NoError(t, os.WriteFile(secondPath, validWAVHeader(), 0o600))

	m := NewLongformPlaybackManager(false)
	drainCalls := 0
	m.SetDrainCallback(mediaslot.Primary, func(_ bool) { drainCalls++ })

	require.NoError(t, m.Play("", firstPath, PlaybackOptions{}))
	first := m.primary
	require.NotNil(t, first)
	assert.InDelta(t, 1.0, first.volume, 1e-9, "zero volume option defaults to unity gain")

	require.NoError(t, m.Play(mediaslot.Primary, secondPath, PlaybackOptions{Volume: 0.25}))
	second := m.primary
	require.NotNil(t, second)
	assert.NotSame(t, first, second)
	assert.InDelta(t, 0.25, second.volume, 1e-9)
	assert.True(t, first.stopped.Load(), "replaced source must be stopped")

	testDevice.devMu.Lock()
	require.Len(t, testDevice.sources, 1)
	assert.Same(t, second, testDevice.sources[0].source)
	testDevice.devMu.Unlock()

	second.onDrained()
	assert.Equal(t, 1, drainCalls)
	assert.Nil(t, m.primary, "drained current source must clear slot")
	second.stopAndDeregister()
}

// TestLongformPlaybackManager_InvalidSlot verifies every mutating method returns
// an error when given a slot name that is neither primary nor background.
func TestLongformPlaybackManager_InvalidSlot(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager(false)

	require.Error(t,
		m.Play("badslot", filepath.Join("Music", "track.mp3"), PlaybackOptions{}),
		"Play should error on invalid slot",
	)
	require.Error(t, m.Stop("badslot"), "Stop should error on invalid slot")
	require.Error(t, m.Pause("badslot"), "Pause should error on invalid slot")
	require.Error(t, m.Resume("badslot"), "Resume should error on invalid slot")
	require.Error(t, m.TogglePause("badslot"), "TogglePause should error on invalid slot")
	require.Error(t, m.Seek("badslot", 0), "Seek should error on invalid slot")
}

// TestLongformPlaybackManager_NoSourceOps verifies that Stop/Pause/Resume/TogglePause/Seek/State
// are all safe no-ops when no source has been registered for a slot.
func TestLongformPlaybackManager_NoSourceOps(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager(false)

	slots := []string{"", mediaslot.Primary, mediaslot.Background}
	for _, slot := range slots {
		require.NoError(t, m.Stop(slot), "Stop should be no-op with no source (slot=%q)", slot)
		require.NoError(t, m.Pause(slot), "Pause should be no-op with no source (slot=%q)", slot)
		require.NoError(t, m.Resume(slot), "Resume should be no-op with no source (slot=%q)", slot)
		require.NoError(t, m.TogglePause(slot), "TogglePause should be no-op with no source (slot=%q)", slot)
		require.NoError(t, m.Seek(slot, 5*time.Second), "Seek should be no-op with no source (slot=%q)", slot)
		assert.Equal(t, PlaybackState{}, m.State(slot), "State should be empty with no source (slot=%q)", slot)
	}
}

// TestLongformPlaybackManager_WithSourcePrimary exercises Stop/Pause/Resume/TogglePause/Seek/State
// on the primary slot when a source has been directly injected (no audio hardware needed).
func TestLongformPlaybackManager_WithSourcePrimary(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager(false)
	s := newTestSource()
	s.played.Store(targetSampleRate / 2) // 0.5 s in
	m.mu.Lock()
	m.primary = s
	m.mu.Unlock()

	// State returns the source's current state.
	ps := m.State(mediaslot.Primary)
	assert.Equal(t, 500*time.Millisecond, ps.Position)
	assert.True(t, ps.Playing)

	// Seek schedules a seek.
	require.NoError(t, m.Seek(mediaslot.Primary, 0))
	assert.True(t, s.seekPending.Load())

	// Pause sets the paused flag.
	require.NoError(t, m.Pause(mediaslot.Primary))
	assert.True(t, s.paused.Load())

	// TogglePause unpauses.
	require.NoError(t, m.TogglePause(mediaslot.Primary))
	assert.False(t, s.paused.Load())

	// Resume is a no-error call when already unpaused.
	require.NoError(t, m.Resume(mediaslot.Primary))

	// Stop sets stopped, clears the slot, and returns no error.
	require.NoError(t, m.Stop(mediaslot.Primary))
	assert.True(t, s.stopped.Load())
	assert.Equal(t, PlaybackState{}, m.State(mediaslot.Primary))
}

// TestLongformPlaybackManager_WithSourceBackground exercises the same operations
// on the background slot.
func TestLongformPlaybackManager_WithSourceBackground(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager(false)
	s := newTestSource()
	m.mu.Lock()
	m.background = s
	m.mu.Unlock()

	// State reflects background source.
	ps := m.State(mediaslot.Background)
	assert.True(t, ps.Playing)

	// Pause/TogglePause/Resume cycle.
	require.NoError(t, m.Pause(mediaslot.Background))
	assert.True(t, s.paused.Load())

	require.NoError(t, m.TogglePause(mediaslot.Background))
	assert.False(t, s.paused.Load())

	require.NoError(t, m.Resume(mediaslot.Background))

	// Stop clears background slot.
	require.NoError(t, m.Stop(mediaslot.Background))
	assert.True(t, s.stopped.Load())
	assert.Equal(t, PlaybackState{}, m.State(mediaslot.Background))
}

// TestLongformPlaybackManager_SlotAliasesResolve verifies that slot aliases and
// formatting variants ("bg", mixed case, whitespace) reach the same source as
// the canonical name.
func TestLongformPlaybackManager_SlotAliasesResolve(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager(false)
	s := newTestSource()
	m.mu.Lock()
	m.background = s
	m.mu.Unlock()

	for _, alias := range []string{"bg", "Background", " background "} {
		require.NoError(t, m.Pause(alias))
		assert.True(t, s.paused.Load(), "alias %q must resolve to the background source", alias)
		s.paused.Store(false)
	}

	require.NoError(t, m.Stop("bg"))
	assert.True(t, s.stopped.Load(), "Stop via alias must stop the background source")
	assert.Equal(t, PlaybackState{}, m.State(mediaslot.Background))
}

// TestLongformPlaybackManager_SetDrainCallback verifies callbacks can be registered and invoked.
func TestLongformPlaybackManager_SetDrainCallback(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager(false)

	var primaryCalled, backgroundCalled bool
	m.SetDrainCallback(mediaslot.Primary, func(_ bool) { primaryCalled = true })
	m.SetDrainCallback(mediaslot.Background, func(_ bool) { backgroundCalled = true })

	m.mu.Lock()
	pcb := m.drainCallbacks[mediaslot.Primary]
	bcb := m.drainCallbacks[mediaslot.Background]
	m.mu.Unlock()

	require.NotNil(t, pcb)
	require.NotNil(t, bcb)

	pcb(true)
	bcb(true)
	assert.True(t, primaryCalled)
	assert.True(t, backgroundCalled)
}
