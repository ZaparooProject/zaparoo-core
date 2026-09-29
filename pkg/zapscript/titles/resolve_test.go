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

package titles

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// setupCacheMiss configures the mock to return a cache miss.
func setupCacheMiss(m *helpers.MockMediaDBI) {
	m.On("GetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return(database.SlugResolution{}, false)
}

// setupAllStrategiesEmpty configures all strategy DB calls to return empty results.
func setupAllStrategiesEmpty(m *helpers.MockMediaDBI) {
	m.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	m.On("SearchMediaBySecondarySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	m.On("SearchMediaBySlugPrefix",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	m.On("SearchMediaBySlugIn",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	m.On("GetTitlesWithPreFilter",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.MediaTitle{}, nil)
}

// setupCacheWrite configures the mock to accept cache write calls.
func setupCacheWrite(m *helpers.MockMediaDBI) {
	m.On("SetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return(nil)
}

// setupCacheWriteSync configures the mock to accept cache write calls and
// returns a channel closed once the call happens. cacheSlugResolution runs
// the write in a background goroutine, so tests must wait on this channel
// before asserting on it instead of racing it.
func setupCacheWriteSync(m *helpers.MockMediaDBI) <-chan struct{} {
	done := make(chan struct{})
	m.On("SetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Run(func(mock.Arguments) {
		close(done)
	}).Return(nil)
	return done
}

func TestResolveTitle_StopsWhenContextCancelled(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	mockMediaDB.On("GetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return(database.SlugResolution{}, false)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Run(func(args mock.Arguments) {
		ctx, ok := args.Get(0).(context.Context)
		require.True(t, ok)
		<-ctx.Done()
	}).Return([]database.SearchResultWithCursor{}, context.DeadlineExceeded)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()

	result, err := ResolveTitle(ctx, &ResolveParams{
		SystemID:  "NES",
		GameName:  "Slow Game",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Less(t, time.Since(started), 500*time.Millisecond)
	mockMediaDB.AssertExpectations(t)
}

func TestResolveTitle_CacheWriteTimeoutDoesNotFailLaunch(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)
	fastPath := filepath.Join("roms", "nes", "fast.nes")
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{{
		SystemID: "nes",
		Name:     "Fast Game",
		Path:     fastPath,
		MediaID:  1,
	}}, nil)
	done := make(chan struct{})
	mockMediaDB.On("SetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Run(func(args mock.Arguments) {
		defer close(done)
		ctx, ok := args.Get(0).(context.Context)
		require.True(t, ok)
		<-ctx.Done()
	}).Return(context.DeadlineExceeded)

	started := time.Now()
	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Fast Game",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, fastPath, result.Result.Path)
	assert.Less(t, time.Since(started), 500*time.Millisecond)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cache write goroutine did not complete in time")
	}
	mockMediaDB.AssertExpectations(t)
}

func TestResolveTitle_ErrNoMatch(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)
	setupAllStrategiesEmpty(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Nonexistent Game",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.ErrorIs(t, err, ErrNoMatch)
	assert.Nil(t, result)
}

func TestResolveTitle_EmptySlug(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "!!!",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.ErrorIs(t, err, ErrNoMatch, "a title with nothing to match is a miss, not a failure")
	assert.Contains(t, err.Error(), "slugified to empty string")
	assert.Nil(t, result)
}

func TestResolveTitle_CacheHit(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	mockMediaDB.On("GetCachedSlugResolution",
		mock.Anything, "NES", mock.Anything, mock.Anything,
	).Return(database.SlugResolution{MediaDBID: 42, Strategy: "exact_match", Confidence: 0.65}, true)

	mockMediaDB.On("GetMediaByDBID", mock.Anything, int64(42)).Return(
		database.SearchResultWithCursor{
			MediaID:  42,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
			Tags:     []database.TagInfo{{Type: "year", Tag: "1985"}},
		}, nil)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Super Mario Bros",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	// A cache hit reports the score the match was cached with, not certainty.
	assert.InDelta(t, 0.65, result.Confidence, 0.001)
	assert.Equal(t, "exact_match", result.Strategy)
	assert.Equal(t, "Super Mario Bros", result.Result.Name)
}

