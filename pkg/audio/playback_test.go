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
	s.played = targetSampleRate / 2 // 0.5 s in

	// Default: not paused, not stopped, not eof → Playing
	ps := s.state()
	assert.True(t, ps.Playing)
	assert.False(t, ps.Paused)
	assert.Equal(t, 500*time.Millisecond, ps.Position)
	assert.Equal(t, time.Second, ps.Duration)

	// Paused → Playing=false
	s.paused = true
	assert.False(t, s.state().Playing)
	assert.True(t, s.state().Paused)
	s.paused = false

	// Stopped → Playing=false
	s.stopped = true
	assert.False(t, s.state().Playing)
	s.stopped = false

	// EOF with ring drained → Playing=false
	s.eof = true
	s.filled = 0
	assert.False(t, s.state().Playing)

	// EOF but ring still has frames → Playing=true (tail draining)
	s.filled = 10
	assert.True(t, s.state().Playing)
}

func TestStreamingSource_IsActive(t *testing.T) {
	t.Parallel()
	s := newTestSource()

	assert.True(t, s.isActive())
	s.paused = true
	assert.False(t, s.isActive())
	s.paused = false
	s.stopped = true
	assert.False(t, s.isActive())
}

func TestStreamingSource_OnDrained(t *testing.T) {
	t.Parallel()
	s := newTestSource()

	// No callback: no panic.
	s.onDrained()

	called := false
	s.onDrain = func() { called = true }
	s.onDrained()
	assert.True(t, called)
}

func TestStreamingSource_SetPaused(t *testing.T) {
	t.Parallel()
	s := newTestSource()

	s.setPaused(true)
	s.mu.Lock()
	assert.True(t, s.paused)
	s.mu.Unlock()

	// Resume writes to wakeCh.
	s.setPaused(false)
	s.mu.Lock()
	assert.False(t, s.paused)
	s.mu.Unlock()

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
	s.mu.Lock()
	assert.True(t, s.paused)
	s.mu.Unlock()

	nowPaused = s.togglePause()
	assert.False(t, nowPaused)
	s.mu.Lock()
	assert.False(t, s.paused)
	s.mu.Unlock()

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
	// Pre-fill the ring to verify it is flushed on seek.
	s.filled = 50
	s.wpos = 50
	s.played = int64(targetSampleRate) // 1 s

	s.seek(0) // seek to current position (offset=0)

	s.mu.Lock()
	assert.True(t, s.seekPending, "seekPending must be set")
	assert.Equal(t, 0, s.filled, "ring must be flushed")
	assert.Equal(t, 0, s.wpos, "write pos must be reset")
	assert.Equal(t, 0, s.rpos, "read pos must be reset")
	s.mu.Unlock()

	select {
	case <-s.wakeCh:
	default:
		t.Fatal("expected wake signal after seek")
	}
}

func TestStreamingSource_MixAdd(t *testing.T) {
	t.Parallel()
	s := newTestSource()

	// Fill ring with known samples.
	const nFrames = 10
	for i := range nFrames {
		s.ring[i] = [2]float64{float64(i+1) * 0.1, float64(i+1) * 0.1}
	}
	s.wpos = nFrames
	s.filled = nFrames

	buf := make([][2]float64, 20)
	n, drained := s.mixAdd(buf, nFrames)

	assert.Equal(t, nFrames, n)
	assert.False(t, drained, "not drained — eof is false")
	// Volume=1 so buf values should equal the ring values.
	for i := range nFrames {
		assert.InDelta(t, float64(i+1)*0.1, buf[i][0], 1e-9)
	}

	// Now drained: eof=true, ring empty after mix.
	s.eof = true
	buf2 := make([][2]float64, 5)
	n2, drained2 := s.mixAdd(buf2, 5)
	assert.Equal(t, 0, n2)
	assert.True(t, drained2)

	// Stopped: drains immediately.
	s2 := newTestSource()
	s2.stopped = true
	_, stopped := s2.mixAdd(buf, 5)
	assert.True(t, stopped)
}

