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

package screenscraper

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewScreenScraper(t *testing.T) {
	t.Parallel()

	ss := NewScreenScraper()
	require.NotNil(t, ss)

	// Verify ScreenScraper implements the Scraper interface
	var _ scraper.Scraper = ss

	// Test GetInfo
	info := ss.GetInfo()
	assert.Equal(t, "ScreenScraper", info.Name)
	assert.Equal(t, "1.0", info.Version)
	assert.True(t, info.RequiresAuth)
	assert.Contains(t, info.Description, "ScreenScraper.fr")
	assert.Equal(t, "https://www.screenscraper.fr", info.Website)
}

func TestScreenScraperSupportedMediaTypes(t *testing.T) {
	t.Parallel()

	ss := NewScreenScraper()
	mediaTypes := ss.GetSupportedMediaTypes()

	// Verify expected media types are supported
	expectedTypes := []scraper.MediaType{
		scraper.MediaTypeCover,
		scraper.MediaTypeBoxBack,
		scraper.MediaTypeScreenshot,
		scraper.MediaTypeTitleShot,
		scraper.MediaTypeFanArt,
		scraper.MediaTypeMarquee,
		scraper.MediaTypeWheel,
		scraper.MediaTypeVideo,
		scraper.MediaTypeManual,
	}

	assert.Len(t, mediaTypes, len(expectedTypes))
	for _, expectedType := range expectedTypes {
		assert.Contains(t, mediaTypes, expectedType)
	}
}

func TestScreenScraperPlatformSupport(t *testing.T) {
	t.Parallel()

	ss := NewScreenScraper()

	// Test supported platforms
	supportedPlatforms := []string{
		"NES", "SNES", "Genesis", "PSX", "PS2", "Nintendo64", "GBA", "Arcade",
	}

	for _, platform := range supportedPlatforms {
		assert.True(t, ss.IsSupportedPlatform(platform),
			"Platform %s should be supported", platform)
	}

	// Test unsupported platform
	assert.False(t, ss.IsSupportedPlatform("unknown_platform"))
}

func TestScreenScraperMediaTypeMapping(t *testing.T) {
	t.Parallel()

	ss := NewScreenScraper()

	// Test media type mapping
	testCases := []struct {
		ssType   string
		expected string
	}{
		{"box-2D", string(scraper.MediaTypeCover)},
		{"box-back", string(scraper.MediaTypeBoxBack)},
		{"ss", string(scraper.MediaTypeScreenshot)},
		{"sstitle", string(scraper.MediaTypeTitleShot)},
		{"fanart", string(scraper.MediaTypeFanArt)},
		{"marquee", string(scraper.MediaTypeMarquee)},
		{"wheel", string(scraper.MediaTypeWheel)},
		{"video", string(scraper.MediaTypeVideo)},
		{"manual", string(scraper.MediaTypeManual)},
		{"bezel", string(scraper.MediaTypeBezel)},
		{"map", string(scraper.MediaTypeMap)},
		{"unknown", ""}, // Should return empty for unknown types
	}

	for _, tc := range testCases {
		result := ss.mapMediaType(tc.ssType)
		assert.Equal(t, tc.expected, result,
			"Media type mapping for %s should be %s", tc.ssType, tc.expected)
	}

	// Test fallback patterns
	assert.Equal(t, string(scraper.MediaTypeCover), ss.mapMediaType("box-3D"))
	assert.Equal(t, string(scraper.MediaTypeScreenshot), ss.mapMediaType("screenshot-gameplay"))
}

func TestScreenScraperTextExtraction(t *testing.T) {
	t.Parallel()

	ss := NewScreenScraper()

	// Test text extraction with multiple languages/regions
	texts := []Text{
		{Region: "us", Language: "en", Text: "English US"},
		{Region: "eu", Language: "en", Text: "English EU"},
		{Region: "jp", Language: "ja", Text: "Japanese"},
		{Region: "fr", Language: "fr", Text: "French"},
	}

	// Test exact match
	result := ss.getPreferredText(texts, "us", "en")
	assert.Equal(t, "English US", result)

	// Test language fallback
	result = ss.getPreferredText(texts, "au", "en")
	assert.Equal(t, "English US", result) // Should pick first English

	// Test region fallback
	result = ss.getPreferredText(texts, "us", "es")
	assert.Equal(t, "English US", result) // Should pick US region

	// Test first available fallback
	result = ss.getPreferredText(texts, "br", "pt")
	assert.Equal(t, "English US", result) // Should pick first available

	// Test empty texts
	result = ss.getPreferredText([]Text{}, "us", "en")
	assert.Empty(t, result)
}