func TestResolveTitle_CacheHitGetMediaByDBIDFails(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	// Cache hit but GetMediaByDBID fails → falls back to full resolution
	mockMediaDB.On("GetCachedSlugResolution",
		mock.Anything, "NES", mock.Anything, mock.Anything,
	).Return(database.SlugResolution{MediaDBID: 42, Strategy: "exact_match", Confidence: 0.65}, true)

	mockMediaDB.On("GetMediaByDBID", mock.Anything, int64(42)).Return(
		database.SearchResultWithCursor{}, errors.New("db error"))

	// Full resolution: Strategy 1 returns a single result
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
			Tags:     []database.TagInfo{{Type: "year", Tag: "1985"}},
		},
	}, nil)

	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Super Mario Bros",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Super Mario Bros", result.Result.Name)
}

func TestResolveTitle_Strategy1_ExactMatchHighConfidence(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	// Strategy 1: Single result, no tag filters → confidence 1.0 → early return
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, "NES", mock.AnythingOfType("string"), mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
			Tags:     []database.TagInfo{{Type: "year", Tag: "1985"}},
		},
	}, nil)

	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Super Mario Bros",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.GreaterOrEqual(t, result.Confidence, ConfidenceHigh)
	assert.Equal(t, StrategyExactMatch, result.Strategy)
	assert.Equal(t, "Super Mario Bros", result.Result.Name)
}

func TestResolveTitle_Strategy1_SearchError(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, errors.New("db error"))

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Super Mario Bros",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to search for slug")
	assert.Nil(t, result)
}

func TestResolveTitle_Strategy1_AllVariantsResolveWithinTitle(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)
	setupCacheWrite(mockMediaDB)

	// The same prototype filed under two folders. Every candidate carries the
	// requested title, so the match stays inside it. No weaker strategy is
	// mocked: reaching one fails the test.
	protoTags := []database.TagInfo{
		{Type: string(tags.TagTypeRegion), Tag: string(tags.TagRegionUS)},
		{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedProto)},
	}
	shallow := filepath.Join("games", "NES", "RoboCop versus The Terminator (USA) (Proto).nes")
	deep := filepath.Join("games", "NES", "Protos", "R-Z", "RoboCop versus The Terminator (USA) (Proto).nes")
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, "NES", mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{MediaID: 1, SystemID: "NES", Name: "RoboCop versus The Terminator", Path: deep, Tags: protoTags},
		{MediaID: 2, SystemID: "NES", Name: "RoboCop versus The Terminator", Path: shallow, Tags: protoTags},
	}, nil)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "RoboCop versus The Terminator",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, StrategyExactMatch, result.Strategy)
	assert.Equal(t, shallow, result.Result.Path)
	assert.InDelta(t, 1.0, result.Confidence, 0.001)
}

func TestResolveTitle_Strategy1_AllVariantsExcludedByRequest(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	// Keeping every candidate when they are all variants must not hand back one
	// the request excluded. Both files carry the refused tag, so the exact match
	// scores zero and the weaker strategies, which find nothing, settle it.
	protoTags := []database.TagInfo{
		{Type: string(tags.TagTypeRegion), Tag: string(tags.TagRegionUS)},
		{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedProto)},
	}
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, "NES", mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID: 1, SystemID: "NES", Name: "RoboCop versus The Terminator",
			Path: filepath.Join("games", "NES", "Protos", "RoboCop versus The Terminator (USA) (Proto).nes"),
			Tags: protoTags,
		},
		{
			MediaID: 2, SystemID: "NES", Name: "RoboCop versus The Terminator",
			Path: filepath.Join("games", "NES", "RoboCop versus The Terminator (USA) (Proto).nes"),
			Tags: protoTags,
		},
	}, nil)
	mockMediaDB.On("SearchMediaBySecondarySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugPrefix",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugIn",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("GetTitlesWithPreFilter",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.MediaTitle{}, nil)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "RoboCop versus The Terminator (-unfinished:proto)",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.ErrorIs(t, err, ErrNoMatch)
	assert.Nil(t, result)
	mockMediaDB.AssertNotCalled(t, "SetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestResolveTitle_Strategy1_TagMatchingSelectsUSA(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	// Multiple results with conflicting tags → tag filter selects the right one
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
			Tags: []database.TagInfo{
				{Type: string(tags.TagTypeRegion), Tag: string(tags.TagRegionJP)},
			},
		},
		{
			MediaID:  2,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb-usa.nes",
			Tags: []database.TagInfo{
				{Type: string(tags.TagTypeRegion), Tag: string(tags.TagRegionUS)},
			},
		},
	}, nil)

	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Super Mario Bros",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
		AdditionalTags: []zapscript.TagFilter{
			{Type: string(tags.TagTypeRegion), Value: string(tags.TagRegionUS), Operator: zapscript.TagOperatorAND},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, StrategyExactMatch, result.Strategy)
	assert.Equal(t, int64(2), result.Result.MediaID)
}

