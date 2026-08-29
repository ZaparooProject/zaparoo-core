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

func TestHandleMediaHistoryTop_NoParams(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()

	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), 25).Return([]database.MediaHistoryTopEntry{
		{
			SystemID:      "SNES",
			SystemName:    "Super Nintendo Entertainment System",
			MediaName:     "Super Mario World",
			MediaPath:     "/games/snes/smw.sfc",
			TotalPlayTime: 7200,
			SessionCount:  12,
			LastPlayedAt:  now,
		},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, "SNES", resp.Entries[0].SystemID)
	assert.Equal(t, "Super Mario World", resp.Entries[0].MediaName)
	assert.Equal(t, 7200, resp.Entries[0].TotalPlayTime)
	assert.Equal(t, 12, resp.Entries[0].SessionCount)
	assert.Equal(t, now.Format(time.RFC3339), resp.Entries[0].LastPlayedAt)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_WithMediaIDAndRelativePath(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	now := time.Now()
	rootDir := filepath.Join(string(filepath.Separator), "mock", "roms")
	mediaPath := filepath.Join(rootDir, "SNES", "smw.sfc")
	relPath := filepath.ToSlash(filepath.Join("SNES", "smw.sfc"))

	launcherCache := &phelpers.LauncherCache{}
	launcherCache.InitializeFromSlice([]platforms.Launcher{
		{ID: "snes-launcher", SystemID: "SNES", Folders: []string{"SNES"}},
	})

	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), 25).Return([]database.MediaHistoryTopEntry{
		{
			SystemID:      "SNES",
			SystemName:    "Super Nintendo Entertainment System",
			MediaName:     "Super Mario World",
			MediaPath:     mediaPath,
			TotalPlayTime: 7200,
			SessionCount:  12,
			LastPlayedAt:  now,
		},
	}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{mediaPath}).
		Return([]database.MediaPathID{{SystemID: "SNES", Path: mediaPath, DBID: 42, MediaTitleDBID: 420}}, nil)

	env := requests.RequestEnv{
		Context:       context.Background(),
		Database:      &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		Platform:      mockPlatform,
		Config:        &config.Instance{},
		LauncherCache: launcherCache,
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 1)
	assert.Equal(t, int64(42), resp.Entries[0].MediaID)
	require.NotNil(t, resp.Entries[0].RelPath)
	assert.Equal(t, relPath, *resp.Entries[0].RelPath)
	assert.Equal(t, []database.TagInfo{}, resp.Entries[0].Tags)

	mockUserDB.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_CustomLimit(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), 10).
		Return([]database.MediaHistoryTopEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"limit": 10}`),
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_EmptySystemsArray(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), 25).
		Return([]database.MediaHistoryTopEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": []}`),
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_SystemFilter(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistoryTop", []string{"SNES"}, (*time.Time)(nil), 25).Return([]database.MediaHistoryTopEntry{
		{
			SystemID:      "SNES",
			SystemName:    "Super Nintendo Entertainment System",
			MediaName:     "Super Mario World",
			MediaPath:     "/games/snes/smw.sfc",
			TotalPlayTime: 3600,
			SessionCount:  5,
			LastPlayedAt:  time.Now(),
		},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": ["SNES"]}`),
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, "SNES", resp.Entries[0].SystemID)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_MultipleSystems(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()

	mockUserDB.On(
		"GetMediaHistoryTop", []string{"SNES", "NES"}, (*time.Time)(nil), 25,
	).Return([]database.MediaHistoryTopEntry{
		{
			SystemID:      "SNES",
			SystemName:    "Super Nintendo Entertainment System",
			MediaName:     "Super Mario World",
			MediaPath:     "/games/snes/smw.sfc",
			TotalPlayTime: 7200,
			SessionCount:  12,
			LastPlayedAt:  now,
		},
		{
			SystemID:      "NES",
			SystemName:    "Nintendo Entertainment System",
			MediaName:     "Super Mario Bros",
			MediaPath:     "/games/nes/smb.nes",
			TotalPlayTime: 3600,
			SessionCount:  5,
			LastPlayedAt:  now,
		},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": ["SNES", "NES"]}`),
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 2)
	assert.Equal(t, "SNES", resp.Entries[0].SystemID)
	assert.Equal(t, "NES", resp.Entries[1].SystemID)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_FuzzySystemResolution(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistoryTop", []string{"Genesis"}, (*time.Time)(nil), 25).
		Return([]database.MediaHistoryTopEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": ["sega genesis"], "fuzzySystem": true}`),
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_SinceFilter(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	sinceStr := "2026-01-01T00:00:00Z"
	sinceTime, _ := time.Parse(time.RFC3339, sinceStr)

	mockUserDB.On("GetMediaHistoryTop", []string(nil), &sinceTime, 25).Return([]database.MediaHistoryTopEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"since": "` + sinceStr + `"}`),
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_AllFilters(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	sinceStr := "2026-02-01T00:00:00Z"
	sinceTime, _ := time.Parse(time.RFC3339, sinceStr)

	mockUserDB.On("GetMediaHistoryTop", []string{"Genesis"}, &sinceTime, 5).Return([]database.MediaHistoryTopEntry{
		{
			SystemID:      "Genesis",
			SystemName:    "Sega Genesis",
			MediaName:     "Sonic the Hedgehog",
			MediaPath:     "/games/gen/sonic.md",
			TotalPlayTime: 1800,
			SessionCount:  3,
			LastPlayedAt:  time.Now(),
		},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": ["Genesis"], "since": "` + sinceStr + `", "limit": 5}`),
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, "Genesis", resp.Entries[0].SystemID)
	assert.Equal(t, 1800, resp.Entries[0].TotalPlayTime)
	assert.Equal(t, 3, resp.Entries[0].SessionCount)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_EmptyResults(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), 25).
		Return([]database.MediaHistoryTopEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_InvalidSystemID(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{"systems": ["NOT_A_REAL_SYSTEM"]}`),
	}

	_, err := HandleMediaHistoryTop(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NOT_A_REAL_SYSTEM")
}

func TestHandleMediaHistoryTop_InvalidParams(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{invalid json`),
	}

	_, err := HandleMediaHistoryTop(env)
	require.Error(t, err)
}

func TestHandleMediaHistoryTop_LimitZero(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{"limit": 0}`),
	}

	_, err := HandleMediaHistoryTop(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid params")
}

func TestHandleMediaHistoryTop_LimitExceedsMax(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{"limit": 200}`),
	}

	_, err := HandleMediaHistoryTop(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid params")
}

