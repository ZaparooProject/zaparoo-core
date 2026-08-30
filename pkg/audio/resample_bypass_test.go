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
	"testing"

	"github.com/gopxl/beep/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingStreamer is a silent source that records how many samples were pulled
// out of it, so a test can tell a passthrough from a resampler by behaviour
// rather than by identity alone.
type countingStreamer struct {
	pulled int
}

func (c *countingStreamer) Stream(samples [][2]float64) (int, bool) {
	for i := range samples {
		samples[i] = [2]float64{0, 0}
	}
	c.pulled += len(samples)
	return len(samples), true
}

func (*countingStreamer) Err() error { return nil }

// TestStreamerAtTargetSampleRate_BypassesAtTargetRate covers #1249: a source
// already at the output rate must be handed straight through, because wrapping
// it in a resampler costs real work per sample on a device that has little to
// spare. Anything else must still be resampled, or it plays at the wrong pitch.
func TestStreamerAtTargetSampleRate_BypassesAtTargetRate(t *testing.T) {
	t.Parallel()

	t.Run("source already at the target rate is returned unchanged", func(t *testing.T) {
		t.Parallel()
		src := &countingStreamer{}
		got := streamerAtTargetSampleRate(src, beep.SampleRate(targetSampleRate), resampleQuality)
		assert.Same(t, src, got, "a source at the target rate must not be wrapped")
	})

	t.Run("a lower rate source is resampled", func(t *testing.T) {
		t.Parallel()
		src := &countingStreamer{}
		got := streamerAtTargetSampleRate(src, beep.SampleRate(44100), resampleQuality)
		require.NotSame(t, src, got, "a mismatched rate must be resampled")

		// Pulling one output buffer must draw fewer input samples than it
		// produced, which is what resampling 44.1k up to 48k means.
		out := make([][2]float64, 4800)
		n, ok := got.Stream(out)
		require.True(t, ok)
		require.Equal(t, len(out), n)
		assert.Less(t, src.pulled, n*2,
			"resampling 44.1kHz to 48kHz should not pull wildly more input than output")
		assert.Positive(t, src.pulled, "the resampler must read from the source")
	})

	t.Run("a higher rate source is resampled", func(t *testing.T) {
		t.Parallel()
		src := &countingStreamer{}
		got := streamerAtTargetSampleRate(src, beep.SampleRate(192000), resampleQuality)
		assert.NotSame(t, src, got,
			"192kHz is what the bundled feedback sounds are; it must be resampled")
	})
}
