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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const idleFor = 5 * time.Minute

func idlePlaylist(items, index int, mutate func(*playlists.Playlist)) *playlists.Playlist {
	return testPlaylist(items, index, func(p *playlists.Playlist) {
		p.Idle = idleFor
		if mutate != nil {
			mutate(p)
		}
	})
}

func armedPlaylist() *playlists.Playlist {
	return idlePlaylist(3, 0, func(p *playlists.Playlist) {
		p.Playing = false
		p.Armed = true
	})
}

// load installs pls without playing it, as playlist.load does.
func (r *rig) load(pls *playlists.Playlist) {
	r.t.Helper()
	r.host.set(pls.Slot, pls)
	r.eng.PlaylistChanged(pls.Slot)
	r.eng.Sync()
}

func TestIdle_ArmedPlaylistStartsAfterTheIdleTime(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput())
	r.load(armedPlaylist())
	w := r.input.nextWatch(t)
	assert.False(t, w.responsive, "waiting for idle keeps the cheap watch")

	r.tick(idleFor - time.Second)
	r.quiet()
	r.tick(time.Second)

	got := r.host.last(t)
	assert.True(t, got.Playing)
	assert.False(t, got.Armed, "starting disarms it")
	assert.Equal(t, 0, got.Index)
	assert.True(t, got.EngineStarted)
}

func TestIdle_ActivityResetsTheTimer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		activity func(w *fakeWatch, r *rig)
		name     string
	}{
		{name: "input", activity: func(w *fakeWatch, _ *rig) { w.activity() }},
		{name: "a scan", activity: func(_ *fakeWatch, r *rig) { r.eng.NoteUserActivity() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, withInput())
			r.load(armedPlaylist())
			w := r.input.nextWatch(t)

			r.tick(4 * time.Minute)
			tt.activity(w, r)
			r.eng.Sync()
			r.tick(4 * time.Minute)
			r.quiet()
			r.tick(time.Minute)
			assert.True(t, r.host.last(t).Playing)
		})
	}
}

func TestIdle_ArmedPlaylistWaitsForNothingToBeRunning(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput())
	r.load(armedPlaylist())
	gen := r.foreign("menu game")
	r.tick(time.Hour)
	r.quiet()

	r.end(gen, models.MediaStopReasonStopped)
	r.tick(idleFor)
	assert.True(t, r.host.last(t).Playing, "once the game is gone the idle time is counted")
}

func TestIdle_WatchOnlyRunsWhileAnIdlePlaylistExists(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput())
	r.eng.Sync()
	assert.Empty(t, r.input.started, "no idle playlist, nothing watched")

	r.load(armedPlaylist())
	w := r.input.nextWatch(t)
	r.load(nil2())
	require.Error(t, w.ctx.Err(), "the watch stops when the playlist goes away")
}

func nil2() *playlists.Playlist {
	return testPlaylist(3, 0, func(p *playlists.Playlist) { p.Playing = false })
}

func TestIdle_PausedPlaylistIsDisarmed(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput())
	armed := armedPlaylist()
	r.load(armed)
	r.load(playlists.Pause(*armed))
	r.tick(time.Hour)
	r.quiet()
}

func TestIdle_NoInputSourceMeansNothingRuns(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.load(armedPlaylist())
	r.tick(time.Hour)
	r.quiet()
}

func TestIdle_PlayingWithNothingRunningMovesOn(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput())
	gen := r.play(idlePlaylist(3, 0, nil))
	r.end(gen, models.MediaStopReasonExited)
	r.input.nextWatch(t)

	r.tick(idleFor - time.Second)
	r.quiet()
	r.tick(time.Second)
	got := r.host.last(t)
	assert.Equal(t, 1, got.Index)
	assert.True(t, got.EngineStarted)
}

func TestIdle_EndOfAPlaylistThatDoesNotRepeatClearsIt(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput())
	gen := r.play(idlePlaylist(3, 2, nil))
	r.end(gen, models.MediaStopReasonExited)
	r.tick(idleFor)
	assert.True(t, r.host.last(t).Clear)
}

func TestIdle_ActivityDelaysMovingOn(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput())
	gen := r.play(idlePlaylist(3, 0, nil))
	w := r.input.nextWatch(t)
	r.end(gen, models.MediaStopReasonExited)
	r.tick(4 * time.Minute)
	w.activity()
	r.eng.Sync()
	r.tick(4 * time.Minute)
	r.quiet()
	r.tick(time.Minute)
	assert.Equal(t, 1, r.host.last(t).Index)
}