func TestResolveTitle_Strategy2_ExactMatchWithoutTags(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	// Strategy 1 uses non-nil tagFilters → empty
	nonNilTags := mock.MatchedBy(func(tf []zapscript.TagFilter) bool {
		return tf != nil
	})
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, nonNilTags,
	).Return([]database.SearchResultWithCursor{}, nil)

	// Strategy 2 uses nil tagFilters → returns result
	nilTags := mock.MatchedBy(func(tf []zapscript.TagFilter) bool {
		return tf == nil
	})
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, nilTags,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
		},
	}, nil)

	// Strategy 2 still goes through remaining strategies since confidence may be
	// below ConfidenceHigh, so mock them empty
	mockMediaDB.On("SearchMediaBySecondarySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugPrefix",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugIn",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("GetTitlesWithPreFilter",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.MediaTitle{}, nil)

	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Super Mario Bros",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
		AdditionalTags: []zapscript.TagFilter{
			{Type: string(tags.TagTypeRegion), Value: string(tags.TagRegionUS), Operator: zapscript.TagOperatorAND},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, StrategyExactMatch, result.Strategy)
	assert.Equal(t, "Super Mario Bros", result.Result.Name)
}

func TestResolveTitle_Strategy2_SearchError(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	// Strategy 1 (non-nil tags) → empty
	nonNilTags := mock.MatchedBy(func(tf []zapscript.TagFilter) bool {
		return tf != nil
	})
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, nonNilTags,
	).Return([]database.SearchResultWithCursor{}, nil)

	// Strategy 2 (nil tags) → error
	nilTags := mock.MatchedBy(func(tf []zapscript.TagFilter) bool {
		return tf == nil
	})
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, nilTags,
	).Return([]database.SearchResultWithCursor{}, errors.New("db error"))

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Super Mario Bros",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
		AdditionalTags: []zapscript.TagFilter{
			{Type: string(tags.TagTypeRegion), Value: string(tags.TagRegionUS), Operator: zapscript.TagOperatorAND},
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to search for slug")
	assert.Nil(t, result)
}

func TestResolveTitle_Strategy3_SecondaryTitleMatch(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	// Strategies 1+2 return empty for the full slug.
	// TrySecondaryTitleExact calls SearchMediaBySlug with secondary slug
	// and SearchMediaBySecondarySlug. The secondary slug "ocarinaoftime" differs
	// from the full slug "legendofzeldaocarinaoftime".
	fullSlug := mock.MatchedBy(func(slug string) bool {
		return slug == "legendofzeldaocarinaoftime"
	})
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, fullSlug, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)

	// Strategy 3: SearchMediaBySlug with secondary slug returns a result.
	// "Ocarina of Time" in DB has slug "ocarinaoftime" and no secondary title.
	secondarySlug := mock.MatchedBy(func(slug string) bool {
		return slug == "ocarinaoftime"
	})
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, secondarySlug, mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Ocarina of Time",
			Path:     "/games/nes/oot.nes",
		},
	}, nil)

	mockMediaDB.On("SearchMediaBySecondarySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugPrefix",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugIn",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("GetTitlesWithPreFilter",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.MediaTitle{}, nil)

	done := setupCacheWriteSync(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Legend of Zelda: Ocarina of Time",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, StrategySecondaryTitleExact, result.Strategy)
	assert.Equal(t, "Ocarina of Time", result.Result.Name)
	assert.Less(t, result.Confidence, ConfidenceHigh)

	// A match that is not certain is cached with the score it is returned with.
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cache write goroutine did not complete in time")
	}
	mockMediaDB.AssertCalled(t, "SetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		database.SlugResolution{MediaDBID: 1, Strategy: StrategySecondaryTitleExact, Confidence: result.Confidence})
}

func TestResolveTitle_Strategy4_FuzzyMatching(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	// Use "Donky Kong Country" (typo) → slug "donkykongcountry".
	// The fuzzy matcher should find "donkeykongcountry" in the pre-filter candidates.
	dbSlug := "donkeykongcountry"

	// Single SearchMediaBySlug mock that returns results only for the fuzzy matched slug.
	// Strategies 1+2 use the query slug "donkykongcountry" → empty.
	// Strategy 4 (fuzzy) retries with the matched slug "donkeykongcountry" → result.
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything,
		mock.MatchedBy(func(slug string) bool { return slug == dbSlug }),
		mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "SNES",
			Name:     "Donkey Kong Country",
			Path:     "/games/snes/dkc.sfc",
		},
	}, nil)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything,
		mock.MatchedBy(func(slug string) bool { return slug != dbSlug }),
		mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)

	mockMediaDB.On("SearchMediaBySecondarySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugPrefix",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugIn",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)

	// Strategy 4: GetTitlesWithPreFilter returns a candidate with close slug
	mockMediaDB.On("GetTitlesWithPreFilter",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.MediaTitle{
		{
			Slug:          dbSlug,
			Name:          "Donkey Kong Country",
			SecondarySlug: sql.NullString{},
			DBID:          1,
		},
	}, nil)

	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "SNES",
		GameName:  "Donky Kong Country",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Donkey Kong Country", result.Result.Name)
	assert.Equal(t, StrategyJaroWinklerDamerau, result.Strategy)
}

