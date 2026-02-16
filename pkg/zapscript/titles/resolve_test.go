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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestResolveTitle_ErrNoMatch(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	// Cache miss
	mockMediaDB.On("GetCachedSlugResolution",
		mock.Anything, "NES", mock.Anything, mock.Anything,
	).Return(int64(0), "", false)

	// All strategies return empty
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
	).Return([]database.MediaTitle{}, nil)

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

	// A game name that slugifies to empty (e.g., all punctuation)
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

	year := "1985"
	mockMediaDB.On("GetCachedSlugResolution",
		mock.Anything, "NES", mock.Anything, mock.Anything,
	).Return(int64(42), "exact_match", true)

	mockMediaDB.On("GetMediaByDBID", mock.Anything, int64(42)).Return(
		database.SearchResultWithCursor{
			MediaID:  42,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
			Year:     &year,
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

func TestResolveTitle_ExactMatchHighConfidence(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	// Cache miss
	mockMediaDB.On("GetCachedSlugResolution",
		mock.Anything, "NES", mock.Anything, mock.Anything,
	).Return(int64(0), "", false)

	year := "1985"
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, "NES", mock.AnythingOfType("string"), mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
			Year:     &year,
		},
	}, nil)

	// Cache the resolution
	mockMediaDB.On("SetCachedSlugResolution",
		mock.Anything, "NES", mock.Anything, mock.Anything, int64(1), mock.Anything,
	).Return(nil)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Super Mario Bros",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Greater(t, result.Confidence, 0.0)
	assert.Equal(t, "Super Mario Bros", result.Result.Name)
}
