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
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type flagCall struct {
	flag string
	ref  platforms.MediaRef
	on   bool
}

type deckCall struct {
	deckID string
	ref    platforms.MediaRef
	add    bool
}

// fakeLibrary records what a command asked of the media library and answers
// with the lists it was given.
type fakeLibrary struct {
	err         error
	recommended map[string][]platforms.LibraryItem
	sections    []string
	lists       []platforms.LibraryItem
	refs        []platforms.MediaRef
	topSystems  []string
	flags       []flagCall
	flagged     []string
	deckEdits   []deckCall
	unchanged   bool
}

var _ platforms.MediaLibrary = (*fakeLibrary)(nil)

func (f *fakeLibrary) Flagged(_ context.Context, flag string) ([]platforms.LibraryItem, error) {
	f.flagged = append(f.flagged, flag)
	return f.lists, f.err
}

func (f *fakeLibrary) Recommended(_ context.Context, section string) ([]platforms.LibraryItem, error) {
	f.sections = append(f.sections, section)
	return f.recommended[section], f.err
}

func (f *fakeLibrary) Similar(_ context.Context, ref platforms.MediaRef) ([]platforms.LibraryItem, error) {
	f.refs = append(f.refs, ref)
	return f.lists, f.err
}

func (f *fakeLibrary) Versions(_ context.Context, ref platforms.MediaRef) ([]platforms.LibraryItem, error) {
	f.refs = append(f.refs, ref)
	return f.lists, f.err
}

func (f *fakeLibrary) MostPlayed(_ context.Context, systemID string) ([]platforms.LibraryItem, error) {
	f.topSystems = append(f.topSystems, systemID)
	return f.lists, f.err
}

func (f *fakeLibrary) SetUserFlag(_ context.Context, ref platforms.MediaRef, flag string, on bool) error {
	f.flags = append(f.flags, flagCall{flag: flag, ref: ref, on: on})
	return f.err
}

func (f *fakeLibrary) EditDeck(_ context.Context, deckID string, ref platforms.MediaRef, add bool) (bool, error) {
	f.deckEdits = append(f.deckEdits, deckCall{deckID: deckID, ref: ref, add: add})
	return !f.unchanged, f.err
}

var libraryTestItems = []platforms.LibraryItem{
	{SystemID: "SNES", Name: "Super Metroid", Path: "roms/snes/Super Metroid.sfc", MediaID: 11},
	{SystemID: "NES", Name: "Metroid", Path: "roms/nes/Metroid.nes", MediaID: 12},
}

func libraryEnv(library platforms.MediaLibrary, mediaDB *testhelpers.MockMediaDBI) *platforms.CmdEnv {
	if mediaDB == nil {
		mediaDB = testhelpers.NewMockMediaDBI()
	}
	return &platforms.CmdEnv{
		Cfg:      &config.Instance{},
		Database: &database.Database{MediaDB: mediaDB, UserDB: testhelpers.NewMockUserDBI()},
		Library:  library,
	}
}

func runningMedia(env *platforms.CmdEnv, active *models.ActiveMedia) {
	env.ActiveMedia = func() *models.ActiveMedia { return active }
}

func collectionPaths(coll *collection) []string {
	paths := make([]string, 0, len(coll.items))
	for _, item := range coll.items {
		paths = append(paths, item.ZapScript)
	}
	return paths
}

func TestResolveCollection_FlagListReadsTheUserFlag(t *testing.T) {
	t.Parallel()
	for link, flag := range map[string]string{
		"favorites://": "favorite", "LIKED://": "liked", "disliked://": "disliked",
		"playlater://": "playlater", "hidden://": "hidden",
	} {
		library := &fakeLibrary{lists: libraryTestItems[:1]}
		coll, handled, err := resolveCollection(newPlaylistTestPlatform(), libraryEnv(library, nil), link)
		require.NoError(t, err, link)
		require.True(t, handled, link)
		assert.Equal(t, []string{flag}, library.flagged, link)
		assert.True(t, coll.media && coll.library, link)
		assert.False(t, coll.ephemeral, "%s is the user's own list and keeps its place", link)
		assert.Equal(t, []string{"roms/snes/Super Metroid.sfc"}, collectionPaths(coll), link)
		assert.Equal(t, []collectionRow{{systemID: "SNES", mediaID: 11}}, coll.rows, link)
	}
}

func TestResolveCollection_FlagListTakesNoArgument(t *testing.T) {
	t.Parallel()
	_, handled, err := resolveCollection(newPlaylistTestPlatform(), libraryEnv(nil, nil), "favorites://snes")
	assert.True(t, handled)
	require.ErrorIs(t, err, ErrInvalidArguments)
}

