// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package scraper

import (
	"context"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMetadataStorage_GetMetadata_Success(t *testing.T) {
	t.Parallel()

	// Create mock database
	mockDB := helpers.NewMockMediaDBI()
	storage := NewMetadataStorage(mockDB)

	// Set up test data
	mediaTitleDBID := int64(123)
	scraperSource := "igdb"

	// Mock the GetTagsForMediaTitle call
	tags := map[string]string{
		"scraper_source": "igdb",
		"description":    "A great adventure game",
		"genre":          "Adventure",
		"players":        "1-2",
		"release_date":   "1991",
		"developer":      "Nintendo",
		"publisher":      "Nintendo",
		"rating":         "9.5",
		"scraped_at":     "1640995200", // Unix timestamp
	}
	mockDB.On("GetTagsForMediaTitle", mediaTitleDBID).Return(tags, nil)

	// Call GetMetadata
	ctx := context.Background()
	metadata, err := storage.GetMetadata(ctx, mediaTitleDBID, scraperSource)

	// Verify results
	require.NoError(t, err)
	require.NotNil(t, metadata)

	assert.Equal(t, mediaTitleDBID, metadata.MediaTitleDBID)
	assert.Equal(t, "igdb", metadata.ScraperSource)
	assert.Equal(t, "A great adventure game", metadata.Description)
	assert.Equal(t, "Adventure", metadata.Genre)
	assert.Equal(t, "1-2", metadata.Players)
	assert.Equal(t, "1991", metadata.ReleaseDate)
	assert.Equal(t, "Nintendo", metadata.Developer)
	assert.Equal(t, "Nintendo", metadata.Publisher)
	assert.InDelta(t, 9.5, metadata.Rating, 0.001)
	assert.Equal(t, time.Unix(1640995200, 0), metadata.ScrapedAt)

	mockDB.AssertExpectations(t)
}

func TestMetadataStorage_GetMetadata_NoScraperSource(t *testing.T) {
	t.Parallel()

	// Create mock database
	mockDB := helpers.NewMockMediaDBI()
	storage := NewMetadataStorage(mockDB)

	// Set up test data - no scraper_source tag
	mediaTitleDBID := int64(123)

	tags := map[string]string{
		"description": "A great adventure game",
		"genre":       "Adventure",
	}
	mockDB.On("GetTagsForMediaTitle", mediaTitleDBID).Return(tags, nil)

	// Call GetMetadata without specifying scraper source
	ctx := context.Background()
	metadata, err := storage.GetMetadata(ctx, mediaTitleDBID, "")

	// Should return nil when no scraper metadata found
	require.NoError(t, err)
	assert.Nil(t, metadata)

	mockDB.AssertExpectations(t)
}

func TestMetadataStorage_GetMetadata_WrongScraperSource(t *testing.T) {
	t.Parallel()

	// Create mock database
	mockDB := helpers.NewMockMediaDBI()
	storage := NewMetadataStorage(mockDB)

	// Set up test data with different scraper source
	mediaTitleDBID := int64(123)

	tags := map[string]string{
		"scraper_source": "screenscraper",
		"description":    "A great adventure game",
		"genre":          "Adventure",
	}
	mockDB.On("GetTagsForMediaTitle", mediaTitleDBID).Return(tags, nil)

	// Call GetMetadata requesting IGDB data
	ctx := context.Background()
	metadata, err := storage.GetMetadata(ctx, mediaTitleDBID, "igdb")

	// Should return nil when wrong scraper source
	require.NoError(t, err)
	assert.Nil(t, metadata)

	mockDB.AssertExpectations(t)
}

func TestMetadataStorage_GetMetadata_DatabaseError(t *testing.T) {
	t.Parallel()

	// Create mock database
	mockDB := helpers.NewMockMediaDBI()
	storage := NewMetadataStorage(mockDB)

	// Set up test data
	mediaTitleDBID := int64(123)

	// Mock database error
	mockDB.On("GetTagsForMediaTitle", mediaTitleDBID).Return(nil, assert.AnError)

	// Call GetMetadata
	ctx := context.Background()
	metadata, err := storage.GetMetadata(ctx, mediaTitleDBID, "")

	// Should return error
	require.Error(t, err)
	assert.Nil(t, metadata)
	assert.Contains(t, err.Error(), "failed to get tags for media title")

	mockDB.AssertExpectations(t)
}

func TestMetadataStorage_GetMetadata_InvalidRating(t *testing.T) {
	t.Parallel()

	// Create mock database
	mockDB := helpers.NewMockMediaDBI()
	storage := NewMetadataStorage(mockDB)

	// Set up test data with invalid rating
	mediaTitleDBID := int64(123)

	tags := map[string]string{
		"scraper_source": "igdb",
		"description":    "A great adventure game",
		"rating":         "not-a-number",
	}
	mockDB.On("GetTagsForMediaTitle", mediaTitleDBID).Return(tags, nil)

	// Call GetMetadata
	ctx := context.Background()
	metadata, err := storage.GetMetadata(ctx, mediaTitleDBID, "")

	// Should succeed but rating should be 0
	require.NoError(t, err)
	require.NotNil(t, metadata)
	assert.InDelta(t, float64(0), metadata.Rating, 0.001)

	mockDB.AssertExpectations(t)
}

func TestMetadataStorage_GetMetadata_AnyScraperSource(t *testing.T) {
	t.Parallel()

	// Create mock database
	mockDB := helpers.NewMockMediaDBI()
	storage := NewMetadataStorage(mockDB)

	// Set up test data
	mediaTitleDBID := int64(123)

	tags := map[string]string{
		"scraper_source": "screenscraper",
		"description":    "A great adventure game",
		"genre":          "Adventure",
	}
	mockDB.On("GetTagsForMediaTitle", mediaTitleDBID).Return(tags, nil)

	// Call GetMetadata without specifying scraper source (should accept any)
	ctx := context.Background()
	metadata, err := storage.GetMetadata(ctx, mediaTitleDBID, "")

	// Should return metadata regardless of source
	require.NoError(t, err)
	require.NotNil(t, metadata)
	assert.Equal(t, "screenscraper", metadata.ScraperSource)
	assert.Equal(t, "A great adventure game", metadata.Description)

	mockDB.AssertExpectations(t)
}

func TestMetadataStorage_StoreMetadata_Success(t *testing.T) {
	t.Parallel()

	// Create mock database
	mockDB := helpers.NewMockMediaDBI()
	storage := NewMetadataStorage(mockDB)

	// Set up test metadata
	metadata := &ScrapedMetadata{
		MediaTitleDBID: 123,
		ScraperSource:  "igdb",
		Description:    "A great adventure game",
		Genre:          "Adventure",
		Players:        "1-2",
		ReleaseDate:    "1991",
		Developer:      "Nintendo",
		Publisher:      "Nintendo",
		Rating:         9.5,
		ScrapedAt:      time.Now(),
	}

	// Mock all the database calls that StoreMetadata will make
	// We expect calls to FindOrInsertTagType and related methods
	mockDB.On("FindOrInsertTagType", mock.AnythingOfType("database.TagType")).Return(
		func(tagType any) any {
			return tagType // Return the same object with a fake DBID
		}, nil).Maybe()

	mockDB.On("FindOrInsertTag", mock.AnythingOfType("database.Tag")).Return(
		func(tag any) any {
			return tag // Return the same object with a fake DBID
		}, nil).Maybe()

	mockDB.On("FindOrInsertMediaTitleTag", mock.AnythingOfType("database.MediaTitleTag")).Return(
		func(mtt any) any {
			return mtt // Return the same object with a fake DBID
		}, nil).Maybe()

	// Call StoreMetadata
	ctx := context.Background()
	err := storage.StoreMetadata(ctx, metadata)

	// Should succeed
	require.NoError(t, err)

	// Verify that database methods were called
	mockDB.AssertExpectations(t)
}
