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
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testShowPath = "kodi-show://42/Purpley"

var testShowItems = []platforms.CollectionItem{
	{Name: "Purpley - One", Path: "kodi-episode://1/Purpley%20-%20One"},
	{Name: "Purpley - Two", Path: "kodi-episode://2/Purpley%20-%20Two"},
	{Name: "Purpley - Three", Path: "kodi-episode://3/Purpley%20-%20Three"},
}

// collectionLaunchers is a Kodi-shaped pair: a show launcher that can list
// its episodes, and an episode launcher that cannot.
func collectionLaunchers(items []platforms.CollectionItem, listErr error, listed *[]string) []platforms.Launcher {
	return []platforms.Launcher{
		{
			ID:      "KodiTVShow",
			Schemes: []string{"kodi-show"},
			Collection: func(_ context.Context, _ *config.Instance, path string) ([]platforms.CollectionItem, error) {
				if listed != nil {
					*listed = append(*listed, path)
				}
				return items, listErr
			},
		},
		{ID: "KodiTVEpisode", Schemes: []string{"kodi-episode"}},
	}
}

func collectionPlatform(cfg *config.Instance, launchers []platforms.Launcher) *mocks.MockPlatform {
	mp := mocks.NewMockPlatform()
	mp.On("Launchers", cfg).Return(launchers).Maybe()
	return mp
}

func randomCollectionEnv(cfg *config.Instance, arg string) platforms.CmdEnv {
	return platforms.CmdEnv{
		Cmd:      zapscript.Command{Name: zapscript.ZapScriptCmdLaunchRandom, Args: []string{arg}},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: testhelpers.NewMockMediaDBI()},
	}
}

func TestCmdRandom_CollectionLaunchesExactlyOneItem(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	var listed []string
	mp := collectionPlatform(cfg, collectionLaunchers(testShowItems, nil, &listed))
	var launched []string
	mp.On("LaunchMedia", cfg, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			path, ok := args.Get(1).(string)
			require.True(t, ok)
			launched = append(launched, path)
		}).Return(nil)

	result, err := cmdRandom(mp, randomCollectionEnv(cfg, testShowPath))

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	assert.Equal(t, []string{testShowPath}, listed)
	require.Len(t, launched, 1, "a collection launch plays one item, not the whole collection")
	paths := make([]string, 0, len(testShowItems))
	for _, item := range testShowItems {
		paths = append(paths, item.Path)
	}
	assert.Contains(t, paths, launched[0])
}

func TestCmdRandom_CollectionPicksVaryAcrossRolls(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mp := collectionPlatform(cfg, collectionLaunchers(testShowItems, nil, nil))
	seen := map[string]struct{}{}
	mp.On("LaunchMedia", cfg, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			path, ok := args.Get(1).(string)
			require.True(t, ok)
			seen[path] = struct{}{}
		}).Return(nil)

	for range 200 {
		_, err := cmdRandom(mp, randomCollectionEnv(cfg, testShowPath))
		require.NoError(t, err)
	}
	assert.Len(t, seen, len(testShowItems), "every item of the collection can be picked")
}

