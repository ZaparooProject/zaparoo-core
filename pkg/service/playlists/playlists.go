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

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

type PlaylistItem struct {
	ZapScript string
	Name      string
}

type Playlist struct {
	// HoldToken is internal runtime ownership for primary hold-mode playback.
	// It is not part of playlist persistence or API responses.
	HoldToken *tokens.Token
	// AllowedCommands is the bound carried by the script that opened this
	// playlist, and it travels to every item token. A playlist is an
	// indirection like a ZapLink: opening one must not let its items reach
	// commands the script that opened it could not run itself.
	AllowedCommands tokens.CommandPolicy
	ID              string
	// DeckID names the deck on this device the playlist was opened from. It
	// is set only by the deck loader, never from a served playlist, so it is
	// what ties an open playlist to its deck; the ID is whatever its source
	// chose to call it.
	DeckID  string
	Name    string
	Slot    string
	Items   []PlaylistItem
	Index   int
	Playing bool
	// Clear signals the queue handler to remove the active playlist for this slot.
	Clear bool
	// Loop and LoopOne control end-of-playlist behavior set at load time.
	// Loop wraps back to the start; LoopOne repeats the current track.
	Loop    bool
	LoopOne bool
	// ForceRelaunch bypasses playlistNeedsUpdate dedup so the same track can be
	// relaunched for LoopOne and single-item Loop.
	ForceRelaunch bool
	// Refresh replaces the items and name of the active playlist with the
	// same ID in place, keeping its position and playback state and never
	// launching anything. It is ignored when no such playlist is active.
	Refresh bool
	// Unsafe is the playlist's effective trust. It is set either because the
	// items came from a source this device does not trust — a fetched
	// ZapLink, or a cached copy of somebody else's deck — or because the
	// script that opened it was already untrusted, which even the user's own
	// deck inherits. Every item token carries it, so commands that drive
	// input or run programs refuse, the same as the script that opened the
	// playlist.
	Unsafe bool
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

// transition copies a playlist for a move to a new position or playback
// state. Every field carries across, so one added later — Unsafe above, which
// decides whether items run with input and program rights — cannot be dropped
// by a copy that forgot to list it. Clear, ForceRelaunch and Refresh are the
// exception: they describe the single update that delivered them to the queue
// handler, not the playlist, so they never outlive it.
func transition(p *Playlist) *Playlist {
	out := *p
	out.Clear = false
	out.ForceRelaunch = false
	out.Refresh = false
	return &out
}

func Next(p Playlist) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	idx := p.Index + 1
	if idx >= len(p.Items) {
		idx = 0
	}
	out := transition(&p)
	out.Index = idx
	return out
}

func Previous(p Playlist) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	idx := p.Index - 1
	if idx < 0 {
		idx = len(p.Items) - 1
	}
	out := transition(&p)
	out.Index = idx
	return out
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
	out := transition(&p)
	out.Index = idx
	return out
}

func Play(p Playlist) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	out := transition(&p)
	out.Playing = true
	return out
}

func Pause(p Playlist) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	out := transition(&p)
	out.Playing = false
	return out
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
	Current    *Playlist
	HoldToken  *tokens.Token
	Queue      chan<- *Playlist
}