func TestIdle_RotatesItsOwnMedia(t *testing.T) {
	t.Parallel()

	t.Run("silently without a renderer", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withInput(), withUI(false))
		r.play(idlePlaylist(3, 0, nil))
		r.input.nextWatch(t)
		r.tick(idleFor - time.Second)
		r.quiet()
		r.tick(time.Second)
		got := r.host.last(t)
		assert.Equal(t, 1, got.Index)
		assert.True(t, got.EngineStarted)
		assert.Zero(t, r.ui.opened())
	})

	t.Run("after a countdown with a renderer", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withInput(), withUI(true))
		r.play(idlePlaylist(3, 0, nil))
		r.input.nextWatch(t)
		r.tick(idleFor)

		require.Equal(t, 1, r.ui.opened())
		assert.Equal(t, "Switching to GameB", r.ui.request(t, 0).Title)
		r.quiet()
		r.tick(time10s)
		assert.Equal(t, 1, r.host.last(t).Index)
	})

	t.Run("input during the countdown cancels it and resets the timer", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withInput(), withUI(true))
		r.play(idlePlaylist(3, 0, nil))
		first := r.input.nextWatch(t)
		r.tick(idleFor)
		require.Equal(t, 1, r.ui.opened())

		second := r.input.nextWatch(t)
		assert.True(t, second.responsive, "a countdown watches responsively")
		require.Error(t, first.ctx.Err())
		second.activity()
		r.eng.Sync()
		assert.True(t, r.ui.dismissed(0))
		r.tick(time10s)
		r.quiet()

		third := r.input.nextWatch(t)
		assert.False(t, third.responsive, "back to the cheap watch")
		r.tick(idleFor - time10s - time.Second)
		assert.Equal(t, 1, r.ui.opened())
		r.tick(time.Second + time10s)
		assert.Equal(t, 2, r.ui.opened(), "idle again for the full time opens the next countdown")
	})

	t.Run("stay on this holds the game", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withInput(), withUI(true))
		r.play(idlePlaylist(3, 0, nil))
		r.input.nextWatch(t)
		r.tick(idleFor)
		r.ui.respond(0, selected(choiceStay))
		eventually(t, func() bool { return r.ui.dismissed(0) })
		r.eng.Sync()
		r.tick(idleFor - time.Second)
		assert.Equal(t, 1, r.ui.opened())
		r.quiet()
	})

	t.Run("stop rotating turns idle off", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, withInput(), withUI(true))
		r.play(idlePlaylist(3, 0, nil))
		r.input.nextWatch(t)
		r.tick(idleFor)
		r.ui.respond(0, selected(choiceHalt))
		eventually(t, func() bool { return len(r.host.queued()) == 1 })
		got := r.host.last(t)
		assert.True(t, got.Reconfigure)
		assert.Zero(t, got.Idle)
		assert.Zero(t, got.Advance)
	})
}

func TestIdle_NeverRotatesAwayFromMediaItDidNotLaunch(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput(), withUI(true))
	r.host.set(mediaslot.Primary, idlePlaylist(3, 0, nil))
	r.eng.PlaylistChanged(mediaslot.Primary)
	r.foreign("started from the menu")
	r.tick(24 * time.Hour)
	r.quiet()
	assert.Zero(t, r.ui.opened())
}

func TestIdle_DoesNotRotateAfterThePlaylistMovedOn(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput(), withUI(false))
	pls := idlePlaylist(3, 0, nil)
	r.play(pls)
	r.host.set(mediaslot.Primary, playlists.Goto(*pls, 2))
	r.changed()
	r.tick(time.Hour)
	r.quiet()
}

func TestIdle_UnsupportedPartialCoverageStillRunsWhatTheCommandAllowed(t *testing.T) {
	t.Parallel()

	assert.True(t, IdleSupported(inputPlatform{coverage: platforms.InputAll}))
	assert.False(t, IdleSupported(inputPlatform{coverage: platforms.InputKeyboard | platforms.InputMouse}))
	assert.False(t, IdleSupported(nil))
	assert.False(t, IdleSupported(plainPlatform{}))
}