// TestResolveTitle_FuzzyMatchAmbiguousAcrossTitlesCompoundsDiscount documents
// and locks in an interaction between two independent safety nets: every raw
// Jaro-Winkler fuzzy result is discounted (resolve.go's Strategy 5, since a
// character-shape typo correction has no structural guarantee it reached the
// right title), and separately SelectBestResult discounts again whenever its
// tie-break resolves across results that are genuinely different titles (not
// just files of one). A fuzzy-matched slug that collides across two distinct
// MediaTitleIDs - e.g. two differently-spelled real MediaTitle rows for what a
// human would call "the same game" ("Ghosts'n Goblins" / "Ghosts 'N Goblins"
// sharing one slug after #1561's normalization fix is a real example from the
// live catalog) - triggers both at once, compounding to at most 1.0 * 0.75 *
// 0.75 = 0.5625 however good the underlying similarity was. That is always
// below ConfidenceMinimum (0.60), so this case can never launch - it always
// refuses with ErrLowConfidence, unconditionally, regardless of how close the
// typo was. That is intentional, not a bug: two independent sources of doubt
// (an uncertain character-shape guess, and then which of several literal
// title records it lands on) should compound, not cancel out, and refusing
// rather than guessing between different real titles is exactly the safe
// direction. This test pins that outcome so a future change to either
// discount that accidentally lets this case launch is a deliberate, visible
// decision, not a silent regression.
func TestResolveTitle_FuzzyMatchAmbiguousAcrossTitlesCompoundsDiscount(t *testing.T) {
	t.Parallel()

	const dbSlug = "donkeykongcountry"
	buildMocks := func(results []database.SearchResultWithCursor) *helpers.MockMediaDBI {
		mockMediaDB := helpers.NewMockMediaDBI()
		setupCacheMiss(mockMediaDB)
		mockMediaDB.On("SearchMediaBySlug",
			mock.Anything, mock.Anything,
			mock.MatchedBy(func(slug string) bool { return slug == dbSlug }),
			mock.Anything,
		).Return(results, nil)
		mockMediaDB.On("SearchMediaBySlug",
			mock.Anything, mock.Anything,
			mock.MatchedBy(func(slug string) bool { return slug != dbSlug }),
			mock.Anything,
		).Return([]database.SearchResultWithCursor{}, nil)
		mockMediaDB.On("SearchMediaBySecondarySlug",
			mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		).Return([]database.SearchResultWithCursor{}, nil)
		mockMediaDB.On("SearchMediaBySlugPrefix",
			mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		).Return([]database.SearchResultWithCursor{}, nil)
		mockMediaDB.On("SearchMediaBySlugIn",
			mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		).Return([]database.SearchResultWithCursor{}, nil)
		mockMediaDB.On("GetTitlesWithPreFilter",
			mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		).Return([]database.MediaTitle{
			{Slug: dbSlug, Name: "Donkey Kong Country", DBID: 1},
		}, nil)
		setupCacheWrite(mockMediaDB)
		return mockMediaDB
	}
	resolve := func(t *testing.T, mockMediaDB *helpers.MockMediaDBI) (*ResolveResult, error) {
		t.Helper()
		cfg, err := helpers.NewTestConfig(nil, t.TempDir())
		require.NoError(t, err)
		return ResolveTitle(context.Background(), &ResolveParams{
			SystemID:  "SNES",
			GameName:  "Donky Kong Country",
			MediaDB:   mockMediaDB,
			Cfg:       cfg,
			MediaType: slugs.MediaTypeGame,
		})
	}

	single, err := resolve(t, buildMocks([]database.SearchResultWithCursor{
		{MediaID: 1, MediaTitleID: 101, SystemID: "SNES", Name: "Donkey Kong Country", Path: "/games/snes/dkc.sfc"},
	}))
	require.NoError(t, err, "a single-title fuzzy match launches normally")
	require.NotNil(t, single)
	require.Equal(t, StrategyJaroWinklerDamerau, single.Strategy)
	assert.GreaterOrEqual(t, single.Confidence, ConfidenceMinimum)

	_, err = resolve(t, buildMocks([]database.SearchResultWithCursor{
		{MediaID: 1, MediaTitleID: 101, SystemID: "SNES", Name: "Donkey Kong Country", Path: "/games/snes/dkc.sfc"},
		{MediaID: 2, MediaTitleID: 102, SystemID: "SNES", Name: "Donkey Kong Kountry", Path: "/games/snes/dkc2.sfc"},
	}))
	require.ErrorIs(t, err, ErrLowConfidence,
		"a fuzzy match ambiguous across title IDs must always refuse rather than guess, since the two "+
			"discounts compound to at most 0.5625 - below ConfidenceMinimum however good the typo match was")
}

