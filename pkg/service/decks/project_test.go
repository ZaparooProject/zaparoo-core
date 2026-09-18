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
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTitleLaunch(t *testing.T) {
	t.Parallel()
	system, game, ok := decks.ParseTitleLaunch(`**launch.title:"SNES/Super Mario World (unlicensed:hack)"`)
	require.True(t, ok)
	assert.Equal(t, "SNES", system.ID)
	assert.Equal(t, "Super Mario World (unlicensed:hack)", game)

	for _, script := range []string{
		"**launch.random:SNES",
		"**launch.title:NoSlash",
		"**launch.title:NotASystem/Game",
		"**launch.title:SNES/A||**launch.title:SNES/B",
		"**launch:/roms/snes/game.sfc",
		"not zapscript ((",
	} {
		_, _, ok = decks.ParseTitleLaunch(script)
		assert.False(t, ok, script)
	}
}

// recordingTagQueue stands in for the background tagger so a test can see
// which decks a write queued.
type recordingTagQueue struct {
	decks []string
	mu    syncutil.Mutex
	all   int
}

func (q *recordingTagQueue) QueueDeckTags(deckIDs ...string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.decks = append(q.decks, deckIDs...)
}

func (q *recordingTagQueue) QueueAllDeckTags() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.all++
}

func (q *recordingTagQueue) queued() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return slices.Clone(q.decks)
}

type projectFixture struct {
	ctx      context.Context
	mediaDB  database.MediaDBI
	userDB   database.UserDBI
	db       *database.Database
	tagQueue *recordingTagQueue
	deps     *decks.ResolveDeps
	pathByID map[int64]string
	idByPath map[string]int64
}

func newProjectFixture(t *testing.T, paths ...string) *projectFixture {
	t.Helper()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	scantest.IndexMediaPaths(t, mediaDB, "NES", paths...)
	rows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	tagQueue := &recordingTagQueue{}
	f := &projectFixture{
		ctx:      context.Background(),
		mediaDB:  mediaDB,
		userDB:   userDB,
		db:       &database.Database{MediaDB: mediaDB, UserDB: userDB, DeckTags: tagQueue},
		tagQueue: tagQueue,
		deps:     &decks.ResolveDeps{MediaDB: mediaDB, UserDB: userDB, Cfg: cfg},
		pathByID: make(map[int64]string),
		idByPath: make(map[string]int64),
	}
	for i := range rows {
		f.pathByID[rows[i].DBID] = rows[i].Path
		f.idByPath[rows[i].Path] = rows[i].DBID
	}
	return f
}

// preferencesRevision returns the token that invalidates browse cursors, so a
// test can tell a write that changed a listing from one that did not.
func preferencesRevision(t *testing.T, f *projectFixture) string {
	t.Helper()
	revision, _, err := f.userDB.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
	require.NoError(t, err)
	return revision
}

// taggedPaths returns the paths carrying a deck's membership tag, found
// through the same tag filter browse and search use.
func (f *projectFixture) taggedPaths(t *testing.T, deckID string) []string {
	t.Helper()
	nes, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	ref := decks.DeckTagRef(deckID)
	results, err := f.mediaDB.SearchMediaWithFilters(f.ctx, &database.SearchFilters{
		Systems: []systemdefs.System{*nes},
		Tags:    []zapscript.TagFilter{{Type: ref.Type, Value: ref.Tag, Operator: zapscript.TagOperatorAND}},
		Limit:   100,
	})
	require.NoError(t, err)
	paths := make([]string, 0, len(results))
	for i := range results {
		paths = append(paths, results[i].Path)
	}
	return paths
}

