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

package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestFormatDisambiguatingTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected string
		input    []database.TagInfo
	}{
		{
			name:     "empty slice",
			input:    []database.TagInfo{},
			expected: "",
		},
		{
			name:     "nil slice",
			input:    nil,
			expected: "",
		},
		{
			name: "single tag",
			input: []database.TagInfo{
				{Type: "region", Tag: "eu"},
			},
			expected: "region:eu",
		},
		{
			name: "multiple same-type tags",
			input: []database.TagInfo{
				{Type: "region", Tag: "eu"},
				{Type: "region", Tag: "us"},
			},
			expected: "region:eu, region:us",
		},
		{
			name: "mixed types preserve given order",
			input: []database.TagInfo{
				{Type: "region", Tag: "eu"},
				{Type: "builddate", Tag: "1996-10-04"},
			},
			expected: "region:eu, builddate:1996-10-04",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatDisambiguatingTags(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTruncateSystemName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "short name unchanged",
			input:    "NES",
			expected: "NES",
		},
		{
			name:     "exact max length",
			input:    "123456789012345678", // exactly 18 chars
			expected: "123456789012345678",
		},
		{
			name:     "one over max length",
			input:    "1234567890123456789", // 19 chars
			expected: "123456789012345...",
		},
		{
			name:     "long name truncated",
			input:    "Nintendo Entertainment System",
			expected: "Nintendo Entert...",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "medium length unchanged",
			input:    "Super Nintendo",
			expected: "Super Nintendo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := truncateSystemName(tt.input)
			assert.Equal(t, tt.expected, result)
			// Verify truncated results are at most 18 chars
			assert.LessOrEqual(t, len(result), 18)
		})
	}
}

func TestBuildSearchMedia_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	// Add main page for navigation
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSystems([]models.System{
		{ID: "nes", Name: "NES"},
		{ID: "snes", Name: "SNES"},
	})
	mockSvc.SetupSearchMedia(&models.SearchResults{
		Results: []models.SearchResultMedia{},
		Total:   0,
	})

	runner.Start(pages)
	runner.Draw()

	session := NewSession()

	runner.QueueUpdateDraw(func() {
		BuildSearchMedia(mockSvc, pages, runner.App(), session)
	})

	require.True(t, runner.WaitForText("Search Media", 100*time.Millisecond), "Search Media title should appear")

	// Verify UI elements are visible
	assert.True(t, runner.ContainsText("Name"), "Name label should be visible")
	assert.True(t, runner.ContainsText("System"), "System label should be visible")
	assert.True(t, runner.ContainsText("Search"), "Search button should be visible")
	assert.True(t, runner.ContainsText("Clear"), "Clear button should be visible")
	assert.True(t, runner.ContainsText("Back"), "Back button should be visible")
}

func TestBuildSearchMedia_SearchWithResults_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSystems([]models.System{
		{ID: "nes", Name: "NES"},
	})

	// Setup search to return results
	searchResults := &models.SearchResults{
		Results: []models.SearchResultMedia{
			{
				Name:      "Super Mario Bros",
				Path:      "/roms/nes/smb.nes",
				ZapScript: "**launch.nes:/roms/nes/smb.nes",
				System:    models.System{ID: "nes", Name: "NES"},
			},
			{
				Name:      "Zelda",
				Path:      "/roms/nes/zelda.nes",
				ZapScript: "**launch.nes:/roms/nes/zelda.nes",
				System:    models.System{ID: "nes", Name: "NES"},
			},
		},
		Total: 2,
	}
	mockSvc.SetupSearchMedia(searchResults)

	runner.Start(pages)
	runner.Draw()

	session := NewSession()

	runner.QueueUpdateDraw(func() {
		BuildSearchMedia(mockSvc, pages, runner.App(), session)
	})

	require.True(t, runner.WaitForText("Search Media", 100*time.Millisecond))

	// Type in search
	runner.SimulateString("mario")

	// Press Tab to navigate to button bar (results list is empty)
	runner.SimulateTab()

	// Press Enter to trigger search (first button in button bar)
	runner.SimulateEnter()

	// Wait for SearchMedia to be called using the mock's signal channel
	called := mockSvc.SearchMediaCalled()
	assert.True(t, runner.WaitForSignal(called, 100*time.Millisecond), "SearchMedia should be called")
}

