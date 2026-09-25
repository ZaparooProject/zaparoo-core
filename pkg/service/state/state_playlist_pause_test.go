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
package state

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pausablePlaylist(id string, index int) *playlists.Playlist {
	pls := playlists.NewPlaylist(id, id, []playlists.PlaylistItem{
		{ZapScript: "**a"}, {ZapScript: "**b"}, {ZapScript: "**c"},
	})
	pls.Index = index
	pls.Playing = true
	return pls
}

func TestPausePlaylistIfCurrent(t *testing.T) {
	t.Parallel()

	launched := pausablePlaylist("p", 1)
	tests := []struct {
		stored     *playlists.Playlist
		name       string
		wantPaused bool
	}{
		{name: "nothing stored", stored: nil, wantPaused: false},
		{name: "the playlist that launched", stored: launched, wantPaused: true},
		{name: "another playlist took the slot", stored: pausablePlaylist("q", 1), wantPaused: false},
		{name: "the playlist moved on", stored: pausablePlaylist("p", 2), wantPaused: false},
		{
			// Every update stores a new value, so an equal copy is a later
			// update: a refresh, or a second unnamed playlist.
			name: "an equal copy stored since", stored: pausablePlaylist("p", 1), wantPaused: false,
		},
		{name: "already paused", stored: playlists.Pause(*launched), wantPaused: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st, _ := NewState(mocks.NewMockPlatform(), "boot")
			st.SetActivePlaylist(tt.stored)
			before := st.GetActivePlaylist()

			paused := st.PausePlaylistIfCurrent(mediaslot.Primary, launched)

			assert.Equal(t, tt.wantPaused, paused)
			after := st.GetActivePlaylist()
			if !tt.wantPaused {
				assert.Same(t, before, after, "a mismatch must leave the stored playlist untouched")
				return
			}
			require.NotNil(t, after)
			assert.False(t, after.Playing)
			assert.True(t, after.PausedByFailure, "moving to another item must resume it")
			assert.Equal(t, launched.ID, after.ID)
			assert.Equal(t, launched.Index, after.Index)
			assert.True(t, launched.Playing, "the launched value itself is never mutated")
		})
	}
}

// Two unnamed playlists share the empty ID. A late failure of the first must
// not pause the second, which isActivePlaylist already treats as different.
func TestPausePlaylistIfCurrent_UnnamedPlaylistsAreDistinct(t *testing.T) {
	t.Parallel()

	st, _ := NewState(mocks.NewMockPlatform(), "boot")
	first := pausablePlaylist("", 0)
	second := pausablePlaylist("", 0)
	st.SetActivePlaylist(second)

	assert.False(t, st.PausePlaylistIfCurrent(mediaslot.Primary, first))
	assert.True(t, st.GetActivePlaylist().Playing)
}

func TestPausePlaylistIfCurrent_BackgroundSlotOnly(t *testing.T) {
	t.Parallel()

	st, _ := NewState(mocks.NewMockPlatform(), "boot")
	primary := pausablePlaylist("p", 0)
	background := pausablePlaylist("p", 0)
	background.Slot = mediaslot.Background
	st.SetActivePlaylist(primary)
	st.SetBackgroundPlaylist(background)

	require.True(t, st.PausePlaylistIfCurrent(mediaslot.Background, background))

	assert.False(t, st.GetBackgroundPlaylist().Playing)
	assert.True(t, st.GetActivePlaylist().Playing, "the primary slot is not the one that failed")
}