func TestResolveCollection_Recommended(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{recommended: map[string][]platforms.LibraryItem{
		"": libraryTestItems, "playlater": libraryTestItems[:1], "queue": libraryTestItems[1:],
	}}
	env := libraryEnv(library, nil)

	coll, handled, err := resolveCollection(newPlaylistTestPlatform(), env, "recommended://")
	require.NoError(t, err)
	require.True(t, handled)
	assert.Equal(t, "recommended://", coll.id)
	assert.True(t, coll.ephemeral, "a computed list is not remembered")
	assert.Len(t, coll.items, 2)

	coll, _, err = resolveCollection(newPlaylistTestPlatform(), env, "recommended://PlayLater")
	require.NoError(t, err)
	assert.Equal(t, []string{"roms/snes/Super Metroid.sfc"}, collectionPaths(coll))

	coll, _, err = resolveCollection(newPlaylistTestPlatform(), env, "recommended://queue")
	require.NoError(t, err)
	assert.Equal(t, []string{"roms/nes/Metroid.nes"}, collectionPaths(coll))
	assert.Equal(t, []string{"", "playlater", "queue"}, library.sections)

	_, _, err = resolveCollection(newPlaylistTestPlatform(), env, "recommended://nope")
	require.ErrorIs(t, err, ErrInvalidArguments)
}

func TestResolveCollection_LibraryListNeedsTheLibrary(t *testing.T) {
	t.Parallel()
	for _, link := range []string{"favorites://", "recommended://", "similar://", "versions://", "top://"} {
		_, handled, err := resolveCollection(newPlaylistTestPlatform(), libraryEnv(nil, nil), link)
		assert.True(t, handled, link)
		require.ErrorIs(t, err, ErrNoMediaLibrary, link)
	}
}

func TestResolveCollection_SimilarToRunningMediaUsesItsRecord(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{lists: libraryTestItems}
	// The MediaDB mock has no lookups set up: the running media's own
	// record is used as it is.
	env := libraryEnv(library, nil)
	runningMedia(env, &models.ActiveMedia{SystemID: "SNES", Path: `roms\snes\Super Metroid.sfc`, MediaID: 11})

	for _, link := range []string{"similar://", "similar://active", "versions://ACTIVE"} {
		coll, _, err := resolveCollection(newPlaylistTestPlatform(), env, link)
		require.NoError(t, err, link)
		assert.True(t, coll.ephemeral, link)
		assert.Len(t, coll.items, 2, link)
	}
	want := platforms.MediaRef{SystemID: "SNES", Path: "roms/snes/Super Metroid.sfc", MediaID: 11}
	assert.Equal(t, []platforms.MediaRef{want, want, want}, library.refs)
}

func TestResolveCollection_SimilarWithNothingRunning(t *testing.T) {
	t.Parallel()
	env := libraryEnv(&fakeLibrary{}, nil)
	_, _, err := resolveCollection(newPlaylistTestPlatform(), env, "similar://")
	require.ErrorIs(t, err, ErrNoActiveMedia)

	runningMedia(env, nil)
	_, _, err = resolveCollection(newPlaylistTestPlatform(), env, "versions://active")
	require.ErrorIs(t, err, ErrNoActiveMedia)
}

func TestResolveCollection_SimilarToLastPlayed(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{lists: libraryTestItems}
	env := libraryEnv(library, nil)
	userDB := testhelpers.NewMockUserDBI()
	userDB.On("GetMediaHistory", mock.Anything, mock.Anything, mock.Anything).Return([]database.MediaHistoryEntry{
		{SystemID: "NES", MediaPath: "roms/nes/Metroid.nes"},
	}, nil)
	env.Database.UserDB = userDB

	_, _, err := resolveCollection(newPlaylistTestPlatform(), env, "similar://last")
	require.NoError(t, err)
	assert.Equal(t, []platforms.MediaRef{{SystemID: "NES", Path: "roms/nes/Metroid.nes"}}, library.refs)
}

func TestResolveCollection_VersionsOfIndexedPath(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{lists: libraryTestItems}
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{"roms/nes/Metroid.nes"}).Return([]database.MediaPathID{
		{SystemID: "NES", Path: "roms/nes/Metroid.nes", DBID: 12},
	}, nil)
	env := libraryEnv(library, mediaDB)

	coll, _, err := resolveCollection(newPlaylistTestPlatform(), env, "versions://roms/nes/Metroid.nes")
	require.NoError(t, err)
	assert.Equal(t, "versions://NES/roms/nes/Metroid.nes", coll.id,
		"the playlist is named for the media it is about")
	assert.Equal(t, []platforms.MediaRef{{SystemID: "NES", Path: "roms/nes/Metroid.nes", MediaID: 12}}, library.refs)
}

