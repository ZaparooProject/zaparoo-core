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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// neverStarted ends media the way a launch that never started does.
func (r *rig) neverStarted(gen uint64) {
	r.t.Helper()
	r.eng.MediaEnded(state.MediaEnd{Slot: mediaslot.Primary, Gen: gen, Reason: exited, NeverStarted: true})
	r.eng.Sync()
}

func choiceLabels(req *uievents.Request) []string {
	labels := make([]string, 0, len(req.Choices))
	for _, c := range req.Choices {
		labels = append(labels, c.Label)
	}
	return labels
}

// quickQuit plays item index of an auto-started three-item playlist, quits it
// at once and lets the engine see the pause it made.
func quickQuit(t *testing.T, index int) *rig {
	t.Helper()
	r := newRig(t, withUI(false))
	gen := r.play(engineStarted(testPlaylist(3, index, nil)))
	r.tick(5 * time.Second)
	r.end(gen, exited)
	r.changed()
	require.Equal(t, 1, r.ui.opened())
	return r
}

func TestPaused_QuickQuitPromptWaitsForAnAnswer(t *testing.T) {
	t.Parallel()

	r := quickQuit(t, 0)
	req := r.ui.request(t, 0)
	assert.Equal(t, models.UIEventKindPicker, req.Kind)
	assert.Equal(t, "Playlist paused", req.Title)
	assert.Equal(t, []string{"Play next: GameB", "Stop playlist"}, choiceLabels(req))
	assert.Zero(t, req.Timeout, "the prompt does not run out")
	assert.True(t, req.Dismissible)
	assert.Equal(t, "Playlist paused. Scan the card again to continue", req.HostNotice)

	r.tick(time.Hour)
	assert.False(t, r.ui.dismissed(0), "the pause it announced does not close the prompt")
	r.quiet()
}

func TestPaused_PlayNextResumesOnTheNextItem(t *testing.T) {
	t.Parallel()

	r := quickQuit(t, 0)
	r.ui.respond(0, selected(choicePlay))
	eventually(t, func() bool { return len(r.host.queued()) == 1 })

	upd := r.host.last(t)
	assert.Equal(t, 1, upd.Index)
	assert.True(t, upd.Playing)
	assert.False(t, upd.PausedByFailure)
	assert.False(t, upd.EngineStarted, "a paused playlist drops the engine's own moves")
}

func TestPaused_StopClosesThePlaylist(t *testing.T) {
	t.Parallel()

	r := quickQuit(t, 0)
	r.ui.respond(0, selected(choiceStop))
	eventually(t, func() bool { return len(r.host.queued()) == 1 })

	upd := r.host.last(t)
	assert.True(t, upd.Clear)
	assert.Equal(t, "p", upd.ID)
	assert.False(t, upd.EngineStarted)
}

func TestPaused_DismissLeavesItPaused(t *testing.T) {
	t.Parallel()

	r := quickQuit(t, 0)
	r.ui.respond(0, uievents.Result{Resolution: models.UIResolution{Outcome: models.UIOutcomeDismissed}})
	eventually(t, func() bool { return r.ui.dismissed(0) })
	r.eng.Sync()
	r.quiet()
	assert.True(t, r.host.ActivePlaylist().PausedByFailure)
}

func TestPaused_PromptClosesWhenThePlaylistPlaysAgain(t *testing.T) {
	t.Parallel()

	r := quickQuit(t, 0)
	r.host.set(mediaslot.Primary, playlists.Play(*r.host.ActivePlaylist()))
	r.changed()
	assert.True(t, r.ui.dismissed(0))
}

func TestPaused_StaleAnswerDoesNothing(t *testing.T) {
	t.Parallel()

	r := quickQuit(t, 0)
	// The person moved the playlist on some other way before answering.
	r.host.set(mediaslot.Primary, playlists.Next(*r.host.ActivePlaylist()))
	r.ui.respond(0, selected(choicePlay))
	eventually(t, func() bool { return r.ui.dismissed(0) })
	r.quiet()
}

