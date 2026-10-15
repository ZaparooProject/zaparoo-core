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
	"time"

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
	DeckID string
	Name   string
	Slot   string
	Items  []PlaylistItem
	Index  int
	// Advance moves to the next item after this much playing time. Zero
	// means the playlist does not rotate on a timer.
	Advance time.Duration
	// Idle moves to the next item after this long with no user input, and
	// starts a loaded playlist the same way. Zero means it does not rotate on
	// idle. It is never set together with Advance.
	Idle time.Duration
	// Countdown is how long the notice before an automatic switch stays up.
	// CountdownSet distinguishes an explicit zero, which switches without a
	// notice, from no value, which uses DefaultCountdown.
	Countdown time.Duration
	// EngineFromIndex is the item the engine moved this update away from, so
	// the queue handler can drop a move the user has already overtaken.
	EngineFromIndex int `json:"-"`
	// StopSeq is the count of stops of the primary media when the update was
	// issued, valid when StopStamped is set. A stop after that cancels an
	// update that has not launched yet.
	StopSeq uint64 `json:"-"`
	// StopStamped says StopSeq was taken for this update.
	StopStamped bool `json:"-"`
	Playing     bool
	// Armed marks a loaded idle playlist that is waiting for the device to go
	// idle before it starts. Playing it, pausing it or stopping it disarms it.
	Armed bool
	// PausedByFailure marks a playlist that stopped because its current item
	// failed to launch, not because anyone paused it. Moving to another item
	// resumes such a playlist, where a paused one only moves its cursor.
	PausedByFailure bool `json:"-"`
	// Clear signals the queue handler to remove the active playlist for this slot.
	Clear bool
	// Loop and LoopOne control end-of-playlist behavior set at load time.
	// Loop wraps back to the start; LoopOne repeats the current track.
	Loop    bool
	LoopOne bool
	// ForceRelaunch bypasses playlistNeedsUpdate dedup so the same track can be
	// relaunched for LoopOne and single-item Loop.
	ForceRelaunch bool
	// FromPlaylistItem marks an update requested by a playlist item's own
	// run, such as a card that plays another playlist. It is part of that
	// item's launch rather than a second one competing with it.
	FromPlaylistItem bool `json:"-"`
	// EngineStarted marks an update the auto-advance engine queued on its own,
	// so the item it launches is known to be one nobody asked for. It is
	// part of that launch rather than of the playlist.
	EngineStarted bool `json:"-"`
	// Reconfigure applies only the rotation settings of this update — Advance,
	// Idle, Countdown and Armed — to the active playlist with the same ID,
	// keeping its position and playback and never launching anything.
	Reconfigure bool `json:"-"`
	// EngineFromArmed says the move was decided on a loaded playlist waiting
	// to start rather than a playing one.
	EngineFromArmed bool `json:"-"`
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
	Unsafe       bool
	CountdownSet bool
}

// DefaultCountdown is how long an automatic switch is announced when a
// playlist does not say.
const DefaultCountdown = 10 * time.Second

// EffectiveCountdown returns the notice length for an automatic switch.
func (p *Playlist) EffectiveCountdown() time.Duration {
	if p.CountdownSet {
		return p.Countdown
	}
	return DefaultCountdown
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
// by a copy that forgot to list it. Clear, ForceRelaunch, Refresh and
// FromPlaylistItem are the exception: they describe the single update that
// delivered them to the queue handler, not the playlist, so they never
// outlive it, and neither do EngineStarted and Reconfigure.
func transition(p *Playlist) *Playlist {
	out := *p
	out.Clear = false
	out.ForceRelaunch = false
	out.Refresh = false
	out.FromPlaylistItem = false
	out.EngineStarted = false
	out.EngineFromIndex = 0
	out.EngineFromArmed = false
	out.StopSeq = 0
	out.StopStamped = false
	out.Reconfigure = false
	return &out
}

// moveTo is transition for a move to another item. A playlist that stopped
// only because an item failed to launch plays again from the new item; one
// that was paused on purpose stays paused and only its cursor moves.
func moveTo(p *Playlist, idx int) *Playlist {
	out := transition(p)
	out.Index = idx
	if out.PausedByFailure {
		out.Playing = true
		out.PausedByFailure = false
	}
	return out
}

func Next(p Playlist) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	idx := p.Index + 1
	if idx >= len(p.Items) {
		idx = 0
	}
	return moveTo(&p, idx)
}

func Previous(p Playlist) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	idx := p.Index - 1
	if idx < 0 {
		idx = len(p.Items) - 1
	}
	return moveTo(&p, idx)
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
	return moveTo(&p, idx)
}

func Play(p Playlist) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	out := transition(&p)
	out.Playing = true
	out.Armed = false
	out.PausedByFailure = false
	return out
}

// Rearm returns p loaded and waiting for the device to go idle: not playing,
// position kept. The hold token is dropped because whatever held the playlist
// open is gone.
func Rearm(p Playlist) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	out := transition(&p)
	out.Playing = false
	out.Armed = true
	out.PausedByFailure = false
	out.HoldToken = nil
	return out
}

// StopRotating returns the reconfiguration that turns off p's advance and idle
// rotation and keeps everything else, including what is playing.
func StopRotating(p Playlist) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	out := transition(&p)
	out.Advance = 0
	out.Idle = 0
	out.Armed = false
	out.Reconfigure = true
	return out
}

func Pause(p Playlist) *Playlist { //nolint:gocritic // value copy preserves immutable-style playlist updates
	out := transition(&p)
	out.Playing = false
	out.Armed = false
	out.PausedByFailure = false
	return out
}

// Current returns the item at the playlist's index, clamped into range. It
// only reads: the stored playlist is shared between goroutines, so a read
// that repaired an out-of-range index in place would be a data race.
func (p *Playlist) Current() PlaylistItem {
	if len(p.Items) == 0 {
		return PlaylistItem{}
	}
	return p.Items[min(max(p.Index, 0), len(p.Items)-1)]
}

type PlaylistController struct {
	Active     *Playlist
	Background *Playlist
	Current    *Playlist
	HoldToken  *tokens.Token
	Queue      chan<- *Playlist
	// LaunchResolved, when set, is told once the launcher of a launch is
	// known whether that launcher publishes the media itself later, outside
	// the launch call. Only the playlist worker sets it.
	LaunchResolved func(publishesLater bool)
}
