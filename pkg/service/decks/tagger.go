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

package decks

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/bgpriority"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

const (
	// taggerRetryFirst and taggerRetryLongest bound the wait before decks whose
	// tags could not be written are tried again. The wait doubles after each
	// pass that still fails.
	taggerRetryFirst   = time.Minute
	taggerRetryLongest = 30 * time.Minute
	// taggerBusyPoll is how often a waiting tagger checks whether the media
	// database is free again.
	taggerBusyPoll = 5 * time.Second
)

// Tagger keeps deck membership tags in the media database in step with the
// decks in the user database. Deck edits and reindexes queue work and return
// at once, and one worker brings each queued deck's tags up to date. A deck
// whose games this device does not have costs a title lookup per item on
// every pass, so doing this inside an edit would hold up the edit and every
// browse behind it. The queue is saved in the user database, so work queued
// before a restart is finished after it.
type Tagger struct {
	deps       *ResolveDeps
	onRelinked func(deckID string)
	wake       chan struct{}
	queued     map[string]uint64
	seq        uint64
	allSeq     uint64
	busyPoll   time.Duration
	mu         syncutil.Mutex
	all        bool
}

// taggerQueue is the saved form of the queue.
type taggerQueue struct {
	Decks []string `json:"decks,omitempty"`
	All   bool     `json:"all,omitempty"`
}

var _ database.DeckTagQueue = (*Tagger)(nil)

// NewTagger returns a tagger holding any work a previous run left queued.
// onRelinked, when set, is called after items of a deck were linked to the
// files their titles matched.
func NewTagger(deps *ResolveDeps, onRelinked func(deckID string)) *Tagger {
	t := &Tagger{
		deps:       deps,
		onRelinked: onRelinked,
		wake:       make(chan struct{}, 1),
		queued:     make(map[string]uint64),
		busyPoll:   taggerBusyPoll,
	}
	raw, found, err := deps.UserDB.GetDeviceState(database.DeviceStateKeyDeckTagsQueue)
	if err != nil {
		log.Warn().Err(err).Msg("failed to read the deck tag queue; re-tagging every deck")
		t.all = true
		return t
	}
	if !found || raw == "" {
		return t
	}
	var saved taggerQueue
	if err = json.Unmarshal([]byte(raw), &saved); err != nil {
		log.Warn().Err(err).Msg("unreadable deck tag queue; re-tagging every deck")
		t.all = true
		return t
	}
	t.all = saved.All
	for _, deckID := range saved.Decks {
		t.seq++
		t.queued[deckID] = t.seq
	}
	return t
}

// QueueDeckTags schedules the tags of the given decks.
func (t *Tagger) QueueDeckTags(deckIDs ...string) {
	if len(deckIDs) == 0 {
		return
	}
	t.mu.Lock()
	for _, deckID := range deckIDs {
		t.seq++
		t.queued[deckID] = t.seq
	}
	t.saveLocked()
	t.mu.Unlock()
	t.signal()
}

// QueueAllDeckTags schedules every deck's tags and the removal of tags that
// belong to no deck.
func (t *Tagger) QueueAllDeckTags() {
	t.mu.Lock()
	t.all = true
	t.allSeq++
	t.saveLocked()
	t.mu.Unlock()
	t.signal()
}

func (t *Tagger) signal() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

// saveLocked stores the queue. A failure is only logged: this run still has
// the queue in memory.
func (t *Tagger) saveLocked() {
	var err error
	if !t.all && len(t.queued) == 0 {
		err = t.deps.UserDB.DeleteDeviceState(database.DeviceStateKeyDeckTagsQueue)
	} else {
		saved := taggerQueue{All: t.all, Decks: make([]string, 0, len(t.queued))}
		for deckID := range t.queued {
			saved.Decks = append(saved.Decks, deckID)
		}
		slices.Sort(saved.Decks)
		encoded, marshalErr := json.Marshal(saved)
		if marshalErr != nil {
			log.Warn().Err(marshalErr).Msg("failed to encode the deck tag queue")
			return
		}
		err = t.deps.UserDB.SetDeviceState(database.DeviceStateKeyDeckTagsQueue, string(encoded))
	}
	if err != nil {
		log.Warn().Err(err).Msg("failed to save the deck tag queue")
	}
}

