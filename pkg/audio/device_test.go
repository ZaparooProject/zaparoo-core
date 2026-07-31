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
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/gen2brain/malgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testMixSource struct {
	onDrainedCh chan struct{}
	frames      [][2]float64
	active      bool
	drained     bool
}

func (s *testMixSource) mixAdd(buf [][2]float64, n int) (int, bool) {
	written := min(n, len(s.frames))
	for i := range written {
		buf[i][0] += s.frames[i][0]
		buf[i][1] += s.frames[i][1]
	}
	return written, s.drained
}

func (s *testMixSource) isActive() bool {
	return s.active
}

func (s *testMixSource) onDrained() {
	if s.onDrainedCh != nil {
		close(s.onDrainedCh)
	}
}

func setTestSources(d *sharedDevice, sources ...mixSource) {
	d.sources = make([]*mixSourceEntry, len(sources))
	for i, src := range sources {
		d.sources[i] = &mixSourceEntry{source: src}
	}
	d.publishSourcesLocked()
}

func float32At(b []byte, frame, channel int) float32 {
	offset := (frame*2 + channel) * 4
	return math.Float32frombits(binary.LittleEndian.Uint32(b[offset:]))
}

func TestSharedDeviceOnSamplesMixesClampsAndQueuesDrainedSources(t *testing.T) {
	t.Parallel()

	drainedSrc := &testMixSource{
		frames:  [][2]float64{{0.75, -2}, {0.25, 0.5}},
		drained: true,
	}
	activeSrc := &testMixSource{
		frames: [][2]float64{{0.5, 0.25}},
	}
	d := newSharedDevice()
	setTestSources(d, drainedSrc, activeSrc)
	d.mixBuf = make([][2]float64, 2)
	output := []byte("sentinel sentinel sentinel")

	d.onSamples(output, nil, 3)

	assert.InDelta(t, float32(1), float32At(output, 0, 0), 1e-6, "mixed samples clamp high")
	assert.InDelta(t, float32(-1), float32At(output, 0, 1), 1e-6, "mixed samples clamp low")
	assert.InDelta(t, float32(0.25), float32At(output, 1, 0), 1e-6)
	assert.InDelta(t, float32(0.5), float32At(output, 1, 1), 1e-6)
	assert.Equal(t, make([]byte, len(output)-16), output[16:], "trailing output must be zeroed")
	view := d.sourceView.Load()
	require.NotNil(t, view)
	require.Len(t, *view, 2)
	assert.True(t, (*view)[0].drained.Load(), "expected drained source to be marked")
}

func TestSharedDeviceOnSamplesDoesNotAllocate(t *testing.T) {
	d := newSharedDevice()
	setTestSources(d, &testMixSource{frames: [][2]float64{{0.25, 0.25}}})
	d.mixBuf = make([][2]float64, 1)
	output := make([]byte, 8)

	allocs := testing.AllocsPerRun(100, func() {
		d.onSamples(output, nil, 1)
	})
	assert.Zero(t, allocs)
}

func TestSharedDeviceOnSamplesDoesNotWaitForControlLock(t *testing.T) {
	t.Parallel()

	d := newSharedDevice()
	setTestSources(d, &testMixSource{frames: [][2]float64{{0.25, 0.25}}})
	d.mixBuf = make([][2]float64, 1)
	output := make([]byte, 8)

	// Control operations may hold devMu while the realtime callback fires.
	// Callback must use its immutable snapshot rather than wait on that lock.
	d.devMu.Lock()
	defer d.devMu.Unlock()
	done := make(chan struct{})
	go func() {
		d.onSamples(output, nil, 1)
		close(done)
	}()

	select {
	case <-done:
		assert.InDelta(t, float32(0.25), float32At(output, 0, 0), 1e-6)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("realtime callback blocked on device control lock")
	}
}

func TestSharedDeviceOpenIfNeededRequiresActiveSource(t *testing.T) {
	t.Parallel()

	d := newSharedDevice()
	setTestSources(d, &testMixSource{active: false})
	d.openIfNeeded()
	d.devMu.Lock()
	opening := d.opening
	d.devMu.Unlock()
	assert.False(t, opening)

	setTestSources(d, d.sources[0].source, &testMixSource{active: true})
	d.openIfNeeded()
	// Read under devMu: failAllSources (called when ALSA is unavailable) also
	// holds devMu when it clears d.opening, so the lock ensures we observe the
	// value set by openIfNeeded before the background goroutine can clear it.
	d.devMu.Lock()
	opening = d.opening
	d.devMu.Unlock()
	assert.True(t, opening)
}

func TestSharedDeviceManageRemovesDrainedSourceAndNotifies(t *testing.T) {
	t.Parallel()

	keepSrc := &testMixSource{active: true}
	drainedSrc := &testMixSource{onDrainedCh: make(chan struct{})}
	d := newSharedDevice()
	setTestSources(d, keepSrc, drainedSrc)
	d.sources[1].drained.Store(true)
	d.drainPending.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go d.manage(ctx, nil, nil, done)

	select {
	case <-drainedSrc.onDrainedCh:
	case <-time.After(time.Second):
		t.Fatal("expected drained source callback")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected manager cleanup after cancellation")
	}

	require.Len(t, d.sources, 1)
	assert.Same(t, keepSrc, d.sources[0].source)
	view := d.sourceView.Load()
	require.NotNil(t, view)
	require.Len(t, *view, 1)
	assert.Same(t, keepSrc, (*view)[0].source)
}

// TestSharedDeviceOpenRequestsRealtimeThreadPriority verifies open passes
// realtime thread priority to the malgo context, so the audio callback runs
// under SCHED_FIFO on Linux instead of competing with normal process threads.
func TestSharedDeviceOpenRequestsRealtimeThreadPriority(t *testing.T) {
	t.Parallel()

	var got malgo.ContextConfig
	stubInit := func(_ []malgo.Backend, cfg malgo.ContextConfig, _ malgo.LogProc) (*malgo.AllocatedContext, error) {
		got = cfg
		return nil, errors.New("stub: no real audio context in tests")
	}
	d := newSharedDevice()
	d.initContext = stubInit
	setTestSources(d, &testMixSource{active: true})

	d.open()

	assert.Equal(t, malgo.ThreadPriorityRealtime, got.ThreadPriority)
	assert.Empty(t, d.sources, "init failure must clear all sources")
}

func TestClampF64(t *testing.T) {
	t.Parallel()

	assert.InDelta(t, -1.0, clampF64(-2, -1, 1), 1e-9)
	assert.InDelta(t, 0.25, clampF64(0.25, -1, 1), 1e-9)
	assert.InDelta(t, 1.0, clampF64(2, -1, 1), 1e-9)
}