// TestResolveTitle_UnnumberedFirstGameStillResolves is the control for
// SameTitleNumbers' one addition beyond issue #1561's own ask: a lone "1" is
// treated as no number, so a series' unnumbered first game - "Final Fantasy,"
// indexed with no number - still resolves from a query that spells it out as
// "Final Fantasy I". This goes through the fuzzy strategy, not the bare-prefix
// one: "finalfantasy1" (13 chars) and "finalfantasy" (12 chars) are within the
// fuzzy length window, and "finalfantasy" is the shorter of the two, so it's
// never reachable as a bare-prefix match (that direction requires the DB title
// to be the longer one).
func TestResolveTitle_UnnumberedFirstGameStillResolves(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	const dbSlug = "finalfantasy"
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything,
		mock.MatchedBy(func(slug string) bool { return slug == dbSlug }),
		mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{MediaID: 1, SystemID: "NES", Name: "Final Fantasy", Path: "/games/nes/ff.nes"},
	}, nil)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything,
		mock.MatchedBy(func(slug string) bool { return slug != dbSlug }),
		mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySecondarySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugPrefix",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugIn",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("GetTitlesWithPreFilter",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.MediaTitle{
		{Slug: dbSlug, Name: "Final Fantasy", DBID: 1},
	}, nil)

	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Final Fantasy I",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Final Fantasy", result.Result.Name)
	assert.Equal(t, StrategyJaroWinklerDamerau, result.Strategy)
}

func TestResolveTitle_Strategy5_MainTitleOnly(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	// Use "Legend of Zelda: Ocarina of Time" with full slug "legendofzeldaocarinaoftime".
	// Strategies 1-2: SearchMediaBySlug returns empty for the full slug.
	// Strategy 3: SearchMediaBySlug for secondary slug + SearchMediaBySecondarySlug both empty.
	// Strategy 4: GetTitlesWithPreFilter returns empty.
	// Strategy 5: SearchMediaBySlugPrefix with main title slug "legendofzelda" finds a result.

	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySecondarySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("GetTitlesWithPreFilter",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.MediaTitle{}, nil)
	mockMediaDB.On("SearchMediaBySlugIn",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)

	// Strategy 5: Prefix match on main title slug finds a different edition
	mockMediaDB.On("SearchMediaBySlugPrefix",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Legend of Zelda",
			Path:     "/games/nes/zelda.nes",
		},
	}, nil)

	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Legend of Zelda: Ocarina of Time",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, StrategyMainTitleOnly, result.Strategy)
	assert.Equal(t, "Legend of Zelda", result.Result.Name)
}