func TestScreenScraperGameConversion(t *testing.T) {
	t.Parallel()

	ss := NewScreenScraper()

	// Create test game data
	game := Game{
		ID: 12345,
		Names: []Text{
			{Region: "us", Language: "en", Text: "Super Mario Bros."},
		},
		Descriptions: []Text{
			{Region: "us", Language: "en", Text: "Classic platformer game"},
		},
		Medias: []Media{
			{Type: "box-2D", URL: "http://example.com/cover.jpg"},
		},
	}

	// Test conversion to ScraperResult
	result := ss.convertGameToResult(&game, "nes")
	assert.Equal(t, "12345", result.ID)
	assert.Equal(t, "Super Mario Bros.", result.Name)
	assert.Equal(t, "Classic platformer game", result.Description)
	assert.Equal(t, "nes", result.SystemID)
	assert.Greater(t, result.Relevance, 0.0)
	assert.LessOrEqual(t, result.Relevance, 1.0)

	// Test conversion to GameInfo
	gameInfo := ss.convertGameToInfo(&game)
	assert.Equal(t, "12345", gameInfo.ID)
	assert.Equal(t, "Super Mario Bros.", gameInfo.Name)
	assert.Equal(t, "Classic platformer game", gameInfo.Description)
	assert.Len(t, gameInfo.Media, 1)
	assert.Equal(t, scraper.MediaTypeCover, gameInfo.Media[0].Type)
	assert.Equal(t, "http://example.com/cover.jpg", gameInfo.Media[0].URL)
}

func TestScreenScraperURLBuilding(t *testing.T) {
	t.Parallel()

	ss := NewScreenScraper()

	// Test search URL building with name
	url, err := ss.buildSearchURL("Super Mario Bros", "1", nil, "us", "en")
	require.NoError(t, err)
	assert.Contains(t, url, "jeuInfos.php")
	assert.Contains(t, url, "recherche=Super+Mario+Bros")
	assert.Contains(t, url, "ssid=1")
	assert.Contains(t, url, "region=us")
	assert.Contains(t, url, "langue=en")

	// Test search URL building with hash
	hash := &scraper.FileHash{
		CRC32:    "12345678",
		MD5:      "abcdef1234567890abcdef1234567890",
		SHA1:     "abcdef1234567890abcdef1234567890abcdef12",
		FileSize: 1048576,
	}
	url, err = ss.buildSearchURL("", "1", hash, "", "")
	require.NoError(t, err)
	assert.Contains(t, url, "crc=12345678")
	assert.Contains(t, url, "md5=ABCDEF1234567890ABCDEF1234567890")
	assert.Contains(t, url, "sha1=ABCDEF1234567890ABCDEF1234567890ABCDEF12")
	assert.Contains(t, url, "romsize=1048576")

	// Test game info URL building
	url, err = ss.buildGameInfoURL("12345")
	require.NoError(t, err)
	assert.Contains(t, url, "jeuInfos.php")
	assert.Contains(t, url, "id=12345")
}

func TestPlatformMapper(t *testing.T) {
	t.Parallel()

	mapper := NewPlatformMapper()
	require.NotNil(t, mapper)

	// Test platform mappings
	testCases := []struct {
		systemID   string
		platformID string
		supported  bool
	}{
		{"NES", "1", true},
		{"SNES", "2", true},
		{"Genesis", "1", true},
		{"PSX", "57", true},
		{"Arcade", "75", true},
		{"unknown", "", false},
	}

	for _, tc := range testCases {
		platformID, supported := mapper.MapToScraperPlatform(tc.systemID)
		assert.Equal(t, tc.supported, supported,
			"Platform %s support should be %v", tc.systemID, tc.supported)
		if supported {
			assert.Equal(t, tc.platformID, platformID,
				"Platform %s should map to %s", tc.systemID, tc.platformID)
		}
	}

	// Test reverse mapping
	systemID, found := mapper.MapFromScraperPlatform("1")
	assert.True(t, found)
	// Note: Multiple systems can map to platform "1", so we just check it's valid
	assert.NotEmpty(t, systemID)

	// Test supported systems list
	systems := mapper.GetSupportedSystems()
	assert.NotEmpty(t, systems)
	assert.Contains(t, systems, "NES")
	assert.Contains(t, systems, "SNES")
}