// Run works through the queue until ctx ends, starting with anything an
// earlier run left queued. It runs at background priority on its own thread.
func (t *Tagger) Run(ctx context.Context) {
	bgpriority.Apply()
	retryDelay := taggerRetryFirst
	for {
		var timer *time.Timer
		var retry <-chan time.Time
		if t.Drain(ctx) {
			timer = time.NewTimer(retryDelay)
			retry = timer.C
			retryDelay = min(retryDelay*2, taggerRetryLongest)
		} else {
			retryDelay = taggerRetryFirst
		}
		select {
		case <-ctx.Done():
		case <-t.wake:
		case <-retry:
		}
		if timer != nil {
			timer.Stop()
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// Drain brings every queued deck's tags up to date, one deck at a time, and
// returns when the queue is empty or ctx ends. It waits while an index,
// optimization, recovery or maintenance job owns the media database: those
// hold long write transactions a tag write would time out behind, and a
// finished index queues every deck anyway. A deck that fails stays queued and
// is not tried again in this call; Drain reports whether any did.
func (t *Tagger) Drain(ctx context.Context) (failed bool) {
	skip := make(map[string]struct{})
	for ctx.Err() == nil {
		if !t.waitForMediaWrites(ctx) {
			return len(skip) > 0
		}
		if err := t.expandAll(ctx); err != nil {
			log.Warn().Err(err).Msg("failed to list decks to re-tag")
			return true
		}
		deckID, seq, ok := t.next(skip)
		if !ok {
			return len(skip) > 0
		}
		started := time.Now()
		relinked, err := ProjectDeck(ctx, t.deps, deckID)
		if err != nil {
			if ctx.Err() == nil {
				log.Warn().Err(err).Str("deck", deckID).Msg("failed to update deck tags; will retry")
			}
			skip[deckID] = struct{}{}
			continue
		}
		log.Debug().Str("deck", deckID).Int("relinked", relinked).Dur("elapsed", time.Since(started)).
			Msg("deck tags updated")
		t.finish(deckID, seq)
		if relinked > 0 && t.onRelinked != nil {
			t.onRelinked(deckID)
		}
	}
	return len(skip) > 0
}

// next returns the longest-queued deck not in skip.
func (t *Tagger) next(skip map[string]struct{}) (deckID string, seq uint64, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, idSeq := range t.queued {
		if _, skipped := skip[id]; skipped {
			continue
		}
		if !ok || idSeq < seq {
			deckID, seq, ok = id, idSeq, true
		}
	}
	return deckID, seq, ok
}

// finish drops a deck from the queue unless it was queued again while its
// tags were being written.
func (t *Tagger) finish(deckID string, seq uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.queued[deckID] != seq {
		return
	}
	delete(t.queued, deckID)
	t.saveLocked()
}

// expandAll turns a request to re-tag everything into one queued entry per
// deck, plus one per deck tag in the media database whose deck is gone.
func (t *Tagger) expandAll(ctx context.Context) error {
	t.mu.Lock()
	all, allSeq := t.all, t.allSeq
	t.mu.Unlock()
	if !all {
		return nil
	}
	deckIDs, err := deckIDsToTag(ctx, t.deps)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, deckID := range deckIDs {
		if _, queued := t.queued[deckID]; !queued {
			t.seq++
			t.queued[deckID] = t.seq
		}
	}
	// A request made while the decks were listed may need a newer listing.
	if t.allSeq == allSeq {
		t.all = false
	}
	t.saveLocked()
	return nil
}

// waitForMediaWrites blocks while a long-running job owns the media database,
// and reports false if ctx ended first. Scraping is not waited for: it can
// run for hours and commits in short transactions.
func (t *Tagger) waitForMediaWrites(ctx context.Context) bool {
	coordinator, err := database.GetMediaDBWriteCoordinator(t.deps.MediaDB)
	if err != nil {
		return ctx.Err() == nil
	}
	for {
		switch coordinator.ActiveMediaWriteOperation() {
		case database.MediaWriteOperationIndexing, database.MediaWriteOperationOptimization,
			database.MediaWriteOperationRecovery, database.MediaWriteOperationMaintenance:
		default:
			return ctx.Err() == nil
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(t.busyPoll):
		}
	}
}

// deckIDsToTag lists every stored deck, then every deck that still has tags
// in the media database but is no longer stored.
func deckIDsToTag(ctx context.Context, deps *ResolveDeps) ([]string, error) {
	stored, err := deps.UserDB.ListDecks()
	if err != nil {
		return nil, fmt.Errorf("list decks: %w", err)
	}
	tagged, err := deps.MediaDB.ListMediaTagValues(ctx, string(tags.TagTypeUser), tags.TagUserDeckPrefix)
	if err != nil {
		return nil, fmt.Errorf("list deck tags: %w", err)
	}
	deckIDs := make([]string, 0, len(stored)+len(tagged))
	known := make(map[string]struct{}, len(stored))
	for i := range stored {
		known[stored[i].DeckID] = struct{}{}
		deckIDs = append(deckIDs, stored[i].DeckID)
	}
	for _, value := range tagged {
		deckID, ok := tags.ParseDeckTag(tags.TagValue(value))
		if _, isKnown := known[deckID]; !ok || isKnown {
			continue
		}
		known[deckID] = struct{}{}
		deckIDs = append(deckIDs, deckID)
	}
	return deckIDs, nil
}
