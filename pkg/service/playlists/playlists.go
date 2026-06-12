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

package playlists

import "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"

type PlaylistItem struct {
	ZapScript string
	Name      string
}

type Playlist struct {
	ID      string
	Name    string
	Slot    string
	Items   []PlaylistItem
	Index   int
	Playing bool
	Clear   bool // signals the queue handler to remove the active playlist for this slot
	// Loop and LoopOne control end-of-playlist behaviour set at load time.
	// Loop wraps back to the start; LoopOne repeats the current track.
	Loop    bool
	LoopOne bool
	// ForceRelaunch bypasses the playlistNeedsUpdate dedup so the same track can be
	// relaunched (needed for LoopOne and single-item Loop).
	ForceRelaunch bool
}

func NewPlaylist(id, name string, item []PlaylistItem) *Playlist {
	return &Playlist{
		ID:      id,
		Name:    name,
		Slot:    mediaslot.Primary,
		Items:   item,
		Index:   0,
		Playing: false,
	}
}

func Next(p Playlist) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	idx := p.Index + 1
	if idx >= len(p.Items) {
		idx = 0
	}
	return &Playlist{
		ID:      p.ID,
		Name:    p.Name,
		Slot:    p.Slot,
		Items:   p.Items,
		Index:   idx,
		Playing: p.Playing,
		Loop:    p.Loop,
		LoopOne: p.LoopOne,
	}
}

func Previous(p Playlist) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	idx := p.Index - 1
	if idx < 0 {
		idx = len(p.Items) - 1
	}
	return &Playlist{
		ID:      p.ID,
		Name:    p.Name,
		Slot:    p.Slot,
		Items:   p.Items,
		Index:   idx,
		Playing: p.Playing,
		Loop:    p.Loop,
		LoopOne: p.LoopOne,
	}
}

func Goto(p Playlist, idx int) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	// Handle empty playlist case
	switch {
	case len(p.Items) == 0:
		idx = 0
	case idx >= len(p.Items):
		idx = len(p.Items) - 1
	case idx < 0:
		idx = 0
	}
	return &Playlist{
		ID:      p.ID,
		Name:    p.Name,
		Slot:    p.Slot,
		Items:   p.Items,
		Index:   idx,
		Playing: p.Playing,
		Loop:    p.Loop,
		LoopOne: p.LoopOne,
	}
}

func Play(p Playlist) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	return &Playlist{
		ID:      p.ID,
		Name:    p.Name,
		Slot:    p.Slot,
		Items:   p.Items,
		Index:   p.Index,
		Playing: true,
		Loop:    p.Loop,
		LoopOne: p.LoopOne,
	}
}

func Pause(p Playlist) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	return &Playlist{
		ID:      p.ID,
		Name:    p.Name,
		Slot:    p.Slot,
		Items:   p.Items,
		Index:   p.Index,
		Playing: false,
		Loop:    p.Loop,
		LoopOne: p.LoopOne,
	}
}

func (p *Playlist) Current() PlaylistItem {
	// Add bounds checking to prevent panic
	if len(p.Items) == 0 {
		return PlaylistItem{}
	}
	if p.Index < 0 || p.Index >= len(p.Items) {
		// Clamp to valid range
		if p.Index < 0 {
			p.Index = 0
		} else {
			p.Index = len(p.Items) - 1
		}
	}
	return p.Items[p.Index]
}

type PlaylistController struct {
	Active     *Playlist
	Background *Playlist
	Queue      chan<- *Playlist
}
