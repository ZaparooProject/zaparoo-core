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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextAfterCompletion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pls       *playlists.Playlist
		name      string
		wantIndex int
		wantOK    bool
		wantForce bool
	}{
		{name: "next item", pls: testPlaylist(3, 0, nil), wantOK: true, wantIndex: 1},
		{name: "last item without repeat ends", pls: testPlaylist(3, 2, nil), wantOK: false},
		{
			name: "repeat all wraps", wantOK: true, wantIndex: 0,
			pls: testPlaylist(3, 2, func(p *playlists.Playlist) { p.Loop = true }),
		},
		{
			name: "repeat all with one item relaunches it", wantOK: true, wantIndex: 0, wantForce: true,
			pls: testPlaylist(1, 0, func(p *playlists.Playlist) { p.Loop = true }),
		},
		{
			name: "repeat one stays and relaunches", wantOK: true, wantIndex: 1, wantForce: true,
			pls: testPlaylist(3, 1, func(p *playlists.Playlist) { p.LoopOne = true }),
		},
		{name: "empty playlist ends", pls: testPlaylist(0, 0, nil), wantOK: false},
		{name: "no playlist", pls: nil, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			next, ok := nextAfterCompletion(tt.pls)
			require.Equal(t, tt.wantOK, ok)
			if !ok {
				assert.Nil(t, next)
				return
			}
			assert.Equal(t, tt.wantIndex, next.Index)
			assert.True(t, next.Playing)
			assert.Equal(t, tt.wantForce, next.ForceRelaunch)
		})
	}
}

func TestNextAfterCompletion_KeepsTrustAndSettings(t *testing.T) {
	t.Parallel()

	pls := testPlaylist(3, 0, func(p *playlists.Playlist) {
		p.Unsafe = true
		p.Advance = 5
		p.Countdown = 3
		p.CountdownSet = true
		p.DeckID = "deck"
	})
	next, ok := nextAfterCompletion(pls)
	require.True(t, ok)
	assert.True(t, next.Unsafe, "an untrusted playlist stays untrusted")
	assert.Equal(t, pls.Advance, next.Advance)
	assert.Equal(t, pls.Countdown, next.Countdown)
	assert.True(t, next.CountdownSet)
	assert.Equal(t, "deck", next.DeckID)
}

func TestItemLabel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Sonic", itemLabel(playlists.PlaylistItem{Name: " Sonic ", ZapScript: "x"}))
	assert.Equal(t, "game", itemLabel(playlists.PlaylistItem{ZapScript: "/roms/snes/game.sfc"}))
	assert.Equal(t, "**launch.random:snes", itemLabel(playlists.PlaylistItem{ZapScript: "**launch.random:snes"}))
	long := itemLabel(playlists.PlaylistItem{Name: string(make([]byte, 500))})
	assert.LessOrEqual(t, len(long), maxLabelBytes+3)
}
