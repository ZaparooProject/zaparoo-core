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

const exited = models.MediaStopReasonExited

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	require.Eventually(t, cond, 5*time.Second, time.Millisecond)
}

func TestExited_CountdownThenAdvance(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	gen := r.play(testPlaylist(3, 0, nil))
	r.end(gen, exited)

	require.Equal(t, 1, r.ui.opened())
	req := r.ui.request(t, 0)
	assert.Equal(t, models.UIEventKindPicker, req.Kind)
	assert.Equal(t, "Up next: GameB", req.Title)
	assert.Equal(t, playlists.DefaultCountdown, req.Timeout)
	require.Len(t, req.Choices, 2)
	assert.Equal(t, "Play now", req.Choices[0].Label)
	assert.Equal(t, "Stop playlist", req.Choices[1].Label)
	r.quiet()

	r.tick(9 * time.Second)
	r.quiet()
	r.tick(time.Second)

	got := r.host.last(t)
	assert.Equal(t, 1, got.Index)
	assert.True(t, got.Playing)
	assert.True(t, got.EngineStarted)
}

func TestExited_CustomCountdown(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	gen := r.play(testPlaylist(3, 0, func(p *playlists.Playlist) {
		p.Countdown = 3 * time.Second
		p.CountdownSet = true
	}))
	r.end(gen, exited)
	assert.Equal(t, 3*time.Second, r.ui.request(t, 0).Timeout)
	r.tick(2 * time.Second)
	r.quiet()
	r.tick(time.Second)
	assert.Equal(t, 1, r.host.last(t).Index)
}

func TestExited_ZeroCountdownAdvancesAtOnce(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	gen := r.play(testPlaylist(3, 0, func(p *playlists.Playlist) { p.CountdownSet = true }))
	r.end(gen, exited)

	assert.Equal(t, 1, r.host.last(t).Index)
	assert.Zero(t, r.ui.opened(), "no countdown is shown when it is zero")
}

func TestExited_WorksWithoutAUI(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	gen := r.play(testPlaylist(3, 0, nil))
	r.end(gen, exited)
	r.tick(9 * time.Second)
	r.quiet()
	r.tick(time.Second)
	assert.Equal(t, 1, r.host.last(t).Index)
}

func TestExited_RepeatOneCountsDownToTheSameItem(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	gen := r.play(testPlaylist(3, 1, func(p *playlists.Playlist) { p.LoopOne = true }))
	r.end(gen, exited)
	assert.Equal(t, "Up next: GameB", r.ui.request(t, 0).Title)
	r.tick(time10s)
	got := r.host.last(t)
	assert.Equal(t, 1, got.Index)
	assert.True(t, got.ForceRelaunch)
}

func TestExited_UIChoices(t *testing.T) {
	t.Parallel()

	t.Run("play now advances at once", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(false))
		gen := r.play(testPlaylist(3, 0, nil))
		r.end(gen, exited)
		r.ui.respond(0, selected(choicePlay))
		eventually(t, func() bool { return len(r.host.queued()) == 1 })
		got := r.host.last(t)
		assert.Equal(t, 1, got.Index)
		assert.True(t, got.EngineStarted)
		r.tick(time.Minute)
		assert.Len(t, r.host.queued(), 1, "the expired countdown does not advance again")
	})

	t.Run("stop playlist clears it", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(false))
		gen := r.play(testPlaylist(3, 0, nil))
		r.end(gen, exited)
		r.ui.respond(0, selected(choiceStop))
		eventually(t, func() bool { return len(r.host.queued()) == 1 })
		assert.True(t, r.host.last(t).Clear)
		assert.False(t, r.host.last(t).Completed, "stopping keeps the playlist's place")
		r.tick(time.Minute)
		assert.Len(t, r.host.queued(), 1)
	})

	t.Run("a timed out prompt is an expiry", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(false))
		gen := r.play(testPlaylist(3, 0, nil))
		r.end(gen, exited)
		r.ui.respond(0, uievents.Result{Resolution: models.UIResolution{Outcome: models.UIOutcomeTimedOut}})
		eventually(t, func() bool { return len(r.host.queued()) == 1 })
		assert.Equal(t, 1, r.host.last(t).Index)
	})

	t.Run("dismissing the prompt cancels", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(false))
		gen := r.play(testPlaylist(3, 0, nil))
		r.end(gen, exited)
		r.ui.respond(0, uievents.Result{Resolution: models.UIResolution{Outcome: models.UIOutcomeDismissed}})
		eventually(t, func() bool { return r.ui.dismissed(0) })
		r.tick(time.Minute)
		r.quiet()
	})

	t.Run("a cancelled prompt does not cancel the countdown", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withUI(false))
		gen := r.play(testPlaylist(3, 0, nil))
		r.end(gen, exited)
		r.ui.respond(0, uievents.Result{Resolution: models.UIResolution{Outcome: models.UIOutcomeCancelled}})
		r.tick(time10s)
		assert.Equal(t, 1, r.host.last(t).Index)
	})
}

