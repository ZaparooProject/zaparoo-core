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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func advancing(items, index int) *playlists.Playlist {
	return testPlaylist(items, index, func(p *playlists.Playlist) { p.Advance = time.Minute })
}

func TestAdvanceTimer_SwitchesSilentlyWithoutARenderer(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	r.play(advancing(3, 0))
	r.tick(59 * time.Second)
	r.quiet()
	r.tick(time.Second)

	got := r.host.last(t)
	assert.Equal(t, 1, got.Index)
	assert.True(t, got.Playing)
	assert.True(t, got.EngineStarted)
	assert.Zero(t, r.ui.opened(), "no countdown without a renderer")
	r.tick(time.Hour)
	assert.Len(t, r.host.queued(), 1, "an expired timer fires once")
}

func TestAdvanceTimer_ZeroCountdownSwitchesSilentlyEvenWithARenderer(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(true))
	r.play(testPlaylist(3, 0, func(p *playlists.Playlist) {
		p.Advance = time.Minute
		p.CountdownSet = true
	}))
	r.tick(time.Minute)
	assert.Equal(t, 1, r.host.last(t).Index)
	assert.Zero(t, r.ui.opened())
}

func TestAdvanceTimer_StartsWhenMediaIsPublishedForEachItem(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	r.play(advancing(3, 0))
	r.tick(30 * time.Second)
	r.tick(30 * time.Second)
	next := r.host.last(t)
	require.Equal(t, 1, next.Index)

	// The next item's media appears later; its timer starts then.
	r.tick(10 * time.Second)
	r.play(next)
	r.tick(59 * time.Second)
	assert.Len(t, r.host.queued(), 1)
	r.tick(time.Second)
	require.Len(t, r.host.queued(), 2)
	assert.Equal(t, 2, r.host.last(t).Index)
}

func TestAdvanceTimer_NothingRunsUntilPlayed(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(true))
	loaded := testPlaylist(3, 0, func(p *playlists.Playlist) {
		p.Advance = time.Minute
		p.Playing = false
	})
	r.host.set(mediaslot.Primary, loaded)
	r.changed()
	r.tick(2 * time.Hour)
	r.quiet()
}

func TestAdvanceTimer_NeedsBoundMedia(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	r.host.set(mediaslot.Primary, advancing(3, 0))
	r.foreign("started from the menu")
	r.tick(2 * time.Hour)
	r.quiet()
}

func TestAdvanceTimer_PauseFreezesAndResumeContinues(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	pls := advancing(3, 0)
	r.play(pls)
	r.tick(20 * time.Second)

	paused := playlists.Pause(*pls)
	r.host.set(mediaslot.Primary, paused)
	r.changed()
	r.tick(time.Hour)
	r.quiet()

	r.host.set(mediaslot.Primary, playlists.Play(*paused))
	r.changed()
	r.tick(39 * time.Second)
	r.quiet()
	r.tick(time.Second)
	assert.Equal(t, 1, r.host.last(t).Index, "the 40 seconds left when it paused ran out")
}

// Pausing a playlist whose media Core stops ends the item; resuming plays it
// again and the timer carries on from where it froze.
func TestAdvanceTimer_ResumeAfterPauseThatStoppedTheMedia(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	pls := advancing(3, 0)
	gen := r.play(pls)
	r.tick(20 * time.Second)

	paused := playlists.Pause(*pls)
	r.host.set(mediaslot.Primary, paused)
	r.eng.PlaylistChanged(mediaslot.Primary)
	r.end(gen, models.MediaStopReasonStopped)
	r.tick(time.Hour)
	r.quiet()

	r.play(playlists.Play(*paused))
	r.tick(39 * time.Second)
	r.quiet()
	r.tick(time.Second)
	assert.Equal(t, 1, r.host.last(t).Index)
}

func TestAdvanceTimer_StoppedMediaWithoutAPauseRestartsTheTimer(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	pls := advancing(3, 0)
	gen := r.play(pls)
	r.tick(20 * time.Second)
	r.end(gen, models.MediaStopReasonStopped)

	r.play(pls)
	r.tick(59 * time.Second)
	r.quiet()
	r.tick(time.Second)
	assert.Equal(t, 1, r.host.last(t).Index)
}

func TestAdvanceTimer_EarlyFinishFollowsTheFinishRulesAndRestarts(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	gen := r.play(advancing(3, 0))
	r.tick(30 * time.Second)
	r.end(gen, models.MediaStopReasonFinished)
	next := r.host.last(t)
	require.Equal(t, 1, next.Index)

	r.play(next)
	r.tick(59 * time.Second)
	assert.Len(t, r.host.queued(), 1, "the finished item's timer is gone and the new one has not run out")
	r.tick(time.Second)
	assert.Len(t, r.host.queued(), 2)
}