func TestBuildSearchMedia_AutoloadsMoreResults_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSystems([]models.System{{ID: "psx", Name: "PlayStation"}})

	nextCursor := "next-page"
	mockSvc.On("SearchMedia", mock.Anything, mock.MatchedBy(func(params models.SearchParams) bool {
		return params.Cursor == nil
	})).Return(&models.SearchResults{
		Results: []models.SearchResultMedia{
			{
				Name:      "Game One",
				Path:      "game-one.chd",
				ZapScript: "@PlayStation/Game One",
				System:    models.System{ID: "psx", Name: "PlayStation"},
			},
			{
				Name:      "Game Two",
				Path:      "game-two.chd",
				ZapScript: "@PlayStation/Game Two",
				System:    models.System{ID: "psx", Name: "PlayStation"},
			},
		},
		Total: 2,
		Pagination: &models.PaginationInfo{
			NextCursor:  &nextCursor,
			HasNextPage: true,
			PageSize:    2,
		},
	}, nil).Once()
	releaseNextPage := make(chan time.Time)
	mockSvc.On("SearchMedia", mock.Anything, mock.MatchedBy(func(params models.SearchParams) bool {
		return params.Cursor != nil && *params.Cursor == nextCursor &&
			params.Query != nil && *params.Query == ""
	})).Return(&models.SearchResults{
		Results: []models.SearchResultMedia{
			{
				Name:      "Game Three",
				Path:      "game-three.chd",
				ZapScript: "@PlayStation/Game Three",
				System:    models.System{ID: "psx", Name: "PlayStation"},
			},
		},
		Total: 1,
		Pagination: &models.PaginationInfo{
			HasNextPage: false,
			PageSize:    2,
		},
	}, nil).WaitUntil(releaseNextPage).Once()

	runner.Start(pages)
	runner.Draw()

	session := NewSession()
	runner.QueueUpdateDraw(func() {
		BuildSearchMedia(mockSvc, pages, runner.App(), session)
	})
	require.True(t, runner.WaitForText("Search Media", 100*time.Millisecond))

	runner.SimulateTab()
	runner.SimulateEnter()
	require.True(t, runner.WaitForSignal(mockSvc.SearchMediaCalled(), 100*time.Millisecond))
	require.True(t, runner.WaitForText("Loaded 2 results", 100*time.Millisecond))
	assert.Equal(t, 1, mockSvc.SearchMediaCallCount(), "initial selection should not immediately prefetch")

	// Editing the input must not combine the previous page's cursor with a new query.
	session.SetSearchMediaName("different query")
	scrollDone := make(chan struct{})
	go func() {
		runner.SimulateArrowDown()
		close(scrollDone)
	}()
	select {
	case <-scrollDone:
	case <-time.After(100 * time.Millisecond):
		close(releaseNextPage)
		<-scrollDone
		t.Fatal("scrolling blocked while the next page loaded")
	}
	require.True(t, runner.WaitForSignal(mockSvc.SearchMediaCalled(), 100*time.Millisecond))
	require.True(t, runner.WaitForText("Loading more results", 100*time.Millisecond))
	close(releaseNextPage)

	assert.True(t, runner.WaitForText("Game Three", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("Game One"), "first page should remain visible")
	assert.True(t, runner.ContainsText("game-two.chd"), "prefetch should preserve current selection")
	assert.False(t, runner.ContainsText("Load more results"), "pagination should not add a list row")
	assert.True(t, runner.ContainsText("Loaded 3 results"))
	mockSvc.AssertExpectations(t)
}

