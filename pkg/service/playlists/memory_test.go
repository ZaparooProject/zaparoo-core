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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func items(scripts ...string) []playlists.PlaylistItem {
	out := make([]playlists.PlaylistItem, len(scripts))
	for i, script := range scripts {
		out[i] = playlists.PlaylistItem{ZapScript: script, Name: "name " + script}
	}
	return out
}

func TestMemoryKeyPerSource(t *testing.T) {
	t.Parallel()

	deck := playlists.NewPlaylist("deck://abc", "Deck", items("a"))
	deck.DeckID = "abc"
	assert.Equal(t, "deck:abc", playlists.MemoryKey(deck), "a deck is remembered by the deck, whatever its ID")

	folder := playlists.NewPlaylist("/media/fat/Music", "Music", items("a"))
	assert.Equal(t, "id:/media/fat/Music", playlists.MemoryKey(folder))

	unnamed := playlists.NewPlaylist("", "", items("b", "a"))
	shuffled := playlists.NewPlaylist("", "", items("a", "b"))
	assert.Equal(t, playlists.MemoryKey(unnamed), playlists.MemoryKey(shuffled),
		"an unnamed playlist is its set of items, in any order")
	assert.NotEqual(t, playlists.MemoryKey(unnamed), playlists.MemoryKey(playlists.NewPlaylist("", "", items("a"))))

	random := playlists.NewPlaylist("uuid", "Random", items("a"))
	random.Ephemeral = true
	assert.Empty(t, playlists.MemoryKey(random))
	assert.Empty(t, playlists.MemoryKey(playlists.NewPlaylist("", "", nil)))
	assert.Empty(t, playlists.MemoryKey(nil))
}

func TestLocateItem(t *testing.T) {
	t.Parallel()

	list := items("a", "b", "a", "c")
	assert.Equal(t, 1, playlists.LocateItem(list, "b", 0))
	assert.Equal(t, 2, playlists.LocateItem(list, "a", 2), "the hint settles which of two identical items")
	assert.Equal(t, -1, playlists.LocateItem(list, "a", 1), "two identical items and no hint is ambiguous")
	assert.Equal(t, -1, playlists.LocateItem(list, "gone", 0))
	assert.Equal(t, -1, playlists.LocateItem(list, "", 0))
	assert.Equal(t, 3, playlists.LocateItem(list, "c", 99))
}

func TestApplyOrder(t *testing.T) {
	t.Parallel()

	list := items("a", "b", "a")
	got, ok := playlists.ApplyOrder(list, []string{"a", "a", "b"})
	require.True(t, ok)
	assert.Equal(t, []string{"a", "a", "b"}, playlists.ItemScripts(got))
	assert.Equal(t, "name b", got[2].Name)

	for _, order := range [][]string{{"a", "b"}, {"a", "b", "b"}, {"a", "b", "c"}} {
		got, ok = playlists.ApplyOrder(list, order)
		assert.False(t, ok, order)
		assert.Equal(t, list, got, order)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	t.Parallel()

	nothing := tokens.NewCommandPolicy("launch").Intersect(tokens.NewCommandPolicy("stop"))
	p := playlists.NewPlaylist("deck://abc", "Weekend", items("c", "a", "b"))
	p.DeckID = "abc"
	p.ProfileID = "kid"
	p.Slot = mediaslot.Background
	p.Index = 2
	p.Advance = 5 * time.Minute
	p.Countdown = 0
	p.CountdownSet = true
	p.Loop = true
	p.Unsafe = true
	p.Shuffled = true
	p.AllowedCommands = nothing
	p.Playing = true
	p.Armed = true
	p.PausedByFailure = true

	data, err := playlists.EncodeSnapshot(p)
	require.NoError(t, err)
	got, err := playlists.DecodeSnapshot(data)
	require.NoError(t, err)

	assert.Equal(t, p.ID, got.ID)
	assert.Equal(t, p.DeckID, got.DeckID)
	assert.Equal(t, p.ProfileID, got.ProfileID)
	assert.Equal(t, p.Name, got.Name)
	assert.Equal(t, p.Slot, got.Slot)
	assert.Equal(t, p.Items, got.Items)
	assert.Equal(t, 2, got.Index)
	assert.Equal(t, 5*time.Minute, got.Advance)
	assert.True(t, got.CountdownSet)
	assert.Zero(t, got.Countdown)
	assert.True(t, got.Loop)
	assert.True(t, got.Unsafe)
	assert.True(t, got.Shuffled)
	assert.False(t, got.AllowedCommands.Unrestricted(), "a bound that permits nothing stays a bound")
	assert.False(t, got.AllowedCommands.Allows("launch"))
	assert.False(t, got.Playing, "a restored playlist never plays by itself")
	assert.False(t, got.Armed, "nor waits for idle to start")
	assert.False(t, got.PausedByFailure)
}

func TestDecodeSnapshotRejectsOtherVersions(t *testing.T) {
	t.Parallel()

	_, err := playlists.DecodeSnapshot(`{"version":99,"items":[]}`)
	require.ErrorIs(t, err, playlists.ErrSnapshotVersion)
	_, err = playlists.DecodeSnapshot(`not json`)
	require.Error(t, err)
}

func TestDecodeSnapshotClampsIndex(t *testing.T) {
	t.Parallel()

	got, err := playlists.DecodeSnapshot(`{"version":1,"index":9,"items":[{"zapScript":"a"},{"zapScript":"b"}]}`)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Index)
}

func TestTransitionDropsCompleted(t *testing.T) {
	t.Parallel()

	p := playlists.NewPlaylist("id", "name", items("a", "b"))
	p.Completed = true
	assert.False(t, playlists.Next(*p).Completed)
}