func TestAdvanceTimer_EndOfAPlaylistThatDoesNotRepeatClearsIt(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(true))
	r.play(testPlaylist(3, 2, func(p *playlists.Playlist) { p.Advance = time.Minute }))
	r.tick(time.Minute)
	assert.True(t, r.host.last(t).Clear)
	assert.Zero(t, r.ui.opened())
}

func TestAdvanceTimer_RepeatingPlaylistWraps(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	r.play(testPlaylist(1, 0, func(p *playlists.Playlist) {
		p.Advance = time.Minute
		p.Loop = true
	}))
	r.tick(time.Minute)
	got := r.host.last(t)
	assert.Equal(t, 0, got.Index)
	assert.True(t, got.ForceRelaunch, "the only item is relaunched so the switch is not dropped")
}

func TestAdvanceTimer_ChangedAdvanceRestartsTheTimer(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	pls := advancing(3, 0)
	r.play(pls)
	r.tick(50 * time.Second)

	longer := *pls
	longer.Advance = 2 * time.Minute
	r.host.set(mediaslot.Primary, &longer)
	r.changed()
	r.tick(time.Minute)
	r.quiet()
	r.tick(time.Minute)
	assert.Equal(t, 1, r.host.last(t).Index)
}

func TestAdvanceTimer_Countdown(t *testing.T) {
	t.Parallel()

	t.Run("expiry switches after the notice", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(true))
		r.play(advancing(3, 0))
		r.tick(time.Minute)

		require.Equal(t, 1, r.ui.opened())
		req := r.ui.request(t, 0)
		assert.Equal(t, "Switching to GameB", req.Title)
		assert.Equal(t, "Stay on this", req.Choices[0].Label)
		assert.Equal(t, "Stop rotating", req.Choices[1].Label)
		assert.Equal(t, playlists.DefaultCountdown, req.Timeout)
		r.quiet()

		r.tick(9 * time.Second)
		r.quiet()
		r.tick(time.Second)
		got := r.host.last(t)
		assert.Equal(t, 1, got.Index)
		assert.True(t, got.EngineStarted)
	})

	t.Run("stay on this restarts the full timer", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(true))
		r.play(advancing(3, 0))
		r.tick(time.Minute)
		r.ui.respond(0, selected(choiceStay))
		eventually(t, func() bool { return r.ui.dismissed(0) })
		r.eng.Sync()
		r.tick(time.Hour - time.Minute)
		// The timer ran out again once, and opened a second prompt.
		assert.Equal(t, 2, r.ui.opened())
		r.quiet()
	})

	t.Run("stay on this waits a whole timer", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(true))
		r.play(advancing(3, 0))
		r.tick(time.Minute)
		r.ui.respond(0, selected(choiceStay))
		eventually(t, func() bool { return r.ui.dismissed(0) })
		r.eng.Sync()
		r.tick(59 * time.Second)
		assert.Equal(t, 1, r.ui.opened())
		r.tick(time.Second)
		assert.Equal(t, 2, r.ui.opened())
	})

	t.Run("stop rotating keeps playing and turns the timer off", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(true))
		r.play(advancing(3, 0))
		r.tick(time.Minute)
		r.ui.respond(0, selected(choiceHalt))
		eventually(t, func() bool { return len(r.host.queued()) == 1 })

		got := r.host.last(t)
		assert.True(t, got.Reconfigure)
		assert.Zero(t, got.Advance)
		assert.Equal(t, 0, got.Index, "it stays on the current item")
		assert.True(t, got.Playing)
		r.tick(time.Hour)
		assert.Len(t, r.host.queued(), 1)
		assert.Equal(t, 1, r.ui.opened())
	})

	t.Run("a playlist update cancels the switch", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(true))
		r.play(advancing(3, 0))
		r.tick(time.Minute)
		r.eng.PlaylistChanged(mediaslot.Primary)
		r.eng.Sync()
		assert.True(t, r.ui.dismissed(0))
		r.tick(time.Minute)
		r.quiet()
	})

	t.Run("the item ending cancels the switch and follows the exit rules", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(true))
		gen := r.play(advancing(3, 0))
		r.tick(time.Minute)
		r.end(gen, models.MediaStopReasonFinished)
		assert.True(t, r.ui.dismissed(0))
		assert.Equal(t, 1, r.host.last(t).Index)
		r.tick(time.Hour)
		assert.Len(t, r.host.queued(), 1)
	})

	t.Run("input does not cancel a timer switch", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(true), withInput())
		r.play(advancing(3, 0))
		r.tick(time.Minute)
		r.eng.NoteUserActivity()
		r.eng.Sync()
		assert.False(t, r.ui.dismissed(0))
		r.tick(time10s)
		assert.Equal(t, 1, r.host.last(t).Index)
	})
}

// The loop is woken by its own timer, not only by events.
func TestAdvanceTimer_TimerWakesTheLoop(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	r.play(advancing(3, 0))
	r.clock.Advance(time.Minute)
	eventually(t, func() bool { return len(r.host.queued()) == 1 })
	assert.Equal(t, 1, r.host.last(t).Index)
}
