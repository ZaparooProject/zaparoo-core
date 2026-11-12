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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Without a live input watch nothing is known about the user, so idle time is
// not counted until the watch is back.
func TestIdle_WatchDownIsNotInactivity(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput())
	r.load(armedPlaylist())
	w := r.input.nextWatch(t)
	r.tick(4 * time.Minute)

	close(w.end)
	eventually(t, func() bool {
		down := false
		r.probe(func(e *Engine) { down = e.watch == nil })
		return down
	})

	// The idle time would have run out here; the watch is back on the way.
	r.tick(2 * time.Minute)
	r.quiet()
	again := r.input.nextWatch(t)
	require.NoError(t, again.ctx.Err())

	r.tick(idleFor - time.Second)
	r.quiet()
	r.tick(time.Second)
	assert.True(t, r.host.last(t).Playing, "a full idle time after the watch came back")
}

func TestIdle_StopsAndForeignLaunchesCountAsActivity(t *testing.T) {
	t.Parallel()

	t.Run("a stop shortly before the deadline", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withInput())
		gen := r.play(idlePlaylist(3, 0, nil))
		r.input.nextWatch(t)
		r.tick(4*time.Minute + 59*time.Second)
		r.end(gen, models.MediaStopReasonStopped)
		r.tick(time.Second)
		r.quiet()
		r.tick(idleFor - 2*time.Second)
		r.quiet()
		r.tick(2 * time.Second)
		assert.Equal(t, 1, r.host.last(t).Index)
	})

	t.Run("a replaced item", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withInput())
		gen := r.play(idlePlaylist(3, 0, nil))
		r.input.nextWatch(t)
		r.tick(4*time.Minute + 59*time.Second)
		r.end(gen, models.MediaStopReasonReplaced)
		r.tick(time.Second)
		r.quiet()
	})

	t.Run("media started by someone else", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withInput())
		r.load(armedPlaylist())
		r.input.nextWatch(t)
		r.tick(4*time.Minute + 59*time.Second)
		gen := r.foreign("api launch")
		r.end(gen, models.MediaStopReasonFinished)
		r.tick(time.Second)
		r.quiet()
		r.tick(idleFor - time.Second)
		assert.True(t, r.host.last(t).Playing)
	})

	t.Run("an exit is not activity", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withInput())
		gen := r.play(idlePlaylist(3, 0, nil))
		r.input.nextWatch(t)
		r.tick(idleFor - time.Second)
		r.end(gen, models.MediaStopReasonExited)
		r.tick(time.Second)
		assert.Equal(t, 1, r.host.last(t).Index)
	})

	t.Run("the engine's own launch is not activity", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withInput(), withUI(false))
		r.play(idlePlaylist(3, 0, nil))
		r.input.nextWatch(t)
		r.tick(idleFor)
		next := r.host.last(t)
		require.Equal(t, 1, next.Index)
		r.play(next)
		r.tick(idleFor)
		assert.Equal(t, 2, r.host.last(t).Index, "rotating again a full idle time later")
	})
}

func asyncLaunch(r *rig, pls *playlists.Playlist) {
	r.t.Helper()
	r.host.set(pls.Slot, pls)
	r.eng.ItemLaunching(pls)
	r.eng.ItemLaunched(pls, nil, true)
	r.eng.Sync()
}

func TestPendingBind_ExternalLauncherMediaBindsWithinTheWindow(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	pls := testPlaylist(3, 0, nil)
	asyncLaunch(r, pls)
	r.tick(20 * time.Second)
	gen := r.foreign("external game")
	r.end(gen, models.MediaStopReasonFinished)

	got := r.host.last(t)
	assert.Equal(t, 1, got.Index)
	assert.True(t, got.EngineStarted)
}

func TestPendingBind_KeepsTheEngineStartedMark(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	pls := engineStarted(testPlaylist(3, 0, nil))
	r.host.set(pls.Slot, pls)
	r.eng.ItemLaunching(pls)
	r.eng.ItemLaunched(pls, nil, true)
	r.eng.Sync()
	gen := r.foreign("external game")
	r.tick(10 * time.Second)
	r.end(gen, exited)
	assert.Equal(t, 1, r.host.pausedCount(), "an automatic start that quit soon after pauses the playlist")
}

