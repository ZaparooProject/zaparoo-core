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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestNewProgressBar(t *testing.T) {
	t.Parallel()

	pb := NewProgressBar()

	assert.NotNil(t, pb)
	assert.NotNil(t, pb.Box)
	assert.InDelta(t, 0.0, pb.progress, 0.001)
}

func TestProgressBar_SetProgress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{
			name:     "zero progress",
			input:    0.0,
			expected: 0.0,
		},
		{
			name:     "half progress",
			input:    0.5,
			expected: 0.5,
		},
		{
			name:     "full progress",
			input:    1.0,
			expected: 1.0,
		},
		{
			name:     "negative clamped to zero",
			input:    -0.5,
			expected: 0.0,
		},
		{
			name:     "over one clamped to one",
			input:    1.5,
			expected: 1.0,
		},
		{
			name:     "quarter progress",
			input:    0.25,
			expected: 0.25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pb := NewProgressBar()
			result := pb.SetProgress(tt.input)

			assert.InDelta(t, tt.expected, pb.GetProgress(), 0.001)
			assert.Equal(t, pb, result, "SetProgress should return self for chaining")
		})
	}
}

func TestProgressBar_GetProgress(t *testing.T) {
	t.Parallel()

	pb := NewProgressBar()

	// Initial progress
	assert.InDelta(t, 0.0, pb.GetProgress(), 0.001)

	// After setting
	pb.SetProgress(0.75)
	assert.InDelta(t, 0.75, pb.GetProgress(), 0.001)
}

func TestProgressBar_Chaining(t *testing.T) {
	t.Parallel()

	pb := NewProgressBar()

	// Verify chaining works
	result := pb.SetProgress(0.5)
	assert.Equal(t, pb, result)

	// Multiple chains
	pb.SetProgress(0.1).SetProgress(0.2).SetProgress(0.3)
	assert.InDelta(t, 0.3, pb.GetProgress(), 0.001)
}

func TestFormatDBMenuLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		db       models.IndexingStatusResponse
		expected string
	}{
		{
			name: "no database exists",
			db: models.IndexingStatusResponse{
				Exists: false,
			},
			expected: "Update index: not indexed",
		},
		{
			name: "database is indexing",
			db: models.IndexingStatusResponse{
				Exists:   true,
				Indexing: true,
			},
			expected: "Update index: in progress",
		},
		{
			name: "database is paused",
			db: models.IndexingStatusResponse{
				Exists:   true,
				Indexing: true,
				Paused:   true,
			},
			expected: "Update index: paused",
		},
		{
			name: "database exists with media",
			db: models.IndexingStatusResponse{
				Exists:     true,
				TotalMedia: intPtr(100),
			},
			expected: "Update index: 100 media",
		},
		{
			name: "database exists with zero media",
			db: models.IndexingStatusResponse{
				Exists:     true,
				TotalMedia: intPtr(0),
			},
			expected: "Update index: 0 media",
		},
		{
			name: "database exists with nil TotalMedia",
			db: models.IndexingStatusResponse{
				Exists:     true,
				TotalMedia: nil,
			},
			expected: "Update index: 0 media",
		},
		{
			name: "database exists with large count",
			db: models.IndexingStatusResponse{
				Exists:     true,
				TotalMedia: intPtr(12345),
			},
			expected: "Update index: 12345 media",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatDBMenuLabel(tt.db)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatScrapeMenuLabel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Scrape metadata", formatScrapeMenuLabel(&models.ScrapingStatusResponse{}))
	assert.Equal(t, "Scrape metadata: 42 scraped", formatScrapeMenuLabel(&models.ScrapingStatusResponse{
		TotalScraped: 42,
	}))
	assert.Equal(t, "Scrape metadata: in progress", formatScrapeMenuLabel(&models.ScrapingStatusResponse{
		ScraperID:    "screenscraper",
		Scraping:     true,
		Processed:    12,
		TotalScraped: 42,
	}))
	assert.Equal(t, "Scrape metadata: paused", formatScrapeMenuLabel(&models.ScrapingStatusResponse{
		ScraperID: "screenscraper",
		Scraping:  true,
		Paused:    true,
	}))
	assert.Equal(t, "Scrape metadata: 42 scraped", formatScrapeMenuLabel(&models.ScrapingStatusResponse{
		ScraperID:    "screenscraper",
		Done:         true,
		Matched:      8,
		TotalScraped: 42,
	}))
	assert.Equal(t, "Scrape metadata: 8 matched", formatScrapeMenuLabel(&models.ScrapingStatusResponse{
		ScraperID: "screenscraper",
		Done:      true,
		Matched:   8,
	}))
}

func TestFormatScrapeProgress(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		"ScreenScraper - NES\nRecords: 3 / 10\nMatched: 2  Skipped: 1",
		formatScrapeProgress(&models.ScrapingStatusResponse{
			ScraperID: "screenscraper",
			SystemID:  "NES",
			Processed: 3,
			Total:     10,
			Matched:   2,
			Skipped:   1,
		}, "ScreenScraper"),
	)
	assert.Equal(t,
		"screenscraper\nRecords: 3 processed\nMatched: 2  Skipped: 1",
		formatScrapeProgress(&models.ScrapingStatusResponse{
			ScraperID: "screenscraper",
			Processed: 3,
			Matched:   2,
			Skipped:   1,
		}, ""),
	)
	assert.Equal(t,
		"screenscraper\nPaused: 3 / 10\nMatched: 2  Skipped: 1",
		formatScrapeProgress(&models.ScrapingStatusResponse{
			ScraperID: "screenscraper",
			Processed: 3,
			Total:     10,
			Matched:   2,
			Skipped:   1,
			Paused:    true,
		}, ""),
	)

	currentStep := 2
	totalSteps := 5
	display := "Super Nintendo"
	assert.Equal(t,
		"ScreenScraper - SNES\nOverall: Super Nintendo (2 / 5)\nRecords: 4 / 12\nMatched: 3  Skipped: 1",
		formatScrapeProgress(&models.ScrapingStatusResponse{
			ScraperID:          "screenscraper",
			CurrentStep:        &currentStep,
			TotalSteps:         &totalSteps,
			CurrentStepDisplay: &display,
			CurrentSystem: &models.ScrapeSystemProgressResponse{
				SystemID:   "snes",
				SystemName: "SNES",
				Processed:  4,
				Total:      12,
				Matched:    3,
				Skipped:    1,
			},
		}, "ScreenScraper"),
	)
	assert.Equal(t,
		"screenscraper - arcade\nOverall: 2 / 5\nPaused\nMatched: 0  Skipped: 0",
		formatScrapeProgress(&models.ScrapingStatusResponse{
			ScraperID:   "screenscraper",
			CurrentStep: &currentStep,
			TotalSteps:  &totalSteps,
			Paused:      true,
			CurrentSystem: &models.ScrapeSystemProgressResponse{
				SystemID:  "arcade",
				Processed: 1,
			},
		}, ""),
	)
}

func TestParseMediaManageUpdate(t *testing.T) {
	t.Parallel()

	currentStep := 3
	indexing, err := parseMediaManageUpdate(
		models.NotificationMediaIndexing,
		`{"indexing":true,"paused":true,"currentStep":3,"totalSteps":10}`,
	)
	assert.NoError(t, err)
	assert.Equal(t, models.NotificationMediaIndexing, indexing.method)
	assert.True(t, indexing.indexing.Indexing)
	assert.True(t, indexing.indexing.Paused)
	assert.Equal(t, &currentStep, indexing.indexing.CurrentStep)

	scraping, err := parseMediaManageUpdate(
		models.NotificationMediaScraping,
		`{"scraping":true,"scraperId":"screenscraper","currentSystem":{"systemId":"nes","processed":4,"total":9}}`,
	)
	assert.NoError(t, err)
	assert.Equal(t, models.NotificationMediaScraping, scraping.method)
	assert.True(t, scraping.scraping.Scraping)
	assert.Equal(t, "screenscraper", scraping.scraping.ScraperID)
	assert.Equal(t, "nes", scraping.scraping.CurrentSystem.SystemID)
	assert.Equal(t, 4, scraping.scraping.CurrentSystem.Processed)
}