func TestResolveCollection_VersionsOfSomethingElse(t *testing.T) {
	t.Parallel()
	env := libraryEnv(&fakeLibrary{}, nil)
	_, _, err := resolveCollection(newPlaylistTestPlatform(), env, "versions://not a title")
	require.ErrorIs(t, err, ErrInvalidArguments)
}

func TestResolveCollection_Top(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{lists: libraryTestItems}
	env := libraryEnv(library, nil)

	coll, _, err := resolveCollection(newPlaylistTestPlatform(), env, "top://")
	require.NoError(t, err)
	assert.Len(t, coll.items, 2)
	_, _, err = resolveCollection(newPlaylistTestPlatform(), env, "top://snes")
	require.NoError(t, err)
	assert.Equal(t, []string{"", "SNES"}, library.topSystems)

	_, _, err = resolveCollection(newPlaylistTestPlatform(), env, "top://NotASystem")
	require.ErrorIs(t, err, ErrInvalidArguments)
}

func TestResolveCollection_LibraryListsHoldToOpenSystems(t *testing.T) {
	t.Parallel()
	env := libraryEnv(&fakeLibrary{lists: libraryTestItems}, nil)
	env.SystemPermitted = func(systemID string) bool { return systemID == "NES" }

	coll, _, err := resolveCollection(newPlaylistTestPlatform(), env, "top://")
	require.NoError(t, err)
	assert.Equal(t, []string{"roms/nes/Metroid.nes"}, collectionPaths(coll))
}

func TestResolveCollection_LibraryErrorIsReported(t *testing.T) {
	t.Parallel()
	refused := errors.New("boom")
	env := libraryEnv(&fakeLibrary{err: refused}, nil)
	_, handled, err := resolveCollection(newPlaylistTestPlatform(), env, "top://")
	assert.True(t, handled)
	require.ErrorIs(t, err, refused)
}

func TestResolveCollection_OtherLinksAreNotLibraryLists(t *testing.T) {
	t.Parallel()
	for _, arg := range []string{"steam://123", "/roms/nes/Metroid.nes", "favorite://", "tag://favorite"} {
		_, handled, err := resolveLibraryCollection(newPlaylistTestPlatform(), libraryEnv(nil, nil), arg)
		require.NoError(t, err, arg)
		assert.False(t, handled, arg)
	}
}

func TestCmdRandom_TagsFilterLibraryList(t *testing.T) {
	t.Parallel()
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("MatchMediaTagFilters", mock.Anything, []int64{11, 12}, mock.Anything).
		Return(map[int64]struct{}{}, nil).Once()
	env := libraryEnv(&fakeLibrary{lists: libraryTestItems}, mediaDB)
	env.Cmd = zapscript.Command{
		Name: zapscript.ZapScriptCmdLaunchRandom, Args: []string{"favorites://"},
		AdvArgs: zapscript.NewAdvArgs(map[string]string{"tags": "region:jp"}),
	}

	_, err := cmdRandom(newPlaylistTestPlatform(), *env)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrInvalidArguments, "tags apply to a library list")
	assert.Contains(t, err.Error(), "no items in collection", "no favorite matched the tags")
	mediaDB.AssertExpectations(t)
}

func TestFilterCollectionByTags_KeepsMatches(t *testing.T) {
	t.Parallel()
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("MatchMediaTagFilters", mock.Anything, []int64{11, 12}, mock.Anything).
		Return(map[int64]struct{}{12: {}}, nil)
	env := libraryEnv(nil, mediaDB)
	coll := &collection{
		library: true, media: true,
		items: []playlists.PlaylistItem{{ZapScript: "a"}, {ZapScript: "b"}},
		rows:  []collectionRow{{systemID: "SNES", mediaID: 11}, {systemID: "NES", mediaID: 12}},
	}
	require.NoError(t, filterCollectionByTags(env, coll, []zapscript.TagFilter{{Type: "region", Value: "us"}}))
	assert.Equal(t, []string{"b"}, collectionPaths(coll))
	assert.Equal(t, []collectionRow{{systemID: "NES", mediaID: 12}}, coll.rows)
}