func TestPaused_NoPromptWhenAnotherEventIsOpen(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	r.ui.setOthers(models.UIEvent{ID: "confirm"})
	gen := r.play(engineStarted(testPlaylist(3, 0, nil)))
	r.end(gen, exited)
	assert.Equal(t, 1, r.host.pausedCount())
	assert.Zero(t, r.ui.opened())
}

func TestFailed_NeverStartedPausesWithAPrompt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pls     *playlists.Playlist
		name    string
		choices []string
	}{
		{
			name: "an item the engine started", pls: engineStarted(testPlaylist(3, 1, nil)),
			choices: []string{"Try again", "Skip", "Stop playlist"},
		},
		{
			name: "an item the user started", pls: testPlaylist(3, 1, nil),
			choices: []string{"Try again", "Skip", "Stop playlist"},
		},
		{
			name: "the last item", pls: testPlaylist(3, 2, nil),
			choices: []string{"Try again", "Stop playlist"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, withUI(false))
			gen := r.play(tt.pls)
			r.tick(time.Minute)
			r.neverStarted(gen)
			r.changed()

			assert.Equal(t, 1, r.host.pausedCount(), "the playlist waits on the item")
			require.Equal(t, 1, r.ui.opened(), "no countdown to the next item")
			req := r.ui.request(t, 0)
			name := tt.pls.Current().Name
			assert.Equal(t, "Couldn't start "+name, req.Title)
			assert.Equal(t, tt.choices, choiceLabels(req))
			assert.Equal(t, "Couldn't start "+name, req.HostNotice)
			assert.Zero(t, req.Timeout)
			r.tick(time.Hour)
			r.quiet()
			assert.False(t, r.ui.dismissed(0))
		})
	}
}

func TestFailed_TryAgainRelaunchesTheItem(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	gen := r.play(testPlaylist(3, 1, nil))
	r.neverStarted(gen)
	r.changed()
	r.ui.respond(0, selected(choiceRetry))
	eventually(t, func() bool { return len(r.host.queued()) == 1 })

	upd := r.host.last(t)
	assert.Equal(t, 1, upd.Index)
	assert.True(t, upd.Playing)
	assert.True(t, upd.ForceRelaunch)
	assert.False(t, upd.PausedByFailure)
}

func TestFailed_SkipPlaysTheNextItem(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	gen := r.play(testPlaylist(3, 1, nil))
	r.neverStarted(gen)
	r.changed()
	r.ui.respond(0, selected(choicePlay))
	eventually(t, func() bool { return len(r.host.queued()) == 1 })

	upd := r.host.last(t)
	assert.Equal(t, 2, upd.Index)
	assert.True(t, upd.Playing)
}

func TestFailed_HandedOffMediaThatNeverStarted(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false), withPublishes(func(string) bool { return true }))
	pls := testPlaylist(3, 0, nil)
	r.host.set(pls.Slot, pls)
	r.eng.PlaylistChanged(pls.Slot)
	r.eng.ItemLaunching(pls)
	r.eng.ItemLaunched(pls, nil, true)
	r.eng.Sync()
	gen := r.foreign("gamea")
	r.neverStarted(gen)
	// Nothing took over from the media that went away.
	r.tick(time.Minute)
	r.changed()

	assert.Equal(t, 1, r.host.pausedCount())
	require.Equal(t, 1, r.ui.opened())
	assert.Equal(t, "Couldn't start GameA", r.ui.request(t, 0).Title)
}

func TestExited_CountdownNotice(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	gen := r.play(testPlaylist(3, 0, nil))
	r.tick(time.Minute)
	r.end(gen, exited)
	require.Equal(t, 1, r.ui.opened())
	assert.Equal(t, "Up next: GameB in 10s", r.ui.request(t, 0).HostNotice)
}
