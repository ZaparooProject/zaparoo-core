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
	"errors"
	"fmt"
	"testing"

	scraperpkg "github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockScraper for testing
type MockScraper struct {
	name               string
	supportedPlatforms []string
	shouldError        bool
}

func (m *MockScraper) IsSupportedPlatform(systemID string) bool {
	for _, platform := range m.supportedPlatforms {
		if platform == systemID {
			return true
		}
	}
	return false
}

func (m *MockScraper) Search(ctx context.Context, query scraperpkg.ScraperQuery) ([]scraperpkg.ScraperResult, error) {
	if m.shouldError {
		return nil, errors.New("mock search error")
	}

	return []scraperpkg.ScraperResult{
		{
			ID:   "mock-result-1",
			Name: query.Name,
		},
	}, nil
}

func (m *MockScraper) DownloadMedia(ctx context.Context, media *scraperpkg.MediaItem) error {
	if m.shouldError {
		return errors.New("mock download error")
	}
	return nil
}

func (m *MockScraper) GetSupportedMediaTypes() []scraperpkg.MediaType {
	return []scraperpkg.MediaType{scraperpkg.MediaTypeCover, scraperpkg.MediaTypeScreenshot}
}

func (m *MockScraper) GetInfo() scraperpkg.ScraperInfo {
	return scraperpkg.ScraperInfo{
		Name:    m.name,
		Version: "1.0.0",
	}
}

func (m *MockScraper) GetGameInfo(ctx context.Context, gameID string) (*scraperpkg.GameInfo, error) {
	if m.shouldError {
		return nil, errors.New("mock game info error")
	}

	return &scraperpkg.GameInfo{
		Name:        "Mock Game",
		Description: "Mock Description",
	}, nil
}

func TestNewScraperRegistry(t *testing.T) {
	t.Parallel()

	registry := NewScraperRegistry()
	require.NotNil(t, registry)

	// Should have default scrapers registered
	assert.True(t, registry.Count() > 0)
	assert.True(t, registry.HasScraper("screenscraper"))
	assert.True(t, registry.HasScraper("thegamesdb"))
	assert.True(t, registry.HasScraper("igdb"))

	names := registry.GetNames()
	assert.Contains(t, names, "screenscraper")
	assert.Contains(t, names, "thegamesdb")
	assert.Contains(t, names, "igdb")
}

func TestScraperRegistry_Register(t *testing.T) {
	t.Parallel()

	registry := NewScraperRegistry()
	initialCount := registry.Count()

	// Register a mock scraper
	mockScraper := &MockScraper{
		name:               "mock",
		supportedPlatforms: []string{"nes", "snes"},
	}

	registry.Register("mock", mockScraper)

	// Verify registration
	assert.Equal(t, initialCount+1, registry.Count())
	assert.True(t, registry.HasScraper("mock"))

	// Verify we can retrieve it
	scraper, err := registry.Get("mock")
	assert.NoError(t, err)
	assert.Equal(t, mockScraper, scraper)

	// Verify it's in the names list
	names := registry.GetNames()
	assert.Contains(t, names, "mock")
}

func TestScraperRegistry_Get(t *testing.T) {
	t.Parallel()

	registry := NewScraperRegistry()

	// Get existing scraper
	scraper, err := registry.Get("screenscraper")
	assert.NoError(t, err)
	assert.NotNil(t, scraper)

	// Get non-existent scraper
	scraper, err = registry.Get("nonexistent")
	assert.Error(t, err)
	assert.Nil(t, scraper)
	assert.Contains(t, err.Error(), "scraper 'nonexistent' not found")
}

func TestScraperRegistry_HasScraper(t *testing.T) {
	t.Parallel()

	registry := NewScraperRegistry()

	// Test existing scrapers
	assert.True(t, registry.HasScraper("screenscraper"))
	assert.True(t, registry.HasScraper("thegamesdb"))
	assert.True(t, registry.HasScraper("igdb"))

	// Test non-existent scraper
	assert.False(t, registry.HasScraper("nonexistent"))

	// Test empty string
	assert.False(t, registry.HasScraper(""))
}

func TestScraperRegistry_GetNames(t *testing.T) {
	t.Parallel()

	registry := NewScraperRegistry()

	names := registry.GetNames()
	assert.NotEmpty(t, names)

	// Should contain default scrapers
	assert.Contains(t, names, "screenscraper")
	assert.Contains(t, names, "thegamesdb")
	assert.Contains(t, names, "igdb")

	// Add a new scraper and verify it appears in names
	mockScraper := &MockScraper{name: "test-scraper"}
	registry.Register("test-scraper", mockScraper)

	updatedNames := registry.GetNames()
	assert.Contains(t, updatedNames, "test-scraper")
	assert.Equal(t, len(names)+1, len(updatedNames))
}