func TestBuildSearchMedia_AutoloadErrorCanRetry_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSystems([]models.System{{ID: "psx", Name: "PlayStation"}})

	nextCursor := "next-page"
	mockSvc.On("SearchMedia", mock.Anything, mock.MatchedBy(func(params models.SearchParams) bool {
		return params.Cursor == nil
	})).Return(&models.SearchResults{
		Results: []models.SearchResultMedia{
			{
				Name:      "Game One",
				Path:      "game-one.chd",
				ZapScript: "@PlayStation/Game One",
				System:    models.System{ID: "psx", Name: "PlayStation"},
			},
			{
				Name:      "Game Two",
				Path:      "game-two.chd",
				ZapScript: "@PlayStation/Game Two",
				System:    models.System{ID: "psx", Name: "PlayStation"},
			},
		},
		Total: 2,
		Pagination: &models.PaginationInfo{
			NextCursor:  &nextCursor,
			HasNextPage: true,
			PageSize:    2,
		},
	}, nil).Once()
	moreParams := mock.MatchedBy(func(params models.SearchParams) bool {
		return params.Cursor != nil && *params.Cursor == nextCursor
	})
	mockSvc.On("SearchMedia", mock.Anything, moreParams).
		Return(nil, errors.New("temporary search failure")).Once()
	mockSvc.On("SearchMedia", mock.Anything, moreParams).Return(&models.SearchResults{
		Results: []models.SearchResultMedia{
			{
				Name:      "Game Three",
				Path:      "game-three.chd",
				ZapScript: "@PlayStation/Game Three",
				System:    models.System{ID: "psx", Name: "PlayStation"},
			},
		},
		Total: 1,
		Pagination: &models.PaginationInfo{
			HasNextPage: false,
			PageSize:    1,
		},
	}, nil).Once()

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		BuildSearchMedia(mockSvc, pages, runner.App(), NewSession())
	})
	require.True(t, runner.WaitForText("Search Media", 100*time.Millisecond))

	runner.SimulateTab()
	runner.SimulateEnter()
	require.True(t, runner.WaitForSignal(mockSvc.SearchMediaCalled(), 100*time.Millisecond))
	require.True(t, runner.WaitForText("Loaded 2 results", 100*time.Millisecond))
	runner.SimulateArrowDown()
	require.True(t, runner.WaitForSignal(mockSvc.SearchMediaCalled(), 100*time.Millisecond))

	assert.True(t, runner.WaitForText("Error loading more results", 100*time.Millisecond))
	assert.False(t, runner.ContainsText("Load more results"), "failed load should not add a list row")

	runner.SimulateArrowUp()
	require.True(t, runner.WaitForSignal(mockSvc.SearchMediaCalled(), 100*time.Millisecond))
	assert.True(t, runner.WaitForText("Game Three", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("Game One"), "retry should preserve first page")
	mockSvc.AssertExpectations(t)
}

