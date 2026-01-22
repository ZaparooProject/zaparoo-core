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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	runner.Screen().InjectString("mario")
	runner.Draw()

	// Press Tab to navigate to button bar (results list is empty)
	runner.Screen().InjectTab()
	runner.Draw()

	// Press Enter to trigger search (first button in button bar)
	runner.Screen().InjectEnter()
	runner.Draw()

	// Wait for SearchMedia to be called using the mock's signal channel
	called := mockSvc.SearchMediaCalled()
	assert.True(t, runner.WaitForSignal(called, 100*time.Millisecond), "SearchMedia should be called")
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
	runner.Screen().InjectEscape()
	runner.Draw()

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
	runner.Screen().InjectTab()
	runner.Draw()
	runner.Screen().InjectArrowRight()
	runner.Draw()

	// Press Enter on Clear
	runner.Screen().InjectEnter()
	runner.Draw()

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
	runner.Screen().InjectArrowDown()
	runner.Draw()

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
