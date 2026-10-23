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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinished_Advances(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pls       *playlists.Playlist
		name      string
		wantIndex int
		wantClear bool
		wantForce bool
	}{
		{name: "next item", pls: testPlaylist(3, 0, nil), wantIndex: 1},
		{
			name: "repeat all wraps", wantIndex: 0,
			pls: testPlaylist(3, 2, func(p *playlists.Playlist) { p.Loop = true }),
		},
		{
			name: "single item loop relaunches", wantIndex: 0, wantForce: true,
			pls: testPlaylist(1, 0, func(p *playlists.Playlist) { p.Loop = true }),
		},
		{
			name: "repeat one relaunches", wantIndex: 1, wantForce: true,
			pls: testPlaylist(3, 1, func(p *playlists.Playlist) { p.LoopOne = true }),
		},
		{name: "end of list clears the playlist", pls: testPlaylist(3, 2, nil), wantClear: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			gen := r.play(tt.pls)
			r.end(gen, models.MediaStopReasonFinished)

			got := r.host.last(t)
			require.Len(t, r.host.queued(), 1)
			if tt.wantClear {
				assert.True(t, got.Clear)
				assert.True(t, got.Completed, "a playlist that played to its end starts over next time")
				assert.Equal(t, mediaslot.Primary, got.Slot)
				return
			}
			assert.False(t, got.Completed)
			assert.Equal(t, tt.wantIndex, got.Index)
			assert.True(t, got.Playing)
			assert.Equal(t, tt.wantForce, got.ForceRelaunch)
			assert.True(t, got.EngineStarted, "an automatic advance is marked as the engine's")
		})
	}
}

func TestFinished_IdlePlaylistStillAdvances(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput())
	gen := r.play(testPlaylist(3, 0, func(p *playlists.Playlist) { p.Idle = 5 * time.Minute }))
	r.end(gen, models.MediaStopReasonFinished)

	assert.Equal(t, 1, r.host.last(t).Index)
}

func TestEnded_GuardsStopAnAdvance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		change func(r *rig, pls *playlists.Playlist)
		name   string
	}{
		{name: "playlist moved to another item", change: func(r *rig, p *playlists.Playlist) {
			r.host.set(mediaslot.Primary, playlists.Goto(*p, 2))
		}},
		{name: "a different playlist is open", change: func(r *rig, p *playlists.Playlist) {
			other := *p
			other.ID = "other"
			r.host.set(mediaslot.Primary, &other)
		}},
		{name: "playlist paused", change: func(r *rig, p *playlists.Playlist) {
			r.host.set(mediaslot.Primary, playlists.Pause(*p))
		}},
		{name: "playlist paused by a failure", change: func(r *rig, p *playlists.Playlist) {
			paused := playlists.Pause(*p)
			paused.PausedByFailure = true
			r.host.set(mediaslot.Primary, paused)
		}},
		{name: "playlist closed", change: func(r *rig, _ *playlists.Playlist) {
			r.host.set(mediaslot.Primary, nil)
		}},
	}
	for _, reason := range []string{models.MediaStopReasonFinished, models.MediaStopReasonExited} {
		for _, tt := range tests {
			t.Run(reason+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				r := newRig(t, withUI(false))
				pls := testPlaylist(3, 0, nil)
				gen := r.play(pls)
				tt.change(r, pls)
				r.end(gen, reason)
				r.tick(time10s)
				r.quiet()
				assert.Zero(t, r.ui.opened(), "no countdown opens for a move that is no longer wanted")
			})
		}
	}
}

func TestEnded_NeverAdvancesFromMediaThePlaylistDidNotLaunch(t *testing.T) {
	t.Parallel()

	for _, reason := range []string{models.MediaStopReasonFinished, models.MediaStopReasonExited} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, withUI(false))
			r.host.set(mediaslot.Primary, testPlaylist(3, 0, nil))
			gen := r.foreign("menu-game")
			r.end(gen, reason)
			r.tick(time10s)
			r.quiet()
			assert.Zero(t, r.ui.opened())
		})
	}
}

func TestEnded_StoppedAndReplacedNeverAdvance(t *testing.T) {
	t.Parallel()

	for _, reason := range []string{models.MediaStopReasonStopped, models.MediaStopReasonReplaced} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, withUI(false))
			gen := r.play(testPlaylist(3, 0, nil))
			r.end(gen, reason)
			r.tick(time10s)
			r.quiet()
			assert.Zero(t, r.ui.opened())
		})
	}
}

// A replaced item is unbound, so the media that replaced it is not mistaken
// for it and its own end is not the item's.
func TestEnded_ReplacementMediaIsNotBound(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	first := r.play(testPlaylist(3, 0, nil))
	r.end(first, models.MediaStopReasonReplaced)
	second := r.foreign("something-else")
	r.end(second, models.MediaStopReasonFinished)
	r.quiet()
}

func TestEnded_StaleGenerationIsIgnored(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	gen := r.play(testPlaylist(3, 0, nil))
	r.end(gen+100, models.MediaStopReasonFinished)
	r.quiet()
}

func TestBackground_ExitedNeverAdvances(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false))
	pls := testPlaylist(3, 0, func(p *playlists.Playlist) { p.Slot = mediaslot.Background })
	r.host.set(mediaslot.Background, pls)
	media := r.media("track")
	media.Slot = mediaslot.Background
	r.eng.ItemLaunching(pls)
	r.eng.MediaStarted(state.MediaStart{Slot: mediaslot.Background, Media: media})
	r.eng.ItemLaunched(pls, nil, false)
	r.eng.MediaEnded(state.MediaEnd{Slot: mediaslot.Background, Media: media, Reason: models.MediaStopReasonExited})
	r.tick(time10s)
	r.quiet()
	assert.Zero(t, r.ui.opened())
}

func TestBackground_FinishedAdvancesBoundTrack(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	pls := testPlaylist(3, 0, func(p *playlists.Playlist) { p.Slot = mediaslot.Background })
	r.host.set(mediaslot.Background, pls)
	media := r.media("track")
	media.Slot = mediaslot.Background
	r.eng.ItemLaunching(pls)
	r.eng.MediaStarted(state.MediaStart{Slot: mediaslot.Background, Media: media})
	r.eng.ItemLaunched(pls, nil, false)
	r.eng.MediaEnded(state.MediaEnd{Slot: mediaslot.Background, Media: media, Reason: models.MediaStopReasonFinished})
	r.eng.Sync()

	got := r.host.last(t)
	assert.Equal(t, 1, got.Index)
	assert.Equal(t, mediaslot.Background, got.Slot)
}