func TestParseMediaManageUpdateErrors(t *testing.T) {
	t.Parallel()

	_, err := parseMediaManageUpdate(models.NotificationMediaIndexing, `{`)
	assert.ErrorContains(t, err, "failed to unmarshal indexing status response")

	_, err = parseMediaManageUpdate(models.NotificationMediaScraping, `{`)
	assert.ErrorContains(t, err, "failed to unmarshal scraping status response")

	_, err = parseMediaManageUpdate("media.unknown", `{}`)
	assert.ErrorContains(t, err, "unexpected notification method")
}

func TestBlockedMediaOperationMenuLabels(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Update index: scrape running", mediaIndexBlockedByScrapeLabel())
	assert.Equal(t, "Scrape metadata: index running", mediaScrapeBlockedByIndexLabel())
}

func TestScraperSupportsSystem(t *testing.T) {
	t.Parallel()

	assert.True(t, scraperSupportsSystem(models.ScraperInfo{}, "nes"))
	assert.True(t, scraperSupportsSystem(models.ScraperInfo{SupportedSystems: []string{"NES"}}, "nes"))
	assert.False(t, scraperSupportsSystem(models.ScraperInfo{SupportedSystems: []string{"snes"}}, "nes"))
}

func TestFilterScrapeSystems(t *testing.T) {
	t.Parallel()

	systems := []SystemItem{
		{ID: "nes", Name: "Nintendo Entertainment System"},
		{ID: "snes", Name: "Super Nintendo"},
		{ID: "gb", Name: "Game Boy"},
	}
	scraper := models.ScraperInfo{SupportedSystems: []string{"SNES", "gb"}}

	assert.Equal(t, []SystemItem{
		{ID: "snes", Name: "Super Nintendo"},
		{ID: "gb", Name: "Game Boy"},
	}, filterScrapeSystems(systems, scraper))
	assert.Equal(t, systems, filterScrapeSystems(systems, models.ScraperInfo{}))
}

func TestPruneSelectedScrapeSystems(t *testing.T) {
	t.Parallel()

	systems := []SystemItem{
		{ID: "nes", Name: "Nintendo Entertainment System"},
		{ID: "gb", Name: "Game Boy"},
	}
	selected := []string{"snes", "gb", "nes"}

	assert.Equal(t, []string{"gb", "nes"}, pruneSelectedScrapeSystems(selected, systems))
}

func TestFormatScrapeSystemsLabel(t *testing.T) {
	t.Parallel()

	systems := []SystemItem{{ID: "nes", Name: "Nintendo Entertainment System"}}

	assert.Equal(t, "No supported systems", formatScrapeSystemsLabel(nil, nil))
	assert.Equal(t, "All systems", formatScrapeSystemsLabel(nil, systems))
	assert.Equal(t, "nes", formatScrapeSystemsLabel([]string{"nes"}, systems))
}

func TestScrapeProgressHelpers(t *testing.T) {
	t.Parallel()

	currentStep := 2
	totalSteps := 5
	status := models.ScrapingStatusResponse{
		CurrentStep: &currentStep,
		TotalSteps:  &totalSteps,
		Processed:   7,
		Total:       10,
		CurrentSystem: &models.ScrapeSystemProgressResponse{
			Processed: 3,
			Total:     6,
		},
	}

	assert.InDelta(t, 0.4, scrapeOverallProgress(&status), 0.001)
	assert.InDelta(t, 0.5, scrapeCurrentSystemProgress(&status), 0.001)
	fallbackStatus := models.ScrapingStatusResponse{
		Processed: 7,
		Total:     10,
	}
	assert.InDelta(t, 0.7, scrapeOverallProgress(&fallbackStatus), 0.001)
}

func TestMediaIndexProgress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		current  int
		total    int
		expected float64
	}{
		{
			name:     "invalid current",
			current:  0,
			total:    10,
			expected: 0,
		},
		{
			name:     "uses current step like web UI",
			current:  3,
			total:    10,
			expected: 0.3,
		},
		{
			name:     "writing database uses current step",
			current:  10,
			total:    10,
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := mediaIndexProgress(tt.current, tt.total)
			assert.InDelta(t, tt.expected, result, 0.001)
		})
	}
}

