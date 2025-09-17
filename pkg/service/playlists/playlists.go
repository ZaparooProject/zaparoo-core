// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

type PlaylistItem struct {
	ZapScript string
	Name      string
}

type Playlist struct {
	ID      string
	Name    string
	Items   []PlaylistItem
	Index   int
	Playing bool
}

func NewPlaylist(id, name string, item []PlaylistItem) *Playlist {
	return &Playlist{
		ID:      id,
		Name:    name,
		Items:   item,
		Index:   0,
		Playing: false,
	}
}

func Next(p Playlist) *Playlist {
	idx := p.Index + 1
	if idx >= len(p.Items) {
		idx = 0
	}
	return &Playlist{
		ID:      p.ID,
		Items:   p.Items,
		Index:   idx,
		Playing: p.Playing,
	}
}

func Previous(p Playlist) *Playlist {
	idx := p.Index - 1
	if idx < 0 {
		idx = len(p.Items) - 1
	}
	return &Playlist{
		ID:      p.ID,
		Items:   p.Items,
		Index:   idx,
		Playing: p.Playing,
	}
}

func Goto(p Playlist, idx int) *Playlist {
	// Handle empty playlist case
	switch {
	case len(p.Items) == 0:
		idx = 0
	case idx >= len(p.Items):
		idx = len(p.Items) - 1
	case idx < 0:
		idx = 0
	}
	p.Index = idx
	return &Playlist{
		ID:      p.ID,
		Items:   p.Items,
		Index:   idx,
		Playing: p.Playing,
	}
}

func Play(p Playlist) *Playlist {
	return &Playlist{
		ID:      p.ID,
		Items:   p.Items,
		Index:   p.Index,
		Playing: true,
	}
}

func Pause(p Playlist) *Playlist {
	return &Playlist{
		ID:      p.ID,
		Items:   p.Items,
		Index:   p.Index,
		Playing: false,
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
	Active *Playlist
	Queue  chan<- *Playlist
}
