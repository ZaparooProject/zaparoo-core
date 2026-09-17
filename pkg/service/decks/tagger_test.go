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

package decks_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// savedQueue returns the tagger queue as stored in the user database.
func (f *projectFixture) savedQueue(t *testing.T) string {
	t.Helper()
	raw, _, err := f.userDB.GetDeviceState(database.DeviceStateKeyDeckTagsQueue)
	require.NoError(t, err)
	return raw
}

// After the media database is rebuilt no deck tags exist; re-tagging every
// deck puts them back and removes the tags of decks that are gone.
func TestTaggerQueueAll(t *testing.T) {
	t.Parallel()
	metroid := filepath.ToSlash(filepath.Join("roms", "NES", "Metroid (USA).nes"))
	contra := filepath.ToSlash(filepath.Join("roms", "NES", "Contra (USA).nes"))
	f := newProjectFixture(t, metroid, contra)

	first, err := decks.ComposeMediaItem(f.ctx, f.mediaDB, "NES", metroid)
	require.NoError(t, err)
	second, err := decks.ComposeMediaItem(f.ctx, f.mediaDB, "NES", contra)
	require.NoError(t, err)
	require.NoError(t, f.userDB.CreateDeck(&database.Deck{
		DeckID: "aaaaaaaaaaaa", Name: "A", Owned: true, Items: []database.DeckItem{first},
	}))
	require.NoError(t, f.userDB.CreateDeck(&database.Deck{
		DeckID: "bbbbbbbbbbbb", Name: "B", Owned: true, Items: []database.DeckItem{first, second},
	}))
	require.NoError(t, f.userDB.CreateDeck(&database.Deck{DeckID: "cccccccccccc", Name: "Empty", Owned: true}))
	// Tags a missed update left behind: a deleted deck, and a deck since
	// emptied.
	for _, deckID := range []string{"dddddddddddd", "cccccccccccc"} {
		_, err = f.mediaDB.SetMediaTagMembership(f.ctx, decks.DeckTagRef(deckID), []int64{f.idByPath[contra]})
		require.NoError(t, err)
	}

	tagger := decks.NewTagger(f.deps, nil)
	tagger.QueueAllDeckTags()
	assert.JSONEq(t, `{"all": true}`, f.savedQueue(t), "the request is saved before any work")
	assert.False(t, tagger.Drain(f.ctx))

	assert.Equal(t, []string{metroid}, f.taggedPaths(t, "aaaaaaaaaaaa"))
	assert.ElementsMatch(t, []string{metroid, contra}, f.taggedPaths(t, "bbbbbbbbbbbb"))
	assert.Empty(t, f.taggedPaths(t, "cccccccccccc"), "an emptied deck loses its tags")
	assert.Empty(t, f.taggedPaths(t, "dddddddddddd"), "a deleted deck loses its tags")
	values, err := f.mediaDB.ListMediaTagValues(f.ctx, "user", "deck:")
	require.NoError(t, err)
	assert.Equal(t, []string{"deck:aaaaaaaaaaaa", "deck:bbbbbbbbbbbb"}, values)
	assert.Empty(t, f.savedQueue(t), "a finished queue leaves nothing saved")

	// Nothing queued: nothing to do.
	assert.False(t, tagger.Drain(f.ctx))
}

// Work queued before a restart is finished by the next run.
func TestTaggerQueueSurvivesRestart(t *testing.T) {
	t.Parallel()
	metroid := filepath.ToSlash(filepath.Join("roms", "NES", "Metroid (USA).nes"))
	f := newProjectFixture(t, metroid)
	item, err := decks.ComposeMediaItem(f.ctx, f.mediaDB, "NES", metroid)
	require.NoError(t, err)
	require.NoError(t, f.userDB.CreateDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Saved", Owned: true, Items: []database.DeckItem{item},
	}))

	decks.NewTagger(f.deps, nil).QueueDeckTags("0123456789ab")
	assert.JSONEq(t, `{"decks": ["0123456789ab"]}`, f.savedQueue(t))
	assert.Empty(t, f.taggedPaths(t, "0123456789ab"), "queueing writes no tags")

	restarted := decks.NewTagger(f.deps, nil)
	assert.False(t, restarted.Drain(f.ctx))
	assert.Equal(t, []string{metroid}, f.taggedPaths(t, "0123456789ab"))
	assert.Empty(t, f.savedQueue(t))
}

