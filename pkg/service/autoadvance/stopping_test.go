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

package autoadvance

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
)

// A stop that takes seconds is running when the advance timer runs out. The
// person stopping wins: nothing is decided until it is over, and the media
// ending ends the item.
func TestAdvanceTimer_DeadlineDuringAStopMakesNoMove(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	gen := r.play(advancing(3, 0))
	r.tick(50 * time.Second)

	r.host.beginStop()
	r.tick(10 * time.Second) // the deadline passes while the stop runs
	r.quiet()
	r.tick(time.Second)
	r.quiet()

	r.end(gen, models.MediaStopReasonStopped)
	r.host.endStop()
	r.tick(time.Minute)
	r.quiet()
}

func TestAdvanceTimer_FailedStopLetsTheDeadlineThrough(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	r.play(advancing(3, 0))
	r.tick(50 * time.Second)
	r.host.beginStop()
	r.tick(10 * time.Second)
	r.quiet()

	r.host.endStop() // it failed: the media is still there
	r.tick(stopPoll)
	assert.Equal(t, 1, r.host.last(t).Index, "the timer carries on once the stop is over")
}

func TestIdle_RotationDuringAStopMakesNoMove(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput(), withUI(false))
	gen := r.play(idlePlaylist(3, 0, nil))
	r.input.nextWatch(t)
	r.tick(idleFor - 2*time.Second)

	r.host.beginStop()
	r.tick(5 * time.Second)
	r.quiet()
	r.end(gen, models.MediaStopReasonStopped)
	r.host.endStop()
	r.tick(idleFor - time.Second)
	r.quiet()
}

func TestMoves_NotMadeWhileAStopRuns(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	gen := r.play(testPlaylist(3, 0, nil))
	r.host.beginStop()
	r.end(gen, models.MediaStopReasonFinished)
	r.quiet()
}

// A move decided before the stop started, or while it ran, carries a count the
// stop's completion has passed.
func TestMoves_StampedBeforeAStopCompletesAreStale(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	gen := r.play(testPlaylist(3, 0, nil))
	r.end(gen, models.MediaStopReasonFinished)
	move := r.host.last(t)

	r.host.beginStop()
	r.host.endStop()
	assert.Greater(t, r.host.StopSeq(), move.StopSeq)
}

// A stop that finished long before the deadline changes nothing about the
// rotation after it.
func TestAdvanceTimer_EarlierStopCompletedAndNextItemRotates(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	pls := advancing(3, 0)
	gen := r.play(pls)
	r.tick(20 * time.Second)
	r.host.beginStop()
	r.end(gen, models.MediaStopReasonStopped)
	r.host.endStop()
	r.tick(time.Minute)
	r.quiet()

	// The user plays the playlist again; its timer runs as usual.
	r.play(pls)
	r.tick(time.Minute)
	assert.Equal(t, 1, r.host.last(t).Index)
}
