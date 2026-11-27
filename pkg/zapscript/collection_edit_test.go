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

package zapscript

import (
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var runningMetroid = &models.ActiveMedia{SystemID: "NES", Path: "roms/nes/Metroid.nes", MediaID: 12}

func editEnv(
	t *testing.T, library platforms.MediaLibrary, cmdName, link string,
) (platforms.CmdEnv, *database.Database) {
	t.Helper()
	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	env := platforms.CmdEnv{
		ServiceCtx: t.Context(),
		Cmd:        zapscript.Command{Name: cmdName, Args: []string{link}},
		Database:   db,
		Library:    library,
	}
	runningMedia(&env, runningMetroid)
	return env, db
}

func TestCmdAdd_IsRegistered(t *testing.T) {
	t.Parallel()
	for _, name := range []string{zapscript.ZapScriptCmdAdd, zapscript.ZapScriptCmdRemove} {
		assert.True(t, IsValidCommand(name), name)
		assert.False(t, IsMediaLaunchingCommand(name), name)
		assert.False(t, IsMediaDisruptingCommand(name), name)
	}
}

func TestCmdAdd_FlagsTheRunningMedia(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{}
	for link, flag := range map[string]string{
		"favorites://": "favorite", "liked://": "liked", "disliked://": "disliked",
		"PlayLater://": "playlater", "hidden://": "hidden",
	} {
		env, _ := editEnv(t, library, zapscript.ZapScriptCmdAdd, link)
		result, err := cmdAdd(newPlaylistTestPlatform(), env)
		require.NoError(t, err, link)
		assert.False(t, result.MediaChanged, link)
		require.NotEmpty(t, library.flags)
		got := library.flags[len(library.flags)-1]
		assert.Equal(t, flagCall{
			flag: flag, on: true,
			ref: platforms.MediaRef{SystemID: "NES", Path: "roms/nes/Metroid.nes", MediaID: 12},
		}, got, link)
	}
}

func TestCmdRemove_UnflagsTheRunningMedia(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{}
	env, _ := editEnv(t, library, zapscript.ZapScriptCmdRemove, "favorites://")
	_, err := cmdRemove(newPlaylistTestPlatform(), env)
	require.NoError(t, err)
	require.Len(t, library.flags, 1)
	assert.Equal(t, "favorite", library.flags[0].flag)
	assert.False(t, library.flags[0].on)
}

func TestCmdAdd_NothingRunning(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{}
	env, _ := editEnv(t, library, zapscript.ZapScriptCmdAdd, "favorites://")
	runningMedia(&env, nil)
	_, err := cmdAdd(newPlaylistTestPlatform(), env)
	require.ErrorIs(t, err, ErrNoActiveMedia)
	assert.True(t, IsExpectedRunError(err))
	assert.Empty(t, library.flags)
}

func TestCmdAdd_ComputedListsCannotBeEdited(t *testing.T) {
	t.Parallel()
	for _, link := range []string{
		"recommended://", "similar://", "versions://", "top://", "kodi-show://1/Show", "/roms/nes/a.nes",
		"favorites://extra",
	} {
		library := &fakeLibrary{}
		env, _ := editEnv(t, library, zapscript.ZapScriptCmdAdd, link)
		_, err := cmdAdd(newPlaylistTestPlatform(), env)
		require.ErrorIs(t, err, ErrInvalidArguments, link)
		assert.Empty(t, library.flags, link)
		assert.Empty(t, library.deckEdits, link)
	}
}

func TestCmdAdd_LibraryRefusalIsExpected(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{err: platforms.ErrLibraryRefused}
	env, _ := editEnv(t, library, zapscript.ZapScriptCmdAdd, "favorites://")
	_, err := cmdAdd(newPlaylistTestPlatform(), env)
	require.ErrorIs(t, err, platforms.ErrLibraryRefused)
	assert.True(t, IsExpectedRunError(err))
}

func TestCmdAdd_DeckByIDOrName(t *testing.T) {
	t.Parallel()
	for _, link := range []string{"deck://0123456789AB", "deck://weekend games", "DECK:// Weekend Games "} {
		library := &fakeLibrary{}
		env, db := editEnv(t, library, zapscript.ZapScriptCmdAdd, link)
		require.NoError(t, db.UserDB.CreateDeck(&database.Deck{
			DeckID: "0123456789ab", Name: "Weekend Games", Owned: true,
		}))
		_, err := cmdAdd(newPlaylistTestPlatform(), env)
		require.NoError(t, err, link)
		require.Len(t, library.deckEdits, 1, link)
		assert.Equal(t, deckCall{
			deckID: "0123456789ab", add: true,
			ref: platforms.MediaRef{SystemID: "NES", Path: "roms/nes/Metroid.nes", MediaID: 12},
		}, library.deckEdits[0], link)
	}
}

func TestCmdRemove_DeckAlreadyWithoutTheMediaSucceeds(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{unchanged: true}
	env, db := editEnv(t, library, zapscript.ZapScriptCmdRemove, "deck://Weekend")
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{DeckID: "0123456789ab", Name: "Weekend", Owned: true}))
	_, err := cmdRemove(newPlaylistTestPlatform(), env)
	require.NoError(t, err)
	require.Len(t, library.deckEdits, 1)
	assert.False(t, library.deckEdits[0].add)
}

