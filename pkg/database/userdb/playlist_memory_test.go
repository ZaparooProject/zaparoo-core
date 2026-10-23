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

package userdb

import (
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlaylistMemoryRoundTrip(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	_, found, err := db.GetPlaylistMemory("", "deck:abc")
	require.NoError(t, err)
	assert.False(t, found)

	mem := database.PlaylistMemory{
		ProfileID: "", PlaylistKey: "deck:abc", ItemZapScript: "**launch:b", ItemIndex: 1,
		ItemsHash: "h1", Order: []string{"**launch:c", "**launch:b", "**launch:a"},
	}
	require.NoError(t, db.UpsertPlaylistMemory(&mem, 10))
	got, found, err := db.GetPlaylistMemory("", "deck:abc")
	require.NoError(t, err)
	require.True(t, found)
	assert.NotZero(t, got.UpdatedAt)
	got.UpdatedAt = 0
	assert.Equal(t, mem, got)

	// The same playlist is remembered separately for each profile.
	other := mem
	other.ProfileID = "kid"
	other.ItemZapScript = "**launch:c"
	other.ItemIndex = 0
	other.Order = nil
	require.NoError(t, db.UpsertPlaylistMemory(&other, 10))
	got, _, err = db.GetPlaylistMemory("", "deck:abc")
	require.NoError(t, err)
	assert.Equal(t, "**launch:b", got.ItemZapScript)
	got, _, err = db.GetPlaylistMemory("kid", "deck:abc")
	require.NoError(t, err)
	assert.Equal(t, "**launch:c", got.ItemZapScript)
	assert.Nil(t, got.Order)

	mem.ItemZapScript = "**launch:a"
	require.NoError(t, db.UpsertPlaylistMemory(&mem, 10))
	got, _, err = db.GetPlaylistMemory("", "deck:abc")
	require.NoError(t, err)
	assert.Equal(t, "**launch:a", got.ItemZapScript)

	require.NoError(t, db.DeletePlaylistMemory("", "deck:abc"))
	_, found, err = db.GetPlaylistMemory("", "deck:abc")
	require.NoError(t, err)
	assert.False(t, found)
	_, found, err = db.GetPlaylistMemory("kid", "deck:abc")
	require.NoError(t, err)
	assert.True(t, found)
}

func TestPlaylistMemoryPrunesPerProfile(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	require.NoError(t, db.UpsertPlaylistMemory(&database.PlaylistMemory{
		ProfileID: "kid", PlaylistKey: "id:kept", ItemZapScript: "x", ItemsHash: "h",
	}, 3))
	for i := range 5 {
		require.NoError(t, db.UpsertPlaylistMemory(&database.PlaylistMemory{
			PlaylistKey: fmt.Sprintf("id:%d", i), ItemZapScript: "x", ItemsHash: "h",
		}, 3))
	}
	// Rewriting the oldest survivor keeps it even when it ties on time.
	require.NoError(t, db.UpsertPlaylistMemory(&database.PlaylistMemory{
		PlaylistKey: "id:2", ItemZapScript: "y", ItemsHash: "h",
	}, 3))

	var count int
	require.NoError(t, db.sql.Load().QueryRowContext(t.Context(),
		`select count(*) from PlaylistMemory where ProfileID = ''`).Scan(&count))
	assert.Equal(t, 3, count)
	for _, key := range []string{"id:2", "id:3", "id:4"} {
		_, found, err := db.GetPlaylistMemory("", key)
		require.NoError(t, err)
		assert.True(t, found, key)
	}
	_, found, err := db.GetPlaylistMemory("kid", "id:kept")
	require.NoError(t, err)
	assert.True(t, found, "another profile's memory is never pruned")
}

func TestOpenPlaylistsRoundTrip(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	require.NoError(t, db.UpsertOpenPlaylist(&database.OpenPlaylist{Slot: "primary", Snapshot: `{"v":1}`}))
	require.NoError(t, db.UpsertOpenPlaylist(&database.OpenPlaylist{
		Slot: "background", ProfileID: "kid", Snapshot: `{"v":1,"bg":true}`,
	}))
	require.NoError(t, db.UpsertOpenPlaylist(&database.OpenPlaylist{
		Slot: "primary", ProfileID: "kid", Snapshot: `{"v":1,"n":2}`,
	}))

	rows, err := db.ListOpenPlaylists()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	bySlot := map[string]database.OpenPlaylist{}
	for _, row := range rows {
		bySlot[row.Slot] = row
	}
	assert.Equal(t, "kid", bySlot["primary"].ProfileID)
	assert.JSONEq(t, `{"v":1,"n":2}`, bySlot["primary"].Snapshot)

	require.NoError(t, db.DeleteOpenPlaylist("primary"))
	rows, err = db.ListOpenPlaylists()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "background", rows[0].Slot)
}

