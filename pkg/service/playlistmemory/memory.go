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

// Package playlistmemory remembers where each playlist was left, which
// playlists were open when Core stopped, and how far into long audio tracks
// playback got. Writes go through one background writer so the queue handler
// never waits on the database.
package playlistmemory

import (
	"context"
	"slices"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/rs/zerolog/log"
)

const (
	// KeepPlaylists is how many playlists each profile remembers.
	KeepPlaylists = 200
	// KeepPositions is how many track positions each profile remembers.
	KeepPositions = 200
	// lookupFlushTimeout bounds how long a lookup waits for writes already
	// queued, so it reads where the playlist was left a moment ago.
	lookupFlushTimeout = 2 * time.Second
)

// Memory is the playlist and position memory of one device. A nil Memory
// remembers nothing, which is what a database without the capability gets.
type Memory struct {
	store         database.PlaylistMemoryStore
	activeProfile func() string
	wake          chan struct{}
	done          chan struct{}
	// Owned by the writer goroutine: what was last written, so an update
	// that changes nothing stored is not written again.
	lastMemory map[string]database.PlaylistMemory
	lastOpen   map[string]string
	pending    []func()
	mu         syncutil.Mutex
	started    bool
	stopping   bool
}

// New returns the memory backed by store, or nil when store is nil.
// activeProfile returns the active profile's ID, "" for none.
func New(store database.PlaylistMemoryStore, activeProfile func() string) *Memory {
	if store == nil {
		return nil
	}
	if activeProfile == nil {
		activeProfile = func() string { return "" }
	}
	return &Memory{
		store:         store,
		activeProfile: activeProfile,
		wake:          make(chan struct{}, 1),
		done:          make(chan struct{}),
		lastMemory:    make(map[string]database.PlaylistMemory),
		lastOpen:      make(map[string]string),
	}
}

// Start runs the writer until Stop.
func (m *Memory) Start() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()
	go m.run()
}

// Stop writes everything already queued and stops the writer. Writes queued
// after it are dropped.
func (m *Memory) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	started := m.started
	m.stopping = true
	m.mu.Unlock()
	if !started {
		return
	}
	m.signal()
	<-m.done
}

func (m *Memory) run() {
	defer close(m.done)
	for range m.wake {
		m.mu.Lock()
		ops := m.pending
		m.pending = nil
		stopping := m.stopping
		m.mu.Unlock()
		for _, op := range ops {
			op()
		}
		if stopping {
			m.mu.Lock()
			left := len(m.pending)
			m.mu.Unlock()
			if left == 0 {
				return
			}
			m.signal()
		}
	}
}

func (m *Memory) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Memory) enqueue(op func()) bool {
	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		return false
	}
	m.pending = append(m.pending, op)
	m.mu.Unlock()
	m.signal()
	return true
}