func TestNewStreamingSource_UnsupportedExtension(t *testing.T) {
	t.Parallel()
	// The file must exist — the extension check happens after os.Open succeeds.
	path := filepath.Join(t.TempDir(), "audio.xyz")
	require.NoError(t, os.WriteFile(path, []byte("fake"), 0o600))
	_, err := newStreamingSource(path, 1.0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported audio format")
}

func TestNewStreamingSource_FileNotFound(t *testing.T) {
	t.Parallel()
	_, err := newStreamingSource(filepath.Join(t.TempDir(), "missing.mp3"), 1.0)
	require.Error(t, err)
}

func TestNewLongformPlaybackManager(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager()
	require.NotNil(t, m)
	assert.NotNil(t, m.drainCallbacks)
	assert.Nil(t, m.primary)
	assert.Nil(t, m.background)
}

// TestLongformPlaybackManager_InvalidSlot verifies every mutating method returns
// an error when given a slot name that is neither primary nor background.
func TestLongformPlaybackManager_InvalidSlot(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager()

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
	m := NewLongformPlaybackManager()

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
	m := NewLongformPlaybackManager()
	s := newTestSource()
	s.played = int64(targetSampleRate / 2) // 0.5 s in
	m.mu.Lock()
	m.primary = s
	m.mu.Unlock()

	// State returns the source's current state.
	ps := m.State(mediaslot.Primary)
	assert.Equal(t, 500*time.Millisecond, ps.Position)
	assert.True(t, ps.Playing)

	// Seek schedules a seek.
	require.NoError(t, m.Seek(mediaslot.Primary, 0))
	s.mu.Lock()
	assert.True(t, s.seekPending)
	s.mu.Unlock()

	// Pause sets the paused flag.
	require.NoError(t, m.Pause(mediaslot.Primary))
	s.mu.Lock()
	assert.True(t, s.paused)
	s.mu.Unlock()

	// TogglePause unpauses.
	require.NoError(t, m.TogglePause(mediaslot.Primary))
	s.mu.Lock()
	assert.False(t, s.paused)
	s.mu.Unlock()

	// Resume is a no-error call when already unpaused.
	require.NoError(t, m.Resume(mediaslot.Primary))

	// Stop sets stopped, clears the slot, and returns no error.
	require.NoError(t, m.Stop(mediaslot.Primary))
	s.mu.Lock()
	assert.True(t, s.stopped)
	s.mu.Unlock()
	assert.Equal(t, PlaybackState{}, m.State(mediaslot.Primary))
}

// TestLongformPlaybackManager_WithSourceBackground exercises the same operations
// on the background slot.
func TestLongformPlaybackManager_WithSourceBackground(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager()
	s := newTestSource()
	m.mu.Lock()
	m.background = s
	m.mu.Unlock()

	// State reflects background source.
	ps := m.State(mediaslot.Background)
	assert.True(t, ps.Playing)

	// Pause/TogglePause/Resume cycle.
	require.NoError(t, m.Pause(mediaslot.Background))
	s.mu.Lock()
	assert.True(t, s.paused)
	s.mu.Unlock()

	require.NoError(t, m.TogglePause(mediaslot.Background))
	s.mu.Lock()
	assert.False(t, s.paused)
	s.mu.Unlock()

	require.NoError(t, m.Resume(mediaslot.Background))

	// Stop clears background slot.
	require.NoError(t, m.Stop(mediaslot.Background))
	s.mu.Lock()
	assert.True(t, s.stopped)
	s.mu.Unlock()
	assert.Equal(t, PlaybackState{}, m.State(mediaslot.Background))
}

// TestLongformPlaybackManager_SetDrainCallback verifies callbacks can be registered and invoked.
func TestLongformPlaybackManager_SetDrainCallback(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager()

	var primaryCalled, backgroundCalled bool
	m.SetDrainCallback(mediaslot.Primary, func() { primaryCalled = true })
	m.SetDrainCallback(mediaslot.Background, func() { backgroundCalled = true })

	m.mu.Lock()
	pcb := m.drainCallbacks[mediaslot.Primary]
	bcb := m.drainCallbacks[mediaslot.Background]
	m.mu.Unlock()

	require.NotNil(t, pcb)
	require.NotNil(t, bcb)

	pcb()
	bcb()
	assert.True(t, primaryCalled)
	assert.True(t, backgroundCalled)
}