func TestHandleMediaHistoryTop_InvalidSince(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{"since": "not-a-timestamp"}`),
	}

	_, err := HandleMediaHistoryTop(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid since timestamp")
}

func TestHandleMediaHistoryTop_DatabaseError(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), 25).Return(
		[]database.MediaHistoryTopEntry{}, errors.New("db error"),
	)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
	}

	_, err := HandleMediaHistoryTop(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error getting media history top")

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_IncludesTags(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	now := time.Now()
	taggedPath := filepath.Join(string(filepath.Separator), "games", "tagged.sfc")
	untaggedPath := filepath.Join(string(filepath.Separator), "games", "untagged.sfc")
	missingPath := filepath.Join(string(filepath.Separator), "games", "missing.sfc")
	tags := []database.TagInfo{{Tag: "favorite", Type: "collection", Label: "Favorite"}}
	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), 25).
		Return([]database.MediaHistoryTopEntry{
			{
				SystemID: "SNES", MediaName: "Tagged", MediaPath: taggedPath,
				TotalPlayTime: 300, SessionCount: 3, LastPlayedAt: now,
			},
			{
				SystemID: "SNES", MediaName: "Untagged", MediaPath: untaggedPath,
				TotalPlayTime: 200, SessionCount: 2, LastPlayedAt: now,
			},
			{
				SystemID: "SNES", MediaName: "Missing", MediaPath: missingPath,
				TotalPlayTime: 100, SessionCount: 1, LastPlayedAt: now,
			},
		}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{taggedPath, untaggedPath, missingPath}).
		Return([]database.MediaPathID{
			{SystemID: "SNES", Path: taggedPath, DBID: 42, MediaTitleDBID: 420},
			{SystemID: "SNES", Path: untaggedPath, DBID: 43, MediaTitleDBID: 430},
		}, nil).Once()
	mockMediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, []database.MediaRef{
		{MediaDBID: 42, MediaTitleDBID: 420},
		{MediaDBID: 43, MediaTitleDBID: 430},
	}).Return(map[int64][]database.TagInfo{42: tags}, nil).Once()

	result, err := HandleMediaHistoryTop(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
	})
	require.NoError(t, err)
	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 3)
	assert.Equal(t, int64(42), resp.Entries[0].MediaID)
	assert.Equal(t, tags, resp.Entries[0].Tags)
	assert.Equal(t, int64(43), resp.Entries[1].MediaID)
	assert.Equal(t, []database.TagInfo{}, resp.Entries[1].Tags)
	assert.Zero(t, resp.Entries[2].MediaID)
	assert.Nil(t, resp.Entries[2].Tags)

	raw, err := json.Marshal(result)
	require.NoError(t, err)
	var decoded struct {
		Entries []map[string]any `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Len(t, decoded.Entries, 3)
	assert.Len(t, decoded.Entries[0]["tags"], 1)
	assert.Equal(t, []any{}, decoded.Entries[1]["tags"])
	assert.NotContains(t, decoded.Entries[2], "tags")
	mockMediaDB.AssertNumberOfCalls(t, "FindMediaIDsByPaths", 1)
	mockMediaDB.AssertNumberOfCalls(t, "GetMediaTagsByMediaRefs", 1)
	mockUserDB.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_TagLookupFailureIsNonFatal(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	mediaPath := filepath.Join(string(filepath.Separator), "games", "top.sfc")
	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), 25).
		Return([]database.MediaHistoryTopEntry{
			{
				SystemID: "SNES", MediaName: "Top", MediaPath: mediaPath,
				TotalPlayTime: 300, SessionCount: 3, LastPlayedAt: time.Now(),
			},
		}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{mediaPath}).
		Return([]database.MediaPathID{{SystemID: "SNES", Path: mediaPath, DBID: 42, MediaTitleDBID: 420}}, nil)
	mockMediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, []database.MediaRef{{MediaDBID: 42, MediaTitleDBID: 420}}).
		Run(func(args mock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			require.True(t, ok)
			deadline, ok := ctx.Deadline()
			require.True(t, ok, "optional enrichment must have a deadline")
			assert.WithinDuration(t, time.Now().Add(optionalDBEnrichmentTimeout), deadline, time.Second)
		}).
		Return(nil, errors.New("tag lookup failed")).Once()

	result, err := HandleMediaHistoryTop(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
	})
	require.NoError(t, err)
	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 1)
	assert.Equal(t, int64(42), resp.Entries[0].MediaID)
	assert.Nil(t, resp.Entries[0].Tags)
	mockUserDB.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_IdentityLookupFailureIsNonFatal(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	mediaPath := filepath.Join(string(filepath.Separator), "games", "top.sfc")
	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), 25).
		Return([]database.MediaHistoryTopEntry{
			{
				SystemID: "SNES", MediaName: "Top", MediaPath: mediaPath,
				TotalPlayTime: 300, SessionCount: 3, LastPlayedAt: time.Now(),
			},
		}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{mediaPath}).
		Return(nil, errors.New("identity lookup failed"))
	mockMediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, mock.Anything).
		Return(map[int64][]database.TagInfo{}, nil).Maybe()

	result, err := HandleMediaHistoryTop(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
	})
	require.NoError(t, err)
	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 1)
	assert.Zero(t, resp.Entries[0].MediaID)
	assert.Nil(t, resp.Entries[0].Tags)
	mockMediaDB.AssertNotCalled(t, "GetMediaTagsByMediaRefs", mock.Anything, mock.Anything)
	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_FullPageUsesSingleTagBatch(t *testing.T) {
	t.Parallel()

	const pageSize = 100
	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	now := time.Now()
	entries := make([]database.MediaHistoryTopEntry, 0, pageSize)
	rows := make([]database.MediaPathID, 0, pageSize)
	for i := range pageSize {
		mediaPath := filepath.Join(string(filepath.Separator), "games", fmt.Sprintf("game-%03d.sfc", i))
		entries = append(entries, database.MediaHistoryTopEntry{
			SystemID: "SNES", MediaName: fmt.Sprintf("Game %d", i), MediaPath: mediaPath,
			TotalPlayTime: pageSize - i, SessionCount: 1, LastPlayedAt: now,
		})
		rows = append(rows, database.MediaPathID{
			SystemID: "SNES", Path: mediaPath, DBID: int64(1000 + i), MediaTitleDBID: int64(10000 + i),
		})
	}
	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), pageSize).Return(entries, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.MatchedBy(func(paths []string) bool {
		return len(paths) == pageSize
	})).Return(rows, nil).Once()
	mockMediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, mock.MatchedBy(func(refs []database.MediaRef) bool {
		return len(refs) == pageSize
	})).Return(map[int64][]database.TagInfo{}, nil).Once()

	result, err := HandleMediaHistoryTop(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		Params:   json.RawMessage(`{"limit": 100}`),
	})
	require.NoError(t, err)
	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, pageSize)
	for i := range resp.Entries {
		assert.Equal(t, int64(1000+i), resp.Entries[i].MediaID)
		assert.Equal(t, []database.TagInfo{}, resp.Entries[i].Tags)
	}
	mockMediaDB.AssertNumberOfCalls(t, "FindMediaIDsByPaths", 1)
	mockMediaDB.AssertNumberOfCalls(t, "GetMediaTagsByMediaRefs", 1)
	mockUserDB.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
}
