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

package playlists_test

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
)

func TestNewPlaylist_DefaultSlot(t *testing.T) {
	t.Parallel()
	p := playlists.NewPlaylist("id", "name", nil)
	assert.Equal(t, mediaslot.Primary, p.Slot)
}

func TestTransitions_PreserveSlot(t *testing.T) {
	t.Parallel()

	items := []playlists.PlaylistItem{
		{ZapScript: "a"},
		{ZapScript: "b"},
		{ZapScript: "c"},
	}

	// Use a non-default slot to prove it survives every transition.
	p := playlists.NewPlaylist("id", "name", items)
	p.Slot = mediaslot.Background

	t.Run("Next", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, mediaslot.Background, playlists.Next(*p).Slot)
	})
	t.Run("Previous", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, mediaslot.Background, playlists.Previous(*p).Slot)
	})
	t.Run("Goto", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, mediaslot.Background, playlists.Goto(*p, 1).Slot)
	})
	t.Run("Play", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, mediaslot.Background, playlists.Play(*p).Slot)
	})
	t.Run("Pause", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, mediaslot.Background, playlists.Pause(*p).Slot)
	})
}

func TestTransitions_PreserveUnsafe(t *testing.T) {
	t.Parallel()

	items := []playlists.PlaylistItem{{ZapScript: "a"}, {ZapScript: "b"}}
	p := playlists.NewPlaylist("id", "name", items)
	assert.False(t, p.Unsafe, "a playlist is trusted unless its source is not")
	p.Unsafe = true

	assert.True(t, playlists.Next(*p).Unsafe)
	assert.True(t, playlists.Previous(*p).Unsafe)
	assert.True(t, playlists.Goto(*p, 1).Unsafe)
	assert.True(t, playlists.Play(*p).Unsafe)
	assert.True(t, playlists.Pause(*p).Unsafe)
}

// TestTransitions_PreserveCommandPolicy pins that the bound survives every
// move through a playlist. Each track re-enters as its own token carrying it,
// so a transition that dropped it would let an item of a bounded playlist run
// commands the script that opened the playlist could not.
func TestTransitions_PreserveCommandPolicy(t *testing.T) {
	t.Parallel()

	items := []playlists.PlaylistItem{{ZapScript: "a"}, {ZapScript: "b"}}
	p := playlists.NewPlaylist("id", "name", items)
	assert.True(t, p.AllowedCommands.Unrestricted(), "a playlist is unbounded unless its source is")
	p.AllowedCommands = tokens.NewCommandPolicy("launch")

	for name, moved := range map[string]*playlists.Playlist{
		"next":     playlists.Next(*p),
		"previous": playlists.Previous(*p),
		"goto":     playlists.Goto(*p, 1),
		"play":     playlists.Play(*p),
		"pause":    playlists.Pause(*p),
	} {
		assert.True(t, moved.AllowedCommands.Allows("launch"), name)
		assert.False(t, moved.AllowedCommands.Allows("execute"), name)
	}
}

// TestTransitions_DropQueueSignals pins that the flags describing one update
// to the queue handler do not survive a transition. An active playlist is
// stored with them set, so carrying them would relaunch a track or clear the
// slot on the next move.
func TestTransitions_DropQueueSignals(t *testing.T) {
	t.Parallel()

	p := playlists.NewPlaylist("id", "name", []playlists.PlaylistItem{{ZapScript: "a"}, {ZapScript: "b"}})
	p.Clear = true
	p.ForceRelaunch = true
	p.Refresh = true

	for name, got := range map[string]*playlists.Playlist{
		"Next":     playlists.Next(*p),
		"Previous": playlists.Previous(*p),
		"Goto":     playlists.Goto(*p, 1),
		"Play":     playlists.Play(*p),
		"Pause":    playlists.Pause(*p),
	} {
		assert.False(t, got.Clear, "%s carried Clear", name)
		assert.False(t, got.ForceRelaunch, "%s carried ForceRelaunch", name)
		assert.False(t, got.Refresh, "%s carried Refresh", name)
	}
}