func TestScraperRegistry_Count(t *testing.T) {
	t.Parallel()

	registry := NewScraperRegistry()

	initialCount := registry.Count()
	assert.True(t, initialCount >= 3) // At least the 3 default scrapers

	// Add scrapers and verify count increases
	mockScraper1 := &MockScraper{name: "mock1"}
	registry.Register("mock1", mockScraper1)
	assert.Equal(t, initialCount+1, registry.Count())

	mockScraper2 := &MockScraper{name: "mock2"}
	registry.Register("mock2", mockScraper2)
	assert.Equal(t, initialCount+2, registry.Count())
}

func TestScraperRegistry_OverwriteRegistration(t *testing.T) {
	t.Parallel()

	registry := NewScraperRegistry()

	// Register first mock scraper
	mockScraper1 := &MockScraper{
		name:               "test",
		supportedPlatforms: []string{"nes"},
	}
	registry.Register("test", mockScraper1)

	initialCount := registry.Count()

	// Register second mock scraper with same name
	mockScraper2 := &MockScraper{
		name:               "test",
		supportedPlatforms: []string{"snes"},
	}
	registry.Register("test", mockScraper2)

	// Count should remain the same
	assert.Equal(t, initialCount, registry.Count())

	// Should get the second scraper
	scraper, err := registry.Get("test")
	assert.NoError(t, err)
	assert.Equal(t, mockScraper2, scraper)

	// Verify platform support changed
	assert.True(t, scraper.IsSupportedPlatform("snes"))
	assert.False(t, scraper.IsSupportedPlatform("nes"))
}

func TestScraperRegistry_Casesensitivity(t *testing.T) {
	t.Parallel()

	registry := NewScraperRegistry()

	// Register with lowercase
	mockScraper := &MockScraper{name: "testScraper"}
	registry.Register("testScraper", mockScraper)

	// Test case sensitivity
	assert.True(t, registry.HasScraper("testScraper"))
	assert.False(t, registry.HasScraper("TESTSCRAPER"))
	assert.False(t, registry.HasScraper("testscraper"))

	// Get with different case should fail
	_, err := registry.Get("TESTSCRAPER")
	assert.Error(t, err)

	// Get with exact case should succeed
	scraper, err := registry.Get("testScraper")
	assert.NoError(t, err)
	assert.Equal(t, mockScraper, scraper)
}

func TestScraperRegistry_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	registry := NewScraperRegistry()

	// Test concurrent registration and access
	const numGoroutines = 10

	done := make(chan bool, numGoroutines)

	// Start multiple goroutines doing operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()

			scraperName := fmt.Sprintf("mock%d", id)
			mockScraper := &MockScraper{name: scraperName}

			// Register
			registry.Register(scraperName, mockScraper)

			// Verify registration
			assert.True(t, registry.HasScraper(scraperName))

			// Get scraper
			scraper, err := registry.Get(scraperName)
			assert.NoError(t, err)
			assert.Equal(t, mockScraper, scraper)

			// Check names and count
			names := registry.GetNames()
			assert.Contains(t, names, scraperName)
			assert.True(t, registry.Count() > 0)
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify all scrapers were registered
	for i := 0; i < numGoroutines; i++ {
		scraperName := fmt.Sprintf("mock%d", i)
		assert.True(t, registry.HasScraper(scraperName))
	}
}

func TestScraperRegistry_EmptyName(t *testing.T) {
	t.Parallel()

	registry := NewScraperRegistry()
	mockScraper := &MockScraper{name: "test"}

	// Register with empty name
	registry.Register("", mockScraper)

	// Should be able to retrieve with empty name
	scraper, err := registry.Get("")
	assert.NoError(t, err)
	assert.Equal(t, mockScraper, scraper)

	assert.True(t, registry.HasScraper(""))

	names := registry.GetNames()
	assert.Contains(t, names, "")
}

func TestScraperRegistry_NilScraper(t *testing.T) {
	t.Parallel()

	registry := NewScraperRegistry()

	// Register nil scraper
	registry.Register("nil-scraper", nil)

	// Should be able to retrieve nil
	scraper, err := registry.Get("nil-scraper")
	assert.NoError(t, err)
	assert.Nil(t, scraper)

	assert.True(t, registry.HasScraper("nil-scraper"))
}

func TestScraperRegistry_GetNamesOrdering(t *testing.T) {
	t.Parallel()

	registry := NewScraperRegistry()

	// Add scrapers in a specific order
	scraperNames := []string{"zebra", "alpha", "beta", "gamma"}
	for _, name := range scraperNames {
		mockScraper := &MockScraper{name: name}
		registry.Register(name, mockScraper)
	}

	names := registry.GetNames()

	// Verify all registered names are present
	for _, name := range scraperNames {
		assert.Contains(t, names, name)
	}

	// The order is not guaranteed, but we should have at least these names
	// plus the default ones
	assert.True(t, len(names) >= len(scraperNames)+3) // +3 for default scrapers
}