func TestCmdPlaylistOpen_ComputedListIsEphemeral(t *testing.T) {
	t.Parallel()
	queue := make(chan *playlists.Playlist, 1)
	env := libraryEnv(&fakeLibrary{recommended: map[string][]platforms.LibraryItem{"": libraryTestItems}}, nil)
	env.ServiceCtx = t.Context()
	env.Playlist = playlists.PlaylistController{Queue: queue}
	env.Cmd = zapscript.Command{Name: zapscript.ZapScriptCmdPlaylistLoad, Args: []string{"recommended://"}}

	_, err := cmdPlaylistLoad(newPlaylistTestPlatform(), *env)
	require.NoError(t, err)
	pls := <-queue
	assert.True(t, pls.Ephemeral)
	assert.Equal(t, "recommended://", pls.ID)
	assert.Equal(t, []string{"roms/snes/Super Metroid.sfc", "roms/nes/Metroid.nes"}, playlists.ItemScripts(pls.Items))
}

func TestCmdPlaylistLoad_EmptyLibraryListFails(t *testing.T) {
	t.Parallel()
	for _, link := range []string{"recommended://series", "favorites://"} {
		queue := make(chan *playlists.Playlist, 1)
		env := libraryEnv(&fakeLibrary{}, nil)
		env.ServiceCtx = t.Context()
		env.Playlist = playlists.PlaylistController{Queue: queue}
		env.Cmd = zapscript.Command{Name: zapscript.ZapScriptCmdPlaylistLoad, Args: []string{link}}

		_, err := cmdPlaylistLoad(newPlaylistTestPlatform(), *env)
		require.Error(t, err, link)
		assert.Contains(t, err.Error(), "no items in collection", link)
		assert.Empty(t, queue, "%s queues no playlist", link)
	}
}

func TestResolveCollection_LastPlayedWithNoHistory(t *testing.T) {
	t.Parallel()
	env := libraryEnv(&fakeLibrary{}, nil)
	env.Database.UserDB = nil
	_, _, err := resolveCollection(newPlaylistTestPlatform(), env, "similar://last")
	require.ErrorIs(t, err, ErrNoHistory)

	failing := testhelpers.NewMockUserDBI()
	failing.On("GetMediaHistory", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.MediaHistoryEntry(nil), errors.New("history unreadable"))
	env.Database.UserDB = failing
	_, _, err = resolveCollection(newPlaylistTestPlatform(), env, "similar://last")
	require.ErrorContains(t, err, "history unreadable")
}

func TestResolveCollection_VersionsOfTitle(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{lists: libraryTestItems}
	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	scantest.IndexMediaPaths(t, db.MediaDB, "NES", "roms/NES/Metroid.nes", "roms/NES/Contra.nes")
	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	env := &platforms.CmdEnv{ServiceCtx: t.Context(), Cfg: cfg, Database: db, Library: library}

	coll, _, err := resolveCollection(newPlaylistTestPlatform(), env, "versions://NES/Metroid")
	require.NoError(t, err)
	require.Len(t, library.refs, 1)
	assert.Equal(t, "NES", library.refs[0].SystemID)
	assert.Equal(t, "roms/NES/Metroid.nes", library.refs[0].Path, "the title resolves the way launch.title does")
	assert.NotZero(t, library.refs[0].MediaID)
	assert.Equal(t, "versions://NES/roms/NES/Metroid.nes", coll.id)
}

func TestResolveCollection_VersionsOfTitleInUnknownSystem(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{lists: libraryTestItems}
	env := libraryEnv(library, nil)
	env.Database.MediaDB = nil
	_, _, err := resolveCollection(newPlaylistTestPlatform(), env, "versions://NotASystem/Metroid")
	require.ErrorContains(t, err, "NotASystem")
	assert.Empty(t, library.refs)
}

func TestFilterCollectionByTags_NoMediaDBLeavesTheList(t *testing.T) {
	t.Parallel()
	env := libraryEnv(nil, nil)
	env.Database.MediaDB = nil
	coll := &collection{items: []playlists.PlaylistItem{{ZapScript: "a"}}, rows: []collectionRow{{mediaID: 11}}}
	require.NoError(t, filterCollectionByTags(env, coll, []zapscript.TagFilter{{Type: "region", Value: "us"}}))
	assert.Equal(t, []string{"a"}, collectionPaths(coll))
}

func TestFilterCollectionByTags_ReportsMatchFailure(t *testing.T) {
	t.Parallel()
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("MatchMediaTagFilters", mock.Anything, []int64{11}, mock.Anything).
		Return(map[int64]struct{}(nil), errors.New("tags unreadable"))
	env := libraryEnv(nil, mediaDB)
	coll := &collection{
		name:  "favorites",
		items: []playlists.PlaylistItem{{ZapScript: "a"}},
		rows:  []collectionRow{{mediaID: 11}},
	}
	err := filterCollectionByTags(env, coll, []zapscript.TagFilter{{Type: "region", Value: "us"}})
	require.ErrorContains(t, err, "tags unreadable")
	assert.Equal(t, []string{"a"}, collectionPaths(coll), "a failed filter leaves the list as it was")
}