func TestCmdRandom_CollectionErrors(t *testing.T) {
	t.Parallel()

	t.Run("empty collection", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Instance{}
		mp := collectionPlatform(cfg, collectionLaunchers(nil, nil, nil))
		_, err := cmdRandom(mp, randomCollectionEnv(cfg, testShowPath))
		require.ErrorContains(t, err, "no items in collection")
		mp.AssertNotCalled(t, "LaunchMedia", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("listing fails", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Instance{}
		mp := collectionPlatform(cfg, collectionLaunchers(nil, errors.New("kodi down"), nil))
		_, err := cmdRandom(mp, randomCollectionEnv(cfg, testShowPath))
		require.ErrorContains(t, err, "kodi down")
		mp.AssertNotCalled(t, "LaunchMedia", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("tags do not apply", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Instance{}
		mp := collectionPlatform(cfg, collectionLaunchers(testShowItems, nil, nil))
		env := randomCollectionEnv(cfg, testShowPath)
		env.Cmd.AdvArgs = zapscript.NewAdvArgs(map[string]string{string(zapscript.KeyTags): "genre:comedy"})
		_, err := cmdRandom(mp, env)
		require.ErrorIs(t, err, ErrInvalidArguments)
		mp.AssertNotCalled(t, "LaunchMedia", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})
}

func TestResolveCollection_OnlyLaunchersThatListCollections(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mp := collectionPlatform(cfg, collectionLaunchers(testShowItems, nil, nil))
	env := randomCollectionEnv(cfg, "")

	for _, arg := range []string{
		"kodi-episode://1/One",
		"SNES/Metroid",
		"all",
		"unknown-scheme://thing",
	} {
		_, ok, err := resolveCollection(mp, &env, arg)
		require.NoError(t, err, arg)
		assert.False(t, ok, "%s is not a collection", arg)
	}

	coll, ok, err := resolveCollection(mp, &env, testShowPath)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "Purpley", coll.name)
	assert.Equal(t, testShowPath, coll.id)
	assert.True(t, coll.media)
	assert.Len(t, coll.items, len(testShowItems))
	assert.False(t, coll.unsafe)

	env.Unsafe = true
	coll, _, err = resolveCollection(mp, &env, testShowPath)
	require.NoError(t, err)
	assert.True(t, coll.unsafe, "a collection opened by an untrusted script stays untrusted")
}

func TestCmdPlaylistPlay_CollectionMakesAPlaylistOfItsItems(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mp := collectionPlatform(cfg, collectionLaunchers(testShowItems, nil, nil))
	queue := make(chan *playlists.Playlist, 1)
	env := randomCollectionEnv(cfg, testShowPath)
	env.Cmd.Name = zapscript.ZapScriptCmdPlaylistPlay
	env.Playlist = playlists.PlaylistController{Queue: queue}

	result, err := cmdPlaylistPlay(mp, env)

	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	pls := <-queue
	assert.Equal(t, "Purpley", pls.Name)
	assert.True(t, pls.Playing)
	assert.Empty(t, pls.DeckID)
	require.Len(t, pls.Items, len(testShowItems))
	for i, item := range testShowItems {
		assert.Equal(t, item.Path, pls.Items[i].ZapScript)
		assert.Equal(t, item.Name, pls.Items[i].Name)
	}
}

func TestCmdPlaylistPlay_CollectionShuffleKeepsEveryItem(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mp := collectionPlatform(cfg, collectionLaunchers(testShowItems, nil, nil))
	queue := make(chan *playlists.Playlist, 1)
	env := randomCollectionEnv(cfg, testShowPath)
	env.Cmd.Name = zapscript.ZapScriptCmdPlaylistPlay
	env.Cmd.AdvArgs = zapscript.NewAdvArgs(map[string]string{string(zapscript.KeyMode): "shuffle"})
	env.Playlist = playlists.PlaylistController{Queue: queue}
	snapshot := slices.Clone(testShowItems)

	_, err := cmdPlaylistPlay(mp, env)

	require.NoError(t, err)
	pls := <-queue
	got := make([]string, 0, len(pls.Items))
	want := make([]string, 0, len(testShowItems))
	for _, item := range pls.Items {
		got = append(got, item.ZapScript)
	}
	for _, item := range testShowItems {
		want = append(want, item.Path)
	}
	assert.ElementsMatch(t, want, got)
	assert.Equal(t, snapshot, testShowItems, "shuffling a playlist leaves the listed items alone")
}

func TestCmdPlaylistPlay_CollectionIsUntrustedWhenItsCallerIs(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mp := collectionPlatform(cfg, collectionLaunchers(testShowItems, nil, nil))
	queue := make(chan *playlists.Playlist, 1)
	env := randomCollectionEnv(cfg, testShowPath)
	env.Cmd.Name = zapscript.ZapScriptCmdPlaylistPlay
	env.Unsafe = true
	env.Playlist = playlists.PlaylistController{Queue: queue}

	_, err := cmdPlaylistPlay(mp, env)

	require.NoError(t, err)
	assert.True(t, (<-queue).Unsafe)
}

func deckRandomEnv(t *testing.T, arg string) (platforms.CmdEnv, *database.Database, chan *playlists.Playlist) {
	t.Helper()
	env, db, queue := deckTestEnv(t, arg)
	env.Cmd.Name = zapscript.ZapScriptCmdLaunchRandom
	return env, db, queue
}

func TestCmdRandom_DeckQueuesOneItemPlaylist(t *testing.T) {
	t.Parallel()
	env, db, queue := deckRandomEnv(t, "deck://0123456789ab")
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Weekend", Owned: true,
		Items: []database.DeckItem{
			{Kind: database.DeckItemKindScript, Name: "First", ZapScript: "**launch.system:NES"},
			{Kind: database.DeckItemKindScript, Name: "Second", ZapScript: "**launch.title:NES/Metroid"},
		},
	}))

	seen := map[string]struct{}{}
	for range 100 {
		result, err := cmdRandom(newPlaylistTestPlatform(), env)
		require.NoError(t, err)
		assert.True(t, result.PlaylistChanged)
		pls := <-queue
		require.Len(t, pls.Items, 1, "one roll plays one item of the deck")
		seen[pls.Items[0].ZapScript] = struct{}{}
		assert.Empty(t, pls.DeckID, "a deck refresh must not swap the one item for the whole deck")
		assert.True(t, pls.Playing)
		assert.True(t, pls.ForceRelaunch, "every roll launches, even onto the item already playing")
		assert.True(t, pls.Ephemeral, "a roll is not a playlist to remember: each one would push a real one out")
		assert.False(t, pls.Unsafe, "the user's own deck is trusted")
		assert.Equal(t, "Weekend", pls.Name)
	}
	assert.Len(t, seen, 2, "every item of the deck can be picked")
}

