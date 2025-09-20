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

package thegamesdb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewTheGamesDB(t *testing.T) {
	scraper := NewTheGamesDB()
	assert.NotNil(t, scraper)
	assert.NotNil(t, scraper.client)
	assert.NotNil(t, scraper.platformMap)
}

func TestGetInfo(t *testing.T) {
	scraper := NewTheGamesDB()
	info := scraper.GetInfo()

	assert.Equal(t, "TheGamesDB", info.Name)
	assert.Equal(t, "1.0", info.Version)
	assert.Equal(t, "https://thegamesdb.net", info.Website)
	assert.True(t, info.RequiresAuth)
}

func TestIsSupportedPlatform(t *testing.T) {
	scraper := NewTheGamesDB()

	// Test supported platforms
	assert.True(t, scraper.IsSupportedPlatform("SNES"))
	assert.True(t, scraper.IsSupportedPlatform("NES"))
	assert.True(t, scraper.IsSupportedPlatform("PSX"))

	// Test unsupported platform
	assert.False(t, scraper.IsSupportedPlatform("unknown"))
}

func TestGetSupportedMediaTypes(t *testing.T) {
	scraper := NewTheGamesDB()
	mediaTypes := scraper.GetSupportedMediaTypes()

	expectedTypes := []string{"cover", "boxback", "screenshot", "fanart", "marquee"}
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

func TestMapMediaType(t *testing.T) {
	scraper := NewTheGamesDB()

	// Test boxart mapping
	assert.Equal(t, "cover", scraper.mapMediaType("boxart", "front"))
	assert.Equal(t, "boxback", scraper.mapMediaType("boxart", "back"))

	// Test other types
	assert.Equal(t, "screenshot", scraper.mapMediaType("screenshot", ""))
	assert.Equal(t, "fanart", scraper.mapMediaType("fanart", ""))
	assert.Equal(t, "marquee", scraper.mapMediaType("banner", ""))

	// Test unknown type
	assert.Empty(t, scraper.mapMediaType("unknown", ""))
}

func TestParseRating(t *testing.T) {
	scraper := NewTheGamesDB()

	assert.InDelta(t, 1.0, scraper.parseRating("E - Everyone"), 0.001)
	assert.InDelta(t, 2.0, scraper.parseRating("E10+"), 0.001)
	assert.InDelta(t, 3.0, scraper.parseRating("T - Teen"), 0.001)
	assert.InDelta(t, 4.0, scraper.parseRating("M"), 0.001)
	assert.InDelta(t, 5.0, scraper.parseRating("AO - Adults Only 18+"), 0.001)
	assert.InDelta(t, 0.0, scraper.parseRating("Unknown"), 0.001)
}