func TestHandleMediaManageEscape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		frontPage   string
		wantBack    bool
		wantInitial bool
		wantSystems bool
	}{
		{
			name:        "systems returns to setup",
			frontPage:   mediaManageSystemsPage,
			wantSystems: true,
		},
		{
			name:        "setup returns to initial",
			frontPage:   mediaManageSetupPage,
			wantInitial: true,
		},
		{
			name:        "progress returns to initial",
			frontPage:   mediaManageProgressPage,
			wantInitial: true,
		},
		{
			name:      "complete exits manage media",
			frontPage: mediaManageCompletePage,
			wantBack:  true,
		},
		{
			name:      "initial exits manage media",
			frontPage: mediaManageInitialPage,
			wantBack:  true,
		},
		{
			name:      "loading exits manage media",
			frontPage: mediaManageLoadingPage,
			wantBack:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			statePages := tview.NewPages()
			for _, page := range []string{
				mediaManageLoadingPage,
				mediaManageInitialPage,
				mediaManageSetupPage,
				mediaManageProgressPage,
				mediaManageCompletePage,
				mediaManageSystemsPage,
			} {
				statePages.AddPage(page, tview.NewTextView().SetText(page), true, page == mediaManageInitialPage)
			}
			statePages.SwitchToPage(tt.frontPage)

			backCalled := false
			initialCalled := false
			systemsCalled := false

			handleMediaManageEscape(
				statePages,
				func() { backCalled = true },
				func() { initialCalled = true },
				func() { systemsCalled = true },
			)

			assert.Equal(t, tt.wantBack, backCalled)
			assert.Equal(t, tt.wantInitial, initialCalled)
			assert.Equal(t, tt.wantSystems, systemsCalled)
		})
	}
}

func TestHandleMediaManageEscape_SystemsPagePreservesSelection(t *testing.T) {
	t.Parallel()

	statePages := tview.NewPages()
	statePages.AddPage(mediaManageSetupPage, tview.NewTextView().SetText("setup"), true, false)
	statePages.AddPage(mediaManageSystemsPage, tview.NewTextView().SetText("systems"), true, true)
	selected := []string{"nes"}

	handleMediaManageEscape(
		statePages,
		func() { selected = nil },
		func() { selected = nil },
		func() {
			statePages.RemovePage(mediaManageSystemsPage)
			statePages.SwitchToPage(mediaManageSetupPage)
		},
	)

	frontPage, _ := statePages.GetFrontPage()
	assert.Equal(t, mediaManageSetupPage, frontPage)
	assert.Equal(t, []string{"nes"}, selected)
}

// intPtr is a helper to create *int.
func intPtr(v int) *int {
	return &v
}

func TestProgressBar_Draw_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 40, 3)
	defer runner.Stop()

	pb := NewProgressBar()
	pb.SetBorder(true)
	pb.SetProgress(0.5)

	runner.Start(pb)
	runner.Draw()

	// The progress bar should render with some filled and empty characters
	// We can't easily assert on specific characters, but we verify it doesn't panic
	// and renders something
}

func TestProgressBar_Draw_Empty(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 40, 3)
	defer runner.Stop()

	pb := NewProgressBar()
	pb.SetBorder(true)
	pb.SetProgress(0)

	runner.Start(pb)
	runner.Draw()

	// Verify no panic with 0% progress
}

func TestProgressBar_Draw_Full(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 40, 3)
	defer runner.Stop()

	pb := NewProgressBar()
	pb.SetBorder(true)
	pb.SetProgress(1.0)

	runner.Start(pb)
	runner.Draw()

	// Verify no panic with 100% progress
}

func TestProgressBar_BoundaryValues(t *testing.T) {
	t.Parallel()

	pb := NewProgressBar()

	// Values at boundaries
	pb.SetProgress(0)
	assert.InDelta(t, 0.0, pb.GetProgress(), 0.001)

	pb.SetProgress(1)
	assert.InDelta(t, 1.0, pb.GetProgress(), 0.001)

	// Just inside boundaries
	pb.SetProgress(0.001)
	assert.InDelta(t, 0.001, pb.GetProgress(), 0.001)

	pb.SetProgress(0.999)
	assert.InDelta(t, 0.999, pb.GetProgress(), 0.001)
}
