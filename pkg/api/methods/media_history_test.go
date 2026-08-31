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
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	phelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleMediaHistory_NoParams(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()
	endTime := now.Add(30 * time.Minute)

	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).Return([]database.MediaHistoryEntry{
		{
			DBID:       1,
			SystemID:   "NES",
			SystemName: "Nintendo Entertainment System",
			MediaName:  "Super Mario Bros",
			MediaPath:  "/games/nes/smb.nes",
			LauncherID: "retroarch-nes",
			StartTime:  now,
			EndTime:    &endTime,
			PlayTime:   1800,
		},
	}, nil)

	env := requests.RequestEnv{
		Context: context.Background(),
		Database: &database.Database{
			UserDB: mockUserDB,
		},
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, "NES", resp.Entries[0].SystemID)
	assert.Equal(t, "Super Mario Bros", resp.Entries[0].MediaName)
	assert.Equal(t, 1800, resp.Entries[0].PlayTime)
	assert.NotNil(t, resp.Entries[0].EndedAt)
	assert.False(t, resp.Entries[0].HasCover)
	assert.NotNil(t, resp.Pagination)
	assert.False(t, resp.Pagination.HasNextPage)
	assert.Equal(t, 25, resp.Pagination.PageSize)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_WithMediaIDAndRelativePath(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	now := time.Now()
	rootDir := filepath.Join(string(filepath.Separator), "mock", "roms")
	mediaPath := filepath.Join(rootDir, "NES", "smb.nes")
	missingPath := filepath.Join(rootDir, "NES", "missing.nes")
	relPath := filepath.ToSlash(filepath.Join("NES", "smb.nes"))

	launcherCache := &phelpers.LauncherCache{}
	launcherCache.InitializeFromSlice([]platforms.Launcher{
		{ID: "nes-launcher", SystemID: "NES", Folders: []string{"NES"}},
	})

	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).Return([]database.MediaHistoryEntry{
		{
			DBID:       1,
			SystemID:   "NES",
			SystemName: "Nintendo Entertainment System",
			MediaName:  "Super Mario Bros",
			MediaPath:  mediaPath,
			LauncherID: "retroarch-nes",
			StartTime:  now,
			PlayTime:   1800,
		},
		{
			DBID:       2,
			SystemID:   "NES",
			SystemName: "Nintendo Entertainment System",
			MediaName:  "Missing Game",
			MediaPath:  missingPath,
			LauncherID: "retroarch-nes",
			StartTime:  now,
			PlayTime:   60,
		},
	}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{mediaPath, missingPath}).
		Return([]database.MediaPathID{{SystemID: "NES", Path: mediaPath, DBID: 42}}, nil)

	env := requests.RequestEnv{
		Context:       context.Background(),
		Database:      &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		Platform:      mockPlatform,
		Config:        &config.Instance{},
		LauncherCache: launcherCache,
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 2)
	assert.Equal(t, int64(42), resp.Entries[0].MediaID)
	require.NotNil(t, resp.Entries[0].RelPath)
	assert.Equal(t, relPath, *resp.Entries[0].RelPath)
	assert.Zero(t, resp.Entries[1].MediaID)
	require.NotNil(t, resp.Entries[1].RelPath)
	assert.Equal(t, filepath.ToSlash(filepath.Join("NES", "missing.nes")), *resp.Entries[1].RelPath)

	mockUserDB.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaHistory_IncludesCoverStatus(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	now := time.Now()
	coveredPath := filepath.Join(string(filepath.Separator), "games", "covered.nes")
	uncoveredPath := filepath.Join(string(filepath.Separator), "games", "uncovered.nes")
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).
		Return([]database.MediaHistoryEntry{
			{DBID: 2, SystemID: "NES", MediaPath: coveredPath, MediaName: "Covered", StartTime: now},
			{DBID: 1, SystemID: "NES", MediaPath: uncoveredPath, MediaName: "Uncovered", StartTime: now},
		}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{coveredPath, uncoveredPath}).
		Return([]database.MediaPathID{
			{SystemID: "NES", Path: coveredPath, DBID: 20, MediaTitleDBID: 200},
			{SystemID: "NES", Path: uncoveredPath, DBID: 10, MediaTitleDBID: 100},
		}, nil)
	mockMediaDB.On("GetMediaCoverStatus", mock.Anything, []database.MediaRef{
		{MediaDBID: 20, MediaTitleDBID: 200},
		{MediaDBID: 10, MediaTitleDBID: 100},
	}).Return(map[int64]bool{20: true, 10: false}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		Params:   json.RawMessage(`{}`),
	}
	result, err := HandleMediaHistory(env)
	require.NoError(t, err)
	response, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, response.Entries, 2)
	assert.True(t, response.Entries[0].HasCover)
	assert.False(t, response.Entries[1].HasCover)
	mockUserDB.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaHistory_EnrichmentFailuresAreNonFatal(t *testing.T) {
	t.Parallel()

	mediaPath := filepath.Join(string(filepath.Separator), "games", "history.nes")
	entry := database.MediaHistoryEntry{
		DBID: 1, SystemID: "NES", MediaPath: mediaPath, MediaName: "History", StartTime: time.Now(),
	}

	assertEnrichmentDeadline := func(t *testing.T, args mock.Arguments) {
		t.Helper()
		ctx, ok := args.Get(0).(context.Context)
		require.True(t, ok)
		deadline, ok := ctx.Deadline()
		require.True(t, ok, "optional enrichment must have a deadline")
		assert.WithinDuration(t, time.Now().Add(optionalDBEnrichmentTimeout), deadline, time.Second)
	}
	resolvedRef := []database.MediaRef{{MediaDBID: 42, MediaTitleDBID: 420}}

	tests := []struct {
		setup           func(*testing.T, *helpers.MockMediaDBI)
		name            string
		expectedTags    []database.TagInfo
		expectedMediaID int64
		expectTagLookup bool
	}{
		{
			name: "media identity lookup",
			setup: func(_ *testing.T, mockMediaDB *helpers.MockMediaDBI) {
				mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{mediaPath}).
					Return(nil, errors.New("identity lookup failed"))
				mockMediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, mock.Anything).
					Return(map[int64][]database.TagInfo{}, nil).Maybe()
			},
		},
		{
			name:            "cover lookup",
			expectedMediaID: 42,
			expectedTags:    []database.TagInfo{},
			expectTagLookup: true,
			setup: func(t *testing.T, mockMediaDB *helpers.MockMediaDBI) {
				t.Helper()
				mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{mediaPath}).
					Return([]database.MediaPathID{{
						SystemID: "NES", Path: mediaPath, DBID: 42, MediaTitleDBID: 420,
					}}, nil)
				mockMediaDB.On("GetMediaCoverStatus", mock.Anything, resolvedRef).
					Run(func(args mock.Arguments) { assertEnrichmentDeadline(t, args) }).
					Return(nil, errors.New("cover lookup failed"))
				mockMediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, resolvedRef).
					Return(map[int64][]database.TagInfo{}, nil).Once()
			},
		},
		{
			name:            "tag lookup",
			expectedMediaID: 42,
			expectTagLookup: true,
			setup: func(t *testing.T, mockMediaDB *helpers.MockMediaDBI) {
				t.Helper()
				mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{mediaPath}).
					Return([]database.MediaPathID{{
						SystemID: "NES", Path: mediaPath, DBID: 42, MediaTitleDBID: 420,
					}}, nil)
				mockMediaDB.On("GetMediaCoverStatus", mock.Anything, resolvedRef).
					Return(map[int64]bool{42: true}, nil)
				mockMediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, resolvedRef).
					Run(func(args mock.Arguments) { assertEnrichmentDeadline(t, args) }).
					Return(nil, errors.New("tag lookup failed")).Once()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockUserDB := helpers.NewMockUserDBI()
			mockMediaDB := helpers.NewMockMediaDBI()
			mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).
				Return([]database.MediaHistoryEntry{entry}, nil)
			tt.setup(t, mockMediaDB)

			result, err := HandleMediaHistory(requests.RequestEnv{
				Context: context.Background(),
				Database: &database.Database{
					UserDB: mockUserDB, MediaDB: mockMediaDB,
				},
			})
			require.NoError(t, err)
			response, ok := result.(models.MediaHistoryResponse)
			require.True(t, ok)
			require.Len(t, response.Entries, 1)
			assert.Equal(t, tt.expectedMediaID, response.Entries[0].MediaID)
			assert.True(t, response.Entries[0].HasCover)
			assert.Equal(t, tt.expectedTags, response.Entries[0].Tags)
			if !tt.expectTagLookup {
				mockMediaDB.AssertNotCalled(t, "GetMediaTagsByMediaRefs", mock.Anything, mock.Anything)
			}
			mockUserDB.AssertExpectations(t)
			mockMediaDB.AssertExpectations(t)
		})
	}
}

