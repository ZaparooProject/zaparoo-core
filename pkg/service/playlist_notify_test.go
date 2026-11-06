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

package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startTestPlaylistNotifier(t *testing.T) (st *state.State, published <-chan models.Notification) {
	t.Helper()
	st, ns := state.NewState(nil, "boot")
	out := make(chan models.Notification, 16)
	t.Cleanup(func() {
		st.StopService()
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})
	notifier := newPlaylistNotifier(st, func(n models.Notification) { out <- n })
	st.SetPlaylistObserver(func(string, *playlists.Playlist) { notifier.Signal() })
	go notifier.run(st.GetContext())
	return st, out
}

func nextPlaylistsChanged(t *testing.T, published <-chan models.Notification) models.PlaylistsChangedParams {
	t.Helper()
	select {
	case n := <-published:
		assert.Equal(t, models.NotificationPlaylistsChanged, n.Method)
		var params models.PlaylistsChangedParams
		require.NoError(t, json.Unmarshal(n.Params, &params))
		return params
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for playlists.changed")
	}
	return models.PlaylistsChangedParams{}
}

func TestPlaylistNotifier_PublishesEverySlot(t *testing.T) {
	t.Parallel()
	st, published := startTestPlaylistNotifier(t)

	background := makeServicePlaylist()
	background.Slot = mediaslot.Background
	background.Shuffled = true
	st.SetBackgroundPlaylist(background)
	params := nextPlaylistsChanged(t, published)
	require.Len(t, params.Playlists, 1)
	assert.Equal(t, mediaslot.Background, params.Playlists[0].Slot)
	assert.True(t, params.Playlists[0].Shuffle)

	primary := makeServicePlaylist()
	primary.ID = "primary"
	primary.Slot = ""
	st.SetActivePlaylist(primary)
	params = nextPlaylistsChanged(t, published)
	require.Len(t, params.Playlists, 2, "every open slot is reported together")
	assert.Equal(t, "primary", params.Playlists[0].ID)
	assert.Equal(t, mediaslot.Primary, params.Playlists[0].Slot, "an unset slot is reported as primary")
	assert.Equal(t, mediaslot.Background, params.Playlists[1].Slot)

	st.SetActivePlaylist(nil)
	st.SetBackgroundPlaylist(nil)
	assert.Eventually(t, func() bool {
		select {
		case n := <-published:
			return string(n.Params) == `{"playlists":[]}`
		default:
			return false
		}
	}, 2*time.Second, 5*time.Millisecond, "closing every playlist reports an empty list")
}

func TestPlaylistNotifier_SkipsUnchangedState(t *testing.T) {
	t.Parallel()
	st, published := startTestPlaylistNotifier(t)

	pls := makeServicePlaylist()
	st.SetActivePlaylist(pls)
	nextPlaylistsChanged(t, published)

	// Storing an equal copy changes nothing a client can see.
	same := *pls
	st.SetActivePlaylist(&same)
	select {
	case n := <-published:
		t.Fatalf("unexpected notification for unchanged playlists: %s", n.Params)
	case <-time.After(100 * time.Millisecond):
	}

	st.SetActivePlaylist(playlists.Next(same))
	params := nextPlaylistsChanged(t, published)
	require.Len(t, params.Playlists, 1)
	assert.Equal(t, 1, params.Playlists[0].Index)
}

func TestPlaylistNotifier_SignalNeverBlocks(t *testing.T) {
	t.Parallel()
	notifier := newPlaylistNotifier(nil, nil)
	for range 10 {
		notifier.Signal()
	}
	var nilNotifier *playlistNotifier
	nilNotifier.Signal()
}