func TestExited_Cancels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cancel func(r *rig)
		name   string
	}{
		{name: "a scan", cancel: func(r *rig) {
			r.tick(time.Second)
			r.eng.NoteUserActivity()
		}},
		{name: "media starting", cancel: func(r *rig) {
			r.foreign("another")
		}},
		{name: "a playlist update", cancel: func(r *rig) {
			r.eng.PlaylistChanged(mediaslot.Primary)
		}},
		{name: "input after the ignore window", cancel: func(r *rig) {
			w := r.input.nextWatch(r.t)
			assert.True(r.t, w.responsive, "a countdown watches for input responsively")
			r.tick(2 * time.Second)
			w.activity()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, withUI(false), withInput())
			gen := r.play(testPlaylist(3, 0, nil))
			r.end(gen, exited)
			tt.cancel(r)
			r.eng.Sync()
			assert.True(t, r.ui.dismissed(0), "the prompt is dismissed")
			r.tick(time.Minute)
			r.quiet()
		})
	}
}

func TestExited_InputInsideTheIgnoreWindowIsIgnored(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false), withInput())
	gen := r.play(testPlaylist(3, 0, nil))
	r.end(gen, exited)
	w := r.input.nextWatch(t)
	r.tick(time.Second)
	w.activity()
	r.eng.Sync()
	assert.False(t, r.ui.dismissed(0))
	r.tick(9 * time.Second)
	assert.Equal(t, 1, r.host.last(t).Index, "the countdown still ran out")
}

func TestExited_InputWatchStopsWithTheCountdown(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false), withInput())
	gen := r.play(testPlaylist(3, 0, nil))
	r.end(gen, exited)
	w := r.input.nextWatch(t)
	require.NoError(t, w.ctx.Err())
	r.tick(time10s)
	require.Error(t, w.ctx.Err(), "the watch is cancelled once the countdown is over")
	assert.Empty(t, r.input.started, "nothing watches input while no countdown is open")
}

func TestExited_NoInputSourceMeansNoInputCancel(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	gen := r.play(testPlaylist(3, 0, nil))
	r.end(gen, exited)
	r.tick(5 * time.Second)
	r.quiet()
	assert.False(t, r.ui.dismissed(0))
}

func TestExited_ShutdownCancels(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false), withInput())
	gen := r.play(testPlaylist(3, 0, nil))
	r.end(gen, exited)
	w := r.input.nextWatch(t)

	r.eng.Stop()
	assert.True(t, r.ui.dismissed(0))
	require.Error(t, w.ctx.Err())
	r.quiet()
}

func TestExited_QuickQuitPausesAutoStartedItem(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	pls := engineStarted(testPlaylist(3, 1, nil))
	gen := r.play(pls)
	r.tick(29 * time.Second)
	r.end(gen, exited)

	assert.Equal(t, 1, r.host.pausedCount(), "the playlist is paused on the item")
	assert.Zero(t, r.ui.opened(), "no countdown follows a quick quit")
	r.tick(time.Minute)
	r.quiet()
	assert.True(t, r.host.ActivePlaylist().PausedByFailure)
	assert.Equal(t, 1, r.host.ActivePlaylist().Index)
}

func TestExited_QuickQuitDoesNotApply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pls  *playlists.Playlist
		name string
		wait time.Duration
	}{
		{name: "an item the user started", pls: testPlaylist(3, 0, nil), wait: 5 * time.Second},
		{
			name: "an auto-started item that ran long enough", wait: 30 * time.Second,
			pls: engineStarted(testPlaylist(3, 0, nil)),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, withUI(false))
			gen := r.play(tt.pls)
			r.tick(tt.wait)
			r.end(gen, exited)
			assert.Zero(t, r.host.pausedCount())
			assert.Equal(t, 1, r.ui.opened(), "the countdown runs")
		})
	}
}

func TestExited_LastItemClearsWithoutCountdown(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	gen := r.play(engineStarted(testPlaylist(3, 2, nil)))
	r.end(gen, exited)

	assert.True(t, r.host.last(t).Clear)
	assert.True(t, r.host.last(t).Completed)
	assert.Zero(t, r.ui.opened())
	assert.Zero(t, r.host.pausedCount(), "nothing is left to pause")
}

func TestExited_LastItemOfARepeatingPlaylistCountsDown(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	gen := r.play(testPlaylist(3, 2, func(p *playlists.Playlist) { p.Loop = true }))
	r.end(gen, exited)
	assert.Equal(t, "Up next: GameA", r.ui.request(t, 0).Title)
}

func TestExited_IdlePlaylistHasNoCountdownAndNoAdvance(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false), withInput())
	gen := r.play(testPlaylist(3, 0, func(p *playlists.Playlist) { p.Idle = 5 * time.Minute }))
	r.end(gen, exited)
	assert.Zero(t, r.ui.opened())
	r.quiet()
}

func TestExited_PlaylistUpdateBeforeTheExitIsHarmless(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	pls := testPlaylist(3, 0, nil)
	gen := r.play(pls)
	r.eng.PlaylistChanged(mediaslot.Primary)
	r.eng.Sync()
	r.end(gen, exited)
	require.Equal(t, 1, r.ui.opened())
}
