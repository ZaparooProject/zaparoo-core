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

package methods

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// operatorDumpPath is where the Epilogue Operator bridge leaves the cartridge
// it dumped: a file the media index never walks.
func operatorDumpPath(name string) string {
	return filepath.Join(string(filepath.Separator), "tmp", "operator", name)
}

func secretOfManaVariants() []database.SearchResultWithCursor {
	libraryDir := filepath.Join(string(filepath.Separator), "media", "fat", "games", "SNES")
	return []database.SearchResultWithCursor{
		{
			SystemID:     "SNES",
			Name:         "Secret of Mana",
			Path:         filepath.Join(libraryDir, "Secret of Mana (Japan).sfc"),
			Tags:         []database.TagInfo{{Type: "region", Tag: "jp"}},
			MediaID:      7,
			MediaTitleID: 70,
		},
		{
			SystemID:     "SNES",
			Name:         "Secret of Mana",
			Path:         filepath.Join(libraryDir, "Secret of Mana (USA).sfc"),
			Tags:         []database.TagInfo{{Type: "region", Tag: "us"}},
			MediaID:      8,
			MediaTitleID: 70,
		},
	}
}

// Recents for an Operator play: the dump path is not indexed, so the entry
// is matched to the library title by its recorded identity and gets the
// library row's media ID, cover, tags and title command.
func TestHandleMediaHistory_UnindexedPlayResolvesByIdentity(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	now := time.Now()
	indexedPath := filepath.Join(string(filepath.Separator), "media", "fat", "games", "NES", "smb.nes")
	dumpPath := operatorDumpPath("SECRET OF MANA.sfc")
	unknownPath := operatorDumpPath("HOMEBREW.sfc")
	// A virtual path has no identity the indexer could derive.
	noIdentityPath := "steam://123/Old Play"

	mockUserDB.On("GetDistinctMediaHistory", mock.Anything, []string(nil), int64(0), 26).
		Return([]database.MediaHistoryEntry{
			{
				DBID: 4, SystemID: "NES", MediaPath: indexedPath, MediaName: "Super Mario Bros", StartTime: now,
				MediaIdentity: &database.MediaIdentity{CanonicalSystemID: "NES", CoreSlug: "supermariobros"},
			},
			{
				DBID: 3, SystemID: "SNES", MediaPath: dumpPath, MediaName: "SECRET OF MANA", StartTime: now,
				MediaIdentity: &database.MediaIdentity{CanonicalSystemID: "SNES", CoreSlug: "secretofmana"},
			},
			{
				DBID: 2, SystemID: "SNES", MediaPath: unknownPath, MediaName: "HOMEBREW", StartTime: now,
				MediaIdentity: &database.MediaIdentity{CanonicalSystemID: "SNES", CoreSlug: "homebrew"},
			},
			{DBID: 1, SystemID: "SNES", MediaPath: noIdentityPath, MediaName: "OLD PLAY", StartTime: now},
		}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).
		Return([]database.MediaPathID{{SystemID: "NES", Path: indexedPath, DBID: 42, MediaTitleDBID: 420}}, nil)
	mockMediaDB.On("SearchMediaBySlug", mock.Anything, "SNES", "secretofmana", []zapscript.TagFilter(nil)).
		Return(secretOfManaVariants(), nil).Once()
	mockMediaDB.On("SearchMediaBySlug", mock.Anything, "SNES", "homebrew", []zapscript.TagFilter(nil)).
		Return([]database.SearchResultWithCursor{}, nil).Once()
	resolvedRefs := []database.MediaRef{
		{MediaDBID: 42, MediaTitleDBID: 420},
		{MediaDBID: 8, MediaTitleDBID: 70},
	}
	mockMediaDB.On("GetMediaCoverStatus", mock.Anything, resolvedRefs).
		Return(map[int64]bool{42: true, 8: true}, nil)
	manaTags := []database.TagInfo{{Type: "region", Tag: "us"}}
	mockMediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, resolvedRefs).
		Return(map[int64][]database.TagInfo{8: manaTags}, nil)
	mockMediaDB.On("GetTitleZapScriptsByMediaDBIDs", mock.Anything, []int64{42, 8}).
		Return(map[int64]string{42: "@NES/Super Mario Bros", 8: "@SNES/Secret of Mana (region:us)"}, nil)

	result, err := HandleMediaHistory(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		Config:   &config.Instance{},
		Params:   json.RawMessage(`{"distinctMedia":true}`),
	})
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 4)

	assert.Equal(t, int64(42), resp.Entries[0].MediaID)

	operator := resp.Entries[1]
	assert.Equal(t, int64(8), operator.MediaID, "the preferred-region variant of the title is picked")
	assert.True(t, operator.HasCover)
	assert.Equal(t, manaTags, operator.Tags)
	assert.Equal(t, "@SNES/Secret of Mana (region:us)", operator.ZapScript)
	assert.Equal(t, dumpPath, operator.MediaPath, "the recorded path is reported as played")
	assert.Equal(t, "SECRET OF MANA", operator.MediaName)

	assert.Zero(t, resp.Entries[2].MediaID, "a title not in the library stays unresolved")
	assert.False(t, resp.Entries[2].HasCover)
	assert.Zero(t, resp.Entries[3].MediaID, "a play with no derivable identity stays unresolved")

	mockMediaDB.AssertNumberOfCalls(t, "SearchMediaBySlug", 2)
	mockUserDB.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
}

