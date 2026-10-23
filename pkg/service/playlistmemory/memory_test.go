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

package playlistmemory_test

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlistmemory"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// profileSwitch is the active profile a test can change.
type profileSwitch struct {
	id string
	mu syncutil.Mutex
}

func (p *profileSwitch) get() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.id
}

func (p *profileSwitch) set(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.id = id
}

func newMemory(t *testing.T) (*playlistmemory.Memory, database.PlaylistMemoryStore, *profileSwitch) {
	t.Helper()
	db, cleanup := helpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	profile := &profileSwitch{}
	mem := playlistmemory.New(db, profile.get)
	mem.Start()
	t.Cleanup(mem.Stop)
	return mem, db, profile
}

func scripts(names ...string) []playlists.PlaylistItem {
	out := make([]playlists.PlaylistItem, len(names))
	for i, name := range names {
		out[i] = playlists.PlaylistItem{ZapScript: "**launch:" + name, Name: name}
	}
	return out
}

// load is a playlist the way a command loads it: fresh, then prepared.
func load(mem *playlistmemory.Memory, id string, start int, names ...string) *playlists.Playlist {
	pls := playlists.NewPlaylist(id, id, scripts(names...))
	mem.Prepare(pls, start)
	return pls
}

func TestPrepareResumesWhereThePlaylistWasLeft(t *testing.T) {
	t.Parallel()
	mem, _, _ := newMemory(t)

	pls := load(mem, "folder", 0, "a", "b", "c")
	assert.Equal(t, 0, pls.Index, "a playlist never played starts at the top")
	mem.PlaylistChanged(mediaslot.Primary, playlists.Goto(*pls, 2))

	again := load(mem, "folder", 0, "a", "b", "c")
	assert.Equal(t, 2, again.Index)

	// The item is followed, not the number, when items are added before it.
	moved := load(mem, "folder", 0, "z", "a", "b", "c")
	assert.Equal(t, 3, moved.Index)
}

func TestPrepareStartWinsOverMemory(t *testing.T) {
	t.Parallel()
	mem, _, _ := newMemory(t)

	pls := load(mem, "folder", 0, "a", "b", "c")
	mem.PlaylistChanged(mediaslot.Primary, playlists.Goto(*pls, 2))

	assert.Equal(t, 0, load(mem, "folder", 1, "a", "b", "c").Index)
	assert.Equal(t, 1, load(mem, "folder", 2, "a", "b", "c").Index)
	assert.Equal(t, 2, load(mem, "folder", 9, "a", "b", "c").Index, "a start past the end opens on the last item")
}

func TestPrepareWithoutMemoryStillHonoursStart(t *testing.T) {
	t.Parallel()
	var mem *playlistmemory.Memory
	pls := playlists.NewPlaylist("x", "x", scripts("a", "b"))
	mem.Prepare(pls, 2)
	assert.Equal(t, 1, pls.Index)
}

func TestPrepareDoesNotAttachAmbiguously(t *testing.T) {
	t.Parallel()
	mem, _, _ := newMemory(t)

	gone := load(mem, "gone", 0, "a", "b")
	mem.PlaylistChanged(mediaslot.Primary, playlists.Goto(*gone, 1))
	assert.Equal(t, 0, load(mem, "gone", 0, "a", "c").Index, "a remembered item that is gone starts over")

	dup := load(mem, "dup", 0, "a", "b", "a")
	mem.PlaylistChanged(mediaslot.Primary, playlists.Goto(*dup, 2))
	assert.Equal(t, 2, load(mem, "dup", 0, "a", "b", "a").Index, "the same position settles a duplicate")
	assert.Equal(t, 0, load(mem, "dup", 0, "b", "a", "x", "a").Index,
		"a duplicate that moved cannot be told apart, so it starts over")
}

func TestPrepareReusesShuffleOrderOnlyForTheSameItems(t *testing.T) {
	t.Parallel()
	mem, _, _ := newMemory(t)

	played := playlists.NewPlaylist("mix", "mix", scripts("c", "a", "b"))
	played.Shuffled = true
	mem.Prepare(played, 0)
	mem.PlaylistChanged(mediaslot.Primary, playlists.Goto(*played, 1))

	reloaded := playlists.NewPlaylist("mix", "mix", scripts("b", "c", "a"))
	reloaded.Shuffled = true
	mem.Prepare(reloaded, 0)
	assert.Equal(t, playlists.ItemScripts(played.Items), playlists.ItemScripts(reloaded.Items),
		"the order it was playing in comes back")
	assert.Equal(t, 1, reloaded.Index)

	changed := playlists.NewPlaylist("mix", "mix", scripts("b", "d", "c", "a"))
	changed.Shuffled = true
	mem.Prepare(changed, 0)
	assert.Equal(t, []string{"**launch:b", "**launch:d", "**launch:c", "**launch:a"},
		playlists.ItemScripts(changed.Items), "a different set of items keeps its fresh order")
	assert.Equal(t, 3, changed.Index, "but still opens on the remembered item")
}