func TestPendingBind_NeverBindsWhatWasNotMarkedAsPublishingLater(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	pls := testPlaylist(3, 0, nil)
	r.host.set(pls.Slot, pls)
	r.eng.ItemLaunching(pls)
	r.eng.ItemLaunched(pls, nil, false)
	r.eng.Sync()
	gen := r.foreign("something else")
	r.end(gen, models.MediaStopReasonFinished)
	r.quiet()
}

func TestPendingBind_DoesNotBindAFailedLaunch(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	pls := testPlaylist(3, 0, nil)
	r.host.set(pls.Slot, pls)
	r.eng.ItemLaunching(pls)
	r.eng.ItemLaunched(pls, assert.AnError, true)
	r.eng.Sync()
	gen := r.foreign("something else")
	r.end(gen, models.MediaStopReasonFinished)
	r.quiet()
}

func TestPendingBind_ClosesOnTimeoutLaunchOrPlaylistChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		close func(r *rig)
		name  string
	}{
		{name: "timeout", close: func(r *rig) { r.tick(31 * time.Second) }},
		{name: "another launch", close: func(r *rig) {
			other := testPlaylist(3, 2, nil)
			r.eng.ItemLaunching(other)
			r.eng.ItemLaunched(other, nil, false)
			r.eng.Sync()
		}},
		{name: "playlist change", close: func(r *rig) { r.changed() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			asyncLaunch(r, testPlaylist(3, 0, nil))
			tt.close(r)
			gen := r.foreign("late")
			r.end(gen, models.MediaStopReasonFinished)
			r.quiet()
		})
	}
}

func TestPendingBind_FirstMediaOnlyAndBackgroundIgnored(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	pls := testPlaylist(3, 0, nil)
	asyncLaunch(r, pls)
	first := r.foreign("first")
	second := r.foreign("second")
	r.end(second, models.MediaStopReasonFinished)
	r.quiet()
	r.end(first, models.MediaStopReasonFinished)
	r.quiet()
}

func TestMoves_RetriedWhenTheQueueRefusesThem(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	gen := r.play(testPlaylist(3, 0, nil))
	r.end(gen, models.MediaStopReasonFinished)
	require.Len(t, r.host.queued(), 1)

	r.eng.ItemRefused(r.host.last(t))
	r.eng.Sync()
	r.tick(199 * time.Millisecond)
	assert.Len(t, r.host.queued(), 1)
	r.tick(time.Millisecond)
	require.Len(t, r.host.queued(), 2)
	again := r.host.last(t)
	assert.Equal(t, 1, again.Index)
	assert.True(t, again.EngineStarted)
	assert.Equal(t, 0, again.EngineFromIndex)
}

func TestMoves_RetryIsBounded(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	gen := r.play(testPlaylist(3, 0, nil))
	r.end(gen, models.MediaStopReasonFinished)
	for range 10 {
		r.eng.ItemRefused(r.host.last(t))
		r.eng.Sync()
		r.tick(200 * time.Millisecond)
	}
	assert.Len(t, r.host.queued(), 1+moveMaxRetries)
}

func TestMoves_RetryStopsWhenThePlaylistMovedOnOrTheMoveLaunched(t *testing.T) {
	t.Parallel()

	t.Run("user moved the playlist", func(t *testing.T) {
		t.Parallel()
		r := newRig(t)
		pls := testPlaylist(3, 0, nil)
		gen := r.play(pls)
		r.end(gen, models.MediaStopReasonFinished)
		r.eng.ItemRefused(r.host.last(t))
		r.eng.Sync()
		r.host.set(mediaslot.Primary, playlists.Goto(*pls, 2))
		r.tick(time.Second)
		assert.Len(t, r.host.queued(), 1)
	})

	t.Run("the move launched", func(t *testing.T) {
		t.Parallel()
		r := newRig(t)
		gen := r.play(testPlaylist(3, 0, nil))
		r.end(gen, models.MediaStopReasonFinished)
		upd := r.host.last(t)
		r.eng.ItemRefused(upd)
		r.eng.Sync()
		r.eng.ItemLaunching(upd)
		r.eng.Sync()
		r.tick(time.Second)
		assert.Len(t, r.host.queued(), 1)
	})

	t.Run("refusal of someone else's update is ignored", func(t *testing.T) {
		t.Parallel()
		r := newRig(t)
		gen := r.play(testPlaylist(3, 0, nil))
		r.end(gen, models.MediaStopReasonFinished)
		r.eng.ItemRefused(&playlists.Playlist{ID: "p", EngineStarted: true})
		r.eng.Sync()
		r.tick(time.Second)
		assert.Len(t, r.host.queued(), 1)
	})
}