// Plays of one cartridge share a single slug lookup.
func TestHandleMediaHistory_IdentityLookupDedupesByTitle(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	now := time.Now()
	identity := &database.MediaIdentity{CanonicalSystemID: "SNES", CoreSlug: "secretofmana"}

	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).
		Return([]database.MediaHistoryEntry{
			{
				DBID: 2, SystemID: "SNES", MediaPath: operatorDumpPath("SECRET OF MANA.sfc"),
				MediaName: "SECRET OF MANA", StartTime: now, MediaIdentity: identity,
			},
			{
				DBID: 1, SystemID: "SNES", MediaPath: operatorDumpPath("SECRET OF MANA.sfc"),
				MediaName: "SECRET OF MANA", StartTime: now, MediaIdentity: identity,
			},
		}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).
		Return([]database.MediaPathID{}, nil)
	mockMediaDB.On("SearchMediaBySlug", mock.Anything, "SNES", "secretofmana", []zapscript.TagFilter(nil)).
		Return(secretOfManaVariants()[1:], nil).Once()

	result, err := HandleMediaHistory(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		Config:   &config.Instance{},
	})
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 2)
	assert.Equal(t, int64(8), resp.Entries[0].MediaID)
	assert.Equal(t, int64(8), resp.Entries[1].MediaID)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_UnindexedPlayResolvesByIdentity(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	dumpPath := operatorDumpPath("SECRET OF MANA.sfc")

	mockUserDB.On("GetMediaHistoryTop", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.MediaHistoryTopEntry{
			{
				SystemID: "SNES", MediaName: "SECRET OF MANA", MediaPath: dumpPath, LastPlayedAt: time.Now(),
				TotalPlayTime: 600, SessionCount: 2,
				MediaIdentity: &database.MediaIdentity{CanonicalSystemID: "SNES", CoreSlug: "secretofmana"},
			},
		}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).
		Return([]database.MediaPathID{}, nil)
	mockMediaDB.On("SearchMediaBySlug", mock.Anything, "SNES", "secretofmana", []zapscript.TagFilter(nil)).
		Return(secretOfManaVariants(), nil).Once()
	mockMediaDB.On("GetTitleZapScriptsByMediaDBIDs", mock.Anything, []int64{8}).
		Return(map[int64]string{8: "@SNES/Secret of Mana (region:us)"}, nil)

	result, err := HandleMediaHistoryTop(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		Config:   &config.Instance{},
	})
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 1)
	assert.Equal(t, int64(8), resp.Entries[0].MediaID)
	assert.Equal(t, "@SNES/Secret of Mana (region:us)", resp.Entries[0].ZapScript)
	assert.Equal(t, dumpPath, resp.Entries[0].MediaPath)
	mockMediaDB.AssertExpectations(t)
}