func TestProjectDeck(t *testing.T) {
	t.Parallel()
	metroid := filepath.ToSlash(filepath.Join("roms", "NES", "Metroid (USA).nes"))
	contra := filepath.ToSlash(filepath.Join("roms", "NES", "Contra (USA).nes"))
	zelda := filepath.ToSlash(filepath.Join("roms", "NES", "Legend of Zelda, The (USA).nes"))
	f := newProjectFixture(t, metroid, contra, zelda)

	anchored, err := decks.ComposeMediaItem(f.ctx, f.mediaDB, "NES", metroid)
	require.NoError(t, err)
	deck := &database.Deck{
		DeckID: "0123456789ab", Name: "Project", Owned: true,
		Items: []database.DeckItem{
			anchored,
			// Added elsewhere: no anchor, resolves by title on this device.
			{
				Kind: database.DeckItemKindScript, Name: "Contra",
				ZapScript: decks.TitleLaunchScript("NES", "Contra", nil),
			},
			// Scripts that are not a title launch and cards never tag media.
			{Kind: database.DeckItemKindScript, Name: "Random", ZapScript: "**launch.random:NES"},
			{Kind: database.DeckItemKindCard, CardID: "abcd1234"},
			// A title nothing on this device matches.
			{
				Kind: database.DeckItemKindScript, Name: "Nope",
				ZapScript: decks.TitleLaunchScript("NES", "Nonexistent Game", nil),
			},
		},
	}
	require.NoError(t, f.userDB.CreateDeck(deck))

	relinked, err := decks.ProjectDeck(f.ctx, f.deps, deck.DeckID)
	require.NoError(t, err)
	assert.Equal(t, 1, relinked)
	assert.ElementsMatch(t, []string{metroid, contra}, f.taggedPaths(t, deck.DeckID))

	stored, err := f.userDB.GetDeck(deck.DeckID)
	require.NoError(t, err)
	assert.Equal(t, contra, stored.Items[1].Anchor.Path, "an item resolved by title is linked to the file it matched")
	assert.Equal(t, "Contra", stored.Items[1].Anchor.MediaName)
	assert.Contains(t, stored.Items[1].Anchor.Tags, "region:us")
	assert.False(t, stored.Items[2].HasAnchor())
	assert.False(t, stored.Items[4].HasAnchor())

	// The projection reads the deck as stored, so an edit moves the tag with
	// the items.
	_, err = f.userDB.UpdateDeck(deck.DeckID, func(edit *database.Deck) error {
		edit.Items = []database.DeckItem{edit.Items[1]}
		return nil
	})
	require.NoError(t, err)
	_, err = decks.ProjectDeck(f.ctx, f.deps, deck.DeckID)
	require.NoError(t, err)
	assert.Equal(t, []string{contra}, f.taggedPaths(t, deck.DeckID))

	// A deck that no longer exists keeps no tags.
	existed, err := f.userDB.DeleteDeck(deck.DeckID)
	require.NoError(t, err)
	require.True(t, existed)
	relinked, err = decks.ProjectDeck(f.ctx, f.deps, deck.DeckID)
	require.NoError(t, err)
	assert.Zero(t, relinked)
	assert.Empty(t, f.taggedPaths(t, deck.DeckID))
}

// An anchor to a file that is no longer indexed falls back to the title and
// re-links to the file that matches now.
func TestProjectDeckRelinksGoneAnchor(t *testing.T) {
	t.Parallel()
	moved := filepath.ToSlash(filepath.Join("roms", "NES", "Moved", "Metroid (USA).nes"))
	f := newProjectFixture(t, moved)

	deck := &database.Deck{
		DeckID: "0123456789ab", Name: "Moved", Owned: true,
		Items: []database.DeckItem{{
			Kind: database.DeckItemKindScript, Name: "Metroid",
			ZapScript: decks.TitleLaunchScript("NES", "Metroid", nil),
			Anchor: database.DeckItemAnchor{
				SystemID: "NES", Path: "roms/NES/Metroid (USA).nes", MediaName: "Metroid",
			},
		}},
	}
	require.NoError(t, f.userDB.CreateDeck(deck))
	_, err := decks.ProjectDeck(f.ctx, f.deps, deck.DeckID)
	require.NoError(t, err)
	assert.Equal(t, []string{moved}, f.taggedPaths(t, deck.DeckID))
	stored, err := f.userDB.GetDeck(deck.DeckID)
	require.NoError(t, err)
	assert.Equal(t, moved, stored.Items[0].Anchor.Path)
}