func TestMediaResponseMediaIDs_BoundsSlowLookup(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	mediaPath := filepath.Join(string(filepath.Separator), "games", "slow.nes")
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{mediaPath}).
		Run(func(args mock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			require.True(t, ok)
			<-ctx.Done()
		}).
		Return(nil, context.DeadlineExceeded).
		Once()

	started := time.Now()
	mediaIDs := mediaResponseMediaIDs(&requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
	}, []mediaPathRef{{SystemID: "NES", Path: mediaPath}})

	assert.Less(t, time.Since(started), 2*time.Second)
	assert.Empty(t, mediaIDs)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaHistory_WithLimit(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()

	entries := make([]database.MediaHistoryEntry, 6)
	for i := range entries {
		entries[i] = database.MediaHistoryEntry{
			DBID: int64(i + 1), SystemID: "NES", SystemName: "NES",
			MediaName:  fmt.Sprintf("Game %d", i+1),
			MediaPath:  fmt.Sprintf("/g%d", i+1),
			LauncherID: "l1", StartTime: now,
			PlayTime: (i + 1) * 100,
		}
	}
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 6).Return(entries, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"limit": 5}`),
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 5)
	require.NotNil(t, resp.Pagination)
	assert.True(t, resp.Pagination.HasNextPage)
	assert.NotNil(t, resp.Pagination.NextCursor)
	assert.Equal(t, 5, resp.Pagination.PageSize)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_DistinctMedia(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()
	firstPath := filepath.Join(string(filepath.Separator), "games", "first.nes")
	secondPath := filepath.Join(string(filepath.Separator), "games", "second.nes")
	thirdPath := filepath.Join(string(filepath.Separator), "games", "third.nes")
	mockUserDB.On(
		"GetDistinctMediaHistory", mock.Anything, []string{"NES"}, int64(0), 3,
	).Return([]database.MediaHistoryEntry{
		{DBID: 10, SystemID: "NES", SystemName: "NES", MediaName: "First", MediaPath: firstPath, StartTime: now},
		{DBID: 8, SystemID: "NES", SystemName: "NES", MediaName: "Second", MediaPath: secondPath, StartTime: now},
		{DBID: 5, SystemID: "NES", SystemName: "NES", MediaName: "Third", MediaPath: thirdPath, StartTime: now},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params: json.RawMessage(`{
			"systems": ["NES"],
			"limit": 2,
			"distinctMedia": true
		}`),
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)
	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 2)
	assert.Equal(t, "First", resp.Entries[0].MediaName)
	assert.Equal(t, "Second", resp.Entries[1].MediaName)
	require.NotNil(t, resp.Pagination)
	assert.True(t, resp.Pagination.HasNextPage)
	require.NotNil(t, resp.Pagination.NextCursor)
	cursor, err := decodeCursor(*resp.Pagination.NextCursor)
	require.NoError(t, err)
	require.NotNil(t, cursor)
	assert.Equal(t, int64(8), *cursor)
	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_WithCursor(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()

	cursorStr, err := encodeCursor(5)
	require.NoError(t, err)

	mockUserDB.On("GetMediaHistory", []string(nil), int64(5), 26).Return([]database.MediaHistoryEntry{
		{
			DBID: 6, SystemID: "SNES", SystemName: "SNES",
			MediaName: "Game 6", MediaPath: "/g6",
			LauncherID: "l1", StartTime: now, PlayTime: 100,
		},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"cursor": "` + cursorStr + `"}`),
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, "SNES", resp.Entries[0].SystemID)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_EmptyHistory(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).Return([]database.MediaHistoryEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)
	assert.Nil(t, resp.Pagination)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_InvalidCursor(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{"cursor": "not-valid-base64!!!"}`),
	}

	_, err := HandleMediaHistory(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid cursor")
}

func TestHandleMediaHistory_NullEndTime(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()

	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).Return([]database.MediaHistoryEntry{
		{
			DBID:       1,
			SystemID:   "Genesis",
			SystemName: "Sega Genesis",
			MediaName:  "Sonic",
			MediaPath:  "/games/gen/sonic.md",
			LauncherID: "retroarch-gen",
			StartTime:  now,
			EndTime:    nil,
			PlayTime:   60,
		},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 1)
	assert.Nil(t, resp.Entries[0].EndedAt)
	assert.Equal(t, 60, resp.Entries[0].PlayTime)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_SystemFilter(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()

	mockUserDB.On("GetMediaHistory", []string{"SNES"}, int64(0), 26).Return([]database.MediaHistoryEntry{
		{
			DBID:       1,
			SystemID:   "SNES",
			SystemName: "Super Nintendo Entertainment System",
			MediaName:  "Super Mario World",
			MediaPath:  "/games/snes/smw.sfc",
			LauncherID: "retroarch-snes",
			StartTime:  now,
			PlayTime:   3600,
		},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": ["SNES"]}`),
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, "SNES", resp.Entries[0].SystemID)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_FuzzySystemResolution(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistory", []string{"Genesis"}, int64(0), 26).
		Return([]database.MediaHistoryEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": ["sega genesis"], "fuzzySystem": true}`),
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_EmptySystemsArray(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).
		Return([]database.MediaHistoryEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": []}`),
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_InvalidSystemID(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{"systems": ["NOT_A_REAL_SYSTEM"]}`),
	}

	_, err := HandleMediaHistory(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NOT_A_REAL_SYSTEM")
}

func TestHandleMediaHistory_InvalidParams(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{invalid json`),
	}

	_, err := HandleMediaHistory(env)
	require.Error(t, err)
}

// marshalledHistoryEntries round-trips a handler result through JSON so tests
// can assert on key presence rather than Go zero values.
func marshalledHistoryEntries(t *testing.T, result any) []map[string]any {
	t.Helper()
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	var decoded struct {
		Entries []map[string]any `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	return decoded.Entries
}

func TestHandleMediaHistory_IncludesTags(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	now := time.Now()
	taggedPath := filepath.Join(string(filepath.Separator), "games", "tagged.nes")
	untaggedPath := filepath.Join(string(filepath.Separator), "games", "untagged.nes")
	taggedTags := []database.TagInfo{
		{Tag: "favorite", Type: "collection", Label: "Favorite"},
		{Tag: "1990", Type: "year"},
	}
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).
		Return([]database.MediaHistoryEntry{
			{DBID: 2, SystemID: "NES", MediaPath: taggedPath, MediaName: "Tagged", StartTime: now},
			{DBID: 1, SystemID: "NES", MediaPath: untaggedPath, MediaName: "Untagged", StartTime: now},
		}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{taggedPath, untaggedPath}).
		Return([]database.MediaPathID{
			{SystemID: "NES", Path: taggedPath, DBID: 20, MediaTitleDBID: 200},
			{SystemID: "NES", Path: untaggedPath, DBID: 10, MediaTitleDBID: 100},
		}, nil)
	mockMediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, []database.MediaRef{
		{MediaDBID: 20, MediaTitleDBID: 200},
		{MediaDBID: 10, MediaTitleDBID: 100},
	}).Return(map[int64][]database.TagInfo{20: taggedTags}, nil).Once()

	result, err := HandleMediaHistory(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		Params:   json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	response, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, response.Entries, 2)
	assert.Equal(t, taggedTags, response.Entries[0].Tags)
	assert.Equal(t, []database.TagInfo{}, response.Entries[1].Tags)

	entries := marshalledHistoryEntries(t, result)
	require.Len(t, entries, 2)
	assert.Len(t, entries[0]["tags"], 2)
	assert.Equal(t, []any{}, entries[1]["tags"], "indexed media without tags must serialise as an empty array")
	mockUserDB.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaHistory_UnresolvedMediaOmitsTags(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	now := time.Now()
	resolvedPath := filepath.Join(string(filepath.Separator), "games", "resolved.nes")
	missingPath := filepath.Join(string(filepath.Separator), "games", "missing.nes")
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).
		Return([]database.MediaHistoryEntry{
			{DBID: 2, SystemID: "NES", MediaPath: resolvedPath, MediaName: "Resolved", StartTime: now},
			{DBID: 1, SystemID: "NES", MediaPath: missingPath, MediaName: "Missing", StartTime: now},
		}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{resolvedPath, missingPath}).
		Return([]database.MediaPathID{
			{SystemID: "NES", Path: resolvedPath, DBID: 20, MediaTitleDBID: 200},
		}, nil)
	mockMediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, []database.MediaRef{
		{MediaDBID: 20, MediaTitleDBID: 200},
	}).Return(map[int64][]database.TagInfo{}, nil).Once()

	result, err := HandleMediaHistory(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
	})
	require.NoError(t, err)
	response, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, response.Entries, 2)
	assert.Equal(t, []database.TagInfo{}, response.Entries[0].Tags)
	assert.Nil(t, response.Entries[1].Tags)

	entries := marshalledHistoryEntries(t, result)
	require.Len(t, entries, 2)
	assert.Contains(t, entries[0], "tags")
	assert.NotContains(t, entries[1], "tags", "unresolved history rows must omit tags")
	assert.NotContains(t, entries[1], "mediaId")
	mockUserDB.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaHistory_NoResolvedMediaSkipsTagLookup(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	mediaPath := filepath.Join(string(filepath.Separator), "games", "missing.nes")
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).
		Return([]database.MediaHistoryEntry{
			{DBID: 1, SystemID: "NES", MediaPath: mediaPath, MediaName: "Missing", StartTime: time.Now()},
		}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{mediaPath}).
		Return([]database.MediaPathID{}, nil)
	mockMediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, mock.Anything).
		Return(map[int64][]database.TagInfo{}, nil).Maybe()

	result, err := HandleMediaHistory(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
	})
	require.NoError(t, err)
	response, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, response.Entries, 1)
	assert.Nil(t, response.Entries[0].Tags)
	mockMediaDB.AssertNotCalled(t, "GetMediaTagsByMediaRefs", mock.Anything, mock.Anything)
	mockMediaDB.AssertNotCalled(t, "GetMediaCoverStatus", mock.Anything, mock.Anything)
	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_CoverFailureDoesNotAffectTags(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	mediaPath := filepath.Join(string(filepath.Separator), "games", "history.nes")
	tags := []database.TagInfo{{Tag: "favorite", Type: "collection"}}
	refs := []database.MediaRef{{MediaDBID: 42, MediaTitleDBID: 420}}
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).
		Return([]database.MediaHistoryEntry{
			{DBID: 1, SystemID: "NES", MediaPath: mediaPath, MediaName: "History", StartTime: time.Now()},
		}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{mediaPath}).
		Return([]database.MediaPathID{{SystemID: "NES", Path: mediaPath, DBID: 42, MediaTitleDBID: 420}}, nil)
	mockMediaDB.On("GetMediaCoverStatus", mock.Anything, refs).
		Return(nil, errors.New("cover lookup failed"))
	mockMediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, refs).
		Return(map[int64][]database.TagInfo{42: tags}, nil).Once()

	result, err := HandleMediaHistory(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
	})
	require.NoError(t, err)
	response, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, response.Entries, 1)
	assert.Equal(t, int64(42), response.Entries[0].MediaID)
	assert.True(t, response.Entries[0].HasCover)
	assert.Equal(t, tags, response.Entries[0].Tags)
	mockUserDB.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaHistory_RepeatedMediaRowsShareOneRef(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	now := time.Now()
	mediaPath := filepath.Join(string(filepath.Separator), "games", "repeat.nes")
	tags := []database.TagInfo{{Tag: "favorite", Type: "collection"}}
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).
		Return([]database.MediaHistoryEntry{
			{DBID: 3, SystemID: "NES", MediaPath: mediaPath, MediaName: "Repeat", StartTime: now},
			{DBID: 2, SystemID: "NES", MediaPath: mediaPath, MediaName: "Repeat", StartTime: now.Add(-time.Hour)},
			{DBID: 1, SystemID: "NES", MediaPath: mediaPath, MediaName: "Repeat", StartTime: now.Add(-2 * time.Hour)},
		}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{mediaPath}).
		Return([]database.MediaPathID{{SystemID: "NES", Path: mediaPath, DBID: 42, MediaTitleDBID: 420}}, nil).
		Once()
	mockMediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, []database.MediaRef{{MediaDBID: 42, MediaTitleDBID: 420}}).
		Return(map[int64][]database.TagInfo{42: tags}, nil).Once()

	result, err := HandleMediaHistory(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		Params:   json.RawMessage(`{"distinctMedia": false}`),
	})
	require.NoError(t, err)
	response, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, response.Entries, 3)
	for i := range response.Entries {
		assert.Equal(t, int64(42), response.Entries[i].MediaID)
		assert.Equal(t, tags, response.Entries[i].Tags)
	}
	mockMediaDB.AssertNumberOfCalls(t, "FindMediaIDsByPaths", 1)
	mockMediaDB.AssertNumberOfCalls(t, "GetMediaTagsByMediaRefs", 1)
	mockUserDB.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaHistory_FullPageUsesSingleTagBatch(t *testing.T) {
	t.Parallel()

	const pageSize = 100
	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	now := time.Now()
	entries := make([]database.MediaHistoryEntry, 0, pageSize)
	rows := make([]database.MediaPathID, 0, pageSize)
	tagsByID := make(map[int64][]database.TagInfo, pageSize)
	for i := range pageSize {
		mediaPath := filepath.Join(string(filepath.Separator), "games", fmt.Sprintf("game-%03d.nes", i))
		mediaID := int64(1000 + i)
		entries = append(entries, database.MediaHistoryEntry{
			DBID: int64(pageSize - i), SystemID: "NES", MediaPath: mediaPath, MediaName: "Game", StartTime: now,
		})
		rows = append(rows, database.MediaPathID{
			SystemID: "NES", Path: mediaPath, DBID: mediaID, MediaTitleDBID: mediaID * 10,
		})
		if i%2 == 0 {
			tagsByID[mediaID] = []database.TagInfo{{Tag: fmt.Sprintf("tag-%d", i), Type: "test"}}
		}
	}
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), pageSize+1).Return(entries, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.MatchedBy(func(paths []string) bool {
		return len(paths) == pageSize
	})).Return(rows, nil).Once()
	mockMediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, mock.MatchedBy(func(refs []database.MediaRef) bool {
		return len(refs) == pageSize
	})).Return(tagsByID, nil).Once()

	result, err := HandleMediaHistory(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		Params:   json.RawMessage(`{"limit": 100}`),
	})
	require.NoError(t, err)
	response, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, response.Entries, pageSize)
	for i := range response.Entries {
		mediaID := int64(1000 + i)
		assert.Equal(t, mediaID, response.Entries[i].MediaID)
		if i%2 == 0 {
			assert.Equal(t, tagsByID[mediaID], response.Entries[i].Tags)
		} else {
			assert.Equal(t, []database.TagInfo{}, response.Entries[i].Tags)
		}
	}
	mockMediaDB.AssertNumberOfCalls(t, "FindMediaIDsByPaths", 1)
	mockMediaDB.AssertNumberOfCalls(t, "GetMediaTagsByMediaRefs", 1)
	mockUserDB.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
}