func TestMemoryIsPerProfile(t *testing.T) {
	t.Parallel()
	mem, _, profile := newMemory(t)

	shared := load(mem, "deck", 0, "a", "b", "c")
	assert.Empty(t, shared.ProfileID)
	mem.PlaylistChanged(mediaslot.Primary, playlists.Goto(*shared, 2))

	profile.set("kid")
	kid := load(mem, "deck", 0, "a", "b", "c")
	assert.Equal(t, "kid", kid.ProfileID)
	assert.Equal(t, 0, kid.Index, "another profile has its own place")
	mem.PlaylistChanged(mediaslot.Primary, playlists.Goto(*kid, 1))

	// A playlist keeps writing to the profile that loaded it after a switch.
	profile.set("")
	mem.PlaylistChanged(mediaslot.Primary, playlists.Goto(*kid, 0))
	assert.Equal(t, 2, load(mem, "deck", 0, "a", "b", "c").Index)
	profile.set("kid")
	assert.Equal(t, 0, load(mem, "deck", 0, "a", "b", "c").Index)
}

func TestForgetStartsOver(t *testing.T) {
	t.Parallel()
	mem, _, _ := newMemory(t)

	pls := load(mem, "deck", 0, "a", "b")
	last := playlists.Goto(*pls, 1)
	mem.PlaylistChanged(mediaslot.Primary, last)
	mem.Forget(last)
	assert.Equal(t, 0, load(mem, "deck", 0, "a", "b").Index)

	// A later change remembers it again, even with the same item as before.
	mem.PlaylistChanged(mediaslot.Primary, last)
	assert.Equal(t, 1, load(mem, "deck", 0, "a", "b").Index)
}

func TestEphemeralPlaylistsAreNotRemembered(t *testing.T) {
	t.Parallel()
	mem, store, _ := newMemory(t)

	random := playlists.NewPlaylist("uuid", "Random", scripts("a"))
	random.Ephemeral = true
	mem.PlaylistChanged(mediaslot.Primary, random)
	mem.Flush(context.Background())

	rows, err := store.ListOpenPlaylists()
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestOpenPlaylistsRestorePausedForTheBootProfile(t *testing.T) {
	t.Parallel()
	mem, store, profile := newMemory(t)

	primary := load(mem, "deck", 0, "a", "b")
	primary.Unsafe = true
	primary.AllowedCommands = tokens.NewCommandPolicy("launch")
	playing := playlists.Play(*playlists.Goto(*primary, 1))
	playing.Idle = 1
	playing.Armed = true
	mem.PlaylistChanged(mediaslot.Primary, playing)

	profile.set("kid")
	bg := load(mem, "music", 0, "x")
	bg.Slot = mediaslot.Background
	mem.PlaylistChanged(mediaslot.Background, bg)
	mem.Flush(context.Background())

	profile.set("")
	restored := mem.OpenPlaylists()
	require.Len(t, restored, 1, "only the boot profile's playlists come back")
	got := restored[0]
	assert.Equal(t, mediaslot.Primary, got.Slot)
	assert.Equal(t, 1, got.Index)
	assert.False(t, got.Playing)
	assert.False(t, got.Armed, "an idle playlist does not start itself after a restart")
	assert.True(t, got.Unsafe)
	assert.True(t, got.AllowedCommands.Allows("launch"))
	assert.False(t, got.AllowedCommands.Allows("execute"))

	profile.set("kid")
	restored = mem.OpenPlaylists()
	require.Len(t, restored, 1)
	assert.Equal(t, mediaslot.Background, restored[0].Slot)

	// Closing a slot clears it.
	mem.PlaylistChanged(mediaslot.Background, nil)
	mem.Flush(context.Background())
	rows, err := store.ListOpenPlaylists()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, mediaslot.Primary, rows[0].Slot)
}

func TestHeldPlaylistIsNotReopened(t *testing.T) {
	t.Parallel()
	mem, store, _ := newMemory(t)

	pls := load(mem, "deck", 0, "a", "b")
	mem.PlaylistChanged(mediaslot.Primary, pls)
	held := playlists.Next(*pls)
	held.HoldToken = &tokens.Token{UID: "card"}
	mem.PlaylistChanged(mediaslot.Primary, held)
	mem.Flush(context.Background())

	rows, err := store.ListOpenPlaylists()
	require.NoError(t, err)
	assert.Empty(t, rows, "a card-held playlist ends with the card")
	assert.Equal(t, 1, load(mem, "deck", 0, "a", "b").Index, "but where it got is still remembered")
}

func TestStopWritesWhatIsQueued(t *testing.T) {
	t.Parallel()
	db, cleanup := helpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	mem := playlistmemory.New(db, nil)
	mem.Start()

	pls := playlists.NewPlaylist("deck", "deck", scripts("a", "b"))
	for range 50 {
		mem.PlaylistChanged(mediaslot.Primary, playlists.Next(*pls))
	}
	mem.Stop()
	mem.PlaylistChanged(mediaslot.Background, pls)

	rows, err := db.ListOpenPlaylists()
	require.NoError(t, err)
	require.Len(t, rows, 1, "everything queued before Stop is written, nothing after")
	assert.Equal(t, mediaslot.Primary, rows[0].Slot)
}

func TestNilMemoryRemembersNothing(t *testing.T) {
	t.Parallel()
	assert.Nil(t, playlistmemory.New(nil, nil))
	var mem *playlistmemory.Memory
	mem.Start()
	mem.PlaylistChanged(mediaslot.Primary, playlists.NewPlaylist("x", "x", scripts("a")))
	mem.Forget(playlists.NewPlaylist("x", "x", scripts("a")))
	mem.SavePosition("", "Audio", "/a.mp3", 1, 2)
	pos, length := mem.ResumeAt("Audio", "/a.mp3")
	assert.Zero(t, pos)
	assert.Zero(t, length)
	assert.Nil(t, mem.OpenPlaylists())
	mem.Stop()
}