// A deck whose tags cannot be written stays queued, saved, for a later try.
func TestTaggerKeepsFailedDeckQueued(t *testing.T) {
	t.Parallel()
	contra := filepath.ToSlash(filepath.Join("roms", "NES", "Contra (USA).nes"))
	f := newProjectFixture(t, contra)
	require.NoError(t, f.userDB.CreateDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Fails", Owned: true, Items: []database.DeckItem{{
			Kind: database.DeckItemKindScript, Name: "Contra",
			ZapScript: decks.TitleLaunchScript("NES", "Contra", nil),
		}},
	}))

	failing := *f.deps
	failing.MediaDB = failingSlugSearch{MediaDBI: f.mediaDB}
	tagger := decks.NewTagger(&failing, nil)
	tagger.QueueDeckTags("0123456789ab")
	assert.True(t, tagger.Drain(f.ctx), "a failed deck is reported")
	assert.JSONEq(t, `{"decks": ["0123456789ab"]}`, f.savedQueue(t), "and kept")
	assert.True(t, tagger.Drain(f.ctx), "and tried again on the next pass")

	assert.False(t, decks.NewTagger(f.deps, nil).Drain(f.ctx))
	assert.Equal(t, []string{contra}, f.taggedPaths(t, "0123456789ab"))
}

// A deck edited while its tags are being written is written again, so the
// tags end on the edit.
func TestTaggerRequeueDuringProjection(t *testing.T) {
	t.Parallel()
	metroid := filepath.ToSlash(filepath.Join("roms", "NES", "Metroid (USA).nes"))
	contra := filepath.ToSlash(filepath.Join("roms", "NES", "Contra (USA).nes"))
	f := newProjectFixture(t, metroid, contra)
	require.NoError(t, f.userDB.CreateDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Edited", Owned: true, Items: []database.DeckItem{{
			Kind: database.DeckItemKindScript, Name: "Metroid",
			ZapScript: decks.TitleLaunchScript("NES", "Metroid", nil),
		}},
	}))

	deps := *f.deps
	var tagger *decks.Tagger
	var edited atomic.Bool
	deps.LaunchersForSystem = func(string) []platforms.Launcher {
		// Edit the deck while its first projection is resolving titles.
		if edited.CompareAndSwap(false, true) {
			_, err := f.userDB.UpdateDeck("0123456789ab", func(deck *database.Deck) error {
				deck.Items = []database.DeckItem{{
					Kind: database.DeckItemKindScript, Name: "Contra",
					ZapScript: decks.TitleLaunchScript("NES", "Contra", nil),
				}}
				return nil
			})
			assert.NoError(t, err)
			tagger.QueueDeckTags("0123456789ab")
		}
		return nil
	}
	tagger = decks.NewTagger(&deps, nil)
	tagger.QueueDeckTags("0123456789ab")
	assert.False(t, tagger.Drain(f.ctx))
	assert.Equal(t, []string{contra}, f.taggedPaths(t, "0123456789ab"))
	assert.Empty(t, f.savedQueue(t))
}