func TestCmdAdd_DeckNameSharedByTwoDecks(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{}
	env, db := editEnv(t, library, zapscript.ZapScriptCmdAdd, "deck://Weekend")
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{DeckID: "0123456789ab", Name: "Weekend", Owned: true}))
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{DeckID: "0123456789ac", Name: "weekend", Owned: true}))
	_, err := cmdAdd(newPlaylistTestPlatform(), env)
	require.ErrorIs(t, err, decks.ErrAmbiguousDeckName)
	require.ErrorIs(t, err, ErrInvalidArguments)
	assert.Empty(t, library.deckEdits)
}

func TestCmdAdd_NoSuchDeck(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{}
	env, _ := editEnv(t, library, zapscript.ZapScriptCmdAdd, "deck://Nothing Here")
	_, err := cmdAdd(newPlaylistTestPlatform(), env)
	require.ErrorIs(t, err, ErrDeckNotFound)
	assert.True(t, IsExpectedRunError(err))
	assert.Empty(t, library.deckEdits)
}

func TestCmdPlaylistLoad_DeckByName(t *testing.T) {
	t.Parallel()
	env, db, queue := deckTestEnv(t, "deck://weekend")
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Weekend", Owned: true,
		Items: []database.DeckItem{{
			Kind: database.DeckItemKindScript, Name: "First", ZapScript: "**launch.system:NES",
		}},
	}))
	_, err := cmdPlaylistLoad(newPlaylistTestPlatform(), env)
	require.NoError(t, err)
	pls := <-queue
	assert.Equal(t, "deck://0123456789ab", pls.ID, "a deck named by its name opens as its own URI")
	assert.Equal(t, "0123456789ab", pls.DeckID)
}

func TestCmdPlaylistLoad_DeckIDBeatsName(t *testing.T) {
	t.Parallel()
	env, db, queue := deckTestEnv(t, "deck://0123456789ab")
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "By ID", Owned: true,
		Items: []database.DeckItem{{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**launch.system:NES"}},
	}))
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{
		DeckID: "0123456789ac", Name: "0123456789ab", Owned: true,
		Items: []database.DeckItem{{Kind: database.DeckItemKindScript, Name: "B", ZapScript: "**launch.system:SNES"}},
	}))
	_, err := cmdPlaylistLoad(newPlaylistTestPlatform(), env)
	require.NoError(t, err)
	pls := <-queue
	assert.Equal(t, "By ID", pls.Name)
}

func TestCmdAdd_NeedsOneCollection(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{}
	env, _ := editEnv(t, library, zapscript.ZapScriptCmdAdd, "")
	_, err := cmdAdd(newPlaylistTestPlatform(), env)
	require.ErrorIs(t, err, ErrRequiredArgs)

	env.Cmd.Args = []string{"favorites://", "liked://"}
	_, err = cmdAdd(newPlaylistTestPlatform(), env)
	require.ErrorIs(t, err, ErrArgCount)
	assert.Empty(t, library.flags)
}

func TestCmdAdd_NeedsTheLibrary(t *testing.T) {
	t.Parallel()
	env, _ := editEnv(t, nil, zapscript.ZapScriptCmdAdd, "favorites://")
	env.Library = nil
	_, err := cmdAdd(newPlaylistTestPlatform(), env)
	require.ErrorIs(t, err, ErrNoMediaLibrary)
}

func TestCmdAdd_DeckEditFailureIsReported(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{err: platforms.ErrLibraryRefused}
	env, db := editEnv(t, library, zapscript.ZapScriptCmdAdd, "deck://Weekend")
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{DeckID: "0123456789ab", Name: "Weekend", Owned: true}))
	_, err := cmdAdd(newPlaylistTestPlatform(), env)
	require.ErrorIs(t, err, platforms.ErrLibraryRefused)
	assert.Contains(t, err.Error(), "Weekend")
}