// sf2Titles are the real MiSTer Arcade file names for issue #1561: none of
// them carry a colon/dash, so only the new bare-prefix case in TryMainTitleOnly
// can reach them; the fuzzy strategy is isolated out (GetTitlesWithPreFilter
// mocked empty) so this test exercises exactly the strategy this fix adds.
var sf2Titles = []database.SearchResultWithCursor{
	{
		MediaID: 1, MediaTitleID: 101, SystemID: "Arcade",
		Name: "Street Fighter II The World Warrior", Path: "/media/fat/_Arcade/sf2ww.mra",
	},
	{
		MediaID: 2, MediaTitleID: 102, SystemID: "Arcade",
		Name: "Street Fighter II' Champion Edition", Path: "/media/fat/_Arcade/sf2ce.mra",
	},
	{
		MediaID: 3, MediaTitleID: 103, SystemID: "Arcade",
		Name: "Street Fighter II' Hyper Fighting", Path: "/media/fat/_Arcade/sf2hf.mra",
	},
}

// setupSequelResolveMocks wires every strategy but 6 (TryMainTitleOnly) to
// return empty, isolating the new bare-prefix case the same way every other
// strategy test in this file isolates its own target.
func setupSequelResolveMocks(m *helpers.MockMediaDBI) {
	setupCacheMiss(m)
	m.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	m.On("SearchMediaBySecondarySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	m.On("GetTitlesWithPreFilter",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.MediaTitle{}, nil)
	m.On("SearchMediaBySlugIn",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	m.On("SearchMediaBySlugPrefix",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return(sf2Titles, nil)
	setupCacheWrite(m)
}

// TestResolveTitle_SequelQueryReachesRealSequelNotOriginal is issue #1561 end
// to end: "Street Fighter II" (and its "2" spelling) used to launch "Street
// Fighter (US, set 1)" - the 1991 original - at reported 0.99 confidence,
// because "streetfighter2" and "streetfighter" differ by one character and
// Jaro-Winkler scored that a near-perfect typo match. It must now resolve to
// one of the three real Street Fighter II games, never the original, and the
// pick - a guess among three distinct games, not a choice of file for one -
// must be scored as one: above the launch floor, below "acceptable."
func TestResolveTitle_SequelQueryReachesRealSequelNotOriginal(t *testing.T) {
	t.Parallel()

	sf2Names := map[string]bool{
		"Street Fighter II The World Warrior": true,
		"Street Fighter II' Champion Edition": true,
		"Street Fighter II' Hyper Fighting":   true,
	}

	for _, query := range []string{"Street Fighter II", "Street Fighter 2"} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()

			mockMediaDB := helpers.NewMockMediaDBI()
			cfg, err := helpers.NewTestConfig(nil, t.TempDir())
			require.NoError(t, err)
			setupSequelResolveMocks(mockMediaDB)

			result, err := ResolveTitle(context.Background(), &ResolveParams{
				SystemID:  "Arcade",
				GameName:  query,
				MediaDB:   mockMediaDB,
				Cfg:       cfg,
				MediaType: slugs.MediaTypeGame,
			})

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.NotEqual(t, "Street Fighter (US, set 1)", result.Result.Name,
				"must never launch the original game for a sequel query")
			assert.True(t, sf2Names[result.Result.Name],
				"expected one of the real Street Fighter II games, got %q", result.Result.Name)
			assert.Equal(t, StrategyMainTitleOnly, result.Strategy)
			assert.Greater(t, result.Confidence, ConfidenceMinimum)
			assert.Less(t, result.Confidence, ConfidenceAcceptable,
				"a pick among several distinct sequel games is a guess, not a sure match")
		})
	}
}

