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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

// MemoryKey is what a playlist is remembered by: its deck when it has one,
// since that is the only identity this device vouches for, else the ID its
// source gave it, else its set of items. It is "" for a playlist that is not
// remembered.
func MemoryKey(p *Playlist) string {
	switch {
	case p == nil || p.Ephemeral:
		return ""
	case p.DeckID != "":
		return "deck:" + p.DeckID
	case p.ID != "":
		return "id:" + p.ID
	case len(p.Items) == 0:
		return ""
	default:
		return "items:" + ItemsHash(p.Items)
	}
}

// ItemsHash identifies the set of items in a playlist regardless of their
// order, so a shuffled copy of the same items hashes the same.
func ItemsHash(items []PlaylistItem) string {
	scripts := ItemScripts(items)
	slices.Sort(scripts)
	h := sha256.New()
	for _, script := range scripts {
		_, _ = h.Write([]byte(script))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ItemScripts returns the ZapScript of each item, in order.
func ItemScripts(items []PlaylistItem) []string {
	scripts := make([]string, len(items))
	for i, item := range items {
		scripts[i] = item.ZapScript
	}
	return scripts
}

// LocateItem finds the item with zapScript. hint, where the item was last
// time, settles which of several identical items it was; without a match
// there the item is only found when exactly one has that script. It returns
// -1 rather than guess.
func LocateItem(items []PlaylistItem, zapScript string, hint int) int {
	if zapScript == "" {
		return -1
	}
	if hint >= 0 && hint < len(items) && items[hint].ZapScript == zapScript {
		return hint
	}
	found := -1
	for i, item := range items {
		if item.ZapScript != zapScript {
			continue
		}
		if found >= 0 {
			return -1
		}
		found = i
	}
	return found
}

// ApplyOrder puts items in order, given as item scripts. It reports false and
// leaves items alone unless order holds exactly the same items.
func ApplyOrder(items []PlaylistItem, order []string) ([]PlaylistItem, bool) {
	if len(order) != len(items) {
		return items, false
	}
	byScript := make(map[string][]PlaylistItem, len(items))
	for _, item := range items {
		byScript[item.ZapScript] = append(byScript[item.ZapScript], item)
	}
	out := make([]PlaylistItem, 0, len(items))
	for _, script := range order {
		queue := byScript[script]
		if len(queue) == 0 {
			return items, false
		}
		out = append(out, queue[0])
		byScript[script] = queue[1:]
	}
	return out, true
}

// snapshotVersion is bumped whenever the stored form changes in a way an
// older reader would misread; a snapshot of another version is not restored.
const snapshotVersion = 1

type snapshotItem struct {
	ZapScript string `json:"zapScript"`
	Name      string `json:"name,omitempty"`
}

type snapshot struct {
	AllowedCommands tokens.CommandPolicy `json:"allowedCommands"`
	ID              string               `json:"id,omitempty"`
	DeckID          string               `json:"deckId,omitempty"`
	ProfileID       string               `json:"profileId,omitempty"`
	Name            string               `json:"name,omitempty"`
	Slot            string               `json:"slot,omitempty"`
	Items           []snapshotItem       `json:"items"`
	Version         int                  `json:"version"`
	Index           int                  `json:"index"`
	AdvanceMs       int64                `json:"advanceMs,omitempty"`
	IdleMs          int64                `json:"idleMs,omitempty"`
	CountdownMs     int64                `json:"countdownMs,omitempty"`
	CountdownSet    bool                 `json:"countdownSet,omitempty"`
	Loop            bool                 `json:"loop,omitempty"`
	LoopOne         bool                 `json:"loopOne,omitempty"`
	Unsafe          bool                 `json:"unsafe,omitempty"`
	Shuffled        bool                 `json:"shuffled,omitempty"`
}

// ErrSnapshotVersion is returned for a snapshot this build cannot read.
var ErrSnapshotVersion = errors.New("unsupported playlist snapshot version")

// EncodeSnapshot stores what a playlist is and where it is, so it can be
// opened again after a restart. Playback state is left out: a restored
// playlist never plays by itself.
func EncodeSnapshot(p *Playlist) (string, error) {
	items := make([]snapshotItem, len(p.Items))
	for i, item := range p.Items {
		items[i] = snapshotItem(item)
	}
	data, err := json.Marshal(snapshot{
		Version: snapshotVersion, ID: p.ID, DeckID: p.DeckID, ProfileID: p.ProfileID, Name: p.Name,
		Slot: p.Slot, Items: items, Index: p.Index, AdvanceMs: p.Advance.Milliseconds(),
		IdleMs: p.Idle.Milliseconds(), CountdownMs: p.Countdown.Milliseconds(), CountdownSet: p.CountdownSet,
		Loop: p.Loop, LoopOne: p.LoopOne, Unsafe: p.Unsafe, Shuffled: p.Shuffled,
		AllowedCommands: p.AllowedCommands,
	})
	if err != nil {
		return "", fmt.Errorf("encode playlist snapshot: %w", err)
	}
	return string(data), nil
}

// DecodeSnapshot opens a playlist stored by EncodeSnapshot, paused and not
// waiting to start.
func DecodeSnapshot(data string) (*Playlist, error) {
	var snap snapshot
	if err := json.Unmarshal([]byte(data), &snap); err != nil {
		return nil, fmt.Errorf("decode playlist snapshot: %w", err)
	}
	if snap.Version != snapshotVersion {
		return nil, fmt.Errorf("%w: %d", ErrSnapshotVersion, snap.Version)
	}
	items := make([]PlaylistItem, len(snap.Items))
	for i, item := range snap.Items {
		items[i] = PlaylistItem(item)
	}
	p := NewPlaylist(snap.ID, snap.Name, items)
	p.DeckID = snap.DeckID
	p.ProfileID = snap.ProfileID
	p.Slot = snap.Slot
	p.Index = min(max(snap.Index, 0), max(len(items)-1, 0))
	p.Advance = time.Duration(snap.AdvanceMs) * time.Millisecond
	p.Idle = time.Duration(snap.IdleMs) * time.Millisecond
	p.Countdown = time.Duration(snap.CountdownMs) * time.Millisecond
	p.CountdownSet = snap.CountdownSet
	p.Loop = snap.Loop
	p.LoopOne = snap.LoopOne
	p.Unsafe = snap.Unsafe
	p.Shuffled = snap.Shuffled
	p.AllowedCommands = snap.AllowedCommands
	return p, nil
}