// Flush waits until every write queued before it is done, or ctx ends.
func (m *Memory) Flush(ctx context.Context) {
	if m == nil {
		return
	}
	m.mu.Lock()
	started := m.started
	m.mu.Unlock()
	if !started {
		return
	}
	done := make(chan struct{})
	if !m.enqueue(func() { close(done) }) {
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// PlaylistChanged records the playlist now in slot, nil when the slot was
// closed: where the playlist is, for the profile that loaded it, and what to
// reopen after a restart. It never blocks, so it is safe to call while the
// caller holds its own lock.
func (m *Memory) PlaylistChanged(slot string, pls *playlists.Playlist) {
	if m == nil {
		return
	}
	m.enqueue(func() {
		m.remember(pls)
		m.recordOpen(slot, pls)
	})
}

// Forget starts a playlist over the next time it loads, as when it played to
// its natural end.
func (m *Memory) Forget(pls *playlists.Playlist) {
	if m == nil {
		return
	}
	key := playlists.MemoryKey(pls)
	if key == "" {
		return
	}
	profileID := pls.ProfileID
	m.enqueue(func() {
		delete(m.lastMemory, memoryCacheKey(profileID, key))
		if err := m.store.DeletePlaylistMemory(profileID, key); err != nil {
			log.Warn().Err(err).Str("playlist", key).Msg("failed to forget playlist position")
		}
	})
}

func memoryCacheKey(profileID, key string) string {
	return profileID + "\x00" + key
}

func (m *Memory) remember(pls *playlists.Playlist) {
	key := playlists.MemoryKey(pls)
	if key == "" || len(pls.Items) == 0 {
		return
	}
	idx := min(max(pls.Index, 0), len(pls.Items)-1)
	mem := database.PlaylistMemory{
		ProfileID:     pls.ProfileID,
		PlaylistKey:   key,
		ItemZapScript: pls.Items[idx].ZapScript,
		ItemIndex:     idx,
		ItemsHash:     playlists.ItemsHash(pls.Items),
	}
	if pls.Shuffled {
		mem.Order = playlists.ItemScripts(pls.Items)
	}
	cacheKey := memoryCacheKey(pls.ProfileID, key)
	if last, ok := m.lastMemory[cacheKey]; ok && sameMemory(&last, &mem) {
		return
	}
	if err := m.store.UpsertPlaylistMemory(&mem, KeepPlaylists); err != nil {
		log.Warn().Err(err).Str("playlist", key).Msg("failed to remember playlist position")
		return
	}
	m.lastMemory[cacheKey] = mem
}

func sameMemory(a, b *database.PlaylistMemory) bool {
	return a.ItemZapScript == b.ItemZapScript && a.ItemIndex == b.ItemIndex &&
		a.ItemsHash == b.ItemsHash && slices.Equal(a.Order, b.Order)
}

// recordOpen keeps the open-playlist row of slot matching pls. A playlist
// held open by a card, or one with no identity, is not reopened after a
// restart, so the slot is recorded as empty for it.
func (m *Memory) recordOpen(slot string, pls *playlists.Playlist) {
	if pls == nil || pls.HoldToken != nil || playlists.MemoryKey(pls) == "" {
		if last, known := m.lastOpen[slot]; known && last == "" {
			return
		}
		if err := m.store.DeleteOpenPlaylist(slot); err != nil {
			log.Warn().Err(err).Str("slot", slot).Msg("failed to clear open playlist")
			return
		}
		m.lastOpen[slot] = ""
		return
	}
	snapshot, err := playlists.EncodeSnapshot(pls)
	if err != nil {
		log.Warn().Err(err).Str("slot", slot).Msg("failed to encode open playlist")
		return
	}
	if m.lastOpen[slot] == snapshot {
		return
	}
	if err := m.store.UpsertOpenPlaylist(&database.OpenPlaylist{
		Slot: slot, ProfileID: pls.ProfileID, Snapshot: snapshot,
	}); err != nil {
		log.Warn().Err(err).Str("slot", slot).Msg("failed to record open playlist")
		return
	}
	m.lastOpen[slot] = snapshot
}

// Prepare readies a playlist that was just loaded: it belongs to the active
// profile, and it opens where that profile left it. An explicit start, counted
// from 1, wins over the memory. Nothing is attached when the remembered item
// cannot be told apart from another, or is gone.
func (m *Memory) Prepare(pls *playlists.Playlist, start int) {
	if pls == nil {
		return
	}
	if start > 0 {
		pls.Index = min(start-1, max(len(pls.Items)-1, 0))
	}
	if m == nil {
		return
	}
	pls.ProfileID = m.activeProfile()
	key := playlists.MemoryKey(pls)
	if start > 0 || key == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), lookupFlushTimeout)
	m.Flush(ctx)
	cancel()
	mem, found, err := m.store.GetPlaylistMemory(pls.ProfileID, key)
	if err != nil {
		log.Warn().Err(err).Str("playlist", key).Msg("failed to look up playlist position")
		return
	}
	if !found {
		return
	}
	if pls.Shuffled && len(mem.Order) > 0 && mem.ItemsHash == playlists.ItemsHash(pls.Items) {
		if items, ok := playlists.ApplyOrder(pls.Items, mem.Order); ok {
			pls.Items = items
		}
	}
	if idx := playlists.LocateItem(pls.Items, mem.ItemZapScript, mem.ItemIndex); idx >= 0 {
		pls.Index = idx
	}
}

// OpenPlaylists returns the playlists that were open when Core last stopped
// and belong to the active profile, paused.
func (m *Memory) OpenPlaylists() []*playlists.Playlist {
	if m == nil {
		return nil
	}
	rows, err := m.store.ListOpenPlaylists()
	if err != nil {
		log.Warn().Err(err).Msg("failed to read open playlists")
		return nil
	}
	profileID := m.activeProfile()
	out := make([]*playlists.Playlist, 0, len(rows))
	for _, row := range rows {
		if row.ProfileID != profileID {
			continue
		}
		pls, decodeErr := playlists.DecodeSnapshot(row.Snapshot)
		if decodeErr != nil {
			log.Warn().Err(decodeErr).Str("slot", row.Slot).Msg("open playlist not restored")
			continue
		}
		pls.Slot = row.Slot
		pls.ProfileID = row.ProfileID
		if len(pls.Items) == 0 {
			continue
		}
		out = append(out, pls)
	}
	return out
}

// ResumeAt returns how far into the track at systemID and path the active
// profile got, and how long the track was then. It is zero when there is
// nothing to resume.
func (m *Memory) ResumeAt(systemID, path string) (position, length time.Duration) {
	if m == nil {
		return 0, 0
	}
	pos, found, err := m.store.GetMediaPosition(m.activeProfile(), systemID, path)
	if err != nil {
		log.Warn().Err(err).Str("path", path).Msg("failed to look up track position")
		return 0, 0
	}
	if !found {
		return 0, 0
	}
	position = time.Duration(pos.PositionMs) * time.Millisecond
	length = time.Duration(pos.DurationMs) * time.Millisecond
	if !Resumable(position, length) {
		return 0, 0
	}
	return position, length
}

// SavePosition queues how far into a track a profile got.
func (m *Memory) SavePosition(profileID, systemID, path string, position, length time.Duration) {
	if m == nil {
		return
	}
	m.enqueue(func() {
		if err := m.store.UpsertMediaPosition(&database.MediaPosition{
			ProfileID: profileID, SystemID: systemID, Path: path,
			PositionMs: position.Milliseconds(), DurationMs: length.Milliseconds(),
		}, KeepPositions); err != nil {
			log.Warn().Err(err).Str("path", path).Msg("failed to save track position")
		}
	})
}

// ForgetPosition queues forgetting how far into a track a profile got.
func (m *Memory) ForgetPosition(profileID, systemID, path string) {
	if m == nil {
		return
	}
	m.enqueue(func() {
		if err := m.store.DeleteMediaPosition(profileID, systemID, path); err != nil {
			log.Warn().Err(err).Str("path", path).Msg("failed to forget track position")
		}
	})
}