// The tagger waits while a long-running job owns the media database, and
// not while it is being scraped.
func TestTaggerWaitsForMediaWrites(t *testing.T) {
	t.Parallel()
	for _, op := range []database.MediaWriteOperation{
		database.MediaWriteOperationIndexing,
		database.MediaWriteOperationOptimization,
		database.MediaWriteOperationRecovery,
		database.MediaWriteOperationMaintenance,
	} {
		t.Run(string(op), func(t *testing.T) {
			t.Parallel()
			f, tagger := newWaitFixture(t)
			coordinator, err := database.GetMediaDBWriteCoordinator(f.mediaDB)
			require.NoError(t, err)
			lease, err := coordinator.AcquireMediaWrite(op)
			require.NoError(t, err)

			done := make(chan bool, 1)
			go func() { done <- tagger.Drain(f.ctx) }()
			select {
			case <-done:
				t.Fatalf("the tagger ran during %s", op)
			case <-time.After(100 * time.Millisecond):
			}
			assert.Empty(t, f.taggedPaths(t, "0123456789ab"))

			lease.Release()
			select {
			case failed := <-done:
				assert.False(t, failed)
			case <-time.After(10 * time.Second):
				t.Fatalf("the tagger did not resume after %s", op)
			}
			assert.Len(t, f.taggedPaths(t, "0123456789ab"), 1)
		})
	}

	t.Run("scraping", func(t *testing.T) {
		t.Parallel()
		f, tagger := newWaitFixture(t)
		coordinator, err := database.GetMediaDBWriteCoordinator(f.mediaDB)
		require.NoError(t, err)
		lease, err := coordinator.AcquireMediaWrite(database.MediaWriteOperationScraping)
		require.NoError(t, err)
		defer lease.Release()
		assert.False(t, tagger.Drain(f.ctx))
		assert.Len(t, f.taggedPaths(t, "0123456789ab"), 1)
	})

	t.Run("cancelled", func(t *testing.T) {
		t.Parallel()
		f, tagger := newWaitFixture(t)
		coordinator, err := database.GetMediaDBWriteCoordinator(f.mediaDB)
		require.NoError(t, err)
		lease, err := coordinator.AcquireMediaWrite(database.MediaWriteOperationIndexing)
		require.NoError(t, err)
		defer lease.Release()
		ctx, cancel := context.WithTimeout(f.ctx, 50*time.Millisecond)
		defer cancel()
		assert.False(t, tagger.Drain(ctx), "cancelling while waiting returns without work")
		assert.JSONEq(t, `{"decks": ["0123456789ab"]}`, f.savedQueue(t))
	})
}

// newWaitFixture returns a fixture holding one anchored deck and a fast
// polling tagger with that deck queued.
func newWaitFixture(t *testing.T) (*projectFixture, *decks.Tagger) {
	t.Helper()
	metroid := filepath.ToSlash(filepath.Join("roms", "NES", "Metroid (USA).nes"))
	f := newProjectFixture(t, metroid)
	item, err := decks.ComposeMediaItem(f.ctx, f.mediaDB, "NES", metroid)
	require.NoError(t, err)
	require.NoError(t, f.userDB.CreateDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Wait", Owned: true, Items: []database.DeckItem{item},
	}))
	tagger := decks.NewTagger(f.deps, nil)
	tagger.SetBusyPoll(5 * time.Millisecond)
	tagger.QueueDeckTags("0123456789ab")
	return f, tagger
}

// Run works through queued decks as they arrive, reports items it linked,
// and returns when its context ends.
func TestTaggerRun(t *testing.T) {
	t.Parallel()
	contra := filepath.ToSlash(filepath.Join("roms", "NES", "Contra (USA).nes"))
	f := newProjectFixture(t, contra)
	require.NoError(t, f.userDB.CreateDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Run", Owned: true, Items: []database.DeckItem{{
			Kind: database.DeckItemKindScript, Name: "Contra",
			ZapScript: decks.TitleLaunchScript("NES", "Contra", nil),
		}},
	}))

	relinked := make(chan string, 1)
	tagger := decks.NewTagger(f.deps, func(deckID string) { relinked <- deckID })
	ctx, cancel := context.WithCancel(f.ctx)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		tagger.Run(ctx)
	}()

	tagger.QueueDeckTags("0123456789ab")
	select {
	case deckID := <-relinked:
		assert.Equal(t, "0123456789ab", deckID)
	case <-time.After(10 * time.Second):
		t.Fatal("the queued deck was not tagged")
	}
	assert.Equal(t, []string{contra}, f.taggedPaths(t, "0123456789ab"))

	// A deck whose items are already linked is tagged without a report.
	tagger.QueueDeckTags("0123456789ab")
	require.Eventually(t, func() bool { return f.savedQueue(t) == "" }, 10*time.Second, 5*time.Millisecond)
	assert.Empty(t, relinked)

	cancel()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
}
