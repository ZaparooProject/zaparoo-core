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

import "math"

const (
	// normTargetLUFS is the loudness every source is levelled to.
	normTargetLUFS = -16.0
	// replayGainRefLUFS is the loudness a ReplayGain 2 adjustment lands on.
	replayGainRefLUFS = -18.0
	// normMaxGainDB bounds the leveller's boost and cut.
	normMaxGainDB = 12.0
	// normSlewDBPerSec is how fast the gain follows the music once settled,
	// slow enough that it is not heard as pumping.
	normSlewDBPerSec = 1.0
	// normFastSlewDBPerSec is how fast the gain moves while the first few
	// seconds are still being measured, and when the setting is switched.
	normFastSlewDBPerSec = 6.0
	// normWarmupFrames is how much audio is measured before the first
	// frame plays, so a track starts at its level rather than fading to it.
	normWarmupFrames = targetSampleRate
	// normBlockFrames is the loudness measurement block.
	normBlockFrames = targetSampleRate / 10
	// normSettleBlocks is how many measured blocks the estimate averages
	// equally before it becomes a running average.
	normSettleBlocks = 30
	// normEstimateAlpha weights each new block in the running average:
	// a time constant of about ten seconds.
	normEstimateAlpha = 0.01
	// normAbsGateLUFS and normRelGateDB drop silence and quiet passages
	// from the estimate, so a fade or a pause is not boosted.
	normAbsGateLUFS = -50.0
	normRelGateDB   = -20.0
	// limiterCeiling is -1 dBFS.
	limiterCeiling = 0.8912509381337456
	// limiterRelease recovers about 63% of the remaining reduction every
	// 50 ms.
	limiterRelease = 1.0 / (0.05 * targetSampleRate)
)

// biquad is one second-order filter section in direct form I.
type biquad struct {
	b0, b1, b2, a1, a2 float64
	x1, x2, y1, y2     float64
}

func (f *biquad) step(x float64) float64 {
	y := f.b0*x + f.b1*f.x1 + f.b2*f.x2 - f.a1*f.y1 - f.a2*f.y2
	f.x2, f.x1 = f.x1, x
	f.y2, f.y1 = f.y1, y
	return y
}

// kWeighting is the ITU-R BS.1770 K-weighting filter at 48 kHz: a high
// shelf for the head followed by a high-pass.
type kWeighting struct {
	shelf, highPass biquad
}

func newKWeighting() kWeighting {
	return kWeighting{
		shelf: biquad{
			b0: 1.53512485958697, b1: -2.69169618940638, b2: 1.19839281085285,
			a1: -1.69065929318241, a2: 0.73248077421585,
		},
		highPass: biquad{
			b0: 1.0, b1: -2.0, b2: 1.0,
			a1: -1.99004745483398, a2: 0.99007225036621,
		},
	}
}

func (k *kWeighting) step(x float64) float64 {
	return k.highPass.step(k.shelf.step(x))
}

// normalizer levels one source to normTargetLUFS. It runs on the source's
// prefetch goroutine, on frames at targetSampleRate before they enter the
// ring, so the audio thread does no extra work.
type normalizer struct {
	weight [2]kWeighting
	// blockPower and blockFrames accumulate the K-weighted power of the
	// block being measured.
	blockPower  float64
	blockFrames int
	// estimate is the mean K-weighted power of the measured blocks; zero
	// until a block passes the gates.
	estimate     float64
	gatedBlocks  int
	gainDB       float64
	limiter      float64
	replayGainDB float64
	hasReplay    bool
	settled      bool
}

func newNormalizer() *normalizer {
	return &normalizer{
		weight:  [2]kWeighting{newKWeighting(), newKWeighting()},
		limiter: 1,
	}
}

// powerToLUFS converts a mean K-weighted power summed over both channels
// to loudness.
func powerToLUFS(power float64) float64 {
	return -0.691 + 10*math.Log10(power)
}

// observe measures frames without changing them.
func (n *normalizer) observe(frames [][2]float64) {
	for i := range frames {
		l := n.weight[0].step(frames[i][0])
		r := n.weight[1].step(frames[i][1])
		n.blockPower += l*l + r*r
		n.blockFrames++
		if n.blockFrames == normBlockFrames {
			n.endBlock()
		}
	}
}

func (n *normalizer) endBlock() {
	power := n.blockPower / float64(n.blockFrames)
	n.blockPower, n.blockFrames = 0, 0
	if power <= 0 {
		return
	}
	loudness := powerToLUFS(power)
	if loudness < normAbsGateLUFS {
		return
	}
	if n.estimate > 0 && loudness < powerToLUFS(n.estimate)+normRelGateDB {
		return
	}
	n.gatedBlocks++
	if n.gatedBlocks <= normSettleBlocks {
		n.estimate += (power - n.estimate) / float64(n.gatedBlocks)
		return
	}
	n.estimate += (power - n.estimate) * normEstimateAlpha
}

// setReplayGain makes the track's ReplayGain tag the source of its level in
// place of the measurement.
func (n *normalizer) setReplayGain(db float64) {
	n.replayGainDB, n.hasReplay = db, true
}

// targetDB is the gain the source should play at, or the current gain when
// nothing has been measured yet.
func (n *normalizer) targetDB() float64 {
	var target float64
	switch {
	case n.hasReplay:
		target = n.replayGainDB + normTargetLUFS - replayGainRefLUFS
	case n.estimate > 0:
		target = normTargetLUFS - powerToLUFS(n.estimate)
	default:
		return n.gainDB
	}
	return max(-normMaxGainDB, min(normMaxGainDB, target))
}

// settle jumps straight to the measured gain. Called once, when the first
// frames are about to play.
func (n *normalizer) settle() {
	n.gainDB = n.targetDB()
	n.settled = true
}

// bypassed reports whether a disabled normalizer has nothing left to undo.
func (n *normalizer) bypassed() bool {
	return n.gainDB == 0 && n.limiter == 1
}

// apply levels frames in place. enabled false moves the gain back to
// unity and then leaves frames untouched.
func (n *normalizer) apply(frames [][2]float64, enabled bool) {
	if len(frames) == 0 {
		return
	}
	target, rate := 0.0, normFastSlewDBPerSec
	if enabled {
		target = n.targetDB()
		// A ReplayGain target never moves, so it is reached quickly.
		if n.gatedBlocks > normSettleBlocks && !n.hasReplay {
			rate = normSlewDBPerSec
		}
	}
	maxStep := rate * float64(len(frames)) / float64(targetSampleRate)
	next := n.gainDB + max(-maxStep, min(maxStep, target-n.gainDB))
	from, to := dbToGain(n.gainDB), dbToGain(next)
	n.gainDB = next
	step := (to - from) / float64(len(frames))
	gain := from
	limiter := n.limiter
	for i := range frames {
		gain += step
		l, r := frames[i][0]*gain, frames[i][1]*gain
		peak := max(math.Abs(l), math.Abs(r))
		if peak*limiter > limiterCeiling {
			limiter = limiterCeiling / peak
		} else {
			limiter += (1 - limiter) * limiterRelease
		}
		frames[i][0], frames[i][1] = l*limiter, r*limiter
	}
	if !enabled && n.gainDB == 0 && limiter > 0.999 {
		limiter = 1
	}
	n.limiter = limiter
}

func dbToGain(db float64) float64 {
	return math.Pow(10, db/20)
}
