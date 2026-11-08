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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestItemMoved_ExitCountdownFollowsTheItem(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	pls := testPlaylist(4, 2, nil)
	gen := r.play(pls)
	r.end(gen, exited)
	require.Equal(t, 1, r.ui.opened())

	r.tick(4 * time.Second)
	shuffled := playlists.SetShuffle(pls, true)
	require.Equal(t, 0, shuffled.Index)
	r.eng.ItemMoved(pls.Slot, pls.ID, pls.Index, shuffled.Index)
	r.host.set(pls.Slot, shuffled)
	r.changed()

	require.Equal(t, 2, r.ui.opened(), "the countdown is reopened for the moved item")
	assert.True(t, r.ui.dismissed(0))
	req := r.ui.request(t, 1)
	assert.Equal(t, "Up next: "+shuffled.Items[1].Name, req.Title)
	assert.Equal(t, 6*time.Second, req.Timeout, "it keeps the time it had left")
	r.quiet()

	r.tick(6 * time.Second)
	got := r.host.last(t)
	assert.Equal(t, 1, got.Index)
	assert.Equal(t, shuffled.ID, got.ID)
	assert.True(t, got.EngineStarted)
}

func TestItemMoved_ModeChangeInPlaceKeepsTheCountdown(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	pls := testPlaylist(3, 0, nil)
	gen := r.play(pls)
	r.end(gen, exited)
	require.Equal(t, 1, r.ui.opened())

	repeat, err := playlists.SetRepeat(pls, playlists.RepeatOne)
	require.NoError(t, err)
	r.eng.ItemMoved(pls.Slot, pls.ID, pls.Index, repeat.Index)
	r.host.set(pls.Slot, repeat)
	r.changed()

	require.Equal(t, 2, r.ui.opened())
	assert.Equal(t, "Up next: GameA", r.ui.request(t, 1).Title, "repeat-one counts down to the same item")
	r.tick(playlists.DefaultCountdown)
	assert.Equal(t, 0, r.host.last(t).Index)
}

func TestPlaylistChanged_StillCancelsTheCountdownOfAReplacement(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	pls := testPlaylist(3, 0, nil)
	gen := r.play(pls)
	r.end(gen, exited)

	r.host.set(pls.Slot, playlists.Goto(*pls, 2))
	r.changed()
	assert.True(t, r.ui.dismissed(0))
	r.tick(playlists.DefaultCountdown)
	r.quiet()
}

func TestItemRemoved_NothingStaysBoundToTheReplacement(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	pls := testPlaylist(4, 3, func(p *playlists.Playlist) { p.Loop = true })
	gen := r.play(pls)

	// The playing item left the deck; the clamped index names another item.
	replacement := *pls
	replacement.Items = pls.Items[:3]
	replacement.Index = 2
	r.eng.ItemRemoved(pls.Slot, pls.ID, pls.Index)
	r.host.set(pls.Slot, &replacement)
	r.changed()

	r.end(gen, models.MediaStopReasonFinished)
	assert.Empty(t, r.host.queued(), "the old media does not advance the replacement")
}

func TestReconcileMove(t *testing.T) {
	t.Parallel()

	from := testPlaylist(3, 1, func(p *playlists.Playlist) { p.Loop = true })
	move := playlists.Next(*from)
	move.Playing = true
	move.EngineStarted = true
	move.EngineFromIndex = 1
	move.StopSeq = 7
	move.StopStamped = true

	same := ReconcileMove(from, move)
	assert.Same(t, move, same, "a move decided under the current mode stands")

	repeatOne, err := playlists.SetRepeat(from, playlists.RepeatOne)
	require.NoError(t, err)
	got := ReconcileMove(repeatOne, move)
	assert.Equal(t, 1, got.Index, "repeat-one repeats the current item")
	assert.True(t, got.LoopOne)
	assert.True(t, got.ForceRelaunch)
	assert.True(t, got.EngineStarted)
	assert.Equal(t, 1, got.EngineFromIndex)
	assert.Equal(t, uint64(7), got.StopSeq)
	assert.True(t, got.StopStamped)

	none, err := playlists.SetRepeat(playlists.Goto(*from, 2), playlists.RepeatNone)
	require.NoError(t, err)
	finish := ReconcileMove(none, move)
	assert.True(t, finish.Clear)
	assert.True(t, finish.Completed)
	assert.True(t, finish.EngineStarted)

	stop := &playlists.Playlist{ID: "p", Clear: true, EngineStarted: true}
	assert.Same(t, stop, ReconcileMove(repeatOne, stop), "a stop the user chose is not turned into a move")
}