// TestTransitions_CarryEveryOtherField pins that a transition copies whole
// playlists rather than a hand-written field list, so a field added later —
// Unsafe, which decides whether items run with input and program rights —
// cannot be dropped by a copy that forgot it.
func TestTransitions_CarryEveryOtherField(t *testing.T) {
	t.Parallel()

	p := &playlists.Playlist{
		HoldToken: &tokens.Token{UID: "owner"},
		ID:        "id",
		Name:      "name",
		Slot:      mediaslot.Background,
		Items:     []playlists.PlaylistItem{{ZapScript: "a", Name: "A"}, {ZapScript: "b", Name: "B"}},
		Index:     1,
		Playing:   true,
		Loop:      true,
		LoopOne:   true,
		Unsafe:    true,
	}

	// Goto to the index it already holds, so nothing but the queue signals
	// may differ from the original.
	got := playlists.Goto(*p, 1)
	assert.Equal(t, p, got, "Goto to the same index changed a field")
}

func TestTransitions_PreserveHoldToken(t *testing.T) {
	t.Parallel()

	owner := &tokens.Token{UID: "playlist-card", Text: "**playlist.next"}
	p := playlists.NewPlaylist("id", "name", []playlists.PlaylistItem{{ZapScript: "a"}, {ZapScript: "b"}})
	p.HoldToken = owner

	assert.Same(t, owner, playlists.Next(*p).HoldToken)
	assert.Same(t, owner, playlists.Previous(*p).HoldToken)
	assert.Same(t, owner, playlists.Goto(*p, 1).HoldToken)
	assert.Same(t, owner, playlists.Play(*p).HoldToken)
	assert.Same(t, owner, playlists.Pause(*p).HoldToken)
}

func failurePaused() playlists.Playlist {
	return playlists.Playlist{
		ID:              "id",
		Items:           []playlists.PlaylistItem{{ZapScript: "a"}, {ZapScript: "b"}, {ZapScript: "c"}},
		Index:           1,
		PausedByFailure: true,
	}
}

// A playlist that stopped because its item failed to launch plays again from
// whichever item it moves to; nobody asked for it to stop.
func TestMoves_ResumeAPlaylistPausedByFailure(t *testing.T) {
	t.Parallel()

	for name, move := range map[string]func(playlists.Playlist) *playlists.Playlist{
		"next":     playlists.Next,
		"previous": playlists.Previous,
		"goto":     func(p playlists.Playlist) *playlists.Playlist { return playlists.Goto(p, 2) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := move(failurePaused())
			assert.True(t, got.Playing)
			assert.False(t, got.PausedByFailure, "the marker describes the stop, not the new item")
			assert.NotEqual(t, 1, got.Index)
		})
	}
}

// A playlist someone paused keeps its cursor-only moves.
func TestMoves_KeepAPausedPlaylistPaused(t *testing.T) {
	t.Parallel()

	p := failurePaused()
	p.PausedByFailure = false
	assert.False(t, playlists.Next(p).Playing)
	assert.False(t, playlists.Previous(p).Playing)
	assert.False(t, playlists.Goto(p, 2).Playing)
}

// Playing again, or pausing on purpose, ends the failure state.
func TestPlayAndPause_ClearPausedByFailure(t *testing.T) {
	t.Parallel()

	assert.False(t, playlists.Play(failurePaused()).PausedByFailure)
	paused := playlists.Pause(failurePaused())
	assert.False(t, paused.PausedByFailure)
	assert.False(t, playlists.Next(*paused).Playing, "a playlist paused on purpose stays paused")
}

// Moving to the item it already holds is still a move: a retry of the failed
// item through goto plays it.
func TestGoto_SameIndexResumesAPlaylistPausedByFailure(t *testing.T) {
	t.Parallel()

	got := playlists.Goto(failurePaused(), 1)
	assert.True(t, got.Playing)
	assert.Equal(t, 1, got.Index)
}

// Current reads the clamped item without repairing the index: the stored
// playlist is shared between goroutines, so writing it from a read would race.
func TestCurrent_ClampsWithoutMutating(t *testing.T) {
	t.Parallel()

	items := []playlists.PlaylistItem{{ZapScript: "a"}, {ZapScript: "b"}}
	for _, tt := range []struct {
		want  string
		index int
	}{
		{index: -3, want: "a"},
		{index: 0, want: "a"},
		{index: 1, want: "b"},
		{index: 9, want: "b"},
	} {
		p := playlists.Playlist{Items: items, Index: tt.index}
		assert.Equal(t, tt.want, p.Current().ZapScript, "index %d", tt.index)
		assert.Equal(t, tt.index, p.Index, "Current must not write the index")
	}
	assert.Equal(t, playlists.PlaylistItem{}, (&playlists.Playlist{Index: 4}).Current())
}
