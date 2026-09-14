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
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
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

type projectFixture struct {
	ctx      context.Context
	mediaDB  database.MediaDBI
	userDB   database.UserDBI
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
	f := &projectFixture{
		ctx:      context.Background(),
		mediaDB:  mediaDB,
		userDB:   userDB,
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

	count, err := decks.ProjectDeck(f.ctx, f.deps, deck)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.ElementsMatch(t, []string{metroid, contra}, f.taggedPaths(t, deck.DeckID))

	stored, err := f.userDB.GetDeck(deck.DeckID)
	require.NoError(t, err)
	assert.Equal(t, contra, stored.Items[1].Anchor.Path, "an item resolved by title is linked to the file it matched")
	assert.Equal(t, "Contra", stored.Items[1].Anchor.MediaName)
	assert.Contains(t, stored.Items[1].Anchor.Tags, "region:us")
	assert.False(t, stored.Items[2].HasAnchor())
	assert.False(t, stored.Items[4].HasAnchor())

	// Editing the deck down to one item moves the tag with it.
	stored.Items = []database.DeckItem{stored.Items[1]}
	require.NoError(t, f.userDB.ReplaceDeckItems(stored.DeckID, stored.Items))
	count, err = decks.ProjectDeck(f.ctx, f.deps, stored)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, []string{contra}, f.taggedPaths(t, deck.DeckID))

	require.NoError(t, decks.ClearDeckProjection(f.ctx, f.mediaDB, deck.DeckID))
	assert.Empty(t, f.taggedPaths(t, deck.DeckID))
}

// An anchor to a file that is no longer indexed falls back to the title and
// re-links to the file that matches now.
func TestProjectDeckRelinksMissingAnchor(t *testing.T) {
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
	count, err := decks.ProjectDeck(f.ctx, f.deps, deck)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, []string{moved}, f.taggedPaths(t, deck.DeckID))
	stored, err := f.userDB.GetDeck(deck.DeckID)
	require.NoError(t, err)
	assert.Equal(t, moved, stored.Items[0].Anchor.Path)
}

// After the media database is rebuilt no deck tags exist; re-applying puts
// every deck's tags back.
func TestReapplyDeckTags(t *testing.T) {
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

	projected, err := decks.ReapplyDeckTags(f.ctx, f.deps)
	require.NoError(t, err)
	assert.Equal(t, 2, projected, "decks with no items have nothing to project")
	assert.Equal(t, []string{metroid}, f.taggedPaths(t, "aaaaaaaaaaaa"))
	assert.ElementsMatch(t, []string{metroid, contra}, f.taggedPaths(t, "bbbbbbbbbbbb"))

	// Idempotent: a second pass changes nothing.
	projected, err = decks.ReapplyDeckTags(f.ctx, f.deps)
	require.NoError(t, err)
	assert.Equal(t, 2, projected)
	assert.ElementsMatch(t, []string{metroid, contra}, f.taggedPaths(t, "bbbbbbbbbbbb"))
}