func TestMoves_ClearCarriesTheIdentityItWasDecidedOn(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	gen := r.play(testPlaylist(3, 2, nil))
	r.end(gen, models.MediaStopReasonFinished)
	got := r.host.last(t)
	assert.True(t, got.Clear)
	assert.True(t, got.EngineStarted)
	assert.Equal(t, "p", got.ID)
	assert.Equal(t, 2, got.EngineFromIndex)
}

func TestPrompt_NotOpenedOverAnotherEvent(t *testing.T) {
	t.Parallel()

	t.Run("an exit countdown runs without a prompt", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(true))
		r.ui.setOthers(models.UIEvent{ID: "launch-guard"})
		gen := r.play(testPlaylist(3, 0, nil))
		r.end(gen, exited)
		assert.Zero(t, r.ui.opened(), "the user's confirmation stays open")
		r.quiet()
		r.tick(time10s)
		assert.Equal(t, 1, r.host.last(t).Index, "the countdown still ran")
	})

	t.Run("a rotation switches silently", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(true))
		r.ui.setOthers(models.UIEvent{ID: "launch-guard"})
		r.play(advancing(3, 0))
		r.tick(time.Minute)
		assert.Zero(t, r.ui.opened())
		assert.Equal(t, 1, r.host.last(t).Index)
	})

	t.Run("the engine's own earlier prompt is not another event", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(true))
		gen := r.play(testPlaylist(3, 0, nil))
		r.end(gen, exited)
		require.Equal(t, 1, r.ui.opened())
	})
}

func TestPrompt_SupersededByAnotherEvent(t *testing.T) {
	t.Parallel()

	superseded := uievents.Result{Resolution: models.UIResolution{Outcome: models.UIOutcomeSuperseded}}

	t.Run("an exit countdown is cancelled", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(false))
		gen := r.play(testPlaylist(3, 0, nil))
		r.end(gen, exited)
		r.ui.respond(0, superseded)
		eventually(t, func() bool { return r.ui.dismissed(0) })
		r.tick(time.Minute)
		r.quiet()
	})

	t.Run("a rotation goes ahead without it", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(true))
		r.play(advancing(3, 0))
		r.tick(time.Minute)
		r.ui.respond(0, superseded)
		r.eng.Sync()
		r.quiet()
		r.tick(time10s)
		assert.Equal(t, 1, r.host.last(t).Index)
	})
}

func TestPrompt_ExpiryLeavesTheTimeoutToTheUI(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	gen := r.play(testPlaylist(3, 0, nil))
	r.end(gen, exited)
	r.tick(time10s)
	assert.Equal(t, 1, r.host.last(t).Index)
	assert.False(t, r.ui.dismissed(0), "the engine does not cancel a prompt that is timing out itself")

	r.ui.respond(0, uievents.Result{Resolution: models.UIResolution{Outcome: models.UIOutcomeTimedOut}})
	eventually(t, func() bool { return r.ui.dismissed(0) })
	assert.Len(t, r.host.queued(), 1, "the late result does not move the playlist again")
}

// A stop the media events have not reported yet still counts as someone using
// the device: an idle rotation that fell due in the meantime was decided
// without it.
func TestIdle_UnreportedStopHoldsBackTheRotation(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput(), withUI(false))
	r.play(idlePlaylist(3, 0, nil))
	r.input.nextWatch(t)
	r.tick(4*time.Minute + 59*time.Second)

	r.host.userStop()
	r.tick(time.Second)
	r.quiet()
	r.tick(idleFor - time.Second)
	r.quiet()
	r.tick(time.Second)
	assert.Equal(t, 1, r.host.last(t).Index)
}

func TestMoves_OnesTheEngineMakesCarryTheStopCountOfTheirDecision(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput(), withUI(false))
	r.play(idlePlaylist(3, 0, nil))
	r.input.nextWatch(t)
	r.host.userStop()
	r.tick(idleFor + time.Second)
	r.tick(idleFor)
	assert.Equal(t, uint64(1), r.host.last(t).StopSeq)
}