// TestResolveTitle_OverspecificSequelQueryNeverLaunchesOriginal covers the
// reported bug's other query, "Street Fighter II Turbo," which is not a real
// title and is not a word-for-word prefix of any of the three real games
// either (none of them continues with "Turbo" as the fourth word) - so unlike
// the bare "Street Fighter II" above, this one is correctly allowed to find no
// match. What it must never do, with or without a match, is reach the original.
func TestResolveTitle_OverspecificSequelQueryNeverLaunchesOriginal(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	setupSequelResolveMocks(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "Arcade",
		GameName:  "Street Fighter II Turbo",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})
	if err != nil {
		require.ErrorIs(t, err, ErrNoMatch)
		assert.Nil(t, result)
		return
	}
	require.NotNil(t, result)
	assert.NotEqual(t, "Street Fighter (US, set 1)", result.Result.Name,
		"must never launch the original game for a sequel query")
}

// TestResolveTitle_LongSharedPrefixIsNotATypoOfAnUnrelatedTitle is the fuzzy
// residual live-confirmed on the MiSTer device after the first #1561 fix
// shipped: "Street Fighter II Turbo" stopped launching the original game, but
// started launching "Street Fighter Zero 2" instead (confidence 0.927,
// undiscounted). Both slugs carry a "2", so SameTitleNumbers doesn't separate
// them, and Jaro-Winkler's prefix weighting scores their shared "streetfighter"
// (13 characters) highly regardless of the completely different "2turbo"/
// "zero2" that follows. Unlike the bare-prefix cases above, this exercises the
// fuzzy strategy directly, so GetTitlesWithPreFilter is NOT mocked empty here.
func TestResolveTitle_LongSharedPrefixIsNotATypoOfAnUnrelatedTitle(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySecondarySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugPrefix",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugIn",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("GetTitlesWithPreFilter",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.MediaTitle{
		{Slug: "streetfighterzero2", Name: "Street Fighter Zero 2", DBID: 1},
	}, nil)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "Arcade",
		GameName:  "Street Fighter II Turbo",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.ErrorIs(t, err, ErrNoMatch)
	assert.Nil(t, result)
}

// TestResolveTitle_FuzzyRejectsCoincidentalScore is the other shape issue
// #1561's fuzzy residual took, live-confirmed on the MiSTer test device once
// the shared-prefix fix above was already in place: "Metriod" (a typo of
// "Metroid," a console game not on this system) scored "Mr. Do!" ("misterdo"
// once "Mr." expands) at 0.855, with barely a shared prefix to blame -
// Jaro-Winkler's core, position-window matching did this on its own. Both
// take 5 real edits, same as the shared-prefix case, and
// WithinEditDistanceBudget rejects both the same way.
func TestResolveTitle_FuzzyRejectsCoincidentalScore(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySecondarySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugPrefix",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugIn",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("GetTitlesWithPreFilter",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.MediaTitle{
		{Slug: "misterdo", Name: "Mr. Do!", DBID: 1},
	}, nil)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "Arcade",
		GameName:  "Metriod",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.ErrorIs(t, err, ErrNoMatch)
	assert.Nil(t, result)
}

func TestResolveTitle_Strategy6_ProgressiveTrim(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	// Use "Donkey Kong Country Returns Tropical Freeze" — a long enough title
	// to produce trim candidates (>=3 words). Slug is
	// "donkeykongcountryreturnstropicalfreeze".
	// All strategies 1-5 return empty. Strategy 6 SearchMediaBySlugIn returns
	// a match for one of the trimmed candidates (e.g., "donkeykongcountry").

	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySecondarySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("GetTitlesWithPreFilter",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.MediaTitle{}, nil)
	mockMediaDB.On("SearchMediaBySlugPrefix",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)

	// Strategy 6: Progressive trim finds a match via SearchMediaBySlugIn
	mockMediaDB.On("SearchMediaBySlugIn",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "SNES",
			Name:     "Donkey Kong Country",
			Path:     "/games/snes/dkc.sfc",
		},
	}, nil)

	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "SNES",
		GameName:  "Donkey Kong Country Returns Tropical Freeze",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, StrategyProgressiveTrim, result.Strategy)
	assert.Equal(t, "Donkey Kong Country", result.Result.Name)
}