// A file that is only marked missing, like one on a drive that was unplugged
// for a scan, keeps the item's anchor. Its title match carries the tag
// meanwhile, and the tag returns to the file when it is indexed again. The
// item is only re-linked once the file's row is gone.
func TestProjectDeckKeepsMissingAnchor(t *testing.T) {
	t.Parallel()
	europe := filepath.ToSlash(filepath.Join("roms", "NES", "Metroid (Europe).nes"))
	usa := filepath.ToSlash(filepath.Join("roms", "NES", "Metroid (USA).nes"))
	f := newProjectFixture(t, europe)
	item, err := decks.ComposeMediaItem(f.ctx, f.mediaDB, "NES", europe)
	require.NoError(t, err)
	deck := &database.Deck{DeckID: "0123456789ab", Name: "Missing", Owned: true, Items: []database.DeckItem{item}}
	require.NoError(t, f.userDB.CreateDeck(deck))

	scantest.IndexMediaPaths(t, f.mediaDB, "NES", usa)
	relinked, err := decks.ProjectDeck(f.ctx, f.deps, deck.DeckID)
	require.NoError(t, err)
	assert.Zero(t, relinked)
	assert.Equal(t, []string{usa}, f.taggedPaths(t, deck.DeckID), "the title match stands in")
	stored, err := f.userDB.GetDeck(deck.DeckID)
	require.NoError(t, err)
	assert.Equal(t, europe, stored.Items[0].Anchor.Path, "the missing file keeps the anchor")

	scantest.IndexMediaPaths(t, f.mediaDB, "NES", europe, usa)
	_, err = decks.ProjectDeck(f.ctx, f.deps, deck.DeckID)
	require.NoError(t, err)
	assert.Equal(t, []string{europe}, f.taggedPaths(t, deck.DeckID), "the tag returns with the file")

	scantest.IndexMediaPaths(t, f.mediaDB, "NES", usa)
	_, err = f.mediaDB.CleanMediaOrphans(f.ctx)
	require.NoError(t, err)
	relinked, err = decks.ProjectDeck(f.ctx, f.deps, deck.DeckID)
	require.NoError(t, err)
	assert.Equal(t, 1, relinked)
	assert.Equal(t, []string{usa}, f.taggedPaths(t, deck.DeckID))
	stored, err = f.userDB.GetDeck(deck.DeckID)
	require.NoError(t, err)
	assert.Equal(t, usa, stored.Items[0].Anchor.Path, "a file removed for good is re-linked")
}

// A title match the resolver cached from an earlier launch reports full
// confidence. The projection must score the match itself, so a weak match is
// never tagged just because it was launched once.
func TestProjectDeckIgnoresCachedWeakMatch(t *testing.T) {
	t.Parallel()
	plain := filepath.ToSlash(filepath.Join("roms", "NES", "Metroid.nes"))
	f := newProjectFixture(t, plain)
	// Five of the six requested tags match and region conflicts, which scores
	// the only candidate 5/6 - 0.2: above the launch minimum, below acceptable.
	require.NoError(t, f.mediaDB.UpdateMediaTags(f.ctx, f.idByPath[plain], nil, []database.MediaTagRef{
		{Type: "developer", Tag: "a"},
		{Type: "publisher", Tag: "b"},
		{Type: "year", Tag: "1986"},
		{Type: "video", Tag: "ntsc"},
		{Type: "edition", Tag: "x"},
		{Type: "region", Tag: "us"},
	}))
	game := "Metroid (developer:a) (publisher:b) (year:1986) (video:ntsc) (edition:x) (region:eu)"
	nes, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	launched, err := titles.ResolveTitle(f.ctx, &titles.ResolveParams{
		MediaDB: f.mediaDB, Cfg: f.deps.Cfg, SystemID: nes.ID, GameName: game, MediaType: nes.GetMediaType(),
	})
	require.NoError(t, err)
	require.Less(t, launched.Confidence, titles.ConfidenceAcceptable)
	require.GreaterOrEqual(t, launched.Confidence, titles.ConfidenceMinimum)
	filters, _ := titles.ExtractCanonicalTagsFromParens(game)
	require.Eventually(t, func() bool {
		_, _, hit := f.mediaDB.GetCachedSlugResolution(f.ctx, nes.ID, "metroid", filters)
		return hit
	}, 5*time.Second, 10*time.Millisecond, "the launch caches its match")

	deck := &database.Deck{DeckID: "0123456789ab", Name: "Weak", Owned: true, Items: []database.DeckItem{{
		Kind: database.DeckItemKindScript, Name: "Metroid",
		ZapScript: `**launch.title:"NES/` + game + `"`,
	}}}
	require.NoError(t, f.userDB.CreateDeck(deck))
	relinked, err := decks.ProjectDeck(f.ctx, f.deps, deck.DeckID)
	require.NoError(t, err)
	assert.Zero(t, relinked)
	assert.Empty(t, f.taggedPaths(t, deck.DeckID))
}

// failingSlugSearch is a media database whose title lookups fail.
type failingSlugSearch struct {
	database.MediaDBI
}