func TestMediaPositionsRoundTrip(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	pos := database.MediaPosition{
		SystemID: "Audio", Path: "/music/book.mp3", PositionMs: 61_000, DurationMs: 3_600_000,
	}
	require.NoError(t, db.UpsertMediaPosition(&pos, 10))
	got, found, err := db.GetMediaPosition("", "Audio", "/music/book.mp3")
	require.NoError(t, err)
	require.True(t, found)
	got.UpdatedAt = 0
	assert.Equal(t, pos, got)

	_, found, err = db.GetMediaPosition("kid", "Audio", "/music/book.mp3")
	require.NoError(t, err)
	assert.False(t, found)

	pos.PositionMs = 90_000
	require.NoError(t, db.UpsertMediaPosition(&pos, 10))
	got, _, err = db.GetMediaPosition("", "Audio", "/music/book.mp3")
	require.NoError(t, err)
	assert.Equal(t, int64(90_000), got.PositionMs)

	for i := range 3 {
		require.NoError(t, db.UpsertMediaPosition(&database.MediaPosition{
			SystemID: "Audio", Path: fmt.Sprintf("/music/%d.mp3", i), PositionMs: 1, DurationMs: 2,
		}, 2))
	}
	_, found, err = db.GetMediaPosition("", "Audio", "/music/book.mp3")
	require.NoError(t, err)
	assert.False(t, found, "least recently used positions are pruned")

	require.NoError(t, db.DeleteMediaPosition("", "Audio", "/music/2.mp3"))
	_, found, err = db.GetMediaPosition("", "Audio", "/music/2.mp3")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestDeleteProfileClearsPlaylistState(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	require.NoError(t, db.CreateProfile(newTestProfile("admin", "a-b-c")))
	require.NoError(t, db.CreateProfile(newTestProfile("kid", "d-e-f")))
	for _, profileID := range []string{"", "kid"} {
		require.NoError(t, db.UpsertPlaylistMemory(&database.PlaylistMemory{
			ProfileID: profileID, PlaylistKey: "deck:x", ItemZapScript: "a", ItemsHash: "h",
		}, 10))
		require.NoError(t, db.UpsertMediaPosition(&database.MediaPosition{
			ProfileID: profileID, SystemID: "Audio", Path: "/a.mp3", PositionMs: 1, DurationMs: 2,
		}, 10))
	}
	require.NoError(t, db.UpsertOpenPlaylist(&database.OpenPlaylist{Slot: "primary", ProfileID: "kid", Snapshot: "{}"}))
	require.NoError(t, db.UpsertOpenPlaylist(&database.OpenPlaylist{Slot: "background", Snapshot: "{}"}))

	require.NoError(t, db.DeleteProfile("kid"))

	_, found, err := db.GetPlaylistMemory("kid", "deck:x")
	require.NoError(t, err)
	assert.False(t, found)
	_, found, err = db.GetMediaPosition("kid", "Audio", "/a.mp3")
	require.NoError(t, err)
	assert.False(t, found)
	_, found, err = db.GetPlaylistMemory("", "deck:x")
	require.NoError(t, err)
	assert.True(t, found)
	rows, err := db.ListOpenPlaylists()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "background", rows[0].Slot)
}

func TestTruncateClearsPlaylistState(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	require.NoError(t, db.UpsertPlaylistMemory(&database.PlaylistMemory{
		PlaylistKey: "deck:x", ItemZapScript: "a", ItemsHash: "h",
	}, 10))
	require.NoError(t, db.UpsertOpenPlaylist(&database.OpenPlaylist{Slot: "primary", Snapshot: "{}"}))
	require.NoError(t, db.UpsertMediaPosition(&database.MediaPosition{
		SystemID: "Audio", Path: "/a.mp3", PositionMs: 1, DurationMs: 2,
	}, 10))

	require.NoError(t, db.Truncate())

	_, found, err := db.GetPlaylistMemory("", "deck:x")
	require.NoError(t, err)
	assert.False(t, found)
	rows, err := db.ListOpenPlaylists()
	require.NoError(t, err)
	assert.Empty(t, rows)
	_, found, err = db.GetMediaPosition("", "Audio", "/a.mp3")
	require.NoError(t, err)
	assert.False(t, found)
}
