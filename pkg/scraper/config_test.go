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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultScraperConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultScraperConfig()
	require.NotNil(t, cfg)

	// Verify default values
	assert.Equal(t, "screenscraper", cfg.DefaultScraper)
	assert.Equal(t, "us", cfg.Region)
	assert.Equal(t, "en", cfg.Language)
	assert.True(t, cfg.DownloadCovers)
	assert.True(t, cfg.DownloadScreenshots)
	assert.False(t, cfg.DownloadVideos) // Videos off by default
	assert.Equal(t, 3, cfg.MaxConcurrent)
	assert.Equal(t, 1000, cfg.RateLimit)

	// Verify default media types
	expectedTypes := []MediaType{MediaTypeCover, MediaTypeScreenshot}
	assert.Equal(t, expectedTypes, cfg.DefaultMediaTypes)
}

func TestScraperConfigUpdateDefaultMediaTypes(t *testing.T) {
	t.Parallel()

	cfg := &ScraperConfig{
		DownloadCovers:      true,
		DownloadScreenshots: false,
		DownloadVideos:      true,
	}

	cfg.UpdateDefaultMediaTypes()

	expectedTypes := []MediaType{MediaTypeCover, MediaTypeVideo}
	assert.Equal(t, expectedTypes, cfg.DefaultMediaTypes)

	// Test with all disabled
	cfg.DownloadCovers = false
	cfg.DownloadScreenshots = false
	cfg.DownloadVideos = false
	cfg.UpdateDefaultMediaTypes()
	assert.Empty(t, cfg.DefaultMediaTypes)

	// Test with all enabled
	cfg.DownloadCovers = true
	cfg.DownloadScreenshots = true
	cfg.DownloadVideos = true
	cfg.UpdateDefaultMediaTypes()
	expectedTypes = []MediaType{MediaTypeCover, MediaTypeScreenshot, MediaTypeVideo}
	assert.Equal(t, expectedTypes, cfg.DefaultMediaTypes)
}

func TestGetScraperConfig(t *testing.T) {
	t.Parallel()

	// Create a mock platform using the existing testing infrastructure
	mockPlatform := &mocks.MockPlatform{}
	mockPlatform.SetupBasicMock()

	// Test with mock platform (function should handle this gracefully)
	cfg := GetScraperConfig(mockPlatform)

	require.NotNil(t, cfg)

	// Should return defaults since no config file exists
	assert.Equal(t, "screenscraper", cfg.DefaultScraper)
	assert.Equal(t, "us", cfg.Region)
	assert.Equal(t, "en", cfg.Language)

	// Media types should be updated
	expectedTypes := []MediaType{MediaTypeCover, MediaTypeScreenshot}
	assert.Equal(t, expectedTypes, cfg.DefaultMediaTypes)
}