func (failingSlugSearch) SearchMediaBySlug(
	context.Context, string, string, []zapscript.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	return nil, errors.New("disk I/O error")
}

// A title lookup that fails says nothing about where the item's file is, so
// the projection stops and the deck keeps the tags it had, rather than
// dropping the item's file from the deck.
func TestProjectDeckKeepsTagsWhenLookupFails(t *testing.T) {
	t.Parallel()
	contra := filepath.ToSlash(filepath.Join("roms", "NES", "Contra (USA).nes"))
	f := newProjectFixture(t, contra)
	deck := &database.Deck{DeckID: "0123456789ab", Name: "Lookup", Owned: true, Items: []database.DeckItem{{
		Kind: database.DeckItemKindScript, Name: "Contra", ZapScript: decks.TitleLaunchScript("NES", "Contra", nil),
	}}}
	require.NoError(t, f.userDB.CreateDeck(deck))
	_, err := decks.ProjectDeck(f.ctx, f.deps, deck.DeckID)
	require.NoError(t, err)
	require.Equal(t, []string{contra}, f.taggedPaths(t, deck.DeckID))

	// The item is linked now, so drop the link to make it resolve by title.
	stored, err := f.userDB.GetDeck(deck.DeckID)
	require.NoError(t, err)
	require.NoError(t, f.userDB.SetDeckItemAnchor(stored.Items[0].DBID, &database.DeckItemAnchor{}))

	failing := *f.deps
	failing.MediaDB = failingSlugSearch{MediaDBI: f.mediaDB}
	_, err = decks.ProjectDeck(f.ctx, &failing, deck.DeckID)
	require.ErrorContains(t, err, "disk I/O error")
	assert.Equal(t, []string{contra}, f.taggedPaths(t, deck.DeckID))

	// A title with nothing to match is a miss, not a failure.
	_, err = f.userDB.UpdateDeck(deck.DeckID, func(edit *database.Deck) error {
		edit.Items = []database.DeckItem{{
			Kind: database.DeckItemKindScript, Name: "Blank", ZapScript: `**launch.title:"NES/!!!"`,
		}}
		return nil
	})
	require.NoError(t, err)
	_, err = decks.ProjectDeck(f.ctx, f.deps, deck.DeckID)
	require.NoError(t, err)
	assert.Empty(t, f.taggedPaths(t, deck.DeckID))
}

// Projections of one deck that race with its edits end on the deck as last
// stored, whatever order they ran in. Items resolve by title through a slow
// launcher lookup, which widens the window between reading a deck and writing
// its tags.
func TestProjectDeckConcurrentEditsEndOnStoredDeck(t *testing.T) {
	t.Parallel()
	names := []string{"Metroid", "Contra", "Kid Icarus", "Gradius", "Castlevania", "Excitebike"}
	paths := make([]string, 0, len(names))
	items := make([]database.DeckItem, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.ToSlash(filepath.Join("roms", "NES", name+" (USA).nes")))
		items = append(items, database.DeckItem{
			Kind: database.DeckItemKindScript, Name: name, ZapScript: decks.TitleLaunchScript("NES", name, nil),
		})
	}
	f := newProjectFixture(t, paths...)
	deps := *f.deps
	var calls atomic.Int64
	deps.LaunchersForSystem = func(string) []platforms.Launcher {
		time.Sleep(time.Duration(calls.Add(1)%3) * time.Millisecond)
		return nil
	}
	require.NoError(t, f.userDB.CreateDeck(&database.Deck{DeckID: "0123456789ab", Name: "Race", Owned: true}))

	var wg sync.WaitGroup
	for worker := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := range 5 {
				pick := (worker + round) % len(items)
				_, err := f.userDB.UpdateDeck("0123456789ab", func(edit *database.Deck) error {
					edit.Items = []database.DeckItem{items[pick]}
					return nil
				})
				assert.NoError(t, err)
				_, err = decks.ProjectDeck(f.ctx, &deps, "0123456789ab")
				assert.NoError(t, err)
			}
		}()
	}
	wg.Wait()

	stored, err := f.userDB.GetDeck("0123456789ab")
	require.NoError(t, err)
	require.Len(t, stored.Items, 1)
	want := filepath.ToSlash(filepath.Join("roms", "NES", stored.Items[0].Name+" (USA).nes"))
	assert.Equal(t, []string{want}, f.taggedPaths(t, "0123456789ab"))
}
