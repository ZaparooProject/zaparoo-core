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

package igdb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewIGDB(t *testing.T) {
	scraper := NewIGDB()
	assert.NotNil(t, scraper)
	assert.NotNil(t, scraper.client)
	assert.NotNil(t, scraper.platformMap)
}

func TestGetInfo(t *testing.T) {
	scraper := NewIGDB()
	info := scraper.GetInfo()

	assert.Equal(t, "IGDB", info.Name)
	assert.Equal(t, "1.0", info.Version)
	assert.Equal(t, "https://igdb.com", info.Website)
	assert.True(t, info.RequiresAuth)
}

func TestIsSupportedPlatform(t *testing.T) {
	scraper := NewIGDB()

	// Test supported platforms
	assert.True(t, scraper.IsSupportedPlatform("snes"))
	assert.True(t, scraper.IsSupportedPlatform("nes"))
	assert.True(t, scraper.IsSupportedPlatform("psx"))

	// Test unsupported platform
	assert.False(t, scraper.IsSupportedPlatform("unknown"))
}

func TestGetSupportedMediaTypes(t *testing.T) {
	scraper := NewIGDB()
	mediaTypes := scraper.GetSupportedMediaTypes()

	expectedTypes := []string{"cover", "screenshot", "fanart", "video"}
	assert.Len(t, mediaTypes, len(expectedTypes))

	for _, expectedType := range expectedTypes {
		found := false
		for _, mediaType := range mediaTypes {
			if string(mediaType) == expectedType {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected media type %s not found", expectedType)
	}
}

func TestBuildSearchQuery(t *testing.T) {
	scraper := NewIGDB()
	query := scraper.buildSearchQuery("Super Mario World", "19")

	assert.Contains(t, query, "Super Mario World")
	assert.Contains(t, query, "platforms = (19)")
	assert.Contains(t, query, "category = 0")
	assert.Contains(t, query, "limit 10")
}

func TestBuildGameInfoQuery(t *testing.T) {
	scraper := NewIGDB()
	query := scraper.buildGameInfoQuery("123")

	assert.Contains(t, query, "where id = 123")
	assert.Contains(t, query, "cover.image_id")
	assert.Contains(t, query, "screenshots.image_id")
	assert.Contains(t, query, "artworks.image_id")
}

func TestConvertGameToResult(t *testing.T) {
	scraper := NewIGDB()

	game := Game{
		ID:      123,
		Name:    "Test Game",
		Summary: "A test game",
		Rating:  85.5,
	}

	result := scraper.convertGameToResult(&game, "snes")

	assert.Equal(t, "123", result.ID)
	assert.Equal(t, "Test Game", result.Name)
	assert.Equal(t, "A test game", result.Description)
	assert.Equal(t, "snes", result.SystemID)
	assert.Equal(t, "global", result.Region)
	assert.Equal(t, "en", result.Language)
	assert.Greater(t, result.Relevance, 0.5)
}