func TestResolveTitle_ErrLowConfidence(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	// Strategy 1: Single result with conflicting tag values.
	// With 2 AND filters (region:us conflicts with result's region:jp,
	// lang:en matches): matchRatio=1/2=0.5, conflictPenalty=0.2,
	// tagConfidence=0.3, confidence = 1.0 * 0.3 = 0.3
	// This is > 0.0 but < ConfidenceMinimum (0.60) → ErrLowConfidence
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
			Tags: []database.TagInfo{
				{Type: string(tags.TagTypeRegion), Tag: string(tags.TagRegionJP)},
				{Type: string(tags.TagTypeLang), Tag: string(tags.TagLangEN)},
			},
		},
	}, nil)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Super Mario Bros",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
		AdditionalTags: []zapscript.TagFilter{
			{Type: string(tags.TagTypeRegion), Value: string(tags.TagRegionUS), Operator: zapscript.TagOperatorAND},
			{Type: string(tags.TagTypeLang), Value: string(tags.TagLangEN), Operator: zapscript.TagOperatorAND},
		},
	})

	require.ErrorIs(t, err, ErrLowConfidence)
	assert.Nil(t, result)
}

func TestResolveTitle_SetCacheFailureDoesNotBlock(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
			Tags:     []database.TagInfo{{Type: "year", Tag: "1985"}},
		},
	}, nil)

	// Cache write fails — should not affect result
	mockMediaDB.On("SetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return(errors.New("cache write error"))

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Super Mario Bros",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Super Mario Bros", result.Result.Name)
}

func TestResolveTitle_BestCandidateCachedAndReturned(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	// Strategy 1: Single result where region and lang match, year tag is
	// missing from the result. Missing tag types are neutral (skipped),
	// so confidence = 1.0 * 1.0 = 1.0 → high confidence early exit.
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
			Tags: []database.TagInfo{
				{Type: string(tags.TagTypeRegion), Tag: string(tags.TagRegionUS)},
				{Type: string(tags.TagTypeLang), Tag: string(tags.TagLangEN)},
			},
		},
	}, nil)

	done := setupCacheWriteSync(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Super Mario Bros",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
		AdditionalTags: []zapscript.TagFilter{
			{Type: string(tags.TagTypeRegion), Value: string(tags.TagRegionUS), Operator: zapscript.TagOperatorAND},
			{Type: string(tags.TagTypeLang), Value: string(tags.TagLangEN), Operator: zapscript.TagOperatorAND},
			{Type: string(tags.TagTypeYear), Value: "1985", Operator: zapscript.TagOperatorAND},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, StrategyExactMatch, result.Strategy)
	assert.GreaterOrEqual(t, result.Confidence, ConfidenceHigh)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cache write goroutine did not complete in time")
	}
	mockMediaDB.AssertCalled(t, "SetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		database.SlugResolution{MediaDBID: 1, Strategy: StrategyExactMatch, Confidence: result.Confidence})
}

func TestResolveTitle_MissingTagTypeIsNeutral(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	// Result has region:us but no lang tag at all. Filters request both
	// region:us AND lang:en. The missing lang tag type should be neutral
	// (skipped), so only region is evaluated → matches → high confidence.
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
			Tags: []database.TagInfo{
				{Type: string(tags.TagTypeRegion), Tag: string(tags.TagRegionUS)},
			},
		},
	}, nil)

	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Super Mario Bros",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
		AdditionalTags: []zapscript.TagFilter{
			{Type: string(tags.TagTypeRegion), Value: string(tags.TagRegionUS), Operator: zapscript.TagOperatorAND},
			{Type: string(tags.TagTypeLang), Value: string(tags.TagLangEN), Operator: zapscript.TagOperatorAND},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.GreaterOrEqual(t, result.Confidence, ConfidenceHigh)
}

func TestResolveTitle_FilenameTagExtraction(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	// "Super Mario Bros (USA)" extracts filename tags:
	//   region:us (from "USA" mapping) + lang:en (implied by USA)
	// Slug is "supermariobrothers" (brackets stripped, "Bros" expanded).
	// The result has both matching tags → high confidence → early return.
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
			Tags: []database.TagInfo{
				{Type: string(tags.TagTypeRegion), Tag: string(tags.TagRegionUS)},
				{Type: string(tags.TagTypeLang), Tag: string(tags.TagLangEN)},
			},
		},
	}, nil)

	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Super Mario Bros (USA)",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, StrategyExactMatch, result.Strategy)
	assert.Equal(t, "Super Mario Bros", result.Result.Name)
}