func newOperatorActiveMediaEnv(
	t *testing.T, mediaPath, indexingStatus string,
) (requests.RequestEnv, *helpers.MockMediaDBI) {
	t.Helper()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	appState, ns := state.NewState(mockPlatform, "test-boot-uuid")
	t.Cleanup(appState.StopService)
	drainNotifications(t, ns)
	appState.SetActiveMedia(models.NewActiveMedia("SNES", "SNES", mediaPath, "SECRET OF MANA", "operator"))

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, "SNES", mediaPath).
		Return([]database.TagInfo{}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).
		Return([]database.MediaPathID{}, nil)
	mockMediaDB.On("GetIndexingStatus").Return(indexingStatus, nil).Maybe()
	mockMediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil).Maybe()
	mockMediaDB.On("GetLastGenerated").Return(time.Now(), nil).Maybe()
	mockMediaDB.On("GetTotalMediaCount").Return(100, nil).Maybe()

	return requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
		Platform: mockPlatform,
		State:    appState,
		Config:   &config.Instance{},
	}, mockMediaDB
}

func expectOperatorSlugMatch(t *testing.T, mockMediaDB *helpers.MockMediaDBI, mediaPath string) {
	t.Helper()
	identity, ok := mediascanner.FallbackMediaIdentity(&config.Instance{}, "SNES", mediaPath)
	require.True(t, ok)
	mockMediaDB.On("SearchMediaBySlug", mock.Anything, "SNES", identity.CoreSlug, []zapscript.TagFilter(nil)).
		Return(secretOfManaVariants(), nil).Once()
	mockMediaDB.On("GetTitleZapScriptsByMediaDBIDs", mock.Anything, []int64{8}).
		Return(map[int64]string{8: "@SNES/Secret of Mana (region:us)"}, nil).Once()
}

