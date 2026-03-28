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
	"testing"

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
	).Return(int64(0), "", false)
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
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return(nil)
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

	require.Error(t, err)
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
	).Return(int64(42), "exact_match", true)

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
	assert.InDelta(t, 1.0, result.Confidence, 0.001)
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
	).Return(int64(42), "exact_match", true)

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

func TestResolveTitle_Strategy1_AllVariantsRejected(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	setupCacheMiss(mockMediaDB)

	// Strategy 1: Returns MULTIPLE results that are ALL variants.
	// SelectBestResult returns confidence 0.0 for all-variant results when
	// there are multiple (variant filtering kicks in with >1 results).
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Super Mario Bros (Demo)",
			Path:     "/games/nes/smb-demo.nes",
			Tags: []database.TagInfo{
				{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedDemo)},
			},
		},
		{
			MediaID:  2,
			SystemID: "NES",
			Name:     "Super Mario Bros (Beta)",
			Path:     "/games/nes/smb-beta.nes",
			Tags: []database.TagInfo{
				{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedBeta)},
			},
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
		GameName:  "Super Mario Bros",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.Error(t, err)
	assert.Nil(t, result)
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
	assert.Equal(t, StrategySecondaryTitleExact, result.Strategy)
	assert.Equal(t, "Ocarina of Time", result.Result.Name)
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
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
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
			{Type: string(tags.TagTypeYear), Value: "1985", Operator: zapscript.TagOperatorAND},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, StrategyExactMatch, result.Strategy)
	assert.GreaterOrEqual(t, result.Confidence, ConfidenceHigh)
	mockMediaDB.AssertCalled(t, "SetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, int64(1), StrategyExactMatch)
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