func TestCmdRandom_DeckTrust(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		owned      bool
		callerSafe bool
		wantUnsafe bool
	}{
		{name: "owned deck from a trusted caller", owned: true, callerSafe: true, wantUnsafe: false},
		{name: "owned deck from an untrusted caller", owned: true, callerSafe: false, wantUnsafe: true},
		{name: "somebody else's deck", owned: false, callerSafe: true, wantUnsafe: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env, db, queue := deckRandomEnv(t, "deck://0123456789ab")
			env.Unsafe = !tt.callerSafe
			deck := &database.Deck{
				DeckID: "0123456789ab", Name: "Deck", Owned: tt.owned,
				Items: []database.DeckItem{
					{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**input.keyboard:a"},
				},
			}
			if tt.owned {
				require.NoError(t, db.UserDB.CreateDeck(deck))
			} else {
				_, err := db.UserDB.UpsertRemoteDeck(deck)
				require.NoError(t, err)
			}

			_, err := cmdRandom(newPlaylistTestPlatform(), env)

			require.NoError(t, err)
			assert.Equal(t, tt.wantUnsafe, (<-queue).Unsafe)
		})
	}
}

func TestCmdRandom_DeckBackgroundSlot(t *testing.T) {
	t.Parallel()
	env, db, queue := deckRandomEnv(t, "deck://0123456789ab")
	env.Cmd.AdvArgs = zapscript.NewAdvArgs(map[string]string{string(zapscript.KeySlot): "background"})
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Deck", Owned: true,
		Items: []database.DeckItem{{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**echo:a"}},
	}))

	_, err := cmdRandom(newPlaylistTestPlatform(), env)

	require.NoError(t, err)
	assert.Equal(t, mediaslot.Background, (<-queue).Slot)
}

func TestCmdRandom_DeckErrors(t *testing.T) {
	t.Parallel()

	t.Run("deck not found", func(t *testing.T) {
		t.Parallel()
		env, _, _ := deckRandomEnv(t, "deck://0123456789ab")
		_, err := cmdRandom(newPlaylistTestPlatform(), env)
		require.ErrorIs(t, err, ErrDeckNotFound)
	})

	t.Run("deck with nothing to play", func(t *testing.T) {
		t.Parallel()
		env, db, queue := deckRandomEnv(t, "deck://0123456789ab")
		require.NoError(t, db.UserDB.CreateDeck(&database.Deck{DeckID: "0123456789ab", Name: "Empty", Owned: true}))
		_, err := cmdRandom(newPlaylistTestPlatform(), env)
		require.ErrorContains(t, err, "no items in collection")
		assert.Empty(t, queue)
	})
}

func TestIsDeckRandom(t *testing.T) {
	t.Parallel()

	assert.True(t, isDeckRandom(&zapscript.Command{
		Name: zapscript.ZapScriptCmdLaunchRandom, Args: []string{"deck://0123456789ab"},
	}))
	assert.False(t, isDeckRandom(&zapscript.Command{
		Name: zapscript.ZapScriptCmdLaunchRandom, Args: []string{testShowPath},
	}))
	assert.False(t, isDeckRandom(&zapscript.Command{
		Name: zapscript.ZapScriptCmdLaunchRandom,
	}))
	assert.False(t, isDeckRandom(&zapscript.Command{
		Name: zapscript.ZapScriptCmdPlaylistPlay, Args: []string{"deck://0123456789ab"},
	}), "only launch.random queues a roll")
}

func TestRunCommand_DeckRandomIsNotRefusedByItsOwnLaunchGuard(t *testing.T) {
	t.Parallel()

	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Deck", Owned: true,
		Items: []database.DeckItem{{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**echo:a"}},
	}))
	lm := state.NewLauncherManager()
	queue := make(chan *playlists.Playlist, 1)
	script, err := zapscript.NewParser("**launch.random:deck://0123456789ab").ParseScript()
	require.NoError(t, err)

	result, err := RunCommand(
		t.Context(), newPlaylistTestPlatform(), &config.Instance{},
		playlists.PlaylistController{Queue: queue},
		tokens.Token{Source: tokens.SourceAPI}, script.Cmds[0], 1, 0, db,
		&RunCommandOptions{LauncherManager: lm}, &zapscript.ArgExprEnv{},
	)

	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	assert.Len(t, (<-queue).Items, 1)
	assert.False(t, lm.Launching(), "the command leaves no launch lock behind")
}