// The running Operator cartridge is matched to the library title so clients
// can show its artwork; the reported path stays the running dump.
func TestHandleActiveMedia_UnindexedFileResolvesByIdentity(t *testing.T) {
	t.Parallel()

	dumpPath := operatorDumpPath("SECRET OF MANA.sfc")
	env, mockMediaDB := newOperatorActiveMediaEnv(t, dumpPath, mediadb.IndexingStatusCompleted)
	expectOperatorSlugMatch(t, mockMediaDB, dumpPath)

	result, err := HandleActiveMedia(env)
	require.NoError(t, err)

	resp, ok := result.(models.ActiveMediaResponse)
	require.True(t, ok)
	assert.Equal(t, int64(8), resp.MediaID)
	assert.Equal(t, "@SNES/Secret of Mana (region:us)", resp.ZapScript)
	assert.Equal(t, dumpPath, resp.Path)
	assert.Equal(t, "SECRET OF MANA", resp.Name)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMedia_UnindexedActiveFileResolvesByIdentity(t *testing.T) {
	t.Parallel()

	dumpPath := operatorDumpPath("SECRET OF MANA.sfc")
	env, mockMediaDB := newOperatorActiveMediaEnv(t, dumpPath, mediadb.IndexingStatusCompleted)
	expectOperatorSlugMatch(t, mockMediaDB, dumpPath)
	ClearIndexingStatus()

	result, err := HandleMedia(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaResponse)
	require.True(t, ok)
	require.Len(t, resp.Active, 1)
	assert.Equal(t, int64(8), resp.Active[0].MediaID)
	assert.Equal(t, "@SNES/Secret of Mana (region:us)", resp.Active[0].ZapScript)
	assert.Equal(t, dumpPath, resp.Active[0].Path)
	mockMediaDB.AssertExpectations(t)
}

// Mid-index a library game may simply not be indexed yet, so a running file
// is not matched to some other row of its title.
func TestHandleActiveMedia_NoIdentityMatchWhileIndexing(t *testing.T) {
	t.Parallel()

	dumpPath := operatorDumpPath("SECRET OF MANA.sfc")
	env, mockMediaDB := newOperatorActiveMediaEnv(t, dumpPath, mediadb.IndexingStatusRunning)

	result, err := HandleActiveMedia(env)
	require.NoError(t, err)

	resp, ok := result.(models.ActiveMediaResponse)
	require.True(t, ok)
	assert.Zero(t, resp.MediaID)
	assert.Equal(t, "@SNES/SECRET OF MANA", resp.ZapScript)
	mockMediaDB.AssertNotCalled(t, "SearchMediaBySlug", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// Only absolute file paths have an identity the indexer would derive.
func TestHandleActiveMedia_NoIdentityMatchForRelativePath(t *testing.T) {
	t.Parallel()

	env, mockMediaDB := newOperatorActiveMediaEnv(t, "SECRET OF MANA.sfc", mediadb.IndexingStatusCompleted)

	result, err := HandleActiveMedia(env)
	require.NoError(t, err)

	resp, ok := result.(models.ActiveMediaResponse)
	require.True(t, ok)
	assert.Zero(t, resp.MediaID)
	mockMediaDB.AssertNotCalled(t, "SearchMediaBySlug", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// A play whose identity snapshot has not landed yet still matches, through
// the identity the indexer would give its file.
func TestHandleMediaHistory_PlayWithoutIdentityUsesFallbackIdentity(t *testing.T) {
	t.Parallel()

	dumpPath := operatorDumpPath("SECRET OF MANA.sfc")
	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).Return([]database.MediaHistoryEntry{
		{DBID: 1, SystemID: "SNES", MediaPath: dumpPath, MediaName: "SECRET OF MANA", StartTime: time.Now()},
	}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).Return([]database.MediaPathID{}, nil)
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil)
	mockMediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil)
	identity, ok := mediascanner.FallbackMediaIdentity(&config.Instance{}, "SNES", dumpPath)
	require.True(t, ok)
	mockMediaDB.On("SearchMediaBySlug", mock.Anything, "SNES", identity.CoreSlug, []zapscript.TagFilter(nil)).
		Return(secretOfManaVariants(), nil).Once()

	result, err := HandleMediaHistory(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		Config:   &config.Instance{},
	})
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 1)
	assert.Equal(t, int64(8), resp.Entries[0].MediaID)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaHistory_NoFallbackIdentityWhileIndexing(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).Return([]database.MediaHistoryEntry{
		{DBID: 1, SystemID: "SNES", MediaPath: operatorDumpPath("SECRET OF MANA.sfc"), StartTime: time.Now()},
	}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).Return([]database.MediaPathID{}, nil)
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusRunning, nil)

	result, err := HandleMediaHistory(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		Config:   &config.Instance{},
	})
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 1)
	assert.Zero(t, resp.Entries[0].MediaID)
	mockMediaDB.AssertNotCalled(t, "SearchMediaBySlug", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// Plays of one title with different region identities each get their own
// variant rather than sharing the first play's match.
func TestHandleMediaHistory_IdentityTagsSeparateMatches(t *testing.T) {
	t.Parallel()

	regionIdentity := func(region string) *database.MediaIdentity {
		return &database.MediaIdentity{
			CanonicalSystemID: "SNES",
			CoreSlug:          "secretofmana",
			Tags: []database.MediaIdentityTag{
				{Type: "region", Value: region, Role: database.MediaIdentityTagRoleIdentity},
			},
		}
	}
	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).Return([]database.MediaHistoryEntry{
		{
			DBID: 2, SystemID: "SNES", MediaPath: operatorDumpPath("MANA JP.sfc"), StartTime: time.Now(),
			MediaIdentity: regionIdentity("jp"),
		},
		{
			DBID: 1, SystemID: "SNES", MediaPath: operatorDumpPath("MANA US.sfc"), StartTime: time.Now(),
			MediaIdentity: regionIdentity("us"),
		},
	}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).Return([]database.MediaPathID{}, nil)
	mockMediaDB.On("SearchMediaBySlug", mock.Anything, "SNES", "secretofmana", []zapscript.TagFilter(nil)).
		Return(secretOfManaVariants(), nil).Twice()

	result, err := HandleMediaHistory(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		Config:   &config.Instance{},
	})
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 2)
	assert.Equal(t, int64(7), resp.Entries[0].MediaID)
	assert.Equal(t, int64(8), resp.Entries[1].MediaID)
	mockMediaDB.AssertExpectations(t)
}