func TestBuildSearchMedia_FreshSearchErrorClearsPagination_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSystems([]models.System{{ID: "psx", Name: "PlayStation"}})

	nextCursor := "stale-cursor"
	mockSvc.On("SearchMedia", mock.Anything, mock.MatchedBy(func(params models.SearchParams) bool {
		return params.Cursor == nil && params.Query != nil && *params.Query == ""
	})).Return(&models.SearchResults{
		Results: []models.SearchResultMedia{
			{
				Name:      "Game One",
				Path:      "game-one.chd",
				ZapScript: "@PlayStation/Game One",
				System:    models.System{ID: "psx", Name: "PlayStation"},
			},
			{
				Name:      "Game Two",
				Path:      "game-two.chd",
				ZapScript: "@PlayStation/Game Two",
				System:    models.System{ID: "psx", Name: "PlayStation"},
			},
		},
		Total: 2,
		Pagination: &models.PaginationInfo{
			NextCursor:  &nextCursor,
			HasNextPage: true,
			PageSize:    2,
		},
	}, nil).Once()
	mockSvc.On("SearchMedia", mock.Anything, mock.MatchedBy(func(params models.SearchParams) bool {
		return params.Cursor == nil && params.Query != nil && *params.Query == "new query"
	})).Return(nil, errors.New("fresh search failed")).Once()
	mockSvc.On("SearchMedia", mock.Anything, mock.MatchedBy(func(params models.SearchParams) bool {
		return params.Cursor != nil && *params.Cursor == nextCursor
	})).Return(&models.SearchResults{}, nil).Maybe()

	runner.Start(pages)
	runner.Draw()

	session := NewSession()
	runner.QueueUpdateDraw(func() {
		BuildSearchMedia(mockSvc, pages, runner.App(), session)
	})
	require.True(t, runner.WaitForText("Search Media", 100*time.Millisecond))

	runner.SimulateTab()
	runner.SimulateEnter()
	require.True(t, runner.WaitForSignal(mockSvc.SearchMediaCalled(), 100*time.Millisecond))
	require.True(t, runner.WaitForText("Loaded 2 results", 100*time.Millisecond))

	runner.SimulateArrowLeft()
	session.SetSearchMediaName("new query")
	runner.SimulateEnter()
	require.True(t, runner.WaitForSignal(mockSvc.SearchMediaCalled(), 100*time.Millisecond))
	require.True(t, runner.WaitForText("An error occurred during search", 100*time.Millisecond))

	runner.SimulateTab()
	runner.SimulateArrowDown()
	assert.Never(t, func() bool {
		return mockSvc.SearchMediaCallCount() > 2
	}, 50*time.Millisecond, 5*time.Millisecond, "failed fresh search should discard the old cursor")
	mockSvc.AssertExpectations(t)
}

func TestBuildSearchMedia_DisambiguatingTags_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSystems([]models.System{
		{ID: "genesis", Name: "Genesis"},
	})

	searchResults := &models.SearchResults{
		Results: []models.SearchResultMedia{
			{
				Name:      "Sonic The Hedgehog",
				Path:      "/roms/genesis/sonic_eu.md",
				ZapScript: "**launch.genesis:/roms/genesis/sonic_eu.md",
				System:    models.System{ID: "genesis", Name: "Genesis"},
				DisambiguatingTags: []database.TagInfo{
					{Type: "region", Tag: "eu"},
					{Type: "region", Tag: "us"},
				},
			},
			{
				Name:      "Sonic The Hedgehog",
				Path:      "/roms/genesis/sonic_jp.md",
				ZapScript: "**launch.genesis:/roms/genesis/sonic_jp.md",
				System:    models.System{ID: "genesis", Name: "Genesis"},
				DisambiguatingTags: []database.TagInfo{
					{Type: "region", Tag: "jp"},
				},
			},
			{
				Name:      "Streets of Rage",
				Path:      "/roms/genesis/streets.md",
				ZapScript: "**launch.genesis:/roms/genesis/streets.md",
				System:    models.System{ID: "genesis", Name: "Genesis"},
			},
		},
		Total: 3,
	}
	mockSvc.SetupSearchMedia(searchResults)

	runner.Start(pages)
	runner.Draw()

	session := NewSession()

	runner.QueueUpdateDraw(func() {
		BuildSearchMedia(mockSvc, pages, runner.App(), session)
	})

	require.True(t, runner.WaitForText("Search Media", 100*time.Millisecond))

	// Trigger search
	runner.SimulateTab()
	runner.SimulateEnter()

	called := mockSvc.SearchMediaCalled()
	require.True(t, runner.WaitForSignal(called, 100*time.Millisecond), "SearchMedia should be called")

	// Results with disambiguating tags should show them in the row
	assert.True(t, runner.WaitForText("region:eu", 100*time.Millisecond), "region:eu tag should appear in results")
	assert.True(t, runner.ContainsText("region:jp"), "region:jp tag should appear in results")

	// Result without tags should still render cleanly (no spurious parens)
	assert.True(t, runner.ContainsText("Streets of Rage"), "unduplicated title should appear")
}

