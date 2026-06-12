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
	"math"
	"testing"
	"time"

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
	d := &sharedDevice{
		manageCh: make(chan struct{}, 1),
		sources:  []mixSource{drainedSrc, activeSrc},
		mixBuf:   make([][2]float64, 2),
	}
	output := []byte("sentinel sentinel sentinel")

	d.onSamples(output, nil, 3)

	assert.InDelta(t, float32(1), float32At(output, 0, 0), 1e-6, "mixed samples clamp high")
	assert.InDelta(t, float32(-1), float32At(output, 0, 1), 1e-6, "mixed samples clamp low")
	assert.InDelta(t, float32(0.25), float32At(output, 1, 0), 1e-6)
	assert.InDelta(t, float32(0.5), float32At(output, 1, 1), 1e-6)
	assert.Equal(t, make([]byte, len(output)-16), output[16:], "trailing output must be zeroed")
	require.Len(t, d.toRemove, 1)
	assert.Same(t, drainedSrc, d.toRemove[0])
	select {
	case <-d.manageCh:
	default:
		t.Fatal("expected manage wake after source drained")
	}
}

func TestSharedDeviceOpenIfNeededRequiresActiveSource(t *testing.T) {
	t.Parallel()

	d := &sharedDevice{sources: []mixSource{&testMixSource{active: false}}}
	d.openIfNeeded()
	assert.False(t, d.opening)

	d.sources = append(d.sources, &testMixSource{active: true})
	d.openIfNeeded()
	assert.True(t, d.opening)
}

func TestSharedDeviceManageRemovesDrainedSourceAndNotifies(t *testing.T) {
	t.Parallel()

	keepSrc := &testMixSource{active: true}
	drainedSrc := &testMixSource{onDrainedCh: make(chan struct{})}
	d := &sharedDevice{
		manageCh: make(chan struct{}, 1),
		sources:  []mixSource{keepSrc, drainedSrc},
		toRemove: []mixSource{drainedSrc},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go d.manage(ctx, nil, nil, done)

	d.manageCh <- struct{}{}

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
	assert.Same(t, keepSrc, d.sources[0])
	assert.Empty(t, d.toRemove)
}

func TestClampF64(t *testing.T) {
	t.Parallel()

	assert.InDelta(t, -1.0, clampF64(-2, -1, 1), 1e-9)
	assert.InDelta(t, 0.25, clampF64(0.25, -1, 1), 1e-9)
	assert.InDelta(t, 1.0, clampF64(2, -1, 1), 1e-9)
}