// Identity matching that runs out its deadline must not take the batch
// lookups for indexed entries down with it.
func TestHandleMediaHistory_SlowIdentityMatchKeepsIndexedMetadata(t *testing.T) {
	t.Parallel()

	indexedPath := filepath.Join(string(filepath.Separator), "media", "fat", "games", "NES", "smb.nes")
	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).Return([]database.MediaHistoryEntry{
		{DBID: 2, SystemID: "NES", MediaPath: indexedPath, StartTime: time.Now()},
		{
			DBID: 1, SystemID: "SNES", MediaPath: operatorDumpPath("SECRET OF MANA.sfc"), StartTime: time.Now(),
			MediaIdentity: &database.MediaIdentity{CanonicalSystemID: "SNES", CoreSlug: "secretofmana"},
		},
	}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).
		Return([]database.MediaPathID{{SystemID: "NES", Path: indexedPath, DBID: 42, MediaTitleDBID: 420}}, nil)
	mockMediaDB.On("SearchMediaBySlug", mock.Anything, "SNES", "secretofmana", []zapscript.TagFilter(nil)).
		Run(func(args mock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			if ok {
				<-ctx.Done()
			}
		}).
		Return([]database.SearchResultWithCursor{}, context.DeadlineExceeded).Once()
	liveContext := mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil })
	indexedTags := []database.TagInfo{{Type: "region", Tag: "us"}}
	mockMediaDB.On("GetMediaTagsByMediaRefs", liveContext, mock.Anything).
		Return(map[int64][]database.TagInfo{42: indexedTags}, nil).Once()

	result, err := HandleMediaHistory(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		Config:   &config.Instance{},
	})
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 2)
	assert.Equal(t, int64(42), resp.Entries[0].MediaID)
	assert.Equal(t, indexedTags, resp.Entries[0].Tags)
	assert.Zero(t, resp.Entries[1].MediaID)
	mockMediaDB.AssertExpectations(t)
}

// A failed path lookup is not a miss: the running file may be indexed, so it
// must not be swapped for a slug match.
func TestHandleActiveMedia_PathLookupFailureSkipsIdentityMatch(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	appState, ns := state.NewState(mockPlatform, "test-boot-uuid")
	t.Cleanup(appState.StopService)
	drainNotifications(t, ns)
	dumpPath := operatorDumpPath("SECRET OF MANA.sfc")
	appState.SetActiveMedia(models.NewActiveMedia("SNES", "SNES", dumpPath, "SECRET OF MANA", "operator"))

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, "SNES", dumpPath).
		Return([]database.TagInfo{}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).
		Return([]database.MediaPathID(nil), errors.New("database is locked"))

	result, err := HandleActiveMedia(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
		Platform: mockPlatform,
		State:    appState,
		Config:   &config.Instance{},
	})
	require.NoError(t, err)

	resp, ok := result.(models.ActiveMediaResponse)
	require.True(t, ok)
	assert.Zero(t, resp.MediaID)
	assert.Equal(t, "@SNES/SECRET OF MANA", resp.ZapScript)
	mockMediaDB.AssertNotCalled(t, "SearchMediaBySlug", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}