func TestBuildSearchMedia_EscapeGoesBack_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, true)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSystems([]models.System{})

	runner.Start(pages)
	runner.Draw()

	session := NewSession()

	runner.QueueUpdateDraw(func() {
		BuildSearchMedia(mockSvc, pages, runner.App(), session)
	})

	require.True(t, runner.WaitForText("Search Media", 100*time.Millisecond))

	// Helper to get front page
	getFrontPage := func() string {
		var name string
		runner.QueueUpdateDraw(func() {
			name, _ = pages.GetFrontPage()
		})
		return name
	}

	// Press escape
	runner.SimulateEscape()

	// Verify we went back
	assert.True(t, runner.WaitForCondition(func() bool {
		return getFrontPage() == PageMain
	}, 100*time.Millisecond), "Should navigate back to main page")
}

func TestBuildSearchMedia_ClearButton_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSystems([]models.System{})

	runner.Start(pages)
	runner.Draw()

	session := NewSession()
	session.SetSearchMediaName("test query")
	session.SetSearchMediaSystem("nes")
	session.SetSearchMediaSystemName("NES")

	runner.QueueUpdateDraw(func() {
		BuildSearchMedia(mockSvc, pages, runner.App(), session)
	})

	require.True(t, runner.WaitForText("Search Media", 100*time.Millisecond))

	// Navigate to Clear button (Tab then right)
	runner.SimulateTab()
	runner.SimulateArrowRight()

	// Press Enter on Clear
	runner.SimulateEnter()

	// Wait for session state to be cleared
	assert.True(t, runner.WaitForCondition(func() bool {
		return session.GetSearchMediaName() == "" &&
			session.GetSearchMediaSystem() == "" &&
			session.GetSearchMediaSystemName() == "All"
	}, 100*time.Millisecond), "Session state should be cleared")
}

func TestBuildSearchMedia_SystemNavigation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSystems([]models.System{
		{ID: "nes", Name: "NES"},
		{ID: "snes", Name: "SNES"},
		{ID: "genesis", Name: "Genesis"},
	})
	// Also need to set up search in case it's called
	mockSvc.SetupSearchMedia(&models.SearchResults{
		Results: []models.SearchResultMedia{},
		Total:   0,
	})

	runner.Start(pages)
	runner.Draw()

	session := NewSession()

	runner.QueueUpdateDraw(func() {
		BuildSearchMedia(mockSvc, pages, runner.App(), session)
	})

	require.True(t, runner.WaitForText("Search Media", 100*time.Millisecond))

	// Navigate down to system button
	runner.SimulateArrowDown()

	// System button should show "All" initially - also check for "System" label
	// which should be visible regardless
	assert.True(t, runner.ContainsText("System"), "System label should be visible")
}

func TestSession_SearchMedia(t *testing.T) {
	t.Parallel()

	session := NewSession()

	// Test default values
	assert.Empty(t, session.GetSearchMediaName())
	assert.Empty(t, session.GetSearchMediaSystem())
	assert.Equal(t, "All", session.GetSearchMediaSystemName())

	// Test setters and getters
	session.SetSearchMediaName("test")
	session.SetSearchMediaSystem("nes")
	session.SetSearchMediaSystemName("NES")

	assert.Equal(t, "test", session.GetSearchMediaName())
	assert.Equal(t, "nes", session.GetSearchMediaSystem())
	assert.Equal(t, "NES", session.GetSearchMediaSystemName())

	// Test ClearSearchMedia
	session.ClearSearchMedia()
	assert.Empty(t, session.GetSearchMediaName())
	assert.Empty(t, session.GetSearchMediaSystem())
	assert.Equal(t, "All", session.GetSearchMediaSystemName())
}
